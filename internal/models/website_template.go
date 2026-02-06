package models

import (
	"time"

	"github.com/google/uuid"
)

type WebsiteTemplate struct {
	ID         uuid.UUID      `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	Name       string         `bun:",notnull"`
	Category   string         `bun:",notnull"`
	PreviewURL string         `bun:",nullzero"`
	PromptHint string         `bun:",type:text,nullzero"`
	Structure  map[string]any `bun:",type:jsonb,default:'{}'"`
	IsActive   bool           `bun:",notnull,default:true"`
	CreatedAt  time.Time      `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt  time.Time      `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}

type AIGenerationLog struct {
	ID         uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	WebsiteID  uuid.UUID `bun:",notnull,type:uuid"`
	UserID     uuid.UUID `bun:",notnull,type:uuid"`
	Prompt     string    `bun:",notnull,type:text"`
	Model      string    `bun:",notnull"`
	TokensUsed int       `bun:",notnull,default:0"`
	Cost       float64   `bun:",notnull,default:0"`
	CreatedAt  time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
}
