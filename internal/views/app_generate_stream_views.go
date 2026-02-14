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

	// Pre-step: create project synchronously
	project, err := v.Controller.CreateAppProject(user, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.ProjectType == "ecommerce" {
		v.Controller.CreateStore(project, user, project.Name+" Store")
	}

	v.Controller.TrackEvent(user.ID, "app_generated", posthog.NewProperties().
		Set("project_type", req.ProjectType).
		Set("app_id", project.ID.String()))

	v.handleStreamOrAsync(w, r, user, StreamConfig{
		JobType: "generate_app",
		StreamFunc: func(ctx context.Context, events chan<- responses.SSEEvent) {
			v.Controller.StreamGenerateAppFiles(ctx, project, user, req.Prompt, req.ProjectType, events)
		},
	})
}

// POST /v2/apps/generate/ecommerce/stream
func (v *HttpViews) GenerateEcommerceAppStream(w http.ResponseWriter, r *http.Request) {
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
	req.ProjectType = "ecommerce"

	// Pre-step: create project + store synchronously
	project, err := v.Controller.CreateAppProject(user, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	v.Controller.CreateStore(project, user, project.Name+" Store")

	v.Controller.TrackEvent(user.ID, "app_generated_ecommerce", posthog.NewProperties().
		Set("app_id", project.ID.String()))

	v.handleStreamOrAsync(w, r, user, StreamConfig{
		JobType: "generate_ecommerce_app",
		StreamFunc: func(ctx context.Context, events chan<- responses.SSEEvent) {
			v.Controller.StreamGenerateAppFiles(ctx, project, user, req.Prompt, "ecommerce", events)
		},
	})
}
