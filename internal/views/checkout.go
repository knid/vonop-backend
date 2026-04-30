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
	ID      string
	Secret  string
	Name    string
	Credits int
	Price   int // USD dollars
	Popular bool
}

func (v *HttpViews) getCreditPacks(userID string) []creditPack {
	rc := v.Controller.RemoteConfig
	c100 := rc.GetInt(remoteconfig.CreditPack100Credits, userID)
	p100 := rc.GetInt(remoteconfig.CreditPack100Price, userID)
	c200 := rc.GetInt(remoteconfig.CreditPack200Credits, userID)
	p200 := rc.GetInt(remoteconfig.CreditPack200Price, userID)
	c500 := rc.GetInt(remoteconfig.CreditPack500Credits, userID)
	p500 := rc.GetInt(remoteconfig.CreditPack500Price, userID)
	c1000 := rc.GetInt(remoteconfig.CreditPack1000Credits, userID)
	p1000 := rc.GetInt(remoteconfig.CreditPack1000Price, userID)
	c3000 := rc.GetInt(remoteconfig.CreditPack3000Credits, userID)
	p3000 := rc.GetInt(remoteconfig.CreditPack3000Price, userID)
	// Keep the 100 pack first — packageSavingsPercent relies on the smallest
	// pack being the "base rate" for the savings calculation on larger packs.
	// 1000 is marked popular: it lands near the sweet spot of the pricing
	// curve (30% savings) and is our expected top seller.
	return []creditPack{
		{ID: "credits_100", Secret: "STRIPE_CREDITS_100_PRICE_ID", Name: fmt.Sprintf("%d AI Credits", c100), Credits: c100, Price: p100},
		{ID: "credits_200", Secret: "STRIPE_CREDITS_200_PRICE_ID", Name: fmt.Sprintf("%d AI Credits", c200), Credits: c200, Price: p200},
		{ID: "credits_500", Secret: "STRIPE_CREDITS_500_PRICE_ID", Name: fmt.Sprintf("%d AI Credits", c500), Credits: c500, Price: p500},
		{ID: "credits_1000", Secret: "STRIPE_CREDITS_1000_PRICE_ID", Name: fmt.Sprintf("%d AI Credits", c1000), Credits: c1000, Price: p1000, Popular: true},
		{ID: "credits_3000", Secret: "STRIPE_CREDITS_3000_PRICE_ID", Name: fmt.Sprintf("%d AI Credits", c3000), Credits: c3000, Price: p3000},
	}
}

