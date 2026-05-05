package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/posthog/posthog-go"
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
	_, err := c.DB.DB.NewUpdate().
		Model((*models.AppDeployment)(nil)).
		Set("status = ?", "failed").
		Set("error_message = ?", reason).
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
		buildErr, syncErr error
		wg                sync.WaitGroup
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		imageRef, _, buildErr = c.FlyService.BuildAndPush(flyAppName, sourceDir, p.DeploymentID, envVars)
	}()
	go func() {
		defer wg.Done()
		syncErr = c.FlyService.SyncSecrets(flyAppName, envVars)
	}()
	wg.Wait()
	// Build failure dominates: without the image we can't deploy machines, and
	// retrying handles secrets idempotently on the next attempt.
	if buildErr != nil {
		return c.maybeRetry(ctx, p.DeploymentID, "build", buildErr)
	}
	if syncErr != nil {
		return c.maybeRetry(ctx, p.DeploymentID, "sync_secrets", syncErr)
	}

	// 7. Deploy machines per plan tier (region from FlyService config).
	spec := fly.PlanMachineSpec(limits, c.FlyService.Region)
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
	if err := c.FlyService.WaitForHealthy(flyAppName, 5*time.Minute); err != nil {
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
	go c.TrackUserEvent(user, "app_deployed_web", posthog.NewProperties().
		Set("app_id", project.ID.String()).
		Set("deploy_version", deployment.Version))
	slog.Info("deploy_succeeded", "deployment_id", p.DeploymentID, "fly_app", flyAppName)
	return nil
}

// failTask marks deployment failed and returns asynq.SkipRetry for non-retryable errors.
func (c *Controller) failTask(deploymentID, stage string, err error) error {
	slog.Error("deploy_failed",
		"deployment_id", deploymentID, "stage", stage,
		"error", err, "retryable", false)
	if mErr := c.MarkDeploymentFailed(deploymentID, fmt.Sprintf("%s: %v", stage, err)); mErr != nil {
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
	retryable := fly.IsRetryable(err)
	finalAttempt := isFinalRetryAttempt(ctx)
	slog.Error("deploy_failed",
		"deployment_id", deploymentID, "stage", stage,
		"error", err, "retryable", retryable, "final_attempt", finalAttempt)
	if retryable && !finalAttempt {
		return err
	}
	if mErr := c.MarkDeploymentFailed(deploymentID, fmt.Sprintf("%s: %v", stage, err)); mErr != nil {
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
	go c.TrackUserEvent(user, "app_deploy_failed", posthog.NewProperties().
		Set("app_id", deployment.AppProjectID.String()).
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
				go c.TrackUserEvent(user, "app_deployed_web", posthog.NewProperties().
					Set("app_id", deployment.AppProjectID.String()).
					Set("deploy_version", deployment.Version))
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
