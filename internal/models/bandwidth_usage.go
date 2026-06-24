package models

import (
	"time"

	"github.com/google/uuid"
)

// AppBandwidthUsage tracks per-org Fly egress bytes within a billing period.
// Updated by a daily cron that queries Fly's GraphQL billingDataUsage. The
// period_start matches the org's SubscriptionStartAt anchored to the current
// month. One row per (organization_id, period_start); the cron upserts.
type AppBandwidthUsage struct {
	ID             uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	OrganizationID uuid.UUID `bun:",notnull,type:uuid"`
	PeriodStart    time.Time `bun:"period_start,notnull"`
	PeriodEnd      time.Time `bun:"period_end,notnull"`
	BytesUsed      int64     `bun:"bytes_used,notnull,default:0"`
	LastSyncedAt   time.Time `bun:"last_synced_at,nullzero,notnull,default:current_timestamp"`
	CreatedAt      time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt      time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}
