package controllers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/posthog/posthog-go"
)

// AppLimitError signals that an app-creation request was rejected by the
// org's plan limits. HTTP views detect this via errors.As and translate it
// into a structured 4xx response; without it the error falls through to the
// catch-all 500 path and the client sees an opaque "internal error".
type AppLimitError struct {
	IsMobile bool
	Limit    int // 0 means the feature is not available on this plan
	Used     int
}

func (e *AppLimitError) Error() string {
	kind := "web"
	if e.IsMobile {
		kind = "mobile"
	}
	if e.Limit == 0 {
		return fmt.Sprintf("%s app creation is not available on your plan", kind)
	}
	return fmt.Sprintf("you have reached your %s app limit (%d)", kind, e.Limit)
}

// CreateDraftAppProject creates a new app project in "draft" status — no
// generation is kicked off. Used by the home composer flow, which creates the
// project synchronously, navigates the user to the studio, and then lets the
// studio kick off the initial generation through the normal chat endpoint.
// This keeps the UI consistent from the very first prompt instead of forcing
// the user through a bespoke bootstrap screen.
func (c *Controller) CreateDraftAppProject(ctx context.Context, user *models.User, req requests.GenerateAppRequest) (*models.AppProject, error) {
	project, err := c.CreateAppProject(ctx, user, req)
	if err != nil {
		return nil, err
	}
	project.Status = "draft"
	if _, err := c.DB.UpdateAppProject(project); err != nil {
		return nil, fmt.Errorf("failed to set draft status: %w", err)
	}
	return project, nil
}

