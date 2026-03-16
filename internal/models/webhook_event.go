package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// WebhookEvent tracks processed Stripe webhook events for deduplication.
type WebhookEvent struct {
	bun.BaseModel `bun:"table:webhook_events"`
	ID            uuid.UUID `bun:"id,pk,type:uuid,default:uuid_generate_v4()"`
	EventID       string    `bun:"event_id,notnull,unique"`
	EventType     string    `bun:"event_type,notnull"`
	ProcessedAt   time.Time `bun:"processed_at,notnull,default:current_timestamp"`
}
