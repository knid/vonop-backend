package utils

import "net/http"

// LLMProvider holds the base URL and API key for a specific LLM provider.
// All supported providers expose an OpenAI-compatible /chat/completions endpoint.
type LLMProvider struct {
	Name             string // "openrouter", "anthropic", "google", "moonshot", "zai"
	BaseURL          string
	APIKey           string
	DefaultReasoning *ReasoningConfig // applied when no explicit Reasoning/Thinking is set
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
func (p *LLMProvider) WebSearchTool() *Tool {
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
	switch p.Name {
	case "anthropic":
		return "claude-sonnet-4"
	case "moonshot":
		return "kimi-k2.5"
	case "google":
		return "gemini-2.5-flash"
	case "zai":
		return "glm-4.7"
	default:
		return "anthropic/claude-sonnet-4" // OpenRouter format
	}
}

// SupportsVision returns true if the provider supports multimodal (image) input.
func (p *LLMProvider) SupportsVision() bool {
	return !p.IsZAI()
}

// setLLMHeaders applies auth and provider-specific headers to an HTTP request.
func setLLMHeaders(req *http.Request, provider *LLMProvider) {
	req.Header.Set("Content-Type", "application/json")
	if provider.IsAnthropic() {
		req.Header.Set("x-api-key", provider.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")
		req.Header.Set("anthropic-beta", "interleaved-thinking-2025-05-14")
		return
	}
	req.Header.Set("Authorization", "Bearer "+provider.APIKey)
	if provider.IsOpenRouter() {
		req.Header.Set("HTTP-Referer", "https://wemob.io")
		req.Header.Set("X-Title", "wemob-app-builder")
	}
}
