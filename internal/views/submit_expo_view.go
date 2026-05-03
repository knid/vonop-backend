package views

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	"github.com/posthog/posthog-go"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
)

// SubmitAppExpo triggers an App Store / Play Store submission for the latest
// successful Expo build of the given platform.
//
// Mirrors BuildAppExpo's auth + permission flow.
//
// Maps controller sentinel errors to specific HTTP statuses:
//
//	ErrSubmitWrongProjectType  → 400
//	ErrSubmitInvalidPlatform   → 400
//	ErrSubmitInvalidTrack      → 400
//	ErrSubmitNoBuildToSubmit   → 422
//	ErrSubmitMissingCredential → 422
func (v *HttpViews) SubmitAppExpo(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	if requireActiveSubscription(w, v.Controller, user) {
		return
	}

	limits, limitsErr := v.Controller.GetUserLimits(user)
	if limitsErr != nil || !limits.CanDeployMobile {
		go v.Controller.EventService.SendEvent(user, constants.EVENT_SUBSCRIPTION_NEED, map[string]any{"action": "submit_expo"})
		utils.JSONError(w, constants.ErrFeatureNotAvailable, "mobile submissions require Professional plan", http.StatusForbidden, nil)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}
	if project.ProjectType != models.ProjectTypeMobileAppNative {
		utils.JSONError(w, constants.ErrValidationBody,
			"this endpoint only accepts mobile_app_native projects",
			http.StatusBadRequest, nil)
		return
	}

	platform := chi.URLParam(r, "platform")

	var req requests.SubmitExpoRequest
	// Body is optional — submitter can call with empty body to use all defaults.
	if r.ContentLength > 0 {
		if err := utils.ParseJSONBody(r, &req); err != nil {
			utils.ValidationError(w, err)
			return
		}
		validate := validator.New(validator.WithRequiredStructEnabled())
		if err := req.Validate(validate); err != nil {
			utils.ValidationError(w, err)
			return
		}
	}

	result, err := v.Controller.SubmitExpoMobile(r.Context(), project, user, controllers.SubmitExpoOptions{
		Platform:              platform,
		TrackOverride:         req.Track,
		PhasedReleaseOverride: req.PhasedRelease,
		SubmitAsDraftOverride: req.SubmitAsDraft,
		RolloutFraction:       req.RolloutFraction,
		SubmitToAppStore:      req.SubmitToAppStore,
	})
	if err != nil {
		switch {
		case errors.Is(err, controllers.ErrSubmitWrongProjectType),
			errors.Is(err, controllers.ErrSubmitInvalidPlatform),
			errors.Is(err, controllers.ErrSubmitInvalidTrack):
			utils.JSONError(w, constants.ErrValidationBody, err.Error(), http.StatusBadRequest, nil)
		case errors.Is(err, controllers.ErrSubmitNoBuildToSubmit),
			errors.Is(err, controllers.ErrSubmitMissingCredential):
			utils.JSONError(w, constants.ErrValidationBody, err.Error(), http.StatusUnprocessableEntity, nil)
		default:
			utils.JSONInternalError(w, err)
		}
		return
	}

	v.Controller.TrackUserEvent(user, "app_submitted_"+result.Platform, posthog.NewProperties().
		Set("app_id", project.ID.String()).
		Set("codemagic_build_id", result.CodemagicBuildID))

	utils.JSONResponse(w, map[string]any{
		"platform":           result.Platform,
		"build_task_id":      result.BuildTaskID,
		"codemagic_build_id": result.CodemagicBuildID,
		"status":             "queued",
	}, http.StatusAccepted)
}
