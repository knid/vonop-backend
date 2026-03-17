package views

import (
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
)

// POST /v1/projects/{projectId}/analytics/session — public endpoint (called from mobile apps).
func (v *HttpViews) CreateSession(w http.ResponseWriter, r *http.Request) {
	var req requests.CreateSessionRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, "invalid request body", http.StatusBadRequest)
		return
	}

	response, err := v.Controller.CreateSession(r, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, response, http.StatusOK)
}

// POST /v1/projects/{projectId}/analytics/events — public endpoint (called from mobile apps).
func (v *HttpViews) LogAppEvent(w http.ResponseWriter, r *http.Request) {
	var req requests.LogAppEventRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := v.Controller.LogAppEvent(r, req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GET /v1/projects/{projectId}/analytics/devices — authenticated.
func (v *HttpViews) GetAllDevices(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	if requireActiveSubscription(w, user) {
		return
	}

	projectID := chi.URLParam(r, "projectId")
	if projectID == "" {
		utils.JSONErrorResponse(w, "project_id is required", http.StatusBadRequest)
		return
	}

	projectInformationRes, err := v.Controller.GetProject(user, projectID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	devices, err := v.Controller.GetDevicesByProjectId(projectInformationRes.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, devices, http.StatusOK)
}

func (v *HttpViews) GetUniqueDevices(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	if requireActiveSubscription(w, user) {
		return
	}

	projectID := chi.URLParam(r, "projectId")
	if projectID == "" {
		utils.JSONErrorResponse(w, "project_id is required", http.StatusBadRequest)
		return
	}

	projectInformationRes, err := v.Controller.GetProject(user, projectID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	timeStartStr := r.URL.Query().Get("time_start")
	timeEndStr := r.URL.Query().Get("time_end")

	timeStartStr, err = url.QueryUnescape(timeStartStr)
	if err != nil {
		utils.JSONErrorResponse(w, "unsupported time_start value", http.StatusBadRequest)
		return
	}

	timeEndStr, err = url.QueryUnescape(timeEndStr)
	if err != nil {
		utils.JSONErrorResponse(w, "unsupported time_end value", http.StatusBadRequest)
		return
	}

	if timeStartStr == "" || timeEndStr == "" {
		utils.JSONErrorResponse(w, "time_start and time_end are required", http.StatusBadRequest)
		return
	}

	timeStart, err := utils.ParseTime(timeStartStr)
	if err != nil {
		utils.JSONErrorResponse(w, "invalid time_start format", http.StatusBadRequest)
		return
	}

	timeEnd, err := utils.ParseTime(timeEndStr)
	if err != nil {
		utils.JSONErrorResponse(w, "invalid time_end format", http.StatusBadRequest)
		return
	}

	devices, err := v.Controller.GetUniqueDevices(projectInformationRes.ID.String(), timeStart, timeEnd)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, devices, http.StatusOK)
}

func (v *HttpViews) GetActiveDevices(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	if requireActiveSubscription(w, user) {
		return
	}

	projectID := chi.URLParam(r, "projectId")
	if projectID == "" {
		utils.JSONErrorResponse(w, "project_id is required", http.StatusBadRequest)
		return
	}

	projectInformationRes, err := v.Controller.GetProject(user, projectID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	devices, err := v.Controller.GetActiveDevices(projectInformationRes.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, devices, http.StatusOK)
}

func (v *HttpViews) GetSessions(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	if requireActiveSubscription(w, user) {
		return
	}

	projectID := chi.URLParam(r, "projectId")
	if projectID == "" {
		utils.JSONErrorResponse(w, "project_id is required", http.StatusBadRequest)
		return
	}

	projectInformationRes, err := v.Controller.GetProject(user, projectID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	timeStartStr := r.URL.Query().Get("time_start")
	timeEndStr := r.URL.Query().Get("time_end")

	timeStartStr, err = url.QueryUnescape(timeStartStr)
	if err != nil {
		utils.JSONErrorResponse(w, "unsupported time_start value", http.StatusBadRequest)
		return
	}

	timeEndStr, err = url.QueryUnescape(timeEndStr)
	if err != nil {
		utils.JSONErrorResponse(w, "unsupported time_end value", http.StatusBadRequest)
		return
	}

	if timeStartStr == "" || timeEndStr == "" {
		utils.JSONErrorResponse(w, "time_start and time_end are required", http.StatusBadRequest)
		return
	}

	timeStart, err := utils.ParseTime(timeStartStr)
	if err != nil {
		utils.JSONErrorResponse(w, "invalid time_start format", http.StatusBadRequest)
		return
	}

	timeEnd, err := utils.ParseTime(timeEndStr)
	if err != nil {
		utils.JSONErrorResponse(w, "invalid time_end format", http.StatusBadRequest)
		return
	}

	sessions, err := v.Controller.GetSessionsByProject(projectInformationRes.ID.String(), timeStart, timeEnd)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, sessions, http.StatusOK)
}

func (v *HttpViews) GetProjectEvents(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	if requireActiveSubscription(w, user) {
		return
	}

	projectID := chi.URLParam(r, "projectId")
	if projectID == "" {
		utils.JSONErrorResponse(w, "project_id is required", http.StatusBadRequest)
		return
	}

	projectInformationRes, err := v.Controller.GetProject(user, projectID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	timeStartStr := r.URL.Query().Get("time_start")
	timeEndStr := r.URL.Query().Get("time_end")

	timeStartStr, err = url.QueryUnescape(timeStartStr)
	if err != nil {
		utils.JSONErrorResponse(w, "unsupported time_start value", http.StatusBadRequest)
		return
	}

	timeEndStr, err = url.QueryUnescape(timeEndStr)
	if err != nil {
		utils.JSONErrorResponse(w, "unsupported time_end value", http.StatusBadRequest)
		return
	}

	if timeStartStr == "" || timeEndStr == "" {
		utils.JSONErrorResponse(w, "time_start and time_end are required", http.StatusBadRequest)
		return
	}

	timeStart, err := utils.ParseTime(timeStartStr)
	if err != nil {
		utils.JSONErrorResponse(w, "invalid time_start format", http.StatusBadRequest)
		return
	}

	timeEnd, err := utils.ParseTime(timeEndStr)
	if err != nil {
		utils.JSONErrorResponse(w, "invalid time_end format", http.StatusBadRequest)
		return
	}

	projectEvents, err := v.Controller.GetProjectEvents(projectInformationRes.ID.String(), timeStart, timeEnd)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, projectEvents, http.StatusOK)
}
