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
	ID           uuid.UUID       `bun:",pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	AppProjectID uuid.UUID       `bun:",notnull,type:uuid" json:"app_project_id"`
	AppProject   *AppProject     `bun:"rel:belongs-to,join:app_project_id=id" json:"-"`
	RunID        *uuid.UUID      `bun:",type:uuid,nullzero" json:"run_id"`
	Role         string          `bun:",notnull" json:"role"`
	Content      string          `bun:",type:text,notnull,default:''" json:"content"`
	Images       json.RawMessage `bun:",type:jsonb,notnull,default:'[]'" json:"images"`     // [{url, alt?}]
	ToolCalls    json.RawMessage `bun:",type:jsonb,notnull,default:'[]'" json:"tool_calls"` // []ChatMessageToolCall
	CreatedAt    time.Time       `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"created_at"`
}

// ChatMessageToolCall is the JSONB shape embedded in AppChatMessage.ToolCalls.
// Stored as a snapshot of what the agent did — no foreign key to any tool table.
//
// NarrationOffset records the byte length of the assistant's accumulated
// narration text at the moment the tool was invoked. The dashboard uses this
// to interleave tool / file cards back into the right spot in the persisted
// narration on refresh — without it, buildPersistedTimeline collapses every
// tool to the end and the chronological order from the live stream is lost.
type ChatMessageToolCall struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	Input           json.RawMessage `json:"input"`
	Result          string          `json:"result"`
	IsError         bool            `json:"is_error"`
	DurationMs      int             `json:"duration_ms"`
	NarrationOffset int             `json:"narration_offset"`
}
