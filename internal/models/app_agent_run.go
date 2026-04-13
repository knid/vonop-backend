package models

import (
	"time"

	"github.com/google/uuid"
)

// Agent run statuses.
const (
	AgentRunStatusRunning        = "running"
	AgentRunStatusCompleted      = "completed"
	AgentRunStatusFailed         = "failed"
	AgentRunStatusCanceled       = "canceled"
	AgentRunStatusBudgetExceeded = "budget_exceeded"
)

// AppAgentRun is the execution record of one agent invocation: tokens, cost,
// commit hashes, status, duration. One row per user turn that triggers the
// agent. Referenced by the assistant AppChatMessage via RunID.
type AppAgentRun struct {
	ID               uuid.UUID   `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	AppProjectID     uuid.UUID   `bun:",notnull,type:uuid"`
	AppProject       *AppProject `bun:"rel:belongs-to,join:app_project_id=id"`
	UserID           uuid.UUID   `bun:",notnull,type:uuid"`
	User             *User       `bun:"rel:belongs-to,join:user_id=id"`
	Status           string      `bun:",notnull"`
	PreCommit        string      `bun:",notnull"`
	FinalCommit      string      `bun:",default:''"`
	UserMessageID    *uuid.UUID  `bun:",type:uuid,nullzero"`
	Model            string      `bun:",notnull"`
	Provider         string      `bun:",notnull"`
	Turns            int         `bun:",notnull,default:0"`
	ToolCallCount    int         `bun:",notnull,default:0"`
	PromptTokens     int         `bun:",notnull,default:0"`
	CompletionTokens int         `bun:",notnull,default:0"`
	CacheReadTokens  int         `bun:",notnull,default:0"`
	CacheWriteTokens int         `bun:",notnull,default:0"`
	CostUSD          float64     `bun:",notnull,default:0"`
	CreditsUsed      int         `bun:",notnull,default:0"`
	DurationMs       int         `bun:",notnull,default:0"`
	Error            string      `bun:",type:text,default:''"`
	CreatedAt        time.Time   `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	CompletedAt      time.Time   `bun:",nullzero"`
}
