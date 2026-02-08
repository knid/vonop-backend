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
	CreatedAt    time.Time   `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt    time.Time   `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}

type AppConversationMessage struct {
	Role      string `json:"role"`      // "user", "assistant", "system"
	Content   string `json:"content"`
	Timestamp int64  `json:"timestamp"`
}
