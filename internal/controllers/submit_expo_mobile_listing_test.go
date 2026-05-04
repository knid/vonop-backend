package controllers

import (
	"testing"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/models"
)

// TestListingLocalesForSubmit_PrefersPerLocaleRows verifies that when a
// per-locale row exists for the parent listing's default locale, the
// per-locale row's data wins (not the parent's legacy single-locale
// Title / Description fields). This is the load-bearing assertion for
// the bug we're fixing — the wizard previously typed into the parent,
// new rows in the localization table should override.
func TestListingLocalesForSubmit_PrefersPerLocaleRows(t *testing.T) {
	listing := &models.AppStoreListing{
		ID:          uuid.New(),
		Title:       "Legacy Title",
		Description: "Legacy Description",
		Locale:      "en-US",
	}
	locales := map[string]*models.AppStoreListingLocalization{
		"en-US": {
			ListingID:   listing.ID,
			Locale:      "en-US",
			Title:       "Per-Locale Title",
			Description: "Per-Locale Description",
			Subtitle:    "A subtitle",
			Keywords:    "kw1,kw2",
		},
		"tr": {
			ListingID:   listing.ID,
			Locale:      "tr",
			Title:       "Türkçe Başlık",
			Description: "Türkçe açıklama",
		},
	}

	out := listingLocalesForSubmit(listing, locales)
	if len(out) != 2 {
		t.Fatalf("expected 2 locales, got %d", len(out))
	}
	// Verify the en-US entry comes from the per-locale row (not the parent).
	var enFound, trFound bool
	for _, l := range out {
		switch l.Locale {
		case "en-US":
			enFound = true
			if l.Title != "Per-Locale Title" {
				t.Errorf("en-US Title = %q, want per-locale value (parent's Title leaked through)", l.Title)
			}
			if l.Subtitle != "A subtitle" {
				t.Errorf("en-US Subtitle = %q, want %q", l.Subtitle, "A subtitle")
			}
			if l.Keywords != "kw1,kw2" {
				t.Errorf("en-US Keywords = %q", l.Keywords)
			}
		case "tr":
			trFound = true
			if l.Title != "Türkçe Başlık" {
				t.Errorf("tr Title = %q", l.Title)
			}
		}
	}
	if !enFound {
		t.Errorf("en-US row missing from output")
	}
	if !trFound {
		t.Errorf("tr row missing from output")
	}
}

// TestListingLocalesForSubmit_BackfillFromParent verifies the legacy
// single-locale path still works: when no per-locale row exists for the
// parent's default Locale, we synthesize one from the parent's
// Title/Description so the submission still carries copy.
func TestListingLocalesForSubmit_BackfillFromParent(t *testing.T) {
	listing := &models.AppStoreListing{
		ID:          uuid.New(),
		Title:       "Single-Locale App",
		Description: "Description in en-US",
		Locale:      "en-US",
	}
	out := listingLocalesForSubmit(listing, nil)
	if len(out) != 1 {
		t.Fatalf("expected 1 backfilled locale, got %d", len(out))
	}
	if out[0].Locale != "en-US" || out[0].Title != "Single-Locale App" {
		t.Errorf("backfill: got %+v", out[0])
	}
}

// TestListingLocalesForSubmit_NilListing returns an empty slice (the
// submit caller treats a missing listing as "publish without per-locale
// metadata" and keeps existing App Store / Play Console copy).
func TestListingLocalesForSubmit_NilListing(t *testing.T) {
	out := listingLocalesForSubmit(nil, nil)
	if out != nil {
		t.Errorf("expected nil for nil listing, got %+v", out)
	}
}

// TestListingLocalesForSubmit_DefaultLocaleFallback handles the edge case
// where the parent's Locale field is empty — we default to en-US so we
// don't end up with a Locale=""  entry (which Apple would reject).
func TestListingLocalesForSubmit_DefaultLocaleFallback(t *testing.T) {
	listing := &models.AppStoreListing{
		ID:          uuid.New(),
		Title:       "T",
		Description: "D",
		Locale:      "", // empty
	}
	out := listingLocalesForSubmit(listing, nil)
	if len(out) != 1 || out[0].Locale != "en-US" {
		t.Errorf("default locale fallback: got %+v", out)
	}
}
