package views

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

// StreamConfig holds the configuration for handleStreamOrAsync.
// AppID scopes the job to an app so clients can resume it after a page refresh
// via GET /v2/apps/{appId}/active-job. Pass uuid.Nil for jobs that aren't
// app-scoped.
type StreamConfig struct {
	JobType    string
	AppID      uuid.UUID
	StreamFunc func(ctx context.Context, events chan<- responses.SSEEvent, jobID string)
}

// handleStreamOrAsync always creates a background job that persists milestone events.
// In SSE mode it also streams events to the client in real time. If the client disconnects
// (e.g. page refresh), the job keeps running in the background and can be polled via
// GET /v1/jobs/{jobId}.
func (v *HttpViews) handleStreamOrAsync(w http.ResponseWriter, r *http.Request, user *models.User, cfg StreamConfig) {
	// Always create a background job
	job, err := v.Controller.CreateJob(user.ID, cfg.JobType, cfg.AppID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusTooManyRequests)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	v.Controller.RunningJobs.Store(job.ID.String(), cancel)

	events := make(chan responses.SSEEvent, 100)

	// Create an event bus for this job so reconnected clients can subscribe
	bus := controllers.NewEventBus()
	v.Controller.JobEventBuses.Store(job.ID.String(), bus)

	go cfg.StreamFunc(ctx, events, job.ID.String())

	if r.URL.Query().Get("background") == "true" {
		// Pure background mode: drain events in goroutine, return job ID immediately
		go v.drainEventsToJob(ctx, cancel, job, events)

		utils.JSONResponse(w, responses.JobCreatedResponse{
			JobID:  job.ID,
			Status: job.Status,
		}, http.StatusAccepted)
		return
	}

	// SSE mode: stream to client AND persist milestone events to DB
	flusher, ok := w.(http.Flusher)
	if !ok {
		// Fallback: run as pure background job
		go v.drainEventsToJob(ctx, cancel, job, events)
		utils.JSONErrorResponse(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	utils.SetSSEHeaders(w)

	// Send job_id as first event so frontend can resume polling on page refresh.
	// seq=0 — this is a bootstrap frame outside the agent's per-event sequence.
	utils.WriteSSEEvent(w, flusher, 0, "job_started", map[string]any{"job_id": job.ID})

	clientGone := false
	// nextSeq is the per-job monotonic sequence number assigned to every
	// event the main goroutine processes. It is the single source of truth
	// for ordering — we stamp it into both the live SSEEvent and any
	// persisted JobEvent so reconnect via Last-Event-ID can deduplicate
	// against either source. The previous "eventIndex" was a per-milestone
	// counter; nextSeq increments on every event, milestone or not.
	var nextSeq int64
	var lastEvent *responses.SSEEvent
	var allMilestoneEvents []models.JobEvent

	streamState := newStreamStateBuffer()
	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()
	stateFlush := time.NewTicker(500 * time.Millisecond)
	defer stateFlush.Stop()

	// Stream state is flushed to the DB asynchronously. A slow
	// UpdateGenerationJobStreamState used to block the event loop here,
	// which in turn stalled agentSSEAdapter.Emit's channel send past its 10s
	// timeout and silently dropped events. The async flusher coalesces
	// bursty updates and drains the tail in a single goroutine so the event
	// loop never waits on the DB.
	stateFlusher := newAsyncStateFlusher(
		streamState.Snapshot,
		func(raw json.RawMessage) {
			if err := v.Controller.DB.UpdateGenerationJobStreamState(job.ID.String(), raw); err != nil {
				slog.Debug("failed to flush stream state", "job_id", job.ID, "error", err)
			}
		},
	)

	for {
		select {
		case event, ok := <-events:
			if !ok {
				// Channel closed — generation finished
				goto done
			}
			nextSeq++
			event.Seq = nextSeq
			lastEvent = &event

			// Persist milestone events to the DB BEFORE publishing to the
			// bus. This guarantees that a reconnected subscriber that reads
			// the DB after subscribing sees no event in the bus that isn't
			// also already in the DB — the dedupe-by-seq logic in StreamJob
			// relies on this ordering invariant.
			if controllers.IsMilestoneEvent(event.Event) {
				jobEvent := models.JobEvent{
					Seq:   nextSeq,
					Event: event.Event,
					Data:  controllers.StripHeavyFields(event.Event, event.Data),
					Time:  time.Now().UnixMilli(),
				}
				allMilestoneEvents = append(allMilestoneEvents, jobEvent)
				if err := v.Controller.DB.AppendGenerationJobEvent(job.ID.String(), jobEvent); err != nil {
					slog.Error("failed to persist job event", "job_id", job.ID, "error", err)
				}
				// Flush the partial buffer alongside every milestone so reconnects
				// between milestones see the latest authoritative state.
				stateFlusher.Trigger()
			}

			// Fan out to any reconnected SSE subscribers
			bus.Publish(event)

			// Update the partial stream buffer for refresh recovery.
			streamState.Apply(event)

			// Write to SSE if client is still connected
			if !clientGone {
				if err := utils.WriteSSEEvent(w, flusher, event.Seq, event.Event, event.Data); err != nil {
					clientGone = true
					slog.Info("SSE client disconnected, continuing job in background", "job_id", job.ID)
				}
			}

		case <-stateFlush.C:
			stateFlusher.Trigger()

		case <-keepalive.C:
			// Send SSE keepalive comment to prevent proxy/client timeout
			if !clientGone {
				if _, err := fmt.Fprintf(w, ":keepalive\n\n"); err != nil {
					clientGone = true
					slog.Info("SSE client disconnected during keepalive, continuing job in background", "job_id", job.ID)
				} else {
					flusher.Flush()
				}
			}

		case <-ctx.Done():
			// Context expired (timeout) — stop waiting for events and finalize
			slog.Warn("Generation job timed out, finalizing", "job_id", job.ID, "elapsed", time.Since(job.CreatedAt))
			goto done
		}
	}
done:

	// Explicit terminal SSE frame so clients know unambiguously that the
	// stream is over. Without this, the FE has to infer "done" from event
	// names ("done"/"error"/"rollback"), which is fragile under reconnect
	// races and unknown future event types. The frame uses seq=0 — it's
	// out-of-band metadata, not a numbered event.
	if !clientGone {
		_ = utils.WriteSSEEvent(w, flusher, 0, "end", map[string]any{
			"job_id": job.ID,
		})
	}

	// Wait for any in-flight async flush to land before the authoritative
	// clear below, otherwise a late background write could overwrite the
	// cleared "{}" state and leak stale partial text on the next reconnect.
	stateFlusher.Wait()

	// Final consistency write: replace the events array with the complete in-memory
	// copy. Individual AppendGenerationJobEvent calls may have failed under DB pressure,
	// so this ensures the persisted state matches what was actually streamed.
	if len(allMilestoneEvents) > 0 {
		if err := v.Controller.DB.SetGenerationJobEvents(job.ID.String(), allMilestoneEvents); err != nil {
			slog.Error("failed to batch-write job events", "job_id", job.ID, "error", err)
		}
	}

	// Clear any lingering partial stream state — the terminal milestone is authoritative.
	if err := v.Controller.DB.UpdateGenerationJobStreamState(job.ID.String(), json.RawMessage("{}")); err != nil {
		slog.Debug("failed to clear stream state on finalize", "job_id", job.ID, "error", err)
	}

	// StreamFunc finished (channel closed) — finalize job before cancelling context,
	// otherwise FinalizeJob sees ctx.Err() != nil and marks the job as "cancelled".
	v.Controller.FinalizeJob(job.ID.String(), ctx, lastEvent)
	cancel()

	// Close the event bus so reconnected subscribers know the job is done
	bus.Close()
	v.Controller.JobEventBuses.Delete(job.ID.String())
}

// drainEventsToJob reads all events from the channel, persists milestone events,
// and finalizes the job when done. Used for pure background mode.
func (v *HttpViews) drainEventsToJob(ctx context.Context, cancel context.CancelFunc, job *models.GenerationJob, events <-chan responses.SSEEvent) {
	defer cancel()

	// See handleStreamOrAsync for the rationale on the per-job seq counter.
	var nextSeq int64
	var lastEvent *responses.SSEEvent
	var allMilestoneEvents []models.JobEvent

	streamState := newStreamStateBuffer()
	stateFlush := time.NewTicker(500 * time.Millisecond)
	defer stateFlush.Stop()

	// See handleStreamOrAsync for the async flusher rationale.
	stateFlusher := newAsyncStateFlusher(
		streamState.Snapshot,
		func(raw json.RawMessage) {
			if err := v.Controller.DB.UpdateGenerationJobStreamState(job.ID.String(), raw); err != nil {
				slog.Debug("failed to flush stream state", "job_id", job.ID, "error", err)
			}
		},
	)

	for {
		select {
		case event, ok := <-events:
			if !ok {
				// Channel closed — generation finished
				goto finalize
			}
			nextSeq++
			event.Seq = nextSeq
			lastEvent = &event

			// Persist BEFORE publish — see handleStreamOrAsync for rationale.
			if controllers.IsMilestoneEvent(event.Event) {
				jobEvent := models.JobEvent{
					Seq:   nextSeq,
					Event: event.Event,
					Data:  controllers.StripHeavyFields(event.Event, event.Data),
					Time:  time.Now().UnixMilli(),
				}
				allMilestoneEvents = append(allMilestoneEvents, jobEvent)
				if err := v.Controller.DB.AppendGenerationJobEvent(job.ID.String(), jobEvent); err != nil {
					slog.Error("failed to persist job event", "job_id", job.ID, "error", err)
				}
				stateFlusher.Trigger()
			}

			// Fan out to any reconnected SSE subscribers
			if busVal, ok := v.Controller.JobEventBuses.Load(job.ID.String()); ok {
				if b, ok := busVal.(*controllers.EventBus); ok {
					b.Publish(event)
				}
			}

			streamState.Apply(event)

		case <-stateFlush.C:
			stateFlusher.Trigger()

		case <-ctx.Done():
			// Context expired — stop draining and finalize
			slog.Warn("Background job timed out, finalizing", "job_id", job.ID)
			goto finalize
		}
	}
finalize:

	// Wait for any in-flight async flush to land before the authoritative clear.
	stateFlusher.Wait()

	// Final consistency write: replace with complete in-memory copy
	if len(allMilestoneEvents) > 0 {
		if err := v.Controller.DB.SetGenerationJobEvents(job.ID.String(), allMilestoneEvents); err != nil {
			slog.Error("failed to batch-write job events", "job_id", job.ID, "error", err)
		}
	}

	if err := v.Controller.DB.UpdateGenerationJobStreamState(job.ID.String(), json.RawMessage("{}")); err != nil {
		slog.Debug("failed to clear stream state on finalize", "job_id", job.ID, "error", err)
	}

	v.Controller.FinalizeJob(job.ID.String(), ctx, lastEvent)

	// Close the event bus so reconnected subscribers know the job is done
	if busVal, ok := v.Controller.JobEventBuses.LoadAndDelete(job.ID.String()); ok {
		if b, ok := busVal.(*controllers.EventBus); ok {
			b.Close()
		}
	}
}

// GET /v1/jobs/{jobId}
func (v *HttpViews) GetJob(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	jobID := chi.URLParam(r, "jobId")
	if jobID == "" {
		utils.JSONErrorResponse(w, "job id is required", http.StatusBadRequest)
		return
	}

	job, err := v.Controller.GetJob(jobID, user.ID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	// Support ?after=N to only return events after index N
	eventsToReturn := job.Events
	if afterStr := r.URL.Query().Get("after"); afterStr != "" {
		after, err := strconv.Atoi(afterStr)
		if err == nil && after >= 0 {
			var allEvents []json.RawMessage
			if json.Unmarshal(job.Events, &allEvents) == nil && after < len(allEvents) {
				filtered := allEvents[after:]
				eventsToReturn, _ = json.Marshal(filtered)
			} else if after >= len(allEvents) {
				eventsToReturn = json.RawMessage("[]")
			}
		}
	}

	utils.JSONResponse(w, responses.JobResponse{
		ID:          job.ID,
		Type:        job.Type,
		Status:      job.Status,
		Events:      eventsToReturn,
		StreamState: job.StreamState,
		Result:      job.Result,
		Error:       job.Error,
		CreatedAt:   job.CreatedAt,
		UpdatedAt:   job.UpdatedAt,
	}, http.StatusOK)
}

// GET /v1/jobs/active
func (v *HttpViews) GetActiveJobs(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	jobs, err := v.Controller.GetActiveJobs(user.ID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var resp []responses.JobResponse
	for _, job := range jobs {
		resp = append(resp, responses.JobResponse{
			ID:          job.ID,
			Type:        job.Type,
			Status:      job.Status,
			Events:      job.Events,
			StreamState: job.StreamState,
			Result:      job.Result,
			Error:       job.Error,
			CreatedAt:   job.CreatedAt,
			UpdatedAt:   job.UpdatedAt,
		})
	}

	if resp == nil {
		resp = []responses.JobResponse{}
	}

	utils.JSONResponse(w, resp, http.StatusOK)
}

// GET /v2/apps/jobs/{jobId}/stream — reconnect to a job's SSE event stream.
//
// This endpoint serves three cases:
//  1. Live: job is running/awaiting_input AND the in-process EventBus exists.
//     Replay persisted state + milestones, then tail live events until the
//     bus closes or the client disconnects.
//  2. Terminal: job is completed/failed/cancelled. Replay persisted state +
//     milestones from the DB (the bus is already gone), then close.
//  3. Orphaned: job is marked running/awaiting_input but no bus is attached
//     (process restart before the sweeper ran). Replay whatever state we
//     have from the DB and close — the next /active-job poll will reflect
//     the sweeper decision.
//
// Before this was multi-path, case 2 returned 404 ("no active stream for this
// job") and the UI hung forever on refresh right after a generation finished.
func (v *HttpViews) StreamJob(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	jobID := chi.URLParam(r, "jobId")
	if jobID == "" {
		utils.JSONErrorResponse(w, "job id is required", http.StatusBadRequest)
		return
	}

	// Verify ownership
	job, err := v.Controller.GetJob(jobID, user.ID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		utils.JSONErrorResponse(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Resume cursor for idempotent reconnect. Browsers' EventSource sends
	// `Last-Event-ID` automatically on reconnect; the dashboard's custom
	// fetch+ReadableStream parser does the same. Both also accept `?since=N`
	// as a query-param fallback for clients that can't set headers.
	since := parseLastEventID(r)

	utils.SetSSEHeaders(w)

	// Step 1: always replay the persisted stream buffer + milestone events
	// the client hasn't seen yet. `since` filters out anything <= the last
	// event id the client confirms it already has, so reconnect doesn't
	// duplicate tool cards or file events.
	replayPersistedJobEvents(w, flusher, job, since)

	// Compute the dedupe cursor for the live tail. Any live event the bus
	// delivers with seq <= maxReplayedSeq is something we just replayed
	// from the DB (race window between subscribe and DB read) and must
	// not be written twice. Start at `since`; the replay updates it via
	// the closure below if higher seq events were emitted.
	maxReplayedSeq := computeMaxReplayedSeq(job, since)

	// Step 2: decide whether a live subscription is possible.
	isLive := job.Status == "running" || job.Status == "awaiting_input"
	busVal, busOK := v.Controller.JobEventBuses.Load(jobID)

	if !isLive {
		// Terminal job — the replay is authoritative. Send the explicit
		// terminal frame so the client's stream listener exits cleanly,
		// then close.
		_ = utils.WriteSSEEvent(w, flusher, 0, "end", map[string]any{
			"job_id": job.ID,
		})
		slog.Info("StreamJob: replayed terminal job and closed", "job_id", jobID, "status", job.Status, "since", since)
		return
	}

	if !busOK {
		// Running job with no bus: the process that owned the job is gone
		// (or never existed — multi-instance deployments). Serve what we
		// have; the next /active-job poll after the sweeper runs will
		// reflect the eventual failure.
		slog.Warn("StreamJob: running job has no event bus — replay only", "job_id", jobID)
		return
	}

	bus, ok := busVal.(*controllers.EventBus)
	if !ok {
		slog.Error("StreamJob: JobEventBuses entry is not *EventBus", "job_id", jobID)
		return
	}

	// Step 3: subscribe to live events and tail.
	subID, ch := bus.Subscribe()
	defer bus.Unsubscribe(subID)

	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case event, ok := <-ch:
			if !ok {
				// Bus closed — job finished. Send the explicit terminal
				// frame so the FE listener exits cleanly without inferring
				// completion from event names.
				_ = utils.WriteSSEEvent(w, flusher, 0, "end", map[string]any{
					"job_id": job.ID,
				})
				return
			}
			// Dedupe against the replay window.
			if event.Seq > 0 && event.Seq <= maxReplayedSeq {
				continue
			}
			if event.Seq > maxReplayedSeq {
				maxReplayedSeq = event.Seq
			}
			if err := utils.WriteSSEEvent(w, flusher, event.Seq, event.Event, event.Data); err != nil {
				// Client disconnected
				return
			}

		case <-keepalive.C:
			if _, err := fmt.Fprintf(w, ":keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()

		case <-r.Context().Done():
			return
		}
	}
}

// parseLastEventID extracts the resume cursor from the request — header
// `Last-Event-ID` (browser EventSource standard, also used by our custom
// parser) with a `?since=N` query-param fallback. Returns 0 when not set
// or unparseable; 0 means "send everything".
func parseLastEventID(r *http.Request) int64 {
	if h := r.Header.Get("Last-Event-ID"); h != "" {
		if n, err := strconv.ParseInt(h, 10, 64); err == nil && n >= 0 {
			return n
		}
	}
	if q := r.URL.Query().Get("since"); q != "" {
		if n, err := strconv.ParseInt(q, 10, 64); err == nil && n >= 0 {
			return n
		}
	}
	return 0
}

// computeMaxReplayedSeq scans the persisted milestone events and returns
// the highest seq <= the replay threshold that was actually emitted by
// replayPersistedJobEvents. The live tail dedupes against this value so a
// race between DB snapshot read and bus subscribe doesn't show events twice.
func computeMaxReplayedSeq(job *models.GenerationJob, since int64) int64 {
	max := since
	if len(job.Events) == 0 {
		return max
	}
	var milestoneEvents []json.RawMessage
	if json.Unmarshal(job.Events, &milestoneEvents) != nil {
		return max
	}
	for _, raw := range milestoneEvents {
		var evt struct {
			Seq int64 `json:"seq"`
		}
		if json.Unmarshal(raw, &evt) == nil && evt.Seq > max {
			max = evt.Seq
		}
	}
	return max
}

// replayPersistedJobEvents writes the last persisted partial stream buffer
// followed by each recorded milestone event into the SSE response. Used by
// StreamJob both for live reconnects (as a primer before tailing the bus)
// and for terminal reconnects (as the only data source).
//
// `since` is the client's last-seen seq (from the Last-Event-ID header or
// ?since= query param). Milestone events with seq <= since are skipped so
// reconnect doesn't redeliver tool cards or file events the client already
// has. The stream_state snapshot is always sent — it carries no seq, and
// the client uses it to rehydrate transient narration buffers regardless
// of what milestones it has seen.
//
// Errors from individual writes are intentionally swallowed — the caller has
// no recovery path mid-stream, and partial replay is still more useful than
// a 500 response.
func replayPersistedJobEvents(w http.ResponseWriter, flusher http.Flusher, job *models.GenerationJob, since int64) {
	if len(job.StreamState) > 0 && string(job.StreamState) != "{}" && string(job.StreamState) != "null" {
		var stateData any
		if err := json.Unmarshal(job.StreamState, &stateData); err == nil {
			_ = utils.WriteSSEEvent(w, flusher, 0, "stream_state", stateData)
		}
	}

	var milestoneEvents []json.RawMessage
	if json.Unmarshal(job.Events, &milestoneEvents) == nil {
		for _, raw := range milestoneEvents {
			var evt struct {
				Seq   int64  `json:"seq"`
				Event string `json:"event"`
				Data  any    `json:"data"`
			}
			if json.Unmarshal(raw, &evt) != nil {
				continue
			}
			if evt.Seq > 0 && evt.Seq <= since {
				continue
			}
			_ = utils.WriteSSEEvent(w, flusher, evt.Seq, evt.Event, evt.Data)
		}
	}
}

// DELETE /v1/jobs/{jobId}
func (v *HttpViews) CancelJob(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	jobID := chi.URLParam(r, "jobId")
	if jobID == "" {
		utils.JSONErrorResponse(w, "job id is required", http.StatusBadRequest)
		return
	}

	// Diagnostic: users report runs getting canceled without clicking Stop,
	// and the frontend only calls this endpoint from the Stop button. Capture
	// enough request context to tell which tab/device/automation hit it.
	ip := r.RemoteAddr
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ip = strings.TrimSpace(strings.SplitN(xff, ",", 2)[0])
	} else if xri := r.Header.Get("X-Real-IP"); xri != "" {
		ip = strings.TrimSpace(xri)
	}
	slog.Info("CancelJob received",
		"job_id", jobID,
		"user_id", user.ID,
		"user_email", user.Email,
		"ip", ip,
		"user_agent", r.UserAgent(),
		"referer", r.Header.Get("Referer"),
		"origin", r.Header.Get("Origin"),
	)

	err = v.Controller.CancelJob(jobID, user.ID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, responses.SuccessResponse{Success: true}, http.StatusOK)
}
