package agent

import (
	"context"
	"log/slog"
	"strings"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/secret"
	"github.com/knid/wemob/internal/utils"
)

// ComplexityLevel indicates whether a user request is simple (cheap model) or
// complex (full model).
type ComplexityLevel string

const (
	ComplexitySimple  ComplexityLevel = "simple"
	ComplexityComplex ComplexityLevel = "complex"
)

// classifyComplexity routes requests through keyword heuristics first, then
// falls back to a Haiku LLM call only when inconclusive.
//
// Rules:
//   - Disabled unless WEBSITE_BUILDER_CLASSIFY_COMPLEXITY secret is "true".
//   - Draft projects are always complex (initial generation needs full power).
//   - Obvious simple edits (text, color, cosmetic) → simple.
//   - Obvious complex edits (auth, payments, backend, features) → complex.
//   - Inconclusive prompts → Haiku LLM classifier.
//   - On classifier failure → complex (safe fallback).
func classifyComplexity(
	ctx context.Context,
	sm secret.SecretManager,
	project *models.AppProject,
	message string,
	provider *utils.LLMProvider,
) ComplexityLevel {
	if sm.GetSecret("WEBSITE_BUILDER_CLASSIFY_COMPLEXITY") != "true" {
		return ComplexityComplex
	}

	if project != nil && project.Status == "draft" {
		return ComplexityComplex
	}

	lower := strings.ToLower(message)

	if looksLikeSimpleEdit(lower) {
		return ComplexitySimple
	}

	if looksLikeComplexEdit(lower) {
		return ComplexityComplex
	}

	result, err := classifyWithLLM(ctx, message, provider)
	if err != nil {
		slog.Warn("complexity classifier failed, defaulting to complex", "error", err)
		return ComplexityComplex
	}
	if result == ComplexitySimple {
		return ComplexitySimple
	}
	return ComplexityComplex
}

func looksLikeSimpleEdit(lower string) bool {
	simplePatterns := []string{
		"change color", "change the color", "update color", "change text color",
		"make it ", "change font", "update font", "font size", "text size",
		"change the text", "update the text", "rewrite this", "rephrase",
		"fix typo", "fix spelling", "correct typo", "update copy",
		"change button text", "update button", "change label", "update label",
		"change title", "update title", "change heading", "update heading",
		"lighter", "darker", "bolder", "align ", "center ", "left align",
		"margin", "padding", "spacing", "gap", "rounded", "shadow",
		"move this", "swap ", "replace image", "update image", "change image",
		"delete this", "remove this", "hide this", "show this",
		"add emoji", "add icon", "change icon", "update logo",
	}

	for _, p := range simplePatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}

	if len(strings.Fields(lower)) <= 3 {
		casual := []string{"hi", "hello", "hey", "thanks", "thank you", "ok", "okay", "great", "nice", "cool"}
		for _, c := range casual {
			if lower == c || strings.HasPrefix(lower, c+" ") {
				return true
			}
		}
	}

	return false
}

func looksLikeComplexEdit(lower string) bool {
	complexPatterns := []string{
		"auth", "authentication", "login", "sign in", "sign up", "register",
		"password", "oauth", "sso", "jwt", "session", "verify email",
		"payment", "stripe", "checkout", "cart", "subscription", "billing",
		"database", "api", "backend", "server", "endpoint", "webhook",
		"push notification", "deep link", "biometric", "in-app purchase", "iap",
		"integrate", "integration", "connect to", "sync with", "import from",
		"add feature", "new feature", "build a", "create a", "implement",
		"mobile app", "ios", "android", "react native", "expo",
		"export", "deploy", "build ", "generate ", "migration",
		"redux", "context", "state management", "zustand",
		"form validation", "search", "filter", "sort", "pagination",
		"upload", "download", "file", "image processing", "resize",
		"animation", "transition", "carousel", "slider", "modal", "drawer",
		"table", "chart", "graph", "dashboard", "analytics",
		"user profile", "settings page", "admin panel", "role", "permission",
		"email", "sms", "notification", "schedule", "cron", "timer",
		"real-time", "websocket", "socket", "live update", "collaborative",
		"ai ", "gpt", "llm", "openai", "claude", "gemini", "model",
	}

	for _, p := range complexPatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}

	return false
}

func classifyWithLLM(ctx context.Context, message string, provider *utils.LLMProvider) (ComplexityLevel, error) {
	if provider == nil || provider.APIKey == "" {
		return ComplexityComplex, nil
	}

	model := "anthropic/claude-haiku-4-5-20251001"
	if provider.IsAnthropic() {
		model = "claude-haiku-4-5-20251001"
	}

	system := `Classify the user's request complexity as either "simple" or "complex".
Reply with ONLY the word "simple" or "complex", nothing else.

Definitions:
- simple: cosmetic/text changes (colors, fonts, spacing, copy edits, single-file fixes, moving elements, hiding/showing). No new features, no backend, no auth, no integrations.
- complex: anything involving new features, authentication, payments, database, backend APIs, integrations, multi-file architectural changes, mobile-native features, state management, forms with validation, real-time, AI features, file uploads, charts/dashboards.

Examples:
- "make the button blue" → simple
- "change the heading text" → simple
- "add more padding" → simple
- "fix this typo" → simple
- "hi" → simple
- "add login with Google" → complex
- "connect Stripe payments" → complex
- "build a todo app" → complex
- "add a dashboard with charts" → complex
- "implement push notifications" → complex`

	result, err := utils.GetTextFromLLM(ctx, model, system, message, provider, 256)
	if err != nil {
		return "", err
	}

	result = strings.TrimSpace(strings.ToLower(result))
	if result == "simple" {
		return ComplexitySimple, nil
	}
	return ComplexityComplex, nil
}
