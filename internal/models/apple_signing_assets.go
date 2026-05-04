package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// AppleSigningAssets is the per-AppProject distribution-cert + provisioning
// profile generated automatically via the App Store Connect API (P1 #14 —
// mobile pipeline audit). The user provides an ASC API key once
// (AppleConnection); Wemob then mints a fresh iOS distribution certificate,
// registers / looks up a bundle ID, and creates an IOS_APP_STORE provisioning
// profile that ties the two together. Codemagic workers download the cert
// + profile via an HMAC-protected endpoint at build time so the user never
// has to copy keystore-style assets around.
//
// One row per AppProject. The cert + profile are stored encrypted-at-rest
// via CredStore (envelope encryption). The unencrypted columns
// (CertificateID, ProfileID, BundleID, ExpiresAt) drive the daily renewal
// cron — when the cert is within 30 days of expiry we re-generate.
//
// Columns produced by Bun snake_case:
//
//	AppleSigningAssets       → apple_signing_assets
//	AppProjectID             → app_project_id (unique — one row per project)
//	OrganizationID           → organization_id
//	AppleConnectionID        → apple_connection_id (FK to AppleConnection)
//	CertificateID            → certificate_id (the ASC certificate ID)
//	CertificateCredRef       → certificate_cred_ref (jsonb, encrypted PKCS#8 priv key + cert PEM)
//	CertificateExpiresAt     → certificate_expires_at
//	ProfileID                → profile_id (the ASC profile ID)
//	ProfileCredRef           → profile_cred_ref (jsonb, encrypted .mobileprovision bytes)
//	ProfileExpiresAt         → profile_expires_at
//	BundleID                 → bundle_id (matches the project's iOS bundle id)
//	CreatedAt / UpdatedAt    → standard Bun timestamps
//	DeletedAt                → soft delete
type AppleSigningAssets struct {
	bun.BaseModel `bun:"table:apple_signing_assets,alias:asa"`

	ID                   uuid.UUID    `bun:",pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	AppProjectID         uuid.UUID    `bun:"app_project_id,notnull,type:uuid,unique" json:"app_project_id"`
	OrganizationID       uuid.UUID    `bun:"organization_id,notnull,type:uuid" json:"organization_id"`
	AppleConnectionID    uuid.UUID    `bun:"apple_connection_id,notnull,type:uuid" json:"apple_connection_id"`
	CertificateID        string       `bun:"certificate_id,notnull" json:"certificate_id"`
	CertificateCredRef   *CredRefJSON `bun:"certificate_cred_ref,type:jsonb,nullzero" json:"certificate_cred_ref,omitempty"`
	CertificateExpiresAt time.Time    `bun:"certificate_expires_at,nullzero" json:"certificate_expires_at"`
	ProfileID            string       `bun:"profile_id,notnull" json:"profile_id"`
	ProfileCredRef       *CredRefJSON `bun:"profile_cred_ref,type:jsonb,nullzero" json:"profile_cred_ref,omitempty"`
	ProfileExpiresAt     time.Time    `bun:"profile_expires_at,nullzero" json:"profile_expires_at"`
	BundleID             string       `bun:"bundle_id,notnull" json:"bundle_id"`
	CreatedAt            time.Time    `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt            time.Time    `bun:"updated_at,nullzero,notnull,default:current_timestamp" json:"updated_at"`
	DeletedAt            time.Time    `bun:"deleted_at,soft_delete,nullzero" json:"deleted_at,omitempty"`
}
