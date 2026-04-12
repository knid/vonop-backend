package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// AppChatMessage is one turn of the user↔agent conversation for an app project.
// A "user" message is what the user typed (plus any images). An "assistant"
// message is the final narration and tool call trace from one agent run.
type AppChatMessage struct {
	ID         uuid.UUID    `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	ProjectID  uuid.UUID    `bun:",notnull,type:uuid"`
	Project    *AppProject  `bun:"rel:belongs-to,join:project_id=id"`
	RunID      *uuid.UUID   `bun:",type:uuid,nullzero"` // null for user messages not yet tied to a run
	Role       string       `bun:",notnull"`            // "user" | "assistant"
	Content    string       `bun:",type:text,notnull,default:''"`
	Images     []byte       `bun:",type:jsonb,notnull,default:'[]'"`     // [{url, alt?}]
	ToolCalls  []byte       `bun:",type:jsonb,notnull,default:'[]'"`     // []ChatMessageToolCall
	CreatedAt  time.Time    `bun:"created_at,nullzero,notnull,default:current_timestamp"`
}

// ChatMessageToolCall is the JSONB shape embedded in AppChatMessage.ToolCalls.
// Stored as a snapshot of what the agent did — no foreign key to any tool table.
type ChatMessageToolCall struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Input      json.RawMessage `json:"input"`
	Result     string          `json:"result"`
	IsError    bool            `json:"is_error"`
	DurationMs int             `json:"duration_ms"`
}
