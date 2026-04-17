package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
)

// GET /v2/apps/{appId}/store-listing
// Returns the persistent store listing for this project, or a zero-value
// default when the user hasn't saved anything yet. The frontend wizard
// uses this as the initial state instead of client-side localStorage.
func (v *HttpViews) GetAppStoreListing(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if requireActiveSubscription(w, user) {
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	listing, err := v.Controller.GetOrCreateAppStoreListing(project)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}
	utils.JSONResponse(w, listing, http.StatusOK)
}

// PUT /v2/apps/{appId}/store-listing
// Debounced from the frontend wizard — each change triggers a partial
// update. Because the client sends the entire ordered screenshots array
// whenever it changes, re-ordering a single drag becomes a single
// round-trip.
func (v *HttpViews) UpdateAppStoreListing(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if requireActiveSubscription(w, user) {
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	var req requests.UpdateAppStoreListingRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	listing, err := v.Controller.UpdateAppStoreListing(project, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}
	utils.JSONResponse(w, listing, http.StatusOK)
}
