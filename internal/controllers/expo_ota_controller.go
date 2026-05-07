package controllers

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/codemagic"
	"github.com/knid/wemob/internal/tasks"
	"github.com/knid/wemob/internal/utils"
)

// otaDeployPlatform is the AppDeployment.Platform value used for self-hosted
// expo-updates publishes. Distinct from "web"/"android"/"ios" so the partial
// unique index on (app_project_id, platform) doesn't conflict with a parallel
// web deploy.
const otaDeployPlatform = "ota"

// expoOTABuildTimeout caps the worker's expo-export pipeline. npm install +
// expo export typically lands in 60-90s on a warm node_modules cache; the
// 10-minute budget covers a cold install from scratch.
const expoOTABuildTimeout = 10 * time.Minute

// expoOTAExportSemaphore caps in-flight `expo export` runs to one across
// the worker process. Each export pegs a CPU core + ~600 MB RSS, so the
// 1 vCPU / 2 GB host can only handle one at a time — running two saturates
// the box, which in turn starves asynq's heartbeat to Redis, expires the
// task lease, and causes asynq's recoverer to re-dispatch a task whose
// original handler goroutine is still running. That spawned a duplicate
// `expo export` for the same repo on 2026-05-04, driving load avg to 20+.
//
// Holding this semaphore from npm-install through the publish loop means
// the second handler invocation blocks until the first finishes, then
// proceeds (or hits asynq's per-task Timeout while waiting and exits
// cleanly) — never a parallel export.
var expoOTAExportSemaphore = make(chan struct{}, 1)

// CreateQueuedExpoOTADeployment writes an AppDeployment row in 'queued' state
// for an expo-updates publish. The partial unique index gives concurrent-POST
// safety; isUniqueViolation maps it to ErrDeployInProgress.
func (c *Controller) CreateQueuedExpoOTADeployment(project *models.AppProject, user *models.User) (*models.AppDeployment, error) {
	deployment := &models.AppDeployment{
		AppProjectID: project.ID,
		UserID:       user.ID,
		Platform:     otaDeployPlatform,
		Provider:     "expo-updates",
		Status:       "queued",
		Version:      1,
	}
	if latest, err := c.DB.GetLatestAppDeployment(project.ID.String(), otaDeployPlatform); err == nil && latest != nil {
		deployment.Version = latest.Version + 1
	}
	deployment, err := c.DB.CreateAppDeployment(deployment)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrDeployInProgress
		}
		return nil, err
	}

	actorID := user.ID
	if err := database.AppendDeploymentEvent(context.Background(), c.DB.DB, deployment.ID,
		models.DeploymentEventSubmitted, models.DeploymentEventActorUser, &actorID,
		map[string]any{"method": "expo-updates"}); err != nil {
		slog.Warn("append deployment_events.submitted failed", "err", err)
	}
	return deployment, nil
}

// EnqueueExpoOTAPublish submits an expo:ota_publish task to the worker on
// the matching plan-tier queue, isolated from web deploys so a 10-minute
// expo export burst can't starve Fly deploys (or vice versa).
func (c *Controller) EnqueueExpoOTAPublish(user *models.User, orgID uuid.UUID, deploymentID uuid.UUID, isPaidPlan bool) (string, error) {
	if err := c.ChargeBuildCredits(user, orgID, constants.BuildCreditKindOTA, deploymentID.String()); err != nil {
		return "", fmt.Errorf("insufficient credits for OTA publish: %w", err)
	}
	queue := "expo-ota-free"
	if isPaidPlan {
		queue = "expo-ota-pro"
	}
	payload, err := json.Marshal(tasks.ExpoOTAPublishPayload{DeploymentID: deploymentID.String()})
	if err != nil {
		return "", fmt.Errorf("marshal payload: %w", err)
	}
	task := asynq.NewTask(tasks.TypeExpoOTAPublish, payload)
	_, err = c.TaskService.Enqueue(task,
		asynq.Queue(queue),
		asynq.MaxRetry(2),
		asynq.Timeout(expoOTABuildTimeout+time.Minute),
		asynq.TaskID(deploymentID.String()),
	)
	if err != nil {
		return queue, fmt.Errorf("enqueue: %w", err)
	}
	return queue, nil
}

