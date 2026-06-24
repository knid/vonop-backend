package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/agent"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/observability"
	"github.com/knid/wemob/internal/services/fal"
	sandboxService "github.com/knid/wemob/internal/services/sandbox"
	"github.com/knid/wemob/internal/utils"
	"github.com/posthog/posthog-go"
)

// AgentDeps builds a per-run RunDependencies bundle for the agent package.
// Credit lifecycle callbacks are bound to the specific user at call time.
// This method is called once per HTTP chat/generate request from the view layer.
func (c *Controller) AgentDeps(user *models.User, jobID string) *agent.RunDependencies {
	provider := c.llmProviderForUser(user)
	_, planName, _ := c.GetUserLimitsWithPlan(user)
	langfuseScores := observability.ScoreClientFromEnvAndSecrets(c.SecretManager)
	langfusePrompts := observability.PromptClientFromEnvAndSecrets(c.SecretManager)

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
		EnableAI: c.enableProjectAIForAgent,
		GatewayAllowedModels: func() []string {
			return c.GatewayAllowedModels(user)
		},
		// trigger_web_deploy — bound here so the agent can close the
		// read-fix-redeploy loop for web deploy failures.
		TriggerWebDeploy: func(ctx context.Context, project *models.AppProject, user *models.User) (string, error) {
			return c.triggerWebDeployForAgent(ctx, project, user)
		},
		SandboxControl: func(ctx context.Context, project *models.AppProject, user *models.User, req agent.SandboxControlRequest) (map[string]any, error) {
			return c.sandboxControlForAgent(ctx, project, user, req)
		},
		SetPreviewURL: func(ctx context.Context, project *models.AppProject, url, label, kind string) error {
			return c.setPreviewURLForAgent(ctx, project, url, label, kind)
		},
		SyncSandboxFiles: func(ctx context.Context, project *models.AppProject, paths []string) error {
			return c.SyncSandboxPaths(ctx, project, paths)
		},
		SyncSandboxDeltas: func(ctx context.Context, project *models.AppProject, deltas []sandboxService.FileDelta) error {
			return c.SyncSandboxDeltas(ctx, project, deltas)
		},
		PersistWorktreeFiles: func(ctx context.Context, project *models.AppProject, files map[string]string) error {
			if project == nil {
				return fmt.Errorf("project is required")
			}
			repo, err := c.GitRepoService.Open(project.ID.String())
			if err != nil {
				return err
			}
			return repo.WriteFileMap(files)
		},
		RecordLLMUsage: func(tracker *utils.TokenTracker, action, model, provider, reference, appType string) {
			c.RecordTokenUsage(user, action, model, provider, reference, appType, tracker)
		},
		RecordLLMError: func(tracker *utils.TokenTracker, action, model, provider, reference, appType string, latencyMs int64, errCode, errMsg string) {
			c.RecordLLMError(user, tracker, action, model, provider, reference, appType, latencyMs, errCode, errMsg)
		},
		RecordLangfuseScore: func(ctx context.Context, score observability.ScoreRequest) error {
			return langfuseScores.CreateScore(ctx, score)
		},
		ResolveLangfusePrompt: func(ctx context.Context, name, fallback string, vars map[string]string) (observability.CompiledPrompt, error) {
			return langfusePrompts.CompileTextPrompt(ctx, name, vars)
		},
		JobID:                 jobID,
		ListMCPTools:          c.AgentMCPTools,
		CreateMCPToolApproval: c.CreateMCPToolApproval,
		WaitForMCPApproval:    c.WaitForMCPApproval,
		ExecuteMCPTool:        c.ExecuteMCPTool,
	}
}

