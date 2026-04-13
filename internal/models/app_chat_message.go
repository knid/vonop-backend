package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// AppChatMessage is one turn of the user↔agent conversation for an app project.
// A "user" message is what the user typed (plus any images). An "assistant"
// message is the final narration and tool call trace from one agent run.
//
// Parts is the canonical chronological record of an assistant turn: an
// ordered array of typed parts (text + tool_call) that mirrors how the
// model actually emitted them. The legacy Content + ToolCalls columns are
// derived from Parts at write time and kept around so old readers (LLM
// history rebuild, analytics) keep working — new code should read Parts.
type AppChatMessage struct {
	ID           uuid.UUID       `bun:",pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	AppProjectID uuid.UUID       `bun:",notnull,type:uuid" json:"app_project_id"`
	AppProject   *AppProject     `bun:"rel:belongs-to,join:app_project_id=id" json:"-"`
	RunID        *uuid.UUID      `bun:",type:uuid,nullzero" json:"run_id"`
	Role         string          `bun:",notnull" json:"role"`
	Content      string          `bun:",type:text,notnull,default:''" json:"content"`
	Images       json.RawMessage `bun:",type:jsonb,notnull,default:'[]'" json:"images"`     // [{url, alt?}]
	ToolCalls    json.RawMessage `bun:",type:jsonb,notnull,default:'[]'" json:"tool_calls"` // []ChatMessageToolCall — derived from Parts, kept for legacy readers
	Parts        json.RawMessage `bun:",type:jsonb,notnull,default:'[]'" json:"parts"`      // []ChatMessagePart — chronological ordered record (canonical)
	CreatedAt    time.Time       `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"created_at"`
}

// ChatMessagePart is one chronological block inside an assistant turn.
// Text parts carry narration the model wrote; tool_call parts carry a
// snapshot of one tool invocation (name, input, result). The dashboard
// renders parts top-to-bottom — text → MarkdownText, tool_call → tool /
// file card — so the on-screen order matches what the model actually did.
type ChatMessagePart struct {
	Type string `json:"type"` // "text" | "tool_call"

	// type=text
	Text string `json:"text,omitempty"`

	// type=tool_call
	ToolCallID string          `json:"tool_call_id,omitempty"`
	Name       string          `json:"name,omitempty"`
	Input      json.RawMessage `json:"input,omitempty"`
	Result     string          `json:"result,omitempty"`
	IsError    bool            `json:"is_error,omitempty"`
	DurationMs int             `json:"duration_ms,omitempty"`
}

// ChatMessageToolCall is the JSONB shape embedded in AppChatMessage.ToolCalls.
// Kept for legacy compatibility — new code should walk Parts instead.
// The struct intentionally has no NarrationOffset: that approach was replaced
// by the parts array (offsets were brittle across encoding boundaries).
type ChatMessageToolCall struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Input      json.RawMessage `json:"input"`
	Result     string          `json:"result"`
	IsError    bool            `json:"is_error"`
	DurationMs int             `json:"duration_ms"`
}
