package views

import (
	"errors"
	"net/http"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/middleware"
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

	locale := middleware.ResolveLocale(r.Header.Get("Accept-Language"))
	loginRes, err := v.Controller.GoogleMobileLogin(r.Context(), req, locale)
	if err != nil {
		if errors.Is(err, controllers.ErrEmailHasCredentialsAccount) {
			utils.JSONErrorCode(w, constants.ErrAuthEmailHasCredentialsAccount, http.StatusConflict, nil)
			return
		}
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	isNewUser := loginRes.IsNewUser
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
		v.Controller.TrackUserEvent(u, constants.EVENT_USER_OAUTH_LOGGED_IN, posthog.NewProperties().
			Set("provider", "google_mobile"))
		// Mirror the web OAuth flow (views/oauth.go): emit the unified
		// user_logged_in event so dashboards filtering on "any login" pick
		// up mobile sign-ins alongside web ones, and on first sign-in also
		// fire user_registered so mobile OAuth signups appear in the
		// activation funnel. Without these the mobile cohort was entirely
		// invisible — user_oauth_logged_in alone isn't in the canonical
		// signup/login funnels.
		v.Controller.TrackUserEvent(u, constants.EVENT_NEW_LOGIN, posthog.NewProperties().
			Set("provider", u.Provider))
		if isNewUser {
			v.Controller.TrackUserEvent(u, constants.EVENT_NEW_USER, posthog.NewProperties().
				Set("provider", u.Provider))
		}
	}()

	utils.JSONResponse(w, loginRes, http.StatusOK)
}
