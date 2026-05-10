package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/utils"
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

func (v *HttpViews) GetProject(w http.ResponseWriter, r *http.Request) {
	// Implementation of the view to list all projects for a user
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	if requireActiveSubscription(w, v.Controller, user) {
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

	if requireActiveSubscription(w, v.Controller, user) {
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

	// EVENT_PROJECT_DELETED previously used `project_id`; switching to the
	// shared AppEventProps so dashboards can filter every app-scoped event by
	// `app_id`. The project row is already gone by this point, so the
	// title-lookup inside AppEventProps will no-op — only `app_id` lands.
	go v.Controller.TrackUserEvent(user, constants.EVENT_PROJECT_DELETED,
		v.Controller.AppEventProps(projectID))

	utils.JSONResponse(w, deletedProjectRes, http.StatusOK)
}
