package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/knid/wemob/internal/agent"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/jobstate"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/services/jobstream"
	"github.com/knid/wemob/internal/tasks"
)

// HandleAgentRunTask is the Asynq handler for agent:run tasks. It runs
// agent.Run in the worker process and streams events to API SSE clients
// via Redis pub/sub.
//
// Retry policy: enqueued with MaxRetry=0 because agent.Run is not
// idempotent — the partial unique index on app_agent_runs would block a
// retry's InsertAgentRun anyway. Worker crashes mid-run are caught by
// ReapStuckAgentRunsTask (30 min stale window) which flips the orphaned
// row to failed.
//
// Cancellation: subscribes to jobstream.CancelChannel(jobID) for the
// duration of the run. CancelJob in the API publishes "1" to that
// channel; this handler cancels its context and lets agent.Run unwind
// via its existing ctx-aware paths.
func (c *Controller) HandleAgentRunTask(ctx context.Context, t *asynq.Task) error {
	var p tasks.AgentRunPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("unmarshal agent_run payload: %w", err)
	}
	if p.JobID == "" || p.UserID == "" || p.ProjectID == "" {
		return asynq.SkipRetry
	}

	// --- Resolve user, project, attachments ---
	user, err := c.DB.GetUserById(p.UserID)
	if err != nil {
		c.markAgentJobFailed(p.JobID, "user not found")
		return asynq.SkipRetry
	}
	project, err := c.DB.GetAppProjectByID(p.ProjectID)
	if err != nil {
		c.markAgentJobFailed(p.JobID, "project not found")
		return asynq.SkipRetry
	}
	turnAttachments, err := c.loadAgentTurnAttachments(ctx, project.ID, p.AttachmentIDs)
	if err != nil {
		c.markAgentJobFailed(p.JobID, "failed to load attachments: "+err.Error())
		return asynq.SkipRetry
	}

	// --- Cancel subscription (best-effort; if Redis is down we still run) ---
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	// stopCancel is closed after agent.Run returns so a late cancel signal
	// can't reach FinalizeJob and flip a completed run to "cancelled".
	// Closing an unread channel is a no-op, so it's safe to close
	// unconditionally even when no listener goroutine was started (i.e.
	// when SubscribeCancel failed).
	stopCancel := make(chan struct{})
	cancelSub, subErr := jobstream.SubscribeCancel(runCtx, c.RedisPubSub, p.JobID)
	if subErr != nil {
		slog.Warn("agent_run: cancel subscribe failed; cancellation via DELETE will not interrupt this run",
			"job_id", p.JobID, "error", subErr)
	} else {
		defer cancelSub.Close()
		go func() {
			ch := cancelSub.Channel()
			select {
			case msg, ok := <-ch:
				// ok=false means the subscription was closed by the deferred
				// Close at function exit. Treat as benign exit, NOT a cancel,
				// otherwise a successful run could be flipped to "cancelled"
				// by FinalizeJob's runCtx.Err() check.
				if !ok {
					return
				}
				_ = msg
				slog.Info("agent_run: cancel signal received", "job_id", p.JobID)
				cancel()
			case <-stopCancel:
				return
			case <-runCtx.Done():
				return
			}
		}()
	}

	// --- Persistence + stream_state hook for the emitter ---
	// Shared helpers from internal/jobstate — same logic as the API-side
	// handleStreamOrAsync, so reconnect-rehydration shape is identical.
	streamState := jobstate.NewStreamStateBuffer()
	stateFlush := time.NewTicker(500 * time.Millisecond)
	defer stateFlush.Stop()
	stateFlusher := jobstate.NewAsyncStateFlusher(
		streamState.Snapshot,
		func(raw json.RawMessage) {
			if err := c.DB.UpdateGenerationJobStreamState(p.JobID, raw); err != nil {
				slog.Debug("agent_run: stream_state flush failed", "job_id", p.JobID, "error", err)
			}
		},
	)
	defer stateFlusher.Wait()

	go func() {
		for {
			select {
			case <-stateFlush.C:
				stateFlusher.Trigger()
			case <-runCtx.Done():
				return
			}
		}
	}()

	var (
		allMilestoneEvents []models.JobEvent
		lastEvent          *responses.SSEEvent
	)

	onEvent := func(seq int64, ev responses.SSEEvent) {
		// Update last-event copy (used by FinalizeJob).
		evCopy := ev
		lastEvent = &evCopy
		streamState.Apply(ev)

		if IsMilestoneEvent(ev.Event) {
			je := models.JobEvent{
				Seq:   seq,
				Event: ev.Event,
				Data:  StripHeavyFields(ev.Event, ev.Data),
				Time:  time.Now().UnixMilli(),
			}
			allMilestoneEvents = append(allMilestoneEvents, je)
			if err := c.DB.AppendGenerationJobEvent(p.JobID, je); err != nil {
				slog.Error("agent_run: append job event failed",
					"job_id", p.JobID, "seq", seq, "event", ev.Event, "error", err)
			}
			stateFlusher.Trigger()
		}
	}

	emitter := newRedisEventEmitter(runCtx, c.RedisPubSub, p.JobID, onEvent)

	// --- Run the agent ---
	deps := c.AgentDeps(user)
	runErr := agent.Run(runCtx, deps, project, user, p.Message, turnAttachments, emitter)
	if runErr != nil {
		slog.Error("agent_run: agent.Run failed", "job_id", p.JobID, "project_id", project.ID, "error", runErr)
		// The agent runtime emits its own typed error event before returning;
		// we don't double-emit here. FinalizeJob picks up "error" from
		// lastEvent and flips the job row to failed.
	}

	// Disarm the cancel listener so a late DELETE /v1/jobs/{id} arriving
	// after agent.Run completed but before this function returns can't
	// flip a completed run to "cancelled" via runCtx.Err() in FinalizeJob.
	// Safe to close unconditionally — no goroutine was started when
	// SubscribeCancel failed, and closing an unread channel is a no-op.
	close(stopCancel)

	// --- Final consistency: re-write events array + clear stream_state ---
	stateFlusher.Wait()
	if len(allMilestoneEvents) > 0 {
		if err := c.DB.SetGenerationJobEvents(p.JobID, allMilestoneEvents); err != nil {
			slog.Error("agent_run: batch-write job events failed", "job_id", p.JobID, "error", err)
		}
	}
	if err := c.DB.UpdateGenerationJobStreamState(p.JobID, json.RawMessage("{}")); err != nil {
		slog.Debug("agent_run: clear stream_state failed", "job_id", p.JobID, "error", err)
	}

	// --- Finalise GenerationJob row ---
	c.FinalizeJob(p.JobID, runCtx, lastEvent)

	// --- Send terminal "end" frame so live SSE clients exit cleanly ---
	endEv := responses.SSEEvent{Seq: 0, Event: "end", Data: map[string]any{"job_id": p.JobID}}
	if err := jobstream.PublishEvent(ctx, c.RedisPubSub, p.JobID, endEv); err != nil {
		slog.Debug("agent_run: end frame publish failed", "job_id", p.JobID, "error", err)
	}

	// Return nil even on agent.Run failure — the failure was reported via
	// events + DB row. Returning the error would trigger Asynq retry, which
	// we don't want for non-idempotent agent runs.
	return nil
}

