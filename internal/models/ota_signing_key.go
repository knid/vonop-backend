package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// OTAUpdateSigningKey holds the per-AppProject RSA signing keypair used to
// sign self-hosted expo-updates manifests (P0 #8a — manifest signing).
//
// Generated lazily on the first publish for a project (see
// EnsureOTAUpdateSigningKey in controllers/ota_signing.go); reused for all
// subsequent publishes of the same project. Rotating the key requires
// shipping a new binary that bundles the new certificate, so we generate
// once and keep it. The cert is PUBLIC — embedded in app.json + the binary
// — so we store it in plaintext. The private key is encrypted via CredStore
// (kind="ota-signing-rsa") and only the cred-ref lives in this row.
//
// Columns produced by Bun snake_case:
//
//	OTAUpdateSigningKey   → ota_update_signing_keys
//	AppProjectID          → app_project_id  (PK; one keypair per project)
//	CredRefPrivate        → cred_ref_private  (jsonb, encrypted private key)
//	CertificatePEM        → certificate_pem   (text, public X.509 PEM)
//	Algorithm             → algorithm         (text, default 'rsa-v1_5-sha256')
//	KeyID                 → key_id            (text, sha256 fingerprint prefix)
//	CreatedAt             → created_at
//	RotatedAt             → rotated_at
type OTAUpdateSigningKey struct {
	bun.BaseModel `bun:"table:ota_update_signing_keys,alias:otask"`

	AppProjectID   uuid.UUID    `bun:"app_project_id,pk,type:uuid" json:"app_project_id"`
	OrganizationID uuid.UUID    `bun:"organization_id,notnull,type:uuid" json:"organization_id"`
	CredRefPrivate *CredRefJSON `bun:"cred_ref_private,type:jsonb,nullzero" json:"cred_ref_private,omitempty"`
	CertificatePEM string       `bun:"certificate_pem,type:text,notnull" json:"certificate_pem"`
	Algorithm      string       `bun:"algorithm,type:text,notnull,default:'rsa-v1_5-sha256'" json:"algorithm"`
	KeyID          string       `bun:"key_id,type:text,notnull" json:"key_id"`
	CreatedAt      time.Time    `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"created_at"`
	RotatedAt      time.Time    `bun:"rotated_at,nullzero" json:"rotated_at,omitempty"`
}
