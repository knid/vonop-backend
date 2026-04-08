package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/utils"
)

// AdminListJobs handles GET /v1/admin/jobs
func (v *HttpViews) AdminListJobs(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if user.Type != "admin" {
		utils.JSONErrorResponse(w, "admin access required", http.StatusForbidden)
		return
	}

	params := adminListParamsFromQuery(r)
	result, err := v.Controller.AdminListJobs(params)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}

// AdminGetJob handles GET /v1/admin/jobs/{id}
func (v *HttpViews) AdminGetJob(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if user.Type != "admin" {
		utils.JSONErrorResponse(w, "admin access required", http.StatusForbidden)
		return
	}

	id := chi.URLParam(r, "id")
	result, err := v.Controller.AdminGetJob(id)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}
