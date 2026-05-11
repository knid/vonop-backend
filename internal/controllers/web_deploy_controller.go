package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/fly"
	"github.com/knid/wemob/internal/tasks"
)

// ErrDeployInProgress is returned by CreateQueuedWebDeployment when the
// partial unique index on (app_project_id, platform) WHERE non-terminal
// rejects the insert because another deploy is already enqueued or running.
var ErrDeployInProgress = errors.New("deploy already in progress for this project")

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	type pgError interface{ SQLState() string }
	var pe pgError
	if errors.As(err, &pe) {
		return pe.SQLState() == "23505"
	}
	return strings.Contains(err.Error(), "23505") || strings.Contains(err.Error(), "duplicate key")
}

// CreateQueuedWebDeployment writes a deployment row in 'queued' state. The
// worker will flip it through 'building' → 'deploying' → 'deployed'/'failed'.
//
// Fires preWarmFlyApp asynchronously: by the time the asynq worker picks up
// the task and finishes its git/scaffold prep, the Fly app + IPs already
// exist, so the worker's EnsureApp call inside processWebDeployTask becomes
// a fast no-op.
func (c *Controller) CreateQueuedWebDeployment(project *models.AppProject, user *models.User) (*models.AppDeployment, error) {
	deployment := &models.AppDeployment{
		AppProjectID: project.ID,
		UserID:       user.ID,
		Platform:     "web",
		Provider:     "fly",
		Status:       "queued",
		Version:      1,
	}
	if latest, err := c.DB.GetLatestAppDeployment(project.ID.String(), "web"); err == nil && latest != nil {
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
		map[string]any{"method": "fly"}); err != nil {
		slog.Warn("append deployment_events.submitted failed", "err", err)
	}

	go c.preWarmFlyApp(project)

	return deployment, nil
}

// preWarmFlyApp creates the Fly app + allocates IPs eagerly so the deploy
// worker's EnsureApp call lands on an already-provisioned app (~3-7s saved
// off first-deploy wall clock). Idempotent on the Fly side, safe to race
// against the worker's own EnsureApp call. Best-effort: any error here
// just means the worker pays the original cost.
func (c *Controller) preWarmFlyApp(project *models.AppProject) {
	if c.FlyService == nil || project == nil {
		return
	}
	flyAppName := "wemob-" + project.Slug
	if err := c.FlyService.EnsureApp(flyAppName); err != nil {
		slog.Warn("preWarmFlyApp failed (worker will retry inline)",
			"fly_app", flyAppName, "project_id", project.ID, "err", err)
	}
}

// EnqueueWebDeploy submits a deploy:web task to the Asynq queue tier matching
// the user's plan. Returns the queue name so the caller can include it in the
// 202 response body for client-side observability.
func (c *Controller) EnqueueWebDeploy(deploymentID uuid.UUID, isPaidPlan bool) (queue string, err error) {
	queue = "deploy-web-free"
	if isPaidPlan {
		queue = "deploy-web-pro"
	}
	payload, err := json.Marshal(tasks.WebDeployPayload{DeploymentID: deploymentID.String()})
	if err != nil {
		return "", fmt.Errorf("marshal payload: %w", err)
	}
	task := asynq.NewTask(tasks.TypeDeployWeb, payload)
	_, err = c.TaskService.Enqueue(task,
		asynq.Queue(queue),
		asynq.MaxRetry(4),
		asynq.Timeout(15*time.Minute),
		asynq.TaskID(deploymentID.String()),
	)
	if err != nil {
		return queue, fmt.Errorf("enqueue: %w", err)
	}
	return queue, nil
}

// MarkDeploymentBuilding flips a deployment row from 'queued' to 'building'.
func (c *Controller) MarkDeploymentBuilding(deploymentID string) error {
	_, err := c.DB.DB.NewUpdate().
		Model((*models.AppDeployment)(nil)).
		Set("status = ?", "building").
		Set("updated_at = ?", time.Now()).
		Where("id = ?", deploymentID).
		Exec(context.Background())
	return err
}

// MarkDeploymentDeploying flips status to 'deploying' once the image is built
// and Machines are being launched.
func (c *Controller) MarkDeploymentDeploying(deploymentID string) error {
	_, err := c.DB.DB.NewUpdate().
		Model((*models.AppDeployment)(nil)).
		Set("status = ?", "deploying").
		Set("updated_at = ?", time.Now()).
		Where("id = ?", deploymentID).
		Exec(context.Background())
	return err
}

