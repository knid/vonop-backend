package views

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

// StreamConfig holds the configuration for handleStreamOrAsync.
type StreamConfig struct {
	JobType    string
	StreamFunc func(ctx context.Context, events chan<- responses.SSEEvent, jobID string)
}

// handleStreamOrAsync always creates a background job that persists milestone events.
// In SSE mode it also streams events to the client in real time. If the client disconnects
// (e.g. page refresh), the job keeps running in the background and can be polled via
// GET /v1/jobs/{jobId}.
func (v *HttpViews) handleStreamOrAsync(w http.ResponseWriter, r *http.Request, user *models.User, cfg StreamConfig) {
	// Always create a background job
	job, err := v.Controller.CreateJob(user.ID, cfg.JobType)
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

	// Send job_id as first event so frontend can resume polling on page refresh
	utils.WriteSSEEvent(w, flusher, "job_started", map[string]any{"job_id": job.ID})

	clientGone := false
	eventIndex := 0
	var lastEvent *responses.SSEEvent
	var allMilestoneEvents []models.JobEvent

	streamState := newStreamStateBuffer()
	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()
	stateFlush := time.NewTicker(500 * time.Millisecond)
	defer stateFlush.Stop()

	flushStreamState := func() {
		raw, ok := streamState.Snapshot()
		if !ok {
			return
		}
		if err := v.Controller.DB.UpdateGenerationJobStreamState(job.ID.String(), raw); err != nil {
			slog.Debug("failed to flush stream state", "job_id", job.ID, "error", err)
		}
	}

	for {
		select {
		case event, ok := <-events:
			if !ok {
				// Channel closed — generation finished
				goto done
			}
			lastEvent = &event

			// Fan out to any reconnected SSE subscribers
			bus.Publish(event)

			// Update the partial stream buffer for refresh recovery.
			streamState.Apply(event)

			// Always persist milestone events to DB
			if controllers.IsMilestoneEvent(event.Event) {
				jobEvent := models.JobEvent{
					Index: eventIndex,
					Event: event.Event,
					Data:  controllers.StripHeavyFields(event.Event, event.Data),
					Time:  time.Now().UnixMilli(),
				}
				allMilestoneEvents = append(allMilestoneEvents, jobEvent)
				if err := v.Controller.DB.AppendGenerationJobEvent(job.ID.String(), jobEvent); err != nil {
					slog.Error("failed to persist job event", "job_id", job.ID, "error", err)
				}
				eventIndex++
				// Flush the partial buffer alongside every milestone so reconnects
				// between milestones see the latest authoritative state.
				flushStreamState()
			}

			// Write to SSE if client is still connected
			if !clientGone {
				if err := utils.WriteSSEEvent(w, flusher, event.Event, event.Data); err != nil {
					clientGone = true
					slog.Info("SSE client disconnected, continuing job in background", "job_id", job.ID)
				}
			}

		case <-stateFlush.C:
			flushStreamState()

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

	eventIndex := 0
	var lastEvent *responses.SSEEvent
	var allMilestoneEvents []models.JobEvent

	streamState := newStreamStateBuffer()
	stateFlush := time.NewTicker(500 * time.Millisecond)
	defer stateFlush.Stop()

	flushStreamState := func() {
		raw, ok := streamState.Snapshot()
		if !ok {
			return
		}
		if err := v.Controller.DB.UpdateGenerationJobStreamState(job.ID.String(), raw); err != nil {
			slog.Debug("failed to flush stream state", "job_id", job.ID, "error", err)
		}
	}

	for {
		select {
		case event, ok := <-events:
			if !ok {
				// Channel closed — generation finished
				goto finalize
			}
			lastEvent = &event

			// Fan out to any reconnected SSE subscribers
			if busVal, ok := v.Controller.JobEventBuses.Load(job.ID.String()); ok {
				if b, ok := busVal.(*controllers.EventBus); ok {
					b.Publish(event)
				}
			}

			streamState.Apply(event)

			if controllers.IsMilestoneEvent(event.Event) {
				jobEvent := models.JobEvent{
					Index: eventIndex,
					Event: event.Event,
					Data:  controllers.StripHeavyFields(event.Event, event.Data),
					Time:  time.Now().UnixMilli(),
				}
				allMilestoneEvents = append(allMilestoneEvents, jobEvent)
				if err := v.Controller.DB.AppendGenerationJobEvent(job.ID.String(), jobEvent); err != nil {
					slog.Error("failed to persist job event", "job_id", job.ID, "error", err)
				}
				eventIndex++
				flushStreamState()
			}

		case <-stateFlush.C:
			flushStreamState()

		case <-ctx.Done():
			// Context expired — stop draining and finalize
			slog.Warn("Background job timed out, finalizing", "job_id", job.ID)
			goto finalize
		}
	}
finalize:

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

// POST /v2/apps/jobs/{jobId}/clarify
func (v *HttpViews) ClarifyJob(w http.ResponseWriter, r *http.Request) {
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

	// Verify ownership and status
	job, err := v.Controller.GetJob(jobID, user.ID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}
	if job.Status != "awaiting_input" {
		utils.JSONErrorResponse(w, "job is not awaiting input", http.StatusBadRequest)
		return
	}

	var req requests.ClarifyJobRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Convert request answers to controller type
	answers := make([]controllers.ClarificationAnswer, len(req.Answers))
	for i, a := range req.Answers {
		answers[i] = controllers.ClarificationAnswer{ID: a.ID, Answer: a.Answer}
	}

	result := controllers.ClarificationResult{
		Answers: answers,
		Skip:    req.Skip,
	}

	if err := v.Controller.SubmitClarification(jobID, result); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, responses.SuccessResponse{Success: true}, http.StatusOK)
}

// POST /v2/apps/jobs/{jobId}/reply
func (v *HttpViews) ReplyJob(w http.ResponseWriter, r *http.Request) {
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

	// Verify ownership and status
	job, err := v.Controller.GetJob(jobID, user.ID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}
	if job.Status != "awaiting_input" {
		utils.JSONErrorResponse(w, "job is not awaiting input", http.StatusBadRequest)
		return
	}

	var req requests.ReplyJobRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := v.Controller.SubmitReply(jobID, controllers.ConversationReply{Message: req.Message}); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, responses.SuccessResponse{Success: true}, http.StatusOK)
}

// GET /v2/apps/jobs/{jobId}/stream — reconnect to a running job's SSE event stream
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
	if job.Status != "running" && job.Status != "awaiting_input" {
		utils.JSONErrorResponse(w, "job is not running", http.StatusBadRequest)
		return
	}

	// Find the event bus for this job
	busVal, ok := v.Controller.JobEventBuses.Load(jobID)
	if !ok {
		utils.JSONErrorResponse(w, "no active stream for this job", http.StatusNotFound)
		return
	}
	bus, ok := busVal.(*controllers.EventBus)
	if !ok {
		utils.JSONErrorResponse(w, "internal error", http.StatusInternalServerError)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		utils.JSONErrorResponse(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	utils.SetSSEHeaders(w)

	// Emit the last persisted partial stream buffer first so the client can rehydrate
	// in-flight streaming text/thinking/tool calls before milestone replay takes over.
	if len(job.StreamState) > 0 && string(job.StreamState) != "{}" && string(job.StreamState) != "null" {
		var stateData any
		if err := json.Unmarshal(job.StreamState, &stateData); err == nil {
			utils.WriteSSEEvent(w, flusher, "stream_state", stateData)
		}
	}

	// Then replay persisted milestone events so the client catches up.
	var milestoneEvents []json.RawMessage
	if json.Unmarshal(job.Events, &milestoneEvents) == nil {
		for _, raw := range milestoneEvents {
			var evt struct {
				Event string `json:"event"`
				Data  any    `json:"data"`
			}
			if json.Unmarshal(raw, &evt) == nil {
				utils.WriteSSEEvent(w, flusher, evt.Event, evt.Data)
			}
		}
	}

	// Subscribe to live events
	subID, ch := bus.Subscribe()
	defer bus.Unsubscribe(subID)

	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case event, ok := <-ch:
			if !ok {
				// Bus closed — job finished
				return
			}
			if err := utils.WriteSSEEvent(w, flusher, event.Event, event.Data); err != nil {
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

	err = v.Controller.CancelJob(jobID, user.ID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, responses.SuccessResponse{Success: true}, http.StatusOK)
}
