package constants

// GatewayKeyPrefix is the prefix for per-project gateway API keys.
const GatewayKeyPrefix = "wemob_gw_"

// GatewayMaxImagesPerRequest is the maximum number of images per generation request.
const GatewayMaxImagesPerRequest = 4

// GatewayDefaultChatModel is the sensible chat-model default when we have no
// tier / allowlist info to narrow the choice. It must appear in every tier
// under DefaultGatewayModels so free-tier users can use it too.
const GatewayDefaultChatModel = "anthropic/claude-haiku-4-5"

// GatewayPreferredChatModel is the higher-quality chat model we prefer when
// the caller's tier allows it. Falls back to GatewayDefaultChatModel when
// not in the allowed set.
const GatewayPreferredChatModel = "anthropic/claude-sonnet-4.6"

// GatewayDefaultImageModel is the default image-generation model. Cheap and
// fast — used for hero/banner images and other generate_image calls.
const GatewayDefaultImageModel = "fal-ai/flux/schnell"

// DefaultGatewayModels is the fallback model allowlist per subscription tier name.
// Used when remote config is unavailable. Tier names are lowercased subscription names.
// Chat model IDs are in OpenRouter's provider/model format because the gateway
// proxies every chat request through OpenRouter (see ResolveGatewayProvider).
var DefaultGatewayModels = map[string][]string{
	"free":     {"openai/gpt-4.1-mini", "anthropic/claude-haiku-4-5"},
	"starter":  {"openai/gpt-4.1-mini", "anthropic/claude-haiku-4-5", "fal-ai/flux/schnell"},
	"pro":      {"openai/gpt-4.1-mini", "openai/gpt-4.1", "anthropic/claude-haiku-4-5", "anthropic/claude-sonnet-4.6", "moonshot/kimi-k2.5", "fal-ai/flux/schnell", "fal-ai/flux-pro/v1.1"},
	"business": {"openai/gpt-4.1-mini", "openai/gpt-4.1", "anthropic/claude-haiku-4-5", "anthropic/claude-sonnet-4.6", "anthropic/claude-opus-4.7", "google/gemini-2.5-pro", "moonshot/kimi-k2.5", "moonshot/kimi-k2.6", "fal-ai/flux/schnell", "fal-ai/flux-pro/v1.1"},
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
	"fal-ai/flux/schnell":  5,
	"fal-ai/flux-pro/v1.1": 15,
	"fal-ai/flux-pro/v1.1-ultra":    40,
}

// GatewayModelInfo defines metadata for a model available through the gateway.
//
// InputUSDPerM / OutputUSDPerM are the upstream prices (what Wemob pays
// OpenRouter per million tokens) and are the source of truth for credit
// accounting in recordGatewayChatUsage. Keep these in sync with
// OpenRouter's live prices — OpenRouter already folds its ~5% platform fee
// into the rate it bills, so no markup needs to be applied here before
// computing the user's credit cost.
//
// CostPer1kInput / CostPer1kOutput are coarse integer display hints used
// by the dashboard pricing cards; the float fields above are what the
// charging path actually reads.
type GatewayModelInfo struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Provider        string   `json:"provider"`
	Type            string   `json:"type"` // "chat" or "image"
	MaxContext      int      `json:"max_context,omitempty"`
	InputUSDPerM    float64  `json:"input_usd_per_m,omitempty"`
	OutputUSDPerM   float64  `json:"output_usd_per_m,omitempty"`
	CostPer1kInput  int      `json:"cost_per_1k_input,omitempty"`
	CostPer1kOutput int      `json:"cost_per_1k_output,omitempty"`
	CostCredits     int      `json:"cost_credits,omitempty"` // fixed cost for image models
	Capabilities    []string `json:"capabilities"`
}