// MarkDeploymentSucceeded is called when the Fly webhook reports a healthy
// machine. Also writes deployed_url for the client's response.
// Status is set to "deployed" — the terminal-success state used by all quota
// checks, billing queries, admin views, and the preview-code controller.
func (c *Controller) MarkDeploymentSucceeded(deploymentID string, deployedURL string) error {
	_, err := c.DB.DB.NewUpdate().
		Model((*models.AppDeployment)(nil)).
		Set("status = ?", "deployed").
		Set("deployed_url = ?", deployedURL).
		Set("updated_at = ?", time.Now()).
		Where("id = ?", deploymentID).
		Exec(context.Background())
	return err
}

// MarkDeploymentFailed records the reason in the deployment row so the UI can
// show a useful error.
func (c *Controller) MarkDeploymentFailed(deploymentID string, reason string) error {
	return c.MarkDeploymentFailedWithDetails(deploymentID, reason, nil)
}

// MarkDeploymentFailedWithDetails records the failure reason and stores
// structured error details in ProviderData so the agent's read_deploy_logs tool
// can surface build logs, stage info, etc.
func (c *Controller) MarkDeploymentFailedWithDetails(deploymentID string, reason string, details map[string]any) error {
	pd := map[string]any{}
	if len(details) > 0 {
		pd["error"] = details
	}
	_, err := c.DB.DB.NewUpdate().
		Model((*models.AppDeployment)(nil)).
		Set("status = ?", "failed").
		Set("error_message = ?", reason).
		Set("provider_data = ?", pd).
		Set("updated_at = ?", time.Now()).
		Where("id = ?", deploymentID).
		Exec(context.Background())
	return err
}

