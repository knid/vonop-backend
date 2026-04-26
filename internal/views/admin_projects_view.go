package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/utils"
)

// AdminListProjects handles GET /v1/admin/projects
func (v *HttpViews) AdminListProjects(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	if user.Type != "admin" {
		utils.JSONErrorCode(w, constants.ErrAdminRequired, http.StatusForbidden, nil)
		return
	}

	params := adminListParamsFromQuery(r)
	result, err := v.Controller.AdminListProjects(params)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}

// AdminGetProject handles GET /v1/admin/projects/{id}
func (v *HttpViews) AdminGetProject(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	if user.Type != "admin" {
		utils.JSONErrorCode(w, constants.ErrAdminRequired, http.StatusForbidden, nil)
		return
	}

	id := chi.URLParam(r, "projectId")
	result, err := v.Controller.AdminGetProject(id)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}

// AdminDeleteProject handles DELETE /v1/admin/projects/{id}
func (v *HttpViews) AdminDeleteProject(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	if user.Type != "admin" {
		utils.JSONErrorCode(w, constants.ErrAdminRequired, http.StatusForbidden, nil)
		return
	}

	id := chi.URLParam(r, "projectId")
	if err := v.Controller.AdminDeleteProject(id); err != nil {
		utils.ValidationError(w, err)
		return
	}

	utils.JSONResponse(w, map[string]string{"message": "project deleted"}, http.StatusOK)
}
