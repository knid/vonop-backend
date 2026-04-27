package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/utils"
)

// GetLaunchpad handles GET /v1/apps/{appId}/launchpad. Returns the
// single aggregate response powering the Ship tab's Launchpad surface.
func (v *HttpViews) GetLaunchpad(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	appIDStr := chi.URLParam(r, "appId")
	if appIDStr == "" {
		utils.JSONErrorCode(w, constants.ErrValidationRequired, http.StatusBadRequest, map[string]any{"field": "app_id"})
		return
	}
	appID, err := uuid.Parse(appIDStr)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrValidationInvalidFormat, http.StatusBadRequest, map[string]any{"field": "app_id"})
		return
	}

	resp, err := v.Controller.GetLaunchpad(r.Context(), user, appID)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}
	utils.JSONResponse(w, resp, http.StatusOK)
}