// HandleWebDeployTask is the Asynq handler for deploy:web tasks. Idempotent:
// every Fly call is safe to re-run, so a retry after a partial failure picks
// up from where it left off.
func (c *Controller) HandleWebDeployTask(ctx context.Context, t *asynq.Task) error {
	var p tasks.WebDeployPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("unmarshal payload: %w", err)
	}

	deployment, err := c.DB.GetAppDeploymentByID(p.DeploymentID)
	if err != nil {
		// Cannot MarkDeploymentFailed without the row. Log + skip retry.
		slog.Error("deploy_failed", "deployment_id", p.DeploymentID, "stage", "load_deployment", "error", err)
		return asynq.SkipRetry
	}
	project, err := c.DB.GetAppProjectByID(deployment.AppProjectID.String())
	if err != nil {
		return c.failTask(p.DeploymentID, "load_project", err)
	}
	user, err := c.DB.GetUserById(deployment.UserID.String())
	if err != nil {
		return c.failTask(p.DeploymentID, "load_user", err)
	}

	if err := c.MarkDeploymentBuilding(p.DeploymentID); err != nil {
		slog.Warn("mark building failed", "err", err)
	}

	// 1. Pull source from Gitea, inject analytics + branding, push back.
	repo, err := c.GitRepoService.Open(project.ID.String())
	if err != nil {
		slog.Warn("deploy_retryable", "deployment_id", p.DeploymentID, "stage", "gitea_open", "error", err)
		return fmt.Errorf("gitea_open: %w", err)
	}
	repoFiles, err := repo.ReadAllFiles()
	if err != nil || len(repoFiles) == 0 {
		if err == nil {
			// Empty repo is permanent — no files to deploy.
			return c.failTask(p.DeploymentID, "gitea_read_empty", fmt.Errorf("repo has no files"))
		}
		slog.Warn("deploy_retryable", "deployment_id", p.DeploymentID, "stage", "gitea_read", "error", err)
		return fmt.Errorf("gitea_read: %w", err)
	}
	fileMap := make(map[string]string, len(repoFiles))
	for k, v := range repoFiles {
		fileMap[k] = v
	}
	injectAnalyticsScript(fileMap, project.ID.String())
	var limits *models.StandardUserLimits
	if project.OrganizationID != uuid.Nil {
		limits, _ = c.DB.GetLimitsForOrg(ctx, project.OrganizationID)
	}
	if limits == nil {
		// Legacy fallback for unmigrated projects (OrganizationID == nil).
		limits, _ = c.GetUserLimits(user)
	}
	if limits == nil || !limits.CanRemoveBrandLogo {
		injectWemobBadge(fileMap)
	}
	for path, content := range fileMap {
		if repoFiles[path] != content {
			if writeErr := repo.WriteFile(path, []byte(content)); writeErr != nil {
				slog.Warn("write file during deploy", "path", path, "err", writeErr)
			}
		}
	}
	if _, commitErr := repo.Commit("deploy: inject analytics and branding"); commitErr != nil {
		slog.Warn("commit branding changes", "err", commitErr)
	}

	// 2. Use the repo working tree as the Fly builder source. mobile_app_native
	// projects have a backend/ + mobile/ split at the repo root; deploy only
	// the backend/ subtree (Next.js API routes). Other project types deploy
	// the whole repo.
	sourceDir := repo.Path
	if project.ProjectType == models.ProjectTypeMobileAppNative {
		sourceDir = filepath.Join(repo.Path, "backend")
		if info, statErr := os.Stat(sourceDir); statErr != nil || !info.IsDir() {
			return c.failTask(p.DeploymentID, "missing_backend_subtree",
				fmt.Errorf("mobile_app_native project has no backend/ subtree at %s — agent must scaffold it before deploying", sourceDir))
		}
		// Defensive: ensure a Dockerfile exists. If the agent didn't generate
		// one, write a sensible Next.js default. The AI can override it on
		// the next turn by writing its own backend/Dockerfile.
		if err := ensureNextjsDockerfile(sourceDir); err != nil {
			return c.failTask(p.DeploymentID, "ensure_dockerfile", err)
		}
	}
	// Defensive: backfill a root layout when the agent shipped a page.tsx
	// without one. Next.js fails `next build` with "page.tsx doesn't have a
	// root layout" — runs for ALL project types since web/ecommerce templates
	// also occasionally have agent-generated pages outside the templated tree.
	if err := ensureNextjsRootLayout(sourceDir); err != nil {
		return c.failTask(p.DeploymentID, "ensure_root_layout", err)
	}

	// 3. Get/create the runtime_apps row.
	flyAppName := "wemob-" + project.Slug
	runtimeApp, err := c.DB.GetRuntimeAppByAppProjectID(project.ID.String())
	if err != nil {
		runtimeApp, err = c.DB.CreateRuntimeApp(&models.RuntimeApp{
			AppProjectID:  project.ID,
			GiteaRepoName: project.Slug,
			FlyAppName:    flyAppName,
			Framework:     "nextjs",
			CustomDomain:  project.Slug + "." + constants.WEMOB_APP_BASE_DOMAIN,
		})
		if err != nil {
			return c.failTask(p.DeploymentID, "create_runtime_app", err)
		}
	}

	// 4. Collect env vars (DB read + lazy Better-Auth secret generation).
	// Includes Better-Auth env vars when MobileAppConfig.AuthEnabled is true;
	// CollectBetterAuthEnvVars also lazily generates + persists the per-customer
	// BETTER_AUTH_SECRET on first call (envelope-encrypted via CredStore).
	envVars := c.CollectAllEnvVars(project)
	authVars, err := c.CollectBetterAuthEnvVars(ctx, project)
	if err != nil {
		return c.maybeRetry(ctx, p.DeploymentID, "collect_auth_env", err)
	}
	for k, v := range authVars {
		envVars[k] = v
	}

	// 5. Ensure the Fly app exists. Required gate before BuildAndPush (which
	// pushes to registry.fly.io/<appName>) and SyncSecrets (which targets the
	// app's secret namespace).
	if err := c.FlyService.EnsureApp(flyAppName); err != nil {
		return c.maybeRetry(ctx, p.DeploymentID, "ensure_app", err)
	}

	// 6. Build + push image AND sync secrets in parallel. Both depend on
	// EnsureApp (step 5) but are otherwise independent: BuildAndPush takes
	// 60–180s on Fly's remote builder, SyncSecrets takes 3–10s
	// (one POST per secret); fanning them out makes secrets effectively
	// free relative to the build wall-clock.
	//
	// envVars is forwarded as --build-arg so Next.js inlines NEXT_PUBLIC_*
	// into the bundle at `next build` time (Fly secrets reach the process at
	// runtime, which is too late for the build). All vars are passed; Docker
	// silently ignores --build-arg values whose keys aren't declared as ARG
	// in the Dockerfile.
	var (
		imageRef          string
		buildLog          string
		buildErr, syncErr error
		wg                sync.WaitGroup
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		imageRef, buildLog, buildErr = c.FlyService.BuildAndPush(flyAppName, sourceDir, p.DeploymentID, envVars)
	}()
	go func() {
		defer wg.Done()
		syncErr = c.FlyService.SyncSecrets(flyAppName, envVars)
	}()
	wg.Wait()
	// Build failure dominates: without the image we can't deploy machines, and
	// retrying handles secrets idempotently on the next attempt.
	if buildErr != nil {
		return c.maybeRetryWithDetails(ctx, p.DeploymentID, "build", buildErr, map[string]any{
			"build_log": buildLog,
		})
	}
	if syncErr != nil {
		return c.maybeRetry(ctx, p.DeploymentID, "sync_secrets", syncErr)
	}

	// 7. Deploy machines per plan tier. Region precedence (highest → lowest):
	//   1. host-init-bug remediation override (per-attempt, persisted in
	//      deployment_events)
	//   2. project.Region (Pro+ region selection, gated on CanChooseRegion)
	//   3. FlyService.Region (FLY_PRIMARY_REGION)
	spec := fly.PlanMachineSpec(limits, c.resolveDeployRegion(ctx, p.DeploymentID, project, limits))
	spec.Image = imageRef
	if err := c.FlyService.DeployMachines(flyAppName, spec); err != nil {
		return c.maybeRetry(ctx, p.DeploymentID, "deploy_machines", err)
	}

	// 8. Persist runtime_apps state and flip deployment to 'deploying'.
	runtimeApp.CurrentImageRef = imageRef
	runtimeApp.LastDeployedAt = time.Now()
	if _, err := c.DB.UpdateRuntimeApp(runtimeApp); err != nil {
		slog.Warn("update runtime_app", "err", err)
	}
	if err := c.MarkDeploymentDeploying(p.DeploymentID); err != nil {
		slog.Warn("mark deploying", "err", err)
	}

	// 9. Wait for the machine to report healthy. Fly doesn't publish
	// machine-state webhooks to own-org apps, so we poll inline here.
	// 5min covers a cold-start build + machine launch comfortably.
	//
	// If the wait fails AND the failure looks like the Fly host-init bug
	// (machine never reaches "started" before flyd's restart cap), this
	// destroys the stuck machines and redeploys once in a fallback region.
	// Other failures (app crash, real timeout) bubble up unchanged.
	if err := c.waitForHealthyWithRemediation(ctx, p.DeploymentID, flyAppName, spec, 5*time.Minute); err != nil {
		return c.maybeRetry(ctx, p.DeploymentID, "wait_healthy", err)
	}

	// 10. Flip to 'deployed' and fire success events.
	deployedURL := "https://" + runtimeApp.CustomDomain
	if err := c.MarkDeploymentSucceeded(p.DeploymentID, deployedURL); err != nil {
		slog.Warn("mark succeeded", "err", err)
	}
	go c.EventService.SendEvent(user, constants.EVENT_WEB_DEPLOYMENT_SUCCEEDED, map[string]any{
		"app_id":       project.ID.String(),
		"version":      deployment.Version,
		"deployed_url": deployedURL,
	})
	go c.TrackUserEvent(user, constants.EVENT_WEB_DEPLOYMENT_SUCCEEDED,
		c.AppEventProps(project.ID.String()).
			Set("deploy_version", deployment.Version).
			Set("deployed_url", deployedURL))
	slog.Info("deploy_succeeded", "deployment_id", p.DeploymentID, "fly_app", flyAppName)
	return nil
}

