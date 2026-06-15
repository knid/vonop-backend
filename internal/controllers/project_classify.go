package controllers

import (
	"context"
	"log/slog"
	"strings"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/observability"
	"github.com/knid/wemob/internal/utils"
)

// classifyProjectType asks a fast LLM to label the user prompt as either
// "mobile_app_native", "ecommerce", or "app" (with "app" as the default).
// The mobile keyword fast-path runs first (cheap and reliable), then the LLM
// classifies everything else. Keyword fallback only runs if the LLM call fails.
//
// The optional `user` is purely for PostHog attribution — the call runs before
// CreateAppProject persists anything, so credit deduction is handled by the
// caller after the project lands. Pass nil to skip the trace.
func (c *Controller) classifyProjectType(ctx context.Context, prompt string, user *models.User) string {
	// Fast path: mobile keyword classifier — cheap and reliable for native mobile
	// signals (iOS, Android, React Native, Expo). The LLM handles everything else.
	if looksLikeMobileNative(prompt) {
		return models.ProjectTypeMobileAppNative
	}

	provider := c.llmProviderForUser(user)
	// Use a fast/cheap model for classification
	model := provider.FastModel()

	tracker := utils.NewTokenTracker()
	tracker.TrackContent = true // ensure the exact classify prompt + model output are captured in PostHog LLM traces for debugging misclassifications
	if user != nil {
		tracker.UserID = user.ID.String()
		if orgID, orgErr := c.RequireOrgIDForUser(user, nil); orgErr == nil {
			tracker.OrganizationID = orgID.String()
		}
	}
	ctx = utils.WithTokenTracker(ctx, tracker)

	fallbackSystem := `You are a project-type classifier. Read the user's description and reply with EXACTLY ONE category name from the list below. No explanations, no punctuation, no extra text — just the category word.

Categories:
- ecommerce — The user wants to sell products or services online. This includes stores, shops, marketplaces, boutiques, or any site with products, catalog, cart, checkout, payments, orders, inventory, shipping. Even if the description is short, vague, or in another language, classify as ecommerce when the intent is clearly commercial selling.
- app — Any web application that is NOT primarily for selling products: blogs, dashboards, portfolios, SaaS tools, landing pages, booking systems, social networks, content sites.
- mobile_app_native — Native mobile app for iOS or Android (React Native, Expo, Swift, Kotlin). Look for explicit mobile signals: push notifications, biometric auth, in-app purchases, deep links, offline mode, App Store, Play Store.

Classification rules:
1. Default to ecommerce when the user mentions selling, products, shopping, or store-related activity — even if the wording is casual or in Turkish/another language.
2. Only choose app when the description is clearly non-commercial (blog, portfolio, dashboard, tool) and has no product-selling intent.
3. Only choose mobile_app_native when there are explicit mobile-native signals.

Examples:
- "build me an online clothing store" → ecommerce
- "kozmetik markamız için e ticaret sitesi" → ecommerce
- "el kremi satmak istiyorum" → ecommerce
- "ürün satışı yapacağım" → ecommerce
- "yemek satışı sitesi" → ecommerce
- "ayakkabı mağazası" → ecommerce
- "create a project management dashboard" → app
- "boutique de vêtements en ligne" → ecommerce
- "habit tracker mobile app with push notifications" → mobile_app_native
- "iOS fitness app with HealthKit" → mobile_app_native
- "satış yapmak istiyorum" → ecommerce
- "e ticaret" → ecommerce
- "online mağaza" → ecommerce
- "ürünlerimi internetten satacağım" → ecommerce`
	systemPrompt := c.compileLangfuseTextPrompt(ctx, observability.PromptProjectClassify, fallbackSystem, nil)
	tracker.Prompt = observability.PromptLink{Name: systemPrompt.Name, Version: systemPrompt.Version}

	result, err := utils.GetTextFromLLM(ctx, model, systemPrompt.Text, prompt, provider, 256)
	if user != nil {
		c.RecordTokenUsage(user, "project_classify", model, provider.Name, "", "standard", tracker)
	}
	if err != nil {
		slog.Warn("LLM classification failed, falling back to keyword detection", "error", err)
		// Record a proper error event so it appears in PostHog LLM traces (with the real error)
		// even though we will keyword-fallback. This makes repeated classify failures visible.
		if user != nil {
			c.RecordLLMError(user, tracker, "project_classify", model, provider.Name, "", "standard", 0, "classify_failed", err.Error())
		}
		if looksLikeEcommerce(prompt) {
			return models.ProjectTypeEcommerce
		}
		return models.ProjectTypeApp
	}

	clean := sanitizeClassifyResult(result)
	if clean == "mobile_app_native" || clean == "mobile" {
		return models.ProjectTypeMobileAppNative
	}
	if clean == "ecommerce" || clean == "e-commerce" || clean == "store" {
		return models.ProjectTypeEcommerce
	}
	return models.ProjectTypeApp
}

// sanitizeClassifyResult makes the LLM classifier output tolerant to common
// variations (punctuation, newlines, extra words, capitalization) while still
// requiring the core category token. This prevents Haiku/Flash from silently
// returning "app" on valid e-commerce prompts.
func sanitizeClassifyResult(s string) string {
	s = strings.TrimSpace(s)
	if idx := strings.IndexAny(s, "\n\r"); idx >= 0 {
		s = s[:idx]
	}
	s = strings.TrimSpace(s)
	s = strings.Trim(s, `"'.,!? `)
	return strings.ToLower(strings.TrimSpace(s))
}

// looksLikeEcommerce returns true if the prompt strongly suggests an
// e-commerce app based on a small keyword/phrase dictionary.
// Kept only as a fallback when the LLM call itself fails (network error,
// provider outage, etc.) so we still have a reasonable default.
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
		"satmak istiyorum", "satış yapmak istiyorum", "satış yapacağım",
		"ürün satışı", "e-ticaret sitesi", "eticaret sitesi", "satış sitesi",
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

	// Pure Turkish selling intent (very common): "X satmak istiyorum"
	// Covers cases that don't mention an explicit store word.
	if strings.Contains(lower, "satmak istiyorum") ||
		strings.Contains(lower, "satış yapmak istiyorum") ||
		strings.Contains(lower, "satış yapacağım") ||
		strings.Contains(lower, "satışı yapacağım") {
		return true
	}

	// Broader selling intent fallback (covers documented examples + common patterns)
	if strings.Contains(lower, "satış sitesi") ||
		strings.Contains(lower, "satışı sitesi") ||
		strings.Contains(lower, "mağaza aç") ||
		strings.Contains(lower, "mağazası aç") ||
		strings.Contains(lower, "satmak istiyorum") ||
		(strings.Contains(lower, "store") && (strings.Contains(lower, "sell") || strings.Contains(lower, "product") || strings.Contains(lower, "buy") || strings.Contains(lower, "clothing"))) ||
		strings.Contains(lower, "boutique") {
		return true
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
