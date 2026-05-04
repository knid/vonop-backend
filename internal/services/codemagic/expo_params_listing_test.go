package codemagic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

// TestTriggerExpoAndroidDeploy_PassesLocalesJSON verifies that per-locale
// metadata is serialized as CM_LOCALES_JSON when triggering the deploy
// workflow. This is the load-bearing assertion for P1#15: per-locale copy
// must reach Codemagic so the workflow can fan it out to fastlane folders.
func TestTriggerExpoAndroidDeploy_PassesLocalesJSON(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"build_id": "x"})
	}))
	defer server.Close()

	s := newServiceForTest(t, server.URL, "t", "a")
	params := ExpoAndroidDeployParams{
		ProjectID:   uuid.New(),
		BuildID:     uuid.New(),
		TaskID:      uuid.New(),
		PackageName: "com.x.y",
		BuildNumber: 1,
		AABS3Key:    "k",
		Locales: []ListingLocale{
			{Locale: "en-US", Title: "Hi", ShortDescription: "Sd", Description: "Full", ReleaseNotes: "Notes"},
			{Locale: "tr", Title: "Merhaba"},
		},
		Copyright:       "© 2026 Wemob",
		PrimaryCategory: "PRODUCTIVITY",
		EULAType:        "apple_default",
	}
	if _, err := s.TriggerExpoAndroidDeploy(context.Background(), params); err != nil {
		t.Fatalf("trigger: %v", err)
	}
	vars := captured["environment"].(map[string]any)["variables"].(map[string]any)
	got, ok := vars["CM_LOCALES_JSON"].(string)
	if !ok || got == "" || got == "[]" {
		t.Fatalf("CM_LOCALES_JSON missing or empty: %q", got)
	}
	// Round-trip and assert structure.
	var parsed []ListingLocale
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("CM_LOCALES_JSON not valid JSON: %v\nraw: %s", err, got)
	}
	if len(parsed) != 2 {
		t.Errorf("expected 2 locales in JSON, got %d", len(parsed))
	}
	if parsed[0].Title != "Hi" {
		t.Errorf("first locale Title = %q, want Hi", parsed[0].Title)
	}
	if vars["CM_COPYRIGHT"].(string) != "© 2026 Wemob" {
		t.Errorf("CM_COPYRIGHT: got %v", vars["CM_COPYRIGHT"])
	}
	if vars["CM_PRIMARY_CATEGORY"].(string) != "PRODUCTIVITY" {
		t.Errorf("CM_PRIMARY_CATEGORY: got %v", vars["CM_PRIMARY_CATEGORY"])
	}
	if vars["CM_EULA_TYPE"].(string) != "apple_default" {
		t.Errorf("CM_EULA_TYPE: got %v", vars["CM_EULA_TYPE"])
	}
}

// TestTriggerExpoIOSDeploy_PassesLocalesJSON confirms iOS deploy also
// emits CM_LOCALES_JSON. Apple cares about more fields than Play (subtitle,
// keywords, promotional_text) so verify those round-trip too.
func TestTriggerExpoIOSDeploy_PassesLocalesJSON(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"build_id": "x"})
	}))
	defer server.Close()

	s := newServiceForTest(t, server.URL, "t", "a")
	params := ExpoIOSDeployParams{
		ProjectID:   uuid.New(),
		BuildID:     uuid.New(),
		TaskID:      uuid.New(),
		BundleID:    "com.x.y",
		BuildNumber: 1,
		IPAS3Key:    "k",
		Locales: []ListingLocale{
			{
				Locale:          "en-US",
				Title:           "App",
				Subtitle:        "Tag",
				Description:     "Body",
				Keywords:        "kw1,kw2",
				PromotionalText: "Promo",
				ReleaseNotes:    "Bug fixes",
			},
		},
	}
	if _, err := s.TriggerExpoIOSDeploy(context.Background(), params); err != nil {
		t.Fatalf("trigger: %v", err)
	}
	vars := captured["environment"].(map[string]any)["variables"].(map[string]any)
	got, ok := vars["CM_LOCALES_JSON"].(string)
	if !ok || got == "" || got == "[]" {
		t.Fatalf("CM_LOCALES_JSON missing/empty: %q", got)
	}
	var parsed []ListingLocale
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(parsed) != 1 {
		t.Fatalf("expected 1 locale, got %d", len(parsed))
	}
	l := parsed[0]
	if l.Subtitle != "Tag" || l.Keywords != "kw1,kw2" || l.PromotionalText != "Promo" {
		t.Errorf("apple-specific fields not round-tripped: %+v", l)
	}
}

// TestLocalesJSON_EmptyReturnsBracketBracket guards the workflow's jq
// parse step from a literal empty/unset value — emit "[]" for nil.
func TestLocalesJSON_EmptyReturnsBracketBracket(t *testing.T) {
	if got := localesJSON(nil); got != "[]" {
		t.Errorf("nil locales: got %q, want []", got)
	}
	if got := localesJSON([]ListingLocale{}); got != "[]" {
		t.Errorf("empty locales: got %q, want []", got)
	}
}
