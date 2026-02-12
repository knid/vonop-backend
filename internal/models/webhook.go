package models

import (
	"time"

	"github.com/google/uuid"
)

// WebhookEndpoint is a user-configured URL to receive event notifications.
type WebhookEndpoint struct {
	ID        uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	UserID    uuid.UUID `bun:",notnull,type:uuid"`
	User      *User     `bun:"rel:belongs-to,join:user_id=id"`
	URL       string    `bun:",notnull"`
	Secret    string    `bun:",notnull"` // HMAC signing secret
	Events    string    `bun:",notnull"` // comma-separated: "form.submitted,build.completed"
	Active    bool      `bun:",notnull,default:true"`
	CreatedAt time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt time.Time `bun:",soft_delete,nullzero"`
}

// Webhook event type constants
const (
	WebhookEventFormSubmitted  = "form.submitted"
	WebhookEventBuildCompleted = "build.completed"
	WebhookEventBuildFailed      = "build.failed"
)
