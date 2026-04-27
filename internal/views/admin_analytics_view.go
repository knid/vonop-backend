package views

import (
	"net/http"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/utils"
)

// AdminGetAnalyticsOverview handles GET /v1/admin/analytics/overview
func (v *HttpViews) AdminGetAnalyticsOverview(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	if user.Type != "admin" {
		utils.JSONErrorCode(w, constants.ErrAdminRequired, http.StatusForbidden, nil)
		return
	}

	result, err := v.Controller.AdminGetAnalyticsOverview()
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}
