package utils

import "net/http"

// LLMProvider holds the base URL and API key for a specific LLM provider.
// All supported providers expose an OpenAI-compatible /chat/completions endpoint.
type LLMProvider struct {
	Name    string // "openrouter", "anthropic", "google", "moonshot", "zai"
	BaseURL string
	APIKey  string
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
