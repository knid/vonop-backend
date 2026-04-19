package views

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/agent"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
	"github.com/posthog/posthog-go"
)

// POST /v2/apps/{appId}/chat
func (v *HttpViews) ChatApp(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if user == nil {
		utils.JSONErrorResponse(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if requireActiveSubscription(w, user) {
		return
	}
	if v.checkMinimumCredits(w, user) {
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}
	if project == nil {
		utils.JSONErrorResponse(w, "project not found", http.StatusNotFound)
		return
	}

	var req requests.AppChatRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	if r.Context().Err() != nil {
		return
	}

	// Draft projects have never been generated — the first chat message is the
	// initial generation. This lets the studio handle both the first prompt and
	// subsequent edits through the same endpoint, so the UI can stay consistent
	// all the way through instead of running a separate bootstrap flow.
	isInitialGeneration := project.Status == "draft"

	jobType := "app_chat"
	eventName := "app_chat_sent"
	if isInitialGeneration {
		jobType = "generate_app"
		eventName = "app_generation_started"
	}

	v.Controller.TrackUserEvent(user, eventName, posthog.NewProperties().
		Set("app_id", project.ID.String()).
		Set("project_type", project.ProjectType).
		Set("has_images", len(req.Images) > 0))

	brevoEvent := constants.BREVO_EVENT_APP_CHAT_SENT
	if isInitialGeneration {
		brevoEvent = constants.BREVO_EVENT_APP_GENERATION_STARTED
	}
	go v.Controller.CRMService.SendEvent(user, brevoEvent, map[string]any{
		"app_id":       project.ID.String(),
		"project_type": project.ProjectType,
	})

	v.handleStreamOrAsync(w, r, user, StreamConfig{
		JobType: jobType,
		AppID:   project.ID,
		StreamFunc: func(ctx context.Context, events chan<- responses.SSEEvent, jobID string) {
			defer close(events)
			deps := v.Controller.AgentDeps(user)
			emitter := newAgentSSEAdapter(events)
			if err := agent.Run(ctx, deps, project, user, req.Message, agent.TurnAttachments{}, emitter); err != nil {
				slog.Error("agent.Run failed", "error", err, "project_id", project.ID)
			}
			emitter.Close()
		},
	})
}

// GET /v2/apps/{appId}/active-job
// Returns the currently running or awaiting_input generation job for this app,
// or 200 with {"job_id": null} if none. The frontend calls this on mount to
// reconnect the SSE stream after a refresh or browser restart — replacing the
// old localStorage-based job-id recovery, which got stuck on stale jobIds and
// didn't survive Safari's storage eviction on mobile.
func (v *HttpViews) GetAppActiveJob(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if user == nil {
		utils.JSONErrorResponse(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil || project == nil {
		utils.JSONErrorResponse(w, "project not found", http.StatusNotFound)
		return
	}

	job, err := v.Controller.GetAppActiveJob(project.ID)
	if err != nil || job == nil {
		utils.JSONResponse(w, map[string]any{"job_id": nil}, http.StatusOK)
		return
	}
	utils.JSONResponse(w, map[string]any{
		"job_id": job.ID,
		"status": job.Status,
		"type":   job.Type,
	}, http.StatusOK)
}