var GatewayModelCatalog = []GatewayModelInfo{
	// Chat models — IDs are OpenRouter-format since the gateway proxies chat
	// through OpenRouter. Provider is the underlying model author (shown in
	// the /v2/gateway/models UI), not our upstream. Prices approximate
	// public OpenRouter rates as of 2026-04-23; update alongside upstream
	// price changes.
	{ID: "openai/gpt-4.1-mini", Name: "GPT-4.1 Mini", Provider: "openai", Type: "chat", MaxContext: 1047576, InputUSDPerM: 0.40, OutputUSDPerM: 1.60, CostPer1kInput: 1, CostPer1kOutput: 3, Capabilities: []string{"streaming", "vision"}},
	{ID: "openai/gpt-4.1", Name: "GPT-4.1", Provider: "openai", Type: "chat", MaxContext: 1047576, InputUSDPerM: 2.00, OutputUSDPerM: 8.00, CostPer1kInput: 2, CostPer1kOutput: 8, Capabilities: []string{"streaming", "vision"}},
	{ID: "anthropic/claude-haiku-4-5", Name: "Claude Haiku 4.5", Provider: "anthropic", Type: "chat", MaxContext: 200000, InputUSDPerM: 1.00, OutputUSDPerM: 5.00, CostPer1kInput: 1, CostPer1kOutput: 5, Capabilities: []string{"streaming", "vision"}},
	{ID: "anthropic/claude-sonnet-4.6", Name: "Claude Sonnet 4.6", Provider: "anthropic", Type: "chat", MaxContext: 200000, InputUSDPerM: 3.00, OutputUSDPerM: 15.00, CostPer1kInput: 3, CostPer1kOutput: 15, Capabilities: []string{"streaming", "vision"}},
	{ID: "anthropic/claude-opus-4.7", Name: "Claude Opus 4.7", Provider: "anthropic", Type: "chat", MaxContext: 200000, InputUSDPerM: 5.00, OutputUSDPerM: 25.00, CostPer1kInput: 5, CostPer1kOutput: 25, Capabilities: []string{"streaming", "vision"}},
	{ID: "google/gemini-2.5-pro", Name: "Gemini 2.5 Pro", Provider: "google", Type: "chat", MaxContext: 1048576, InputUSDPerM: 1.25, OutputUSDPerM: 5.00, CostPer1kInput: 1, CostPer1kOutput: 4, Capabilities: []string{"streaming", "vision"}},
	{ID: "moonshot/kimi-k2.5", Name: "Kimi K2.5", Provider: "moonshot", Type: "chat", MaxContext: 256000, InputUSDPerM: 0.50, OutputUSDPerM: 2.00, CostPer1kInput: 1, CostPer1kOutput: 2, Capabilities: []string{"streaming", "vision"}},
	{ID: "moonshot/kimi-k2.6", Name: "Kimi K2.6", Provider: "moonshot", Type: "chat", MaxContext: 256000, InputUSDPerM: 0.60, OutputUSDPerM: 2.80, CostPer1kInput: 1, CostPer1kOutput: 3, Capabilities: []string{"streaming", "vision"}},
	// Image models — proxied directly to fal.ai, not through OpenRouter.
	{ID: "fal-ai/flux/schnell", Name: "FLUX Schnell", Provider: "fal", Type: "image", CostCredits: 5, Capabilities: []string{"fast"}},
	{ID: "fal-ai/flux-pro/v1.1", Name: "FLUX Pro v1.1", Provider: "fal", Type: "image", CostCredits: 15, Capabilities: []string{"hd"}},
	{ID: "fal-ai/flux-pro/v1.1-ultra", Name: "FLUX Ultra", Provider: "fal", Type: "image", CostCredits: 40, Capabilities: []string{"hd", "ultra"}},
}

// GatewayChatModelByID returns a pointer to the chat model metadata for an
// ID, or nil if the model is unknown. Used by the chat charging path to
// look up upstream USD prices when computing the per-call credit owed.
func GatewayChatModelByID(id string) *GatewayModelInfo {
	for i := range GatewayModelCatalog {
		if GatewayModelCatalog[i].ID == id && GatewayModelCatalog[i].Type == "chat" {
			return &GatewayModelCatalog[i]
		}
	}
	return nil
}
