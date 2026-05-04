// Package views — Codemagic OTA bundle ingest + Quick-publish trigger.
//
// P1 #12 (mobile audit, 2026-05-04): JS-only changes don't need the 25-min
// native rebuild. The user-facing "Quick publish" button calls
// POST /v2/apps/{appId}/ota/quick-publish, which dispatches the
// expo-ota-publish Codemagic workflow. ~3 min vs ~25 min.
//
// Bundle ingest (Codemagic → backend) reuses the existing OTAPublishHandler
// at /v2/apps/{appId}/ota/publish — same HMAC scheme (purpose=ota-publish,
// scope=app_id), same multipart shape. The new workflow's tail step posts
// twice (once per platform) to that endpoint.
package views

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/posthog/posthog-go"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/utils"
)

// QuickPublishOTA — POST /v2/apps/{appId}/ota/quick-publish.
//
// Dispatches the Codemagic expo-ota-publish workflow for a mobile_app_native
// project. Returns the AppBuildTask the frontend's existing build-status
// polling can pick up.
func (v *HttpViews) QuickPublishOTA(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	if requireActiveSubscription(w, v.Controller, user) {
		return
	}

	// Plan-gate on the mobile-deploy flag — same gate the full native build
	// path uses for /v2/apps/{appId}/deploy/mobile.
	limits, _, limitsErr := v.Controller.GetUserLimitsWithPlan(user)
	if limitsErr == nil && !limits.CanDeployMobile {
		go v.Controller.EventService.SendEvent(user, constants.EVENT_SUBSCRIPTION_NEED, map[string]any{"action": "ota_quick_publish"})
		utils.JSONError(w, constants.ErrFeatureNotAvailable,
			"mobile OTA publishing is not available on your plan",
			http.StatusForbidden, nil)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}
	if project.ProjectType != models.ProjectTypeMobileAppNative {
		utils.JSONError(w, "wrong_project_type",
			"OTA quick publish requires a mobile_app_native project",
			http.StatusConflict, nil)
		return
	}

	build, task, err := v.Controller.TriggerCodemagicOTAPublish(r.Context(), project, user)
	if err != nil {
		switch {
		case errors.Is(err, controllers.ErrQuickPublishWrongType):
			utils.JSONError(w, "wrong_project_type", err.Error(), http.StatusConflict, nil)
		default:
			utils.JSONInternalError(w, err)
		}
		return
	}

	v.Controller.TrackUserEvent(user, "app_ota_quick_published", posthog.NewProperties().
		Set("app_id", project.ID.String()).
		Set("build_id", build.ID.String()).
		Set("task_id", task.ID.String()))

	utils.JSONResponse(w, map[string]any{
		"build_id":     build.ID.String(),
		"task_id":      task.ID.String(),
		"build_number": build.BuildNumber,
		"version":      build.VersionString,
		"status":       task.StatusText,
	}, http.StatusAccepted)
}
