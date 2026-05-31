package models

import (
	"time"

	"github.com/google/uuid"
)

// AICreditsBalance tracks per-user AI credit balance within a billing period.
//
// PendingGateway holds fractional credit debt from Wemob AI Gateway chat
// traffic. Gateway calls are priced from the upstream USD cost of the
// specific model used, which almost never lands on a whole credit — the
// fractional remainder accumulates here and, whenever it crosses an
// integer, that many whole credits are deducted from Balance. This keeps
// gateway reselling margin-accurate without forcing every tiny call to
// round up to a full credit (the previous behavior, which over-taxed
// cheap models like claude-haiku-4-5 by ~20×).
type AICreditsBalance struct {
	ID             uuid.UUID     `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	OrganizationID uuid.UUID     `bun:",notnull,unique,type:uuid"`
	Organization   *Organization `bun:"rel:belongs-to,join:organization_id=id"`
	UserID         uuid.UUID     `bun:",type:uuid,nullzero"` // deprecated; dropped in a future cleanup
	User           *User         `bun:"rel:belongs-to,join:user_id=id"`
	Balance        int           `bun:",notnull,default:0"`
	PendingGateway float64       `bun:",notnull,default:0"`
	PeriodStart    time.Time     `bun:",notnull"`
	PeriodEnd      time.Time     `bun:",notnull"`
	// Backfill marker — see Plan A migration in cmd/main.go that re-keyed legacy
	// per-user pending_gateway debt onto each user's personal-org balance row.
	// Declared so `SELECT *` raw queries (e.g. the FOR UPDATE row lock in
	// GrantSubscriptionCredits) can scan into this struct without erroring.
	GatewayPendingBackfilledV1 bool      `bun:"gateway_pending_backfilled_v1,notnull,default:false"`
	CreatedAt                  time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt                  time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}

// AICreditTransaction records individual credit additions or deductions.
type AICreditTransaction struct {
	ID             uuid.UUID     `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	OrganizationID uuid.UUID     `bun:",notnull,type:uuid"`
	Organization   *Organization `bun:"rel:belongs-to,join:organization_id=id"`
	UserID         uuid.UUID     `bun:",notnull,type:uuid"` // KEPT — "who spent" attribution
	User           *User         `bun:"rel:belongs-to,join:user_id=id"`
	Amount         int           `bun:",notnull"`  // positive = added, negative = consumed
	Action         string        `bun:",notnull"`  // e.g. "generate", "chat", "screenshot_to_code", "element_edit", "purchase", "monthly_grant", "gateway_chat", "gateway_chat_pending"
	Reference      string        `bun:",nullzero"` // optional: website ID, Stripe payment ID, etc. (legacy string form)
	// ProjectID provides typed, indexable attribution for agency cost reporting.
	// Populated on all modern deduction paths (agent runs, dashboard AI, builds, etc.).
	// Backfilled where possible from Reference during the 2026-05 cost attribution work.
	ProjectID      uuid.UUID     `bun:",nullzero,type:uuid"`
	// PendingAmount carries the fractional credit cost for sub-credit gateway
	// calls (action="gateway_chat_pending"), where Amount is 0 because no whole
	// credit moved out of pending_gateway. Preserves audit fidelity for
	// reconciliation against gateway request logs.
	PendingAmount  float64       `bun:"pending_amount,nullzero"`
	CreatedAt      time.Time     `bun:"created_at,nullzero,notnull,default:current_timestamp"`
}

// LLMTokenUsage records per-request token consumption for billing visibility and dashboards.
type LLMTokenUsage struct {
	ID               uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	UserID           uuid.UUID `bun:",notnull,type:uuid"`
	User             *User     `bun:"rel:belongs-to,join:user_id=id"`
	Action           string    `bun:",notnull"`          // "generate_app", "app_chat", etc.
	Model            string    `bun:",notnull"`
	PromptTokens     int       `bun:",notnull,default:0"`
	CompletionTokens int       `bun:",notnull,default:0"`
	TotalTokens      int       `bun:",notnull,default:0"`
	CallCount        int       `bun:",notnull,default:0"`
	Reference        string    `bun:",nullzero"`         // project/app ID (legacy string form)
	// ProjectID provides typed, indexable attribution (added alongside Reference in 2026-05 cost work).
	ProjectID        uuid.UUID `bun:",nullzero,type:uuid"`
	CreatedAt        time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
}

// AICreditsMinBalance is the maximum allowed overdraft (negative floor).
const AICreditsMinBalance = -100

// Token-to-credit conversion: 1 credit per 5,000 weighted tokens.
// Output tokens are weighted at 5× input tokens to reflect API pricing
// (input: $3/MTok, output: $15/MTok for Sonnet 4.6).
// Cache-read tokens are weighted at 1/10× input — Anthropic prompt-cache reads
// are billed at ~10% of the fresh-input rate ($0.30/MTok vs $3/MTok). Counting
// them as zero (the previous behavior) let cache-heavy agent loops accumulate
// real cost we never charged for.
const AICreditsTokensPerCredit = 5_000
const AICreditsOutputMultiplier = 5
const AICreditsCacheReadDivisor = 10


// TokensToCreditsWithConfig is like TokensToCredits but uses caller-provided
// conversion parameters from remote config. cacheReadDivisor falls back to the
// hardcoded default if non-positive.
//
// cacheReadInclusive should be true when the provider (e.g. DeepSeek) includes
// cache-read tokens inside prompt_tokens. In that case the cache-read tokens
// are already counted at full weight in promptTokens, so we subtract them
// before adding back their discounted weight to avoid double-counting.
func TokensToCreditsWithConfig(promptTokens, completionTokens, cacheReadTokens, tokensPerCredit, outputMultiplier, cacheReadDivisor int, cacheReadInclusive ...bool) int {
	if tokensPerCredit <= 0 {
		return 0
	}
	if cacheReadDivisor <= 0 {
		cacheReadDivisor = AICreditsCacheReadDivisor
	}
	weighted := promptTokens + cacheReadTokens/cacheReadDivisor + (completionTokens * outputMultiplier)
	if len(cacheReadInclusive) > 0 && cacheReadInclusive[0] {
		// cacheReadTokens are already in promptTokens at 1× weight;
		// replace that with the discounted weight.
		weighted = promptTokens - cacheReadTokens + cacheReadTokens/cacheReadDivisor + (completionTokens * outputMultiplier)
	}
	if weighted <= 0 {
		return 0
	}
	return (weighted + tokensPerCredit - 1) / tokensPerCredit
}

