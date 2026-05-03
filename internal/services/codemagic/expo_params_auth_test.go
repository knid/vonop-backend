package codemagic

import "testing"

// TestExpoAndroidBuildParams_AuthBaseURLInEnvVars verifies that the auth base
// URL is mapped to EXPO_PUBLIC_AUTH_BASE_URL so the mobile auth client picks
// it up at expo prebuild time.
func TestExpoAndroidBuildParams_AuthBaseURLInEnvVars(t *testing.T) {
	p := ExpoAndroidBuildParams{ExpoPublicAuthBaseURL: "https://x.wemob.app"}
	env := p.toEnvVars()
	if got := env["EXPO_PUBLIC_AUTH_BASE_URL"]; got != "https://x.wemob.app" {
		t.Errorf("EXPO_PUBLIC_AUTH_BASE_URL: got %q, want %q", got, "https://x.wemob.app")
	}
}

func TestExpoIOSBuildParams_AuthBaseURLInEnvVars(t *testing.T) {
	p := ExpoIOSBuildParams{ExpoPublicAuthBaseURL: "https://y.wemob.app"}
	env := p.toEnvVars()
	if got := env["EXPO_PUBLIC_AUTH_BASE_URL"]; got != "https://y.wemob.app" {
		t.Errorf("EXPO_PUBLIC_AUTH_BASE_URL: got %q, want %q", got, "https://y.wemob.app")
	}
}

// TestExpoAndroidBuildParams_AuthBaseURLEmptyWhenUnset verifies the empty
// string passes through (so the codemagic.yaml expansion produces an empty
// EXPO_PUBLIC_AUTH_BASE_URL — the mobile auth client falls back to
// EXPO_PUBLIC_API_URL in that case).
func TestExpoAndroidBuildParams_AuthBaseURLEmptyWhenUnset(t *testing.T) {
	p := ExpoAndroidBuildParams{}
	env := p.toEnvVars()
	if got, ok := env["EXPO_PUBLIC_AUTH_BASE_URL"]; !ok || got != "" {
		t.Errorf("EXPO_PUBLIC_AUTH_BASE_URL: got (%q,%v), want (empty,true)", got, ok)
	}
}
