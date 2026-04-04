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
var CreditPack100Credits = Key{Flag: "rc-credit-pack-100-credits", Fallback: 100}
var CreditPack100Price = Key{Flag: "rc-credit-pack-100-price", Fallback: 15}
var CreditPack200Credits = Key{Flag: "rc-credit-pack-200-credits", Fallback: 200}
var CreditPack200Price = Key{Flag: "rc-credit-pack-200-price", Fallback: 30}
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
