package codemagic

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// ListingLocale is one row of per-locale store metadata serialized into
// CM_LOCALES_JSON. Apple / Google Play accept different fields, so the
// struct is the union; the codemagic.yaml step writes only the fields
// relevant to the platform it's running for.
//
// Apple uses: Title, Subtitle, Description, Keywords, PromotionalText,
// ReleaseNotes, MarketingURL, SupportURL.
//
// Play uses: Title, ShortDescription, Description (full description),
// ReleaseNotes (whatsnew/<lang>.txt).
//
// Screenshots maps device_type → list of S3 keys; the workflow downloads
// each from S3 into the fastlane metadata folder per locale.
type ListingLocale struct {
	Locale           string              `json:"locale"`
	Title            string              `json:"title,omitempty"`
	Subtitle         string              `json:"subtitle,omitempty"`
	Description      string              `json:"description,omitempty"`
	Keywords         string              `json:"keywords,omitempty"`
	PromotionalText  string              `json:"promotional_text,omitempty"`
	ReleaseNotes     string              `json:"release_notes,omitempty"`
	ShortDescription string              `json:"short_description,omitempty"`
	MarketingURL     string              `json:"marketing_url,omitempty"`
	SupportURL       string              `json:"support_url,omitempty"`
	Screenshots      map[string][]string `json:"screenshots,omitempty"`
}

