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

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	v.Controller.RunningJobs.Store(job.ID.String(), cancel)

	events := make(chan responses.SSEEvent, 100)
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

	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case event, ok := <-events:
			if !ok {
				// Channel closed — generation finished
				goto done
			}
			lastEvent = &event

			// Always persist milestone events to DB
			if controllers.IsMilestoneEvent(event.Event) {
				jobEvent := models.JobEvent{
					Index: eventIndex,
					Event: event.Event,
					Data:  event.Data,
					Time:  time.Now().UnixMilli(),
				}
				if err := v.Controller.DB.AppendGenerationJobEvent(job.ID.String(), jobEvent); err != nil {
					slog.Error("failed to persist job event", "job_id", job.ID, "error", err)
				}
				eventIndex++
			}

			// Write to SSE if client is still connected
			if !clientGone {
				if err := utils.WriteSSEEvent(w, flusher, event.Event, event.Data); err != nil {
					clientGone = true
					slog.Info("SSE client disconnected, continuing job in background", "job_id", job.ID)
				}
			}

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
		}
	}
done:

	// StreamFunc finished (channel closed) — finalize job
	cancel()
	v.Controller.FinalizeJob(job.ID.String(), ctx, lastEvent)
}

// drainEventsToJob reads all events from the channel, persists milestone events,
// and finalizes the job when done. Used for pure background mode.
func (v *HttpViews) drainEventsToJob(ctx context.Context, cancel context.CancelFunc, job *models.GenerationJob, events <-chan responses.SSEEvent) {
	defer cancel()

	eventIndex := 0
	var lastEvent *responses.SSEEvent

	for event := range events {
		lastEvent = &event

		if controllers.IsMilestoneEvent(event.Event) {
			jobEvent := models.JobEvent{
				Index: eventIndex,
				Event: event.Event,
				Data:  event.Data,
				Time:  time.Now().UnixMilli(),
			}
			if err := v.Controller.DB.AppendGenerationJobEvent(job.ID.String(), jobEvent); err != nil {
				slog.Error("failed to persist job event", "job_id", job.ID, "error", err)
			}
			eventIndex++
		}
	}

	v.Controller.FinalizeJob(job.ID.String(), ctx, lastEvent)
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
		ID:        job.ID,
		Type:      job.Type,
		Status:    job.Status,
		Events:    eventsToReturn,
		Result:    job.Result,
		Error:     job.Error,
		CreatedAt: job.CreatedAt,
		UpdatedAt: job.UpdatedAt,
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
			ID:        job.ID,
			Type:      job.Type,
			Status:    job.Status,
			Events:    job.Events,
			Result:    job.Result,
			Error:     job.Error,
			CreatedAt: job.CreatedAt,
			UpdatedAt: job.UpdatedAt,
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
