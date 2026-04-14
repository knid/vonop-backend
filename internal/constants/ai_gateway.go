package constants

// GatewayKeyPrefix is the prefix for per-project gateway API keys.
const GatewayKeyPrefix = "wemob_gw_"

// GatewayMaxImagesPerRequest is the maximum number of images per generation request.
const GatewayMaxImagesPerRequest = 4

// DefaultGatewayModels is the fallback model allowlist per subscription tier name.
// Used when remote config is unavailable. Tier names are lowercased subscription names.
// Chat model IDs are in OpenRouter's provider/model format because the gateway
// proxies every chat request through OpenRouter (see ResolveGatewayProvider).
var DefaultGatewayModels = map[string][]string{
	"free":     {"openai/gpt-4.1-mini", "anthropic/claude-haiku-4-5"},
	"starter":  {"openai/gpt-4.1-mini", "anthropic/claude-haiku-4-5", "fal-ai/flux-schnell"},
	"pro":      {"openai/gpt-4.1-mini", "openai/gpt-4.1", "anthropic/claude-haiku-4-5", "anthropic/claude-sonnet-4", "fal-ai/flux-schnell", "fal-ai/flux-pro/v1.1"},
	"business": {"openai/gpt-4.1-mini", "openai/gpt-4.1", "anthropic/claude-haiku-4-5", "anthropic/claude-sonnet-4", "anthropic/claude-opus-4", "google/gemini-2.5-pro", "fal-ai/flux-schnell", "fal-ai/flux-pro/v1.1"},
}

// DefaultGatewayRPM is the fallback requests-per-minute limit per tier.
var DefaultGatewayRPM = map[string]int{
	"free":     10,
	"starter":  30,
	"pro":      60,
	"business": 120,
}

// DefaultGatewayImageCosts maps fal.ai model IDs to their credit cost per image.
var DefaultGatewayImageCosts = map[string]int{
	"fal-ai/flux-schnell":  5,
	"fal-ai/flux-pro/v1.1": 15,
	"fal-ai/flux-ultra":    40,
}

// GatewayModelInfo defines metadata for a model available through the gateway.
type GatewayModelInfo struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Provider        string   `json:"provider"`
	Type            string   `json:"type"` // "chat" or "image"
	MaxContext      int      `json:"max_context,omitempty"`
	CostPer1kInput  int      `json:"cost_per_1k_input,omitempty"`
	CostPer1kOutput int      `json:"cost_per_1k_output,omitempty"`
	CostCredits     int      `json:"cost_credits,omitempty"` // fixed cost for image models
	Capabilities    []string `json:"capabilities"`
}

var GatewayModelCatalog = []GatewayModelInfo{
	// Chat models — IDs are OpenRouter-format since the gateway proxies chat
	// through OpenRouter. Provider is the underlying model author (shown in
	// the /v2/gateway/models UI), not our upstream.
	{ID: "openai/gpt-4.1-mini", Name: "GPT-4.1 Mini", Provider: "openai", Type: "chat", MaxContext: 1047576, CostPer1kInput: 1, CostPer1kOutput: 3, Capabilities: []string{"streaming", "vision"}},
	{ID: "openai/gpt-4.1", Name: "GPT-4.1", Provider: "openai", Type: "chat", MaxContext: 1047576, CostPer1kInput: 2, CostPer1kOutput: 8, Capabilities: []string{"streaming", "vision"}},
	{ID: "anthropic/claude-haiku-4-5", Name: "Claude Haiku 4.5", Provider: "anthropic", Type: "chat", MaxContext: 200000, CostPer1kInput: 1, CostPer1kOutput: 5, Capabilities: []string{"streaming", "vision"}},
	{ID: "anthropic/claude-sonnet-4", Name: "Claude Sonnet 4", Provider: "anthropic", Type: "chat", MaxContext: 200000, CostPer1kInput: 3, CostPer1kOutput: 15, Capabilities: []string{"streaming", "vision"}},
	{ID: "anthropic/claude-opus-4", Name: "Claude Opus 4", Provider: "anthropic", Type: "chat", MaxContext: 200000, CostPer1kInput: 15, CostPer1kOutput: 75, Capabilities: []string{"streaming", "vision"}},
	{ID: "google/gemini-2.5-pro", Name: "Gemini 2.5 Pro", Provider: "google", Type: "chat", MaxContext: 1048576, CostPer1kInput: 1, CostPer1kOutput: 4, Capabilities: []string{"streaming", "vision"}},
	// Image models — proxied directly to fal.ai, not through OpenRouter.
	{ID: "fal-ai/flux-schnell", Name: "FLUX Schnell", Provider: "fal", Type: "image", CostCredits: 5, Capabilities: []string{"fast"}},
	{ID: "fal-ai/flux-pro/v1.1", Name: "FLUX Pro v1.1", Provider: "fal", Type: "image", CostCredits: 15, Capabilities: []string{"hd"}},
	{ID: "fal-ai/flux-ultra", Name: "FLUX Ultra", Provider: "fal", Type: "image", CostCredits: 40, Capabilities: []string{"hd", "ultra"}},
}
