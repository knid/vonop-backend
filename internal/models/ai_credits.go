package models

import (
	"time"

	"github.com/google/uuid"
)

// AICreditsBalance tracks per-user AI credit balance within a billing period.
type AICreditsBalance struct {
	ID          uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	UserID      uuid.UUID `bun:",notnull,type:uuid,unique"`
	User        *User     `bun:"rel:belongs-to,join:user_id=id"`
	Balance     int       `bun:",notnull,default:0"`
	PeriodStart time.Time `bun:",notnull"`
	PeriodEnd   time.Time `bun:",notnull"`
	CreatedAt   time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt   time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}

// AICreditTransaction records individual credit additions or deductions.
type AICreditTransaction struct {
	ID        uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	UserID    uuid.UUID `bun:",notnull,type:uuid"`
	User      *User     `bun:"rel:belongs-to,join:user_id=id"`
	Amount    int       `bun:",notnull"`         // positive = added, negative = consumed
	Action    string    `bun:",notnull"`         // e.g. "generate", "chat", "screenshot_to_code", "element_edit", "purchase", "monthly_grant"
	Reference string    `bun:",nullzero"`        // optional: website ID, Stripe payment ID, etc.
	CreatedAt time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
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
	Reference        string    `bun:",nullzero"`         // project/app ID
	CreatedAt        time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
}

// AICreditsMinBalance is the maximum allowed overdraft (negative floor).
const AICreditsMinBalance = -100

// Token-to-credit conversion: 1 credit per 5,000 weighted tokens.
// Output tokens are weighted at 5× input tokens to reflect API pricing
// (input: $3/MTok, output: $15/MTok for Sonnet 4.6).
const AICreditsTokensPerCredit = 5_000
const AICreditsOutputMultiplier = 5

// TokensToCredits converts prompt and completion token counts to credit cost
// using weighted tokens (output × 5) and rounds up.
func TokensToCredits(promptTokens, completionTokens int) int {
	weighted := promptTokens + (completionTokens * AICreditsOutputMultiplier)
	if weighted <= 0 {
		return 0
	}
	return (weighted + AICreditsTokensPerCredit - 1) / AICreditsTokensPerCredit
}

