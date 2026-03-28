package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
	"github.com/posthog/posthog-go"
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

	if requireActiveSubscription(w, user) {
		go v.Controller.CRMService.SendEvent(user, constants.BREVO_EVENT_SUBSCRIPTION_NEED, map[string]any{
			"action": "update_project",
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
	go v.Controller.TrackUserEvent(user, "project_updated", posthog.NewProperties().
		Set("project_id", projectCreateRes.ID.String()))

	utils.JSONResponse(w, projectCreateRes, http.StatusOK)
}

func (v *HttpViews) CreateProject(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	if requireActiveSubscription(w, user) {
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
		go v.Controller.TrackUserEvent(user, "quota_reached", posthog.NewProperties().Set("action", "create_project").Set("limit", limits.MaximumProjectCount))
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
	go v.Controller.TrackUserEvent(user, "project_created", posthog.NewProperties().
		Set("project_id", projectCreateRes.ProjectID.String()).
		Set("project_title", projectCreateReq.Title))

	utils.JSONResponse(w, projectCreateRes, http.StatusCreated)
}

func (v *HttpViews) GetProject(w http.ResponseWriter, r *http.Request) {
	// Implementation of the view to list all projects for a user
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

	if requireActiveSubscription(w, user) {
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

	go v.Controller.TrackUserEvent(user, "project_deleted", posthog.NewProperties().
		Set("project_id", projectID))

	utils.JSONResponse(w, deletedProjectRes, http.StatusOK)
}