func (c *Controller) CreateAppProject(ctx context.Context, user *models.User, req requests.GenerateAppRequest) (*models.AppProject, error) {
	validate := validator.New()
	if err := req.Validate(validate); err != nil {
		return nil, err
	}

	// Determine project type first so we can enforce the right limit. Only
	// auto-classify when the client didn't pick one — an explicit "app"
	// (web) selection from the home composer must be honored, otherwise the
	// LLM classifier can flip prompts that "sound mobile" (camera, scanning,
	// etc.) into mobile_app_native and bounce Starter users with a misleading
	// "not available in your plan" 403.
	projectType := req.ProjectType
	if projectType == "" {
		classified := c.classifyProjectType(ctx, req.Prompt, user)
		if classified != "" {
			projectType = classified
		}
	}
	if projectType == "" {
		projectType = "app"
	}

	// Enforce app project limit by type. A5/A7/A8: limits errors now fail
	// closed instead of silently allowing the action.
	limits, err := c.GetUserLimits(user)
	if err != nil {
		return nil, fmt.Errorf("could not load plan limits: %w", err)
	}
	// §B Phase 4: project quota is org-scoped (Plan A made limits org-scoped),
	// so the count must be too. Resolve the actor's org early so both the count
	// and the new project row write the same organization_id.
	orgIDForCount, err := c.RequireOrgIDForUser(user, req.OrganizationID)
	if err != nil {
		return nil, err
	}
	{
		isMobile := projectType == models.ProjectTypeMobileAppNative
		var maxApps int
		var countFilter string
		if isMobile {
			maxApps = limits.MaxMobileApps
			countFilter = "mobile"
		} else {
			maxApps = limits.MaxWebApps
			countFilter = "" // counts all non-mobile
		}

		if maxApps == 0 {
			return nil, &AppLimitError{IsMobile: isMobile, Limit: 0}
		}

		appCount, countErr := c.DB.CountAppProjectsByOrgIDAndType(orgIDForCount.String(), countFilter)
		if countErr != nil {
			slog.Warn("CreateAppProject: failed to count projects", "error", countErr, "org_id", orgIDForCount)
		} else if appCount >= maxApps {
			action := "create_app"
			if isMobile {
				action = "create_mobile_app"
			}
			go c.EventService.SendEvent(user, constants.EVENT_SUBSCRIPTION_QUOTA_REACHED, map[string]any{"action": action, "limit": maxApps})
			go c.TrackUserEvent(user, constants.EVENT_QUOTA_REACHED, posthog.NewProperties().Set("action", action).Set("limit", maxApps))
			return nil, &AppLimitError{IsMobile: isMobile, Limit: maxApps, Used: appCount}
		}
	}

	name, slug := c.generateAppNameAndSlug(ctx, req.Prompt, user)

	// Reuse the org we already resolved for the quota gate above. Avoids a
	// second RequireOrgIDForUser call (and its DB roundtrip).
	orgID := orgIDForCount

	// Seed build_defaults for native mobile apps so the Ship page's
	// "+ New build" button is enabled from app creation. Without this the
	// launchpad gate (build_defaults.app_name must be set) blocks the user
	// until the agent or a manual edit writes one — but the project
	// already has a generated name we can mirror in.
	settings := map[string]any{}
	if projectType == models.ProjectTypeMobileAppNative {
		settings["build_defaults"] = map[string]any{
			"app_name": name,
		}
	}

	project := &models.AppProject{
		UserID:         user.ID,
		OrganizationID: orgID,
		Name:           name,
		Slug:           slug,
		Prompt:         req.Prompt,
		ProjectType:    projectType,
		Theme:          []byte("{}"),
		DataModels:     []byte("[]"),
		Settings:       settings,
		Status:         "generating",
	}

	project, err = c.DB.CreateAppProject(project)
	if err != nil {
		slog.Error("Failed to create app project", "error", err)
		return nil, fmt.Errorf("failed to create project")
	}

	// Initialize git repo for this project
	if _, initErr := c.GitRepoService.Init(project.ID.String()); initErr != nil {
		slog.Error("Failed to initialize git repo for project", "error", initErr, "project_id", project.ID)
	}

	// Generate gateway API key for the AI gateway
	if _, gwErr := c.CreateProjectGatewayKey(project); gwErr != nil {
		slog.Error("Failed to create gateway key for project", "error", gwErr, "project_id", project.ID)
	}

	// Ecommerce projects have a canonical schema — provision the tenant DB
	// eagerly so the AI never needs to call provision_database. If this fails,
	// we log and proceed; the model will see has_database=false and can
	// retry via the tool on its first turn.
	if project.ProjectType == "ecommerce" && c.NeonService != nil {
		if err := c.provisionDatabaseForAgent(ctx, project, user, nil); err != nil {
			slog.Error("eager ecommerce db provision failed",
				"error", err, "project_id", project.ID)
		}
	}

	// mobile_app_native projects get an eager MobileAppConfig row + a
	// canonical mobile/ + backend/ skeleton in the project repo so the
	// agent has somewhere to write from turn 1 and read_mobile_config
	// returns sensible defaults instead of "missing config".
	if project.ProjectType == models.ProjectTypeMobileAppNative {
		// Pre-populate every field with the same defaults the DB column
		// DEFAULT clauses use. Bun's INSERT emits explicit values for all
		// non-pointer scalar fields (including bool zeros), which would
		// override the column defaults if we left them unset.
		cfg := &models.MobileAppConfig{
			ProjectID:            project.ID,
			PushEnabled:          true,
			AnalyticsEnabled:     true,
			PullToRefreshEnabled: true,
			ExternalBrowserLinks: true,
			BridgeEnabled:        true,
			OfflineEnabled:       false,
			CachingStrategy:      "network-first",
			BiometricEnabled:     false,
			DarkModeEnabled:      true,
			RatingEnabled:        false,
			RatingSessions:       5,
			UpdateCheckEnabled:   false,
			// IAP defaults OFF — most apps don't need payments. The agent flips
			// this on (and triggers lazy RC provisioning at next build) only
			// when the user asks for paywalls / subscriptions.
			IAPEnabled: false,
		}
		if _, err := c.DB.UpsertMobileAppConfig(cfg); err != nil {
			// Non-fatal: the agent can still call read_mobile_config and
			// get a missing-config response. Log so we can investigate
			// if this becomes a pattern.
			slog.Warn("eager MobileAppConfig insert failed",
				"error", err, "project_id", project.ID)
		}

		// RevenueCat + OneSignal provisioning is intentionally LAZY — only
		// triggered from the build path (BuildExpoMobile) when the agent has
		// flipped iap_enabled / push_enabled on. Eager goroutines were
		// dropped because most projects (calculators, utilities, single-
		// purpose tools) never need IAP or push, and provisioning created
		// orphan RC projects + OneSignal apps for every signup.

		// Scaffold the wemob-expo-template baseline into mobile/ so the AI
		// agent has a complete Expo SDK 54 + Expo Router + NativeWind v4 +
		// react-native-reusables source tree to read + edit, AND the preview
		// sandbox can run `npx expo start --web` against it with no extra
		// setup. Without this, the agent flies blind (no package.json to
		// read, no lib/api.ts to import) and the preview sandbox can't
		// resolve its own Expo CLI.
		//
		// backend/ is opt-in: the agent creates it (via the write tool) only
		// when the user asks for server-side state, auth, payments, or a
		// marketing landing. For pure client-side apps the project stays
		// mobile-only and never spawns a Fly machine.
		if c.GitRepoService != nil {
			if repo, err := c.GitRepoService.Open(project.ID.String()); err == nil {
				readme := "# " + project.Name + `

The mobile app source lives in mobile/ — a complete Expo SDK 54 +
NativeWind v4 + react-native-reusables baseline. The AI agent edits
this directly; Codemagic builds it into a signed APK/IPA.

backend/ is OPTIONAL — created only when this app needs server-side
functionality (persisted data, auth, payments, push, marketing landing).
`
				if err := repo.WriteFile("README.md", []byte(readme)); err != nil {
					slog.Warn("mobile_app_native README write failed",
						"error", err, "project_id", project.ID)
				}
				if err := scaffoldExpoTemplate(c, repo, project, cfg.AuthEnabled); err != nil {
					slog.Warn("mobile_app_native template scaffold failed",
						"error", err, "project_id", project.ID)
				}
				if _, err := repo.Commit("scaffold: Expo template baseline under mobile/"); err != nil {
					slog.Warn("mobile_app_native skeleton commit failed",
						"error", err, "project_id", project.ID)
				}
			} else {
				slog.Warn("mobile_app_native skeleton: failed to open repo (will rely on agent to create dirs)",
					"error", err, "project_id", project.ID)
			}
		}
	}

	return project, nil
}

