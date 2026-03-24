package models

import (
	"time"

	"github.com/google/uuid"
)

// AppProjectMemory stores structured long-term memory for a project's AI interactions.
// Extracted after each chat session, it persists design preferences, rejected approaches,
// and technical decisions so future sessions feel contextually aware.
type AppProjectMemory struct {
	ID           uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	AppProjectID uuid.UUID `bun:",notnull,type:uuid,unique"`
	AppProject   *AppProject `bun:"rel:belongs-to,join:app_project_id=id"`

	// Structured memory fields stored as JSON arrays of strings
	DesignPreferences  []byte `bun:",type:jsonb,default:'[]'"` // e.g. ["dark mode preferred", "no gradients"]
	RejectedApproaches []byte `bun:",type:jsonb,default:'[]'"` // e.g. ["tried sidebar nav, user wanted tabs"]
	TechnicalDecisions []byte `bun:",type:jsonb,default:'[]'"` // e.g. ["using Stripe for payments", "Neon for DB"]
	UserPreferences    []byte `bun:",type:jsonb,default:'[]'"` // e.g. ["prefers Turkish UI", "likes minimal design"]

	CreatedAt time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}
