package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// AppleConnection is a user's App Store Connect API key and team metadata.
// Used to submit iOS builds to the App Store under the user's own account.
type AppleConnection struct {
	bun.BaseModel `bun:"table:apple_connections,alias:ac"`

	ID             uuid.UUID     `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	UserID         uuid.UUID     `bun:",notnull,type:uuid"`
	User           *User         `bun:"rel:belongs-to,join:user_id=id"`
	OrganizationID uuid.UUID     `bun:",notnull,type:uuid"`
	Organization   *Organization `bun:"rel:belongs-to,join:organization_id=id"`
	Name           string        `bun:",notnull"`
	KeyID          string        `bun:",notnull"`
	TeamID         string        `bun:",notnull"`
	IssuerID       string        `bun:",notnull"`
	P8KeyPath      string        `bun:",notnull"`
	APNSP8KeyPath  string        `bun:",notnull"`
	APNSP8KeyID    string        `bun:",notnull"`
	// Encrypted-at-rest references for the .p8 contents (KMS envelope via CredStore).
	// nullzero so legacy rows that haven't been backfilled stay unmarshallable.
	P8KeyCredRef     *CredRefJSON `bun:"p8_key_cred_ref,type:jsonb,nullzero"`
	APNSP8KeyCredRef *CredRefJSON `bun:"apns_p8_key_cred_ref,type:jsonb,nullzero"`
	CreatedAt        time.Time    `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt        time.Time    `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt        time.Time    `bun:",soft_delete,nullzero"`
}

// GooglePlayConnection is a user's Google Play Console service account key.
// Used to submit Android builds to Play Store under the user's own account.
type GooglePlayConnection struct {
	bun.BaseModel `bun:"table:google_play_connections,alias:gpc"`

	ID                 uuid.UUID     `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	UserID             uuid.UUID     `bun:",notnull,type:uuid"`
	User               *User         `bun:"rel:belongs-to,join:user_id=id"`
	OrganizationID     uuid.UUID     `bun:",notnull,type:uuid"`
	Organization       *Organization `bun:"rel:belongs-to,join:organization_id=id"`
	Name               string        `bun:",notnull"`
	ServiceAccountPath string        `bun:",notnull"`
	// Encrypted-at-rest reference for the service account JSON (KMS envelope via CredStore).
	ServiceAccountCredRef *CredRefJSON `bun:"service_account_cred_ref,type:jsonb,nullzero"`
	CreatedAt             time.Time    `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt             time.Time    `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt             time.Time    `bun:",soft_delete,nullzero"`
}
