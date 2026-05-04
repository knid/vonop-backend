package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// OTAUpdate records a single self-hosted expo-updates publish event for a
// mobile_app_native project. Each row is the result of one
// `npx expo export` + Codemagic upload to /v2/apps/{appId}/ota/publish.
//
// Columns produced by Bun snake_case:
//
//	OTAUpdate              → ota_updates
//	AppProjectID           → app_project_id
//	RuntimeVersion         → runtime_version
//	Platform               → platform
//	UpdateID               → update_id          (UUIDv5-ish derived from manifest bytes)
//	S3Prefix               → s3_prefix          (ota/{appProjectID}/{rv}/{ts})
//	MetadataJSON           → metadata_json      (parsed metadata.json from dist/)
//	ExpoConfigJSON         → expo_config_json   (extracted app.json's expo block)
//	Channel                → channel            (P2#22 — "production"/"staging"/"preview")
//	RolloutPercentage      → rollout_percentage (P2#22 — 0-100, deterministic-hash gated)
//	IsActive               → is_active          (P2#22 — false ⇒ hidden from manifest, used for rollback)
//	RolledBackFromUpdateID → rolled_back_from_update_id (P2#22 — audit pointer to the row this rollback re-promoted away from)
//
// We don't soft-delete OTA updates. Old ones are pruned by the size-cap +
// (P2.1.B) daily cron; both are hard deletes that also clean up S3 objects.
type OTAUpdate struct {
	bun.BaseModel `bun:"table:ota_updates,alias:ota"`

	ID             uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	AppProjectID   uuid.UUID `bun:",notnull,type:uuid" json:"app_project_id"`
	RuntimeVersion string    `bun:",notnull" json:"runtime_version"`
	Platform       string    `bun:",notnull" json:"platform"`
	UpdateID       uuid.UUID `bun:",notnull,type:uuid" json:"update_id"`
	S3Prefix       string    `bun:",notnull" json:"s3_prefix"`
	MetadataJSON   []byte    `bun:"metadata_json,type:jsonb,nullzero" json:"metadata_json,omitempty"`
	ExpoConfigJSON []byte    `bun:"expo_config_json,type:jsonb,nullzero" json:"expo_config_json,omitempty"`
	// <P2#22> ota channels + rollout
	Channel                string     `bun:",notnull,default:'production'" json:"channel"`
	RolloutPercentage      int16      `bun:",notnull,default:100" json:"rollout_percentage"`
	IsActive               bool       `bun:",notnull,default:true" json:"is_active"`
	RolledBackFromUpdateID *uuid.UUID `bun:"rolled_back_from_update_id,type:uuid,nullzero" json:"rolled_back_from_update_id,omitempty"`
	CreatedAt              time.Time  `bun:",nullzero,notnull,default:current_timestamp" json:"created_at"`
}
