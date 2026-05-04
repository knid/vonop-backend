package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// AppStoreListingLocalization is a per-locale set of store-listing copy +
// screenshots. Apple supports ~40 storefront locales and Google Play ~80,
// each with independent title / subtitle / description / keywords /
// screenshots. The parent AppStoreListing keeps the locale-independent
// fields (categories, copyright, EULA) and a default locale; this table
// holds the per-locale copy that ships to the stores.
//
// Stored as a sibling table (not jsonb on AppStoreListing) so we can
// upsert one locale at a time from the wizard without re-writing the
// whole listing — and so a future per-locale screenshot upload UI can
// fan out without growing a single jsonb blob to multi-MB sizes.
//
// Unique on (listing_id, locale). Soft-deleted rows shouldn't fight the
// unique index because Bun's soft_delete filter only affects SELECT — we
// reset deleted_at = NULL on UPSERT to recover from prior soft-deletes.
type AppStoreListingLocalization struct {
	bun.BaseModel `bun:"table:app_store_listing_localizations,alias:asll"`

	ID        uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	ListingID uuid.UUID `bun:",notnull,type:uuid" json:"listing_id"`
	// Listing relation is intentionally light — call sites load the parent
	// AppStoreListing separately via GetAppStoreListing(projectID).
	Locale string `bun:",notnull" json:"locale"` // e.g. "en-US", "tr", "de", "es-MX"

	// Apple + Play shared copy
	Title        string `bun:",nullzero" json:"title"`
	Description  string `bun:",nullzero,type:text" json:"description"` // Play "full description"; iOS description
	ReleaseNotes string `bun:",nullzero,type:text" json:"release_notes"`

	// Apple-specific (ignored on Play submissions)
	Subtitle        string `bun:",nullzero" json:"subtitle"`         // 30 chars
	PromotionalText string `bun:",nullzero,type:text" json:"promotional_text"` // 170 chars
	Keywords        string `bun:",nullzero" json:"keywords"`         // 100 chars CSV

	// Play-specific (ignored on Apple submissions)
	ShortDescription string `bun:",nullzero" json:"short_description"` // 80 chars

	// Common URLs
	SupportURL   string `bun:",nullzero" json:"support_url"`
	MarketingURL string `bun:",nullzero" json:"marketing_url"`

	// Per-locale screenshots: device_type → list of S3 keys.
	// Apple device_type values: "iphone_6_5", "iphone_6_7", "ipad_12_9", etc.
	// Play device_type values: "phone", "seven_inch", "ten_inch", "tv", "wear".
	// jsonb so the schema can grow new device types without a migration.
	Screenshots map[string][]string `bun:",type:jsonb,default:'{}'" json:"screenshots"`

	CreatedAt time.Time `bun:",nullzero,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt time.Time `bun:",nullzero,notnull,default:current_timestamp" json:"updated_at"`
	DeletedAt time.Time `bun:",soft_delete,nullzero" json:"-"`
}
