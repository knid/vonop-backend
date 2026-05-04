package codemagic

import "testing"

func TestExpoIOSBuildParams_PermissionsEmitsCMPermEnv(t *testing.T) {
	p := ExpoIOSBuildParams{
		Permissions: map[string]string{
			"camera":        "Acme uses your camera to take photos and videos within the app.",
			"photo_library": "Acme uses your photo library to let you pick photos.",
		},
	}
	env := p.toEnvVars()
	if got := env["CM_PERM_CAMERA"]; got == "" || got != p.Permissions["camera"] {
		t.Errorf("CM_PERM_CAMERA = %q, want %q", got, p.Permissions["camera"])
	}
	if got := env["CM_PERM_PHOTO_LIBRARY"]; got == "" || got != p.Permissions["photo_library"] {
		t.Errorf("CM_PERM_PHOTO_LIBRARY = %q, want %q", got, p.Permissions["photo_library"])
	}
}

func TestExpoIOSBuildParams_PermissionsEmptyValueSkipped(t *testing.T) {
	p := ExpoIOSBuildParams{
		Permissions: map[string]string{
			"camera":     "non-empty",
			"microphone": "", // should be skipped
		},
	}
	env := p.toEnvVars()
	if _, ok := env["CM_PERM_MICROPHONE"]; ok {
		t.Errorf("CM_PERM_MICROPHONE should be skipped on empty value, got %q", env["CM_PERM_MICROPHONE"])
	}
	if env["CM_PERM_CAMERA"] != "non-empty" {
		t.Errorf("CM_PERM_CAMERA = %q, want non-empty", env["CM_PERM_CAMERA"])
	}
}

func TestExpoIOSBuildParams_NilPermissionsNoEnv(t *testing.T) {
	env := ExpoIOSBuildParams{}.toEnvVars()
	for k := range env {
		if len(k) > 8 && k[:8] == "CM_PERM_" {
			t.Errorf("nil Permissions should emit no CM_PERM_*, got %s=%q", k, env[k])
		}
	}
}

func TestExpoAndroidBuildParams_NoPermissionField(t *testing.T) {
	// Android pass deliberately omits permissions — they go in
	// expo.android.permissions[] via app.json, not Info.plist. This test
	// is the canary that a future refactor doesn't accidentally couple
	// the iOS permission map onto the Android struct.
	env := ExpoAndroidBuildParams{}.toEnvVars()
	for k := range env {
		if len(k) > 8 && k[:8] == "CM_PERM_" {
			t.Errorf("Android struct should not emit CM_PERM_*, got %s", k)
		}
	}
}
