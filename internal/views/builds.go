package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) GetProjectBuilds(w http.ResponseWriter, r *http.Request) {
	// Implementation of the view to get builds for a specific project

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
	project, err := v.Controller.GetProjectModelById(projectInformationRes.ID.String())
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	builds, err := v.Controller.GetBuilds(project)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, builds, http.StatusOK)
}

func (v *HttpViews) GetProjectBuild(w http.ResponseWriter, r *http.Request) {
	// Implementation of the view to get a specific build for a specific project

	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	if requireActiveSubscription(w, v.Controller, user) {
		return
	}

	buildID := chi.URLParam(r, "buildId")
	if buildID == "" {
		utils.JSONErrorCode(w, constants.ErrValidationRequired, http.StatusBadRequest, map[string]any{"field": "build_id"})
		return
	}

	build, err := v.Controller.GetBuild(user, buildID)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, build, http.StatusOK)
}
