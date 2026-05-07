package controllers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/codemagic"
	"github.com/knid/wemob/internal/utils"
)

// ErrQuickPublishWrongType is returned by TriggerCodemagicOTAPublish when the
// project isn't a mobile_app_native project. Mapped to HTTP 409 by the view.
var ErrQuickPublishWrongType = errors.New("quick publish requires a mobile_app_native project")

// TriggerCodemagicOTAPublish dispatches the expo-ota-publish Codemagic
// workflow (P1 #12 mobile audit, 2026-05-04). The single mobile-deploy entry
// point — the legacy in-process Asynq publisher was removed 2026-05-07
// because Codemagic finishes ~3 min vs the worker's 25 min.
//
// Pipeline:
//  1. Verify project type (mobile_app_native).
//  2. Tarball mobile/ from the user's git repo.
//  3. Create the Build + AppBuildTask shadow rows so the existing
//     /v2/reports/expo-build webhook can update task status.
//  4. Upload the source tarball to the same S3 path Codemagic expects for
//     native builds (expo-source/<projectID>/<buildID>.tar.gz) — the YAML
//     step is identical.
//  5. Trigger Codemagic with the new workflow ID.
//
// Returns the Build + AppBuildTask so the frontend's existing build-status
// polling Just Works (the same path as a full native build).
func (c *Controller) TriggerCodemagicOTAPublish(
	ctx context.Context,
	project *models.AppProject,
	user *models.User,
) (*models.Build, *models.AppBuildTask, error) {
	if project == nil {
		return nil, nil, fmt.Errorf("project is required")
	}
	if project.ProjectType != models.ProjectTypeMobileAppNative {
		return nil, nil, ErrQuickPublishWrongType
	}
	if user == nil {
		return nil, nil, fmt.Errorf("user is required")
	}
	if c.CodemagicService == nil {
		return nil, nil, fmt.Errorf("codemagic service is not configured")
	}
	if c.GitRepoService == nil {
		return nil, nil, fmt.Errorf("git repo service is not configured")
	}
	if c.AssetsStorage == nil {
		return nil, nil, fmt.Errorf("assets storage is not configured")
	}

	bundleID := bundleIDForProject(project, "")

	// Pending build guard — direct AppProject scope (replaces the legacy
	// v1-shadow-Project pending check).
	if pending, perr := c.DB.GetPendingAppBuildTasksByAppProject(project.ID.String()); perr == nil && len(pending) > 0 {
		return nil, nil, fmt.Errorf("a build is already in progress, please wait")
	}

	// Tarball mobile/ from the project repo.
	repo, err := c.GitRepoService.Open(project.ID.String())
	if err != nil {
		return nil, nil, fmt.Errorf("open project repo: %w", err)
	}
	tarball, err := repo.TarSubtree("mobile/")
	if err != nil {
		return nil, nil, fmt.Errorf("tarball mobile/ subtree: %w", err)
	}

	// Next build number — direct AppProject scope. Keeps the same monotonic
	// sequence as native builds so AppBuildTask history reads cleanly.
	var lastBuildNumber int
	if last, lerr := c.DB.GetLastBuildByAppProject(project.ID.String()); lerr == nil && last != nil {
		lastBuildNumber = last.BuildNumber
	}
	nextNum := lastBuildNumber + 1

	// AppProjectID is the canonical FK to AppProject (v1 shadow Project was
	// removed 2026-05-07).
	build := &models.Build{
		AppProjectID:      project.ID,
		Active:            false,
		Title:             nonEmptyOr(project.Name, project.Slug),
		Icon:              "",
		SplashScreenColor: "#FFFFFF",
		NavBarStyle:       "default",
		BuildNumber:       nextNum,
		VersionString:     utils.CreateVersionString(nextNum),
	}
	build, err = c.DB.CreateProjectBuild(build)
	if err != nil {
		return nil, nil, fmt.Errorf("create build row: %w", err)
	}

	// Upload tarball at the same S3 key the YAML expects.
	tarKey := fmt.Sprintf("expo-source/%s/%s.tar.gz", project.ID.String(), build.ID.String())
	if err := c.AssetsStorage.PutBytes(ctx, tarKey, tarball, "application/gzip"); err != nil {
		return build, nil, fmt.Errorf("upload tarball to %s: %w", tarKey, err)
	}

	task, err := c.DB.CreateAppBuildTask(*build, project.ID, "OTA", "Queued", false)
	if err != nil {
		return build, nil, fmt.Errorf("create OTA build task: %w", err)
	}

	// Compute env vars matching what BuildExpoMobile passes to native builds.
	expoPublicAPIURL := c.BackendDeployURL(project)
	mobileCfg, _ := c.DB.GetMobileAppConfig(project.ID.String())

	expoPublicAuthBaseURL := ""
	if mobileCfg != nil && mobileCfg.AuthEnabled {
		expoPublicAuthBaseURL = expoPublicAPIURL
	}

	rcKeyIOS, rcKeyAndroid := "", ""
	if mobileCfg != nil && mobileCfg.IAPEnabled {
		rcKeyIOS = mobileCfg.RevenueCatPublicKeyIOS
		rcKeyAndroid = mobileCfg.RevenueCatPublicKeyAndroid
	}

	osAppID := ""
	if mobileCfg != nil && mobileCfg.OneSignalAppID != "" {
		osAppID = mobileCfg.OneSignalAppID
	}

	expoPublicOTAURL := ""
	if mobileCfg == nil || mobileCfg.OTAEnabled {
		expoPublicOTAURL = expoPublicAPIURL + "/v2/apps/" + project.ID.String() + "/ota"
	}

	marketingVer := "1.0.0"

	cmBuildID, triggerErr := c.CodemagicService.TriggerExpoOTAPublish(ctx, codemagic.ExpoOTAPublishParams{
		ProjectID:                      project.ID,
		BuildID:                        build.ID,
		TaskID:                         task.ID,
		AppTitle:                       build.Title,
		AppSlug:                        project.Slug,
		BundleID:                       bundleID,
		PackageName:                    bundleID,
		BuildNumber:                    nextNum,
		MarketingVersion:               marketingVer,
		IconKey:                        "",
		EASProjectUUID:                 project.ID.String(),
		OTAURL:                         expoPublicOTAURL,
		ExpoPublicAPIURL:               expoPublicAPIURL,
		ExpoPublicProjectID:            project.ID.String(),
		ExpoPublicAuthBaseURL:          expoPublicAuthBaseURL,
		ExpoPublicRevenueCatKeyIOS:     rcKeyIOS,
		ExpoPublicRevenueCatKeyAndroid: rcKeyAndroid,
		ExpoPublicOneSignalAppID:       osAppID,
	})
	if triggerErr != nil {
		task.StatusText = "Failed"
		task.ErrorMessage = triggerErr.Error()
		task.Finished = true
		_ = c.DB.UpdateAppBuildTask(task)
		return build, task, fmt.Errorf("trigger codemagic ota-publish: %w", triggerErr)
	}
	if cmBuildID != "" {
		// Persist the Codemagic build id so CancelBuildTask can call
		// POST /builds/{id}/cancel mid-publish (~3 min OTA workflow, but a
		// user might still cancel a stuck one).
		task.CodemagicBuildID = cmBuildID
		if uerr := c.DB.SetAppBuildTaskCodemagicBuildID(ctx, task.ID, cmBuildID); uerr != nil {
			slog.Warn("failed to persist codemagic build id (OTA)", "task_id", task.ID, "err", uerr)
		}
	}

	slog.Info("ota_quick_publish_triggered",
		"project_id", project.ID,
		"build_id", build.ID,
		"task_id", task.ID,
	)
	return build, task, nil
}