// localesJSON returns the JSON encoding of locales for CM_LOCALES_JSON.
// Returns "[]" (not empty string) for nil/empty input so the workflow's
// jq parse step doesn't fail on an unset variable.
func localesJSON(locales []ListingLocale) string {
	if len(locales) == 0 {
		return "[]"
	}
	b, err := json.Marshal(locales)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// ExpoAndroidBuildParams are the parameters for the expo-android-build workflow.
type ExpoAndroidBuildParams struct {
	ProjectID        uuid.UUID
	BuildID          uuid.UUID
	TaskID           uuid.UUID
	AppTitle         string
	AppSlug          string
	BundleID         string
	PackageName      string
	BuildNumber      int
	MarketingVersion string
	SplashColor      string
	FirstBuild       bool
	IconKey          string
	EASProjectUUID   string
	// Build-time public env vars baked into the JS bundle by webpack.
	// Must be set BEFORE expo prebuild — runtime env is too late (NEXT_PUBLIC-style).
	// Empty string is passed through; the AI-generated lib/api.ts can fall back.
	ExpoPublicAPIURL    string
	ExpoPublicProjectID string
	// ExpoPublicAuthBaseURL is the Better-Auth base URL (matches the backend's
	// deployed URL). Empty when MobileAppConfig.AuthEnabled is false; the
	// mobile auth client falls back to EXPO_PUBLIC_API_URL.
	ExpoPublicAuthBaseURL string
	// ExpoPublicRevenueCatKeyIOS / Android — RevenueCat public SDK keys
	// baked into the binary at expo prebuild time. Empty when
	// MobileAppConfig.IAPEnabled is false OR provisioning hasn't happened
	// yet; lib/iap.ts no-ops + warns at runtime in that case.
	ExpoPublicRevenueCatKeyIOS     string
	ExpoPublicRevenueCatKeyAndroid string
	// ExpoPublicOneSignalAppID — OneSignal app ID baked into the binary at
	// expo prebuild time. Empty when MobileAppConfig.PushEnabled is false OR
	// provisioning hasn't happened yet (placeholder still present); the
	// template's lib/push.ts initPush() no-ops + warns at runtime in that case.
	ExpoPublicOneSignalAppID string
	// P2.1: per-app self-hosted expo-updates manifest URL (without trailing
	// /manifest). The codemagic.yaml step substitutes this into
	// app.json's expo.updates.url at prebuild time. Empty string means OTA
	// is disabled for this project — codemagic publishing step is gated on
	// it being non-empty.
	ExpoPublicOTAURL string
	// ExpoPublicWEMOBAPIURL is the Wemob platform API base URL. Used by
	// lib/forceUpdate.ts for min-version checks and other platform endpoints.
	ExpoPublicWEMOBAPIURL string
}

func (p ExpoAndroidBuildParams) toEnvVars() map[string]string {
	return map[string]string{
		"CM_PROJECT_ID":        p.ProjectID.String(),
		"CM_BUILD_ID":          p.BuildID.String(),
		"CM_TASK_ID":           p.TaskID.String(),
		"CM_APP_TITLE":         p.AppTitle,
		"CM_APP_SLUG":          p.AppSlug,
		"CM_BUNDLE_ID":         p.BundleID,
		"CM_PACKAGE_NAME":      p.PackageName,
		"CM_BUILD_NUMBER":      strconv.Itoa(p.BuildNumber),
		"CM_MARKETING_VERSION": p.MarketingVersion,
		"CM_SPLASH_COLOR":      p.SplashColor,
		"CM_FIRST_BUILD":                     boolStr(p.FirstBuild),
		"CM_ICON_KEY":                        p.IconKey,
		"CM_EAS_PROJECT_UUID":                p.EASProjectUUID,
		"EXPO_PUBLIC_API_URL":                p.ExpoPublicAPIURL,
		"EXPO_PUBLIC_PROJECT_ID":             p.ExpoPublicProjectID,
		"EXPO_PUBLIC_AUTH_BASE_URL":          p.ExpoPublicAuthBaseURL,
		"EXPO_PUBLIC_REVENUECAT_KEY_IOS":     p.ExpoPublicRevenueCatKeyIOS,
		"EXPO_PUBLIC_REVENUECAT_KEY_ANDROID": p.ExpoPublicRevenueCatKeyAndroid,
		"EXPO_PUBLIC_ONESIGNAL_APP_ID":       p.ExpoPublicOneSignalAppID,
		"EXPO_PUBLIC_OTA_URL":                p.ExpoPublicOTAURL,
		"EXPO_PUBLIC_WEMOB_API_URL":          p.ExpoPublicWEMOBAPIURL,
	}
}

// ExpoIOSBuildParams are the parameters for the expo-ios-build workflow.
type ExpoIOSBuildParams struct {
	ProjectID        uuid.UUID
	BuildID          uuid.UUID
	TaskID           uuid.UUID
	AppTitle         string
	AppSlug          string
	BundleID         string
	BuildNumber      int
	MarketingVersion string
	SplashColor      string
	IconKey          string
	EASProjectUUID   string
	XcodeScheme      string // defaults to "WemobApp"
	// Build-time public env vars (see ExpoAndroidBuildParams above).
	ExpoPublicAPIURL    string
	ExpoPublicProjectID string
	// ExpoPublicAuthBaseURL is the Better-Auth base URL. See above.
	ExpoPublicAuthBaseURL string
	// ExpoPublicRevenueCatKeyIOS / Android — see ExpoAndroidBuildParams above.
	ExpoPublicRevenueCatKeyIOS     string
	ExpoPublicRevenueCatKeyAndroid string
	// ExpoPublicOneSignalAppID — see ExpoAndroidBuildParams above.
	ExpoPublicOneSignalAppID string
	// P2.1: per-app self-hosted expo-updates manifest URL.
	ExpoPublicOTAURL string
	// ExpoPublicWEMOBAPIURL is the Wemob platform API base URL. See above.
	ExpoPublicWEMOBAPIURL string
	// Permissions is a map of permission key → rendered NS*UsageDescription
	// string emitted to Codemagic as CM_PERM_<KEY> env vars. Keys (lowercase,
	// snake_case) cover the common Expo-module-driven sensitive APIs:
	//   camera, photo_library, microphone, location_when_in_use,
	//   location_always, contacts, calendar, reminders, face_id, motion,
	//   bluetooth, speech_recognition, media_library, tracking
	// Empty / nil map means no per-permission env vars are emitted (the
	// codemagic.yaml step then leaves the bundled app.json values intact).
	// Keys with empty-string values are skipped during env emission so the
	// CI step can use [ -z "$VAL" ] && continue.
	Permissions map[string]string
}

func (p ExpoIOSBuildParams) toEnvVars() map[string]string {
	scheme := p.XcodeScheme
	if scheme == "" {
		scheme = "WemobApp"
	}
	env := map[string]string{
		"CM_PROJECT_ID":        p.ProjectID.String(),
		"CM_BUILD_ID":          p.BuildID.String(),
		"CM_TASK_ID":           p.TaskID.String(),
		"CM_APP_TITLE":         p.AppTitle,
		"CM_APP_SLUG":          p.AppSlug,
		"CM_BUNDLE_ID":         p.BundleID,
		"CM_PACKAGE_NAME":      p.BundleID,
		"CM_BUILD_NUMBER":      strconv.Itoa(p.BuildNumber),
		"CM_MARKETING_VERSION": p.MarketingVersion,
		"CM_SPLASH_COLOR":      p.SplashColor,
		"CM_ICON_KEY":                        p.IconKey,
		"CM_EAS_PROJECT_UUID":                p.EASProjectUUID,
		"XCODE_SCHEME":                       scheme,
		"EXPO_PUBLIC_API_URL":                p.ExpoPublicAPIURL,
		"EXPO_PUBLIC_PROJECT_ID":             p.ExpoPublicProjectID,
		"EXPO_PUBLIC_AUTH_BASE_URL":          p.ExpoPublicAuthBaseURL,
		"EXPO_PUBLIC_REVENUECAT_KEY_IOS":     p.ExpoPublicRevenueCatKeyIOS,
		"EXPO_PUBLIC_REVENUECAT_KEY_ANDROID": p.ExpoPublicRevenueCatKeyAndroid,
		"EXPO_PUBLIC_ONESIGNAL_APP_ID":       p.ExpoPublicOneSignalAppID,
		"EXPO_PUBLIC_OTA_URL":                p.ExpoPublicOTAURL,
		"EXPO_PUBLIC_WEMOB_API_URL":          p.ExpoPublicWEMOBAPIURL,
	}
	// Emit per-permission usage strings as CM_PERM_<KEY>. Empty values are
	// skipped so the codemagic step's "[ -z "$VAL" ] && continue" guard
	// doesn't accidentally null out an existing Info.plist entry.
	for key, val := range p.Permissions {
		if val == "" {
			continue
		}
		env["CM_PERM_"+strings.ToUpper(key)] = val
	}
	return env
}

// ExpoAndroidDeployParams are the parameters for the expo-android-deploy
// workflow (publishing.google_play submission). The service-account JSON is
// base64-encoded into the env var because the trigger API only accepts
// scalar variables — the workflow decodes it back into a temp file before
// the publishing block reads it.
type ExpoAndroidDeployParams struct {
	ProjectID   uuid.UUID
	BuildID     uuid.UUID
	TaskID      uuid.UUID
	PackageName string
	BuildNumber int
	// Base64-encoded Google Play service-account JSON.
	ServiceAccountJSONB64 string
	// Track: "internal" | "alpha" | "beta" | "production".
	Track string
	// RolloutFraction is the staged-rollout percentage as a fraction of 1.0.
	// 0 (default) means no staged rollout (full release). Only meaningful
	// when Track == "production".
	RolloutFraction float64
	// SubmitAsDraft uploads but does not release. Workflow consults this to
	// pass --draft to the publishing block.
	SubmitAsDraft bool
	// AABS3Key points the workflow at the AAB to download from S3.
	AABS3Key string

	// P1#15 — per-locale store metadata. Serialized to CM_LOCALES_JSON env
	// var; the workflow's "Write per-locale store metadata" step parses it
	// and writes fastlane/metadata/android/<locale>/ files. Empty slice
	// means no per-locale metadata; the publishing block falls back to
	// whatever is already in the Play Console.
	Locales []ListingLocale
	// P1#15 — locale-independent metadata applicable across all locales.
	Copyright         string
	PrimaryCategory   string
	SecondaryCategory string
	EULAType          string // "apple_default" | "custom_url" — Play ignores; iOS uses
	EULAURL           string
	// ReleaseNotes is the per-platform release notes sent to the store.
	// When empty, the publishing block falls back to whatever is already
	// in Play Console.
	ReleaseNotes string
}

func (p ExpoAndroidDeployParams) toEnvVars() map[string]string {
	track := p.Track
	if track == "" {
		track = "internal"
	}
	rollout := ""
	if p.RolloutFraction > 0 && p.RolloutFraction <= 1.0 {
		rollout = strconv.FormatFloat(p.RolloutFraction, 'f', 4, 64)
	}
	return map[string]string{
		"CM_PROJECT_ID":              p.ProjectID.String(),
		"CM_BUILD_ID":                p.BuildID.String(),
		"CM_TASK_ID":                 p.TaskID.String(),
		"CM_PACKAGE_NAME":            p.PackageName,
		"CM_BUILD_NUMBER":            strconv.Itoa(p.BuildNumber),
		"CM_GOOGLE_PLAY_SA_JSON_B64": p.ServiceAccountJSONB64,
		"CM_PLAY_TRACK":              track,
		"CM_PLAY_ROLLOUT_FRACTION":   rollout,
		"CM_SUBMIT_AS_DRAFT":         boolStr(p.SubmitAsDraft),
		"CM_AAB_S3_KEY":              p.AABS3Key,
		// P1#15 — per-locale store metadata + parent-listing locale-independent fields.
		"CM_LOCALES_JSON":      localesJSON(p.Locales),
		"CM_COPYRIGHT":         p.Copyright,
		"CM_PRIMARY_CATEGORY":  p.PrimaryCategory,
		"CM_SECONDARY_CATEGORY": p.SecondaryCategory,
		"CM_EULA_TYPE":         p.EULAType,
		"CM_EULA_URL":          p.EULAURL,
		"CM_RELEASE_NOTES":     p.ReleaseNotes,
	}
}

// ExpoIOSDeployParams are the parameters for the expo-ios-deploy workflow
// (publishing.app_store_connect submission). The .p8 key contents are
// base64-encoded into the env var; the workflow decodes back to a file.
type ExpoIOSDeployParams struct {
	ProjectID   uuid.UUID
	BuildID     uuid.UUID
	TaskID      uuid.UUID
	BundleID    string
	BuildNumber int
	// Base64-encoded App Store Connect API key (.p8 file).
	ASCAPIKeyB64 string
	// ASCKeyID is the App Store Connect API key identifier.
	ASCKeyID string
	// ASCIssuerID is the App Store Connect issuer UUID.
	ASCIssuerID string
	// SubmitToTestFlight: when true, sets publishing.app_store_connect.submit_to_testflight=true.
	SubmitToTestFlight bool
	// SubmitToAppStore: when true, push to App Store Connect for review (production submission).
	// Independent of SubmitToTestFlight; both can be true (TestFlight first, App Store later).
	SubmitToAppStore bool
	// PhasedRelease: when true and SubmitToAppStore is true, enable Apple's phased rollout.
	PhasedRelease bool
	// SubmitAsDraft: when true, upload but don't release.
	SubmitAsDraft bool
	// IPAS3Key points the workflow at the IPA to download from S3.
	IPAS3Key string

	// P1#15 — per-locale App Store Connect metadata. Serialized to
	// CM_LOCALES_JSON env var; the workflow's "Write per-locale store
	// metadata" step parses it and writes fastlane/metadata/<locale>/
	// files. Empty slice means no per-locale metadata; the publishing
	// block falls back to whatever is already in App Store Connect.
	Locales []ListingLocale
	// P1#15 — locale-independent App Store Connect metadata.
	Copyright         string
	PrimaryCategory   string
	SecondaryCategory string
	EULAType          string // "apple_default" | "custom_url"
	EULAURL           string
	// ReleaseNotes is the per-platform release notes sent to the store.
	// When empty, the publishing block falls back to whatever is already
	// in App Store Connect.
	ReleaseNotes string
}

func (p ExpoIOSDeployParams) toEnvVars() map[string]string {
	return map[string]string{
		"CM_PROJECT_ID":           p.ProjectID.String(),
		"CM_BUILD_ID":             p.BuildID.String(),
		"CM_TASK_ID":              p.TaskID.String(),
		"CM_BUNDLE_ID":            p.BundleID,
		"CM_BUILD_NUMBER":         strconv.Itoa(p.BuildNumber),
		"CM_ASC_API_KEY_B64":      p.ASCAPIKeyB64,
		"CM_ASC_KEY_ID":           p.ASCKeyID,
		"CM_ASC_ISSUER_ID":        p.ASCIssuerID,
		"CM_SUBMIT_TO_TESTFLIGHT": boolStr(p.SubmitToTestFlight),
		"CM_SUBMIT_TO_APP_STORE":  boolStr(p.SubmitToAppStore),
		"CM_PHASED_RELEASE":       boolStr(p.PhasedRelease),
		"CM_SUBMIT_AS_DRAFT":      boolStr(p.SubmitAsDraft),
		"CM_IPA_S3_KEY":           p.IPAS3Key,
		// P1#15 — per-locale store metadata + locale-independent fields.
		"CM_LOCALES_JSON":      localesJSON(p.Locales),
		"CM_COPYRIGHT":         p.Copyright,
		"CM_PRIMARY_CATEGORY":  p.PrimaryCategory,
		"CM_SECONDARY_CATEGORY": p.SecondaryCategory,
		"CM_EULA_TYPE":         p.EULAType,
		"CM_EULA_URL":          p.EULAURL,
		"CM_RELEASE_NOTES":     p.ReleaseNotes,
	}
}
