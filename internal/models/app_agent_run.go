package models

import (
	"encoding/json"
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
	ID               uuid.UUID       `bun:",pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	AppProjectID     uuid.UUID       `bun:",notnull,type:uuid" json:"app_project_id"`
	AppProject       *AppProject     `bun:"rel:belongs-to,join:app_project_id=id" json:"-"`
	UserID           uuid.UUID       `bun:",notnull,type:uuid" json:"user_id"`
	User             *User           `bun:"rel:belongs-to,join:user_id=id" json:"-"`
	Status           string          `bun:",notnull" json:"status"`
	PreCommit        string          `bun:",notnull" json:"pre_commit"`
	FinalCommit      string          `bun:",default:''" json:"final_commit"`
	UserMessageID    *uuid.UUID      `bun:",type:uuid,nullzero" json:"user_message_id"`
	Model            string          `bun:",notnull" json:"model"`
	Provider         string          `bun:",notnull" json:"provider"`
	Turns            int             `bun:",notnull,default:0" json:"turns"`
	ToolCallCount    int             `bun:",notnull,default:0" json:"tool_call_count"`
	PromptTokens     int             `bun:",notnull,default:0" json:"prompt_tokens"`
	CompletionTokens int             `bun:",notnull,default:0" json:"completion_tokens"`
	CacheReadTokens  int             `bun:",notnull,default:0" json:"cache_read_tokens"`
	CacheWriteTokens int             `bun:",notnull,default:0" json:"cache_write_tokens"`
	CostUSD          float64         `bun:",notnull,default:0" json:"cost_usd"`
	CreditsUsed      int             `bun:",notnull,default:0" json:"credits_used"`
	DurationMs       int             `bun:",notnull,default:0" json:"duration_ms"`
	Error            string          `bun:",type:text,default:''" json:"error,omitempty"`
	Todos            json.RawMessage `bun:",type:jsonb,notnull,default:'[]'" json:"todos"`
	CreatedAt        time.Time       `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"created_at"`
	CompletedAt      time.Time       `bun:",nullzero" json:"completed_at,omitempty"`
}
