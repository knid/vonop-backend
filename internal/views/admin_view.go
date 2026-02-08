package views

import (
	"net/http"

	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) GetAdminDashboard(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	result, err := v.Controller.GetAdminDashboard(user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusForbidden)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}
