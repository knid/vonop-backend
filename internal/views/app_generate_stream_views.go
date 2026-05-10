package views

import (
	"net/http"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/requests"
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
	if requireActiveSubscription(w, v.Controller, user) {
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

	// Pre-build dedup: see CreateJob's per-app generate_app block, which is
	// authoritative. This kept-around check rejects the request before
	// CreateAppProject runs, so a spam-clicked Create button doesn't churn
	// orphan AppProject rows.
	activeJobs, _ := v.Controller.GetActiveJobs(user.ID)
	for _, j := range activeJobs {
		if j.Type == "generate_app" {
			utils.JSONErrorCode(w, constants.ErrGenerationInProgress, http.StatusTooManyRequests, nil)
			return
		}
	}

	project, err := v.Controller.CreateAppProject(r.Context(), user, req)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	v.Controller.TrackUserEvent(user, constants.EVENT_APP_GENERATION_STARTED, posthog.NewProperties().
		Set("project_type", req.ProjectType).
		Set("app_id", project.ID.String()))

	go v.Controller.EventService.SendEvent(user, constants.EVENT_APP_GENERATION_STARTED, map[string]any{
		"app_id":       project.ID.String(),
		"project_type": req.ProjectType,
	})

	v.enqueueAgentRunAndStream(w, r, user, AgentRunRequest{
		JobType: "generate_app",
		AppID:   project.ID,
		UserID:  user.ID,
		Message: req.Prompt,
		// Initial generation has no per-turn attachments.
	})
}
