package constants

import "strings"

// GatewayKeyPrefix is the prefix for per-project gateway API keys.
const GatewayKeyPrefix = "wemob_gw_"

// GatewayMaxImagesPerRequest is the maximum number of images per generation request.
const GatewayMaxImagesPerRequest = 4

// GatewayDefaultChatModel is the sensible chat-model default when we have no
// tier / allowlist info to narrow the choice. It must appear in every tier
// under DefaultGatewayModels so free-tier users can use it too.
const GatewayDefaultChatModel = "openai/gpt-5.4-nano"

// GatewayPreferredChatModel is the higher-quality chat model we prefer when
// the caller's tier allows it. Falls back to GatewayDefaultChatModel when
// not in the allowed set.
const GatewayPreferredChatModel = "openai/gpt-5.4"

// GatewayDefaultImageModel is the default image-generation model. Cheap and
// fast — used for hero/banner images and other generate_image calls.
const GatewayDefaultImageModel = "fal-ai/nano-banana-2"

// DefaultGatewayModels is the fallback model allowlist per subscription tier name.
// Used when remote config is unavailable. Tier names are lowercased subscription names.
// Chat model IDs are in OpenRouter's provider/model format because the gateway
// proxies every chat request through OpenRouter (see ResolveGatewayProvider).
var DefaultGatewayModels = map[string][]string{
	"free": {
		"openai/gpt-5.4-nano", "anthropic/claude-haiku-4-5", "qwen/qwen3.7-plus",
	},
	"starter": {
		"openai/gpt-5.4-nano", "openai/gpt-5.4-mini", "anthropic/claude-haiku-4-5",
		"google/gemini-3.1-flash-lite", "qwen/qwen3.7-plus", "fal-ai/flux/schnell",
		"fal-ai/nano-banana-2", "fal-ai/flux-2-pro",
	},
	"pro": {
		"openai/gpt-5.4-nano", "openai/gpt-5.4-mini", "openai/gpt-5.4",
		"anthropic/claude-haiku-4-5", "anthropic/claude-sonnet-4.6", "google/gemini-3.5-flash",
		"google/gemini-3.1-flash-lite", "x-ai/grok-4.3", "qwen/qwen3.7-plus", "qwen/qwen3.7-max",
		"minimax/minimax-m3", "inception/mercury-2", "deepseek/deepseek-r1",
		"fal-ai/flux/schnell", "fal-ai/flux-pro/v1.1", "fal-ai/flux-pro/v1.1-ultra",
		"fal-ai/nano-banana-2", "fal-ai/nano-banana-pro", "fal-ai/flux-2-pro", "fal-ai/flux-pro/kontext",
		"fal-ai/gpt-image-2",
	},
	"business": {
		"openai/gpt-5.4-nano", "openai/gpt-5.4-mini", "openai/gpt-5.4", "openai/gpt-5.4-pro",
		"anthropic/claude-haiku-4-5", "anthropic/claude-sonnet-4.6", "anthropic/claude-opus-4.8",
		"anthropic/claude-opus-4.8-fast", "google/gemini-3.1-pro-preview", "google/gemini-3.5-flash",
		"google/gemini-3.1-flash-lite", "x-ai/grok-4.3", "x-ai/grok-build-0.1",
		"qwen/qwen3.7-plus", "qwen/qwen3.7-max", "minimax/minimax-m3", "inception/mercury-2",
		"deepseek/deepseek-r1", "stepfun/step-3.7-flash",
		"fal-ai/flux/schnell", "fal-ai/flux-pro/v1.1", "fal-ai/flux-pro/v1.1-ultra",
		"fal-ai/nano-banana-2", "fal-ai/nano-banana-2/edit", "fal-ai/nano-banana-pro",
		"fal-ai/nano-banana-pro/edit", "fal-ai/flux-2-pro", "fal-ai/flux-pro/kontext",
		"fal-ai/gpt-image-2", "fal-ai/gpt-image-2/image-to-image", "fal-ai/bytedance/seedream/v4.5/edit",
		"xai/grok-imagine-image", "xai/grok-imagine-image/edit",
	},
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
	"fal-ai/flux/schnell":                 5,
	"fal-ai/flux-pro/v1.1":                15,
	"fal-ai/flux-pro/v1.1-ultra":          40,
	"fal-ai/flux-2-pro":                   10,
	"fal-ai/flux-pro/kontext":             15,
	"fal-ai/nano-banana-2":                10,
	"fal-ai/nano-banana-2/edit":           10,
	"fal-ai/nano-banana-pro":              20,
	"fal-ai/nano-banana-pro/edit":         20,
	"fal-ai/gpt-image-2":                  25,
	"fal-ai/gpt-image-2/image-to-image":   25,
	"fal-ai/bytedance/seedream/v4.5/edit": 10,
	"xai/grok-imagine-image":              15,
	"xai/grok-imagine-image/edit":         15,
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
	// public OpenRouter rates; update alongside upstream price changes.
	{ID: "openai/gpt-5.4-nano", Name: "GPT-5.4 Nano", Provider: "openai", Type: "chat", MaxContext: 400000, InputUSDPerM: 0.20, OutputUSDPerM: 1.25, CostPer1kInput: 1, CostPer1kOutput: 1, Capabilities: []string{"reasoning", "tools", "structured_outputs", "vision"}},
	{ID: "openai/gpt-5.4-mini", Name: "GPT-5.4 Mini", Provider: "openai", Type: "chat", MaxContext: 400000, InputUSDPerM: 0.75, OutputUSDPerM: 4.50, CostPer1kInput: 1, CostPer1kOutput: 5, Capabilities: []string{"reasoning", "tools", "structured_outputs", "vision"}},
	{ID: "openai/gpt-5.4", Name: "GPT-5.4", Provider: "openai", Type: "chat", MaxContext: 1050000, InputUSDPerM: 2.50, OutputUSDPerM: 15.00, CostPer1kInput: 3, CostPer1kOutput: 15, Capabilities: []string{"reasoning", "tools", "structured_outputs", "vision"}},
	{ID: "openai/gpt-5.4-pro", Name: "GPT-5.4 Pro", Provider: "openai", Type: "chat", MaxContext: 1050000, InputUSDPerM: 30.00, OutputUSDPerM: 180.00, CostPer1kInput: 30, CostPer1kOutput: 180, Capabilities: []string{"reasoning", "tools", "structured_outputs", "vision", "premium"}},
	{ID: "openai/gpt-4.1-mini", Name: "GPT-4.1 Mini", Provider: "openai", Type: "chat", MaxContext: 1047576, InputUSDPerM: 0.40, OutputUSDPerM: 1.60, CostPer1kInput: 1, CostPer1kOutput: 3, Capabilities: []string{"streaming", "vision"}},
	{ID: "openai/gpt-4.1", Name: "GPT-4.1", Provider: "openai", Type: "chat", MaxContext: 1047576, InputUSDPerM: 2.00, OutputUSDPerM: 8.00, CostPer1kInput: 2, CostPer1kOutput: 8, Capabilities: []string{"streaming", "vision"}},
	{ID: "anthropic/claude-haiku-4-5", Name: "Claude Haiku 4.5", Provider: "anthropic", Type: "chat", MaxContext: 200000, InputUSDPerM: 1.00, OutputUSDPerM: 5.00, CostPer1kInput: 1, CostPer1kOutput: 5, Capabilities: []string{"streaming", "vision"}},
	{ID: "anthropic/claude-sonnet-4.6", Name: "Claude Sonnet 4.6", Provider: "anthropic", Type: "chat", MaxContext: 200000, InputUSDPerM: 3.00, OutputUSDPerM: 15.00, CostPer1kInput: 3, CostPer1kOutput: 15, Capabilities: []string{"streaming", "vision"}},
	{ID: "anthropic/claude-opus-4.8", Name: "Claude Opus 4.8", Provider: "anthropic", Type: "chat", MaxContext: 1000000, InputUSDPerM: 5.00, OutputUSDPerM: 25.00, CostPer1kInput: 5, CostPer1kOutput: 25, Capabilities: []string{"reasoning", "tools", "structured_outputs", "vision", "premium"}},
	{ID: "anthropic/claude-opus-4.8-fast", Name: "Claude Opus 4.8 Fast", Provider: "anthropic", Type: "chat", MaxContext: 1000000, InputUSDPerM: 10.00, OutputUSDPerM: 50.00, CostPer1kInput: 10, CostPer1kOutput: 50, Capabilities: []string{"reasoning", "tools", "structured_outputs", "vision", "premium", "fast"}},
	{ID: "google/gemini-2.5-pro", Name: "Gemini 2.5 Pro", Provider: "google", Type: "chat", MaxContext: 1048576, InputUSDPerM: 1.25, OutputUSDPerM: 5.00, CostPer1kInput: 1, CostPer1kOutput: 4, Capabilities: []string{"streaming", "vision"}},
	{ID: "google/gemini-3.1-pro-preview", Name: "Gemini 3.1 Pro Preview", Provider: "google", Type: "chat", MaxContext: 1048576, InputUSDPerM: 2.00, OutputUSDPerM: 12.00, CostPer1kInput: 2, CostPer1kOutput: 12, Capabilities: []string{"reasoning", "tools", "structured_outputs", "vision", "audio", "video", "premium"}},
	{ID: "google/gemini-3.5-flash", Name: "Gemini 3.5 Flash", Provider: "google", Type: "chat", MaxContext: 1048576, InputUSDPerM: 1.50, OutputUSDPerM: 9.00, CostPer1kInput: 2, CostPer1kOutput: 9, Capabilities: []string{"reasoning", "tools", "structured_outputs", "vision", "audio", "video", "fast"}},
	{ID: "google/gemini-3.1-flash-lite", Name: "Gemini 3.1 Flash Lite", Provider: "google", Type: "chat", MaxContext: 1048576, InputUSDPerM: 0.25, OutputUSDPerM: 1.50, CostPer1kInput: 1, CostPer1kOutput: 2, Capabilities: []string{"reasoning", "tools", "structured_outputs", "vision", "audio", "video", "fast"}},
	{ID: "x-ai/grok-4.3", Name: "Grok 4.3", Provider: "x-ai", Type: "chat", MaxContext: 1000000, InputUSDPerM: 1.25, OutputUSDPerM: 2.50, CostPer1kInput: 1, CostPer1kOutput: 3, Capabilities: []string{"reasoning", "tools", "structured_outputs", "vision"}},
	{ID: "x-ai/grok-build-0.1", Name: "Grok Build 0.1", Provider: "x-ai", Type: "chat", MaxContext: 256000, InputUSDPerM: 1.00, OutputUSDPerM: 2.00, CostPer1kInput: 1, CostPer1kOutput: 2, Capabilities: []string{"reasoning", "tools", "structured_outputs", "vision", "coding"}},
	{ID: "qwen/qwen3.7-plus", Name: "Qwen3.7 Plus", Provider: "qwen", Type: "chat", MaxContext: 1000000, InputUSDPerM: 0.40, OutputUSDPerM: 1.60, CostPer1kInput: 1, CostPer1kOutput: 2, Capabilities: []string{"reasoning", "tools", "structured_outputs", "vision"}},
	{ID: "qwen/qwen3.7-max", Name: "Qwen3.7 Max", Provider: "qwen", Type: "chat", MaxContext: 1000000, InputUSDPerM: 1.25, OutputUSDPerM: 3.75, CostPer1kInput: 1, CostPer1kOutput: 4, Capabilities: []string{"reasoning", "tools", "structured_outputs"}},
	{ID: "minimax/minimax-m3", Name: "MiniMax M3", Provider: "minimax", Type: "chat", MaxContext: 1048576, InputUSDPerM: 0.30, OutputUSDPerM: 1.20, CostPer1kInput: 1, CostPer1kOutput: 2, Capabilities: []string{"reasoning", "tools", "vision", "video"}},
	{ID: "inception/mercury-2", Name: "Mercury 2", Provider: "inception", Type: "chat", MaxContext: 128000, InputUSDPerM: 0.25, OutputUSDPerM: 0.75, CostPer1kInput: 1, CostPer1kOutput: 1, Capabilities: []string{"reasoning", "tools", "structured_outputs", "fast"}},
	{ID: "deepseek/deepseek-r1", Name: "DeepSeek R1", Provider: "deepseek", Type: "chat", MaxContext: 163840, InputUSDPerM: 0.70, OutputUSDPerM: 2.50, CostPer1kInput: 1, CostPer1kOutput: 3, Capabilities: []string{"reasoning", "tools", "structured_outputs"}},
	{ID: "stepfun/step-3.5-flash", Name: "Step 3.5 Flash", Provider: "stepfun", Type: "chat", MaxContext: 32768, InputUSDPerM: 0.09, OutputUSDPerM: 0.30, CostPer1kInput: 1, CostPer1kOutput: 1, Capabilities: []string{"streaming"}},
	{ID: "stepfun/step-3.7-flash", Name: "Step 3.7 Flash", Provider: "stepfun", Type: "chat", MaxContext: 32768, InputUSDPerM: 0.20, OutputUSDPerM: 1.15, CostPer1kInput: 1, CostPer1kOutput: 1, Capabilities: []string{"streaming"}},
	// Image models — proxied directly to fal.ai, not through OpenRouter.
	{ID: "fal-ai/flux/schnell", Name: "FLUX Schnell", Provider: "fal", Type: "image", CostCredits: 5, Capabilities: []string{"fast"}},
	{ID: "fal-ai/flux-pro/v1.1", Name: "FLUX Pro v1.1", Provider: "fal", Type: "image", CostCredits: 15, Capabilities: []string{"hd"}},
	{ID: "fal-ai/flux-pro/v1.1-ultra", Name: "FLUX Ultra", Provider: "fal", Type: "image", CostCredits: 40, Capabilities: []string{"hd", "ultra"}},
	{ID: "fal-ai/flux-2-pro", Name: "FLUX 2 Pro", Provider: "fal", Type: "image", CostCredits: 10, Capabilities: []string{"hd", "production"}},
	{ID: "fal-ai/flux-pro/kontext", Name: "FLUX Kontext Pro", Provider: "fal", Type: "image", CostCredits: 15, Capabilities: []string{"edit", "image_to_image", "character_consistency"}},
	{ID: "fal-ai/nano-banana-2", Name: "Nano Banana 2", Provider: "fal", Type: "image", CostCredits: 10, Capabilities: []string{"fast", "typography", "character_consistency"}},
	{ID: "fal-ai/nano-banana-2/edit", Name: "Nano Banana 2 Edit", Provider: "fal", Type: "image", CostCredits: 10, Capabilities: []string{"edit", "image_to_image", "typography"}},
	{ID: "fal-ai/nano-banana-pro", Name: "Nano Banana Pro", Provider: "fal", Type: "image", CostCredits: 20, Capabilities: []string{"premium", "typography", "hd"}},
	{ID: "fal-ai/nano-banana-pro/edit", Name: "Nano Banana Pro Edit", Provider: "fal", Type: "image", CostCredits: 20, Capabilities: []string{"premium", "edit", "image_to_image", "typography"}},
	{ID: "fal-ai/gpt-image-2", Name: "GPT Image 2", Provider: "fal", Type: "image", CostCredits: 25, Capabilities: []string{"premium", "typography", "infographic", "hd"}},
	{ID: "fal-ai/gpt-image-2/image-to-image", Name: "GPT Image 2 Edit", Provider: "fal", Type: "image", CostCredits: 25, Capabilities: []string{"premium", "edit", "image_to_image", "mask", "typography"}},
	{ID: "fal-ai/bytedance/seedream/v4.5/edit", Name: "Seedream 4.5 Edit", Provider: "fal", Type: "image", CostCredits: 10, Capabilities: []string{"edit", "image_to_image", "multi_reference"}},
	{ID: "xai/grok-imagine-image", Name: "Grok Imagine Image", Provider: "fal", Type: "image", CostCredits: 15, Capabilities: []string{"creative", "hd"}},
	{ID: "xai/grok-imagine-image/edit", Name: "Grok Imagine Image Edit", Provider: "fal", Type: "image", CostCredits: 15, Capabilities: []string{"edit", "image_to_image", "creative"}},
}

