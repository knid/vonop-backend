package views

import (
	"net/http"

	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
)

// ValidateAppleConnection handles POST /v1/connections/apple/validate.
func (v *HttpViews) ValidateAppleConnection(w http.ResponseWriter, r *http.Request) {
	_, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req requests.ValidateAppleConnectionRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	result := v.Controller.ValidateAppleConnection(r.Context(), req)
	utils.JSONResponse(w, result, http.StatusOK)
}

// ValidateGooglePlayConnection handles POST /v1/connections/google-play/validate.
func (v *HttpViews) ValidateGooglePlayConnection(w http.ResponseWriter, r *http.Request) {
	_, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req requests.ValidateGooglePlayConnectionRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	result := v.Controller.ValidateGooglePlayConnection(r.Context(), req)
	utils.JSONResponse(w, result, http.StatusOK)
}
