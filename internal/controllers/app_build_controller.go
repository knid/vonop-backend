package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/codemagic"
	"github.com/knid/wemob/internal/utils"
	"github.com/knid/wemob/internal/utils/external"
)

// BuildAppMobile creates a native mobile build for an AI-generated app by wrapping
// its deployed web URL using the URL-wrapper build system.
//
// Accepts one or more platforms so the UI can kick off Android + iOS in one
// click without the second request colliding with the first's pending-task
// guard. A single Build record is shared across platforms; each platform gets
// its own AppBuildTask.
//
// Codemagic triggers run best-effort per platform: a trigger failure marks
// that platform's task as Failed but does not abort the others. The error
// return is non-nil only when the whole call can't proceed (limits, pending
// build, missing web deploy, etc.). Callers should inspect task.StatusText
// per returned task to see per-platform outcomes.
func (c *Controller) BuildAppMobile(appProject *models.AppProject, user *models.User, platforms []string, appName string, icon string, splashColor string) (*models.Build, []*models.AppBuildTask, error) {
	if len(platforms) == 0 {
		return nil, nil, fmt.Errorf("at least one platform is required")
	}

	// Normalise + dedupe platform list so downstream code can rely on a clean
	// ["android"]/["ios"]/["android","ios"] shape.
	wantAndroid := false
	wantIOS := false
	for _, p := range platforms {
		switch strings.ToLower(strings.TrimSpace(p)) {
		case "android":
			wantAndroid = true
		case "ios":
			wantIOS = true
		default:
			return nil, nil, fmt.Errorf("unsupported platform: %q", p)
		}
	}

	// Enforce platform-specific build permissions up front. Reject the whole
	// call if any requested platform is disallowed — "partial build" is a
	// surprise we don't want to spring on paid users.
	limits, err := c.GetUserLimits(user)
	if err != nil {
		slog.Warn("BuildAppMobile: failed to get user limits", "error", err, "user_id", user.ID)
	} else {
		if wantAndroid && !limits.CanBuildToAndroid {
			return nil, nil, fmt.Errorf("your subscription does not allow Android builds")
		}
		if wantIOS && !limits.CanBuildToIOS {
			return nil, nil, fmt.Errorf("your subscription does not allow iOS builds")
		}
	}

	// Get the deployed web URL — the app must be deployed to web first
	deployment, err := c.DB.GetLatestAppDeployment(appProject.ID.String(), "web")
	if err != nil || deployment.DeployedURL == "" {
		return nil, nil, fmt.Errorf("app must be deployed to web first")
	}

	wrappedURL := deployment.DeployedURL

	// Find or create an old-style Project for this URL
	project, err := c.DB.GetProjectByUserAndURL(user.ID.String(), wrappedURL)
	if err != nil {
		// Auto-generate package name
		pkHex, hexErr := utils.RandomHex(5)
		if hexErr != nil {
			return nil, nil, fmt.Errorf("failed to generate package name: %w", hexErr)
		}
		pkName := "com.wemob.app" + pkHex[0:5]

		// Create new project linked to this AI app's URL
		project = &models.Project{
			UserID:            user.ID,
			URL:               wrappedURL,
			PackageName:       pkName,
			IconPath:          icon,
			OneSignalAppID:    "wemob-app-" + appProject.ID.String(),
			OneSignalAppToken: "wemob-app-token-" + appProject.ID.String(),
			BuildForAndroid:   wantAndroid,
			BuildForIOS:       wantIOS,
		}
		project, err = c.DB.CreateProject(project)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create build project: %w", err)
		}
	} else {
		// Update platform flags and metadata on existing project. Union the
		// flags with the new request so a project that previously only built
		// Android now also builds iOS without losing its Android flag.
		if wantAndroid {
			project.BuildForAndroid = true
		}
		if wantIOS {
			project.BuildForIOS = true
		}
		if icon != "" {
			project.IconPath = icon
		}

		// Replace fake OneSignal IDs with real ones if needed
		if strings.HasPrefix(project.OneSignalAppID, "wemob-app-") {
			if err := c.replaceWithRealOneSignal(project); err != nil {
				slog.Warn("Failed to replace fake OneSignal IDs, continuing with existing", "error", err, "project_id", project.ID)
			}
		}

		c.DB.UpdateProject(project)
	}

	// Check for pending builds (any platform)
	pendingTasks, err := c.DB.GetPendingAppBuildTasks(project)
	if err == nil && len(pendingTasks) > 0 {
		return nil, nil, fmt.Errorf("a build is already in progress, please wait")
	}

	// Get last build number
	var lastBuildNumber int
	lastBuild, err := c.DB.GetLastProjectBuild(project)
	if err == nil {
		lastBuildNumber = lastBuild.BuildNumber
	}

	// Create single build record shared across platforms
	build := &models.Build{
		ProjectID:         project.ID,
		Active:            false,
		Title:             appName,
		Icon:              icon,
		SplashScreenColor: splashColor,
		NavBarStyle:       "default",
		BuildNumber:       lastBuildNumber + 1,
		VersionString:     utils.CreateVersionString(lastBuildNumber + 1),
	}

	build, err = c.DB.CreateProjectBuild(build)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create build record")
	}

	// Fetch mobile app config — try v1 Project ID first, then AppProject ID
	mobileConfig, _ := c.DB.GetMobileAppConfig(project.ID.String())
	if mobileConfig == nil {
		mobileConfig, _ = c.DB.GetMobileAppConfig(appProject.ID.String())
	}

	// Sync config to v1 Project ID so the Flutter app can fetch it at runtime
	if mobileConfig != nil && mobileConfig.ProjectID != project.ID {
		configCopy := *mobileConfig
		configCopy.ProjectID = project.ID
		c.DB.UpsertMobileAppConfig(&configCopy)
	}

	baseParams := codemagic.AndroidBuildParams{
		ProjectID:           project.ID.String(),
		BuildID:             build.ID.String(),
		Version:             build.BuildNumber,
		Title:               appName,
		URL:                 wrappedURL,
		IconURL:             icon,
		SplashColor:         splashColor,
		PackageName:         project.PackageName,
		Permissions:         getPermissions(mobileConfig),
		URLScheme:           getURLScheme(mobileConfig),
		UniversalLinkDomain: getUniversalLink(mobileConfig),
	}

	// Stable platform order for deterministic response
	ordered := make([]string, 0, 2)
	if wantAndroid {
		ordered = append(ordered, "android")
	}
	if wantIOS {
		ordered = append(ordered, "ios")
	}

	tasks := make([]*models.AppBuildTask, 0, len(ordered))
	triggerErrs := make([]string, 0)
	for _, platform := range ordered {
		label := "Android"
		if platform == "ios" {
			label = "IOS"
		}
		task, err := c.DB.CreateAppBuildTask(*build, label, "Queued", false)
		if err != nil {
			return build, tasks, fmt.Errorf("failed to create %s build task: %w", platform, err)
		}
		tasks = append(tasks, task)

		p := baseParams
		p.TaskID = task.ID.String()

		var triggerErr error
		if platform == "android" {
			_, triggerErr = c.CodemagicService.TriggerAndroidBuild(p)
		} else {
			_, triggerErr = c.CodemagicService.TriggerIOSBuild(codemagic.IOSBuildParams{
				ProjectID:           p.ProjectID,
				BuildID:             p.BuildID,
				TaskID:              p.TaskID,
				Version:             p.Version,
				Title:               p.Title,
				URL:                 p.URL,
				IconURL:             p.IconURL,
				SplashColor:         p.SplashColor,
				PackageName:         p.PackageName,
				Permissions:         p.Permissions,
				URLScheme:           p.URLScheme,
				UniversalLinkDomain: p.UniversalLinkDomain,
			})
		}

		if triggerErr != nil {
			// Mark this platform's task as failed, capture the error, and
			// keep going — the other platform's trigger may still succeed.
			task.StatusText = "Failed"
			task.ErrorMessage = triggerErr.Error()
			task.Finished = true
			_ = c.DB.UpdateAppBuildTask(task)
			triggerErrs = append(triggerErrs, fmt.Sprintf("%s: %v", platform, triggerErr))
		}
	}

	slog.Info("BuildAppMobile: build enqueued",
		"app_project_id", appProject.ID,
		"platforms", ordered,
		"build_id", build.ID,
		"url", wrappedURL,
	)

	// If every platform failed to trigger, surface an error so the view
	// returns a 5xx. If only some failed, the caller can see per-task state.
	if len(triggerErrs) == len(ordered) {
		return build, tasks, fmt.Errorf("failed to trigger build(s): %s", strings.Join(triggerErrs, "; "))
	}

	return build, tasks, nil
}

