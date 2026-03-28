# Remote Config via PostHog Feature Flags — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace 22 hardcoded business/AI config values with a PostHog feature-flag-backed RemoteConfig service, enabling remote changes and A/B testing without deploys.

**Architecture:** New `internal/services/remoteconfig/` package wrapping PostHog Go SDK feature flags with in-memory caching (5-min TTL) and hardcoded fallbacks. Injected into `Controller` like other services. Call sites change from `models.SomeConst` to `c.RemoteConfig.GetInt(remoteconfig.SomeKey, userID)`.

**Tech Stack:** Go, PostHog Go SDK v1.10.0 (already in go.mod), sync.Map for cache

**Spec:** `docs/superpowers/specs/2026-03-29-remote-config-design.md`

---

## File Structure

**New files:**
- `internal/services/remoteconfig/remoteconfig.go` — Service struct, cache, typed getters (GetInt, GetString, GetBool, GetVariant)
- `internal/services/remoteconfig/keys.go` — 22 key definitions with PostHog flag names and fallback values

**Modified files (Phase 1 — wiring):**
- `internal/controllers/controller.go` — Add `RemoteConfig` field + constructor param
- `cmd/main.go` — Create remoteconfig.Service, add PersonalApiKey to PostHog config, pass to NewController

**Modified files (Phase 2 — migration, grouped by domain):**
- `internal/models/ai_credits.go` — Add `TokensToCreditsWithConfig()` variant
- `internal/controllers/referrals.go` — 4 credit amount call sites
- `internal/controllers/ai_credits.go` — `AICreditsMinBalance` + token conversion
- `internal/views/checkout.go` — Dynamic credit packs from remote config
- `internal/controllers/generation_jobs.go` — `maxConcurrentJobsPerUser`
- `internal/views/app_asset_views.go` — `maxAssetFileSize`
- `internal/views/mobile_chat_views.go` — `maxContentLen`
- `internal/controllers/app_dashboard_query_controller.go` — `maxQueryRows`
- `internal/controllers/mobile_chat_controller.go` — `maxMessages`, `keepRecent`, `summarizeThreshold`
- `internal/controllers/app_builder_controller.go` — `keepRecent`, `summarizeThreshold`
- `internal/controllers/app_builder_context.go` — `maxAutoInline`, `maxExpandedFiles`, `maxFileChars`
- `internal/controllers/app_builder_validation.go` — `maxIncludedFiles`, `maxIncludedChars`
- `internal/controllers/app_builder_tools.go` — `MaxRounds: 30` and `MaxRounds: 3`
- `internal/controllers/app_builder_generation.go` — `MaxRounds: 8`
- `internal/constants/ai_structure.go` — Parameterize "maximum 25 files" in prompts
- `internal/controllers/app_codegen_controller.go` — Pass maxStructureFiles into prompt function

---

### Task 1: Create remoteconfig package — keys.go

**Files:**
- Create: `internal/services/remoteconfig/keys.go`

- [ ] **Step 1: Create the keys.go file**

