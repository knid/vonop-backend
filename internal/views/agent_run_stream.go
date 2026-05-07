package views

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/services/jobstream"
	"github.com/knid/wemob/internal/tasks"
	"github.com/knid/wemob/internal/utils"
	"github.com/redis/go-redis/v9"
)

// AgentRunRequest carries the inputs needed to enqueue a worker-side
// agent.Run. The handler (chat or generate) builds this and passes it to
// enqueueAgentRunAndStream.
type AgentRunRequest struct {
	JobType       string // "generate_app" or "app_chat"
	AppID         uuid.UUID
	UserID        uuid.UUID
	Message       string
	AttachmentIDs []uuid.UUID
}

// enqueueAgentRunAndStream creates the GenerationJob row, enqueues an
// agent:run task, and either streams events via SSE (default) or returns
// 202 immediately (when ?background=true is set). Replaces the agent-run
// path through handleStreamOrAsync — dashboards still go through the old
// helper because they don't use agent.Run.
func (v *HttpViews) enqueueAgentRunAndStream(
	w http.ResponseWriter,
	r *http.Request,
	user *models.User,
	req AgentRunRequest,
) {
	job, err := v.Controller.CreateJob(req.UserID, req.JobType, req.AppID)
	if err != nil {
		switch {
		case errors.Is(err, controllers.ErrGenerationAlreadyInProgress):
			utils.JSONErrorCode(w, constants.ErrGenerationInProgress, http.StatusTooManyRequests, nil)
		case errors.Is(err, controllers.ErrConcurrentJobLimit):
			utils.JSONErrorCode(w, constants.ErrRateLimited, http.StatusTooManyRequests, nil)
		default:
			utils.JSONInternalError(w, err)
		}
		return
	}

	// Marshal the payload (cheap; do before subscribe so we can fail fast).
	attachmentIDs := make([]string, 0, len(req.AttachmentIDs))
	for _, id := range req.AttachmentIDs {
		attachmentIDs = append(attachmentIDs, id.String())
	}
	payload, err := json.Marshal(tasks.AgentRunPayload{
		JobID:         job.ID.String(),
		UserID:        req.UserID.String(),
		ProjectID:     req.AppID.String(),
		Message:       req.Message,
		AttachmentIDs: attachmentIDs,
	})
	if err != nil {
		_ = v.Controller.DB.UpdateGenerationJobStatus(job.ID.String(), "failed", "marshal payload: "+err.Error())
		utils.JSONInternalError(w, err)
		return
	}

	// Background mode: enqueue + return 202 immediately. No subscription
	// needed — the client polls /v1/jobs/{id} for milestones.
	if r.URL.Query().Get("background") == "true" {
		task := asynq.NewTask(tasks.TypeAgentRun, payload)
		if _, err := v.Controller.TaskService.Enqueue(task,
			asynq.Queue("agent"),
			asynq.MaxRetry(0), // Non-idempotent — partial unique index on app_agent_runs
			asynq.Timeout(50*time.Minute),
			asynq.TaskID(job.ID.String()),
		); err != nil {
			_ = v.Controller.DB.UpdateGenerationJobStatus(job.ID.String(), "failed", "enqueue: "+err.Error())
			utils.JSONInternalError(w, err)
			return
		}
		utils.JSONResponse(w, responses.JobCreatedResponse{
			JobID:  job.ID,
			Status: job.Status,
		}, http.StatusAccepted)
		return
	}

	// SSE mode: SUBSCRIBE FIRST so we don't miss any events the worker emits
	// between enqueue and the start of our read loop. Subscription
	// confirmation (sub.Receive inside SubscribeEvents) guarantees that by
	// the time we return from that call, any subsequent Publish on the
	// channel will be delivered to our subscription.
	flusher, ok := w.(http.Flusher)
	if !ok {
		// We haven't enqueued yet — can still report the failure cleanly.
		_ = v.Controller.DB.UpdateGenerationJobStatus(job.ID.String(), "failed", "client does not support streaming")
		utils.JSONErrorCode(w, constants.ErrStreamingUnsupported, http.StatusInternalServerError, nil)
		return
	}
	utils.SetSSEHeaders(w)

	subCtx, subCancel := context.WithCancel(r.Context())
	defer subCancel()
	sub, err := jobstream.SubscribeEvents(subCtx, v.Controller.RedisPubSub, job.ID.String())
	if err != nil {
		_ = v.Controller.DB.UpdateGenerationJobStatus(job.ID.String(), "failed", "subscribe: "+err.Error())
		slog.Error("agent_run_stream: subscribe before enqueue failed", "job_id", job.ID, "error", err)
		// SSE headers already sent — write a clean terminal pair so the
		// client exits its read loop instead of hanging on an empty stream.
		_ = utils.WriteSSEEvent(w, flusher, 0, "error", map[string]any{
			"job_id":  job.ID,
			"phase":   "subscribe",
			"message": "failed to subscribe to job events",
		})
		_ = utils.WriteSSEEvent(w, flusher, 0, "end", map[string]any{"job_id": job.ID})
		return
	}
	defer sub.Close()

	// NOW enqueue — any event the worker emits will land on our active
	// subscription.
	task := asynq.NewTask(tasks.TypeAgentRun, payload)
	if _, err := v.Controller.TaskService.Enqueue(task,
		asynq.Queue("agent"),
		asynq.MaxRetry(0), // Non-idempotent — partial unique index on app_agent_runs
		// Cap matches agent.defaultMaxWallclockMs (45 min) plus headroom for
		// finalisation. If a worker fails to drain in this window the task
		// timer fires; the row is still flipped to failed by the reaper.
		asynq.Timeout(50*time.Minute),
		asynq.TaskID(job.ID.String()),
	); err != nil {
		_ = v.Controller.DB.UpdateGenerationJobStatus(job.ID.String(), "failed", "enqueue: "+err.Error())
		utils.JSONInternalError(w, err)
		return
	}

	_ = utils.WriteSSEEvent(w, flusher, 0, "job_started", map[string]any{"job_id": job.ID})

	v.streamFromActiveSubscription(w, r, flusher, job.ID.String(), 0, sub)
}

