package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/utils"
)

// AdminListOrganizations handles GET /v1/admin/organizations
func (v *HttpViews) AdminListOrganizations(w http.ResponseWriter, r *http.Request) {
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
	result, err := v.Controller.AdminListOrganizations(params)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}

// AdminGetOrganization handles GET /v1/admin/organizations/{id}
func (v *HttpViews) AdminGetOrganization(w http.ResponseWriter, r *http.Request) {
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
	result, err := v.Controller.AdminGetOrganization(id)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}
