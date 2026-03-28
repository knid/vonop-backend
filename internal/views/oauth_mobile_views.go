package views

import (
	"net/http"

	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
	"github.com/posthog/posthog-go"
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

	go func() {
		if u, lookupErr := v.Controller.DB.GetUserByEmail(req.Email); lookupErr == nil {
			v.Controller.EnsureIdentified(u)
			v.Controller.TrackUserEvent(u, "user_oauth_logged_in", posthog.NewProperties().
				Set("provider", "google_mobile").
				Set("subscription_status", u.SubscriptionStatus))
		}
	}()

	utils.JSONResponse(w, loginRes, http.StatusOK)
}
