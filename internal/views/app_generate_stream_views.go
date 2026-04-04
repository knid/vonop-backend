package views

import (
	"context"
	"net/http"

	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
	"github.com/posthog/posthog-go"
)

// POST /v2/apps/generate/stream
func (v *HttpViews) GenerateAppStream(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if requireActiveSubscription(w, user) {
		return
	}
	if v.checkMinimumCredits(w, user) {
		return
	}

	var req requests.GenerateAppRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Check for active generation jobs BEFORE creating a new project to prevent
	// orphan projects from spam-clicks (each click was creating a new DB record).
	activeJobs, _ := v.Controller.GetActiveJobs(user.ID)
	for _, j := range activeJobs {
		if j.Type == "generate_app" {
			utils.JSONErrorResponse(w, "a generation is already in progress", http.StatusTooManyRequests)
			return
		}
	}

	// Pre-step: create project synchronously
	project, err := v.Controller.CreateAppProject(r.Context(), user, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Store creation is handled by runPreGenerationSetup inside StreamBuildApp
	// (with existence check), so no need to create it here.

	v.Controller.TrackUserEvent(user, "app_generation_started", posthog.NewProperties().
		Set("project_type", req.ProjectType).
		Set("app_id", project.ID.String()))

	v.handleStreamOrAsync(w, r, user, StreamConfig{
		JobType: "generate_app",
		StreamFunc: func(ctx context.Context, events chan<- responses.SSEEvent, jobID string) {
			v.Controller.StreamBuildApp(ctx, project, user, req.Prompt, req.Images, "", true, jobID, events)
		},
	})
}