// HandleExpoOTAPublishTask is the asynq handler for expo:ota_publish.
//
// Pipeline:
//  1. Open the user's repo via GitRepoService.
//  2. cd mobile/, install deps if node_modules is missing.
//  3. npx expo export --platform all → mobile/dist/.
//  4. Read app.json to extract runtimeVersion + the expo block.
//  5. Tar dist/ in memory.
//  6. Call PublishOTAUpdate for ios + android (same dist tarball).
//  7. Mark the deployment 'deployed'.
//
// Idempotent across retries: PublishOTAUpdate derives the manifest UpdateID
// from the bundle/asset hashes, so a re-run that produces the same output
// reuses the same row on the client side.
func (c *Controller) HandleExpoOTAPublishTask(ctx context.Context, t *asynq.Task) error {
	var p tasks.ExpoOTAPublishPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("unmarshal payload: %w", err)
	}

	deployment, err := c.DB.GetAppDeploymentByID(p.DeploymentID)
	if err != nil {
		slog.Error("ota_publish_failed", "deployment_id", p.DeploymentID, "stage", "load_deployment", "error", err)
		return asynq.SkipRetry
	}
	project, err := c.DB.GetAppProjectByID(deployment.AppProjectID.String())
	if err != nil {
		return c.maybeRetryOTA(ctx, p.DeploymentID, "load_project", err, true)
	}
	if project.ProjectType != models.ProjectTypeMobileAppNative {
		return c.failTask(p.DeploymentID, "wrong_project_type",
			fmt.Errorf("expo OTA publish requires mobile_app_native, got %q", project.ProjectType))
	}

	if err := c.MarkDeploymentBuilding(p.DeploymentID); err != nil {
		slog.Warn("mark building failed", "err", err)
	}

	repo, err := c.GitRepoService.Open(project.ID.String())
	if err != nil {
		return c.maybeRetryOTA(ctx, p.DeploymentID, "git_open", err, true)
	}
	mobileDir := filepath.Join(repo.Path, "mobile")
	if info, statErr := os.Stat(mobileDir); statErr != nil || !info.IsDir() {
		return c.failTask(p.DeploymentID, "missing_mobile_subtree",
			fmt.Errorf("mobile_app_native project has no mobile/ subtree at %s", mobileDir))
	}

	expoConfigBytes, err := readExpoAppJSON(mobileDir)
	if err != nil {
		return c.failTask(p.DeploymentID, "read_app_json", err)
	}

	buildCtx, cancel := context.WithTimeout(ctx, expoOTABuildTimeout)
	defer cancel()

	// Block until we own the single export slot, or the task times out /
	// asynq cancels us. The wait is bounded by buildCtx, which is the
	// asynq.Timeout from EnqueueExpoOTAPublish.
	select {
	case expoOTAExportSemaphore <- struct{}{}:
		defer func() { <-expoOTAExportSemaphore }()
	case <-buildCtx.Done():
		return c.maybeRetryOTA(ctx, p.DeploymentID, "wait_export_slot", buildCtx.Err(), true)
	}

	if err := ensureNodeModules(buildCtx, mobileDir); err != nil {
		// npm registry blips are common — retry once before giving up.
		return c.maybeRetryOTA(ctx, p.DeploymentID, "npm_install", err, true)
	}

	// Materialize the per-project signing cert + keyid into the mobile/ tree.
	// app.json declares expo.updates.codeSigningCertificate = "./certs/ota-cert.pem";
	// without the file on disk, `expo prebuild` throws "File not found at
	// updates.codeSigningCertificate path" before any platform work starts.
	// Codemagic builds get the same file via POST /v2/codemagic/ota-cert (HMAC);
	// the worker, running in-process, calls EnsureOTAUpdateSigningKey directly.
	if _, err := c.prepareOTAArtifactsInRepo(buildCtx, project, mobileDir); err != nil {
		return c.maybeRetryOTA(ctx, p.DeploymentID, "prepare_ota_artifacts", err, true)
	}

	// Per-platform prebuild + export + fingerprint. Mirrors what Codemagic does
	// at binary-build time so the runtime version we publish under matches what
	// the installed binary will send in `expo-runtime-version`:
	//   1. expo prebuild --platform <p> --no-install --clean → generates ios/ or android/
	//   2. expo export --platform <p> → writes the JS bundle + assets to dist-<p>/
	//   3. @expo/fingerprint fingerprint:generate --platform <p> → walks the
	//      prebuilt project tree and prints {hash, sources} JSON. The bare hex
	//      `hash` is the runtime version. (expo export does NOT embed this in
	//      metadata.json — verified empirically; only `version`, `bundler`,
	//      `fileMetadata` ship there.)
	// Without prebuild, @expo/fingerprint sees a different project state
	// (no native dirs) and produces a hash that doesn't match the binary's,
	// silently breaking OTA delivery.
	timestamp := time.Now().UnixMilli()
	publishedVersions := make(map[string]string, 2)
	for _, platform := range []string{"ios", "android"} {
		if err := runExpoPrebuild(buildCtx, mobileDir, platform); err != nil {
			return c.failTask(p.DeploymentID, "expo_prebuild_"+platform, err)
		}
		distDir, err := runExpoExport(buildCtx, mobileDir, platform)
		if err != nil {
			// expo export almost always fails on user code (TS/JS errors,
			// missing deps). Retrying won't fix that — surface immediately.
			return c.failTask(p.DeploymentID, "expo_export_"+platform, err)
		}
		rv, err := resolveRuntimeVersion(buildCtx, mobileDir, platform, expoConfigBytes)
		if err != nil {
			return c.failTask(p.DeploymentID, "resolve_runtime_version_"+platform, err)
		}
		distTarGz, err := tarDirGzip(distDir)
		if err != nil {
			return c.maybeRetryOTA(ctx, p.DeploymentID, "tar_dist_"+platform, err, true)
		}
		if _, err := c.PublishOTAUpdate(ctx, project.ID, rv, platform, timestamp, distTarGz, expoConfigBytes); err != nil {
			return c.maybeRetryOTA(ctx, p.DeploymentID, "publish_"+platform, err, true)
		}
		publishedVersions[platform] = rv
	}

	manifestURL := fmt.Sprintf("/v2/apps/%s/ota/manifest", project.ID.String())
	if err := c.MarkDeploymentSucceeded(p.DeploymentID, manifestURL); err != nil {
		slog.Warn("mark deployed failed", "err", err)
	}
	slog.Info("ota_publish_succeeded",
		"deployment_id", p.DeploymentID,
		"app_project_id", project.ID.String(),
		"runtime_version_ios", publishedVersions["ios"],
		"runtime_version_android", publishedVersions["android"])

	if user, err := c.DB.GetUserById(deployment.UserID.String()); err == nil {
		go c.EventService.SendEvent(user, constants.EVENT_OTA_PUBLISH_SUCCEEDED, map[string]any{
			"app_id":                  project.ID.String(),
			"runtime_version_ios":     publishedVersions["ios"],
			"runtime_version_android": publishedVersions["android"],
			"version":                 deployment.Version,
		})
	}
	return nil
}

