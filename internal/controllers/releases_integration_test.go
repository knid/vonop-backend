//go:build integration

package controllers

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
)

// TestRelease_StatusTransitions_Idempotent verifies the per-status helper
// methods keep the row in sync, stamp the matching timestamp column, and
// no-op when re-called on a terminal status.
func TestRelease_StatusTransitions_Idempotent(t *testing.T) {
	db := database.NewTestDB(t,
		(*models.Release)(nil),
	)
	c, _ := NewTestController(t, db, TestControllerOptions{})
	ctx := context.Background()

	rel, err := c.CreateReleaseFromSubmit(ctx, CreateReleaseFromSubmitInput{
		AppProjectID:     uuid.New(),
		OrganizationID:   uuid.New(),
		Platform:         models.ReleasePlatformIOS,
		Track:            models.ReleaseTrackTestFlight,
		VersionCode:      42,
		MarketingVersion: "1.0.0",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if rel.Status != models.ReleaseStatusUploaded {
		t.Fatalf("got status %q, want uploaded", rel.Status)
	}
	if rel.SubmittedAt == nil {
		t.Errorf("expected SubmittedAt set after CreateReleaseFromSubmit")
	}

	// Forward transition: uploaded → in_review → approved → live.
	rel, err = c.MarkReleaseInReview(ctx, rel.ID, "Apple: IN_REVIEW", time.Time{})
	if err != nil || rel.Status != models.ReleaseStatusInReview {
		t.Fatalf("in_review: status=%q err=%v", rel.Status, err)
	}
	rel, err = c.MarkReleaseApproved(ctx, rel.ID, "Apple: PROCESSING_FOR_APP_STORE", time.Time{})
	if err != nil || rel.Status != models.ReleaseStatusApproved {
		t.Fatalf("approved: status=%q err=%v", rel.Status, err)
	}
	if rel.ApprovedAt == nil {
		t.Errorf("expected ApprovedAt set on approved")
	}
	rel, err = c.MarkReleaseLive(ctx, rel.ID, "Apple: READY_FOR_SALE", time.Time{})
	if err != nil || rel.Status != models.ReleaseStatusLive {
		t.Fatalf("live: status=%q err=%v", rel.Status, err)
	}
	if rel.LiveAt == nil {
		t.Errorf("expected LiveAt set on live")
	}

	// Idempotent: marking live again is a no-op (no error, status stays).
	rel2, err := c.MarkReleaseLive(ctx, rel.ID, "Apple: READY_FOR_SALE", time.Time{})
	if err != nil {
		t.Fatalf("idempotent live: %v", err)
	}
	if !rel2.LiveAt.Equal(*rel.LiveAt) {
		t.Errorf("LiveAt changed on idempotent call: was %v now %v", rel.LiveAt, rel2.LiveAt)
	}

	// Cannot revert from terminal (live) → in_review without rollback path.
	rel3, err := c.MarkReleaseInReview(ctx, rel.ID, "should be ignored", time.Time{})
	if err != nil {
		t.Fatalf("terminal forward call returned err: %v", err)
	}
	if rel3.Status != models.ReleaseStatusLive {
		t.Errorf("terminal status changed: got %q, want live", rel3.Status)
	}
}

// TestRelease_RollbackFlipsTerminalState verifies RollbackRelease can move a
// release out of `live` (an otherwise-terminal status) into `rolled_back`,
// while stamping the rollback target with rolled_back_from_release_id.
func TestRelease_RollbackFlipsTerminalState(t *testing.T) {
	db := database.NewTestDB(t,
		(*models.Release)(nil),
	)
	c, _ := NewTestController(t, db, TestControllerOptions{})
	ctx := context.Background()

	appProjectID := uuid.New()
	orgID := uuid.New()
	old, err := c.CreateReleaseFromSubmit(ctx, CreateReleaseFromSubmitInput{
		AppProjectID:   appProjectID,
		OrganizationID: orgID,
		Platform:       models.ReleasePlatformIOS,
		Track:          models.ReleaseTrackProduction,
		VersionCode:    100,
	})
	if err != nil {
		t.Fatalf("create old: %v", err)
	}
	old, _ = c.MarkReleaseLive(ctx, old.ID, "Apple: READY_FOR_SALE", time.Time{})
	if old.Status != models.ReleaseStatusLive {
		t.Fatalf("seed: old status=%q", old.Status)
	}

	target, err := c.CreateReleaseFromSubmit(ctx, CreateReleaseFromSubmitInput{
		AppProjectID:   appProjectID,
		OrganizationID: orgID,
		Platform:       models.ReleasePlatformIOS,
		Track:          models.ReleaseTrackProduction,
		VersionCode:    99,
	})
	if err != nil {
		t.Fatalf("create target: %v", err)
	}

	rolledBack, err := c.RollbackRelease(ctx, old.ID, target.ID)
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if rolledBack.Status != models.ReleaseStatusRolledBack {
		t.Errorf("old status: got %q want rolled_back", rolledBack.Status)
	}
	got, _ := c.DB.GetRelease(ctx, target.ID)
	if got.RolledBackFromReleaseID == nil || *got.RolledBackFromReleaseID != old.ID {
		t.Errorf("target.RolledBackFromReleaseID: got %v, want %v", got.RolledBackFromReleaseID, old.ID)
	}
}

// TestRelease_GetCurrentLiveRelease returns the latest live release on a
// (project, platform), or (nil, nil) when nothing has shipped yet.
func TestRelease_GetCurrentLiveRelease(t *testing.T) {
	db := database.NewTestDB(t,
		(*models.Release)(nil),
	)
	c, _ := NewTestController(t, db, TestControllerOptions{})
	ctx := context.Background()
	appID := uuid.New()

	got, err := c.GetCurrentLiveRelease(ctx, appID, models.ReleasePlatformIOS)
	if err != nil {
		t.Fatalf("empty: %v", err)
	}
	if got != nil {
		t.Errorf("empty: expected nil, got %+v", got)
	}

	first, _ := c.CreateReleaseFromSubmit(ctx, CreateReleaseFromSubmitInput{
		AppProjectID: appID, OrganizationID: uuid.New(),
		Platform: models.ReleasePlatformIOS, Track: models.ReleaseTrackProduction, VersionCode: 1,
	})
	_, _ = c.MarkReleaseLive(ctx, first.ID, "READY_FOR_SALE", time.Now().Add(-time.Hour))

	second, _ := c.CreateReleaseFromSubmit(ctx, CreateReleaseFromSubmitInput{
		AppProjectID: appID, OrganizationID: uuid.New(),
		Platform: models.ReleasePlatformIOS, Track: models.ReleaseTrackProduction, VersionCode: 2,
	})
	_, _ = c.MarkReleaseLive(ctx, second.ID, "READY_FOR_SALE", time.Now())

	got, err = c.GetCurrentLiveRelease(ctx, appID, models.ReleasePlatformIOS)
	if err != nil {
		t.Fatalf("live: %v", err)
	}
	if got == nil || got.ID != second.ID {
		t.Errorf("expected newest (second) live release, got %+v", got)
	}
}

// TestRelease_HandleASCNotification_TransitionsRelease drives the controller
// path the ASC webhook view calls into. Asserts that an inbound notification
// for an in-flight release flips the status correctly + idempotently.
func TestRelease_HandleASCNotification_TransitionsRelease(t *testing.T) {
	db := database.NewTestDB(t,
		(*models.Release)(nil),
	)
	c, _ := NewTestController(t, db, TestControllerOptions{})
	ctx := context.Background()

	rel, err := c.CreateReleaseFromSubmit(ctx, CreateReleaseFromSubmitInput{
		AppProjectID:   uuid.New(),
		OrganizationID: uuid.New(),
		Platform:       models.ReleasePlatformIOS,
		Track:          models.ReleaseTrackProduction,
		VersionCode:    7,
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Stamp a store id so the webhook can find this release by it.
	rel.StoreReleaseID = "asv_12345"
	if _, err := c.DB.UpsertRelease(ctx, rel); err != nil {
		t.Fatalf("set store id: %v", err)
	}

	body, _ := json.Marshal(map[string]any{
		"notificationType": "APP_STORE_VERSION_STATE_UPDATED",
		"data": map[string]any{
			"appStoreVersionId": "asv_12345",
			"appStoreState":     "IN_REVIEW",
		},
	})
	if err := c.HandleASCNotification(ctx, body); err != nil {
		t.Fatalf("handle: %v", err)
	}
	got, _ := c.DB.GetRelease(ctx, rel.ID)
	if got.Status != models.ReleaseStatusInReview {
		t.Errorf("after IN_REVIEW: got %q, want in_review", got.Status)
	}

	// Re-applying the same notification is a no-op; replaying with a
	// terminal status promotes correctly.
	if err := c.HandleASCNotification(ctx, body); err != nil {
		t.Fatalf("replay: %v", err)
	}

	body2, _ := json.Marshal(map[string]any{
		"notificationType": "APP_STORE_VERSION_STATE_UPDATED",
		"data": map[string]any{
			"appStoreVersionId": "asv_12345",
			"appStoreState":     "READY_FOR_SALE",
		},
	})
	if err := c.HandleASCNotification(ctx, body2); err != nil {
		t.Fatalf("ready-for-sale: %v", err)
	}
	got, _ = c.DB.GetRelease(ctx, rel.ID)
	if got.Status != models.ReleaseStatusLive {
		t.Errorf("after READY_FOR_SALE: got %q, want live", got.Status)
	}
}
