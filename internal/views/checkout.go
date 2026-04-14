package views

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/services/remoteconfig"
	"github.com/knid/wemob/internal/utils"
	"github.com/posthog/posthog-go"
	"github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/checkout/session"
)

// creditPack defines a credit package with Stripe secret key, display name, credit amount, and price.
type creditPack struct {
	ID       string
	Secret   string
	Name     string
	Credits  int
	Price    int // USD dollars
	Popular  bool
}

func (v *HttpViews) getCreditPacks(userID string) []creditPack {
	rc := v.Controller.RemoteConfig
	c100 := rc.GetInt(remoteconfig.CreditPack100Credits, userID)
	p100 := rc.GetInt(remoteconfig.CreditPack100Price, userID)
	c200 := rc.GetInt(remoteconfig.CreditPack200Credits, userID)
	p200 := rc.GetInt(remoteconfig.CreditPack200Price, userID)
	return []creditPack{
		{ID: "credits_100", Secret: "STRIPE_CREDITS_100_PRICE_ID", Name: fmt.Sprintf("%d AI Credits", c100), Credits: c100, Price: p100},
		{ID: "credits_200", Secret: "STRIPE_CREDITS_200_PRICE_ID", Name: fmt.Sprintf("%d AI Credits", c200), Credits: c200, Price: p200, Popular: true},
	}
}

// creditPackByID returns the pack definition and its Stripe price ID, or nil if not found.
func (v *HttpViews) creditPackByID(packageID string, packs []creditPack) (*creditPack, string) {
	for _, p := range packs {
		if p.ID == packageID {
			priceID := v.Controller.SecretManager.GetSecret(p.Secret)
			if priceID == "" {
				return nil, ""
			}
			return &p, priceID
		}
	}
	return nil, ""
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

	go v.Controller.CRMService.SendEvent(user, constants.BREVO_EVENT_CHECKOUT_STARTED, map[string]any{
		"price_id": priceID,
	})

	utils.JSONResponse(w, map[string]string{"url": s.URL}, http.StatusCreated)
}

// GetCreditPackages returns the available credit packs for purchase.
func (v *HttpViews) GetCreditPackages(w http.ResponseWriter, r *http.Request) {
	type packageResponse struct {
		ID             string `json:"id"`
		Name           string `json:"name"`
		Credits        int    `json:"credits"`
		Price          int    `json:"price"`
		Popular        bool   `json:"popular,omitempty"`
		SavingsPercent int    `json:"savings_percent,omitempty"`
	}

	var packages []packageResponse
	for _, p := range v.getCreditPacks(remoteconfig.Global) {
		// Only include packs that have a configured Stripe price
		if v.Controller.SecretManager.GetSecret(p.Secret) != "" {
			packages = append(packages, packageResponse{
				ID:      p.ID,
				Name:    p.Name,
				Credits: p.Credits,
				Price:   p.Price,
				Popular: p.Popular,
			})
		}
	}

	utils.JSONResponse(w, packages, http.StatusOK)
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
		PackageID string `json:"package_id"`
	}
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, "invalid request body", http.StatusBadRequest)
		return
	}

	pack, priceID := v.creditPackByID(req.PackageID, v.getCreditPacks(user.ID.String()))
	if pack == nil {
		utils.JSONErrorResponse(w, "invalid package_id", http.StatusBadRequest)
		return
	}

	dashboardURL := v.Controller.SecretManager.GetSecret("FRONTEND_URL")
	if dashboardURL == "" {
		dashboardURL = "https://dashboard.wemob.io"
	}
	successURL := dashboardURL + "/subscription?credits=purchased"
	cancelURL := dashboardURL + "/subscription"

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
			"pack_size": fmt.Sprintf("%d", pack.Credits),
		},
	}

	s, err := session.New(params)
	if err != nil {
		slog.Error("stripe credit purchase checkout failed", "error", err)
		utils.JSONErrorResponse(w, "Failed to create checkout session. Please try again.", http.StatusInternalServerError)
		return
	}

	v.Controller.TrackUserEvent(user, "credit_purchase_started", posthog.NewProperties().
		Set("package_id", req.PackageID).
		Set("pack_size", pack.Credits).
		Set("price_id", priceID))

	go v.Controller.CRMService.SendEvent(user, constants.BREVO_EVENT_CREDIT_PURCHASE_STARTED, map[string]any{
		"package_id": req.PackageID,
		"pack_size":  pack.Credits,
		"price_id":   priceID,
	})

	utils.JSONResponse(w, map[string]string{"url": s.URL}, http.StatusCreated)
}
