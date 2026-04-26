package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
)

// AdminListApps handles GET /v1/admin/apps
func (v *HttpViews) AdminListApps(w http.ResponseWriter, r *http.Request) {
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
	result, err := v.Controller.AdminListApps(params)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}

// AdminGetApp handles GET /v1/admin/apps/{id}
func (v *HttpViews) AdminGetApp(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	if user.Type != "admin" {
		utils.JSONErrorCode(w, constants.ErrAdminRequired, http.StatusForbidden, nil)
		return
	}

	id := chi.URLParam(r, "appId")
	result, err := v.Controller.AdminGetApp(id)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}

// AdminUpdateApp handles PATCH /v1/admin/apps/{id}
func (v *HttpViews) AdminUpdateApp(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	if user.Type != "admin" {
		utils.JSONErrorCode(w, constants.ErrAdminRequired, http.StatusForbidden, nil)
		return
	}

	id := chi.URLParam(r, "appId")

	var req requests.AdminUpdateAppRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.ValidationError(w, err)
		return
	}

	result, err := v.Controller.AdminUpdateApp(id, req)
	if err != nil {
		utils.ValidationError(w, err)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}

// AdminDeleteApp handles DELETE /v1/admin/apps/{id}
func (v *HttpViews) AdminDeleteApp(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	if user.Type != "admin" {
		utils.JSONErrorCode(w, constants.ErrAdminRequired, http.StatusForbidden, nil)
		return
	}

	id := chi.URLParam(r, "appId")
	if err := v.Controller.AdminDeleteApp(id); err != nil {
		utils.ValidationError(w, err)
		return
	}

	utils.JSONResponse(w, map[string]string{"message": "app deleted"}, http.StatusOK)
}
