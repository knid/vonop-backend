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
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	projects, err := v.Controller.GetProjects(user)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, projects, http.StatusOK)

}

func (v *HttpViews) UpdateProject(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	if requireActiveSubscription(w, user) {
		go v.Controller.EventService.SendEvent(user, constants.EVENT_SUBSCRIPTION_NEED, map[string]any{
			"action": "update_project",
		})
		return
	}

	var projectCreateReq requests.ProjectCreateRequest
	if err := utils.ParseJSONBody(r, &projectCreateReq); err != nil {
		utils.ValidationError(w, err)
		return
	}

	projectCreateRes, err := v.Controller.UpdateProject(r, user, projectCreateReq)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	go v.Controller.EventService.SendEvent(user, constants.EVENT_PROJECT_UPDATED, map[string]any{
		"project_id": projectCreateRes.ID.String(),
	})
	go v.Controller.TrackUserEvent(user, "project_updated", posthog.NewProperties().
		Set("project_id", projectCreateRes.ID.String()))

	utils.JSONResponse(w, projectCreateRes, http.StatusOK)
}

func (v *HttpViews) CreateProject(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	if requireActiveSubscription(w, user) {
		go v.Controller.EventService.SendEvent(user, constants.EVENT_SUBSCRIPTION_NEED, map[string]any{
			"action": "create_project",
		})
		return
	}

	limits, planName, err := v.Controller.GetUserLimitsWithPlan(user)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	projects, err := v.Controller.GetProjects(user)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	if len(projects) >= limits.MaximumProjectCount {
		utils.JSONErrorCode(w, constants.ErrProjectLimitReached, http.StatusForbidden, map[string]any{
			"used":      len(projects),
			"limit":     limits.MaximumProjectCount,
			"plan_name": planName,
		})
		go v.Controller.EventService.SendEvent(user, constants.EVENT_SUBSCRIPTION_QUOTA_REACHED, map[string]any{
			"action": "create_project",
			"limit":  limits.MaximumProjectCount,
		})
		go v.Controller.TrackUserEvent(user, "quota_reached", posthog.NewProperties().Set("action", "create_project").Set("limit", limits.MaximumProjectCount))
		return
	}

	var projectCreateReq requests.ProjectCreateRequest
	if err := utils.ParseJSONBody(r, &projectCreateReq); err != nil {
		utils.ValidationError(w, err)
		return
	}

	projectCreateRes, err := v.Controller.CreateProject(user, projectCreateReq)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	go v.Controller.EventService.SendEvent(user, constants.EVENT_NEW_PROJECT, map[string]any{
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
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	if requireActiveSubscription(w, user) {
		return
	}

	projectID := chi.URLParam(r, "projectId")
	if projectID == "" {
		utils.JSONErrorCode(w, constants.ErrValidationRequired, http.StatusBadRequest, map[string]any{"field": "project_id"})
		return
	}

	projectInformationRes, err := v.Controller.GetProject(user, projectID)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, projectInformationRes, http.StatusOK)
}

func (v *HttpViews) DeleteProject(w http.ResponseWriter, r *http.Request) {
	// Implementation of the view to list all projects for a user
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	if requireActiveSubscription(w, user) {
		return
	}

	projectID := chi.URLParam(r, "projectId")
	if projectID == "" {
		utils.JSONErrorCode(w, constants.ErrValidationRequired, http.StatusBadRequest, map[string]any{"field": "project_id"})
		return
	}

	deletedProjectRes, err := v.Controller.DeleteProject(user, projectID)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	go v.Controller.TrackUserEvent(user, "project_deleted", posthog.NewProperties().
		Set("project_id", projectID))

	utils.JSONResponse(w, deletedProjectRes, http.StatusOK)
}
