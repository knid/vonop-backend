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

	// Validate that the price ID belongs to a known subscription plan
	subs, err := v.Controller.DB.GetSubscriptionByPriceID(priceID)
	if err != nil || subs == nil {
		// Also check annual price IDs
		allSubs, subsErr := v.Controller.DB.GetSubscriptions()
		validPrice := false
		if subsErr == nil && allSubs != nil {
			for _, s := range *allSubs {
				if s.AnnualPriceID == priceID {
					validPrice = true
					break
				}
			}
		}
		if !validPrice {
			utils.JSONErrorResponse(w, "invalid price", http.StatusBadRequest)
			return
		}
	}

	mode := string(stripe.CheckoutSessionModeSubscription)

	successURL := "https://dashboard.wemob.io/success?session_id={CHECKOUT_SESSION_ID}"
	cancelURL := "https://dashboard.wemob.io/subscription"

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
