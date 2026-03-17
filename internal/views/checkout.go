package views

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/knid/wemob/internal/utils"
	"github.com/posthog/posthog-go"
	"github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/checkout/session"
)

// creditPackPriceID returns the Stripe Price ID for a given credit pack size,
// or empty string if the pack is not configured.
func (v *HttpViews) creditPackPriceID(packSize int) string {
	switch packSize {
	case 100:
		return v.Controller.SecretManager.GetSecret("STRIPE_CREDITS_100_PRICE_ID")
	case 200:
		return v.Controller.SecretManager.GetSecret("STRIPE_CREDITS_200_PRICE_ID")
	default:
		return ""
	}
}

func (v *HttpViews) StripeCheckout(w http.ResponseWriter, r *http.Request) {
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
			Price:    &priceID,
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

	s, err := session.New(params)
	if err != nil {
		slog.Error("stripe checkout session creation failed", "error", err)
		utils.JSONErrorResponse(w, "Failed to create checkout session. Please try again.", http.StatusInternalServerError)
		return
	}

	v.Controller.TrackUserEvent(user, "checkout_started", posthog.NewProperties().
		Set("price_id", priceID).
		Set("trial_eligible", !user.TrialUsed))

	utils.JSONResponse(w, map[string]string{"url": s.URL}, http.StatusCreated)
}

// PurchaseCredits creates a one-time Stripe Checkout Session for buying AI credit packs.
func (v *HttpViews) PurchaseCredits(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	if requireActiveSubscription(w, user) {
		return
	}

	var req struct {
		PackSize int `json:"pack_size"`
	}
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, "invalid request body", http.StatusBadRequest)
		return
	}

	priceID := v.creditPackPriceID(req.PackSize)
	if priceID == "" {
		utils.JSONErrorResponse(w, "pack_size must be 100 or 200", http.StatusBadRequest)
		return
	}

	dashboardURL := v.Controller.SecretManager.GetSecret("FRONTEND_URL")
	if dashboardURL == "" {
		dashboardURL = "https://dashboard.wemob.io"
	}
	successURL := dashboardURL + "/billing?credits=purchased"
	cancelURL := dashboardURL + "/billing"

	mode := string(stripe.CheckoutSessionModePayment)
	params := &stripe.CheckoutSessionParams{
		Customer: stripe.String(user.CustomerID),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				Price:    stripe.String(priceID),
				Quantity: stripe.Int64(1),
			},
		},
		Mode:       &mode,
		SuccessURL: &successURL,
		CancelURL:  &cancelURL,
		Metadata: map[string]string{
			"type":      "credit_purchase",
			"user_id":   user.ID.String(),
			"pack_size": fmt.Sprintf("%d", req.PackSize),
		},
	}

	s, err := session.New(params)
	if err != nil {
		slog.Error("stripe credit purchase checkout failed", "error", err)
		utils.JSONErrorResponse(w, "Failed to create checkout session. Please try again.", http.StatusInternalServerError)
		return
	}

	v.Controller.TrackUserEvent(user, "credit_purchase_started", posthog.NewProperties().
		Set("pack_size", req.PackSize).
		Set("price_id", priceID))

	utils.JSONResponse(w, map[string]string{"url": s.URL}, http.StatusCreated)
}
