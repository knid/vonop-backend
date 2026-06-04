package controllers

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/agent"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/fal"
	"github.com/knid/wemob/internal/utils"
	"github.com/posthog/posthog-go"
)

// AgentDeps builds a per-run RunDependencies bundle for the agent package.
// Credit lifecycle callbacks are bound to the specific user at call time.
// This method is called once per HTTP chat/generate request from the view layer.
func (c *Controller) AgentDeps(user *models.User, jobID string) *agent.RunDependencies {
	provider := c.llmProviderForUser(user)
	_, planName, _ := c.GetUserLimitsWithPlan(user)

	return &agent.RunDependencies{
		DB:            c.DB,
		GitRepo:       c.GitRepoService,
		AssetsStorage: c.AssetsStorage,
		SecretManager: c.SecretManager,
		NeonService:   c.NeonService,
		LLMProvider:   provider,
		PostHog:       c.PostHog,
		RemoteConfig:  c.RemoteConfig,
		UserPlanName:  planName,
		PlanNameForProject: func(projectID string) string {
			orgID, err := c.ResolveOrgForProjectID(user, projectID)
			if err != nil {
				slog.Error("agent_deps: failed to resolve project org for plan", "error", err, "user_id", user.ID, "project_id", projectID)
				return planName
			}
			_, projectPlanName, err := c.GetOrgLimitsWithPlan(orgID.String())
			if err != nil {
				slog.Error("agent_deps: failed to resolve project org plan", "error", err, "user_id", user.ID, "project_id", projectID, "org_id", orgID)
				return planName
			}
			return projectPlanName
		},

		CreditChecker: func(projectID string, tracker *utils.TokenTracker) func() bool {
			// Budget is org-scoped; resolve the org that owns the project so the
			// pre-check and the final deduction use the same balance.
			orgID, err := c.ResolveOrgForProjectID(user, projectID)
			if err != nil {
				slog.Error("agent_deps: failed to resolve project org for credit checker", "error", err, "user_id", user.ID, "project_id", projectID)
				return func() bool { return false }
			}
			checker := c.MakeCreditBudgetChecker(user, orgID, tracker)
			if checker == nil {
				slog.Error("agent_deps: failed to create credit checker", "user_id", user.ID, "project_id", projectID, "org_id", orgID)
				return func() bool { return false }
			}
			return checker
		},
		DeductCredits: func(ctx context.Context, credits int, action, projectID string) error {
			// Resolve the project's organization so the deduction lands on the
			// owning org's balance.
			orgID, err := c.ResolveOrgForProjectID(user, projectID)
			if err != nil {
				return err
			}
			// Pull the user's first message off the in-flight TokenTracker (if any)
			// so the ai_credits_deducted event carries a prompt_preview. Other
			// deduction call sites (dashboard_chat, navbar_autofill, …) don't
			// have a prompt in scope, so this enrichment stays agent-only.
			var meta map[string]any
			if t := utils.TokenTrackerFromContext(ctx); t != nil && t.TraceInput != "" {
				meta = map[string]any{"prompt_preview": utils.TruncatePromptPreview(t.TraceInput, 50)}
			}
			return c.DeductAICreditsToFloor(user, orgID, credits, action, projectID, meta)
		},
		TrackUserEvent: func(event constants.EventName, props map[string]any) {
			p := posthog.NewProperties()
			for k, v := range props {
				p.Set(k, v)
			}
			c.TrackUserEvent(user, event, p)
		},
		SendResendEvent: func(event constants.EventName, attrs map[string]any) {
			go c.EventService.SendEvent(user, event, attrs)
		},
		GenerateImage: c.generateImageForAgent,
		NeonConnString: func(projectID string) (string, error) {
			return c.neonConnString(projectID), nil
		},
		ProvisionDatabase: func(ctx context.Context, project *models.AppProject, tables []models.DataModel) error {
			return c.provisionDatabaseForAgent(ctx, project, user, tables)
		},
		EnableAI:            c.enableProjectAIForAgent,
		EnableLocalDatabase: c.enableProjectLocalDatabaseForAgent,
		AutoDeployBackend: func(ctx context.Context, project *models.AppProject) error {
			return c.autoDeployBackendForAgent(ctx, project, user)
		},
		GatewayAllowedModels: func() []string {
			return c.GatewayAllowedModels(user)
		},
		// <P1#17> trigger_build_after_fix — bound here so the agent tool
		// closes the read-fix-rebuild loop without poking BuildExpoMobile
		// directly. Non-owner / non-mobile callers are rejected upstream
		// at the tool layer (rc.Project + rc.User checks).
		TriggerMobileBuild: func(ctx context.Context, project *models.AppProject, user *models.User, platforms []string) (string, map[string]string, error) {
			return c.triggerMobileBuildForAgent(ctx, project, user, platforms)
		},
		// trigger_web_deploy — bound here so the agent can close the
		// read-fix-redeploy loop for web deploy failures.
		TriggerWebDeploy: func(ctx context.Context, project *models.AppProject, user *models.User) (string, error) {
			return c.triggerWebDeployForAgent(ctx, project, user)
		},
		RecordLLMUsage: func(tracker *utils.TokenTracker, action, model, provider, reference, appType string) {
			c.RecordTokenUsage(user, action, model, provider, reference, appType, tracker)
		},
		RecordLLMError: func(tracker *utils.TokenTracker, action, model, provider, reference, appType string, latencyMs int64, errCode, errMsg string) {
			c.RecordLLMError(user, tracker, action, model, provider, reference, appType, latencyMs, errCode, errMsg)
		},
		JobID:                 jobID,
		ListMCPTools:          c.AgentMCPTools,
		CreateMCPToolApproval: c.CreateMCPToolApproval,
		WaitForMCPApproval:    c.WaitForMCPApproval,
		ExecuteMCPTool:        c.ExecuteMCPTool,
	}
}

