package views

import (
	"log/slog"
	"net/http"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/utils"
	"github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/billingportal/session"
)

func (v *HttpViews) StripeCustomerPortal(w http.ResponseWriter, r *http.Request) {
	// Prefer Authorization header, fall back to form POST for legacy clients
	tokenString, _ := utils.ExtractTokenFromHeader(r)
	if tokenString == "" {
		tokenString = r.PostFormValue("token")
	}
	if tokenString == "" {
		utils.JSONErrorCode(w, constants.ErrAuthRequired, http.StatusUnauthorized, nil)
		return
	}

	userInformationRes, err := v.Controller.GetUser(tokenString)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	user, err := v.Controller.GetUserModelById(userInformationRes.ID.String())
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	// Stripe customer is bound to the user's personal org (Plan 5).
	org, err := v.Controller.DB.GetPersonalOrgByUserID(user.ID.String())
	if err != nil || org == nil || org.StripeCustomerID == "" {
		slog.Error("no Stripe customer for user's personal org", "error", err, "user_id", user.ID)
		utils.JSONErrorCode(w, constants.ErrInternal, http.StatusInternalServerError, nil)
		return
	}

	params := stripe.BillingPortalSessionParams{
		Configuration: stripe.String(v.Controller.SecretManager.GetSecret("STRIPE_BILLING_PORTAL_CONFIG_ID")),
		Customer:      stripe.String(org.StripeCustomerID),
	}

	s, err := session.New(&params)
	if err != nil {
		slog.Error("failed to create billing portal session", "error", err)
		http.Error(w, "Failed to create checkout session. Please try again.", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, s.URL, http.StatusSeeOther)
}