// readExpoAppJSON reads mobile/app.json and returns the raw expo block
// JSON. The block is passed to PublishOTAUpdate as expoConfigJSON so the
// manifest's `extra.expoClient` mirrors the project's actual config.
//
// Runtime version resolution is deferred to resolveRuntimeVersion because
// the `fingerprint` policy requires a project-tree hash that only
// @expo/fingerprint can compute correctly.
func readExpoAppJSON(mobileDir string) ([]byte, error) {
	raw, err := os.ReadFile(filepath.Join(mobileDir, "app.json"))
	if err != nil {
		return nil, fmt.Errorf("read app.json: %w", err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, fmt.Errorf("parse app.json: %w", err)
	}
	expoRaw, ok := top["expo"]
	if !ok {
		return nil, fmt.Errorf("app.json has no expo block")
	}
	return expoRaw, nil
}

// resolveRuntimeVersion returns the runtime version that the installed binary
// will send in its `expo-runtime-version` header for `platform`.
//
// Verified against Expo's own source:
//   - resolveRuntimeVersionAsync.ts returns `fingerprint.hash` directly
//     (bare hex hash, NO "fingerprint:" prefix)
//   - expo-updates Android FileDownloader.kt sends
//     `header("Expo-Runtime-Version", configuration.runtimeVersionRaw)`
//     as-is with no transformation
//   - expo's reference custom-expo-updates-server reads the header as an
//     opaque string with no prefix parsing
//
// Resolution:
//   - Literal "runtimeVersion": "1.0.0" → return as-is.
//   - {"policy": "fingerprint"} → shell out to @expo/fingerprint
//     fingerprint:generate, parse {hash, sources} JSON, return hash.
//
// `expo export` does NOT embed the fingerprint hash in dist/metadata.json
// (verified empirically — only `version`, `bundler`, `fileMetadata` ship
// there). @expo/fingerprint is the source of truth, and Codemagic's binary
// build runs the same CLI, so the hashes line up.
func resolveRuntimeVersion(ctx context.Context, mobileDir, platform string, expoConfigBytes []byte) (string, error) {
	literal, policy, err := parseRuntimeVersionShape(expoConfigBytes)
	if err != nil {
		return "", err
	}
	switch {
	case literal != "":
		return literal, nil
	case policy == "fingerprint":
		return runFingerprintGenerate(ctx, mobileDir, platform)
	case policy == "":
		return "", fmt.Errorf("expo.runtimeVersion is missing in app.json")
	default:
		return "", fmt.Errorf("unsupported runtimeVersion policy %q (expected \"fingerprint\" or a literal string)", policy)
	}
}

// parseRuntimeVersionShape inspects the expo block and returns either a
// literal string or a policy name. Returns ("", "", nil) when the field is
// absent.
func parseRuntimeVersionShape(expoConfigBytes []byte) (literal, policy string, err error) {
	var expo struct {
		RuntimeVersion json.RawMessage `json:"runtimeVersion"`
	}
	if err := json.Unmarshal(expoConfigBytes, &expo); err != nil {
		return "", "", fmt.Errorf("parse expo block: %w", err)
	}
	if len(expo.RuntimeVersion) == 0 {
		return "", "", nil
	}
	var asString string
	if err := json.Unmarshal(expo.RuntimeVersion, &asString); err == nil {
		return asString, "", nil
	}
	var asObj struct {
		Policy string `json:"policy"`
	}
	if err := json.Unmarshal(expo.RuntimeVersion, &asObj); err == nil {
		return "", asObj.Policy, nil
	}
	return "", "", fmt.Errorf("unrecognized runtimeVersion shape in app.json")
}

// runFingerprintGenerate invokes `npx @expo/fingerprint fingerprint:generate
// --platform <p>` in mobileDir and returns the bare hex `hash` from its JSON
// output. Must run AFTER expo prebuild so the native dirs (ios/ or android/)
// exist — without them @expo/fingerprint sees a different project tree than
// the binary did at build time and produces a non-matching hash.
func runFingerprintGenerate(ctx context.Context, mobileDir, platform string) (string, error) {
	cmd := exec.CommandContext(ctx, "npx", "--no-install",
		"@expo/fingerprint", "fingerprint:generate", "--platform", platform)
	cmd.Dir = mobileDir
	cmd.Env = append(os.Environ(), "CI=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("@expo/fingerprint fingerprint:generate %s: %w (stderr: %s)",
			platform, err, truncateOutput(stderr.Bytes()))
	}
	var fp struct {
		Hash string `json:"hash"`
	}
	if err := json.Unmarshal(out, &fp); err != nil {
		return "", fmt.Errorf("parse fingerprint output: %w (output: %s)", err, truncateOutput(out))
	}
	if fp.Hash == "" {
		return "", fmt.Errorf("@expo/fingerprint returned empty hash (output: %s)", truncateOutput(out))
	}
	return fp.Hash, nil
}

