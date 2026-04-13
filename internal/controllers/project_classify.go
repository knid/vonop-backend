package controllers

import (
	"context"
	"log/slog"
	"strings"

	"github.com/knid/wemob/internal/utils"
)

// classifyProjectType asks a fast LLM to label the user prompt as either
// "ecommerce" or "app" (with "app" as the default). Used by the mobile build
// pipeline to pick the right code template bundle. Falls back to the
// keyword-based looksLikeEcommerce check if the LLM call fails.
func (c *Controller) classifyProjectType(ctx context.Context, prompt string) string {
	provider := c.llmProvider()
	// Use a fast/cheap model for classification
	model := "anthropic/claude-haiku-4-5-20251001"
	if provider.IsAnthropic() {
		model = "claude-haiku-4-5-20251001"
	}

	system := `Classify the user's project description into exactly one category.
Reply with ONLY the category name, nothing else.

Categories:
- ecommerce: Online store, shop, marketplace with products, cart, checkout, payments
- app: Any other web application (blog, dashboard, portfolio, SaaS, etc.)

Examples:
- "build me an online clothing store" → ecommerce
- "kozmetik markamız için e ticaret sitesi" → ecommerce
- "create a project management dashboard" → app
- "boutique de vêtements en ligne" → ecommerce`

	result, err := utils.GetTextFromLLM(ctx, model, system, prompt, provider, 256)
	if err != nil {
		slog.Warn("LLM classification failed, falling back to keyword detection", "error", err)
		if looksLikeEcommerce(prompt) {
			return "ecommerce"
		}
		return ""
	}
	result = strings.TrimSpace(strings.ToLower(result))
	if result == "ecommerce" || result == "e-commerce" {
		return "ecommerce"
	}
	return "app"
}

// looksLikeEcommerce returns true if the prompt strongly suggests an
// e-commerce app based on a small keyword/phrase dictionary. Used as the
// fallback when the LLM classifier fails.
func looksLikeEcommerce(prompt string) bool {
	lower := strings.ToLower(prompt)

	// Strong signals — any one suffices
	strongSignals := []string{
		"e-commerce", "ecommerce", "online store", "online shop",
		"shopping cart", "storefront", "product catalog",
		"product listing", "product page", "checkout page",
		"add to cart", "buy now", "shopping platform",
	}
	for _, s := range strongSignals {
		if strings.Contains(lower, s) {
			return true
		}
	}

	// Combination signals — "store"/"shop" + commerce indicator
	storeWords := []string{"store", "shop", "marketplace", "boutique"}
	commerceIndicators := []string{
		"product", "cart", "payment", "price", "order",
		"checkout", "inventory", "catalog", "shipping",
		"purchase", "sell", "buy",
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
