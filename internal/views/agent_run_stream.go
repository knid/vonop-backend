package views

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/services/jobstream"
	"github.com/knid/wemob/internal/utils"
	"github.com/redis/go-redis/v9"
)

// AgentRunRequest carries the inputs needed to start a Deep Agents run. The
// handler (chat or generate) builds this and passes it to
// enqueueAgentRunAndStream.
type AgentRunRequest struct {
	JobType       string // "generate_app" or "app_chat"
	AppID         uuid.UUID
	UserID        uuid.UUID
	Message       string
	AttachmentIDs []uuid.UUID
}

// enqueueAgentRunAndStream creates the GenerationJob row and starts a native
// Deep Agents run. It returns runtime connection metadata for the frontend to
// use with the LangChain/Deep Agents stream client directly.
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

	project, err := v.Controller.DB.GetAppProjectByID(req.AppID.String())
	if err != nil {
		_ = v.Controller.DB.UpdateGenerationJobStatus(job.ID.String(), "failed", "project lookup: "+err.Error())
		utils.JSONInternalError(w, err)
		return
	}

	launch, err := v.Controller.StartDeepAgentsRunForJob(r.Context(), user, project, job, req.Message, req.AttachmentIDs)
	if err != nil {
		_ = v.Controller.DB.UpdateGenerationJobStatus(job.ID.String(), "failed", err.Error())
		utils.JSONInternalError(w, err)
		return
	}
	utils.JSONResponse(w, responses.JobCreatedResponse{
		JobID:   job.ID,
		Status:  job.Status,
		Runtime: launch.Access,
	}, http.StatusAccepted)
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
