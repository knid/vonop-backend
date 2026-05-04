package database

import (
	"context"
	"time"

	"github.com/knid/wemob/internal/models"
)

// UpsertMobileSubscription inserts or updates the subscription row keyed by
// (app_project_id, rc_app_user_id, product_id). The unique index on those
// three columns (created in createSchema()) backs the ON CONFLICT clause.
func (db *Database) UpsertMobileSubscription(ctx context.Context, m *models.MobileSubscription) (*models.MobileSubscription, error) {
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now().UTC()
	}
	m.UpdatedAt = time.Now().UTC()

	_, err := db.DB.NewInsert().Model(m).
		On("CONFLICT (app_project_id, rc_app_user_id, product_id) WHERE deleted_at IS NULL DO UPDATE").
		Set("entitlement_id = EXCLUDED.entitlement_id").
		Set("status = EXCLUDED.status").
		Set("store = EXCLUDED.store").
		Set("period_start = EXCLUDED.period_start").
		Set("period_end = EXCLUDED.period_end").
		Set("canceled_at = EXCLUDED.canceled_at").
		Set("country_code = EXCLUDED.country_code").
		Set("price_cents = EXCLUDED.price_cents").
		Set("currency = EXCLUDED.currency").
		Set("is_trial = EXCLUDED.is_trial").
		Set("is_lifetime = EXCLUDED.is_lifetime").
		Set("updated_at = now()").
		Returning("*").
		Exec(ctx)
	return m, err
}

// GetMobileSubscription returns the row for a given (project, rc_app_user_id, product_id).
// Returns nil, nil when no row exists.
func (db *Database) GetMobileSubscription(ctx context.Context, appProjectID, rcAppUserID, productID string) (*models.MobileSubscription, error) {
	var m models.MobileSubscription
	err := db.DB.NewSelect().Model(&m).
		Where("app_project_id = ?", appProjectID).
		Where("rc_app_user_id = ?", rcAppUserID).
		Where("product_id = ?", productID).
		Scan(ctx)
	if err != nil {
		// Caller decides if "no rows" is fatal.
		return nil, err
	}
	return &m, nil
}

// MarkMobileSubscriptionStatus updates only the status (and optionally
// canceled_at) for a row keyed by (project, rc_app_user_id, product_id).
// Returns the number of rows updated.
func (db *Database) MarkMobileSubscriptionStatus(ctx context.Context, appProjectID, rcAppUserID, productID, status string, canceledAt *time.Time) (int64, error) {
	q := db.DB.NewUpdate().
		Model((*models.MobileSubscription)(nil)).
		Set("status = ?", status).
		Set("updated_at = now()").
		Where("app_project_id = ?", appProjectID).
		Where("rc_app_user_id = ?", rcAppUserID).
		Where("product_id = ?", productID)
	if canceledAt != nil {
		q = q.Set("canceled_at = ?", *canceledAt)
	}
	res, err := q.Exec(ctx)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// ListActiveMobileSubscriptionsByProject returns every row whose status is
// "active" or "in_grace_period" for the given project.
func (db *Database) ListActiveMobileSubscriptionsByProject(ctx context.Context, appProjectID string) ([]models.MobileSubscription, error) {
	var rows []models.MobileSubscription
	err := db.DB.NewSelect().Model(&rows).
		Where("app_project_id = ?", appProjectID).
		Where("status IN (?, ?)",
			models.MobileSubscriptionStatusActive,
			models.MobileSubscriptionStatusInGracePeriod,
		).
		Order("created_at DESC").
		Scan(ctx)
	return rows, err
}

// CountActiveMobileSubscriptionsByProject returns the count of rows in the
// active set for the project (active + in_grace_period).
func (db *Database) CountActiveMobileSubscriptionsByProject(ctx context.Context, appProjectID string) (int, error) {
	return db.DB.NewSelect().
		Model((*models.MobileSubscription)(nil)).
		Where("app_project_id = ?", appProjectID).
		Where("status IN (?, ?)",
			models.MobileSubscriptionStatusActive,
			models.MobileSubscriptionStatusInGracePeriod,
		).
		Count(ctx)
}

// MobileSubscriptionSummary is the projection returned by SummarizeMobileSubscriptions.
type MobileSubscriptionSummary struct {
	ActiveCount   int   `json:"active_count"`
	MRRCents      int64 `json:"mrr_cents"`
	Churn30d      int   `json:"churn_30d"`
	TrialsCount   int   `json:"trials_count"`
}

// SummarizeMobileSubscriptions returns the four panel numbers used by the
// Studio "Subscriptions" panel:
//
//   - active_count: rows in (active, in_grace_period)
//   - mrr_cents: sum of price_cents over the active set
//   - churn_30d: rows with status canceled OR expired and canceled_at >= now-30d
//   - trials_count: active rows where is_trial is true
//
// MRR is a coarse proxy: we don't normalise period length here (a yearly
// subscription gets counted at its full price_cents). Good enough for a
// single-line dashboard widget; not a finance system.
func (db *Database) SummarizeMobileSubscriptions(ctx context.Context, appProjectID string) (*MobileSubscriptionSummary, error) {
	out := &MobileSubscriptionSummary{}

	if err := db.DB.NewSelect().
		Model((*models.MobileSubscription)(nil)).
		ColumnExpr("COUNT(*) AS active_count").
		ColumnExpr("COALESCE(SUM(price_cents), 0) AS mrr_cents").
		ColumnExpr("SUM(CASE WHEN is_trial THEN 1 ELSE 0 END) AS trials_count").
		Where("app_project_id = ?", appProjectID).
		Where("status IN (?, ?)",
			models.MobileSubscriptionStatusActive,
			models.MobileSubscriptionStatusInGracePeriod,
		).
		Scan(ctx, out); err != nil {
		return nil, err
	}

	churnSince := time.Now().UTC().Add(-30 * 24 * time.Hour)
	churn, err := db.DB.NewSelect().
		Model((*models.MobileSubscription)(nil)).
		Where("app_project_id = ?", appProjectID).
		Where("status IN (?, ?)",
			models.MobileSubscriptionStatusCanceled,
			models.MobileSubscriptionStatusExpired,
		).
		Where("canceled_at IS NOT NULL AND canceled_at >= ?", churnSince).
		Count(ctx)
	if err != nil {
		return nil, err
	}
	out.Churn30d = churn

	return out, nil
}

// GetMobileAppConfigByRCProject finds the MobileAppConfig whose
// revenuecat_project_id matches the given RC project id. Returns (nil, nil)
// when no row matches — RC events for projects we don't recognise are
// expected (admin-deleted apps, dev/test apps).
func (db *Database) GetMobileAppConfigByRCProject(ctx context.Context, rcProjectID string) (*models.MobileAppConfig, error) {
	if rcProjectID == "" {
		return nil, nil
	}
	var cfg models.MobileAppConfig
	err := db.DB.NewSelect().Model(&cfg).
		Where("revenuecat_project_id = ?", rcProjectID).
		Limit(1).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}