func (c *Controller) GetAppProjectsByUserFiltered(user *models.User, types []string) ([]models.AppProject, error) {
	if len(types) == 0 {
		return c.DB.GetAppProjectsByUserID(user.ID.String())
	}
	return c.DB.GetAppProjectsByUserIDAndTypes(user.ID.String(), types)
}

func (c *Controller) GetAppProject(id string, user *models.User) (*models.AppProject, error) {
	project, err := c.DB.GetAppProjectByID(id)
	if err != nil {
		return nil, fmt.Errorf("project not found")
	}
	if err := c.AuthorizeProjectAccess(user, project); err != nil {
		return nil, fmt.Errorf("unauthorized")
	}
	return project, nil
}

func (c *Controller) UpdateAppProject(id string, user *models.User, req requests.UpdateAppProjectRequest) (*models.AppProject, error) {
	validate := validator.New()
	if err := req.Validate(validate); err != nil {
		return nil, err
	}

	project, err := c.GetAppProject(id, user)
	if err != nil {
		return nil, err
	}

	oldSlug := project.Slug

	project.Name = req.Name
	project.Slug = req.Slug
	if len(req.Settings) > 0 {
		if project.Settings == nil {
			project.Settings = make(map[string]any)
		}
		for k, v := range req.Settings {
			project.Settings[k] = v
		}
	}
	project.UpdatedAt = time.Now()

	updated, err := c.DB.UpdateAppProject(project)
	if err != nil {
		return nil, err
	}

	// If slug changed, refresh the wemob.app subdomain on the runtime app row.
	// The actual Caddy/Fly route picks up the new subdomain on the next deploy.
	if oldSlug != req.Slug {
		go func() {
			ra, raErr := c.DB.GetRuntimeAppByAppProjectID(id)
			if raErr != nil || ra == nil {
				return
			}
			newDomain := req.Slug + "." + constants.WEMOB_APP_BASE_DOMAIN
			ra.CustomDomain = newDomain
			ra.ProductionURL = "https://" + newDomain
			c.DB.UpdateRuntimeApp(ra)
		}()
	}

	return updated, nil
}

func (c *Controller) DeleteAppProject(id string, user *models.User) error {
	project, err := c.GetAppProject(id, user)
	if err != nil {
		return err
	}
	c.tearDownProjectInfra(context.Background(), project)
	return c.DB.DeleteAppProject(project)
}