// ensureNodeModules runs `npm install` in mobileDir if node_modules is
// missing. Uses npm ci when package-lock.json is present for reproducibility.
func ensureNodeModules(ctx context.Context, mobileDir string) error {
	if info, err := os.Stat(filepath.Join(mobileDir, "node_modules")); err == nil && info.IsDir() {
		return nil
	}
	args := []string{"install", "--no-audit", "--no-fund", "--prefer-offline"}
	if _, err := os.Stat(filepath.Join(mobileDir, "package-lock.json")); err == nil {
		args = []string{"ci", "--no-audit", "--no-fund", "--prefer-offline"}
	}
	cmd := exec.CommandContext(ctx, "npm", args...)
	cmd.Dir = mobileDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("npm %s: %w (output: %s)", strings.Join(args, " "), err, truncateOutput(out))
	}
	return nil
}

// runExpoPrebuild runs `npx expo prebuild --platform <p> --no-install
// --clean` in mobileDir. Generates the per-platform native folder
// (ios/ or android/) deterministically from app.json + plugins. Mirrors
// the corresponding step in Codemagic's build job — running it here
// ensures @expo/fingerprint sees the SAME project tree at our OTA-export
// time as it does at Codemagic's build-time export, so the resulting
// fingerprintHash values match.
func runExpoPrebuild(ctx context.Context, mobileDir, platform string) error {
	cmd := exec.CommandContext(ctx, "npx", "--yes", "expo", "prebuild",
		"--platform", platform, "--no-install", "--clean")
	cmd.Dir = mobileDir
	cmd.Env = append(os.Environ(), "CI=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("expo prebuild %s: %w (output: %s)", platform, err, truncateOutput(out))
	}
	return nil
}

