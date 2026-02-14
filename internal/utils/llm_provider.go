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

// UsesThinkingFormat returns true for providers that use the ZAI-style
// "thinking" config (type: "enabled"/"disabled") instead of OpenRouter's
// "reasoning" config (effort: "high"/"medium"/etc).
func (p *LLMProvider) UsesThinkingFormat() bool {
	return p.IsZAI() || p.IsMoonshot()
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

// SupportsVision returns true if the provider supports multimodal (image) input.
func (p *LLMProvider) SupportsVision() bool {
	return !p.IsZAI()
}

// setLLMHeaders applies auth and provider-specific headers to an HTTP request.
func setLLMHeaders(req *http.Request, provider *LLMProvider) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+provider.APIKey)
	if provider.IsOpenRouter() {
		req.Header.Set("HTTP-Referer", "https://wemob.io")
		req.Header.Set("X-Title", "wemob-app-builder")
	}
}