// ResolveOrgForProjectID returns the OrganizationID owning the given project.
// Falls back to the user's personal org if the project lookup fails or has no
// org set (defensive — every project should have an org after Phase 7).
func (c *Controller) ResolveOrgForProjectID(user *models.User, projectID string) (uuid.UUID, error) {
	if projectID != "" {
		if project, err := c.DB.GetAppProjectByID(projectID); err == nil && project != nil && project.OrganizationID != uuid.Nil {
			return project.OrganizationID, nil
		}
	}
	return c.RequireOrgIDForUser(user, nil)
}

// generateImageForAgent calls Fal to generate an image from a prompt, downloads
// the result, and uploads the bytes to the Assets S3 bucket under the given
// project's images folder. If the S3 upload fails after retries (or the download
// fails), it falls back to returning the temporary Fal CDN URL — it's not
// ideal long-term but the image remains usable for the current session.
//
// Width, height, and style are accepted to satisfy the agent's ImageGenerator
// interface but are not currently passed to the Fal wrapper; see the underlying
// `fal.GenerateImage` for the parameters that actually flow through today.
func (c *Controller) generateImageForAgent(ctx context.Context, prompt string, w, h int, style, projectID string) (string, error) {
	_ = w
	_ = h
	_ = style

	apiKey := c.SecretManager.GetSecret("FAL_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("FAL_API_KEY not configured")
	}
	falModel := c.SecretManager.GetSecret("FAL_IMAGE_MODEL")
	if falModel == "" {
		falModel = "fal-ai/flux/schnell"
	}

	cdnURL, err := fal.GenerateImage(ctx, apiKey, falModel, prompt, "")
	if err != nil {
		return "", fmt.Errorf("fal generate: %w", err)
	}

	imgData, err := downloadImage(ctx, cdnURL)
	if err != nil {
		// Fall back to the CDN URL. It's temporary but works for this session.
		slog.Warn("failed to download generated image, returning CDN URL", "error", err, "project_id", projectID)
		return cdnURL, nil
	}

	filename := fmt.Sprintf("generated-%d.jpg", time.Now().UnixNano())
	assetPath := fmt.Sprintf("images/%s", filename)
	s3Key := fmt.Sprintf("apps/%s/%s", projectID, assetPath)

	// Detached context for the S3 upload so the image isn't lost if the
	// caller's context is near its deadline.
	uploadCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var uploadErr error
	for attempt := 0; attempt < 3; attempt++ {
		uploadErr = c.AssetsStorage.UploadFileByteWithContentType(uploadCtx, s3Key, imgData, "image/jpeg")
		if uploadErr == nil {
			break
		}
		slog.Warn("S3 upload failed for generated image, retrying",
			"project_id", projectID, "s3_key", s3Key,
			"attempt", attempt+1, "error", uploadErr)
		time.Sleep(time.Duration(attempt+1) * time.Second)
	}
	if uploadErr != nil {
		return cdnURL, nil
	}
	// The assets bucket is private — return the /v2/apps/{id}/file/* proxy URL
	// which redirects to a fresh presigned S3 URL on each request, so the
	// stored link never expires.
	return fmt.Sprintf("%s/v2/apps/%s/file/%s", constants.WEMOB_API_BASE_URL, projectID, assetPath), nil
}