// tearDownProjectInfra runs every external-resource cleanup associated with an
// AppProject: Neon DB delete, git repo delete, undeploy + Fly teardown enqueue,
// Gitea backup repo delete, runtime_apps row delete, and S3 asset cleanup.
// Each step is best-effort and logged but never returned, so a flaky downstream
// (Fly, Neon, S3, Gitea) can't block deletion. Used both by the user-driven
// DeleteAppProject and the system-driven org grace-teardown handler — neither
// can afford a partial failure to abort the rest of the cleanup.
func (c *Controller) tearDownProjectInfra(ctx context.Context, project *models.AppProject) {
	// Clean up Neon database project (best-effort). Resolve the right
	// org-scoped service from the integration's stored tier — a project
	// living in the Free org can't be deleted with a Paid-org API key.
	if c.NeonService != nil || c.NeonRegistry != nil {
		neonInteg, neonErr := c.getNeonIntegration(project.ID.String())
		if neonErr != nil {
			slog.Warn("Failed to look up Neon integration during app deletion", "error", neonErr, "app_project_id", project.ID)
		} else if neonInteg != nil {
			neonProjectID := neonProjectIDFromIntegration(neonInteg)
			tier := neonTierFromIntegration(neonInteg)
			if neonProjectID != "" {
				svc := c.neonForTier(tier)
				if svc == nil {
					slog.Warn("No Neon service configured for tier; skipping delete", "tier", tier, "neon_project_id", neonProjectID, "app_project_id", project.ID)
				} else if err := svc.DeleteProject(neonProjectID); err != nil {
					slog.Warn("Failed to delete Neon project during app deletion", "error", err, "neon_project_id", neonProjectID, "app_project_id", project.ID)
				} else {
					slog.Info("Deleted Neon project", "neon_project_id", neonProjectID, "tier", tier, "app_project_id", project.ID)
				}
			}
		}
	}

	// Clean up git repo (best-effort)
	if err := c.GitRepoService.Delete(project.ID.String()); err != nil {
		slog.Warn("Failed to delete git repo during app deletion", "error", err, "project_id", project.ID)
	}

	if err := c.UndeployApp(project.ID.String()); err != nil {
		slog.Warn("Failed to undeploy app during deletion", "error", err, "project_id", project.ID)
	}

	// Clean up Gitea backup repo + runtime_apps row (best-effort). The Fly
	// teardown is enqueued (not inline) so an unreachable Fly API doesn't
	// block project deletion; Asynq retries handle transient failures.
	runtimeApp, raErr := c.DB.GetRuntimeAppByAppProjectID(project.ID.String())
	if raErr == nil && runtimeApp != nil {
		if runtimeApp.FlyAppName != "" {
			if err := c.EnqueueFlyDelete(runtimeApp.FlyAppName, runtimeApp.ID); err != nil {
				slog.Warn("Failed to enqueue Fly app delete during app deletion", "error", err, "fly_app", runtimeApp.FlyAppName)
			}
		}
		if c.GiteaService != nil {
			if err := c.GiteaService.DeleteRepo(runtimeApp.GiteaRepoName); err != nil {
				slog.Warn("Failed to delete Gitea repo during app deletion", "error", err, "repo", runtimeApp.GiteaRepoName)
			}
		}
		c.DB.DeleteRuntimeApp(runtimeApp)
	}

	c.cleanupProjectAssets(ctx, project.ID)
}

// cleanupProjectAssets removes every app_asset for a project — both the S3
// object and the DB row. Inline; per-project asset count is bounded; an
// orphan-reaper for failed S3 deletes is TBD. Errors are logged but never
// returned: the caller is mid-delete and a flaky S3 must not block them.
func (c *Controller) cleanupProjectAssets(ctx context.Context, projectID uuid.UUID) {
	assets, err := c.DB.GetAppAssetsByProjectID(projectID.String())
	if err != nil {
		slog.Warn("cleanupProjectAssets: failed to list assets", "error", err, "project_id", projectID)
		return
	}
	if len(assets) == 0 {
		return
	}

	if c.AssetsStorage != nil {
		for i := range assets {
			asset := &assets[i]
			if asset.S3Key == "" {
				continue
			}
			if err := c.AssetsStorage.DeleteObjectDirect(ctx, asset.S3Key); err != nil {
				slog.Warn("cleanupProjectAssets: S3 delete failed", "error", err, "project_id", projectID, "asset_id", asset.ID)
			}
		}
	}

	if err := c.DB.SoftDeleteAppAssetsByProject(ctx, projectID); err != nil {
		slog.Warn("cleanupProjectAssets: bulk soft-delete failed", "error", err, "project_id", projectID)
	}
}

