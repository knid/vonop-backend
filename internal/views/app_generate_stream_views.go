package views

import (
	"net/http"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
)

// POST /v2/apps/generate/stream
func (v *HttpViews) GenerateAppStream(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	var req requests.GenerateAppRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.ValidationError(w, err)
		return
	}

	// Resolve the target org before subscription/credit checks so gates
	// are evaluated against the org where the app will actually live.
	targetOrgID, err := v.Controller.RequireOrgIDForUser(user, req.OrganizationID)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrForbidden, http.StatusForbidden, nil)
		return
	}

	if requireActiveSubscription(w, v.Controller, user, targetOrgID) {
		return
	}
	if v.checkMinimumCredits(w, user, targetOrgID) {
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

	v.Controller.TrackUserEvent(user, constants.EVENT_APP_GENERATION_STARTED,
		v.Controller.AppEventProps(project.ID.String()).
			Set("project_type", req.ProjectType).
			Set("prompt_preview", utils.TruncatePromptPreview(req.Prompt, 50)))

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
