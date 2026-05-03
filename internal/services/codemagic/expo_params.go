package codemagic

import (
	"strconv"

	"github.com/google/uuid"
)

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
		"EXPO_PUBLIC_REVENUECAT_KEY_IOS":     p.ExpoPublicRevenueCatKeyIOS,
		"EXPO_PUBLIC_REVENUECAT_KEY_ANDROID": p.ExpoPublicRevenueCatKeyAndroid,
		"EXPO_PUBLIC_ONESIGNAL_APP_ID":       p.ExpoPublicOneSignalAppID,
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
	// ExpoPublicRevenueCatKeyIOS / Android — see ExpoAndroidBuildParams above.
	ExpoPublicRevenueCatKeyIOS     string
	ExpoPublicRevenueCatKeyAndroid string
	// ExpoPublicOneSignalAppID — see ExpoAndroidBuildParams above.
	ExpoPublicOneSignalAppID string
}

func (p ExpoIOSBuildParams) toEnvVars() map[string]string {
	scheme := p.XcodeScheme
	if scheme == "" {
		scheme = "WemobApp"
	}
	return map[string]string{
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
		"EXPO_PUBLIC_REVENUECAT_KEY_IOS":     p.ExpoPublicRevenueCatKeyIOS,
		"EXPO_PUBLIC_REVENUECAT_KEY_ANDROID": p.ExpoPublicRevenueCatKeyAndroid,
		"EXPO_PUBLIC_ONESIGNAL_APP_ID":       p.ExpoPublicOneSignalAppID,
	}
}
