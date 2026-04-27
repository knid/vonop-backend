package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
)

// AdminListSubscriptions handles GET /v1/admin/subscriptions
func (v *HttpViews) AdminListSubscriptions(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	if user.Type != "admin" {
		utils.JSONErrorCode(w, constants.ErrAdminRequired, http.StatusForbidden, nil)
		return
	}

	result, err := v.Controller.AdminListSubscriptions()
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}

// AdminUpdateSubscription handles PATCH /v1/admin/subscriptions/{id}
func (v *HttpViews) AdminUpdateSubscription(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	if user.Type != "admin" {
		utils.JSONErrorCode(w, constants.ErrAdminRequired, http.StatusForbidden, nil)
		return
	}

	id := chi.URLParam(r, "subId")

	var req requests.AdminUpdateSubscriptionPlanRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.ValidationError(w, err)
		return
	}

	result, err := v.Controller.AdminUpdateSubscription(id, req)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}
