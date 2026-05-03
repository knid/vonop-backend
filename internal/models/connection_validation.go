package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// ConnectionValidation is one row per validation outcome of an Apple or Google
// Play connection. Append-only — every call to ValidateAppleConnection /
// ValidateGooglePlayConnection writes a new row.
//
// Used by:
//   - The credentials UI to show "✓ verified 3 days ago" badges
//   - The daily credential health cron to detect expiring/broken credentials
//   - Pre-flight checks before triggering Codemagic builds
type ConnectionValidation struct {
	bun.BaseModel `bun:"table:connection_validations,alias:cv"`

	ID             uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	ConnectionKind string    `bun:",type:varchar(32),notnull"` // "apple" | "google_play"
	ConnectionID   uuid.UUID `bun:",type:uuid,notnull"`
	Status         string    `bun:",type:varchar(32),notnull"` // "valid" | "invalid" | "expired" | "rate_limited"
	Message        string    `bun:",type:text"`
	AppCount       int       `bun:",nullzero"`
	ValidatedAt    time.Time `bun:",notnull"`
	CreatedAt      time.Time `bun:",nullzero,notnull,default:current_timestamp"`
}
