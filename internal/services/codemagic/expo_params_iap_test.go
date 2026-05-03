package codemagic

import "testing"

func TestExpoAndroidBuildParams_RevenueCatKeyInEnvVars(t *testing.T) {
	p := ExpoAndroidBuildParams{
		ExpoPublicRevenueCatKeyIOS:     "appl_xxx",
		ExpoPublicRevenueCatKeyAndroid: "goog_yyy",
	}
	env := p.toEnvVars()
	if got := env["EXPO_PUBLIC_REVENUECAT_KEY_IOS"]; got != "appl_xxx" {
		t.Errorf("EXPO_PUBLIC_REVENUECAT_KEY_IOS: got %q, want appl_xxx", got)
	}
	if got := env["EXPO_PUBLIC_REVENUECAT_KEY_ANDROID"]; got != "goog_yyy" {
		t.Errorf("EXPO_PUBLIC_REVENUECAT_KEY_ANDROID: got %q, want goog_yyy", got)
	}
}

func TestExpoIOSBuildParams_RevenueCatKeyInEnvVars(t *testing.T) {
	p := ExpoIOSBuildParams{
		ExpoPublicRevenueCatKeyIOS:     "appl_xxx",
		ExpoPublicRevenueCatKeyAndroid: "goog_yyy",
	}
	env := p.toEnvVars()
	if got := env["EXPO_PUBLIC_REVENUECAT_KEY_IOS"]; got != "appl_xxx" {
		t.Errorf("EXPO_PUBLIC_REVENUECAT_KEY_IOS: got %q, want appl_xxx", got)
	}
	if got := env["EXPO_PUBLIC_REVENUECAT_KEY_ANDROID"]; got != "goog_yyy" {
		t.Errorf("EXPO_PUBLIC_REVENUECAT_KEY_ANDROID: got %q, want goog_yyy", got)
	}
}

// TestExpoAndroidBuildParams_RevenueCatEmpty verifies that empty keys pass
// through cleanly — graceful-degrade: when REVENUECAT_API_KEY is unset OR
// MobileAppConfig.IAPEnabled is false, the build still triggers and the
// template's lib/iap.ts initIAP() no-ops + warns at runtime.
func TestExpoAndroidBuildParams_RevenueCatEmpty(t *testing.T) {
	p := ExpoAndroidBuildParams{}
	env := p.toEnvVars()
	if got := env["EXPO_PUBLIC_REVENUECAT_KEY_IOS"]; got != "" {
		t.Errorf("EXPO_PUBLIC_REVENUECAT_KEY_IOS: got %q, want empty", got)
	}
	if got := env["EXPO_PUBLIC_REVENUECAT_KEY_ANDROID"]; got != "" {
		t.Errorf("EXPO_PUBLIC_REVENUECAT_KEY_ANDROID: got %q, want empty", got)
	}
}
