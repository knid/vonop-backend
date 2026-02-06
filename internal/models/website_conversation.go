package models

import (
	"time"

	"github.com/google/uuid"
)

type ConversationMessage struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	Timestamp int64  `json:"timestamp"`
}

type WebsiteConversation struct {
	ID        uuid.UUID             `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	WebsiteID uuid.UUID             `bun:",notnull,type:uuid"`
	Website   *Website              `bun:"rel:belongs-to,join:website_id=id"`
	Messages  []ConversationMessage `bun:",type:jsonb,default:'[]'"`
	CreatedAt time.Time             `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt time.Time             `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}
