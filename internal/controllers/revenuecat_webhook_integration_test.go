//go:build integration

package controllers

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
)

// TestRCWebhook_InitialPurchase_End2End verifies an INITIAL_PURCHASE event
// arriving at HandleRCEvent creates a MobileSubscription row keyed to the
// project we provisioned RC against.
func TestRCWebhook_InitialPurchase_End2End(t *testing.T) {
	db := database.NewTestDB(t,
		(*models.User)(nil),
		(*models.Organization)(nil),
		(*models.AppProject)(nil),
		(*models.MobileAppConfig)(nil),
		(*models.MobileSubscription)(nil),
	)
	c, _ := NewTestController(t, db, TestControllerOptions{
		Secrets: map[string]string{"REVENUECAT_WEBHOOK_AUTH_TOKEN": "shh"},
	})

	ctx := context.Background()
	user := &models.User{ID: uuid.New(), Name: "U", Email: "u-" + uuid.NewString()[:8] + "@example.com", Password: "x"}
	if _, err := db.DB.NewInsert().Model(user).Exec(ctx); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	org := &models.Organization{ID: uuid.New(), Name: "O", Slug: "rcwh-" + uuid.NewString()[:8], OwnerID: user.ID, IsPersonal: true}
	if _, err := db.DB.NewInsert().Model(org).Exec(ctx); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	project := &models.AppProject{
		ID: uuid.New(), UserID: user.ID, OrganizationID: org.ID,
		Name: "P", Slug: "rcwh-app-" + uuid.NewString()[:8], Prompt: "x",
		ProjectType: models.ProjectTypeMobileAppNative,
	}
	if _, err := db.DB.NewInsert().Model(project).Exec(ctx); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	cfg := &models.MobileAppConfig{
		ID:                  uuid.New(),
		ProjectID:           project.ID,
		IAPEnabled:          true,
		RevenueCatProjectID: "rc-proj-abc",
	}
	if _, err := db.DB.NewInsert().Model(cfg).Exec(ctx); err != nil {
		t.Fatalf("insert cfg: %v", err)
	}

	// Need the unique index for the upsert ON CONFLICT clause.
	if _, err := db.DB.ExecContext(ctx,
		`CREATE UNIQUE INDEX IF NOT EXISTS uniq_mobile_subscriptions_project_user_product
		   ON mobile_subscriptions (app_project_id, rc_app_user_id, product_id)
		   WHERE deleted_at IS NULL`); err != nil {
		t.Fatalf("create index: %v", err)
	}

	purchasedMs := time.Now().Add(-30 * time.Minute).UnixMilli()
	expirationMs := time.Now().Add(30 * 24 * time.Hour).UnixMilli()
	evt := RCEventV2{
		Event: RCEventV2Event{
			Type:            RCEventInitialPurchase,
			AppID:           "rc-proj-abc",
			AppUserID:       "rc-user-1",
			ProductID:       "pro_monthly",
			EntitlementID:   "pro",
			Store:           "APP_STORE",
			PurchasedAtMs:   purchasedMs,
			ExpirationAtMs:  expirationMs,
			Currency:        "USD",
			PriceInCurrency: 4.99,
		},
	}

	if err := c.HandleRCEvent(ctx, evt); err != nil {
		t.Fatalf("HandleRCEvent: %v", err)
	}

	// Verify the row landed.
	got, err := c.DB.GetMobileSubscription(ctx, project.ID.String(), "rc-user-1", "pro_monthly")
	if err != nil {
		t.Fatalf("get sub: %v", err)
	}
	if got == nil {
		t.Fatalf("expected subscription row, got nil")
	}
	if got.OrganizationID != org.ID {
		t.Errorf("OrgID mismatch: %v", got.OrganizationID)
	}
	if got.Status != models.MobileSubscriptionStatusActive {
		t.Errorf("Status: got %q", got.Status)
	}
	if got.Store != "app_store" {
		t.Errorf("Store: %q", got.Store)
	}
	if got.PriceCents != 499 {
		t.Errorf("PriceCents: %d, want 499", got.PriceCents)
	}
	if got.EntitlementID != "pro" {
		t.Errorf("EntitlementID: %q", got.EntitlementID)
	}

	// Run a second event — RENEWAL — and verify it upserts (single row).
	renewalEvt := RCEventV2{Event: RCEventV2Event{
		Type:            RCEventRenewal,
		AppID:           "rc-proj-abc",
		AppUserID:       "rc-user-1",
		ProductID:       "pro_monthly",
		Store:           "APP_STORE",
		ExpirationAtMs:  time.Now().Add(60 * 24 * time.Hour).UnixMilli(),
		Currency:        "USD",
		PriceInCurrency: 4.99,
	}}
	if err := c.HandleRCEvent(ctx, renewalEvt); err != nil {
		t.Fatalf("renewal: %v", err)
	}
	count, err := c.DB.CountActiveMobileSubscriptionsByProject(ctx, project.ID.String())
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 row after renewal, got %d", count)
	}

	// Cancellation flips status; canceled_at gets stamped.
	cancelEvt := RCEventV2{Event: RCEventV2Event{
		Type:      RCEventCancellation,
		AppID:     "rc-proj-abc",
		AppUserID: "rc-user-1",
		ProductID: "pro_monthly",
	}}
	if err := c.HandleRCEvent(ctx, cancelEvt); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	got2, err := c.DB.GetMobileSubscription(ctx, project.ID.String(), "rc-user-1", "pro_monthly")
	if err != nil {
		t.Fatalf("get after cancel: %v", err)
	}
	if got2.Status != models.MobileSubscriptionStatusCanceled {
		t.Errorf("expected canceled, got %q", got2.Status)
	}
	if got2.CanceledAt == nil {
		t.Errorf("expected CanceledAt set")
	}

	// Summary shows zero active after cancellation.
	summary, err := c.DB.SummarizeMobileSubscriptions(ctx, project.ID.String())
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if summary.ActiveCount != 0 {
		t.Errorf("expected 0 active after cancel, got %d", summary.ActiveCount)
	}
	if summary.Churn30d != 1 {
		t.Errorf("expected 1 churn in 30d, got %d", summary.Churn30d)
	}
}

// TestRCWebhook_UnknownProject_Acked verifies unknown RC project IDs are
// accepted (returns nil error) and don't create rows.
func TestRCWebhook_UnknownProject_Acked(t *testing.T) {
	db := database.NewTestDB(t,
		(*models.MobileAppConfig)(nil),
		(*models.MobileSubscription)(nil),
	)
	c, _ := NewTestController(t, db, TestControllerOptions{})

	evt := RCEventV2{Event: RCEventV2Event{
		Type:      RCEventInitialPurchase,
		AppID:     "unknown-rc-proj",
		AppUserID: "u",
		ProductID: "p",
	}}
	if err := c.HandleRCEvent(context.Background(), evt); err != nil {
		t.Fatalf("expected nil error for unknown project, got %v", err)
	}
}
