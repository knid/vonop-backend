package utils

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/knid/wemob/internal/constants"
)

// anthropicBetaFeatures lists the Anthropic beta features to enable.
var anthropicBetaFeatures = "interleaved-thinking-2025-05-14,web-search-2025-03-05"

// bareModel strips a provider prefix like "anthropic/" from a model ID,
// giving the bare name that Anthropic's native API expects.
func bareModel(id string) string {
	if i := strings.Index(id, "/"); i >= 0 {
		return id[i+1:]
	}
	return id
}

// LLMProvider holds the base URL and API key for a specific LLM provider.
// All supported providers expose an OpenAI-compatible /chat/completions endpoint.
type LLMProvider struct {
	Name              string // "openrouter", "anthropic", "google", "moonshot", "zai"
	BaseURL           string
	APIKey            string
	DefaultReasoning  *ReasoningConfig // applied when no explicit Reasoning/Thinking is set
	FastModelOverride string           // if set, overrides the default FastModel()

	// Fallback is an optional secondary provider used when the primary returns
	// an overloaded error (Anthropic 529 / overloaded_error). The fallback is
	// attempted once per call before the request fails. After the swap, normal
	// rate-limit retries continue against the fallback provider.
	Fallback      *LLMProvider
	FallbackModel string // model ID to use with Fallback (provider-specific format)

	// ConfiguredModel is set when the provider was resolved per-user from a
	// PostHog feature flag (e.g. rc-builder-provider-model-starter). When set,
	// it overrides the global WEBSITE_BUILDER_LLM_MODEL secret.
	ConfiguredModel string
}

// Endpoint returns the full chat completions URL for this provider.
func (p *LLMProvider) Endpoint() string {
	if p.IsAnthropic() {
		return p.BaseURL + "/messages"
	}
	return p.BaseURL + "/chat/completions"
}

// IsOpenRouter returns true when using the OpenRouter aggregator.
func (p *LLMProvider) IsOpenRouter() bool {
	return p.Name == "openrouter"
}

// IsZAI returns true when using the ZAI (GLM-4.7) provider.
func (p *LLMProvider) IsZAI() bool {
	return p.Name == "zai"
}

// IsMoonshot returns true when using the Moonshot (Kimi) provider.
func (p *LLMProvider) IsMoonshot() bool {
	return p.Name == "moonshot"
}

// IsAnthropic returns true when using the native Anthropic Messages API.
func (p *LLMProvider) IsAnthropic() bool {
	return p.Name == "anthropic"
}

// UsesThinkingFormat returns true for providers that use a non-OpenRouter
// thinking/reasoning format. This includes ZAI, Moonshot, and Anthropic.
func (p *LLMProvider) UsesThinkingFormat() bool {
	return p.IsZAI() || p.IsMoonshot() || p.IsAnthropic()
}

// WebSearchTool returns the builtin web search tool for providers that support it, or nil.
// The model parameter is used to disable web_search for Moonshot K2.6 specifically,
// since K2.6's thinking mode is incompatible with the $web_search builtin.
func (p *LLMProvider) WebSearchTool(model string) *Tool {
	if p.IsAnthropic() {
		return &Tool{
			Type: "server_tool",
			ServerTool: &ServerTool{
				Type:    "web_search_20250305",
				Name:    "web_search",
				MaxUses: 5,
			},
		}
	}
	// Moonshot K2.6: $web_search is temporarily incompatible with thinking mode.
	// Only disable for K2.6; K2.5 and earlier models are unaffected.
	if p.IsMoonshot() && strings.Contains(model, "k2.6") {
		return nil
	}
	if p.IsMoonshot() {
		return &Tool{
			Type:     "builtin_function",
			Function: ToolFunction{Name: "$web_search"},
		}
	}
	return nil
}

// DefaultModel returns the default model name for this provider.
// Used as fallback when WEBSITE_BUILDER_LLM_MODEL secret is empty.
func (p *LLMProvider) DefaultModel() string {
	if p == nil {
		return constants.GatewayPreferredChatModel
	}
	switch p.Name {
	case "anthropic":
		return bareModel(constants.GatewayPreferredChatModel)
	case "moonshot":
		return "kimi-k2.6"
	case "google":
		return "gemini-2.5-flash"
	case "zai":
		return "glm-4.7"
	default:
		return constants.GatewayPreferredChatModel // OpenRouter format
	}
}

// FastModel returns a cheaper/faster model for non-creative tasks like fix passes
// and conversation summarization. Falls back to DefaultModel if no fast option exists.
func (p *LLMProvider) FastModel() string {
	if p.FastModelOverride != "" {
		slog.Debug("using fast model override", "model", p.FastModelOverride, "provider", p.Name)
		return p.FastModelOverride
	}
	switch p.Name {
	case "anthropic":
		// TODO keep synced with GatewayDefaultChatModel when Anthropic publishes a new haiku
		return "claude-haiku-4-5-20251001"
	case "google":
		return "gemini-2.5-flash"
	default:
		return "google/gemini-2.5-flash" // OpenRouter format
	}
}

// SupportsVision returns true if the provider supports multimodal (image) input.
func (p *LLMProvider) SupportsVision() bool {
	return !p.IsZAI()
}

// SetLLMHeaders applies auth and provider-specific headers to an HTTP request.
func SetLLMHeaders(req *http.Request, provider *LLMProvider) {
	if provider == nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if provider.IsAnthropic() {
		req.Header.Set("x-api-key", provider.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")
		req.Header.Set("anthropic-beta", anthropicBetaFeatures)
		return
	}
	req.Header.Set("Authorization", "Bearer "+provider.APIKey)
	if provider.IsOpenRouter() {
		req.Header.Set("HTTP-Referer", "https://wemob.io")
		req.Header.Set("X-Title", "wemob-app-builder")
	}
}
