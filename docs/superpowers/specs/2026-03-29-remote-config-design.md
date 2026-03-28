# Remote Config via PostHog Feature Flags

## Problem

Business configuration values (credit amounts, referral rewards, pricing, AI tuning parameters, platform limits) are hardcoded as Go constants. Changing any value requires a code change and redeploy. There is no way to:
- Change values remotely from a dashboard
- A/B test different values per user
- Gradually roll out changes to a subset of users

## Solution

A `RemoteConfig` service that wraps PostHog feature flags to provide typed, cached, fallback-safe access to configuration values. Every config key has a hardcoded fallback so the system works identically when no flags are configured in PostHog.

## Architecture

```
PostHog Dashboard (feature flags + experiments)
        │
        │ local evaluation (PersonalApiKey)
        ▼
RemoteConfig Service (internal/services/remoteconfig/)
├── In-memory cache (sync.Map, 5-min TTL)
├── Typed getters: GetInt(), GetString(), GetBool()
├── Hardcoded fallbacks per key
└── nil-safe: returns fallbacks when PostHog is disabled
        │
        ▼
Controller (RemoteConfig field)
        │
        ▼
Views / Controllers (call sites)
```

### Key Design Decisions

- **PostHog feature flags** chosen over alternatives (DB config table, LaunchDarkly, Infisical) because PostHog is already integrated, provides per-user targeting, A/B experiments with statistical analysis, and costs nothing extra.
- **One flag per value** (not grouped JSON payloads) — each value can be independently A/B tested or changed.
- **Hardcoded fallbacks + in-memory cache** — if PostHog is nil, unreachable, or returns unexpected types, the fallback is returned silently. Cache avoids per-request overhead.
- **Progressive adoption** — zero flags need to exist in PostHog for the system to work. Create flags only when you want to change a value or run an experiment.
- **PostHog local evaluation** — adding `PersonalApiKey` to PostHog config enables local flag evaluation (flag definitions fetched once, evaluated in-memory). Near-zero latency, works during brief PostHog outages.

## Package Structure

```
internal/services/remoteconfig/
├── remoteconfig.go     # Service struct, cache, typed getters
└── keys.go             # All key constants + fallback defaults
```

### Service API

```go
type Service struct {
    posthog posthog.Client   // nil-safe
    cache   sync.Map         // "key:userID" → cachedEntry{value, expiresAt}
    ttl     time.Duration    // 5 minutes default
}

func New(posthogClient posthog.Client) *Service
func (s *Service) GetInt(key, userID string) int
func (s *Service) GetString(key, userID string) string
func (s *Service) GetBool(key, userID string) bool
func (s *Service) GetVariant(key, userID string) string
```

Each key constant in `keys.go` includes its PostHog flag name and fallback value. The typed getters look up the key's fallback internally — callers don't pass fallbacks manually.

For global (non-user-scoped) config, pass `remoteconfig.Global` (`"__global__"`) as userID.

## Config Keys

### Flag Naming Convention

All flags prefixed with `rc-` in PostHog to distinguish from future feature flags.

### Category 1: Business & Pricing

| Key Constant | PostHog Flag | Type | Fallback | Source |
|---|---|---|---|---|
| `ReferralSignupCredits` | `rc-referral-signup-credits` | int | 10 | `models/referral.go:33` |
| `ReferralSubscriptionCredits` | `rc-referral-subscription-credits` | int | 50 | `models/referral.go:34` |
| `CreditPack100Credits` | `rc-credit-pack-100-credits` | int | 100 | `views/checkout.go:26` |
| `CreditPack100Price` | `rc-credit-pack-100-price` | int | 15 | `views/checkout.go:26` |
| `CreditPack200Credits` | `rc-credit-pack-200-credits` | int | 200 | `views/checkout.go:27` |
| `CreditPack200Price` | `rc-credit-pack-200-price` | int | 30 | `views/checkout.go:27` |
| `AICreditsTokensPerCredit` | `rc-ai-credits-tokens-per-credit` | int | 5000 | `models/ai_credits.go:53` |
| `AICreditsOutputMultiplier` | `rc-ai-credits-output-multiplier` | int | 5 | `models/ai_credits.go:54` |
| `AICreditsMinBalance` | `rc-ai-credits-min-balance` | int | -100 | `models/ai_credits.go:48` |

### Category 2: Platform Limits

| Key Constant | PostHog Flag | Type | Fallback | Source |
|---|---|---|---|---|
| `MaxConcurrentBuilds` | `rc-max-concurrent-builds` | int | 1 | `constants/settings.go:4` |
| `MaxDomainsPerProject` | `rc-max-domains-per-project` | int | 10 | `constants/settings.go:16` |
| `MaxConcurrentJobsPerUser` | `rc-max-concurrent-jobs-per-user` | int | 3 | `controllers/generation_jobs.go:32` |
| `MaxAssetFileSizeMB` | `rc-max-asset-file-size-mb` | int | 50 | `views/app_asset_views.go:18` |
| `MaxMobileContentSizeKB` | `rc-max-mobile-content-size-kb` | int | 50 | `views/mobile_chat_views.go:134` |
| `MaxQueryRows` | `rc-max-query-rows` | int | 1000 | `controllers/app_dashboard_query_controller.go:20` |

### Category 3: AI Generation Tuning

