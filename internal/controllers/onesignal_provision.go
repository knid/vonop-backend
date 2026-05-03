package controllers

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/utils/external"
)

// onesignalShadowPrefix marks placeholder OneSignal IDs that the P1.1.C
// ensureV1ProjectForExpo shim writes when the v1 shadow row is created
// before real per-tenant provisioning has happened.
const onesignalShadowPrefix = "expo-shadow-"

// provisionOneSignalApp ensures a real OneSignal application + REST API token
// exist for this mobile_app_native project. The IDs are stored on the v1
// shadow Project row (the same row P1.1.C creates for AppBuildTask FK reasons).
//
// Skip cases (return nil, no-op):
//   - ONESIGNAL_ORG_ID or ONESIGNAL_ORG_API_KEY unset (master keys missing)
//   - The v1 shadow row's OneSignalAppID does NOT have the "expo-shadow-" prefix
//     (already provisioned, idempotent guard)
//
// On any provisioning error, persist whatever we have (so a partial provision
// can be resumed by the caller on retry) and return the error so the caller
// can decide whether to surface it.
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

	v1, err := c.findOrCreateV1ShadowForExpo(ctx, project)
	if err != nil {
		return fmt.Errorf("v1 shadow lookup: %w", err)
	}

	// Idempotency guard: if the OneSignalAppID already has a real (non-shadow)
	// value, the project was provisioned already — skip.
	if v1.OneSignalAppID != "" && !strings.HasPrefix(v1.OneSignalAppID, onesignalShadowPrefix) {
		return nil
	}

	// Align package name with the bundle ID Codemagic uses; OneSignal app
	// "name" mirrors the user-visible bundle so the OneSignal dashboard is
	// readable per-tenant.
	v1.PackageName = bundleIDForProject(project, "")
	fcmB64 := c.SecretManager.GetSecret("FCM_V1_SERVICE_ACCOUNT_JSON_B64")

	osAppID, err := external.CreateOneSignalApplication(*v1, orgID, orgKey, fcmB64)
	if err != nil {
		return fmt.Errorf("CreateOneSignalApplication: %w", err)
	}
	v1.OneSignalAppID = osAppID

	// CreateOneSignalAppToken reads project.OneSignalAppID — populate it before calling.
	osToken, err := external.CreateOneSignalAppToken(*v1, orgKey)
	if err != nil {
		// Persist the app ID so a re-run only needs to mint the token. Bun
		// whole-row UPDATE footgun: scope the write to the columns we changed
		// (per memory feedback_bun_whole_row_update.md).
		if perr := c.persistShadowOneSignalIDs(ctx, v1); perr != nil {
			slog.Warn("provisionOneSignalApp: failed to persist app ID after token error", "error", perr, "project_id", project.ID)
		}
		return fmt.Errorf("CreateOneSignalAppToken: %w", err)
	}
	v1.OneSignalAppToken = osToken

	if err := c.persistShadowOneSignalIDs(ctx, v1); err != nil {
		return fmt.Errorf("persist OneSignal IDs: %w", err)
	}

	slog.Info("provisionOneSignalApp: complete",
		"project_id", project.ID,
		"v1_project_id", v1.ID,
		"onesignal_app_id", osAppID,
	)
	return nil
}

// persistShadowOneSignalIDs writes only the OneSignal columns (+ package name + updated_at)
// on the v1 shadow row. Avoids the bun whole-row UPDATE footgun where stale
// fields on the in-memory struct could overwrite real DB columns.
func (c *Controller) persistShadowOneSignalIDs(ctx context.Context, v1 *models.Project) error {
	if v1 == nil || v1.ID == uuid.Nil {
		return fmt.Errorf("nil v1 project")
	}
	_, err := c.DB.DB.NewUpdate().
		Model(v1).
		Column("one_signal_app_id", "one_signal_app_token", "package_name", "updated_at").
		WherePK().
		Exec(ctx)
	return err
}

// findOrCreateV1ShadowForExpo returns the v1 Project row keyed by url="expo://"+slug.
// Creates it (with the legacy "expo-shadow-{projectID}" placeholders) if missing.
// Caller mutates and persists.
func (c *Controller) findOrCreateV1ShadowForExpo(ctx context.Context, project *models.AppProject) (*models.Project, error) {
	if project == nil {
		return nil, fmt.Errorf("nil project")
	}
	shadowURL := "expo://" + project.Slug
	v1, err := c.DB.GetProjectByUserAndURL(project.UserID.String(), shadowURL)
	if err == nil && v1 != nil && v1.ID != uuid.Nil {
		return v1, nil
	}
	// Mirror BuildExpoMobile's shadow-row factory.
	v1 = &models.Project{
		UserID:            project.UserID,
		URL:               shadowURL,
		PackageName:       bundleIDForProject(project, ""),
		IconPath:          "",
		OneSignalAppID:    onesignalShadowPrefix + project.ID.String(),
		OneSignalAppToken: onesignalShadowPrefix + "token-" + project.ID.String(),
	}
	v1, err = c.DB.CreateProject(v1)
	if err != nil {
		return nil, fmt.Errorf("create v1 shadow: %w", err)
	}
	return v1, nil
}