```go
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
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /home/knid/Projects/wemob/wemob-backend && go build ./internal/services/remoteconfig/`
Expected: No errors (package compiles but isn't imported yet)

- [ ] **Step 3: Commit**

```bash
git add internal/services/remoteconfig/keys.go
git commit -m "feat(remoteconfig): define 22 config keys with fallbacks"
```

---

### Task 2: Create remoteconfig package — remoteconfig.go

**Files:**
- Create: `internal/services/remoteconfig/remoteconfig.go`

- [ ] **Step 1: Create the remoteconfig.go file**

```go
package remoteconfig

import (
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/posthog/posthog-go"
)

type cachedEntry struct {
	value     interface{}
	expiresAt time.Time
}

// Service provides typed, cached access to PostHog feature flag payloads
// with hardcoded fallbacks. Safe to use when PostHog is nil (returns fallbacks).
type Service struct {
	posthog posthog.Client
	cache   sync.Map
	ttl     time.Duration
}

// New creates a RemoteConfig service. posthogClient may be nil (fallback-only mode).
func New(posthogClient posthog.Client) *Service {
	return &Service{
		posthog: posthogClient,
		ttl:     5 * time.Minute,
	}
}

// GetInt returns the integer value of a feature flag payload, or the key's fallback.
func (s *Service) GetInt(key Key, userID string) int {
	fallback, _ := key.Fallback.(int)
	raw := s.getPayload(key.Flag, userID)
	if raw == nil {
		return fallback
	}
	switch v := raw.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

// GetString returns the string value of a feature flag payload, or the key's fallback.
func (s *Service) GetString(key Key, userID string) string {
	fallback, _ := key.Fallback.(string)
	raw := s.getPayload(key.Flag, userID)
	if raw == nil {
		return fallback
	}
	if v, ok := raw.(string); ok {
		return v
	}
	return fallback
}

// GetBool returns the boolean value of a feature flag payload, or the key's fallback.
func (s *Service) GetBool(key Key, userID string) bool {
	fallback, _ := key.Fallback.(bool)
	raw := s.getPayload(key.Flag, userID)
	if raw == nil {
		return fallback
	}
	if v, ok := raw.(bool); ok {
		return v
	}
	return fallback
}

// GetVariant returns the raw feature flag variant (e.g. "control", "test").
// Returns empty string if PostHog is nil or the flag doesn't exist.
func (s *Service) GetVariant(key Key, userID string) string {
	if s.posthog == nil {
		return ""
	}

	cacheKey := "variant:" + key.Flag + ":" + userID
	if entry, ok := s.cache.Load(cacheKey); ok {
		if ce := entry.(cachedEntry); time.Now().Before(ce.expiresAt) {
			if v, ok := ce.value.(string); ok {
				return v
			}
		}
	}

	variant, err := s.posthog.GetFeatureFlag(posthog.FeatureFlagPayload{
		Key:        key.Flag,
		DistinctId: userID,
	})
	if err != nil {
		slog.Debug("remoteconfig: GetVariant error", "flag", key.Flag, "error", err)
		return ""
	}

	result := fmt.Sprint(variant)
	s.cache.Store(cacheKey, cachedEntry{value: result, expiresAt: time.Now().Add(s.ttl)})
	return result
}

// getPayload fetches the feature flag payload from PostHog with caching.
// Returns nil on any error or when PostHog is disabled.
func (s *Service) getPayload(flag string, userID string) interface{} {
	if s.posthog == nil {
		return nil
	}

	cacheKey := flag + ":" + userID
	if entry, ok := s.cache.Load(cacheKey); ok {
		if ce := entry.(cachedEntry); time.Now().Before(ce.expiresAt) {
			return ce.value
		}
	}

	payload, err := s.posthog.GetFeatureFlagPayload(posthog.FeatureFlagPayload{
		Key:        flag,
		DistinctId: userID,
	})
	if err != nil {
		slog.Debug("remoteconfig: getPayload error", "flag", flag, "error", err)
		return nil
	}

	s.cache.Store(cacheKey, cachedEntry{value: payload, expiresAt: time.Now().Add(s.ttl)})
	return payload
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /home/knid/Projects/wemob/wemob-backend && go build ./internal/services/remoteconfig/`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add internal/services/remoteconfig/remoteconfig.go
git commit -m "feat(remoteconfig): implement Service with cache and typed getters"
```

---

### Task 3: Wire RemoteConfig into Controller and main.go

**Files:**
- Modify: `internal/controllers/controller.go` — Add RemoteConfig field + constructor param
- Modify: `cmd/main.go` — Create service, add PersonalApiKey, pass to NewController

- [ ] **Step 1: Add RemoteConfig to Controller struct and import**

In `internal/controllers/controller.go`, add the import:
```go
"github.com/knid/wemob/internal/services/remoteconfig"
```

Add field to the `Controller` struct (after the `PostHog` field, line 42):
```go
RemoteConfig           *remoteconfig.Service
```

Add parameter to `NewController` function signature (after `codemagicService *codemagic.Service`):
```go
remoteConfigService *remoteconfig.Service
```

Add assignment in the return block (after `PostHog: posthogClient,`):
```go
RemoteConfig:           remoteConfigService,
```

- [ ] **Step 2: Add PersonalApiKey to PostHog config in main.go**

In `cmd/main.go`, find the PostHog initialization block (around line 747). Change:
```go
phClient, phErr := posthog.NewWithConfig(phKey, posthog.Config{
    Endpoint: "https://eu.i.posthog.com",
})
```
To:
```go
phClient, phErr := posthog.NewWithConfig(phKey, posthog.Config{
    Endpoint:       "https://eu.i.posthog.com",
    PersonalApiKey: sm.GetSecret("POSTHOG_PERSONAL_API_KEY"),
})
```

- [ ] **Step 3: Create RemoteConfig service and pass to NewController in main.go**

In `cmd/main.go`, add the import:
```go
"github.com/knid/wemob/internal/services/remoteconfig"
```

After the PostHog initialization block (after `posthogClient = phClient`, around line 755), add:
```go
remoteConfigService := remoteconfig.New(posthogClient)
slog.Info("RemoteConfig service initialized")
```

Then add `remoteConfigService` as the last argument in the `controllers.NewController(...)` call (after `codemagicService,`, line 804):
```go
remoteConfigService,
```

- [ ] **Step 4: Verify it compiles**

Run: `cd /home/knid/Projects/wemob/wemob-backend && go build ./cmd/main.go`
Expected: No errors

- [ ] **Step 5: Commit**

```bash
git add internal/controllers/controller.go cmd/main.go
git commit -m "feat(remoteconfig): wire Service into Controller and main.go"
```

---

### Task 4: Migrate referral credits

**Files:**
- Modify: `internal/controllers/referrals.go`

- [ ] **Step 1: Add remoteconfig import**

In `internal/controllers/referrals.go`, add to imports:
```go
"github.com/knid/wemob/internal/services/remoteconfig"
```

- [ ] **Step 2: Migrate GetReferralStats (display totals)**

In `GetReferralStats` (around line 70), replace:
```go
totalRewards += models.ReferralSignupCredits
```
With:
```go
totalRewards += c.RemoteConfig.GetInt(remoteconfig.ReferralSignupCredits, user.ID.String())
```

And replace (around line 73):
```go
totalRewards += models.ReferralSubscriptionCredits
```
With:
```go
totalRewards += c.RemoteConfig.GetInt(remoteconfig.ReferralSubscriptionCredits, user.ID.String())
```

- [ ] **Step 3: Migrate ApplyReferralCode (signup credits)**

In `ApplyReferralCode` (around line 118), replace:
```go
if err := c.AddAICredits(referrer, models.ReferralSignupCredits, "referral_signup_reward", user.ID.String()); err != nil {
```
With:
```go
signupCredits := c.RemoteConfig.GetInt(remoteconfig.ReferralSignupCredits, referrer.ID.String())
if err := c.AddAICredits(referrer, signupCredits, "referral_signup_reward", user.ID.String()); err != nil {
```

And replace (around line 122):
```go
if err := c.AddAICredits(user, models.ReferralSignupCredits, "referral_signup_bonus", code.UserID.String()); err != nil {
```
With:
```go
signupBonus := c.RemoteConfig.GetInt(remoteconfig.ReferralSignupCredits, user.ID.String())
if err := c.AddAICredits(user, signupBonus, "referral_signup_bonus", code.UserID.String()); err != nil {
```

- [ ] **Step 4: Migrate GrantReferralSubscriptionReward (subscription credits)**

In `GrantReferralSubscriptionReward` (around line 153), replace:
```go
if err := c.AddAICredits(referrer, models.ReferralSubscriptionCredits, "referral_subscription_reward", user.ID.String()); err != nil {
```
With:
```go
subCredits := c.RemoteConfig.GetInt(remoteconfig.ReferralSubscriptionCredits, referrer.ID.String())
if err := c.AddAICredits(referrer, subCredits, "referral_subscription_reward", user.ID.String()); err != nil {
```

And replace (around line 159):
```go
if err := c.AddAICredits(user, models.ReferralSubscriptionCredits, "referral_subscription_bonus", referral.ReferrerID.String()); err != nil {
```
With:
```go
subBonus := c.RemoteConfig.GetInt(remoteconfig.ReferralSubscriptionCredits, user.ID.String())
if err := c.AddAICredits(user, subBonus, "referral_subscription_bonus", referral.ReferrerID.String()); err != nil {
```

- [ ] **Step 5: Verify it compiles**

Run: `cd /home/knid/Projects/wemob/wemob-backend && go build ./cmd/main.go`
Expected: No errors

- [ ] **Step 6: Commit**

```bash
git add internal/controllers/referrals.go
git commit -m "feat(remoteconfig): migrate referral credits to remote config"
```

---

### Task 5: Migrate AI credits (min balance + token conversion)

**Files:**
- Modify: `internal/models/ai_credits.go` — Add `TokensToCreditsWithConfig()`
- Modify: `internal/controllers/ai_credits.go` — Use remote min balance + token config

- [ ] **Step 1: Add TokensToCreditsWithConfig to models/ai_credits.go**

After the existing `TokensToCredits` function (after line 64), add:

```go
// TokensToCreditsWithConfig is like TokensToCredits but uses caller-provided
// conversion parameters from remote config.
func TokensToCreditsWithConfig(promptTokens, completionTokens, tokensPerCredit, outputMultiplier int) int {
	weighted := promptTokens + (completionTokens * outputMultiplier)
	if weighted <= 0 {
		return 0
	}
	return (weighted + tokensPerCredit - 1) / tokensPerCredit
}
```

- [ ] **Step 2: Migrate DeductAICreditsUnchecked to use remote AICreditsMinBalance**

In `internal/controllers/ai_credits.go`, add to imports:
```go
"github.com/knid/wemob/internal/services/remoteconfig"
```

In `DeductAICreditsUnchecked` (around line 152), replace:
```go
newBalance, err = c.DB.AtomicDeductCreditsUnchecked(tx, balance.ID.String(), cost, models.AICreditsMinBalance)
```
With:
```go
minBalance := c.RemoteConfig.GetInt(remoteconfig.AICreditsMinBalance, user.ID.String())
newBalance, err = c.DB.AtomicDeductCreditsUnchecked(tx, balance.ID.String(), cost, minBalance)
```

And replace (around line 155):
```go
return fmt.Errorf("AI credits overdraft limit reached (floor: %d)", models.AICreditsMinBalance)
```
With:
```go
return fmt.Errorf("AI credits overdraft limit reached (floor: %d)", minBalance)
```

- [ ] **Step 3: Verify it compiles**

Run: `cd /home/knid/Projects/wemob/wemob-backend && go build ./cmd/main.go`
Expected: No errors

- [ ] **Step 4: Commit**

```bash
git add internal/models/ai_credits.go internal/controllers/ai_credits.go
git commit -m "feat(remoteconfig): migrate AI credits min balance and add TokensToCreditsWithConfig"
```

---

### Task 6: Migrate credit packs (checkout.go)

**Files:**
- Modify: `internal/views/checkout.go`

- [ ] **Step 1: Add remoteconfig import and convert creditPacks to a method**

In `internal/views/checkout.go`, add to imports:
```go
"github.com/knid/wemob/internal/services/remoteconfig"
```

Replace the package-level `creditPacks` variable (lines 25-28):
```go
var creditPacks = []creditPack{
	{ID: "credits_100", Secret: "STRIPE_CREDITS_100_PRICE_ID", Name: "100 AI Credits", Credits: 100, Price: 15},
	{ID: "credits_200", Secret: "STRIPE_CREDITS_200_PRICE_ID", Name: "200 AI Credits", Credits: 200, Price: 30, Popular: true},
}
```
With a method on HttpViews:
```go
func (v *HttpViews) getCreditPacks(userID string) []creditPack {
	rc := v.Controller.RemoteConfig
	c100 := rc.GetInt(remoteconfig.CreditPack100Credits, userID)
	p100 := rc.GetInt(remoteconfig.CreditPack100Price, userID)
	c200 := rc.GetInt(remoteconfig.CreditPack200Credits, userID)
	p200 := rc.GetInt(remoteconfig.CreditPack200Price, userID)
	return []creditPack{
		{ID: "credits_100", Secret: "STRIPE_CREDITS_100_PRICE_ID", Name: fmt.Sprintf("%d AI Credits", c100), Credits: c100, Price: p100},
		{ID: "credits_200", Secret: "STRIPE_CREDITS_200_PRICE_ID", Name: fmt.Sprintf("%d AI Credits", c200), Credits: c200, Price: p200, Popular: true},
	}
}
```

- [ ] **Step 2: Update creditPackByID to accept packs parameter**

Replace the `creditPackByID` method (lines 31-42):
```go
func (v *HttpViews) creditPackByID(packageID string) (*creditPack, string) {
	for _, p := range creditPacks {
```
With:
```go
func (v *HttpViews) creditPackByID(packageID string, packs []creditPack) (*creditPack, string) {
	for _, p := range packs {
```

- [ ] **Step 3: Update GetCreditPackages to use getCreditPacks**

In `GetCreditPackages` (around line 128), replace:
```go
for _, p := range creditPacks {
```
With:
```go
for _, p := range v.getCreditPacks(remoteconfig.Global) {
```

Note: `GetCreditPackages` is unauthenticated — use `remoteconfig.Global` here. Per-user pricing A/B testing can be added later by extracting the optional auth token.

- [ ] **Step 4: Update PurchaseCredits to use getCreditPacks**

In `PurchaseCredits` (around line 164), replace:
```go
pack, priceID := v.creditPackByID(req.PackageID)
```
With:
```go
pack, priceID := v.creditPackByID(req.PackageID, v.getCreditPacks(user.ID.String()))
```

- [ ] **Step 5: Verify it compiles**

Run: `cd /home/knid/Projects/wemob/wemob-backend && go build ./cmd/main.go`
Expected: No errors

- [ ] **Step 6: Commit**

```bash
git add internal/views/checkout.go
git commit -m "feat(remoteconfig): migrate credit packs to remote config"
```

---

### Task 7: Migrate platform limits

**Files:**
- Modify: `internal/controllers/generation_jobs.go`
- Modify: `internal/views/app_asset_views.go`
- Modify: `internal/views/mobile_chat_views.go`
- Modify: `internal/controllers/app_dashboard_query_controller.go`

- [ ] **Step 1: Migrate maxConcurrentJobsPerUser in generation_jobs.go**

In `internal/controllers/generation_jobs.go`, add to imports:
```go
"github.com/knid/wemob/internal/services/remoteconfig"
```

In `CreateJob` (around line 41), replace:
```go
if running >= maxConcurrentJobsPerUser {
    return nil, fmt.Errorf("maximum %d concurrent jobs allowed", maxConcurrentJobsPerUser)
```
With:
```go
maxJobs := c.RemoteConfig.GetInt(remoteconfig.MaxConcurrentJobsPerUser, userID.String())
if running >= maxJobs {
    return nil, fmt.Errorf("maximum %d concurrent jobs allowed", maxJobs)
```

The package-level `const maxConcurrentJobsPerUser = 3` can be removed (line 32), since the fallback is defined in `keys.go`.

- [ ] **Step 2: Migrate maxAssetFileSize in app_asset_views.go**

In `internal/views/app_asset_views.go`, add to imports:
```go
"github.com/knid/wemob/internal/services/remoteconfig"
```

In the `UploadAppAsset` handler where `maxAssetFileSize` is used (around line 83), the check is:
```go
if req.FileSize > maxAssetFileSize {
```

Replace with:
```go
maxSizeMB := v.Controller.RemoteConfig.GetInt(remoteconfig.MaxAssetFileSizeMB, user.ID.String())
maxSize := int64(maxSizeMB) * 1024 * 1024
if req.FileSize > maxSize {
```

Also update the error message to use the dynamic value:
```go
utils.JSONErrorResponse(w, fmt.Sprintf("file size exceeds %dMB limit", maxSizeMB), http.StatusBadRequest)
```

Add `"fmt"` to imports if not already present. Remove the package-level `const maxAssetFileSize` (line 18).

- [ ] **Step 3: Migrate maxContentLen in mobile_chat_views.go**

In `internal/views/mobile_chat_views.go`, add to imports:
```go
"github.com/knid/wemob/internal/services/remoteconfig"
```

In `UpdateMobileInjection` (around line 134), replace:
```go
const maxContentLen = 50 * 1024
```
With:
```go
maxContentLen := v.Controller.RemoteConfig.GetInt(remoteconfig.MaxMobileContentSizeKB, user.ID.String()) * 1024
```

Where `user` is already available from earlier in the handler. If the handler doesn't have `user` at that point, use `remoteconfig.Global` instead.

- [ ] **Step 4: Migrate maxQueryRows in app_dashboard_query_controller.go**

In `internal/controllers/app_dashboard_query_controller.go`, add to imports:
```go
"github.com/knid/wemob/internal/services/remoteconfig"
```

The `maxQueryRows` constant (line 20) is used in multiple places in this file. Since the controller methods receive user context, replace each usage. At each call site where `maxQueryRows` is used (lines 467-468, 588, 813-814, 911), replace:
```go
if limit <= 0 || limit > maxQueryRows {
    limit = maxQueryRows
```
With:
```go
maxRows := c.RemoteConfig.GetInt(remoteconfig.MaxQueryRows, remoteconfig.Global)
if limit <= 0 || limit > maxRows {
    limit = maxRows
```

And replace:
```go
if rowCount >= maxQueryRows {
```
With:
```go
if rowCount >= maxRows {
```

Remove the package-level `maxQueryRows` constant (line 20). Keep `supabaseHTTPTimeout` and `maxConcurrentQueries` as-is (operational, not business config).

- [ ] **Step 5: Verify it compiles**

Run: `cd /home/knid/Projects/wemob/wemob-backend && go build ./cmd/main.go`
Expected: No errors

- [ ] **Step 6: Commit**

```bash
git add internal/controllers/generation_jobs.go internal/views/app_asset_views.go internal/views/mobile_chat_views.go internal/controllers/app_dashboard_query_controller.go
git commit -m "feat(remoteconfig): migrate platform limits to remote config"
```

---

### Task 8: Migrate AI chat tuning (mobile + builder)

**Files:**
- Modify: `internal/controllers/mobile_chat_controller.go`
- Modify: `internal/controllers/app_builder_controller.go`

- [ ] **Step 1: Migrate mobile_chat_controller.go**

In `internal/controllers/mobile_chat_controller.go`, add to imports:
```go
"github.com/knid/wemob/internal/services/remoteconfig"
```

Replace the maxMessages block (around lines 81-83):
```go
const maxMessages = 200
if len(messages) >= maxMessages {
    messages = messages[len(messages)-maxMessages+1:]
```
With:
```go
maxMessages := c.RemoteConfig.GetInt(remoteconfig.AIMaxChatMessages, remoteconfig.Global)
if len(messages) >= maxMessages {
    messages = messages[len(messages)-maxMessages+1:]
```

Replace the summarization constants (around lines 126-127):
```go
const keepRecent = 10
const summarizeThreshold = 15
```
With:
```go
keepRecent := c.RemoteConfig.GetInt(remoteconfig.AIChatKeepRecent, remoteconfig.Global)
summarizeThreshold := c.RemoteConfig.GetInt(remoteconfig.AIChatSummarizeThreshold, remoteconfig.Global)
```

- [ ] **Step 2: Migrate app_builder_controller.go**

In `internal/controllers/app_builder_controller.go`, add to imports:
```go
"github.com/knid/wemob/internal/services/remoteconfig"
```

Replace the summarization constants (around lines 486-487):
```go
const keepRecent = 10
const summarizeThreshold = 15
```
With:
```go
keepRecent := c.RemoteConfig.GetInt(remoteconfig.AIChatKeepRecent, remoteconfig.Global)
summarizeThreshold := c.RemoteConfig.GetInt(remoteconfig.AIChatSummarizeThreshold, remoteconfig.Global)
```

- [ ] **Step 3: Verify it compiles**

Run: `cd /home/knid/Projects/wemob/wemob-backend && go build ./cmd/main.go`
Expected: No errors

- [ ] **Step 4: Commit**

```bash
git add internal/controllers/mobile_chat_controller.go internal/controllers/app_builder_controller.go
git commit -m "feat(remoteconfig): migrate AI chat tuning params to remote config"
```

---

### Task 9: Migrate AI builder context limits

**Files:**
- Modify: `internal/controllers/app_builder_context.go`

- [ ] **Step 1: Add remoteconfig import**

In `internal/controllers/app_builder_context.go`, add to imports:
```go
"github.com/knid/wemob/internal/services/remoteconfig"
```

- [ ] **Step 2: Migrate maxAutoInline (line 484)**

Replace:
```go
const maxAutoInline = 15 // if project has ≤ this many files, inline all
```
With:
```go
maxAutoInline := c.RemoteConfig.GetInt(remoteconfig.AIBuilderMaxAutoInline, remoteconfig.Global)
```

- [ ] **Step 3: Migrate maxExpandedFiles (line 587)**

Replace:
```go
const maxExpandedFiles = 25
```
With:
```go
maxExpandedFiles := c.RemoteConfig.GetInt(remoteconfig.AIBuilderMaxExpandedFiles, remoteconfig.Global)
```

- [ ] **Step 4: Migrate maxFileChars (line 723)**

Replace:
```go
const maxFileChars = 150_000
```
With:
```go
maxFileChars := c.RemoteConfig.GetInt(remoteconfig.AIBuilderMaxFileChars, remoteconfig.Global)
```

- [ ] **Step 5: Verify it compiles**

Run: `cd /home/knid/Projects/wemob/wemob-backend && go build ./cmd/main.go`
Expected: No errors

- [ ] **Step 6: Commit**

```bash
git add internal/controllers/app_builder_context.go
git commit -m "feat(remoteconfig): migrate AI builder context limits to remote config"
```

---

### Task 10: Migrate AI validation limits

**Files:**
- Modify: `internal/controllers/app_builder_validation.go`

- [ ] **Step 1: Add remoteconfig import and migrate constants**

In `internal/controllers/app_builder_validation.go`, add to imports:
```go
"github.com/knid/wemob/internal/services/remoteconfig"
```

Replace (around lines 332-333):
```go
const maxIncludedFiles = 15
const maxIncludedChars = 60000
```
With:
```go
maxIncludedFiles := c.RemoteConfig.GetInt(remoteconfig.AIValidationMaxFiles, remoteconfig.Global)
maxIncludedChars := c.RemoteConfig.GetInt(remoteconfig.AIValidationMaxChars, remoteconfig.Global)
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /home/knid/Projects/wemob/wemob-backend && go build ./cmd/main.go`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add internal/controllers/app_builder_validation.go
git commit -m "feat(remoteconfig): migrate AI validation limits to remote config"
```

---

### Task 11: Migrate AI builder tool rounds

**Files:**
- Modify: `internal/controllers/app_builder_tools.go`
- Modify: `internal/controllers/app_builder_generation.go`

- [ ] **Step 1: Migrate MaxRounds in app_builder_tools.go**

In `internal/controllers/app_builder_tools.go`, add to imports:
```go
"github.com/knid/wemob/internal/services/remoteconfig"
```

At the main tool-calling config (around line 288), replace:
```go
MaxRounds:  30,
```
With:
```go
MaxRounds:  c.RemoteConfig.GetInt(remoteconfig.AIBuilderToolMaxRounds, remoteconfig.Global),
```

Note: The `MaxRounds: 3` at line 440 is a fast-model validation call with a fundamentally different purpose. Leave it as a hardcoded `3` — it's not the same tunable as the main builder rounds.

- [ ] **Step 2: Migrate MaxRounds in app_builder_generation.go**

In `internal/controllers/app_builder_generation.go`, add to imports:
```go
"github.com/knid/wemob/internal/services/remoteconfig"
```

At the ecommerce setup config (around line 646), replace:
```go
MaxRounds:  8,
```
With:
```go
MaxRounds:  c.RemoteConfig.GetInt(remoteconfig.AIGenerationMaxRounds, remoteconfig.Global),
```

- [ ] **Step 3: Verify it compiles**

Run: `cd /home/knid/Projects/wemob/wemob-backend && go build ./cmd/main.go`
Expected: No errors

- [ ] **Step 4: Commit**

```bash
git add internal/controllers/app_builder_tools.go internal/controllers/app_builder_generation.go
git commit -m "feat(remoteconfig): migrate AI builder round limits to remote config"
```

---

### Task 12: Parameterize AI structure prompt file limit

**Files:**
- Modify: `internal/constants/ai_structure.go` — Convert prompts to functions
- Modify: `internal/controllers/app_codegen_controller.go` — Pass maxStructureFiles

- [ ] **Step 1: Convert structure prompt constants to functions**

In `internal/constants/ai_structure.go`, the prompts `NEXTJS_STRUCTURE_PROMPT`, `NEXTJS_MOBILE_STRUCTURE_PROMPT` contain hardcoded "maximum 25 files". Convert each from `const` to a function.

For `NEXTJS_STRUCTURE_PROMPT` (line 61), change:
```go
const NEXTJS_STRUCTURE_PROMPT = `You are an expert...
```
To:
```go
func NextjsStructurePrompt(maxFiles int) string {
	return fmt.Sprintf(`You are an expert...
```

Inside the prompt text, replace every occurrence of `maximum 25 files` with `maximum %d files` and `Do NOT exceed 25 files` with `Do NOT exceed %d files`. The `fmt.Sprintf` wraps the entire template literal with `maxFiles, maxFiles` arguments (once for each `%d`).

Do the same for `NEXTJS_MOBILE_STRUCTURE_PROMPT` (line 144) → `NextjsMobileStructurePrompt(maxFiles int) string`.

`NEXTJS_ECOMMERCE_STRUCTURE_PROMPT` and `NEXTJS_SCREENSHOT_TO_FILES_PROMPT` don't contain the "25 files" limit — leave them as constants.

Add `"fmt"` to the package imports if not already present.

- [ ] **Step 2: Update PlanAppFileStructure in app_codegen_controller.go**

In `internal/controllers/app_codegen_controller.go`, add to imports:
```go
"github.com/knid/wemob/internal/services/remoteconfig"
```

In `PlanAppFileStructure` (around line 1865), replace:
```go
systemPrompt := constants.NEXTJS_STRUCTURE_PROMPT
if projectType == "ecommerce" {
    systemPrompt = constants.NEXTJS_ECOMMERCE_STRUCTURE_PROMPT
} else if projectType == "mobile_app" {
    systemPrompt = constants.NEXTJS_MOBILE_STRUCTURE_PROMPT
}
```
With:
```go
maxFiles := c.RemoteConfig.GetInt(remoteconfig.AIMaxStructureFiles, remoteconfig.Global)
systemPrompt := constants.NextjsStructurePrompt(maxFiles)
if projectType == "ecommerce" {
    systemPrompt = constants.NEXTJS_ECOMMERCE_STRUCTURE_PROMPT
} else if projectType == "mobile_app" {
    systemPrompt = constants.NextjsMobileStructurePrompt(maxFiles)
}
```

- [ ] **Step 3: Verify it compiles**

Run: `cd /home/knid/Projects/wemob/wemob-backend && go build ./cmd/main.go`
Expected: No errors

- [ ] **Step 4: Commit**

```bash
git add internal/constants/ai_structure.go internal/controllers/app_codegen_controller.go
git commit -m "feat(remoteconfig): parameterize AI structure prompt file limit"
```

---

### Task 13: Final build verification

- [ ] **Step 1: Full build**

Run: `cd /home/knid/Projects/wemob/wemob-backend && go build -o /dev/null ./cmd/main.go`
Expected: No errors

- [ ] **Step 2: Check for unused imports**

Run: `cd /home/knid/Projects/wemob/wemob-backend && go vet ./...`
Expected: No errors. If there are unused imports from removed constants, clean them up.

- [ ] **Step 3: Verify no remaining hardcoded references that should be migrated**

Run: `grep -rn "models.ReferralSignupCredits\|models.ReferralSubscriptionCredits\|models.AICreditsMinBalance\|maxConcurrentJobsPerUser\b\|maxAssetFileSize\b\|maxContentLen\b\|maxQueryRows\b" internal/`
Expected: No matches in non-test, non-model files. The constants in `models/referral.go` and `models/ai_credits.go` stay (they're the fallback source of truth referenced by `keys.go`).

- [ ] **Step 4: Commit any cleanup**

If Step 2/3 found issues, fix and commit:
```bash
git add -A
git commit -m "fix(remoteconfig): clean up unused imports and references"
```
