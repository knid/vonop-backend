package views

import (
	"log"
	"net/http"

	"github.com/knid/wemob/internal/utils"
	"github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/checkout/session"
)

func (v *HttpViews) StripeCheckout(w http.ResponseWriter, r *http.Request) {
	tokenString, err := utils.ExtractTokenFromHeader(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	userInformationRes, err := v.Controller.GetUser(tokenString)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	user, err := v.Controller.GetUserModelById(userInformationRes.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	priceID := r.URL.Query().Get("price")
	mode := string(stripe.CheckoutSessionModeSubscription)

	successURL := "https://dashboard.wemob.io/billing/success?session_id={CHECKOUT_SESSION_ID}"
	cancelURL := "https://example.com/pricing"

	lineItems := []*stripe.CheckoutSessionLineItemParams{
		{
			Price:    &priceID, // The Price ID from your Stripe dashboard
			Quantity: stripe.Int64(1),
		},
	}

	params := &stripe.CheckoutSessionParams{
		Customer:   stripe.String(user.CustomerID),
		LineItems:  lineItems,
		Mode:       &mode,
		SuccessURL: &successURL,
		CancelURL:  &cancelURL,
	}

	if !user.TrialUsed {
		params.SubscriptionData = &stripe.CheckoutSessionSubscriptionDataParams{
			TrialPeriodDays: stripe.Int64(14),
		}
	}
	// Create the actual Stripe Checkout Session!
	s, err := session.New(params)
	if err != nil {
		log.Printf("session.New: %v", err) // Log the detailed error
		http.Error(w, "Failed to create checkout session. Please try again.", http.StatusInternalServerError)
		return
	}

	// Redirect the user's browser to Stripe's hosted checkout page!
	http.Redirect(w, r, s.URL, http.StatusSeeOther)
}