// runExpoExport runs `npx expo export --platform <p>` in mobileDir and
// returns the absolute path to the produced per-platform dist directory.
// Per-platform output dirs (dist-ios / dist-android) so the caller can
// publish each independently with its own fingerprint hash.
func runExpoExport(ctx context.Context, mobileDir, platform string) (string, error) {
	outName := "dist-" + platform
	distDir := filepath.Join(mobileDir, outName)
	_ = os.RemoveAll(distDir)
	cmd := exec.CommandContext(ctx, "npx", "--yes", "expo", "export", "--platform", platform, "--output-dir", outName)
	cmd.Dir = mobileDir
	cmd.Env = append(os.Environ(), "CI=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("expo export %s: %w (output: %s)", platform, err, truncateOutput(out))
	}
	if _, statErr := os.Stat(filepath.Join(distDir, "metadata.json")); statErr != nil {
		return "", fmt.Errorf("expo export %s produced no metadata.json (output: %s)", platform, truncateOutput(out))
	}
	return distDir, nil
}

// tarDirGzip walks dir and writes a gzipped tarball where every entry's
// path is relative to dir (no leading "dist/" prefix). Matches what
// PublishOTAUpdate expects.
func tarDirGzip(dir string) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	walkErr := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		if _, err := io.Copy(tw, f); err != nil {
			return err
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// maybeRetryOTA returns the original error (asynq backs off + re-enters the
// handler) when the stage is retryable AND retry budget remains; otherwise
// marks the deployment failed and returns asynq.SkipRetry. Mirrors the
// pattern used by HandleWebDeployTask.maybeRetry but takes an explicit
// retryable bool because OTA failure modes are stage-shaped, not error-typed.
func (c *Controller) maybeRetryOTA(ctx context.Context, deploymentID, stage string, err error, retryable bool) error {
	finalAttempt := isFinalRetryAttempt(ctx)
	slog.Error("ota_publish_failed",
		"deployment_id", deploymentID, "stage", stage,
		"error", err, "retryable", retryable, "final_attempt", finalAttempt)
	if retryable && !finalAttempt {
		return err
	}
	if mErr := c.MarkDeploymentFailed(deploymentID, fmt.Sprintf("%s: %v", stage, err)); mErr != nil {
		slog.Error("mark_failed_db_error", "deployment_id", deploymentID, "stage", stage, "err", mErr)
	}
	return asynq.SkipRetry
}

func truncateOutput(b []byte) string {
	const max = 4 * 1024
	if len(b) <= max {
		return string(b)
	}
	return string(b[len(b)-max:])
}

// ErrQuickPublishWrongType is returned by TriggerCodemagicOTAPublish when the
// project isn't a mobile_app_native project. Mapped to HTTP 409 by the view.
var ErrQuickPublishWrongType = errors.New("quick publish requires a mobile_app_native project")

// TriggerCodemagicOTAPublish dispatches the expo-ota-publish Codemagic
// workflow (P1 #12 mobile audit, 2026-05-04). Used by the studio "Quick
// publish" button — Codemagic runs `expo export` and uploads the bundle to
// /v2/apps/{id}/ota/publish.
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

	// AppProjectID is the new direct FK target; legacy ProjectID column is
	// left NULL on Expo mobile builds (v1 shadow Project has been removed).
	build := &models.Build{
		AppProjectID:      &project.ID,
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

	task, err := c.DB.CreateAppBuildTask(*build, &project.ID, "OTA", "Queued", false)
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

	_, triggerErr := c.CodemagicService.TriggerExpoOTAPublish(ctx, codemagic.ExpoOTAPublishParams{
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

	slog.Info("ota_quick_publish_triggered",
		"project_id", project.ID,
		"build_id", build.ID,
		"task_id", task.ID,
	)
	return build, task, nil
}
