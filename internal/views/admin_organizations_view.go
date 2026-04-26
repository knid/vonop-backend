package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/utils"
)

// AdminListOrganizations handles GET /v1/admin/organizations
func (v *HttpViews) AdminListOrganizations(w http.ResponseWriter, r *http.Request) {
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
	result, err := v.Controller.AdminListOrganizations(params)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}

// AdminGetOrganization handles GET /v1/admin/organizations/{id}
func (v *HttpViews) AdminGetOrganization(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	if user.Type != "admin" {
		utils.JSONErrorCode(w, constants.ErrAdminRequired, http.StatusForbidden, nil)
		return
	}

	id := chi.URLParam(r, "orgId")
	result, err := v.Controller.AdminGetOrganization(id)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}