// BuildExpoMobile triggers a new Expo build for a mobile_app_native project.
//
// Flow:
//  1. Validate project type + per-platform CanBuild* permissions
//  2. Find or create a v1 Project shadow keyed by url="expo://"+slug — the
//     AppBuildTask FK still references the legacy Project model, so we need
//     a stable shadow row per AppProject (mirrors BuildAppMobile pattern)
//  3. Reject if a build for that shadow project is still in flight
//  4. Compute the next build number from the last shadow-project build
//  5. Tarball the mobile/ subtree from the project's git repo
//  6. Upload tarball to s3://{assets}/expo-source/{projectID}/{buildID}.tar.gz
//  7. Insert Build + per-platform AppBuildTask rows
//  8. Trigger Codemagic per platform; mark per-task failed on trigger error
//
// Per-platform error isolation matches BuildAppMobile: a triggerErr on one
// platform marks that task Failed and continues with the others. Returns a
// non-nil error only when the call can't make ANY progress (no platforms
// triggered) OR a precondition failed (limits, pending build, missing
// services, repo open failure).
func (c *Controller) BuildExpoMobile(ctx context.Context, project *models.AppProject, user *models.User, opts ExpoBuildOptions) (*models.Build, []*models.AppBuildTask, error) {
	if project == nil {
		return nil, nil, fmt.Errorf("project is required")
	}
	if project.ProjectType != models.ProjectTypeMobileAppNative {
		return nil, nil, fmt.Errorf("BuildExpoMobile: project type %q is not %q", project.ProjectType, models.ProjectTypeMobileAppNative)
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

	// Normalise + dedupe platform list so downstream code can rely on a
	// clean ["android"]/["ios"]/["android","ios"] shape.
	wantAndroid := false
	wantIOS := false
	for _, p := range opts.Platforms {
		switch strings.ToLower(strings.TrimSpace(p)) {
		case "android":
			wantAndroid = true
		case "ios":
			wantIOS = true
		default:
			return nil, nil, fmt.Errorf("unsupported platform: %q", p)
		}
	}
	if !wantAndroid && !wantIOS {
		return nil, nil, fmt.Errorf("at least one platform is required")
	}

	// Per-platform permission gate. GetUserLimits returns an error when the
	// user has no subscription/limits row — we silently skip the check in
	// that case (matches BuildAppMobile behavior; the test fixture for the
	// permission-denied case wires real limits + RemoteConfig).
	if limits, err := c.GetUserLimits(user); err == nil && limits != nil {
		if wantAndroid && !limits.CanBuildToAndroid {
			return nil, nil, fmt.Errorf("your subscription does not allow Android builds")
		}
		if wantIOS && !limits.CanBuildToIOS {
			return nil, nil, fmt.Errorf("your subscription does not allow iOS builds")
		}
	}

	bundleID := bundleIDForProject(project, opts.BundleIDOverride)

	// AppBuildTask + Build are keyed by v1 Project. Find or create the
	// shadow row keyed by URL="expo://"+slug (unique per AppProject,
	// avoids collision with legacy URL-wrapper builds).
	shadowURL := "expo://" + project.Slug
	v1project, err := c.DB.GetProjectByUserAndURL(user.ID.String(), shadowURL)
	if err != nil || v1project == nil || v1project.ID == uuid.Nil {
		// OneSignal IDs are notnull,unique on Project. Expo apps don't use
		// OneSignal (push goes through Expo Notifications), so we stash
		// stable placeholders here that won't collide with the real
		// "wemob-app-{appProjectID}" prefix used by BuildAppMobile.
		v1project = &models.Project{
			UserID:            user.ID,
			URL:               shadowURL,
			PackageName:       bundleID,
			IconPath:          opts.Icon,
			OneSignalAppID:    "expo-shadow-" + project.ID.String(),
			OneSignalAppToken: "expo-shadow-token-" + project.ID.String(),
			BuildForAndroid:   wantAndroid,
			BuildForIOS:       wantIOS,
		}
		v1project, err = c.DB.CreateProject(v1project)
		if err != nil {
			return nil, nil, fmt.Errorf("create v1 shadow project: %w", err)
		}
	} else {
		// Union platform flags so a re-build picks up new platforms.
		if wantAndroid {
			v1project.BuildForAndroid = true
		}
		if wantIOS {
			v1project.BuildForIOS = true
		}
		if opts.Icon != "" {
			v1project.IconPath = opts.Icon
		}
		c.DB.UpdateProject(v1project)
	}

	// Pending build guard.
	if pending, perr := c.DB.GetPendingAppBuildTasks(v1project); perr == nil && len(pending) > 0 {
		return nil, nil, fmt.Errorf("a build is already in progress, please wait")
	}

	// Next build number.
	var lastBuildNumber int
	if last, lerr := c.DB.GetLastProjectBuild(v1project); lerr == nil && last != nil {
		lastBuildNumber = last.BuildNumber
	}
	nextNum := lastBuildNumber + 1

	marketingVer := nonEmptyOr(opts.MarketingVersion, "1.0.0")

	// Tarball mobile/ from the project repo.
	repo, err := c.GitRepoService.Open(project.ID.String())
	if err != nil {
		return nil, nil, fmt.Errorf("open project repo: %w", err)
	}
	tarball, err := repo.TarSubtree("mobile/")
	if err != nil {
		return nil, nil, fmt.Errorf("tarball mobile/ subtree: %w", err)
	}

	// Create Build row first so we have the buildID for the S3 path.
	build := &models.Build{
		ProjectID:         v1project.ID,
		Active:            false,
		Title:             nonEmptyOr(opts.AppName, project.Name),
		Icon:              opts.Icon,
		SplashScreenColor: nonEmptyOr(opts.SplashColor, "#FFFFFF"),
		NavBarStyle:       "default",
		BuildNumber:       nextNum,
		VersionString:     utils.CreateVersionString(nextNum),
	}
	build, err = c.DB.CreateProjectBuild(build)
	if err != nil {
		return nil, nil, fmt.Errorf("create build row: %w", err)
	}

	// Upload tarball to S3 at the path the codemagic.yaml expects.
	tarKey := fmt.Sprintf("expo-source/%s/%s.tar.gz", project.ID.String(), build.ID.String())
	if err := c.AssetsStorage.PutBytes(ctx, tarKey, tarball, "application/gzip"); err != nil {
		return build, nil, fmt.Errorf("upload tarball to %s: %w", tarKey, err)
	}

	// Compute the public API URL the mobile app will hit at runtime.
	// Resolves to the backend's deployed URL when present, or the canonical
	// {slug}.wemob.app fallback for first builds before any deploy lands.
	expoPublicAPIURL := c.BackendDeployURL(project)

	// Fetch MobileAppConfig once. nil-safe — a missing config (e.g. legacy
	// mobile_app projects) leaves all the optional sets empty.
	mobileCfg, _ := c.DB.GetMobileAppConfig(project.ID.String())

	// Compute the auth base URL (P2.4). Empty when AuthEnabled is false; the
	// mobile auth client falls back to EXPO_PUBLIC_API_URL in that case.
	expoPublicAuthBaseURL := ""
	if mobileCfg != nil && mobileCfg.AuthEnabled {
		expoPublicAuthBaseURL = expoPublicAPIURL
	}

	// IAP / RevenueCat (P2.3): bake public SDK keys into the binary at
	// build time. Lazy-provision when iap_enabled=true but RC project ID is
	// missing — covers projects created before P2.3 shipped, transient
	// outages, and future config-toggle flow.
	rcKeyIOS, rcKeyAndroid := "", ""
	if mobileCfg != nil && mobileCfg.IAPEnabled {
		if mobileCfg.RevenueCatProjectID == "" && c.RevenueCatService != nil {
			if perr := c.provisionRevenueCat(ctx, project); perr != nil {
				slog.Warn("BuildExpoMobile: lazy RC provision failed; building with empty IAP keys",
					"error", perr, "project_id", project.ID)
			}
			mobileCfg, _ = c.DB.GetMobileAppConfig(project.ID.String())
		}
		if mobileCfg != nil {
			rcKeyIOS = mobileCfg.RevenueCatPublicKeyIOS
			rcKeyAndroid = mobileCfg.RevenueCatPublicKeyAndroid
		}
	}

	// P2.2 — OneSignal app ID (per-tenant). Lazy-provision when the v1
	// shadow row still has the "expo-shadow-{projectID}" placeholder.
	osAppID := ""
	if !strings.HasPrefix(v1project.OneSignalAppID, onesignalShadowPrefix) {
		osAppID = v1project.OneSignalAppID
	} else {
		if perr := c.provisionOneSignalApp(ctx, project); perr != nil {
			slog.Warn("BuildExpoMobile: lazy OneSignal provision failed; building with empty app ID",
				"error", perr, "project_id", project.ID)
		}
		if refreshed, rerr := c.DB.GetProjectByUserAndURL(user.ID.String(), shadowURL); rerr == nil && refreshed != nil && !strings.HasPrefix(refreshed.OneSignalAppID, onesignalShadowPrefix) {
			osAppID = refreshed.OneSignalAppID
		}
	}

	// P2.1: per-app self-hosted OTA URL. The codemagic.yaml step substitutes
	// this into app.json's expo.updates.url at prebuild time so the mobile
	// client cold-start checkForUpdateAsync hits us. When OTAEnabled=false on
	// MobileAppConfig, we pass empty string — the codemagic step then sets
	// expo.updates = null, which makes checkForUpdateAsync silently no-op.
	// Default true on missing config (matches the column's NOT NULL DEFAULT true).
	expoPublicOTAURL := ""
	if mobileCfg == nil || mobileCfg.OTAEnabled {
		expoPublicOTAURL = expoPublicAPIURL + "/v2/apps/" + project.ID.String() + "/ota"
	}

	ordered := make([]string, 0, 2)
	if wantAndroid {
		ordered = append(ordered, "android")
	}
	if wantIOS {
		ordered = append(ordered, "ios")
	}

	tasks := make([]*models.AppBuildTask, 0, len(ordered))
	triggerErrs := make([]string, 0)
	firstBuild := lastBuildNumber == 0

	for _, platform := range ordered {
		label := "Android"
		if platform == "ios" {
			label = "IOS"
		}
		task, err := c.DB.CreateAppBuildTask(*build, label, "Queued", false)
		if err != nil {
			return build, tasks, fmt.Errorf("create %s build task: %w", platform, err)
		}
		tasks = append(tasks, task)

		var triggerErr error
		switch platform {
		case "android":
			_, triggerErr = c.CodemagicService.TriggerExpoAndroidBuild(ctx, codemagic.ExpoAndroidBuildParams{
				ProjectID:                      project.ID,
				BuildID:                        build.ID,
				TaskID:                         task.ID,
				AppTitle:                       build.Title,
				AppSlug:                        project.Slug,
				BundleID:                       bundleID,
				PackageName:                    bundleID,
				BuildNumber:                    nextNum,
				MarketingVersion:               marketingVer,
				SplashColor:                    build.SplashScreenColor,
				FirstBuild:                     firstBuild,
				IconKey:                        opts.Icon,
				EASProjectUUID:                 project.ID.String(), // stable per project, set in P0.1
				ExpoPublicAPIURL:               expoPublicAPIURL,
				ExpoPublicProjectID:            project.ID.String(),
				ExpoPublicAuthBaseURL:          expoPublicAuthBaseURL,
				ExpoPublicRevenueCatKeyIOS:     rcKeyIOS,
				ExpoPublicRevenueCatKeyAndroid: rcKeyAndroid,
				ExpoPublicOneSignalAppID:       osAppID,
				ExpoPublicOTAURL:               expoPublicOTAURL,
			})
		case "ios":
			_, triggerErr = c.CodemagicService.TriggerExpoIOSBuild(ctx, codemagic.ExpoIOSBuildParams{
				ProjectID:                      project.ID,
				BuildID:                        build.ID,
				TaskID:                         task.ID,
				AppTitle:                       build.Title,
				AppSlug:                        project.Slug,
				BundleID:                       bundleID,
				BuildNumber:                    nextNum,
				MarketingVersion:               marketingVer,
				SplashColor:                    build.SplashScreenColor,
				IconKey:                        opts.Icon,
				EASProjectUUID:                 project.ID.String(),
				// XcodeScheme intentionally left blank — codemagic.yaml derives
				// it post-prebuild (P1.1.A T4 fix).
				ExpoPublicAPIURL:               expoPublicAPIURL,
				ExpoPublicProjectID:            project.ID.String(),
				ExpoPublicAuthBaseURL:          expoPublicAuthBaseURL,
				ExpoPublicRevenueCatKeyIOS:     rcKeyIOS,
				ExpoPublicRevenueCatKeyAndroid: rcKeyAndroid,
				ExpoPublicOneSignalAppID:       osAppID,
				ExpoPublicOTAURL:               expoPublicOTAURL,
			})
		}

		if triggerErr != nil {
			task.StatusText = "Failed"
			task.ErrorMessage = triggerErr.Error()
			task.Finished = true
			_ = c.DB.UpdateAppBuildTask(task)
			triggerErrs = append(triggerErrs, fmt.Sprintf("%s: %v", platform, triggerErr))
		}
	}

	slog.Info("BuildExpoMobile: build enqueued",
		"project_id", project.ID,
		"v1_project_id", v1project.ID,
		"build_id", build.ID,
		"platforms", ordered,
		"tarball_size_bytes", len(tarball),
	)

	if len(triggerErrs) == len(ordered) {
		return build, tasks, fmt.Errorf("failed to trigger build(s): %s", strings.Join(triggerErrs, "; "))
	}
	return build, tasks, nil
}

// ExpoBuildOptions describes the parameters for a new Expo mobile build.
// User-passthrough fields (AppName, Icon, SplashColor) mirror
// BuildAppMobile's signature so the HTTP view can pass them through
// unchanged in P1.1.C T3.
type ExpoBuildOptions struct {
	Platforms        []string // "android", "ios", or both
	AppName          string   // user-facing app title (Info.plist + strings.xml)
	Icon             string   // S3 key (under icons/) for the app icon
	SplashColor      string   // hex color for the splash screen
	MarketingVersion string   // semantic version, e.g. "1.2.0"; defaults to "1.0.0"
	BundleIDOverride string   // optional override; defaults to com.wemob.{slug}
}

// bundleIDForProject returns opts.BundleIDOverride if set, else
// `com.wemob.{slug}` with hyphens stripped (Apple bundle IDs disallow
// hyphens in the application identifier).
func bundleIDForProject(project *models.AppProject, override string) string {
	if override != "" {
		return override
	}
	clean := strings.ReplaceAll(strings.ToLower(project.Slug), "-", "")
	return "com.wemob." + clean
}

// nonEmptyOr returns s if non-empty, otherwise fallback.
func nonEmptyOr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// UndeployApp marks all deployments for this project as undeployed and
// enqueues a Fly app teardown so machines, IPs, and registry images stop
// accumulating on canceled accounts.
func (c *Controller) UndeployApp(projectID string) error {
	deployments, err := c.DB.GetAppDeploymentsByProjectID(projectID)
	if err != nil {
		return fmt.Errorf("failed to get deployments: %w", err)
	}
	for _, d := range deployments {
		if d.Status == "deployed" || d.Status == "building" {
			d.Status = "undeployed"
			d.UpdatedAt = time.Now()
			if _, err := c.DB.UpdateAppDeployment(&d); err != nil {
				slog.Error("failed to update deployment status", "error", err, "deployment_id", d.ID)
			}
		}
	}

	if runtimeApp, raErr := c.DB.GetRuntimeAppByAppProjectID(projectID); raErr == nil && runtimeApp != nil && runtimeApp.FlyAppName != "" {
		if err := c.EnqueueFlyDelete(runtimeApp.FlyAppName, runtimeApp.ID); err != nil {
			slog.Warn("failed to enqueue Fly app delete on undeploy", "error", err, "fly_app", runtimeApp.FlyAppName, "project_id", projectID)
		}
	}
	return nil
}

// GetAppDeployments returns all deployments for a project
func (c *Controller) GetAppDeployments(projectID string) ([]models.AppDeployment, error) {
	return c.DB.GetAppDeploymentsByProjectID(projectID)
}

func (c *Controller) GetLatestDeploymentStatus(projectID string) (*models.AppDeployment, error) {
	// For mobile_app_native projects, the user-facing deploy is an
	// expo-updates publish (platform="ota"). All other types deploy a
	// web bundle to Fly (platform="web").
	platform := "web"
	if project, err := c.DB.GetAppProjectByID(projectID); err == nil && project != nil &&
		project.ProjectType == models.ProjectTypeMobileAppNative {
		platform = otaDeployPlatform
	}
	return c.DB.GetLatestAppDeployment(projectID, platform)
}

type AppValidationError struct {
	File  string `json:"file"`
	Line  int    `json:"line,omitempty"`
	Error string `json:"error"`
	Fix   string `json:"fix,omitempty"`
}

type AppValidationResult struct {
	Valid  bool                 `json:"valid"`
	Errors []AppValidationError `json:"errors"`
}

// ValidateAppFiles runs LLM-based validation on project files.
// Static deterministic checks were removed — the E2B sandbox catches build
// errors at runtime, making a static pre-flight layer redundant.
func (c *Controller) ValidateAppFiles(projectID string) (*AppValidationResult, error) {
	repo, err := c.GitRepoService.Open(projectID)
	if err != nil {
		return nil, fmt.Errorf("no files found to validate")
	}
	fileMap, err := repo.ReadAllFiles()
	if err != nil || len(fileMap) == 0 {
		return nil, fmt.Errorf("no files found to validate")
	}

	result := AppValidationResult{Valid: true}

	// Run LLM-based validation
	provider := c.llmProvider()
	model := c.SecretManager.GetSecret("WEBSITE_BUILDER_LLM_MODEL")
	if model == "" {
		model = provider.DefaultModel()
	}

	var fileContext strings.Builder
	fileContext.WriteString("Project files:\n")
	for path, content := range fileMap {
		fileContext.WriteString(fmt.Sprintf("\n--- %s (%s) ---\n%s\n", path, InferFileType(path), content))
	}

	responseJSON, err := utils.GetJSONFromLLM(context.Background(), model, constants.NEXTJS_VALIDATE_PROMPT, fileContext.String(), provider)
	if err != nil {
		slog.Error("validation LLM call failed", "error", err)
		return &result, nil
	}

	var llmResult AppValidationResult
	if err := json.Unmarshal([]byte(responseJSON), &llmResult); err != nil {
		cleaned := utils.ExtractJSON(responseJSON)
		if err2 := json.Unmarshal([]byte(cleaned), &llmResult); err2 != nil {
			slog.Warn("failed to parse validation response", "error", err2)
			return &result, nil
		}
	}

	if !llmResult.Valid {
		result.Valid = false
		result.Errors = append(result.Errors, llmResult.Errors...)
	}

	return &result, nil
}

// GetPreviewBundle returns all project files and env vars for the E2B
// sandbox preview. The shape varies by project_type:
//
//   - mobile_app_native: returns only the mobile/ subtree (with the prefix
//     stripped) so the sandbox runs `npx expo start --web` against the
//     Expo project directly. backend/ is omitted — it deploys to Fly,
//     not into the preview sandbox. Next.js-specific patches
//     (custom error pages, analytics script) are skipped because the
//     sandbox runs Metro/Expo, not Next.js.
//   - everything else: returns the full file map with Next.js patches.
//
// project_type is included so the frontend's /api/e2b/start route can
// branch its sandbox setup (which dev-server command, which port to wait
// on) without an extra backend round-trip.
func (c *Controller) GetPreviewBundle(project *models.AppProject) (map[string]interface{}, error) {
	repo, err := c.GitRepoService.Open(project.ID.String())
	if err != nil {
		return nil, fmt.Errorf("no generated files found")
	}
	fileMap, err := repo.ReadAllFiles()
	if err != nil || len(fileMap) == 0 {
		return nil, fmt.Errorf("no generated files found")
	}

	envVars := c.CollectAllEnvVars(project)

	if project.ProjectType == models.ProjectTypeMobileAppNative {
		mobileFiles := make(map[string]string, len(fileMap))
		for path, content := range fileMap {
			if strings.HasPrefix(path, "mobile/") {
				mobileFiles[strings.TrimPrefix(path, "mobile/")] = content
			}
		}
		return map[string]interface{}{
			"files":        mobileFiles,
			"env_vars":     envVars,
			"project_type": project.ProjectType,
		}, nil
	}

	// Defensive: ensure the preview bundle always ships custom error pages so
	// users never see Next.js's default dev error UI in the iframe. Only writes
	// files that are missing — user-customized versions are preserved.
	injectCustomErrorPages(fileMap, constants.DetectLanguage(project.Prompt))

	return map[string]interface{}{
		"files":        fileMap,
		"env_vars":     envVars,
		"project_type": project.ProjectType,
	}, nil
}

// injectCustomErrorPages writes app/error.tsx, app/not-found.tsx, and
// app/global-error.tsx into the file map if they're missing. Supports both the
// "app/" and "src/app/" directory layouts — whichever one the project uses is
// detected from the existing layout file location.
func injectCustomErrorPages(fileMap map[string]string, lang string) {
	// Detect app-directory layout from existing layout file
	prefix := "app/"
	if _, ok := fileMap["src/app/layout.tsx"]; ok {
		prefix = "src/app/"
	} else if _, ok := fileMap["src/app/layout.jsx"]; ok {
		prefix = "src/app/"
	}

	writeIfMissing := func(path, content string) {
		if _, exists := fileMap[path]; !exists {
			fileMap[path] = content
		}
	}

	writeIfMissing(prefix+"error.tsx", constants.GetErrorTemplate(lang))
	writeIfMissing(prefix+"not-found.tsx", constants.GetNotFoundTemplate(lang))
	writeIfMissing(prefix+"global-error.tsx", constants.GetGlobalErrorTemplate(lang))
}

// injectAnalyticsScript adds the web analytics tracking script into the root
// layout file. For JSX files (.tsx/.jsx) it uses the JSX-safe
// dangerouslySetInnerHTML pattern so the parser doesn't choke on raw JS.
// The __PROJECT_ID__ placeholder is replaced with the actual project UUID.
// Note: the script body is a static, server-controlled string (not user input)
// so there is no XSS risk from using dangerouslySetInnerHTML here.
func injectAnalyticsScript(fileMap map[string]string, projectID string) {
	jsBody := strings.Replace(constants.WEB_ANALYTICS_SCRIPT_BODY, "__PROJECT_ID__", projectID, 1)

	// Escape backticks so the JS body is safe inside a JSX template literal
	escapedBody := strings.ReplaceAll(jsBody, "`", "\\`")

	// JSX-compatible: uses dangerouslySetInnerHTML so the JSX parser
	// doesn't try to interpret the JS as JSX children.
	jsxScript := `<script data-wemob-analytics dangerouslySetInnerHTML={{ __html: ` + "`" + escapedBody + "`" + ` }} />`

	layoutPaths := []string{
		"app/layout.tsx",
		"app/layout.jsx",
		"src/app/layout.tsx",
		"src/app/layout.jsx",
	}

	for _, path := range layoutPaths {
		content, ok := fileMap[path]
		if !ok {
			continue
		}

		// Prefer injecting before </head>
		if idx := strings.Index(content, "</head>"); idx != -1 {
			fileMap[path] = content[:idx] + jsxScript + "\n" + content[idx:]
			return
		}
		// Fallback: inject before </body>
		if idx := strings.Index(content, "</body>"); idx != -1 {
			fileMap[path] = content[:idx] + jsxScript + "\n" + content[idx:]
			return
		}
	}

	slog.Warn("analytics script injection: no suitable layout found", "project_id", projectID)
}

// injectWemobBadge injects a "Created by Wemob" badge script into the root layout.
// Uses the same pattern as injectAnalyticsScript — hardcoded constant, not user input.
func injectWemobBadge(fileMap map[string]string) {
	escapedBody := strings.ReplaceAll(constants.WEMOB_BADGE_SCRIPT, "`", "\\`")
	jsxScript := `<script data-wemob-badge dangerouslySetInnerHTML={{ __html: ` + "`" + escapedBody + "`" + ` }} />`

	layoutPaths := []string{
		"app/layout.tsx",
		"app/layout.jsx",
		"src/app/layout.tsx",
		"src/app/layout.jsx",
	}

	for _, path := range layoutPaths {
		content, ok := fileMap[path]
		if !ok {
			continue
		}
		if idx := strings.Index(content, "</body>"); idx != -1 {
			fileMap[path] = content[:idx] + jsxScript + "\n" + content[idx:]
			return
		}
	}
}

// CaptureAppScreenshot takes a desktop screenshot of the preview URL and uploads it to S3.
// Intended to be called in a goroutine (fire-and-forget).
func (c *Controller) CaptureAppScreenshot(project *models.AppProject, previewURL string) {
	// Wait for the preview to fully render before capturing
	time.Sleep(10 * time.Second)

	apiKey := c.SecretManager.GetSecret("SITESHOT_API_KEY")
	if apiKey == "" {
		slog.Warn("CaptureAppScreenshot: SITESHOT_API_KEY not configured")
		return
	}

	// Use mobile resolution if the project has a mobile app config
	var imgData []byte
	var err error
	_, mobileErr := c.DB.GetMobileAppConfig(project.ID.String())
	isMobile := mobileErr == nil
	if isMobile {
		imgData, err = external.GetScreenshotOfURL(previewURL, apiKey)
	} else {
		imgData, err = external.GetDesktopScreenshotOfURL(previewURL, apiKey)
	}
	if err != nil {
		slog.Warn("CaptureAppScreenshot: failed to capture screenshot", "error", err, "project_id", project.ID)
		return
	}

	if len(imgData) == 0 {
		slog.Warn("CaptureAppScreenshot: empty screenshot data", "project_id", project.ID)
		return
	}

	// Compose mobile screenshots into phone mockup frame
	if isMobile {
		imgData, err = utils.ComposeMockup(imgData)
		if err != nil {
			slog.Warn("CaptureAppScreenshot: failed to compose mockup", "error", err, "project_id", project.ID)
			return
		}
	}

	s3Key := fmt.Sprintf("app-screenshots/%s.png", project.ID.String())
	if err := c.AssetsStorage.UploadFileByteWithContentType(context.Background(), s3Key, imgData, "image/png"); err != nil {
		slog.Warn("CaptureAppScreenshot: failed to upload to S3", "error", err, "project_id", project.ID)
		return
	}

	project.ScreenshotS3Key = s3Key
	if _, err := c.DB.UpdateAppProject(project); err != nil {
		slog.Warn("CaptureAppScreenshot: failed to update project", "error", err, "project_id", project.ID)
	}

	slog.Info("CaptureAppScreenshot: screenshot captured", "project_id", project.ID, "s3_key", s3Key)
}

// ensureV1ProjectForMobileApp creates a v1 Project (with real OneSignal IDs)
// and an initial Build record for the given mobile_app AppProject, unless one
// already exists for the same user+URL.
func (c *Controller) ensureV1ProjectForMobileApp(appProject *models.AppProject, user *models.User, deployedURL string) error {
	// Check if v1 Project already exists for this URL
	if existing, err := c.DB.GetProjectByUserAndURL(user.ID.String(), deployedURL); err == nil && existing.ID.String() != "" {
		// Ensure the mobile AppProject points at it — earlier deploys may
		// have created the v1 Project without writing the id into settings.
		if err := c.DB.SetAppProjectV1ProjectID(appProject.ID.String(), existing.ID.String()); err != nil {
			slog.Warn("failed to persist v1_project_id to app settings", "error", err,
				"app_project_id", appProject.ID, "v1_project_id", existing.ID)
		}
		return nil
	}

	pkHex, err := utils.RandomHex(5)
	if err != nil {
		return fmt.Errorf("failed to generate package name: %w", err)
	}
	pkName := "com.wemob.app" + pkHex[0:5]

	project := models.Project{
		UserID:      user.ID,
		URL:         deployedURL,
		PackageName: pkName,
		IconPath:    appProject.LogoS3Key,
	}

	// Create real OneSignal application
	onesignalAppID, err := external.CreateOneSignalApplication(project,
		c.SecretManager.GetSecret("ONESIGNAL_ORG_ID"),
		c.SecretManager.GetSecret("ONESIGNAL_ORG_API_KEY"),
		c.SecretManager.GetSecret("FCM_V1_SERVICE_ACCOUNT_JSON_B64"))
	if err != nil {
		return fmt.Errorf("OneSignal app creation failed: %w", err)
	}
	project.OneSignalAppID = onesignalAppID

	time.Sleep(2 * time.Second)

	onesignalAppToken, err := external.CreateOneSignalAppToken(project, c.SecretManager.GetSecret("ONESIGNAL_ORG_API_KEY"))
	if err != nil {
		return fmt.Errorf("OneSignal token creation failed: %w", err)
	}
	project.OneSignalAppToken = onesignalAppToken

	created, err := c.DB.CreateProject(&project)
	if err != nil {
		return fmt.Errorf("failed to create v1 project: %w", err)
	}

	if err := c.DB.SetAppProjectV1ProjectID(appProject.ID.String(), created.ID.String()); err != nil {
		slog.Warn("failed to persist v1_project_id to app settings", "error", err,
			"app_project_id", appProject.ID, "v1_project_id", created.ID)
	}

	slog.Info("Created v1 Project for mobile app",
		"app_project_id", appProject.ID,
		"project_id", created.ID,
		"url", deployedURL,
	)

	return nil
}

// BackfillV1ProjectsForMobileApps finds mobile_app AppProjects that have been
// deployed to web but don't have a corresponding v1 Project, and creates one
// for each. Intended to be called once at startup in a goroutine.
func (c *Controller) BackfillV1ProjectsForMobileApps() {
	// Find mobile_app AppProjects with a web deployment but no v1 Project for that URL.
	// We join app_deployments to get the deployed URL and left-join projects to check absence.
	type backfillRow struct {
		AppProjectID string `bun:"app_project_id"`
		UserID       string `bun:"user_id"`
		DeployedURL  string `bun:"deployed_url"`
		Name         string `bun:"name"`
		LogoS3Key    string `bun:"logo_s3_key"`
	}

	var rows []backfillRow
	err := c.DB.DB.NewRaw(`
		SELECT DISTINCT ON (ap.id)
			ap.id AS app_project_id,
			ap.user_id,
			ad.deployed_url,
			ap.name,
			ap.logo_s3_key
		FROM app_projects ap
		JOIN app_deployments ad ON ad.app_project_id = ap.id AND ad.platform = 'web' AND ad.deployed_url != ''
		LEFT JOIN projects p ON p.user_id = ap.user_id AND p.url = ad.deployed_url AND p.deleted_at IS NULL
		WHERE ap.project_type = 'mobile_app'
			AND ap.deleted_at IS NULL
			AND p.id IS NULL
		ORDER BY ap.id, ad.created_at DESC
	`).Scan(context.Background(), &rows)
	if err != nil {
		slog.Error("BackfillV1ProjectsForMobileApps: query failed", "error", err)
		return
	}

	if len(rows) == 0 {
		slog.Info("BackfillV1ProjectsForMobileApps: no mobile apps need backfill")
		return
	}

	slog.Info("BackfillV1ProjectsForMobileApps: starting", "count", len(rows))

	for _, row := range rows {
		// Note: shell of an existing AppProject (ID parsed from row) — not persisted;
		// passed to ensureV1ProjectForMobileApp which only creates a v1 Project.
		appProject := &models.AppProject{
			Name:      row.Name,
			LogoS3Key: row.LogoS3Key,
		}
		appProject.ID = uuid.MustParse(row.AppProjectID)

		user := &models.User{}
		user.ID = uuid.MustParse(row.UserID)

		if err := c.ensureV1ProjectForMobileApp(appProject, user, row.DeployedURL); err != nil {
			slog.Error("BackfillV1ProjectsForMobileApps: failed for app",
				"app_project_id", row.AppProjectID,
				"error", err,
			)
			continue
		}
	}

	slog.Info("BackfillV1ProjectsForMobileApps: done", "processed", len(rows))
}

// BackendDeployURL returns the URL where the project's backend is (or will
// be) deployed. Used by BuildExpoMobile to bake the right EXPO_PUBLIC_API_URL
// into the mobile binary at build time, and by the README scaffold so docs
// stay in sync with reality.
//
// Resolution order:
//  1. RuntimeApp.CustomDomain (set on first deploy; equals {slug}.wemob.app
//     by default but extensible for future user-bring-your-own-domain support)
//  2. Canonical {slug}.{WEMOB_APP_BASE_DOMAIN} fallback (used for first-build
//     before any deploy lands the runtime app row)
//
// Always returns an https:// scheme. Returns "" only when project is nil.
func (c *Controller) BackendDeployURL(project *models.AppProject) string {
	if project == nil {
		return ""
	}
	if c.DB != nil {
		if ra, err := c.DB.GetRuntimeAppByAppProjectID(project.ID.String()); err == nil && ra != nil && ra.CustomDomain != "" {
			return "https://" + ra.CustomDomain
		}
	}
	return "https://" + project.Slug + "." + constants.WEMOB_APP_BASE_DOMAIN
}

// WebPreviewURL returns the URL where the project's Expo Web preview is
// served. P1.3 locked architecture: same Fly app as the Next.js backend,
// with the web export bundled into the Next.js public/_web/ directory at
// deploy time (see web_deploy_controller.go for the bake step — a follow-up
// in P1.3.B). The studio's MobilePreview pane points an iframe at this URL.
//
// Builds atop BackendDeployURL so a custom domain on RuntimeApp transparently
// flows through. Returns "" only when project is nil — the dashboard treats
// "" as "render the empty state".
func (c *Controller) WebPreviewURL(project *models.AppProject) string {
	base := c.BackendDeployURL(project)
	if base == "" {
		return ""
	}
	return base + "/_web/"
}

// replaceWithRealOneSignal replaces fake placeholder OneSignal IDs on a v1
// Project with real ones from the OneSignal API. Mutates the project in place
// (caller is responsible for persisting via UpdateProject).
func (c *Controller) replaceWithRealOneSignal(project *models.Project) error {
	onesignalAppID, err := external.CreateOneSignalApplication(*project,
		c.SecretManager.GetSecret("ONESIGNAL_ORG_ID"),
		c.SecretManager.GetSecret("ONESIGNAL_ORG_API_KEY"),
		c.SecretManager.GetSecret("FCM_V1_SERVICE_ACCOUNT_JSON_B64"))
	if err != nil {
		return fmt.Errorf("OneSignal app creation failed: %w", err)
	}
	project.OneSignalAppID = onesignalAppID

	time.Sleep(2 * time.Second)

	onesignalAppToken, err := external.CreateOneSignalAppToken(*project, c.SecretManager.GetSecret("ONESIGNAL_ORG_API_KEY"))
	if err != nil {
		return fmt.Errorf("OneSignal token creation failed: %w", err)
	}
	project.OneSignalAppToken = onesignalAppToken

	slog.Info("Replaced fake OneSignal IDs with real ones", "project_id", project.ID)
	return nil
}