// packageSavingsPercent returns the percent discount of `pack` relative to
// the reference pack's per-credit rate, rounded to the nearest integer.
// Returns 0 for packs at or below the reference rate, and for the reference
// pack itself — "save 0%" is noise, not a feature. Used by GetCreditPackages
// to populate the savings_percent badge on larger packs.
func packageSavingsPercent(pack, reference creditPack) int {
	if reference.Credits <= 0 || reference.Price <= 0 || pack.Credits <= 0 {
		return 0
	}
	// Work in integer cents to avoid floating-point drift on whole-dollar
	// prices. nominal = price-per-credit * credits; savings = nominal - actual.
	nominalCents := reference.Price * 100 * pack.Credits / reference.Credits
	actualCents := pack.Price * 100
	if nominalCents <= actualCents {
		return 0
	}
	savings := nominalCents - actualCents
	// Round half-up.
	return (savings*100 + nominalCents/2) / nominalCents
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
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	slog.Info("checkout: user authenticated", "user_id", user.ID.String())

	// Stripe customer is bound to the user's personal org (Plan 5).
	org, err := v.Controller.DB.GetPersonalOrgByUserID(user.ID.String())
	if err != nil || org == nil || org.StripeCustomerID == "" {
		slog.Error("no Stripe customer for user's personal org", "error", err, "user_id", user.ID)
		utils.JSONErrorCode(w, constants.ErrInternal, http.StatusInternalServerError, nil)
		return
	}

	priceID := r.URL.Query().Get("price")
	if priceID == "" {
		utils.JSONErrorCode(w, constants.ErrValidationRequired, http.StatusBadRequest, map[string]any{"field": "price"})
		return
	}

	// Validate price ID against known subscription plans
	allSubs, err := v.Controller.DB.GetSubscriptions()
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrInternal, http.StatusInternalServerError, nil)
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
		utils.JSONErrorCode(w, constants.ErrValidationInvalidFormat, http.StatusBadRequest, map[string]any{"field": "price"})
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
		Customer:            stripe.String(org.StripeCustomerID),
		LineItems:           lineItems,
		Mode:                &mode,
		SuccessURL:          &successURL,
		CancelURL:           &cancelURL,
		AllowPromotionCodes: stripe.Bool(true),
	}

	s, err := session.New(params)
	if err != nil {
		slog.Error("stripe checkout session creation failed", "error", err)
		utils.JSONErrorCode(w, constants.ErrBillingCheckoutFailed, http.StatusInternalServerError, nil)
		return
	}

	v.Controller.TrackUserEvent(user, "checkout_started", posthog.NewProperties().
		Set("price_id", priceID).
		Set("trial_eligible", !user.TrialUsed))

	go v.Controller.EventService.SendEvent(user, constants.EVENT_CHECKOUT_STARTED, map[string]any{
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

	packs := v.getCreditPacks(remoteconfig.Global)
	// Use the first pack (the smallest, by convention of getCreditPacks) as
	// the savings reference. If that secret isn't configured we still need
	// a reference rate, so we fall back to the first listed pack regardless
	// of whether its Stripe secret is set.
	var reference creditPack
	if len(packs) > 0 {
		reference = packs[0]
	}

	var packages []packageResponse
	for _, p := range packs {
		// Only include packs that have a configured Stripe price
		if v.Controller.SecretManager.GetSecret(p.Secret) == "" {
			continue
		}
		savings := 0
		if p.ID != reference.ID {
			savings = packageSavingsPercent(p, reference)
		}
		packages = append(packages, packageResponse{
			ID:             p.ID,
			Name:           p.Name,
			Credits:        p.Credits,
			Price:          p.Price,
			Popular:        p.Popular,
			SavingsPercent: savings,
		})
	}

	utils.JSONResponse(w, packages, http.StatusOK)
}

// PurchaseCredits creates a one-time Stripe Checkout Session for buying AI credit packs.
func (v *HttpViews) PurchaseCredits(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	if requireActiveSubscription(w, v.Controller, user) {
		return
	}

	// Stripe customer is bound to the user's personal org (Plan 5).
	org, err := v.Controller.DB.GetPersonalOrgByUserID(user.ID.String())
	if err != nil || org == nil || org.StripeCustomerID == "" {
		slog.Error("no Stripe customer for user's personal org", "error", err, "user_id", user.ID)
		utils.JSONErrorCode(w, constants.ErrInternal, http.StatusInternalServerError, nil)
		return
	}

	var req struct {
		PackageID string `json:"package_id"`
	}
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorCode(w, constants.ErrValidationBody, http.StatusBadRequest, nil)
		return
	}

	pack, priceID := v.creditPackByID(req.PackageID, v.getCreditPacks(user.ID.String()))
	if pack == nil {
		utils.JSONErrorCode(w, constants.ErrValidationInvalidFormat, http.StatusBadRequest, map[string]any{"field": "package_id"})
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
		Customer: stripe.String(org.StripeCustomerID),
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
		utils.JSONErrorCode(w, constants.ErrBillingCheckoutFailed, http.StatusInternalServerError, nil)
		return
	}

	v.Controller.TrackUserEvent(user, "credit_purchase_started", posthog.NewProperties().
		Set("package_id", req.PackageID).
		Set("pack_size", pack.Credits).
		Set("price_id", priceID))

	go v.Controller.EventService.SendEvent(user, constants.EVENT_CREDIT_PURCHASE_STARTED, map[string]any{
		"package_id": req.PackageID,
		"pack_size":  pack.Credits,
		"price_id":   priceID,
	})

	utils.JSONResponse(w, map[string]string{"url": s.URL}, http.StatusCreated)
}
