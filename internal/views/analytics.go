package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) CreateSession(w http.ResponseWriter, r *http.Request) {
	var createSessionReq requests.CreateSessionRequest
	err := utils.ParseJSONBody(r, &createSessionReq)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	response, err := v.Controller.CreateSession(r, createSessionReq)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, response, http.StatusOK)
}

func (v *HttpViews) LogAppEvent(w http.ResponseWriter, r *http.Request) {
	var logAppEventReq requests.LogAppEventRequest
	err := utils.ParseJSONBody(r, &logAppEventReq)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	response, err := v.Controller.LogAppEvent(r, logAppEventReq)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, response, http.StatusOK)
}

func (v *HttpViews) GetDevices(w http.ResponseWriter, r *http.Request) {

	tokenString, err := utils.ExtractTokenFromHeader(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}
	userInformationRes, err := v.Controller.GetUser(tokenString)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	user, err := v.Controller.GetUserModelById(userInformationRes.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	if !(user.SubscriptionStatus == "active" || user.SubscriptionStatus == "trial") {
		utils.JSONErrorResponse(w, "you do not have access to this resource", http.StatusForbidden)
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
	project, err := v.Controller.GetProjectModelById(projectInformationRes.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	devices, err := v.Controller.GetDevicesByProjectId(project.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, devices, http.StatusOK)
}

func (v *HttpViews) GetActiveUsers(w http.ResponseWriter, r *http.Request) {
	tokenString, err := utils.ExtractTokenFromHeader(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}
	userInformationRes, err := v.Controller.GetUser(tokenString)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	user, err := v.Controller.GetUserModelById(userInformationRes.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	if !(user.SubscriptionStatus == "active" || user.SubscriptionStatus == "trial") {
		utils.JSONErrorResponse(w, "you do not have access to this resource", http.StatusForbidden)
		return
	}

	subs, err := v.Controller.GetSubscription(user)
	if err != nil || subs == nil || !subs.Limits.CanSeeActiveUsers {
		utils.JSONErrorResponse(w, "you do not have access to this resource. please upgrade your subscription", http.StatusForbidden)
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
	project, err := v.Controller.GetProjectModelById(projectInformationRes.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	devices, err := v.Controller.GetActiveUsers(project.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, devices, http.StatusOK)

}

func (v *HttpViews) GetUniqueUsers(w http.ResponseWriter, r *http.Request) {
	tokenString, err := utils.ExtractTokenFromHeader(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}
	userInformationRes, err := v.Controller.GetUser(tokenString)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	user, err := v.Controller.GetUserModelById(userInformationRes.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	if !(user.SubscriptionStatus == "active" || user.SubscriptionStatus == "trial") {
		utils.JSONErrorResponse(w, "you do not have access to this resource", http.StatusForbidden)
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
	project, err := v.Controller.GetProjectModelById(projectInformationRes.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	// extract timeStart and timeEnd from query parameters
	timeStartStr := r.URL.Query().Get("time_start")
	timeEndStr := r.URL.Query().Get("time_end")

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

	devices, err := v.Controller.GetUniqueUsers(project.ID.String(), timeStart, timeEnd)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, devices, http.StatusOK)
}
