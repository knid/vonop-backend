package controllers

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
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
	"github.com/knid/wemob/internal/tasks"
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
func (c *Controller) EnqueueExpoOTAPublish(deploymentID uuid.UUID, isPaidPlan bool) (string, error) {
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

	// Per-platform prebuild + export. Mirrors Codemagic's pipeline:
	//   1. expo prebuild --platform <p> --no-install --clean → generates ios/ or android/
	//   2. expo export --platform <p> → writes dist-<p>/metadata.json with fingerprintHash
	//   3. publish keyed on "fingerprint:<hash>" so the row matches what the
	//      installed binary sends in expo-runtime-version (the binary's value
	//      is baked in by the SAME prebuild + export at Codemagic build time).
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
		rv, err := readRuntimeVersionFromMetadata(distDir, expoConfigBytes)
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
// Runtime version resolution is deferred to resolveRuntimeVersionViaExpoCLI
// because the `fingerprint` policy requires a project-tree hash that only
// expo's own CLI can compute correctly.
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

// readRuntimeVersionFromMetadata returns the runtime version that the
// installed binary will send in its `expo-runtime-version` header.
//
// Verified against Expo's own source:
//  - resolveRuntimeVersionAsync.ts returns `fingerprint.hash` directly
//    (bare hex hash, NO "fingerprint:" prefix)
//  - expo-updates Android FileDownloader.kt sends
//    `header("Expo-Runtime-Version", configuration.runtimeVersionRaw)`
//    as-is with no transformation
//  - expo's reference custom-expo-updates-server reads the header as an
//    opaque string with no prefix parsing
//
// So we publish keyed on the bare hash. (Codemagic's existing yaml
// adds a "fingerprint:" prefix — that's a bug; needs to be fixed there
// too. Until both sides drop the prefix, a Codemagic-published OTA
// would never match a binary's request, which is why ota_updates was
// empty in production.)
//
// Resolution order:
//  1. dist/metadata.json fingerprintHash  (policy=fingerprint, the template default)
//  2. expo block's runtimeVersion as a literal string fallback (policy="1.0.0" style)
func readRuntimeVersionFromMetadata(distDir string, expoConfigBytes []byte) (string, error) {
	metaBytes, err := os.ReadFile(filepath.Join(distDir, "metadata.json"))
	if err != nil {
		return "", fmt.Errorf("read metadata.json: %w", err)
	}
	var meta struct {
		FingerprintHash string `json:"fingerprintHash"`
	}
	if err := json.Unmarshal(metaBytes, &meta); err == nil && meta.FingerprintHash != "" {
		return meta.FingerprintHash, nil
	}
	var expo struct {
		RuntimeVersion json.RawMessage `json:"runtimeVersion"`
	}
	if err := json.Unmarshal(expoConfigBytes, &expo); err == nil {
		var literal string
		if err := json.Unmarshal(expo.RuntimeVersion, &literal); err == nil && literal != "" {
			return literal, nil
		}
	}
	return "", fmt.Errorf("no fingerprintHash in metadata.json and no literal expo.runtimeVersion")
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
