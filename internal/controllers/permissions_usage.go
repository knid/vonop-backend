package controllers

import (
	"fmt"
	"strings"
)

// iosPermissionTemplate captures the static per-permission render data: the
// resource phrase that fits "<AppTitle> uses your <Resource> to ..." and the
// purpose phrase that follows. The full string is rendered as:
//
//	"<AppTitle> uses your <Resource> to <Purpose>."
//
// Static phrasing is deliberately specific — Apple has rejected generic copy
// like "This app needs camera access" since 2021. Keep these short, concrete,
// and grounded in a plausible in-app feature. When a permission has no
// natural "<Resource>" (e.g. tracking, face_id), the renderer uses an
// alternate sentence skeleton.
type iosPermissionTemplate struct {
	// Resource is the noun for the standard template (e.g. "camera",
	// "photo library"). Empty means use AlternateSkeleton.
	Resource string
	// Purpose finishes the standard sentence: "<App> uses your <Resource> to <Purpose>."
	Purpose string
	// AlternateSkeleton is a full sentence template containing exactly one
	// "%s" — the AppTitle. Used when the standard "uses your X to Y" form
	// reads awkwardly (Face ID, tracking).
	AlternateSkeleton string
}

// iosPermissionTemplates is the canonical lowercase-key → render-template map.
// Keys are the values stored in MobileAppConfig.Permissions[]; values produce
// the strings written to Info.plist's NS*UsageDescription keys.
//
// Coverage tracks the common Expo modules: expo-camera, expo-image-picker,
// expo-av, expo-location, expo-contacts, expo-calendar,
// expo-local-authentication, expo-sensors, expo-tracking-transparency, etc.
// New permissions added to MobileAppConfig.Permissions[] need an entry here
// or they're silently dropped by renderPermissionUsageStrings.
var iosPermissionTemplates = map[string]iosPermissionTemplate{
	"camera": {
		Resource: "camera",
		Purpose:  "take photos and videos within the app",
	},
	"photo_library": {
		Resource: "photo library",
		Purpose:  "let you pick photos and videos to use in the app",
	},
	"microphone": {
		Resource: "microphone",
		Purpose:  "record audio for in-app features",
	},
	"location_when_in_use": {
		Resource: "location",
		Purpose:  "show you nearby content and save your location to your activity",
	},
	"location_always": {
		Resource: "location",
		Purpose:  "provide background location features when the app is closed",
	},
	"contacts": {
		Resource: "contacts",
		Purpose:  "let you find friends and share content with people in your address book",
	},
	"calendar": {
		Resource: "calendar",
		Purpose:  "create and read events relevant to your in-app activity",
	},
	"reminders": {
		Resource: "reminders",
		Purpose:  "create reminders related to your in-app activity",
	},
	"face_id": {
		AlternateSkeleton: "%s uses Face ID or Touch ID to let you sign in securely.",
	},
	"motion": {
		Resource: "motion data",
		Purpose:  "track activity that you've enabled in the app",
	},
	"bluetooth": {
		Resource: "Bluetooth",
		Purpose:  "connect to and communicate with nearby devices",
	},
	"speech_recognition": {
		Resource: "speech recognition",
		Purpose:  "convert your voice to text for in-app features",
	},
	"media_library": {
		Resource: "media library",
		Purpose:  "play and manage media you choose",
	},
	"tracking": {
		AlternateSkeleton: "%s uses tracking to personalize content and ads — you can opt out at any time.",
	},
}

// renderPermissionUsageStrings turns a list of declared permission keys into
// a map of key → rendered NS*UsageDescription string suitable for CM_PERM_*
// env-var emission. Permissions not present in iosPermissionTemplates are
// silently skipped — Android-only permissions ("notifications", arbitrary
// strings) won't blow up the build.
//
// When permissions is nil/empty or appTitle is empty (a defensive guard
// against half-populated MobileAppConfigs), the returned map is empty and
// the caller emits no CM_PERM_* env vars.
//
// appDescription is currently unused — the templated copy is fully static
// per-permission. It's accepted as a parameter so the deferred custom-copy
// schema can interpolate description-derived purposes without changing the
// callsite signature.
func renderPermissionUsageStrings(appTitle string, appDescription string, permissions []string) map[string]string {
	_ = appDescription // reserved for the deferred custom-copy schema
	out := map[string]string{}
	title := strings.TrimSpace(appTitle)
	if title == "" || len(permissions) == 0 {
		return out
	}
	for _, raw := range permissions {
		key := strings.ToLower(strings.TrimSpace(raw))
		tpl, ok := iosPermissionTemplates[key]
		if !ok {
			// Unknown permission keys (Android-only, "notifications", typos)
			// — skip rather than emit a generic description that Apple will
			// reject anyway.
			continue
		}
		var rendered string
		if tpl.AlternateSkeleton != "" {
			rendered = fmt.Sprintf(tpl.AlternateSkeleton, title)
		} else {
			rendered = fmt.Sprintf("%s uses your %s to %s.", title, tpl.Resource, tpl.Purpose)
		}
		out[key] = rendered
	}
	return out
}
