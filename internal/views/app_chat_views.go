package views

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
	"github.com/posthog/posthog-go"
)

// DELETE /v2/apps/{appId}/conversation
func (v *HttpViews) ClearAppConversation(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	_, err = v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	if err := v.Controller.ClearAppConversation(appID); err != nil {
		utils.JSONErrorResponse(w, "failed to clear conversation", http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, map[string]any{"success": true}, http.StatusOK)
}

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

	v.Controller.TrackUserEvent(user, "app_chat_sent", posthog.NewProperties().
		Set("app_id", project.ID.String()).
		Set("has_images", len(req.Images) > 0))

	v.handleStreamOrAsync(w, r, user, StreamConfig{
		JobType: "app_chat",
		AppID:   project.ID,
		StreamFunc: func(ctx context.Context, events chan<- responses.SSEEvent, jobID string) {
			v.Controller.StreamBuildApp(ctx, project, user, req.Message, req.Images, req.ReasoningEffort, false, "", events)
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
