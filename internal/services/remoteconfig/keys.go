package remoteconfig

// Global is the distinct ID used for non-user-scoped config keys.
const Global = "__global__"

// Key defines a remote config key with its PostHog flag name, type, and fallback.
type Key struct {
	Flag     string // PostHog feature flag name (e.g. "rc-referral-signup-credits")
	Fallback any    // Default value when PostHog is unavailable or flag not created
}

// --- Category 1: Business & Pricing ---

var ReferralSignupCredits = Key{Flag: "rc-referral-signup-credits", Fallback: 10}
var ReferralSubscriptionCredits = Key{Flag: "rc-referral-subscription-credits", Fallback: 50}

// Base pack — cheapest per-credit rate; used as the reference for computing
// savings_percent on larger packs. Keep $0.10/credit as the baseline.
var CreditPack100Credits = Key{Flag: "rc-credit-pack-100-credits", Fallback: 100}
var CreditPack100Price = Key{Flag: "rc-credit-pack-100-price", Fallback: 10}

// Entry pack — 10% savings over the base rate ($18 vs $20 nominal).
var CreditPack200Credits = Key{Flag: "rc-credit-pack-200-credits", Fallback: 200}
var CreditPack200Price = Key{Flag: "rc-credit-pack-200-price", Fallback: 18}

// Small-mid pack — 30% savings over the base rate ($35 vs $50 nominal).
var CreditPack500Credits = Key{Flag: "rc-credit-pack-500-credits", Fallback: 500}
var CreditPack500Price = Key{Flag: "rc-credit-pack-500-price", Fallback: 35}

// Mid pack — 50% savings over the base rate ($50 vs $100 nominal).
var CreditPack1000Credits = Key{Flag: "rc-credit-pack-1000-credits", Fallback: 1000}
var CreditPack1000Price = Key{Flag: "rc-credit-pack-1000-price", Fallback: 50}

// Power pack — ~58% savings over the base rate ($125 vs $300 nominal).
var CreditPack3000Credits = Key{Flag: "rc-credit-pack-3000-credits", Fallback: 3000}
var CreditPack3000Price = Key{Flag: "rc-credit-pack-3000-price", Fallback: 125}

// First-completion pack — micro top-up sized to finish a typical first app
// (median first run is ~30 credits, p90 is ~57 — 50 credits @ $3 covers the
// long tail). Server filters this pack out once the user has any prior
// `purchase` transaction so it's a true one-time onboarding lever.
var CreditPackFirstCompletionCredits = Key{Flag: "rc-credit-pack-first-completion-credits", Fallback: 50}
var CreditPackFirstCompletionPrice = Key{Flag: "rc-credit-pack-first-completion-price", Fallback: 3}
var AICreditsTokensPerCredit = Key{Flag: "rc-ai-credits-tokens-per-credit", Fallback: 5000}
var AICreditsOutputMultiplier = Key{Flag: "rc-ai-credits-output-multiplier", Fallback: 5}
var AICreditsCacheReadDivisor = Key{Flag: "rc-ai-credits-cache-read-divisor", Fallback: 10}
var AICreditsMinBalance = Key{Flag: "rc-ai-credits-min-balance", Fallback: -100}

// PlanAICreditsTokensPerCredit returns the per-plan rc flag for tokens-per-credit.
func PlanAICreditsTokensPerCredit(planName string) Key {
	return PlanInt(planName, "ai-credits-tokens-per-credit", 5000)
}

// PlanAICreditsOutputMultiplier returns the per-plan rc flag for output multiplier.
func PlanAICreditsOutputMultiplier(planName string) Key {
	return PlanInt(planName, "ai-credits-output-multiplier", 5)
}

// PlanAICreditsCacheReadDivisor returns the per-plan rc flag for cache-read divisor.
func PlanAICreditsCacheReadDivisor(planName string) Key {
	return PlanInt(planName, "ai-credits-cache-read-divisor", 10)
}

// StarterPlanEnabled controls whether new users are automatically assigned
// the free Starter plan on signup. When false, users must select a paid plan
// (Hobby/Pro/Max) before they can use Wemob. Supports A/B testing via PostHog
// percentage rollouts — evaluate per-user during signup.
var StarterPlanEnabled = Key{Flag: "rc-starter-plan-enabled", Fallback: true}

// --- Category 1b: Plan Display Overrides ---
// These override the DB-stored plan limits for the billing/plans API.
// Flag names: rc-plan-{planName}-{field} where planName is lowercased.
// Helper constructors build Keys dynamically per plan name.

func PlanInt(planName, field string, fallback int) Key {
	return Key{Flag: "rc-plan-" + planName + "-" + field, Fallback: fallback}
}

func PlanBool(planName, field string, fallback bool) Key {
	return Key{Flag: "rc-plan-" + planName + "-" + field, Fallback: fallback}
}

// --- Category 2: Platform Limits ---

var MaxConcurrentJobsPerUser = Key{Flag: "rc-max-concurrent-jobs-per-user", Fallback: 3}
var MaxConcurrentJobsPerOrg = Key{Flag: "rc-max-concurrent-jobs-per-org", Fallback: 6}
var MaxQueryRows = Key{Flag: "rc-max-query-rows", Fallback: 1000}

// --- Category 3: AI Generation Tuning ---

