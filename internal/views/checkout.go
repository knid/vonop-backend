package views

import (
	"log/slog"
	"net/http"

	"github.com/knid/wemob/internal/utils"
	"github.com/posthog/posthog-go"
	"github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/checkout/session"
)

func (v *HttpViews) StripeCheckout(w http.ResponseWriter, r *http.Request) {
	// TODO: Check user location, if user in GB return error with like this text: "We can't provide service for United Kingdom currently. Please try again later.". You can make it more professional.
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	slog.Info("checkout: user authenticated", "user_id", user.ID.String())

	priceID := r.URL.Query().Get("price")
	if priceID == "" {
		utils.JSONErrorResponse(w, "price parameter is required", http.StatusBadRequest)
		return
	}

	// Validate price ID against known subscription plans
	allSubs, err := v.Controller.DB.GetSubscriptions()
	if err != nil {
		utils.JSONErrorResponse(w, "failed to validate price", http.StatusInternalServerError)
		return
	}
	validPrice := false
	for _, s := range *allSubs {
		if s.PriceID == priceID || s.AnnualPriceID == priceID {
			validPrice = true
			break
		}
	}
	if !validPrice {
		utils.JSONErrorResponse(w, "invalid price ID", http.StatusBadRequest)
		return
	}

	mode := string(stripe.CheckoutSessionModeSubscription)

	dashboardURL := v.Controller.SecretManager.GetSecret("FRONTEND_URL")
	if dashboardURL == "" {
		dashboardURL = "https://dashboard.wemob.io"
	}
	successURL := dashboardURL + "/success?session_id={CHECKOUT_SESSION_ID}"
	cancelURL := dashboardURL + "/subscription"

	lineItems := []*stripe.CheckoutSessionLineItemParams{
		{
			Price:    &priceID, // The Price ID from your Stripe dashboard
			Quantity: stripe.Int64(1),
		},
	}

	params := &stripe.CheckoutSessionParams{
		Customer:            stripe.String(user.CustomerID),
		LineItems:           lineItems,
		Mode:                &mode,
		SuccessURL:          &successURL,
		CancelURL:           &cancelURL,
		AllowPromotionCodes: stripe.Bool(true),
	}

	// Create the actual Stripe Checkout Session!
	s, err := session.New(params)
	if err != nil {
		slog.Error("stripe checkout session creation failed", "error", err)
		http.Error(w, "Failed to create checkout session. Please try again.", http.StatusInternalServerError)
		return
	}

	v.Controller.TrackUserEvent(user, "checkout_started", posthog.NewProperties().
		Set("price_id", priceID).
		Set("trial_eligible", !user.TrialUsed))

	utils.JSONResponse(w, map[string]string{"url": s.URL}, http.StatusCreated)
	// Redirect the user's browser to Stripe's hosted checkout page!
	// http.Redirect(w, r, s.URL, http.StatusSeeOther)
}
