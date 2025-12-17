package views

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) GetProjects(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	projects, err := v.Controller.GetProjects(user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, projects, http.StatusOK)

}

func (v *HttpViews) UpdateProject(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	if !(user.SubscriptionStatus == "active" || user.SubscriptionStatus == "trial") {
		utils.JSONErrorResponse(w, "you do not have access to this resource", http.StatusForbidden)
		go v.Controller.CRMService.SendEvent(user, constants.BREVO_EVENT_SUBSCRIPTION_NEED, map[string]any{
			"action": "create_project",
		})
		return
	}

	builds, err := v.Controller.DB.GetBuildsByUserId(user.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	subsStart := user.SubscriptionStartAt
	now := time.Now()

	renewStart := time.Date(now.Year(), now.Month(), subsStart.Day(), 0, 0, 0, 0, now.Location())

	if now.Before(renewStart) {
		renewStart = renewStart.AddDate(0, -1, 0)
	}

	renewEnd := renewStart.AddDate(0, 1, 0)

	count := 0
	for _, build := range builds {
		if build.CreatedAt.After(renewStart) && build.CreatedAt.Before(renewEnd) {
			count++
		}
	}

	subsInfo, err := v.Controller.GetSubscription(user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	limits, err := v.Controller.DB.GetStandartUserLimitsById(subsInfo.Limits.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	if len(builds) >= limits.MontlyBuildCount {
		utils.JSONErrorResponse(w, "you have reached your montly build limit", http.StatusForbidden)
		go v.Controller.CRMService.SendEvent(user, constants.BREVO_EVENT_SUBSCRIPTION_QUOTA_REACHED, map[string]any{
			"action": "update_project",
			"limit":  limits.MontlyBuildCount,
		})
		return
	}

	var projectCreateReq requests.ProjectCreateRequest
	if err := utils.ParseJSONBody(r, &projectCreateReq); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	projectCreateRes, err := v.Controller.UpdateProject(r, user, projectCreateReq)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	go v.Controller.CRMService.SendEvent(user, constants.BREVO_EVENT_PROJECT_UPDATED, map[string]any{
		"project_id": projectCreateRes.ID.String(),
	})

	utils.JSONResponse(w, projectCreateRes, http.StatusOK)

}

func (v *HttpViews) CreateProject(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	if !(user.SubscriptionStatus == "active" || user.SubscriptionStatus == "trial") {
		utils.JSONErrorResponse(w, "you do not have access to this resource", http.StatusForbidden)
		go v.Controller.CRMService.SendEvent(user, constants.BREVO_EVENT_SUBSCRIPTION_NEED, map[string]any{
			"action": "create_project",
		})
		return
	}

	projects, err := v.Controller.DB.GetProjects(user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	subsInfo, err := v.Controller.GetSubscription(user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	limits, err := v.Controller.DB.GetStandartUserLimitsById(subsInfo.Limits.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	if len(projects) >= limits.MaximumProjectCount {
		utils.JSONErrorResponse(w, "you have reached your project limit", http.StatusForbidden)
		go v.Controller.CRMService.SendEvent(user, constants.BREVO_EVENT_SUBSCRIPTION_QUOTA_REACHED, map[string]any{
			"action": "create_project",
			"limit":  limits.MaximumProjectCount,
		})
		return
	}

	var projectCreateReq requests.ProjectCreateRequest
	if err := utils.ParseJSONBody(r, &projectCreateReq); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	projectCreateRes, err := v.Controller.CreateProject(user, projectCreateReq)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	go v.Controller.CRMService.SendEvent(user, constants.BREVO_EVENT_NEW_PROJECT, map[string]any{
		"project_id": projectCreateRes.ProjectID.String(),
	})

	utils.JSONResponse(w, projectCreateRes, http.StatusCreated)
}

func (v *HttpViews) GetProject(w http.ResponseWriter, r *http.Request) {
	// Implementation of the view to list all projects for a user
	user, err := v.Controller.GetUserFromReq(r)
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
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, projectInformationRes, http.StatusOK)
}

func (v *HttpViews) DeleteProject(w http.ResponseWriter, r *http.Request) {
	// Implementation of the view to list all projects for a user
	user, err := v.Controller.GetUserFromReq(r)
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

	deletedProjectRes, err := v.Controller.DeleteProject(user, projectID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, deletedProjectRes, http.StatusNoContent)
}
