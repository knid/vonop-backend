package views

import (
	"net/http"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) GetShipItems(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	resp, err := v.Controller.GetShipItems(r.Context(), user)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}
	utils.JSONResponse(w, resp, http.StatusOK)
}
