package controllers

import (
	"strings"
	"testing"
)

func TestRenderPermissionUsageStrings_KnownPermissions(t *testing.T) {
	got := renderPermissionUsageStrings("Acme", "", []string{"camera", "photo_library", "microphone"})
	if got["camera"] == "" || !strings.Contains(got["camera"], "Acme") || !strings.Contains(got["camera"], "camera") {
		t.Errorf("camera = %q, want non-empty Acme+camera string", got["camera"])
	}
	if got["photo_library"] == "" || !strings.Contains(got["photo_library"], "photo library") {
		t.Errorf("photo_library = %q, want photo library copy", got["photo_library"])
	}
	if got["microphone"] == "" || !strings.Contains(got["microphone"], "microphone") {
		t.Errorf("microphone = %q, want microphone copy", got["microphone"])
	}
}

func TestRenderPermissionUsageStrings_AlternateSkeleton(t *testing.T) {
	got := renderPermissionUsageStrings("Bumble", "", []string{"face_id", "tracking"})
	if !strings.Contains(got["face_id"], "Bumble") || !strings.Contains(got["face_id"], "Face ID") {
		t.Errorf("face_id = %q, want Bumble + Face ID", got["face_id"])
	}
	if !strings.Contains(got["tracking"], "Bumble") || !strings.Contains(got["tracking"], "tracking") {
		t.Errorf("tracking = %q, want Bumble + tracking", got["tracking"])
	}
}

func TestRenderPermissionUsageStrings_UnknownSkipped(t *testing.T) {
	// Real-world: "notifications" is in MobileAppConfig.Permissions[] (auto-
	// appended by the views layer) but isn't an iOS NS* key. Don't emit it.
	got := renderPermissionUsageStrings("X", "", []string{"camera", "notifications", "totally-made-up"})
	if _, ok := got["notifications"]; ok {
		t.Error("notifications should be skipped (no NS key)")
	}
	if _, ok := got["totally-made-up"]; ok {
		t.Error("unknown perms should be skipped")
	}
	if got["camera"] == "" {
		t.Error("known perms should still render alongside skipped ones")
	}
}

func TestRenderPermissionUsageStrings_EmptyInputs(t *testing.T) {
	if got := renderPermissionUsageStrings("", "", []string{"camera"}); len(got) != 0 {
		t.Errorf("empty appTitle should return empty map, got %v", got)
	}
	if got := renderPermissionUsageStrings("Acme", "", nil); len(got) != 0 {
		t.Errorf("nil permissions should return empty map, got %v", got)
	}
	if got := renderPermissionUsageStrings("Acme", "", []string{}); len(got) != 0 {
		t.Errorf("empty permissions should return empty map, got %v", got)
	}
}

func TestRenderPermissionUsageStrings_CaseInsensitive(t *testing.T) {
	got := renderPermissionUsageStrings("Acme", "", []string{"CAMERA", " Photo_Library "})
	if got["camera"] == "" {
		t.Error("uppercase CAMERA should normalize to lowercase camera key")
	}
	if got["photo_library"] == "" {
		t.Error("trimmed/cased Photo_Library should normalize")
	}
}

func TestRenderPermissionUsageStrings_AllSpecialPurposes(t *testing.T) {
	// Sanity-check coverage matches the task's permission list. If a future
	// edit drops one, this test catches it.
	allKeys := []string{
		"camera", "photo_library", "microphone",
		"location_when_in_use", "location_always",
		"contacts", "calendar", "reminders",
		"face_id", "motion", "bluetooth",
		"speech_recognition", "media_library", "tracking",
	}
	got := renderPermissionUsageStrings("Acme", "", allKeys)
	for _, k := range allKeys {
		if got[k] == "" {
			t.Errorf("missing template for %q", k)
		}
		if !strings.Contains(got[k], "Acme") {
			t.Errorf("%q missing AppTitle: %q", k, got[k])
		}
	}
}
