package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/utils"
)

// GetLaunchpad handles GET /v1/apps/{appId}/launchpad. Returns the
// single aggregate response powering the Ship tab's Launchpad surface.
func (v *HttpViews) GetLaunchpad(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appIDStr := chi.URLParam(r, "appId")
	if appIDStr == "" {
		utils.JSONErrorResponse(w, "app_id is required", http.StatusBadRequest)
		return
	}
	appID, err := uuid.Parse(appIDStr)
	if err != nil {
		utils.JSONErrorResponse(w, "invalid app_id", http.StatusBadRequest)
		return
	}

	resp, err := v.Controller.GetLaunchpad(r.Context(), user, appID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}
	utils.JSONResponse(w, resp, http.StatusOK)
}
