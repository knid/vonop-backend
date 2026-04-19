package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
)

// DraftListing handles POST /v1/apps/{appId}/listing/draft.
func (v *HttpViews) DraftListing(w http.ResponseWriter, r *http.Request) {
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

	appIDStr := chi.URLParam(r, "appId")
	appID, err := uuid.Parse(appIDStr)
	if err != nil {
		utils.JSONErrorResponse(w, "invalid app_id", http.StatusBadRequest)
		return
	}

	var req requests.DraftListingRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	resp, err := v.Controller.DraftListing(r.Context(), user, appID, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	utils.JSONResponse(w, resp, http.StatusOK)
}
