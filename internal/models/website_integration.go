package models

import (
	"time"

	"github.com/google/uuid"
)

type WebsiteIntegration struct {
	ID          uuid.UUID      `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	WebsiteID   uuid.UUID      `bun:",notnull,type:uuid"`
	Website     *Website       `bun:"rel:belongs-to,join:website_id=id"`
	Provider    string         `bun:",notnull"` // supabase, firebase, stripe, iyzico, resend, cloudinary, google_sheets
	Credentials map[string]any `bun:",type:jsonb,default:'{}'"` // encrypted API keys, project URLs, etc.
	Config      map[string]any `bun:",type:jsonb,default:'{}'"` // provider-specific settings
	Status      string         `bun:",notnull,default:'connected'"` // connected, disconnected, error
	CreatedAt   time.Time      `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt   time.Time      `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt   time.Time      `bun:",soft_delete,nullzero"`
}
