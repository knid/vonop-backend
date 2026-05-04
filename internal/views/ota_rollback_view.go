// Package views — OTA rollback + rollout-promote endpoints.
//
// P2#22 (mobile audit, 2026-05-04): the manifest endpoint now filters by
// (channel, is_active=true). These two endpoints let project owners (a)
// rewind the active pointer to an older publish ("rollback") and (b) bump
// rollout_percentage on a partial-rollout publish ("promote").
//
// Both endpoints are project-owner-authenticated. The HMAC-protected
// /v2/apps/{appId}/ota/publish endpoint stays Codemagic-only.
package views

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/posthog/posthog-go"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/utils"
)

// otaRollbackRequest — body of POST /v2/apps/{appId}/ota/rollback.
type otaRollbackRequest struct {
	Channel        string `json:"channel"`
	TargetUpdateID string `json:"target_update_id"`
}

// otaPromoteRequest — body of POST /v2/apps/{appId}/ota/promote.
type otaPromoteRequest struct {
	UpdateID          string `json:"update_id"`
	RolloutPercentage int16  `json:"rollout_percentage"`
}

// RollbackOTA — POST /v2/apps/{appId}/ota/rollback.
//
// Body: {channel, target_update_id}.
// Auth: project owner.
//
// Effect: marks every OTAUpdate row in (appId, channel, target.platform)
// created strictly after target.CreatedAt as is_active=false. Re-activates
// target if it had been deactivated by a previous rollback. The platform
// is read off the target row, not the body — operators identify ONE row
// to re-promote (per platform) and the server figures out which lane it
// lives in.
func (v *HttpViews) RollbackOTA(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	appID := chi.URLParam(r, "appId")
	appProjectID, err := uuid.Parse(appID)
	if err != nil {
		utils.JSONError(w, "bad_request", "invalid app id", http.StatusBadRequest, nil)
		return
	}
	// Resolves AND verifies project ownership; ErrNotFound covers both
	// missing-row and not-yours so we don't leak existence.
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil || project == nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	var body otaRollbackRequest
	if err := utils.ParseJSONBody(r, &body); err != nil {
		utils.JSONError(w, "bad_request", "invalid json body", http.StatusBadRequest, nil)
		return
	}
	if body.TargetUpdateID == "" {
		utils.JSONError(w, "bad_request", "target_update_id is required", http.StatusBadRequest, nil)
		return
	}
	targetID, err := uuid.Parse(body.TargetUpdateID)
	if err != nil {
		utils.JSONError(w, "bad_request", "invalid target_update_id", http.StatusBadRequest, nil)
		return
	}
	channel := body.Channel
	if channel == "" {
		channel = "production"
	}

	row, err := v.Controller.RollbackOTAUpdate(r.Context(), appProjectID, channel, targetID)
	if err != nil {
		switch {
		case errors.Is(err, controllers.ErrOTANotFound),
			errors.Is(err, controllers.ErrOTAProjectMismatch):
			utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		case errors.Is(err, controllers.ErrOTAChannelMismatch):
			utils.JSONError(w, "channel_mismatch", err.Error(), http.StatusConflict, nil)
		default:
			slog.Error("ota rollback failed", "app_project_id", appProjectID, "target", targetID, "err", err)
			utils.JSONInternalError(w, err)
		}
		return
	}

	v.Controller.TrackUserEvent(user, "app_ota_rolled_back", posthog.NewProperties().
		Set("app_id", appProjectID.String()).
		Set("channel", row.Channel).
		Set("platform", row.Platform).
		Set("target_update_id", row.UpdateID.String()))

	utils.JSONResponse(w, map[string]any{
		"update_id":          row.UpdateID.String(),
		"channel":            row.Channel,
		"platform":           row.Platform,
		"runtime_version":    row.RuntimeVersion,
		"rollout_percentage": row.RolloutPercentage,
		"is_active":          row.IsActive,
	}, http.StatusOK)
}

// PromoteOTA — POST /v2/apps/{appId}/ota/promote.
//
// Body: {update_id, rollout_percentage}.
// Auth: project owner.
//
// Effect: bumps rollout_percentage on the row identified by update_id.
// Used to gradually open up a partial rollout: 10% → 25% → 50% → 100%.
// Lower percentages are also accepted (e.g. 100% → 25%) for emergency
// throttling, but the proper "stop serving this" path is rollback, not
// promote-to-zero.
func (v *HttpViews) PromoteOTA(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	appID := chi.URLParam(r, "appId")
	appProjectID, err := uuid.Parse(appID)
	if err != nil {
		utils.JSONError(w, "bad_request", "invalid app id", http.StatusBadRequest, nil)
		return
	}
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil || project == nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	var body otaPromoteRequest
	if err := utils.ParseJSONBody(r, &body); err != nil {
		utils.JSONError(w, "bad_request", "invalid json body", http.StatusBadRequest, nil)
		return
	}
	if body.UpdateID == "" {
		utils.JSONError(w, "bad_request", "update_id is required", http.StatusBadRequest, nil)
		return
	}
	updateID, err := uuid.Parse(body.UpdateID)
	if err != nil {
		utils.JSONError(w, "bad_request", "invalid update_id", http.StatusBadRequest, nil)
		return
	}

	row, err := v.Controller.PromoteRollout(r.Context(), appProjectID, updateID, body.RolloutPercentage)
	if err != nil {
		switch {
		case errors.Is(err, controllers.ErrOTANotFound),
			errors.Is(err, controllers.ErrOTAProjectMismatch):
			utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		default:
			slog.Error("ota promote failed", "app_project_id", appProjectID, "update", updateID, "err", err)
			utils.JSONInternalError(w, err)
		}
		return
	}

	v.Controller.TrackUserEvent(user, "app_ota_promoted", posthog.NewProperties().
		Set("app_id", appProjectID.String()).
		Set("channel", row.Channel).
		Set("platform", row.Platform).
		Set("rollout_percentage", row.RolloutPercentage))

	utils.JSONResponse(w, map[string]any{
		"update_id":          row.UpdateID.String(),
		"channel":            row.Channel,
		"platform":           row.Platform,
		"runtime_version":    row.RuntimeVersion,
		"rollout_percentage": row.RolloutPercentage,
		"is_active":          row.IsActive,
	}, http.StatusOK)
}