var gatewayModelAliases = map[string]string{
	"gpt-4.1-mini":               "openai/gpt-4.1-mini",
	"gpt-4.1":                    "openai/gpt-4.1",
	"gpt-5.4-nano":               "openai/gpt-5.4-nano",
	"gpt-5.4-mini":               "openai/gpt-5.4-mini",
	"gpt-5.4":                    "openai/gpt-5.4",
	"gpt-5.4-pro":                "openai/gpt-5.4-pro",
	"claude-haiku-4-5":           "anthropic/claude-haiku-4-5",
	"claude-sonnet-4.6":          "anthropic/claude-sonnet-4.6",
	"claude-opus-4.8":            "anthropic/claude-opus-4.8",
	"claude-opus-4.8-fast":       "anthropic/claude-opus-4.8-fast",
	"gemini-2.5-pro":             "google/gemini-2.5-pro",
	"gemini-3.1-pro-preview":     "google/gemini-3.1-pro-preview",
	"gemini-3.5-flash":           "google/gemini-3.5-flash",
	"gemini-3.1-flash-lite":      "google/gemini-3.1-flash-lite",
	"grok-4.3":                   "x-ai/grok-4.3",
	"grok-build-0.1":             "x-ai/grok-build-0.1",
	"qwen3.7-plus":               "qwen/qwen3.7-plus",
	"qwen3.7-max":                "qwen/qwen3.7-max",
	"minimax-m3":                 "minimax/minimax-m3",
	"mercury-2":                  "inception/mercury-2",
	"deepseek-r1":                "deepseek/deepseek-r1",
	"step-3.5-flash":             "stepfun/step-3.5-flash",
	"step-3.7-flash":             "stepfun/step-3.7-flash",
	"flux/schnell":               "fal-ai/flux/schnell",
	"flux-pro/v1.1":              "fal-ai/flux-pro/v1.1",
	"flux-pro/v1.1-ultra":        "fal-ai/flux-pro/v1.1-ultra",
	"flux-2-pro":                 "fal-ai/flux-2-pro",
	"flux-pro/kontext":           "fal-ai/flux-pro/kontext",
	"nano-banana-2":              "fal-ai/nano-banana-2",
	"nano-banana-2/edit":         "fal-ai/nano-banana-2/edit",
	"nano-banana-pro":            "fal-ai/nano-banana-pro",
	"nano-banana-pro/edit":       "fal-ai/nano-banana-pro/edit",
	"gpt-image-2":                "fal-ai/gpt-image-2",
	"gpt-image-2/edit":           "fal-ai/gpt-image-2/image-to-image",
	"gpt-image-2/image-to-image": "fal-ai/gpt-image-2/image-to-image",
	"seedream/v4.5/edit":         "fal-ai/bytedance/seedream/v4.5/edit",
	"grok-imagine-image":         "xai/grok-imagine-image",
	"grok-imagine-image/edit":    "xai/grok-imagine-image/edit",
}

// GatewayCanonicalModelID normalizes old/bare remote-config model IDs to the
// provider/model IDs accepted by the gateway. Unknown IDs are returned trimmed
// so callers can decide whether to reject or pass them through.
func GatewayCanonicalModelID(id string) string {
	id = strings.TrimSpace(id)
	if canonical, ok := gatewayModelAliases[id]; ok {
		return canonical
	}
	return id
}

// GatewayModelByID returns model metadata for any gateway model type.
func GatewayModelByID(id string) *GatewayModelInfo {
	id = GatewayCanonicalModelID(id)
	for i := range GatewayModelCatalog {
		if GatewayModelCatalog[i].ID == id {
			return &GatewayModelCatalog[i]
		}
	}
	return nil
}

// GatewayChatModelByID returns a pointer to the chat model metadata for an
// ID, or nil if the model is unknown. Used by the chat charging path to
// look up upstream USD prices when computing the per-call credit owed.
func GatewayChatModelByID(id string) *GatewayModelInfo {
	m := GatewayModelByID(id)
	if m != nil && m.Type == "chat" {
		return m
	}
	return nil
}
