package views

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/knid/wemob/internal/agent"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
	"github.com/posthog/posthog-go"
)

// POST /v2/apps/generate/stream
func (v *HttpViews) GenerateAppStream(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
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
		utils.ValidationError(w, err)
		return
	}

	// Check for active generation jobs BEFORE creating a new project to prevent
	// orphan projects from spam-clicks (each click was creating a new DB record).
	activeJobs, _ := v.Controller.GetActiveJobs(user.ID)
	for _, j := range activeJobs {
		if j.Type == "generate_app" {
			utils.JSONErrorCode(w, constants.ErrGenerationInProgress, http.StatusTooManyRequests, nil)
			return
		}
	}

	// Pre-step: create project synchronously
	project, err := v.Controller.CreateAppProject(r.Context(), user, req)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	// Store creation is handled by agent.Run's pre-generation setup
	// (with existence check), so no need to create it here.

	v.Controller.TrackUserEvent(user, "app_generation_started", posthog.NewProperties().
		Set("project_type", req.ProjectType).
		Set("app_id", project.ID.String()))

	go v.Controller.CRMService.SendEvent(user, constants.EVENT_APP_GENERATION_STARTED, map[string]any{
		"app_id":       project.ID.String(),
		"project_type": req.ProjectType,
	})

	v.handleStreamOrAsync(w, r, user, StreamConfig{
		JobType: "generate_app",
		AppID:   project.ID,
		StreamFunc: func(ctx context.Context, events chan<- responses.SSEEvent, jobID string) {
			defer close(events)
			deps := v.Controller.AgentDeps(user)
			emitter := newAgentSSEAdapter(events)
			if err := agent.Run(ctx, deps, project, user, req.Prompt, agent.TurnAttachments{}, emitter); err != nil {
				slog.Error("agent.Run failed (generate)", "error", err, "project_id", project.ID)
			}
			emitter.Close()
		},
	})
}
