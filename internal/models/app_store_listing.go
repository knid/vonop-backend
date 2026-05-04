package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// AppStoreListingScreenshot is a single entry in the screenshots array of
// an AppStoreListing. Stored as JSONB so we can reorder / add canvas state
// without schema churn.
type AppStoreListingScreenshot struct {
	// ID is the S3 storage id (also used for presigned URL lookups).
	ID string `json:"id"`
	// CanvasState is the Fabric.js JSON blob from the screenshot editor.
	// Empty when the screenshot hasn't been edited through the editor.
	CanvasState string `json:"canvas_state,omitempty"`
}

// AppStoreListing is the durable store-listing record per AppProject.
// Replaces the old per-deployment form in the publish wizard so users don't
// have to re-enter icon / title / description / screenshots each time they
// submit a new build. Listings are snapshotted into Deployment rows at
// submit time for audit.
type AppStoreListing struct {
	bun.BaseModel `bun:"table:app_store_listings,alias:asl"`

	ID           uuid.UUID                    `bun:",pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	AppProjectID uuid.UUID                    `bun:",notnull,type:uuid,unique" json:"app_project_id"`
	AppProject   *AppProject                  `bun:"rel:belongs-to,join:app_project_id=id" json:"-"`

	IconS3Key   string                         `bun:",nullzero" json:"icon_s3_key"`
	Title       string                         `bun:",nullzero" json:"title"`
	Description string                         `bun:",nullzero,type:text" json:"description"`
	Locale      string                         `bun:",nullzero,default:'en-US'" json:"locale"`
	Screenshots []AppStoreListingScreenshot    `bun:",type:jsonb,default:'[]'" json:"screenshots"`
	// PrivacyPolicyURL is required by App Store / Play Store when the app
	// uses camera / location / contacts / microphone / photos / bluetooth.
	// Stored here so the pre-submit checklist can validate it.
	PrivacyPolicyURL string `bun:",nullzero" json:"privacy_policy_url"`

	// P1#15 — locale-independent metadata that applies across all listings.
	// Apple App Store Connect refuses submissions without primary category +
	// copyright; Google Play requires the same. EULA defaults to Apple's
	// standard terms unless the developer supplies a custom URL.
	Copyright         string `bun:",nullzero" json:"copyright"`
	PrimaryCategory   string `bun:",nullzero" json:"primary_category"`
	SecondaryCategory string `bun:",nullzero" json:"secondary_category"`
	EULAType          string `bun:",nullzero,default:'apple_default'" json:"eula_type"` // "apple_default" | "custom_url"
	EULAURL           string `bun:",nullzero" json:"eula_url"`

	CreatedAt time.Time `bun:",nullzero,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt time.Time `bun:",nullzero,notnull,default:current_timestamp" json:"updated_at"`
}