// enableProjectAIForAgent flips Settings["has_ai"] = true on the project so
// the AIAddendum is appended on subsequent runs and the gateway env vars are
// surfaced to subsequent generations. Idempotent at the tool layer — the
// SetupAIGatewayTool checks the flag before invoking this callback.
func (c *Controller) enableProjectAIForAgent(ctx context.Context, project *models.AppProject) error {
	_ = ctx
	if project == nil {
		return fmt.Errorf("project is required")
	}
	if project.Settings == nil {
		project.Settings = make(map[string]any)
	}
	project.Settings["has_ai"] = true
	if _, err := c.DB.UpdateAppProject(project); err != nil {
		return fmt.Errorf("persist has_ai on project: %w", err)
	}
	return nil
}

// enableProjectLocalDatabaseForAgent flips Settings["has_local_database"] = true
// on the project so the LocalDatabaseAddendum is appended on subsequent runs.
// Idempotent at the tool layer — ProvisionLocalDatabaseTool checks the flag
// before invoking this callback.
func (c *Controller) enableProjectLocalDatabaseForAgent(ctx context.Context, project *models.AppProject) error {
	_ = ctx
	if project == nil {
		return fmt.Errorf("project is required")
	}
	if project.Settings == nil {
		project.Settings = make(map[string]any)
	}
	project.Settings["has_local_database"] = true
	if _, err := c.DB.UpdateAppProject(project); err != nil {
		return fmt.Errorf("persist has_local_database on project: %w", err)
	}
	return nil
}

// triggerMobileBuildForAgent kicks off a fresh Codemagic Expo build on
// behalf of the chat agent's trigger_build_after_fix tool (P1 #17). Wraps
// BuildExpoMobile with the minimal options the agent path can supply:
// app name + icon are pulled from the project / its last build, splash
// color falls back to the controller default. Per-platform error
// isolation is preserved — partial-failure builds (one platform triggers,
// the other fails) still return a non-nil buildID so the agent can report
// the partial success to the user.
//
// platforms may be ["android"], ["ios"], or ["android", "ios"]. Empty
// slice defaults to "both" — matches the tool's schema default.
func (c *Controller) triggerMobileBuildForAgent(ctx context.Context, project *models.AppProject, user *models.User, platforms []string) (string, map[string]string, error) {
	if project == nil {
		return "", nil, fmt.Errorf("project is required")
	}
	if user == nil {
		return "", nil, fmt.Errorf("user is required")
	}
	if len(platforms) == 0 {
		platforms = []string{"android", "ios"}
	}

	// Pull last-build metadata so we keep app name / icon / splash color
	// stable across the rebuild. A first-time build (no Build row yet) is
	// fine — BuildExpoMobile creates one.
	opts := ExpoBuildOptions{
		Platforms: platforms,
		AppName:   project.Name,
	}

	// Inherit metadata from last build via direct AppProject scope.
	if last, lerr := c.DB.GetLastBuildByAppProject(project.ID.String()); lerr == nil && last != nil {
		if last.Title != "" {
			opts.AppName = last.Title
		}
		if last.Icon != "" {
			opts.Icon = last.Icon
		}
		if last.SplashScreenColor != "" {
			opts.SplashColor = last.SplashScreenColor
		}
	}

	build, tasks, err := c.BuildExpoMobile(ctx, project, user, opts)
	taskIDs := make(map[string]string, len(tasks))
	for _, t := range tasks {
		if t == nil {
			continue
		}
		switch t.Platform {
		case "Android":
			taskIDs["android"] = t.ID.String()
		case "IOS":
			taskIDs["ios"] = t.ID.String()
		}
	}
	buildID := ""
	if build != nil {
		buildID = build.ID.String()
	}
	return buildID, taskIDs, err
}
