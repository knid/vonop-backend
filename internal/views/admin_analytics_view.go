package views

import (
	"net/http"

	"github.com/knid/wemob/internal/utils"
)

// AdminGetAnalyticsOverview handles GET /v1/admin/analytics/overview
func (v *HttpViews) AdminGetAnalyticsOverview(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if user.Type != "admin" {
		utils.JSONErrorResponse(w, "admin access required", http.StatusForbidden)
		return
	}

	result, err := v.Controller.AdminGetAnalyticsOverview()
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}
