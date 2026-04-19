package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
	"github.com/uptrace/bun"
)

// SeedBuildAnnotations inserts the initial seven known failure patterns.
// Idempotent — uses the unique Name column to avoid duplicate inserts on
// re-runs. Existing rows are left untouched so operator-tuned entries
// survive a redeploy.
func SeedBuildAnnotations(ctx context.Context, db *bun.DB) error {
	seeds := []models.BuildAnnotation{
		{
			Name:          "pod_install_failed",
			LogRegex:      `(?i)(Error running pod install|CocoaPods could not find compatible versions)`,
			HumanMessage:  "CocoaPods couldn't install — usually a Flutter plugin version conflict.",
			SuggestedFix:  "Retry with repo update: sets POD_REPO_UPDATE=1 so CocoaPods refreshes its spec repo before installing.",
			RetryStrategy: "POD_REPO_UPDATE=1",
			Enabled:       true,
		},
		{
			Name:          "bundle_id_mismatch",
			LogRegex:      `(?i)(No profiles for .* were found|The provisioning profile is not compatible|bundle identifier .* does not match)`,
			HumanMessage:  "The bundle ID on this build doesn't match what's registered in App Store Connect.",
			SuggestedFix:  "Check the bundle ID in Identity. It must exactly match the identifier you registered at https://developer.apple.com/account/resources/identifiers/list.",
			RetryStrategy: "",
			Enabled:       true,
		},
		{
			Name:          "keystore_corrupt",
			LogRegex:      `(?i)(keystore was tampered with|Invalid keystore format|Wrong keystore password)`,
			HumanMessage:  "Your Android keystore file couldn't be read.",
			SuggestedFix:  "Retry with key regeneration: we'll generate a new upload keystore. Warning: if this app is already on Play Store, you must enroll in Play App Signing so Google keeps the app-signing key.",
			RetryStrategy: "REGEN_KEYSTORE=1",
			Enabled:       true,
		},
		{
			Name:          "flutter_version_conflict",
			LogRegex:      `(?i)(requires Dart SDK version|Flutter SDK version .* is incompatible|Because .* requires SDK version)`,
			HumanMessage:  "A Flutter plugin needs a different Flutter or Dart SDK version than the build environment provides.",
			SuggestedFix:  "Retry pinning Flutter to 3.41.6 (our tested version) via FLUTTER_VERSION env var.",
			RetryStrategy: "FLUTTER_VERSION=3.41.6",
			Enabled:       true,
		},
		{
			Name:          "xcode_missing_entitlement",
			LogRegex:      `(?i)(Provisioning profile .* doesn't include the .* entitlement|Missing entitlement|Capability .* is not enabled)`,
			HumanMessage:  "The app uses a capability (push, associated domains, HealthKit) that isn't turned on in your App Store Connect App ID.",
			SuggestedFix:  "Open https://developer.apple.com/account/resources/identifiers/list, find your bundle ID, and enable the capability flagged in the log. Then retry.",
			RetryStrategy: "",
			Enabled:       true,
		},
		{
			Name:          "app_store_precheck_failed",
			LogRegex:      `(?i)(App Store metadata precheck failed|precheck failed)`,
			HumanMessage:  "Apple's metadata precheck rejected the listing before upload.",
			SuggestedFix:  "Common causes: URL in description, the word 'beta' or 'test' in the title, placeholder copy, or a privacy/support URL that returns a non-200. Fix the listing in the Launchpad and re-submit.",
			RetryStrategy: "",
			Enabled:       true,
		},
		{
			Name:          "produce_first_submission_failed",
			LogRegex:      `(?i)(fastlane produce|App Store Connect app record creation failed|bundle identifier .* is already taken|supply .* track: 'internal')`,
			HumanMessage:  "Couldn't create the store record for this app.",
			SuggestedFix:  "Check two things: (1) the bundle ID isn't already used by another developer, (2) your Apple Team ID / Play Console service account has permission to create apps. iOS: produce runs on first submission. Android: first submissions publish to the Internal track — promote to Production in Play Console when ready.",
			RetryStrategy: "",
			Enabled:       true,
		},
	}

	for _, seed := range seeds {
		_, err := db.NewInsert().
			Model(&seed).
			On("CONFLICT (name) DO NOTHING").
			Exec(ctx)
		if err != nil {
			return err
		}
	}
	return nil
}