// resolveDeployRegion picks the region for this deploy attempt. Precedence:
//   1. host-init-bug remediation override (per-attempt, persisted in
//      deployment_events) — re-running the known-bad primary would burn
//      another 5min WaitForHealthy budget.
//   2. project.Region (E6 region selection), gated on limits.CanChooseRegion.
//   3. FlyService.Region (FLY_PRIMARY_REGION fallback).
func (c *Controller) resolveDeployRegion(ctx context.Context, deploymentID string, project *models.AppProject, limits *models.StandardUserLimits) string {
	if id, err := uuid.Parse(deploymentID); err == nil {
		if meta, err := database.LatestEventMetadata(ctx, c.DB.DB, id, models.DeploymentEventFlyHostRemediation); err == nil && meta != nil {
			if to, ok := meta["to_region"].(string); ok && to != "" {
				slog.Info("deploy_region_overridden_by_prior_remediation",
					"deployment_id", deploymentID, "region", to, "primary", c.FlyService.Region)
				return to
			}
		}
	}
	// E6: per-app region preference (Pro+ only). Silently ignored on plans
	// without CanChooseRegion so a downgrade doesn't break the next deploy
	// — the app falls back to the primary region instead.
	if project != nil && project.Region != "" && limits != nil && limits.CanChooseRegion {
		return project.Region
	}
	return c.FlyService.Region
}

