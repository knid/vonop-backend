package views

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

// GetMinVersion serves GET /v1/mobile/{slugOrID}/min-version?platform=...
//
// Public, un-authenticated. The mobile app cold-start gate calls this with
// no creds — the response only exposes operator-controlled values
// (min_build_number, store_url, message), no PII or secrets.
//
// Cache-Control: public, max-age=300 — five minutes is short enough that
// a freshly-bumped floor reaches users on their next cold start within ten
// minutes (5min cache + 5min stale window in the worst case), and long
// enough to absorb a thundering herd from a popular customer's app.
func (v *HttpViews) GetMinVersion(w http.ResponseWriter, r *http.Request) {
	slugOrID := chi.URLParam(r, "slugOrID")

	platform := r.URL.Query().Get("platform")
	if platform == "" {
		utils.JSONErrorCode(w, constants.ErrValidationRequired, http.StatusBadRequest, map[string]any{"field": "platform"})
		return
	}
	if platform != "ios" && platform != "android" {
		utils.JSONError(w, constants.ErrValidationBody, "platform must be 'ios' or 'android'", http.StatusBadRequest, nil)
		return
	}

	project, err := v.Controller.GetProjectForPublicMobile(slugOrID)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	minBuild, storeURL, message, err := v.Controller.MinVersionForPlatform(project, platform)
	if err != nil {
		// MinVersionForPlatform's only non-nil-project error is unknown
		// platform, which we already validated above. Defensive: treat any
		// other error as a 500 so we don't silently lock out users.
		utils.JSONInternalError(w, err)
		return
	}

	w.Header().Set("Cache-Control", "public, max-age=300")
	utils.JSONResponse(w, responses.MinVersionResponse{
		MinBuildNumber: minBuild,
		StoreURL:       storeURL,
		Message:        message,
	}, http.StatusOK)
}

// UpdateMinVersion serves PATCH /v2/apps/{appId}/min-version.
//
// Owner/admin-only. Mirrors MoveAppProjectToOrg's role check pattern. Returns
// 403 on insufficient role so the dashboard can render a clean inline error
// without confusing the user with a generic "permission denied".
func (v *HttpViews) UpdateMinVersion(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	appID := chi.URLParam(r, "appId")

	var req requests.UpdateMinVersionRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.ValidationError(w, err)
		return
	}
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := req.Validate(validate); err != nil {
		utils.ValidationError(w, err)
		return
	}

	updated, err := v.Controller.UpdateMinVersion(user, appID, req.Platform, req.MinBuildNumber, req.Message)
	if err != nil {
		switch {
		case errors.Is(err, controllers.ErrMinVersionProjectNotFound):
			utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		case errors.Is(err, controllers.ErrMinVersionPermissionDenied):
			utils.JSONErrorCode(w, constants.ErrForbidden, http.StatusForbidden, nil)
		case errors.Is(err, controllers.ErrMinVersionNotMobileNative):
			utils.JSONError(w, constants.ErrValidationBody, err.Error(), http.StatusBadRequest, nil)
		case errors.Is(err, controllers.ErrMinVersionUnknownPlatform):
			utils.JSONError(w, constants.ErrValidationBody, err.Error(), http.StatusBadRequest, nil)
		default:
			utils.JSONInternalError(w, err)
		}
		return
	}

	// Echo back the platform-specific value the caller just set.
	var minBuild int64
	switch req.Platform {
	case "ios":
		minBuild = updated.MinBuildNumberIOS
	case "android":
		minBuild = updated.MinBuildNumberAndroid
	}
	utils.JSONResponse(w, responses.AdminMinVersionResponse{
		Platform:       req.Platform,
		MinBuildNumber: minBuild,
		Message:        updated.MinVersionMessage,
	}, http.StatusOK)
}
