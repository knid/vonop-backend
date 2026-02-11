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

// setLLMHeaders applies auth and provider-specific headers to an HTTP request.
func setLLMHeaders(req *http.Request, provider *LLMProvider) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+provider.APIKey)
	if provider.IsOpenRouter() {
		req.Header.Set("HTTP-Referer", "https://wemob.io")
		req.Header.Set("X-Title", "wemob-app-builder")
	}
}