// waitForHealthyWithRemediation wraps Fly's WaitForHealthy with a one-shot
// auto-recovery for the Fly host-init bug — a Fly platform-side failure where
// init kernel-panics during net-namespace setup, before the app's container
// runs. The signature is unmistakable (machine never reaches "started" across
// 10 restart cycles), so we can safely auto-redeploy in a sibling region
// without risking masking real app bugs.
//
// Budget: at most one fallback. If the fallback region also fails, that's
// either a Fly-wide incident or a real app bug — either way, bubble up so
// the operator sees it and the asynq retry budget kicks in normally.
//
// State: the remediation event is persisted to deployment_events so an asynq
// retry that re-enters this handler picks up the fallback region directly
// (see resolveDeployRegion), instead of re-trying the known-bad primary.
func (c *Controller) waitForHealthyWithRemediation(
	ctx context.Context, deploymentID, appName string, spec fly.MachineSpec, timeout time.Duration,
) error {
	waitErr := c.FlyService.WaitForHealthy(appName, timeout)
	if waitErr == nil {
		return nil
	}

	diag, dErr := c.FlyService.DiagnoseDeploymentFailure(appName)
	if dErr != nil {
		slog.Warn("fly_diagnose_failed", "deployment_id", deploymentID, "app", appName, "err", dErr)
		return waitErr
	}
	if diag.Kind != fly.FailureHostInitBug {
		// App crash or unknown — not auto-remediable. Caller's maybeRetry
		// path handles asynq retries normally.
		return waitErr
	}

	// Already in a remediated region? Then the fallback also failed — bail
	// before cycling regions further.
	if spec.Region != c.FlyService.Region {
		slog.Error("fly_host_init_bug_persisted_across_regions",
			"deployment_id", deploymentID, "app", appName,
			"primary", c.FlyService.Region, "fallback", spec.Region,
			"detail", diag.Detail)
		return fmt.Errorf("fly host init bug persisted in fallback region %s: %w", spec.Region, waitErr)
	}

	fallback := fly.FallbackRegion(spec.Region)
	slog.Warn("fly_host_init_bug_detected_remediating",
		"deployment_id", deploymentID, "app", appName,
		"from_region", spec.Region, "to_region", fallback,
		"machine_id", diag.MachineID, "restart_count", diag.RestartCount,
		"exit_count", diag.ExitCount, "detail", diag.Detail)

	if id, err := uuid.Parse(deploymentID); err == nil {
		_ = database.AppendDeploymentEvent(ctx, c.DB.DB, id,
			models.DeploymentEventFlyHostRemediation,
			models.DeploymentEventActorSystem, nil,
			map[string]any{
				"kind":          "host_init_bug",
				"from_region":   spec.Region,
				"to_region":     fallback,
				"machine_id":    diag.MachineID,
				"restart_count": diag.RestartCount,
				"exit_count":    diag.ExitCount,
				"detail":        diag.Detail,
			})
	}

	if err := c.FlyService.DestroyAllMachines(appName); err != nil {
		return fmt.Errorf("destroy_stuck_machines: %w", err)
	}
	spec.Region = fallback
	if err := c.FlyService.DeployMachines(appName, spec); err != nil {
		return fmt.Errorf("redeploy_alt_region: %w", err)
	}
	return c.FlyService.WaitForHealthy(appName, timeout)
}

// failTask marks deployment failed and returns asynq.SkipRetry for non-retryable errors.
func (c *Controller) failTask(deploymentID, stage string, err error) error {
	return c.failTaskWithDetails(deploymentID, stage, err, nil)
}

// failTaskWithDetails is like failTask but stores additional structured error
// details in the deployment row's ProviderData so the agent's read_deploy_logs
// tool can surface rich context (e.g. flyctl build output) to the model.
func (c *Controller) failTaskWithDetails(deploymentID, stage string, err error, details map[string]any) error {
	slog.Error("deploy_failed",
		"deployment_id", deploymentID, "stage", stage,
		"error", err, "retryable", false)
	if mErr := c.MarkDeploymentFailedWithDetails(deploymentID, fmt.Sprintf("%s: %v", stage, err), details); mErr != nil {
		slog.Error("mark_failed_db_error", "deployment_id", deploymentID, "stage", stage, "err", mErr)
	}
	c.fireDeployFailedEvent(deploymentID, stage, err)
	return asynq.SkipRetry
}

// maybeRetry returns the error if it's retryable AND retry budget remains
// (asynq backs off and re-enters the handler). Otherwise — non-retryable, or
// retryable but on the final attempt — marks the deployment failed in the DB
// and returns asynq.SkipRetry. Without the final-attempt check, exhausting the
// retry budget would archive the task in asynq with the row still stuck in
// 'building' forever.
func (c *Controller) maybeRetry(ctx context.Context, deploymentID, stage string, err error) error {
	return c.maybeRetryWithDetails(ctx, deploymentID, stage, err, nil)
}

// maybeRetryWithDetails is like maybeRetry but stores additional structured
// error details on the final failure mark.
func (c *Controller) maybeRetryWithDetails(ctx context.Context, deploymentID, stage string, err error, details map[string]any) error {
	retryable := fly.IsRetryable(err)
	finalAttempt := isFinalRetryAttempt(ctx)
	slog.Error("deploy_failed",
		"deployment_id", deploymentID, "stage", stage,
		"error", err, "retryable", retryable, "final_attempt", finalAttempt)
	if retryable && !finalAttempt {
		return err
	}
	if mErr := c.MarkDeploymentFailedWithDetails(deploymentID, fmt.Sprintf("%s: %v", stage, err), details); mErr != nil {
		slog.Error("mark_failed_db_error", "deployment_id", deploymentID, "stage", stage, "err", mErr)
	}
	c.fireDeployFailedEvent(deploymentID, stage, err)
	return asynq.SkipRetry
}

