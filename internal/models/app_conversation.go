package models

import (
	"time"

	"github.com/google/uuid"
)

type AppConversation struct {
	ID           uuid.UUID   `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	AppProjectID uuid.UUID   `bun:",notnull,type:uuid,unique"`
	AppProject   *AppProject `bun:"rel:belongs-to,join:app_project_id=id"`
	Messages     []byte      `bun:",type:jsonb,default:'[]'"` // stores []AppConversationMessage
	Summary      string      `bun:",default:''"` // LLM-generated summary of older messages
	CreatedAt    time.Time   `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt    time.Time   `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}

type AppConversationMessage struct {
	ID                 string           `json:"id"`
	Role               string           `json:"role"` // "user", "assistant", "system"
	Content            string           `json:"content"`
	Timestamp          int64            `json:"timestamp"`
	FileOperations     []AppChatFileOp  `json:"file_operations,omitempty"`
	ImageURLs          []string         `json:"image_urls,omitempty"`
	SnapshotID         string           `json:"snapshot_id,omitempty"`
	Suggestions        []string         `json:"suggestions,omitempty"`
	ThinkingText       string           `json:"thinking_text,omitempty"`
	ThinkingDurationMs int              `json:"thinking_duration_ms,omitempty"`
	ToolCalls          []AppChatToolCall `json:"tool_calls,omitempty"`
}

type AppChatToolCall struct {
	Tool      string `json:"tool"`
	Arguments string `json:"arguments"`
}

type AppChatFileOp struct {
	Action   string `json:"action"`
	FilePath string `json:"file_path"`
	FileType string `json:"file_type,omitempty"`
}