func (c *Controller) UpdateAppProjectStatus(project *models.AppProject, status string) (*models.AppProject, error) {
	project.Status = status
	project.UpdatedAt = time.Now()
	return c.DB.UpdateAppProject(project)
}

// Versions — backed by git commit history

// parseCommitSource splits a commit message of the form "source: description"
// into its source and description parts. Returns ("", fullMessage) if no prefix.
func parseCommitSource(message string) (source, description string) {
	if idx := strings.Index(message, ": "); idx > 0 && idx < 20 {
		prefix := message[:idx]
		switch prefix {
		case "auto", "manual", "undo", "init", "deploy":
			return prefix, message[idx+2:]
		}
	}
	return "", message
}

func (c *Controller) GetAppSnapshots(projectID string) ([]responses.CommitHistoryEntry, error) {
	repo, err := c.GitRepoService.Open(projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to open repo: %w", err)
	}

	commits, err := repo.Log(50)
	if err != nil {
		return nil, fmt.Errorf("failed to read commit history: %w", err)
	}

	entries := make([]responses.CommitHistoryEntry, 0, len(commits))
	for _, ci := range commits {
		source, description := parseCommitSource(ci.Message)
		entries = append(entries, responses.CommitHistoryEntry{
			Hash:        ci.Hash,
			Description: description,
			Source:      source,
			CreatedAt:   ci.Timestamp,
		})
	}
	return entries, nil
}

func (c *Controller) CreateAppSnapshot(project *models.AppProject, description string, source string) (string, error) {
	repo, err := c.GitRepoService.OpenOrInit(project.ID.String())
	if err != nil {
		return "", fmt.Errorf("failed to open repo: %w", err)
	}

	// Write theme to repo so it's captured in the commit
	if err := repo.WriteTheme(project.Theme); err != nil {
		slog.Warn("CreateAppSnapshot: failed to write theme", "error", err, "project_id", project.ID)
	}

	commitMsg := fmt.Sprintf("%s: %s", source, description)
	hash, err := repo.Commit(commitMsg)
	if err != nil {
		return "", fmt.Errorf("failed to create snapshot commit: %w", err)
	}
	return hash, nil
}

func (c *Controller) RestoreAppSnapshot(project *models.AppProject, commitHash string) (*models.AppProject, error) {
	repo, err := c.GitRepoService.Open(project.ID.String())
	if err != nil {
		return nil, fmt.Errorf("failed to open repo: %w", err)
	}

	// Revert to the target commit and commit the result atomically
	_, err = repo.RevertAndCommit(commitHash, "undo: reverted to previous version")
	if err != nil {
		return nil, fmt.Errorf("failed to restore snapshot: %w", err)
	}

	// Restore theme from repo
	if themeData, readErr := repo.ReadTheme(); readErr == nil && len(themeData) > 0 && models.ValidateThemeJSON(themeData) == nil {
		project.Theme = themeData
		project.UpdatedAt = time.Now()
		updated, updateErr := c.DB.UpdateAppProject(project)
		if updateErr != nil {
			slog.Error("RestoreAppSnapshot: failed to update project theme", "error", updateErr, "project_id", project.ID)
			return project, nil
		}
		return updated, nil
	}

	return project, nil
}

// GetAppSnapshotDiff returns the diff for a specific commit (what it changed).
func (c *Controller) GetAppSnapshotDiff(projectID, commitHash string) (string, error) {
	repo, err := c.GitRepoService.Open(projectID)
	if err != nil {
		return "", fmt.Errorf("failed to open repo: %w", err)
	}

	diff, err := repo.Diff(commitHash+"~1", commitHash)
	if err != nil {
		// If commitHash~1 doesn't exist (first commit), diff against empty tree
		diff, err = repo.Diff("4b825dc642cb6eb9a060e54bf899d69f82cf17a8", commitHash)
		if err != nil {
			return "", fmt.Errorf("failed to get diff: %w", err)
		}
	}
	return diff, nil
}

