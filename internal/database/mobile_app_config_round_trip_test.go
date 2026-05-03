package database

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/models"
)

// TestMobileAppConfig_LandingFields_JSONRoundTrip is a pure-Go unit (no DB)
// guarding against accidental rename / drop of the json tags on the three
// P3.1 landing-page fields. The fields are surfaced to the agent via
// read_mobile_config (which uses these JSON tags through Bun's struct
// reflection on insert), so a tag rename would silently break the agent
// without breaking any existing tests.
func TestMobileAppConfig_LandingFields_JSONRoundTrip(t *testing.T) {
	cfg := &models.MobileAppConfig{
		ProjectID:            uuid.New(),
		AppStoreURL:          "https://apps.apple.com/app/example/id123",
		PlayStoreURL:         "https://play.google.com/store/apps/details?id=com.example",
		LandingPagePublished: true,
	}

	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := string(raw)
	if !contains(body, `"app_store_url":"https://apps.apple.com/app/example/id123"`) {
		t.Errorf("missing app_store_url JSON tag; got: %s", body)
	}
	if !contains(body, `"play_store_url":"https://play.google.com/store/apps/details?id=com.example"`) {
		t.Errorf("missing play_store_url JSON tag; got: %s", body)
	}
	if !contains(body, `"landing_page_published":true`) {
		t.Errorf("missing landing_page_published JSON tag; got: %s", body)
	}

	var back models.MobileAppConfig
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.AppStoreURL != cfg.AppStoreURL {
		t.Errorf("AppStoreURL round-trip: got %q, want %q", back.AppStoreURL, cfg.AppStoreURL)
	}
	if back.PlayStoreURL != cfg.PlayStoreURL {
		t.Errorf("PlayStoreURL round-trip: got %q, want %q", back.PlayStoreURL, cfg.PlayStoreURL)
	}
	if back.LandingPagePublished != cfg.LandingPagePublished {
		t.Errorf("LandingPagePublished round-trip: got %v, want %v",
			back.LandingPagePublished, cfg.LandingPagePublished)
	}
}

// contains is a tiny stdlib-free helper so the test file stays self-contained.
func contains(haystack, needle string) bool {
	if len(needle) > len(haystack) {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
