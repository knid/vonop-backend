package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// AppleConnection is a user's App Store Connect API key and team metadata.
// Used to submit iOS builds to the App Store under the user's own account.
//
// The .p8 contents live encrypted in CredStore (P8KeyCredRef). The legacy
// plaintext-S3-pointer (P8KeyPath) and the entire APNs subsystem
// (APNSP8KeyPath / APNSP8KeyID / APNSP8KeyCredRef) were dropped 2026-05-07
// since CredRef is the sole credential storage and APNs was never wired up
// to a consumer. iOS push notification keys, when needed, will live in a
// dedicated APNsCredential model with proper consumers.
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
	// Encrypted-at-rest reference for the .p8 contents (KMS envelope via CredStore).
	P8KeyCredRef *CredRefJSON `bun:"p8_key_cred_ref,type:jsonb,nullzero"`
	CreatedAt    time.Time    `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt    time.Time    `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt    time.Time    `bun:",soft_delete,nullzero"`
}

// GooglePlayConnection is a user's Google Play Console service account key.
// Used to submit Android builds to Play Store under the user's own account.
//
// The SA JSON contents live encrypted in CredStore (ServiceAccountCredRef);
// the legacy plaintext-S3-pointer (ServiceAccountPath) was dropped
// 2026-05-07.
type GooglePlayConnection struct {
	bun.BaseModel `bun:"table:google_play_connections,alias:gpc"`

	ID             uuid.UUID     `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	UserID         uuid.UUID     `bun:",notnull,type:uuid"`
	User           *User         `bun:"rel:belongs-to,join:user_id=id"`
	OrganizationID uuid.UUID     `bun:",notnull,type:uuid"`
	Organization   *Organization `bun:"rel:belongs-to,join:organization_id=id"`
	Name           string        `bun:",notnull"`
	// Encrypted-at-rest reference for the service account JSON (KMS envelope via CredStore).
	ServiceAccountCredRef *CredRefJSON `bun:"service_account_cred_ref,type:jsonb,nullzero"`
	CreatedAt             time.Time    `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt             time.Time    `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt             time.Time    `bun:",soft_delete,nullzero"`
}