func (c *Controller) sandboxControlForAgent(ctx context.Context, project *models.AppProject, user *models.User, req agent.SandboxControlRequest) (map[string]any, error) {
	if project == nil {
		return nil, fmt.Errorf("project is required")
	}
	if user == nil {
		return nil, fmt.Errorf("user is required")
	}
	if c.SandboxManager == nil {
		return nil, fmt.Errorf("sandbox manager is not configured")
	}
	switch req.Action {
	case "preview_status":
		status, err := c.PreviewStatus(ctx, project)
		out := structToMap(status)
		out["env_status"] = sandboxEnvStatus(c.CollectAllEnvVars(project))
		return out, err
	case "preview_logs":
		logs, err := c.PreviewLogs(ctx, project, req.Target, 200)
		if err != nil {
			return nil, err
		}
		return map[string]any{"logs": logs, "sandbox_id": project.SandboxID, "target": req.Target}, nil
	case "status":
		status, err := c.SandboxManager.Status(ctx, project)
		out := agentSandboxStatusMap(project, status)
		out["env_status"] = sandboxEnvStatus(c.CollectAllEnvVars(project))
		return out, err
	case "start":
		var (
			status *sandboxService.Status
			err    error
		)
		if len(req.Files) > 0 {
			status, err = c.startSandboxWithFiles(ctx, project, copySandboxFiles(req.Files))
		} else {
			status, err = c.StartSandbox(ctx, project)
		}
		out := agentSandboxStatusMap(project, status)
		out["env_status"] = sandboxEnvStatus(c.CollectAllEnvVars(project))
		return out, err
	case "sync":
		var err error
		if len(req.Files) > 0 {
			err = c.syncSandboxWithFiles(ctx, project, copySandboxFiles(req.Files))
		} else {
			err = c.SyncSandbox(ctx, project)
		}
		return map[string]any{"ok": err == nil, "sandbox_id": project.SandboxID}, err
	case "exec":
		c.refreshSandboxID(ctx, project)
		stdout, stderr, exitCode, err := c.SandboxManager.Execute(ctx, project, req.Command, req.Cwd, req.TimeoutMs)
		return sandboxExecOutput(stdout, stderr, exitCode, project.SandboxID, err), err
	case "logs", "process_logs":
		logs, err := c.SandboxManager.ProcessLogs(ctx, project, sandboxProcessName(firstNonEmptySandboxValue(req.Process, req.Target)), 200)
		if err != nil {
			return nil, err
		}
		return map[string]any{"logs": logs, "sandbox_id": project.SandboxID}, nil
	case "restart_process", "process_restart":
		err := c.SandboxManager.RestartProcess(ctx, project, sandboxProcessName(firstNonEmptySandboxValue(req.Process, req.Target)), c.CollectAllEnvVars(project))
		return map[string]any{"ok": err == nil, "sandbox_id": project.SandboxID}, err
	case "open_route", "preview_open_route":
		target, err := c.SandboxManager.OpenRoute(ctx, project, req.Route, "agent")
		return structToMap(target), err
	case "serve_artifact", "serve-artifact":
		target, err := c.SandboxManager.OpenArtifact(ctx, project, req.Path, "agent")
		return structToMap(target), err
	case "diagnostics":
		diagnostics, err := c.SandboxManager.RunDiagnostics(ctx, project)
		return structToMap(diagnostics), err
	case "terminal_create":
		terminal, err := c.SandboxManager.CreateTerminal(ctx, project, req.Cols, req.Rows)
		return structToMap(terminal), err
	case "terminal_input":
		err := c.SandboxManager.SendTerminalInput(ctx, project, req.Target, firstNonEmptySandboxValue(req.Input, req.Content))
		return map[string]any{"ok": err == nil, "sandbox_id": project.SandboxID}, err
	case "terminal_close":
		err := c.SandboxManager.CloseTerminal(ctx, project, req.Target)
		return map[string]any{"ok": err == nil, "sandbox_id": project.SandboxID}, err
	case "write_file", "delete_file":
		return nil, fmt.Errorf("sandbox source writes are disabled; use Git-backed app file tools, then sync to sandbox")
	default:
		return nil, fmt.Errorf("unsupported sandbox action %q", req.Action)
	}
}