// Generated Files

func (c *Controller) GetAppGeneratedFiles(projectID string) (map[string]string, error) {
	repo, err := c.GitRepoService.Open(projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to open repo: %w", err)
	}
	return repo.ReadAllFiles()
}

// File CRUD

func (c *Controller) CreateAppFile(project *models.AppProject, filePath string, content string, fileType string) error {
	repo, err := c.GitRepoService.OpenOrInit(project.ID.String())
	if err != nil {
		return fmt.Errorf("failed to open repo: %w", err)
	}

	if repo.FileExists(filePath) {
		return fmt.Errorf("a file already exists at this path")
	}

	if err := repo.WriteFile(filePath, []byte(content)); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	if _, err := repo.Commit(fmt.Sprintf("manual: create %s", filePath)); err != nil {
		return fmt.Errorf("failed to commit: %w", err)
	}

	return nil
}

func (c *Controller) UpdateAppFile(project *models.AppProject, filePath string, content string) error {
	repo, err := c.GitRepoService.Open(project.ID.String())
	if err != nil {
		return fmt.Errorf("failed to open repo: %w", err)
	}

	if !repo.FileExists(filePath) {
		return fmt.Errorf("file not found")
	}

	if err := repo.WriteFile(filePath, []byte(content)); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	if _, err := repo.Commit(fmt.Sprintf("manual: update %s", filePath)); err != nil {
		return fmt.Errorf("failed to commit: %w", err)
	}

	return nil
}

func (c *Controller) DeleteAppFile(project *models.AppProject, filePath string) error {
	repo, err := c.GitRepoService.Open(project.ID.String())
	if err != nil {
		return fmt.Errorf("failed to open repo: %w", err)
	}

	if !repo.FileExists(filePath) {
		return fmt.Errorf("file not found")
	}

	if err := repo.DeleteFile(filePath); err != nil {
		return fmt.Errorf("failed to delete file: %w", err)
	}

	if _, err := repo.Commit(fmt.Sprintf("manual: delete %s", filePath)); err != nil {
		return fmt.Errorf("failed to commit: %w", err)
	}

	return nil
}

// Export

func (c *Controller) ExportAppAsZIP(projectID string) (map[string]string, error) {
	repo, err := c.GitRepoService.Open(projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to open repo: %w", err)
	}
	return repo.ReadAllFiles()
}

// InferFileType extracts a file type identifier from the file extension.
func InferFileType(path string) string {
	ext := filepath.Ext(path)
	if ext == "" {
		return "text"
	}
	ext = strings.TrimPrefix(ext, ".")
	switch ext {
	case "tsx", "ts", "jsx", "js", "css", "json", "mjs", "yaml", "yml", "dart", "html", "md", "svg":
		return ext
	default:
		return "text"
	}
}

// MoveAppProjectToOrg transfers a project between organizations. The caller
// must own (or be admin of) BOTH the source org (where the project currently
// lives) and the destination org. The project itself must belong to the
// caller (UserID match) — we don't yet support agency-style transfers where
// an org admin moves another member's project.
func (c *Controller) MoveAppProjectToOrg(user *models.User, projectID string, destOrgID uuid.UUID) (*models.AppProject, error) {
	project, err := c.DB.GetAppProjectByID(projectID)
	if err != nil {
		return nil, errors.New("project not found")
	}
	if project.UserID != user.ID {
		return nil, errors.New("only the project owner can transfer it")
	}

	srcMember, err := c.DB.GetOrganizationMember(project.OrganizationID.String(), user.ID.String())
	if err != nil || (srcMember.Role != "owner" && srcMember.Role != "admin") {
		return nil, errors.New("insufficient permissions on source organization")
	}
	dstMember, err := c.DB.GetOrganizationMember(destOrgID.String(), user.ID.String())
	if err != nil || (dstMember.Role != "owner" && dstMember.Role != "admin") {
		return nil, errors.New("insufficient permissions on destination organization")
	}

	if err := c.DB.AssignAppProjectOrg(project.ID.String(), destOrgID); err != nil {
		return nil, fmt.Errorf("transfer project: %w", err)
	}
	project.OrganizationID = destOrgID
	return project, nil
}

