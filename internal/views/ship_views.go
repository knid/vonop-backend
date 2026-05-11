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

	var orgID string
	if rawOrgID := r.URL.Query().Get("org_id"); rawOrgID != "" {
		verifiedOrgID, oErr := v.Controller.RequireOrgMembership(user, rawOrgID)
		if oErr != nil {
			utils.JSONErrorCode(w, constants.ErrForbidden, http.StatusForbidden, nil)
			return
		}
		orgID = verifiedOrgID.String()
	}

	resp, err := v.Controller.GetShipItems(r.Context(), user, orgID)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}
	utils.JSONResponse(w, resp, http.StatusOK)
}
