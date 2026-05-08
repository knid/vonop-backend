package views

import (
	"errors"
	"net/http"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/controllers"
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
		if errors.Is(err, controllers.ErrEmailHasCredentialsAccount) {
			utils.JSONErrorCode(w, constants.ErrAuthEmailHasCredentialsAccount, http.StatusConflict, nil)
			return
		}
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
			Set("subscription_status", v.Controller.GetUserBilling(u).SubscriptionStatus))
	}()

	utils.JSONResponse(w, loginRes, http.StatusOK)
}
