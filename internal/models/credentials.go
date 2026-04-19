package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// AppleConnection is a user's App Store Connect API key and team metadata.
// Used to submit iOS builds to the App Store under the user's own account.
// Table name kept as "app_store_credentials" for now; Task 2 renames it.
type AppleConnection struct {
	bun.BaseModel `bun:"table:app_store_credentials,alias:ac"`

	ID            uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	UserID        uuid.UUID `bun:",notnull,type:uuid"`
	User          Project   `bun:"rel:belongs-to,join:user_id=id"`
	Name          string    `bun:",notnull"`
	KeyID         string    `bun:",notnull"`
	TeamID        string    `bun:",notnull"`
	IssuerID      string    `bun:",notnull"`
	P8KeyPath     string    `bun:",notnull"`
	APNSP8KeyPath string    `bun:",notnull"`
	APNSP8KeyID   string    `bun:",notnull"`
	CreatedAt     time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt     time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt     time.Time `bun:",soft_delete,nullzero"`
}

// GooglePlayConnection is a user's Google Play Console service account key.
// Used to submit Android builds to Play Store under the user's own account.
// Table name kept as "google_play_credentials" for now; Task 2 renames it.
type GooglePlayConnection struct {
	bun.BaseModel `bun:"table:google_play_credentials,alias:gpc"`

	ID                 uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	UserID             uuid.UUID `bun:",notnull,type:uuid"`
	User               Project   `bun:"rel:belongs-to,join:user_id=id"`
	Name               string    `bun:",notnull"`
	ServiceAccountPath string    `bun:",notnull"`
	CreatedAt          time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt          time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt          time.Time `bun:",soft_delete,nullzero"`
}