// isFinalRetryAttempt returns true when the current handler invocation is the
// last one asynq will make for this task — any error returned now will archive
// the task. Returns false when invoked outside an asynq handler (ctx has no
// retry metadata), so the caller treats it like a non-final attempt.
func isFinalRetryAttempt(ctx context.Context) bool {
	retried, ok1 := asynq.GetRetryCount(ctx)
	max, ok2 := asynq.GetMaxRetry(ctx)
	if !ok1 || !ok2 {
		return false
	}
	return retried >= max
}

// fireDeployFailedEvent fires the lifecycle + PostHog events for a permanent
// deploy failure. Best-effort — never blocks the worker.
func (c *Controller) fireDeployFailedEvent(deploymentID, stage string, deployErr error) {
	deployment, err := c.DB.GetAppDeploymentByID(deploymentID)
	if err != nil {
		return
	}
	user, err := c.DB.GetUserById(deployment.UserID.String())
	if err != nil {
		return
	}
	go c.EventService.SendEvent(user, constants.EVENT_WEB_DEPLOYMENT_FAILED, map[string]any{
		"app_id":        deployment.AppProjectID.String(),
		"stage":         stage,
		"error_message": deployErr.Error(),
	})
	go c.TrackUserEvent(user, constants.EVENT_WEB_DEPLOYMENT_FAILED,
		c.AppEventProps(deployment.AppProjectID.String()).
			Set("stage", stage).
			Set("error", deployErr.Error()))
}

// ReapStuckDeploymentsTask is invoked every minute by the asynq scheduler. It
// flips any deployment row that's been sitting in an in-progress state for
// longer than the stale threshold to 'failed', so the UI doesn't lock the user
// out of redeploying after a worker crash or asynq retry-budget exhaustion.
//
// stuckThreshold needs to comfortably exceed the longest legitimate stage
// duration. Today the worst case is BuildAndPush (Fly remote builder, ~5–10
// min for a Next.js app) plus WaitForHealthy (5 min, hard cap). 20 min keeps
// well clear of that while still failing fast enough to feel responsive.
const stuckDeploymentThreshold = 20 * time.Minute

func (c *Controller) ReapStuckDeploymentsTask(ctx context.Context) error {
	n, err := c.DB.ReapStuckAppDeployments(ctx, stuckDeploymentThreshold,
		fmt.Sprintf("worker disappeared (no progress > %s)", stuckDeploymentThreshold))
	if err != nil {
		return err
	}
	if n > 0 {
		slog.Warn("reaped_stuck_deployments", "count", n, "stale_after", stuckDeploymentThreshold)
	}
	return nil
}

// MarkDeploymentFailedIfNotTerminal flips the deployment to 'failed' only when
// it's still in an in-progress state. Used as a backstop from the asynq
// ErrorHandler — by the time we get there the in-handler logic may have
// already written the final state, so we don't want to overwrite a clean
// 'failed' / 'deployed' with a generic timeout reason.
func (c *Controller) MarkDeploymentFailedIfNotTerminal(deploymentID, reason string) error {
	d, err := c.DB.GetAppDeploymentByID(deploymentID)
	if err != nil || d == nil {
		return err
	}
	if d.Status == "deployed" || d.Status == "failed" {
		return nil
	}
	return c.MarkDeploymentFailed(deploymentID, reason)
}

// HandleFlyMachineEvent reacts to a Fly webhook. Updates app_deployments
// based on machine state. Also fires lifecycle events for the success path
// (failure events fire from the worker's failTask/maybeRetry path).
func (c *Controller) HandleFlyMachineEvent(event fly.MachineEvent) error {
	runtimeApp, err := c.DB.GetRuntimeAppByFlyAppName(event.AppName)
	if err != nil {
		return fmt.Errorf("unknown app: %s", event.AppName)
	}

	deployment, err := c.DB.GetLatestAppDeployment(runtimeApp.AppProjectID.String(), "web")
	if err != nil || deployment == nil {
		return fmt.Errorf("no deployment for app %s", event.AppName)
	}

	switch event.State {
	case "started":
		// Only flip in-progress states → succeeded; never override an already-final state.
		if deployment.Status == "queued" || deployment.Status == "building" || deployment.Status == "deploying" {
			deployedURL := "https://" + runtimeApp.CustomDomain
			if err := c.MarkDeploymentSucceeded(deployment.ID.String(), deployedURL); err != nil {
				slog.Warn("mark succeeded failed", "deployment", deployment.ID, "err", err)
			}
			slog.Info("deploy_succeeded", "app", event.AppName, "deployment_id", deployment.ID.String())

			// Fire success events for the user — best-effort, don't block.
			user, uErr := c.DB.GetUserById(deployment.UserID.String())
			if uErr == nil {
				go c.EventService.SendEvent(user, constants.EVENT_WEB_DEPLOYMENT_SUCCEEDED, map[string]any{
					"app_id":       deployment.AppProjectID.String(),
					"version":      deployment.Version,
					"deployed_url": deployedURL,
				})
				go c.TrackUserEvent(user, constants.EVENT_WEB_DEPLOYMENT_SUCCEEDED,
					c.AppEventProps(deployment.AppProjectID.String()).
						Set("deploy_version", deployment.Version).
						Set("deployed_url", deployedURL))
			}
		}
	case "failed", "destroyed":
		if deployment.Status != "deployed" && deployment.Status != "failed" {
			if err := c.MarkDeploymentFailed(deployment.ID.String(), event.Message); err != nil {
				slog.Warn("mark failed failed", "deployment", deployment.ID, "err", err)
			}
			slog.Warn("deploy_failed_via_webhook", "app", event.AppName, "msg", event.Message)
		}
	}
	return nil
}

