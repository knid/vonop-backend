package controllers

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/revenuecat"
)

// provisionRevenueCat ensures a RevenueCat project + iOS + Android apps exist
// for this wemob project, fetches the public SDK keys, and persists IDs +
// keys to MobileAppConfig.
//
// Idempotency contract: skip + return nil when any of:
//   - c.RevenueCatService == nil (REVENUECAT_API_KEY not configured)
//   - cfg == nil (no MobileAppConfig yet — non-mobile_app_native project)
//   - cfg.IAPEnabled == false
//   - cfg.RevenueCatProjectID != "" (already provisioned)
//
// On partial provisioning failure (e.g. iOS app created, Android app create
// fails), the function persists whatever was created before returning the
// error — the wemob project ends up half-provisioned but a subsequent
// retry can resume from where it stopped (RC project+iOS app stay; the
// next retry checks RevenueCatProjectID != "" and currently SKIPS, leaving
// the project half-provisioned).
//
// TODO(P2.3.B): smarter resume — if RevenueCatProjectID is set but
// RevenueCatPublicKeyAndroid is empty, retry just the missing pieces.
// For now the manual remedy is: clear RevenueCatProjectID in DB, retry.
func (c *Controller) provisionRevenueCat(ctx context.Context, project *models.AppProject) error {
	if c == nil || c.RevenueCatService == nil {
		return nil
	}
	if project == nil {
		return fmt.Errorf("provisionRevenueCat: nil project")
	}
	cfg, err := c.DB.GetMobileAppConfig(project.ID.String())
	if err != nil || cfg == nil {
		// No config (non-mobile_app_native or pre-bootstrap) → no-op.
		return nil
	}
	if !cfg.IAPEnabled || cfg.RevenueCatProjectID != "" {
		return nil
	}

	bundleID := bundleIDForProject(project, "")

	// 1. Create RC project named after the wemob slug.
	rcProject, err := c.RevenueCatService.CreateProject(ctx, project.Slug)
	if err != nil {
		return fmt.Errorf("provisionRevenueCat: create RC project: %w", err)
	}
	cfg.RevenueCatProjectID = rcProject.ID
	if _, perr := c.DB.UpsertMobileAppConfig(cfg); perr != nil {
		// Non-fatal — keep going; we'll persist again at the end.
		slog.Warn("provisionRevenueCat: persist project ID failed (continuing)",
			"error", perr, "project_id", project.ID, "rc_project_id", rcProject.ID)
	}

	// 2. Create iOS app.
	iosApp, ierr := c.RevenueCatService.CreateApp(ctx, rcProject.ID, revenuecat.CreateAppParams{
		Name:     project.Slug + " iOS",
		Type:     revenuecat.AppTypeAppStore,
		AppStore: &revenuecat.AppStoreConfig{BundleID: bundleID},
	})
	if ierr != nil {
		return fmt.Errorf("provisionRevenueCat: create iOS app: %w", ierr)
	}
	cfg.RevenueCatIOSAppID = iosApp.ID

	// 3. Create Android app. Persist iOS ID first so a failure here doesn't
	// lose the iOS app reference.
	if _, perr := c.DB.UpsertMobileAppConfig(cfg); perr != nil {
		slog.Warn("provisionRevenueCat: persist iOS app ID failed (continuing)",
			"error", perr, "project_id", project.ID)
	}
	andApp, aerr := c.RevenueCatService.CreateApp(ctx, rcProject.ID, revenuecat.CreateAppParams{
		Name:      project.Slug + " Android",
		Type:      revenuecat.AppTypePlayStore,
		PlayStore: &revenuecat.PlayStoreConfig{PackageName: bundleID},
	})
	if aerr != nil {
		return fmt.Errorf("provisionRevenueCat: create Android app: %w", aerr)
	}
	cfg.RevenueCatAndroidAppID = andApp.ID

	// 4. Fetch public SDK keys for both apps. ListPublicAPIKeys errors
	// are logged but not fatal — the build can still proceed with the
	// missing key, lib/iap.ts will warn at runtime, and a future retry
	// (manually clear RevenueCatProjectID + re-run) can pick the keys up.
	if iosKeys, kerr := c.RevenueCatService.ListPublicAPIKeys(ctx, rcProject.ID, iosApp.ID); kerr == nil {
		cfg.RevenueCatPublicKeyIOS = strings.TrimSpace(revenuecat.ProductionKeyForApp(iosKeys))
	} else {
		slog.Warn("provisionRevenueCat: list iOS public keys failed",
			"error", kerr, "rc_project_id", rcProject.ID, "ios_app_id", iosApp.ID)
	}
	if andKeys, kerr := c.RevenueCatService.ListPublicAPIKeys(ctx, rcProject.ID, andApp.ID); kerr == nil {
		cfg.RevenueCatPublicKeyAndroid = strings.TrimSpace(revenuecat.ProductionKeyForApp(andKeys))
	} else {
		slog.Warn("provisionRevenueCat: list Android public keys failed",
			"error", kerr, "rc_project_id", rcProject.ID, "android_app_id", andApp.ID)
	}

	if _, perr := c.DB.UpsertMobileAppConfig(cfg); perr != nil {
		return fmt.Errorf("provisionRevenueCat: persist final config: %w", perr)
	}

	slog.Info("provisionRevenueCat: complete",
		"project_id", project.ID,
		"rc_project_id", rcProject.ID,
		"ios_app_id", iosApp.ID,
		"android_app_id", andApp.ID,
		"ios_key_present", cfg.RevenueCatPublicKeyIOS != "",
		"android_key_present", cfg.RevenueCatPublicKeyAndroid != "",
	)
	return nil
}
