package controllers

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/utils"
)

// SyncFlyBandwidthTask is invoked by the daily cron. For every live Fly app
// (runtime_apps.fly_app_name != ''), it queries Fly's GraphQL billing data
// usage, aggregates per-org egress bytes, and upserts the per-period row
// into app_bandwidth_usages. The deploy gate later reads this table to block
// new deploys when an org has exceeded its MonthlyBandwidthGB quota.
//
// Per-app errors are logged and skipped — one bad app shouldn't poison the
// rollup for the rest of the org. Wall-clock budget: a few hundred apps at
// ~1s/query each fits comfortably in the 20-minute task timeout.
func (c *Controller) SyncFlyBandwidthTask(ctx context.Context) error {
	if c.FlyService == nil {
		return nil
	}
	rows, err := c.DB.ListRuntimeAppsWithOrg(ctx)
	if err != nil {
		return err
	}
	now := time.Now()

	// Aggregate egress bytes by org, then anchor to the org's billing window
	// at write time. Two-pass so a single Fly query failure doesn't hide
	// the rest of an org's traffic — each app's error is logged and skipped.
	bytesByOrg := make(map[uuid.UUID]int64, len(rows))
	for _, r := range rows {
		orgID, err := uuid.Parse(r.OrganizationID)
		if err != nil {
			continue
		}
		bytes, err := c.FlyService.GetAppEgressBytes(r.FlyAppName)
		if err != nil {
			slog.Warn("fly_bandwidth_query_failed",
				"fly_app", r.FlyAppName, "org_id", orgID, "error", err)
			continue
		}
		bytesByOrg[orgID] += bytes
	}

	written := 0
	for orgID, bytes := range bytesByOrg {
		org, oerr := c.DB.GetOrganizationByID(orgID.String())
		if oerr != nil {
			slog.Warn("fly_bandwidth_org_lookup_failed",
				"org_id", orgID, "error", oerr)
			continue
		}
		periodStart, periodEnd := utils.BillingPeriod(org.SubscriptionStartAt, now)
		row := &models.AppBandwidthUsage{
			OrganizationID: orgID,
			PeriodStart:    periodStart,
			PeriodEnd:      periodEnd,
			BytesUsed:      bytes,
			LastSyncedAt:   now,
		}
		if err := c.DB.UpsertBandwidthUsage(ctx, row); err != nil {
			slog.Warn("fly_bandwidth_upsert_failed",
				"org_id", orgID, "error", err)
			continue
		}
		written++
	}
	slog.Info("fly_bandwidth_sync_completed",
		"app_count", len(rows), "org_count", written)
	return nil
}
