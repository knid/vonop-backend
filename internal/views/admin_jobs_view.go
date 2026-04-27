package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/utils"
)

// AdminListJobs handles GET /v1/admin/jobs
func (v *HttpViews) AdminListJobs(w http.ResponseWriter, r *http.Request) {
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
	result, err := v.Controller.AdminListJobs(params)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}

// AdminGetJob handles GET /v1/admin/jobs/{id}
func (v *HttpViews) AdminGetJob(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	if user.Type != "admin" {
		utils.JSONErrorCode(w, constants.ErrAdminRequired, http.StatusForbidden, nil)
		return
	}

	id := chi.URLParam(r, "jobId")
	result, err := v.Controller.AdminGetJob(id)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}
