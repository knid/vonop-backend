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
		"CM_FIRST_BUILD":         boolStr(p.FirstBuild),
		"CM_ICON_KEY":            p.IconKey,
		"CM_EAS_PROJECT_UUID":    p.EASProjectUUID,
		"EXPO_PUBLIC_API_URL":    p.ExpoPublicAPIURL,
		"EXPO_PUBLIC_PROJECT_ID": p.ExpoPublicProjectID,
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
		"CM_ICON_KEY":            p.IconKey,
		"CM_EAS_PROJECT_UUID":    p.EASProjectUUID,
		"XCODE_SCHEME":           scheme,
		"EXPO_PUBLIC_API_URL":    p.ExpoPublicAPIURL,
		"EXPO_PUBLIC_PROJECT_ID": p.ExpoPublicProjectID,
	}
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
	}
}
