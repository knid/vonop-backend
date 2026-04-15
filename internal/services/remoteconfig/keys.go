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
// Entry pack — 25% savings over the base rate ($15 vs $20 nominal).
var CreditPack200Credits = Key{Flag: "rc-credit-pack-200-credits", Fallback: 200}
var CreditPack200Price = Key{Flag: "rc-credit-pack-200-price", Fallback: 15}
// Small-mid pack — 28% savings over the base rate ($36 vs $50 nominal).
var CreditPack500Credits = Key{Flag: "rc-credit-pack-500-credits", Fallback: 500}
var CreditPack500Price = Key{Flag: "rc-credit-pack-500-price", Fallback: 36}
// Mid pack — 30% savings over the base rate ($70 vs $100 nominal).
var CreditPack1000Credits = Key{Flag: "rc-credit-pack-1000-credits", Fallback: 1000}
var CreditPack1000Price = Key{Flag: "rc-credit-pack-1000-price", Fallback: 70}
// Power pack — ~33% savings over the base rate ($200 vs $300 nominal).
var CreditPack3000Credits = Key{Flag: "rc-credit-pack-3000-credits", Fallback: 3000}
var CreditPack3000Price = Key{Flag: "rc-credit-pack-3000-price", Fallback: 200}
var AICreditsTokensPerCredit = Key{Flag: "rc-ai-credits-tokens-per-credit", Fallback: 5000}
var AICreditsOutputMultiplier = Key{Flag: "rc-ai-credits-output-multiplier", Fallback: 5}
var AICreditsMinBalance = Key{Flag: "rc-ai-credits-min-balance", Fallback: -100}

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

var MaxConcurrentBuilds = Key{Flag: "rc-max-concurrent-builds", Fallback: 1}
var MaxDomainsPerProject = Key{Flag: "rc-max-domains-per-project", Fallback: 10}
var MaxConcurrentJobsPerUser = Key{Flag: "rc-max-concurrent-jobs-per-user", Fallback: 3}
var MaxAssetFileSizeMB = Key{Flag: "rc-max-asset-file-size-mb", Fallback: 50}
var MaxMobileContentSizeKB = Key{Flag: "rc-max-mobile-content-size-kb", Fallback: 50}
var MaxQueryRows = Key{Flag: "rc-max-query-rows", Fallback: 1000}

// --- Category 3: AI Generation Tuning ---

var AIMaxChatMessages = Key{Flag: "rc-ai-max-chat-messages", Fallback: 200}
var AIChatKeepRecent = Key{Flag: "rc-ai-chat-keep-recent", Fallback: 10}
var AIChatSummarizeThreshold = Key{Flag: "rc-ai-chat-summarize-threshold", Fallback: 15}
var AIMaxStructureFiles = Key{Flag: "rc-ai-max-structure-files", Fallback: 25}
var AIBuilderMaxAutoInline = Key{Flag: "rc-ai-builder-max-auto-inline", Fallback: 15}
var AIBuilderMaxExpandedFiles = Key{Flag: "rc-ai-builder-max-expanded-files", Fallback: 25}
var AIBuilderMaxFileChars = Key{Flag: "rc-ai-builder-max-file-chars", Fallback: 150000}
var AIValidationMaxFiles = Key{Flag: "rc-ai-validation-max-files", Fallback: 15}
var AIValidationMaxChars = Key{Flag: "rc-ai-validation-max-chars", Fallback: 60000}
var AIBuilderToolMaxRounds = Key{Flag: "rc-ai-builder-tool-max-rounds", Fallback: 30}
var AIGenerationMaxRounds = Key{Flag: "rc-ai-generation-max-rounds", Fallback: 8}

// --- Category 4: AI Gateway ---

var GatewayModelsFree = Key{Flag: "rc-gateway-models-free", Fallback: "gpt-4.1-mini,claude-haiku-4-5"}
var GatewayModelsStarter = Key{Flag: "rc-gateway-models-starter", Fallback: "gpt-4.1-mini,claude-haiku-4-5,fal-ai/flux-schnell"}
var GatewayModelsPro = Key{Flag: "rc-gateway-models-pro", Fallback: "gpt-4.1-mini,gpt-4.1,claude-haiku-4-5,claude-sonnet-4,fal-ai/flux-schnell,fal-ai/flux-pro/v1.1"}
var GatewayModelsBusiness = Key{Flag: "rc-gateway-models-business", Fallback: "gpt-4.1-mini,gpt-4.1,claude-haiku-4-5,claude-sonnet-4,claude-opus-4,gemini-2.5-pro,fal-ai/flux-schnell,fal-ai/flux-pro/v1.1"}

var GatewayRPMFree = Key{Flag: "rc-gateway-rpm-free", Fallback: 10}
var GatewayRPMStarter = Key{Flag: "rc-gateway-rpm-starter", Fallback: 30}
var GatewayRPMPro = Key{Flag: "rc-gateway-rpm-pro", Fallback: 60}
var GatewayRPMBusiness = Key{Flag: "rc-gateway-rpm-business", Fallback: 120}

var GatewayImageCostFluxSchnell = Key{Flag: "rc-gateway-image-cost-flux-schnell", Fallback: 5}
var GatewayImageCostFluxPro = Key{Flag: "rc-gateway-image-cost-flux-pro", Fallback: 15}
var GatewayImageCostFluxUltra = Key{Flag: "rc-gateway-image-cost-flux-ultra", Fallback: 40}

var GatewayChatTimeoutSeconds = Key{Flag: "rc-gateway-chat-timeout-seconds", Fallback: 30}
var GatewayImageTimeoutSeconds = Key{Flag: "rc-gateway-image-timeout-seconds", Fallback: 120}

// Gateway chat pricing — cost-based (computed from upstream OpenRouter USD
// prices in the model catalog), not token-based. Markup is an integer
// percent on top of the raw upstream cost (5 = 5%). Credit reference
// price is in hundredths of a cent so the value fits GetInt — set to 20
// for $0.20/credit (matches the Starter/Pro plan implied price). Raise
// the reference price to capture more margin, lower it to subsidize
// gateway users against heavier package discounts.
var GatewayMarkupPercent = Key{Flag: "rc-gateway-markup-percent", Fallback: 5}
var GatewayCreditPriceUSDCents = Key{Flag: "rc-gateway-credit-price-usd-cents", Fallback: 20}