| Key Constant | PostHog Flag | Type | Fallback | Source |
|---|---|---|---|---|
| `AIMaxChatMessages` | `rc-ai-max-chat-messages` | int | 200 | `controllers/mobile_chat_controller.go:81` |
| `AIChatKeepRecent` | `rc-ai-chat-keep-recent` | int | 10 | `controllers/app_builder_controller.go:486` |
| `AIChatSummarizeThreshold` | `rc-ai-chat-summarize-threshold` | int | 15 | `controllers/app_builder_controller.go:487` |
| `AIMaxStructureFiles` | `rc-ai-max-structure-files` | int | 25 | `constants/ai_structure.go:69` |
| `AIBuilderMaxAutoInline` | `rc-ai-builder-max-auto-inline` | int | 15 | `controllers/app_builder_context.go:484` |
| `AIBuilderMaxExpandedFiles` | `rc-ai-builder-max-expanded-files` | int | 25 | `controllers/app_builder_context.go:587` |
| `AIBuilderMaxFileChars` | `rc-ai-builder-max-file-chars` | int | 150000 | `controllers/app_builder_context.go:723` |
| `AIValidationMaxFiles` | `rc-ai-validation-max-files` | int | 15 | `controllers/app_builder_validation.go:332` |
| `AIValidationMaxChars` | `rc-ai-validation-max-chars` | int | 60000 | `controllers/app_builder_validation.go:333` |
| `AIBuilderToolMaxRounds` | `rc-ai-builder-tool-max-rounds` | int | 30 | `controllers/app_builder_tools.go:288` |
| `AIGenerationMaxRounds` | `rc-ai-generation-max-rounds` | int | 8 | `controllers/app_builder_generation.go:646` |

**Total: 22 keys** (9 business/pricing + 6 platform limits + 7 AI tuning)

## Migration Strategy

### Phase 1: Build the service (no behavior change)

- Create `internal/services/remoteconfig/` package with Service, cache, typed getters
- Define all 22 keys with fallback values in `keys.go`
- Add `RemoteConfig *remoteconfig.Service` to `Controller` struct
- Wire up in `main.go`: create service, pass to `NewController()`, add `PersonalApiKey` to PostHog config

### Phase 2: Migrate call sites (no behavior change)

Replace direct constant references with `RemoteConfig.GetInt(...)` calls across all files. Existing constants stay in place as the fallback values defined in `keys.go`.

**Files to modify:**

| File | Change |
|---|---|
| `internal/controllers/controller.go` | Add `RemoteConfig` field + constructor param |
| `cmd/main.go` | Create service, pass to controller, add PersonalApiKey |
| `internal/controllers/referrals.go` | 4 call sites: signup + subscription credits for both parties |
| `internal/controllers/ai_credits.go` | `AICreditsMinBalance` reference |
| `internal/controllers/generation_jobs.go` | `maxConcurrentJobsPerUser` |
| `internal/controllers/mobile_chat_controller.go` | `maxMessages`, `keepRecent`, `summarizeThreshold` |
| `internal/controllers/app_builder_controller.go` | `keepRecent`, `summarizeThreshold` |
| `internal/controllers/app_builder_context.go` | `maxAutoInline`, `maxExpandedFiles`, `maxFileChars` |
| `internal/controllers/app_builder_validation.go` | `maxIncludedFiles`, `maxIncludedChars` |
| `internal/controllers/app_builder_tools.go` | `MaxRounds: 30` |
| `internal/controllers/app_builder_generation.go` | `MaxRounds: 8` |
| `internal/controllers/app_dashboard_query_controller.go` | `maxQueryRows` |
| `internal/views/checkout.go` | Credit packs built dynamically from remote values |
| `internal/views/app_asset_views.go` | `maxAssetFileSize` |
| `internal/views/mobile_chat_views.go` | `maxContentLen` |
| `internal/constants/settings.go` | Keep constants (used as fallbacks in keys.go) |
| `internal/constants/ai_structure.go` | Parameterize "maximum 25 files" via `fmt.Sprintf` |
| `internal/models/ai_credits.go` | Add `TokensToCreditsWithConfig()` variant; keep original |
| `internal/models/referral.go` | Keep constants (used as fallbacks in keys.go) |

### Phase 3: Use it (dashboard-driven, no code changes)

- Create flags in PostHog when you want to change values
- Run experiments when you want to A/B test
- Roll back by deleting the flag (reverts to hardcoded fallback)

## Special Cases

### TokensToCredits

`TokensToCredits()` in `models/ai_credits.go` is called from 8+ locations. Add a `TokensToCreditsWithConfig(prompt, completion, tokensPerCredit, outputMultiplier int)` variant. Callers with access to `Controller` pass remote values; the original function stays as a convenience using hardcoded defaults.

### AI Structure Prompt

The "maximum 25 files" limit is embedded in prompt text in `constants/ai_structure.go`. Change to a function that takes the limit as a parameter and uses `fmt.Sprintf` to inject it into the prompt string.

### Credit Packs in checkout.go

The `creditPacks` slice is currently a package-level `var`. Migration converts it to a method on `HttpViews` that builds the slice dynamically from remote config values, since packs need access to the `Controller`'s `RemoteConfig`.

### Non-user-scoped Keys

Keys like `MaxConcurrentBuilds` don't vary per user. These use `remoteconfig.Global` (`"__global__"`) as the distinct ID. Still cacheable, still remotely configurable, just not A/B testable per user.

## Flutter Template

The Flutter mobile template already fetches per-project config from the backend API via `ConfigService.fetchConfig()`. No changes needed on the Flutter side. Internal values like ping intervals and buffer sizes are implementation details, not product config.

## Not In Scope

- Admin UI for managing config (PostHog dashboard serves this purpose)
- Config versioning or audit trail (PostHog tracks flag change history)
- Complex flag targeting rules (available in PostHog but not needed initially)
- Creating all 22 flags in PostHog upfront (progressive adoption — create when needed)
