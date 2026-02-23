package models

import (
	"time"

	"github.com/google/uuid"
)

// WebAnalyticsEvent stores a single analytics event from a deployed web app.
// Append-only — no UpdatedAt or soft-delete.
type WebAnalyticsEvent struct {
	ID         uuid.UUID      `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	ProjectID  uuid.UUID      `bun:",notnull,type:uuid"`
	AppProject AppProject     `bun:"rel:belongs-to,join:project_id=id"`
	VisitorID  string         `bun:",notnull"`
	SessionID  string         `bun:",notnull"`
	EventType  string         `bun:",notnull"` // pageview, pageleave, event
	EventName  string         `bun:",nullzero"` // for custom events
	URL        string         `bun:",nullzero"`
	Path       string         `bun:",nullzero"`
	Referrer   string         `bun:",nullzero"`
	Title      string         `bun:",nullzero"`
	UserAgent  string         `bun:",nullzero"`
	ScreenSize string         `bun:",nullzero"`
	Language   string         `bun:",nullzero"`
	UTMSource   string        `bun:"utm_source,nullzero"`
	UTMMedium   string        `bun:"utm_medium,nullzero"`
	UTMCampaign string        `bun:"utm_campaign,nullzero"`
	UTMTerm     string        `bun:"utm_term,nullzero"`
	UTMContent  string        `bun:"utm_content,nullzero"`
	ScrollDepth int           `bun:",nullzero"` // 0-100
	Duration    int           `bun:",nullzero"` // seconds
	Country     string        `bun:",nullzero"`
	City        string        `bun:",nullzero"`
	Properties  map[string]any `bun:",type:jsonb,nullzero"`
	CreatedAt   time.Time     `bun:"created_at,nullzero,notnull,default:current_timestamp"`
}
