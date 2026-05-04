package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// MobileSubscription is the backend's mirror of a single RevenueCat subscriber's
// state for one (project, app_user_id, product_id) tuple. Rows are written
// exclusively from the RC v2 webhook (POST /v2/webhooks/revenuecat) — never
// from the mobile binary; the RC SDK is the source of truth on-device.
//
// RCAppUserID is whatever RevenueCat sends in the event:
//   - the Better-Auth user id when the user was logged in at purchase time
//   - "$RCAnonymousID:<uuid>" when the user was anonymous
//
// We keep both shapes verbatim so the dashboard can correlate later if/when
// the user signs in (RC's SUBSCRIBER_ALIAS event triggers a join).
//
// Status is one of:
//   - "active"           — initial purchase / renewal landed cleanly
//   - "in_grace_period"  — billing failed but RC is still retrying
//   - "billing_issue"    — payment retries exhausted, entitlement still on
//   - "canceled"         — user opted out; stays active until period_end
//   - "expired"          — period_end passed without a renewal
//
// Store is "app_store" | "play_store" | "stripe" | "amazon" | "promotional".
type MobileSubscription struct {
	bun.BaseModel `bun:"table:mobile_subscriptions,alias:msub"`

	ID             uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	AppProjectID   uuid.UUID `bun:",notnull,type:uuid" json:"app_project_id"`
	OrganizationID uuid.UUID `bun:",notnull,type:uuid" json:"organization_id"`

	RCAppUserID   string `bun:",notnull" json:"rc_app_user_id"`
	ProductID     string `bun:",notnull" json:"product_id"`
	EntitlementID string `bun:",nullzero" json:"entitlement_id"`
	Status        string `bun:",notnull,default:'active'" json:"status"`
	Store         string `bun:",notnull,default:''" json:"store"`

	PeriodStart time.Time  `bun:",nullzero" json:"period_start"`
	PeriodEnd   time.Time  `bun:",nullzero" json:"period_end"`
	CanceledAt  *time.Time `bun:",nullzero" json:"canceled_at,omitempty"`

	CountryCode string `bun:",nullzero" json:"country_code"`
	PriceCents  int64  `bun:",notnull,default:0" json:"price_cents"`
	Currency    string `bun:",nullzero" json:"currency"`

	IsTrial    bool `bun:",notnull,default:false" json:"is_trial"`
	IsLifetime bool `bun:",notnull,default:false" json:"is_lifetime"`

	CreatedAt time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp" json:"updated_at"`
	DeletedAt time.Time `bun:",soft_delete,nullzero" json:"-"`
}

// MobileSubscription Status enum — string constants so callers don't typo.
const (
	MobileSubscriptionStatusActive         = "active"
	MobileSubscriptionStatusInGracePeriod  = "in_grace_period"
	MobileSubscriptionStatusBillingIssue   = "billing_issue"
	MobileSubscriptionStatusCanceled       = "canceled"
	MobileSubscriptionStatusExpired        = "expired"
	MobileSubscriptionStatusNonRenewing    = "non_renewing"
)
