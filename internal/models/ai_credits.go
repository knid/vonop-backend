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

// Token-to-credit conversion: 1 credit per 10,000 tokens
const AICreditsTokensPerCredit = 10_000

// TokensToCredits converts total token count to credit cost (rounds up).
func TokensToCredits(totalTokens int) int {
	if totalTokens <= 0 {
		return 0
	}
	return (totalTokens + AICreditsTokensPerCredit - 1) / AICreditsTokensPerCredit
}

// AI credit costs per action
const (
	AICreditCostGenerate       = 10
	AICreditCostChat           = 5
	AICreditCostScreenshotCode = 15
	AICreditCostElementEdit    = 3
	AICreditCostAddPage        = 8
	AICreditCostPublish        = 2

	// V2 app builder costs
	AICreditCostGenerateApp  = 15
	AICreditCostDeployWeb    = 5
	AICreditCostBuildFlutter = 10
	AICreditCostStoreChat    = 5

	// V1 legacy endpoint costs
	AICreditCostNavbarAutoFill     = 3
	AICreditCostScreenshotGenerate = 5
)
