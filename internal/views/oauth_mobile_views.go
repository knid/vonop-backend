package views

import (
	"net/http"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
	"github.com/posthog/posthog-go"
)

func (v *HttpViews) GoogleMobileLogin(w http.ResponseWriter, r *http.Request) {
	var req requests.GoogleMobileLoginRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.ValidationError(w, err)
		return
	}

	if req.IDToken == "" {
		utils.JSONErrorCode(w, constants.ErrValidationRequired, http.StatusBadRequest, map[string]any{"field": "id_token"})
		return
	}

	loginRes, err := v.Controller.GoogleMobileLogin(r.Context(), req)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	go func() {
		token, tokErr := v.Controller.DB.GetTokenByToken(loginRes.Token)
		if tokErr != nil {
			return
		}
		u, userErr := v.Controller.DB.GetUserById(token.UserID.String())
		if userErr != nil {
			return
		}
		v.Controller.EnsureIdentified(u)
		v.Controller.TrackUserEvent(u, "user_oauth_logged_in", posthog.NewProperties().
			Set("provider", "google_mobile").
			Set("subscription_status", u.SubscriptionStatus))
	}()

	utils.JSONResponse(w, loginRes, http.StatusOK)
}