// markAgentJobFailed flips a generation_job to failed when a precondition
// (user/project lookup, attachment resolution) fails before agent.Run gets
// a chance to emit its own error event.
//
// The error is persisted as a milestone event so a later reconnect via
// StreamJob (terminal-job replay path) surfaces the failure reason
// instead of an empty events[] slice. Two pub/sub frames are then
// published in order — error, then end — so any subscribed SSE client
// receives the reason and exits its read loop. The DB row is the
// durable record; the publishes are best-effort.
func (c *Controller) markAgentJobFailed(jobID, reason string) {
	errEvent := models.JobEvent{
		Seq:   1, // Single milestone, fixed seq.
		Event: "error",
		Data: map[string]any{
			"job_id":  jobID,
			"phase":   "preflight",
			"message": reason,
		},
		Time: time.Now().UnixMilli(),
	}
	if err := c.DB.AppendGenerationJobEvent(jobID, errEvent); err != nil {
		slog.Error("agent_run: mark failed (append event)", "job_id", jobID, "reason", reason, "error", err)
	}

	if err := c.DB.UpdateGenerationJobStatus(jobID, "failed", reason); err != nil {
		slog.Error("agent_run: mark failed (status)", "job_id", jobID, "reason", reason, "error", err)
	}

	// Publish the error frame AND the terminal end frame so any subscribed
	// SSE client receives the reason then exits its read loop. Both are
	// best-effort — persisted state in the DB is the durable record.
	publishCtx := context.Background()
	errFrame := responses.SSEEvent{Seq: 1, Event: "error", Data: errEvent.Data}
	_ = jobstream.PublishEvent(publishCtx, c.RedisPubSub, jobID, errFrame)
	endFrame := responses.SSEEvent{Seq: 0, Event: "end", Data: map[string]any{"job_id": jobID}}
	_ = jobstream.PublishEvent(publishCtx, c.RedisPubSub, jobID, endFrame)
}

// loadAgentTurnAttachments mirrors views.loadTurnAttachments, but lives in
// controllers because the worker can't import views (cycle). Logic matches
// the view-side helper one-for-one — same ownership check, same image cap,
// same pool resolution. The view-side helper takes []uuid.UUID; here we
// take []string to match the AgentRunPayload shape.
func (c *Controller) loadAgentTurnAttachments(
	ctx context.Context,
	projectID uuid.UUID,
	attachmentIDs []string,
) (agent.TurnAttachments, error) {
	var current []models.AppAsset
	if len(attachmentIDs) > 0 {
		fetched, err := c.DB.GetAppAssetsByIDs(attachmentIDs)
		if err != nil {
			return agent.TurnAttachments{}, fmt.Errorf("failed to load attachments: %w", err)
		}
		byID := make(map[string]models.AppAsset, len(fetched))
		for _, a := range fetched {
			if a.AppProjectID != projectID {
				return agent.TurnAttachments{}, errors.New("one or more attachments are invalid")
			}
			byID[a.ID.String()] = a
		}
		for _, id := range attachmentIDs {
			if _, ok := byID[id]; !ok {
				return agent.TurnAttachments{}, errors.New("one or more attachments are invalid")
			}
		}
		current = make([]models.AppAsset, 0, len(attachmentIDs))
		for _, id := range attachmentIDs {
			current = append(current, byID[id])
		}
	}
	imgCount := 0
	for _, a := range current {
		if constants.ClassifyAttachment(a.ContentType) == constants.AttachmentKindImage {
			imgCount++
		}
	}
	if imgCount > constants.MaxImageAttachmentsPerTurn {
		return agent.TurnAttachments{}, fmt.Errorf("too many image attachments (max %d per message)", constants.MaxImageAttachmentsPerTurn)
	}
	pool, err := c.DB.GetRecentAppAssets(projectID, constants.ManifestMaxAssets)
	if err != nil {
		return agent.TurnAttachments{}, fmt.Errorf("failed to load asset pool: %w", err)
	}
	return agent.TurnAttachments{Current: current, Pool: pool}, nil
}
