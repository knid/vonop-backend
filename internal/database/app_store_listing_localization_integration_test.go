//go:build integration

package database

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/models"
)

// TestAppStoreListingLocalization_UpsertAndGet exercises the
// UpsertLocalization → GetLocalizationsByListing round trip end-to-end on
// a real Postgres container. Validates:
//  1. Insert path creates a fresh row.
//  2. Conflict path on (listing_id, locale) updates instead of duplicating.
//  3. GetLocalizationsByListing returns rows keyed by locale.
//  4. Two locales for the same listing don't fight the unique index.
func TestAppStoreListingLocalization_UpsertAndGet(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: skipping in short mode")
	}

	db := NewTestDB(t,
		(*models.AppStoreListingLocalization)(nil),
	)

	ctx := context.Background()
	// Add the unique index that production runs via cmd/main.go alterStatements.
	if _, err := db.DB.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS uniq_app_store_listing_localizations_listing_locale ON app_store_listing_localizations (listing_id, locale) WHERE deleted_at IS NULL`); err != nil {
		t.Fatalf("create unique index: %v", err)
	}

	listingID := uuid.New()

	// Insert path
	en := &models.AppStoreListingLocalization{
		ListingID:        listingID,
		Locale:           "en-US",
		Title:            "My App",
		Subtitle:         "Tagline",
		Description:      "Long description here.",
		Keywords:         "kw1,kw2",
		ShortDescription: "Short desc",
		ReleaseNotes:     "Initial release",
		Screenshots: map[string][]string{
			"iphone_6_5": {"s3/a.png", "s3/b.png"},
		},
	}
	if _, err := db.UpsertLocalization(ctx, en); err != nil {
		t.Fatalf("insert en-US: %v", err)
	}

	// Different locale on same listing — must succeed (separate row).
	tr := &models.AppStoreListingLocalization{
		ListingID:   listingID,
		Locale:      "tr",
		Title:       "Uygulamam",
		Description: "Türkçe açıklama",
	}
	if _, err := db.UpsertLocalization(ctx, tr); err != nil {
		t.Fatalf("insert tr: %v", err)
	}

	// Update path — same listing+locale should update, not duplicate.
	enUpdated := &models.AppStoreListingLocalization{
		ListingID:   listingID,
		Locale:      "en-US",
		Title:       "My App v2",
		Description: "Updated description",
	}
	if _, err := db.UpsertLocalization(ctx, enUpdated); err != nil {
		t.Fatalf("update en-US: %v", err)
	}

	got, err := db.GetLocalizationsByListing(ctx, listingID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 locales (en-US + tr), got %d", len(got))
	}
	if row, ok := got["en-US"]; !ok {
		t.Errorf("en-US row missing")
	} else if row.Title != "My App v2" {
		t.Errorf("en-US Title = %q, want %q (update was lost?)", row.Title, "My App v2")
	}
	if row, ok := got["tr"]; !ok {
		t.Errorf("tr row missing")
	} else if row.Title != "Uygulamam" {
		t.Errorf("tr Title = %q", row.Title)
	}
}

// TestAppStoreListingLocalization_DifferentListings verifies the unique
// index scopes by listing_id — two listings can both have an en-US row.
func TestAppStoreListingLocalization_DifferentListings(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: skipping in short mode")
	}

	db := NewTestDB(t, (*models.AppStoreListingLocalization)(nil))
	ctx := context.Background()
	if _, err := db.DB.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS uniq_app_store_listing_localizations_listing_locale ON app_store_listing_localizations (listing_id, locale) WHERE deleted_at IS NULL`); err != nil {
		t.Fatalf("create unique index: %v", err)
	}

	a := uuid.New()
	b := uuid.New()
	if _, err := db.UpsertLocalization(ctx, &models.AppStoreListingLocalization{ListingID: a, Locale: "en-US", Title: "A"}); err != nil {
		t.Fatalf("upsert listing A: %v", err)
	}
	if _, err := db.UpsertLocalization(ctx, &models.AppStoreListingLocalization{ListingID: b, Locale: "en-US", Title: "B"}); err != nil {
		t.Fatalf("upsert listing B (different listing, same locale): %v", err)
	}

	gotA, err := db.GetLocalizationsByListing(ctx, a)
	if err != nil {
		t.Fatalf("get A: %v", err)
	}
	if len(gotA) != 1 || gotA["en-US"].Title != "A" {
		t.Errorf("listing A en-US: got %+v", gotA)
	}
	gotB, err := db.GetLocalizationsByListing(ctx, b)
	if err != nil {
		t.Fatalf("get B: %v", err)
	}
	if len(gotB) != 1 || gotB["en-US"].Title != "B" {
		t.Errorf("listing B en-US: got %+v", gotB)
	}
}