// ensureNextjsRootLayout writes a minimal app/layout.tsx when the project
// has any page.tsx under the App Router but no accompanying layout — Next.js
// fails `next build` with "page.tsx doesn't have a root layout" otherwise.
//
// Idempotent: returns immediately if a layout exists in any common form
// (.tsx/.jsx/.ts/.js) under either app/ or src/app/. Looks for pages with a
// shallow walk so route-grouped pages (app/(marketing)/page.tsx) also count.
//
// The default content is intentionally minimal: a valid <html><body> shell,
// a `metadata` export, and no external imports. The agent can overwrite it
// on the next turn — this is a safety net, not a style decision.
func ensureNextjsRootLayout(sourceDir string) error {
	for _, prefix := range []string{"app", "src/app"} {
		appDir := filepath.Join(sourceDir, prefix)
		info, err := os.Stat(appDir)
		if err != nil || !info.IsDir() {
			continue
		}
		for _, ext := range []string{"layout.tsx", "layout.jsx", "layout.ts", "layout.js"} {
			if _, err := os.Stat(filepath.Join(appDir, ext)); err == nil {
				return nil
			}
		}
		hasPage := false
		walkErr := filepath.WalkDir(appDir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "node_modules" || strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			switch d.Name() {
			case "page.tsx", "page.jsx", "page.ts", "page.js":
				hasPage = true
				return filepath.SkipAll
			}
			return nil
		})
		if walkErr != nil {
			return fmt.Errorf("walk %s: %w", appDir, walkErr)
		}
		if !hasPage {
			return nil
		}
		layoutPath := filepath.Join(appDir, "layout.tsx")
		if err := os.WriteFile(layoutPath, []byte(defaultNextjsRootLayout), 0o644); err != nil {
			return fmt.Errorf("write default root layout: %w", err)
		}
		slog.Warn("backfilled missing root layout", "path", layoutPath)
		return nil
	}
	return nil
}

const defaultNextjsRootLayout = `import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "App",
  description: "",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
`

// ensureNextjsDockerfile writes a default standalone-output Next.js Dockerfile
// into sourceDir if one doesn't already exist. Idempotent — pre-existing
// Dockerfiles (whether AI-generated or human-written) are left untouched so
// users can override the default by writing their own backend/Dockerfile.
//
// The default mirrors the standalone-output Dockerfile from the official
// Next.js examples (https://github.com/vercel/next.js/blob/canary/examples/with-docker/Dockerfile)
// so it works with `next build` + `output: 'standalone'` in next.config.js.
func ensureNextjsDockerfile(sourceDir string) error {
	dockerPath := filepath.Join(sourceDir, "Dockerfile")
	if _, err := os.Stat(dockerPath); err == nil {
		return nil // already exists
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat Dockerfile: %w", err)
	}
	if err := os.WriteFile(dockerPath, []byte(defaultNextjsDockerfile), 0o644); err != nil {
		return fmt.Errorf("write default Dockerfile: %w", err)
	}
	return nil
}