var AIChatKeepRecent = Key{Flag: "rc-ai-chat-keep-recent", Fallback: 10}
var AIMaxStructureFiles = Key{Flag: "rc-ai-max-structure-files", Fallback: 25}
var AIBuilderToolMaxRounds = Key{Flag: "rc-ai-builder-tool-max-rounds", Fallback: 30}
var AIGenerationMaxRounds = Key{Flag: "rc-ai-generation-max-rounds", Fallback: 8}
var AIMaxTokensPerRun = Key{Flag: "rc-ai-max-tokens-per-run", Fallback: 500000}
var AICompactAfterTokens = Key{Flag: "rc-ai-compact-after-tokens", Fallback: 250000}
var AICompactIntervalTokens = Key{Flag: "rc-ai-compact-interval-tokens", Fallback: 150000}
var AITrimToolResultChars = Key{Flag: "rc-ai-trim-tool-result-chars", Fallback: 4000}
var AITrimToolArgChars = Key{Flag: "rc-ai-trim-tool-arg-chars", Fallback: 1200}
var AIMoonshotThinkingEnabled = Key{Flag: "rc-ai-moonshot-thinking-enabled", Fallback: true}

// Nudge/Strip thresholds per project type. Draft (new project) gets more lenient
// defaults because scaffolding from scratch legitimately needs more consecutive
// tool rounds than targeted chat edits.
var AIGenerationNudgeAfter = Key{Flag: "rc-ai-generation-nudge-after", Fallback: 5}
var AIGenerationStripAfter = Key{Flag: "rc-ai-generation-strip-after", Fallback: 8}
var AIBuilderToolNudgeAfter = Key{Flag: "rc-ai-builder-tool-nudge-after", Fallback: 3}
var AIBuilderToolStripAfter = Key{Flag: "rc-ai-builder-tool-strip-after", Fallback: 5}

// --- Category 4: AI Gateway ---

var GatewayModelsFree = Key{Flag: "rc-gateway-models-free", Fallback: "gpt-4.1-mini,claude-haiku-4-5"}
var GatewayModelsStarter = Key{Flag: "rc-gateway-models-starter", Fallback: "gpt-4.1-mini,claude-haiku-4-5,fal-ai/flux/schnell"}
var GatewayModelsPro = Key{Flag: "rc-gateway-models-pro", Fallback: "gpt-4.1-mini,gpt-4.1,claude-haiku-4-5,claude-sonnet-4.6,fal-ai/flux/schnell,fal-ai/flux-pro/v1.1"}
var GatewayModelsBusiness = Key{Flag: "rc-gateway-models-business", Fallback: "gpt-4.1-mini,gpt-4.1,claude-haiku-4-5,claude-sonnet-4.6,claude-opus-4.7,gemini-2.5-pro,fal-ai/flux/schnell,fal-ai/flux-pro/v1.1"}

var GatewayRPMFree = Key{Flag: "rc-gateway-rpm-free", Fallback: 10}
var GatewayRPMStarter = Key{Flag: "rc-gateway-rpm-starter", Fallback: 30}
var GatewayRPMPro = Key{Flag: "rc-gateway-rpm-pro", Fallback: 60}
var GatewayRPMBusiness = Key{Flag: "rc-gateway-rpm-business", Fallback: 120}

var GatewayImageCostFluxSchnell = Key{Flag: "rc-gateway-image-cost-flux-schnell", Fallback: 5}
var GatewayImageCostFluxPro = Key{Flag: "rc-gateway-image-cost-flux-pro", Fallback: 15}
var GatewayImageCostFluxUltra = Key{Flag: "rc-gateway-image-cost-flux-ultra", Fallback: 40}

var GatewayChatTimeoutSeconds = Key{Flag: "rc-gateway-chat-timeout-seconds", Fallback: 30}
var GatewayImageTimeoutSeconds = Key{Flag: "rc-gateway-image-timeout-seconds", Fallback: 120}

// --- Category 5: Builder LLM Provider / Model (per plan) ---
// These control which LLM provider + model the app builder uses per user plan.
// Format: "provider/model" — e.g. "anthropic/claude-sonnet-4.6" or
// "deepseek/deepseek-v4-flash" (implicit openrouter).

var BuilderProviderModelStarter = Key{Flag: "rc-builder-provider-model-starter", Fallback: "deepseek/deepseek-v4-flash"}
var BuilderProviderModelHobby = Key{Flag: "rc-builder-provider-model-hobby", Fallback: "moonshot/kimi-k2.6"}
var BuilderProviderModelPro = Key{Flag: "rc-builder-provider-model-pro", Fallback: "anthropic/claude-sonnet-4.6"}
var BuilderProviderModelMax = Key{Flag: "rc-builder-provider-model-max", Fallback: "anthropic/claude-opus-4.7"}

// Gateway chat pricing — cost-based (computed from upstream OpenRouter USD
// prices in the model catalog), not token-based. Markup is an integer
// percent on top of the raw upstream cost (5 = 5%). Credit reference
// price is in hundredths of a cent so the value fits GetInt — set to 20
// for $0.20/credit (matches the Starter/Pro plan implied price). Raise
// the reference price to capture more margin, lower it to subsidize
// gateway users against heavier package discounts.
var GatewayMarkupPercent = Key{Flag: "rc-gateway-markup-percent", Fallback: 5}
var GatewayCreditPriceUSDCents = Key{Flag: "rc-gateway-credit-price-usd-cents", Fallback: 20}
