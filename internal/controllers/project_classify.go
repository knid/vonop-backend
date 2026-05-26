package controllers

import (
	"context"
	"log/slog"
	"strings"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/utils"
)

// classifyProjectType asks a fast LLM to label the user prompt as either
// "mobile_app_native", "ecommerce", or "app" (with "app" as the default).
// The mobile keyword fast-path runs first, then the ecommerce keyword fast-path,
// then the LLM distinguishes "ecommerce" vs "app" for remaining prompts.
//
// The optional `user` is purely for PostHog attribution — the call runs before
// CreateAppProject persists anything, so credit deduction is handled by the
// caller after the project lands. Pass nil to skip the trace.
func (c *Controller) classifyProjectType(ctx context.Context, prompt string, user *models.User) string {
	// Fast path: mobile keyword classifier (added in P0.1 T15)
	if looksLikeMobileNative(prompt) {
		return models.ProjectTypeMobileAppNative
	}

	// Second fast path: keyword-based ecommerce detection. Runs before the LLM
	// so we don't burn a model call on obvious store prompts, and again after
	// the LLM as a safety net when the cheap classifier misses implicit language.
	if looksLikeEcommerce(prompt) {
		return models.ProjectTypeEcommerce
	}

	// (Existing LLM-based classifier code follows — DO NOT remove it. The mobile
	// fast-path runs first, but the LLM still distinguishes "ecommerce" vs "app"
	// for non-mobile prompts.)
	provider := c.llmProviderForUser(user)
	// Use a fast/cheap model for classification
	model := provider.FastModel()

	tracker := utils.NewTokenTracker()
	if user != nil {
		tracker.UserID = user.ID.String()
		if orgID, orgErr := c.RequireOrgIDForUser(user, nil); orgErr == nil {
			tracker.OrganizationID = orgID.String()
		}
	}
	ctx = utils.WithTokenTracker(ctx, tracker)

	system := `Classify the user's project description into exactly one category.
Reply with ONLY the category name, nothing else.

Categories:
- mobile_app_native: Native mobile app (iOS, Android, React Native, Expo). Push notifications, biometric, IAP, deep links, offline.
- ecommerce: Online store, shop, marketplace with products, cart, checkout, payments
- app: Any other web application (blog, dashboard, portfolio, SaaS, etc.)

Examples:
- "build me an online clothing store" → ecommerce
- "kozmetik markamız için e ticaret sitesi" → ecommerce
- "el kremi satmak istiyorum" → ecommerce
- "ürün satışı yapacağım" → ecommerce
- "create a project management dashboard" → app
- "boutique de vêtements en ligne" → ecommerce
- "habit tracker mobile app with push notifications" → mobile_app_native
- "iOS fitness app with HealthKit" → mobile_app_native`

	result, err := utils.GetTextFromLLM(ctx, model, system, prompt, provider, 256)
	if user != nil {
		c.RecordTokenUsage(user, "project_classify", model, provider.Name, "", "standard", tracker)
	}
	if err != nil {
		slog.Warn("LLM classification failed, falling back to keyword detection", "error", err)
		if looksLikeEcommerce(prompt) {
			return models.ProjectTypeEcommerce
		}
		return models.ProjectTypeApp
	}
	result = strings.TrimSpace(strings.ToLower(result))
	if result == "mobile_app_native" || result == "mobile" {
		return models.ProjectTypeMobileAppNative
	}
	if result == "ecommerce" || result == "e-commerce" {
		return models.ProjectTypeEcommerce
	}
	// Safety net: if the LLM said "app" but keywords strongly suggest ecommerce,
	// trust the keywords. This catches cases where the cheap/fast model misses
	// implicit or non-English store language that the dictionary covers.
	if looksLikeEcommerce(prompt) {
		return models.ProjectTypeEcommerce
	}
	return models.ProjectTypeApp
}

// looksLikeEcommerce returns true if the prompt strongly suggests an
// e-commerce app based on a small keyword/phrase dictionary. Runs as a
// pre-LLM fast path (saves a model call on obvious store prompts) and as
// a post-LLM safety net when the cheap classifier mislabels implicit language.
func looksLikeEcommerce(prompt string) bool {
	lower := strings.ToLower(prompt)

	// Strong signals — any one suffices
	strongSignals := []string{
		"e-commerce", "ecommerce", "online store", "online shop",
		"shopping cart", "storefront", "product catalog",
		"product listing", "product page", "checkout page",
		"add to cart", "buy now", "shopping platform",
		// Turkish strong signals
		"e ticaret", "eticaret", "online satış", "online magaza",
		"mağaza açmak", "ürün satmak", "satış sitesi",
	}
	for _, s := range strongSignals {
		if strings.Contains(lower, s) {
			return true
		}
	}

	// Combination signals — "store"/"shop" + commerce indicator
	storeWords := []string{"store", "shop", "marketplace", "boutique", "mağaza", "magaza"}
	commerceIndicators := []string{
		"product", "cart", "payment", "price", "order",
		"checkout", "inventory", "catalog", "shipping",
		"purchase", "sell", "buy",
		// Turkish commerce indicators
		"ürün", "urun", "satmak", "satış", "satis",
		"sipariş", "siparis", "fiyat", "ödeme", "odeme",
		"sepet", "kargo", "alışveriş", "alisveris",
	}
	for _, sw := range storeWords {
		if strings.Contains(lower, sw) {
			for _, ci := range commerceIndicators {
				if strings.Contains(lower, ci) {
					return true
				}
			}
		}
	}

	return false
}

// looksLikeMobileNative returns true if the prompt strongly suggests a
// native mobile app (rather than a web app or web-based ecommerce). Used as
// the fast-path classifier before falling back to the LLM.
func looksLikeMobileNative(prompt string) bool {
	lower := strings.ToLower(prompt)

	strongSignals := []string{
		"mobile app", "ios app", "android app", "native app", "native ios", "native android",
		"react native", "expo app", "expo project",
		"mobile-first", "mobile first",
		"push notification", "deep link", "biometric login", "biometric auth",
		"in-app purchase", "in app purchase", "iap",
		"app store", "play store", "google play",
	}
	for _, s := range strongSignals {
		if strings.Contains(lower, s) {
			return true
		}
	}

	// Combination signals: "mobile" + a native indicator
	if strings.Contains(lower, "mobile") {
		nativeIndicators := []string{"haptic", "biometric", "offline mode", "background sync", "swipe gesture"}
		for _, ni := range nativeIndicators {
			if strings.Contains(lower, ni) {
				return true
			}
		}
	}

	return false
}
