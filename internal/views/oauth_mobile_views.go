package views

import (
	"net/http"

	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) GoogleMobileLogin(w http.ResponseWriter, r *http.Request) {
	var req requests.GoogleMobileLoginRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.IDToken == "" {
		utils.JSONErrorResponse(w, "id_token is required", http.StatusBadRequest)
		return
	}

	loginRes, err := v.Controller.GoogleMobileLogin(req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	utils.JSONResponse(w, loginRes, http.StatusOK)
}
