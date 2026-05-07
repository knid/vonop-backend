package controllers

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/utils/external"
)

// onesignalRESTKeyKind is the CredStore "kind" tag for an encrypted OneSignal
// REST API key blob. Tagged so an out-of-band CredStore audit can distinguish
// these from Apple/Google credentials.
const onesignalRESTKeyKind = "onesignal-rest-key"

// onesignalShadowPrefix is retained as a compatibility shim for the few
// remaining v1-shadow consumers (compose_push.go in particular). Task 3
// rewrites those paths to read from MobileAppConfig directly, at which point
// this constant can be removed. Until then it lets the build keep compiling
// while Task 2 lands.
const onesignalShadowPrefix = "expo-shadow-"

// provisionOneSignalApp ensures a real OneSignal application + REST API token
// exist for this mobile_app_native project. Persists the app id +
// envelope-encrypted REST key on MobileAppConfig (per-AppProject) — the v1
// shadow Project no longer participates.
//
// Skip cases (return nil, no-op):
//   - ONESIGNAL_ORG_ID or ONESIGNAL_ORG_API_KEY unset (master keys missing)
//   - MobileAppConfig already has a non-empty OneSignalAppID (idempotent guard)
//
// On any provisioning error, returns the error so the caller can decide
// whether to surface it.
func (c *Controller) provisionOneSignalApp(ctx context.Context, project *models.AppProject) error {
	if c == nil || project == nil {
		return nil
	}
	if project.ProjectType != models.ProjectTypeMobileAppNative {
		return nil
	}

	orgID := strings.TrimSpace(c.SecretManager.GetSecret("ONESIGNAL_ORG_ID"))
	orgKey := strings.TrimSpace(c.SecretManager.GetSecret("ONESIGNAL_ORG_API_KEY"))
	if orgID == "" || orgKey == "" {
		slog.Warn("provisionOneSignalApp: ONESIGNAL_ORG_ID / ONESIGNAL_ORG_API_KEY not set; skipping",
			"project_id", project.ID)
		return nil
	}

	// Idempotency guard: already provisioned → skip.
	if cfg, _ := c.DB.GetMobileAppConfig(project.ID.String()); cfg != nil && cfg.OneSignalAppID != "" {
		return nil
	}

	if c.CredStore == nil {
		return fmt.Errorf("provisionOneSignalApp: CredStore is not configured")
	}

	pkg := bundleIDForProject(project, "")
	fcmB64 := c.SecretManager.GetSecret("FCM_V1_SERVICE_ACCOUNT_JSON_B64")

	// Synthetic v1 Project value used purely as a parameter shape for the
	// external OneSignal helpers. Only PackageName (then OneSignalAppID for
	// the token call) are read by the helper.
	shape := models.Project{PackageName: pkg}

	osAppID, err := external.CreateOneSignalApplication(shape, orgID, orgKey, fcmB64)
	if err != nil {
		return fmt.Errorf("CreateOneSignalApplication: %w", err)
	}
	shape.OneSignalAppID = osAppID

	osToken, err := external.CreateOneSignalAppToken(shape, orgKey)
	if err != nil {
		return fmt.Errorf("CreateOneSignalAppToken: %w", err)
	}

	// Encrypt the REST key via CredStore + persist the cred ref alongside the
	// app id on MobileAppConfig. Plaintext REST key never lands in the DB.
	ref, err := c.CredStore.Put(ctx, project.OrganizationID, onesignalRESTKeyKind, []byte(osToken))
	if err != nil {
		return fmt.Errorf("credstore put onesignal rest key: %w", err)
	}
	modelRef := credRefFromStorage(ref)

	if _, err := c.DB.UpsertMobileAppConfigOneSignal(ctx, project.ID, osAppID, modelRef); err != nil {
		return fmt.Errorf("persist OneSignal IDs: %w", err)
	}

	slog.Info("provisionOneSignalApp: complete",
		"project_id", project.ID,
		"onesignal_app_id", osAppID,
	)
	return nil
}
