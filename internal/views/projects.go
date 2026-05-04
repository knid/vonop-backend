package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/constants"
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

	go v.Controller.TrackUserEvent(user, "project_deleted", posthog.NewProperties().
		Set("project_id", projectID))

	utils.JSONResponse(w, deletedProjectRes, http.StatusOK)
}
