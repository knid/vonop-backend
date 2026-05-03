package codemagic

import "testing"

func TestExpoAndroidBuildParams_OneSignalAppIDInEnvVars(t *testing.T) {
	p := ExpoAndroidBuildParams{
		ExpoPublicOneSignalAppID: "os-app-abc",
	}
	env := p.toEnvVars()
	if env["EXPO_PUBLIC_ONESIGNAL_APP_ID"] != "os-app-abc" {
		t.Errorf("got %q, want os-app-abc", env["EXPO_PUBLIC_ONESIGNAL_APP_ID"])
	}
}

func TestExpoIOSBuildParams_OneSignalAppIDInEnvVars(t *testing.T) {
	p := ExpoIOSBuildParams{
		ExpoPublicOneSignalAppID: "os-app-xyz",
	}
	env := p.toEnvVars()
	if env["EXPO_PUBLIC_ONESIGNAL_APP_ID"] != "os-app-xyz" {
		t.Errorf("got %q, want os-app-xyz", env["EXPO_PUBLIC_ONESIGNAL_APP_ID"])
	}
}

func TestExpoBuildParams_EmptyOneSignalAppID_PassesThrough(t *testing.T) {
	envA := ExpoAndroidBuildParams{}.toEnvVars()
	envI := ExpoIOSBuildParams{}.toEnvVars()
	if envA["EXPO_PUBLIC_ONESIGNAL_APP_ID"] != "" {
		t.Errorf("android: empty appID should be empty string, got %q", envA["EXPO_PUBLIC_ONESIGNAL_APP_ID"])
	}
	if envI["EXPO_PUBLIC_ONESIGNAL_APP_ID"] != "" {
		t.Errorf("ios: empty appID should be empty string, got %q", envI["EXPO_PUBLIC_ONESIGNAL_APP_ID"])
	}
}
