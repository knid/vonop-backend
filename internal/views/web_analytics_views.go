package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
)

// POST /v1/analytics/collect — public endpoint (no auth).
// Accepts a single event JSON object (from the JS tracker) or a batch
// wrapper with {"events": [...]}.
func (v *HttpViews) CollectAnalytics(w http.ResponseWriter, r *http.Request) {
	// The JS tracker sends a single event object. Parse it and wrap in a batch.
	var single requests.CollectAnalyticsRequest
	if err := utils.ParseJSONBody(r, &single); err != nil {
		utils.JSONErrorResponse(w, "invalid request body", http.StatusBadRequest)
		return
	}

	batch := requests.CollectAnalyticsBatchRequest{
		Events: []requests.CollectAnalyticsRequest{single},
	}

	if err := v.Controller.CollectAnalyticsEvents(r, batch); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// POST /v1/analytics/collect/batch — public endpoint for batch event submission.
func (v *HttpViews) CollectAnalyticsBatch(w http.ResponseWriter, r *http.Request) {
	var batch requests.CollectAnalyticsBatchRequest
	if err := utils.ParseJSONBody(r, &batch); err != nil {
		utils.JSONErrorResponse(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := v.Controller.CollectAnalyticsEvents(r, batch); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GET /v2/apps/{appId}/analytics/overview
func (v *HttpViews) GetAppAnalyticsOverview(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	start, end := controllers.ParsePeriodFromRequest(r)
	result, err := v.Controller.GetAnalyticsOverview(project.ID.String(), start, end)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}

// GET /v2/apps/{appId}/analytics/pageviews
func (v *HttpViews) GetAppAnalyticsPageviews(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	start, end := controllers.ParsePeriodFromRequest(r)
	result, err := v.Controller.GetAnalyticsPageviews(project.ID.String(), start, end)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}

// GET /v2/apps/{appId}/analytics/pages
func (v *HttpViews) GetAppAnalyticsPages(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	start, end := controllers.ParsePeriodFromRequest(r)
	result, err := v.Controller.GetAnalyticsTopPages(project.ID.String(), start, end)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}

// GET /v2/apps/{appId}/analytics/referrers
func (v *HttpViews) GetAppAnalyticsReferrers(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	start, end := controllers.ParsePeriodFromRequest(r)
	result, err := v.Controller.GetAnalyticsReferrers(project.ID.String(), start, end)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}

// GET /v2/apps/{appId}/analytics/devices
func (v *HttpViews) GetAppAnalyticsDevices(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	start, end := controllers.ParsePeriodFromRequest(r)
	result, err := v.Controller.GetAnalyticsDevices(project.ID.String(), start, end)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}

// GET /v2/apps/{appId}/analytics/locations
func (v *HttpViews) GetAppAnalyticsLocations(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	start, end := controllers.ParsePeriodFromRequest(r)
	result, err := v.Controller.GetAnalyticsLocations(project.ID.String(), start, end)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}

// GET /v2/apps/{appId}/analytics/events
func (v *HttpViews) GetAppAnalyticsEvents(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	start, end := controllers.ParsePeriodFromRequest(r)
	result, err := v.Controller.GetAnalyticsCustomEvents(project.ID.String(), start, end)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}

// GET /v2/apps/{appId}/analytics/realtime
func (v *HttpViews) GetAppAnalyticsRealtime(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	result, err := v.Controller.GetAnalyticsRealtime(project.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}