func copySandboxFiles(files map[string]string) map[string]string {
	if files == nil {
		return nil
	}
	copied := make(map[string]string, len(files))
	for path, content := range files {
		copied[path] = content
	}
	return copied
}

func sandboxExecOutput(stdout, stderr string, exitCode int, sandboxID string, execErr error) map[string]any {
	out := map[string]any{
		"stdout":     stdout,
		"stderr":     stderr,
		"exit_code":  exitCode,
		"sandbox_id": sandboxID,
	}
	if execErr != nil {
		out["error"] = execErr.Error()
		if exitCode == 0 {
			out["exit_code"] = -1
		}
	}
	return out
}

func sandboxEnvStatus(envVars map[string]string) map[string]string {
	out := make(map[string]string, len(envVars))
	for key, value := range envVars {
		if value != "" {
			out[key] = "set"
		} else {
			out[key] = "unset"
		}
	}
	return out
}

func structToMap(v any) map[string]any {
	if v == nil {
		return map[string]any{}
	}
	b, _ := json.Marshal(v)
	out := map[string]any{}
	_ = json.Unmarshal(b, &out)
	if out == nil {
		return map[string]any{}
	}
	return out
}

func agentSandboxStatusMap(project *models.AppProject, status *sandboxService.Status) map[string]any {
	out := structToMap(status)
	if project == nil || status == nil || len(status.PreviewURLs) == 0 {
		return out
	}
	publicURLs := stableSandboxPreviewURLs(project, status.PreviewURLs)
	if len(publicURLs) == 0 {
		return out
	}
	out["preview_urls"] = publicURLs
	if appURL, ok := publicURLs[string(sandboxService.PreviewApp)]; ok {
		out["preview_url"] = appURL
	}
	return out
}

func stableSandboxPreviewURLs(project *models.AppProject, previewURLs map[sandboxService.PreviewKind]string) map[string]string {
	out := make(map[string]string, len(previewURLs))
	for kind, rawURL := range previewURLs {
		if rawURL == "" {
			continue
		}
		publicURL := sandboxService.StablePreviewBaseURL(project, kind)
		if publicURL == "" {
			continue
		}
		out[string(kind)] = publicURL
	}
	return out
}

func firstNonEmptySandboxValue(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func sandboxProcessName(target string) sandboxService.ProcessName {
	switch target {
	case string(sandboxService.ProcessBackend):
		return sandboxService.ProcessBackend
	case string(sandboxService.ProcessExpoGo):
		return sandboxService.ProcessExpoGo
	case string(sandboxService.ProcessExpoWeb):
		return sandboxService.ProcessExpoWeb
	case "artifacts", "artifact":
		return sandboxService.ProcessArtifacts
	default:
		return sandboxService.ProcessFrontend
	}
}

func (c *Controller) setPreviewURLForAgent(ctx context.Context, project *models.AppProject, url, label, kind string) error {
	_ = ctx
	if project == nil {
		return fmt.Errorf("project is required")
	}
	target := map[string]any{
		"url":        url,
		"label":      label,
		"kind":       kind,
		"updated_by": "agent",
		"updated_at": time.Now().UTC().Format(time.RFC3339),
	}
	if project.Settings == nil {
		project.Settings = make(map[string]any)
	}
	project.Settings["preview_target"] = target
	err := c.DB.UpdateAppProjectPreviewTarget(project.ID.String(), target)
	return err
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
	if project.UsesSplitWebRuntime() || project.ProjectType == models.ProjectTypeMobileAppNative || project.FrontendRuntime == models.FrontendRuntimeNone {
		project.BackendEnabled = true
		project.BackendRuntime = models.BackendRuntimeNextJSAPI
	}
	if _, err := c.DB.UpdateAppProject(project); err != nil {
		return fmt.Errorf("persist has_ai on project: %w", err)
	}
	return nil
}