// defaultNextjsDockerfile is the standalone-output Next.js Dockerfile we
// write into backend/ when the agent didn't generate one. Multi-stage build
// (deps → builder → runner). Final image is ~150MB on alpine.
const defaultNextjsDockerfile = `# syntax=docker/dockerfile:1.7
# WEMOB_BASE_IMAGE points at the pre-warmed wemob-base-nextjs image whose
# /root/.npm is pre-populated with seed deps. Override via --build-arg if the
# base image is unavailable: --build-arg WEMOB_BASE_IMAGE=node:22-alpine
ARG WEMOB_BASE_IMAGE=registry.fly.io/wemob-base-nextjs:v1

FROM ${WEMOB_BASE_IMAGE} AS deps
WORKDIR /app
COPY package.json package-lock.json* ./
# --mount=type=cache persists the npm cache across builds on the same BuildKit
# builder VM (seeded from the base image's /root/.npm on first hit).
# --ignore-scripts skips package postinstall hooks (standard Next.js deps
# don't need them). --legacy-peer-deps preempts ERESOLVE failures when an
# agent-added dep (better-auth) conflicts with a template-pinned peer
# (drizzle-kit ^0.28.0 vs better-auth's drizzle-kit >=0.31.4 requirement).
RUN --mount=type=cache,target=/root/.npm \
    npm install --no-audit --no-fund --prefer-offline --ignore-scripts --legacy-peer-deps

FROM node:22-alpine AS builder
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY . .
RUN mkdir -p ./public

ARG NEXT_PUBLIC_SUPABASE_URL
ARG NEXT_PUBLIC_SUPABASE_ANON_KEY
ARG NEXT_PUBLIC_FIREBASE_API_KEY
ARG NEXT_PUBLIC_FIREBASE_AUTH_DOMAIN
ARG NEXT_PUBLIC_FIREBASE_PROJECT_ID
ARG NEXT_PUBLIC_FIREBASE_STORAGE_BUCKET
ARG NEXT_PUBLIC_FIREBASE_MESSAGING_SENDER_ID
ARG NEXT_PUBLIC_FIREBASE_APP_ID
ARG NEXT_PUBLIC_STRIPE_PK
ARG NEXT_PUBLIC_CLOUDINARY_CLOUD_NAME
ARG NEXT_PUBLIC_CLOUDINARY_UPLOAD_PRESET
ARG NEXT_PUBLIC_GOOGLE_SHEET_ID
ARG NEXT_PUBLIC_WEMOB_GATEWAY_URL

ENV NEXT_PUBLIC_SUPABASE_URL=$NEXT_PUBLIC_SUPABASE_URL \
    NEXT_PUBLIC_SUPABASE_ANON_KEY=$NEXT_PUBLIC_SUPABASE_ANON_KEY \
    NEXT_PUBLIC_FIREBASE_API_KEY=$NEXT_PUBLIC_FIREBASE_API_KEY \
    NEXT_PUBLIC_FIREBASE_AUTH_DOMAIN=$NEXT_PUBLIC_FIREBASE_AUTH_DOMAIN \
    NEXT_PUBLIC_FIREBASE_PROJECT_ID=$NEXT_PUBLIC_FIREBASE_PROJECT_ID \
    NEXT_PUBLIC_FIREBASE_STORAGE_BUCKET=$NEXT_PUBLIC_FIREBASE_STORAGE_BUCKET \
    NEXT_PUBLIC_FIREBASE_MESSAGING_SENDER_ID=$NEXT_PUBLIC_FIREBASE_MESSAGING_SENDER_ID \
    NEXT_PUBLIC_FIREBASE_APP_ID=$NEXT_PUBLIC_FIREBASE_APP_ID \
    NEXT_PUBLIC_STRIPE_PK=$NEXT_PUBLIC_STRIPE_PK \
    NEXT_PUBLIC_CLOUDINARY_CLOUD_NAME=$NEXT_PUBLIC_CLOUDINARY_CLOUD_NAME \
    NEXT_PUBLIC_CLOUDINARY_UPLOAD_PRESET=$NEXT_PUBLIC_CLOUDINARY_UPLOAD_PRESET \
    NEXT_PUBLIC_GOOGLE_SHEET_ID=$NEXT_PUBLIC_GOOGLE_SHEET_ID \
    NEXT_PUBLIC_WEMOB_GATEWAY_URL=$NEXT_PUBLIC_WEMOB_GATEWAY_URL \
    NEXT_TELEMETRY_DISABLED=1

# .next/cache is Next.js's incremental build cache; persisting it across
# builds on the same BuildKit builder VM is the largest single Dockerfile
# win for repeat deploys.
RUN --mount=type=cache,target=/app/.next/cache \
    npm run build

FROM node:22-alpine AS runner
WORKDIR /app
ENV NODE_ENV=production \
    NEXT_TELEMETRY_DISABLED=1 \
    PORT=3000 \
    HOSTNAME=0.0.0.0
RUN addgroup -g 1001 -S nodejs && adduser -S nextjs -u 1001
COPY --from=builder --chown=nextjs:nodejs /app/.next/standalone ./
COPY --from=builder --chown=nextjs:nodejs /app/.next/static ./.next/static
COPY --from=builder --chown=nextjs:nodejs /app/public ./public
USER nextjs
EXPOSE 3000
CMD ["node", "server.js"]
`