// streamFromJobChannel subscribes to jobstream.EventsChannel(jobID) and
// pumps events to the SSE response. It is a convenience wrapper that
// subscribes then delegates to streamFromActiveSubscription. Use it when
// the caller has not yet subscribed; if you've already subscribed (e.g.
// to close a race window), call streamFromActiveSubscription directly.
//
// `since` is the resume cursor (Last-Event-ID); events with seq <= since
// are dropped (already replayed by the caller).
func (v *HttpViews) streamFromJobChannel(
	w http.ResponseWriter,
	r *http.Request,
	flusher http.Flusher,
	jobID string,
	since int64,
) {
	subCtx, subCancel := context.WithCancel(r.Context())
	defer subCancel()

	sub, err := jobstream.SubscribeEvents(subCtx, v.Controller.RedisPubSub, jobID)
	if err != nil {
		slog.Error("agent_run_stream: subscribe failed", "job_id", jobID, "error", err)
		return
	}
	defer sub.Close()

	v.streamFromActiveSubscription(w, r, flusher, jobID, since, sub)
}

// streamFromActiveSubscription reads from an already-confirmed Redis
// pub/sub subscription and pumps decoded events to the SSE response
// until the worker publishes the terminal "end" frame, the client
// disconnects, or the keepalive write fails. The caller owns sub's
// lifecycle (Close on return).
//
// `since` is the resume cursor; events with seq <= since are dropped.
func (v *HttpViews) streamFromActiveSubscription(
	w http.ResponseWriter,
	r *http.Request,
	flusher http.Flusher,
	jobID string,
	since int64,
	sub *redis.PubSub,
) {
	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()

	ch := sub.Channel()
	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return
			}
			ev, err := jobstream.DecodeEvent([]byte(msg.Payload))
			if err != nil {
				slog.Warn("agent_run_stream: decode event", "job_id", jobID, "error", err)
				continue
			}
			if ev.Seq > 0 && ev.Seq <= since {
				continue
			}
			if err := utils.WriteSSEEvent(w, flusher, ev.Seq, ev.Event, ev.Data); err != nil {
				return
			}
			if ev.Event == "end" {
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
