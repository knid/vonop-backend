package codemagic

import (
	"strconv"

	"github.com/google/uuid"
)

// ExpoOTAPublishParams are the parameters for the expo-ota-publish workflow
// (P1 #12, mobile audit, 2026-05-04).
//
// Mirrors ExpoAndroidBuildParams field-for-field on the inputs the YAML step
// actually consumes (icon download, app.json substitution, OTA cert fetch,
// public env vars baked into the JS bundle). Differences from build params:
//   - no FirstBuild / SplashColor: the OTA workflow doesn't provision keystores
//     or render adaptive-icon backgrounds; only the JS bundle ships.
//   - no Android-only or iOS-only fields: this workflow always exports both
//     platforms in a single run, so per-platform inputs (XcodeScheme, iOS
//     permissions) are not relevant. The YAML step skips the prebuild that
//     would consume them.
type ExpoOTAPublishParams struct {
	ProjectID        uuid.UUID
	BuildID          uuid.UUID
	TaskID           uuid.UUID
	AppTitle         string
	AppSlug          string
	BundleID         string
	PackageName      string
	BuildNumber      int
	MarketingVersion string
	IconKey          string
	EASProjectUUID   string
	// OTAURL is the per-app self-hosted expo-updates manifest URL (without
	// trailing /manifest). Empty string means OTA is disabled — the workflow
	// then no-ops the cert fetch + publish steps and exits with success.
	OTAURL string
	// Build-time public env vars baked into the JS bundle by webpack. Same
	// semantics as ExpoAndroidBuildParams — empty string is passed through.
	ExpoPublicAPIURL               string
	ExpoPublicProjectID            string
	ExpoPublicAuthBaseURL          string
	ExpoPublicRevenueCatKeyIOS     string
	ExpoPublicRevenueCatKeyAndroid string
	ExpoPublicOneSignalAppID       string
}

func (p ExpoOTAPublishParams) toEnvVars() map[string]string {
	return map[string]string{
		"CM_PROJECT_ID":                      p.ProjectID.String(),
		"CM_BUILD_ID":                        p.BuildID.String(),
		"CM_TASK_ID":                         p.TaskID.String(),
		"CM_APP_TITLE":                       p.AppTitle,
		"CM_APP_SLUG":                        p.AppSlug,
		"CM_BUNDLE_ID":                       p.BundleID,
		"CM_PACKAGE_NAME":                    p.PackageName,
		"CM_BUILD_NUMBER":                    strconv.Itoa(p.BuildNumber),
		"CM_MARKETING_VERSION":               p.MarketingVersion,
		"CM_ICON_KEY":                        p.IconKey,
		"CM_EAS_PROJECT_UUID":                p.EASProjectUUID,
		"EXPO_PUBLIC_API_URL":                p.ExpoPublicAPIURL,
		"EXPO_PUBLIC_PROJECT_ID":             p.ExpoPublicProjectID,
		"EXPO_PUBLIC_AUTH_BASE_URL":          p.ExpoPublicAuthBaseURL,
		"EXPO_PUBLIC_REVENUECAT_KEY_IOS":     p.ExpoPublicRevenueCatKeyIOS,
		"EXPO_PUBLIC_REVENUECAT_KEY_ANDROID": p.ExpoPublicRevenueCatKeyAndroid,
		"EXPO_PUBLIC_ONESIGNAL_APP_ID":       p.ExpoPublicOneSignalAppID,
		"EXPO_PUBLIC_OTA_URL":                p.OTAURL,
	}
}
