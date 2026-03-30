package controllers

import (
	"context"
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
	"github.com/knid/wemob/internal/utils"
	"github.com/posthog/posthog-go"
)

func (c *Controller) CreateAppProject(ctx context.Context, user *models.User, req requests.GenerateAppRequest) (*models.AppProject, error) {
	validate := validator.New()
	if err := req.Validate(validate); err != nil {
		return nil, err
	}

	// Enforce app project limit
	limits, err := c.DB.GetStandartUserLimitsByUser(user)
	if err != nil {
		slog.Warn("CreateAppProject: failed to get user limits", "error", err, "user_id", user.ID)
	} else if limits.MonthlyAppCount > 0 {
		periodStart, _ := utils.BillingPeriod(user.SubscriptionStartAt, time.Now())
		appCount, countErr := c.DB.CountAppProjectsByUserIDSince(user.ID.String(), periodStart)
		if countErr != nil {
			slog.Warn("CreateAppProject: failed to count projects", "error", countErr, "user_id", user.ID)
		} else if appCount >= limits.MonthlyAppCount {
			go c.CRMService.SendEvent(user, constants.BREVO_EVENT_SUBSCRIPTION_QUOTA_REACHED, map[string]any{"action": "create_app", "limit": limits.MonthlyAppCount})
			go c.TrackUserEvent(user, "quota_reached", posthog.NewProperties().Set("action", "create_app").Set("limit", limits.MonthlyAppCount))
			return nil, fmt.Errorf("you have reached your monthly app project limit (%d)", limits.MonthlyAppCount)
		}
	}

	projectType := req.ProjectType
	if projectType == "" || projectType == "app" {
		classified := c.classifyProjectType(ctx, req.Prompt)
		if classified != "" {
			projectType = classified
		}
	}
	if projectType == "" {
		projectType = "app"
	}

	slug := generateAppSlug(req.Prompt)

	project := &models.AppProject{
		UserID:      user.ID,
		Name:        req.Prompt[:min(50, len(req.Prompt))],
		Slug:        slug,
		Prompt:      req.Prompt,
		ProjectType: projectType,
		Theme:       []byte("{}"),
		DataModels:  []byte("[]"),
		Settings:    map[string]any{},
		Status:      "generating",
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

	return project, nil
}

func (c *Controller) CloneAppProjectAsMobile(user *models.User, sourceProjectID string) (*models.AppProject, error) {
	source, err := c.DB.GetAppProjectByID(sourceProjectID)
	if err != nil {
		return nil, fmt.Errorf("source project not found")
	}
	if source.UserID != user.ID {
		return nil, fmt.Errorf("unauthorized")
	}

	slug := generateAppSlug(source.Name)

	clone := &models.AppProject{
		UserID:      user.ID,
		Name:        source.Name,
		Slug:        slug,
		Prompt:      source.Prompt,
		ProjectType: "mobile_app",
		Theme:       source.Theme,
		DataModels:  source.DataModels,
		Settings:    source.Settings,
		Status:      "ready",
	}

	clone, err = c.DB.CreateAppProject(clone)
	if err != nil {
		return nil, fmt.Errorf("failed to create mobile project")
	}

	// Clone git repo from source to new project
	if _, cloneErr := c.GitRepoService.Clone(source.ID.String(), clone.ID.String()); cloneErr != nil {
		slog.Error("Failed to clone git repo for mobile project", "error", cloneErr,
			"source_id", source.ID, "clone_id", clone.ID)
		return nil, fmt.Errorf("failed to copy files to mobile project")
	}

	return clone, nil
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
	if project.UserID != user.ID {
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

	// If slug changed and a Coolify project exists, reassign the wemob.app subdomain
	if oldSlug != req.Slug && c.CoolifyService != nil {
		go func() {
			cp, cpErr := c.DB.GetCoolifyProjectByAppProjectID(id)
			if cpErr != nil || cp == nil {
				return
			}

			newDomain := req.Slug + "." + constants.WEMOB_APP_BASE_DOMAIN

			if err := c.CoolifyService.UpdateDomains(cp.CoolifyAppUUID, []string{"https://" + newDomain}); err != nil {
				slog.Warn("Failed to update wemob.app subdomain on Coolify", "error", err)
				return
			}

			cp.CustomDomain = newDomain
			cp.ProductionURL = "https://" + newDomain
			c.DB.UpdateCoolifyProject(cp)
		}()
	}

	return updated, nil
}

func (c *Controller) DeleteAppProject(id string, user *models.User) error {
	project, err := c.GetAppProject(id, user)
	if err != nil {
		return err
	}

	// Clean up Neon database project (best-effort)
	if c.NeonService != nil {
		neonInteg, neonErr := c.getNeonIntegration(project.ID.String())
		if neonErr != nil {
			slog.Warn("Failed to look up Neon integration during app deletion", "error", neonErr, "app_project_id", project.ID)
		} else if neonInteg != nil {
			if neonProjectID, ok := neonInteg.Config["project_id"].(string); ok && neonProjectID != "" {
				if err := c.NeonService.DeleteProject(neonProjectID); err != nil {
					slog.Warn("Failed to delete Neon project during app deletion", "error", err, "neon_project_id", neonProjectID, "app_project_id", project.ID)
				} else {
					slog.Info("Deleted Neon project", "neon_project_id", neonProjectID, "app_project_id", project.ID)
				}
			}
		}
	}

	// Clean up git repo (best-effort)
	if err := c.GitRepoService.Delete(project.ID.String()); err != nil {
		slog.Warn("Failed to delete git repo during app deletion", "error", err, "project_id", project.ID)
	}

	// Mark deployments as undeployed and stop Coolify app (best-effort)
	if err := c.UndeployApp(project.ID.String()); err != nil {
		slog.Warn("Failed to undeploy app during deletion", "error", err, "project_id", project.ID)
	}

	// Clean up Coolify app and Gitea repo (best-effort)
	coolifyProject, cpErr := c.DB.GetCoolifyProjectByAppProjectID(project.ID.String())
	if cpErr == nil && coolifyProject != nil {
		if c.CoolifyService != nil {
			if err := c.CoolifyService.DeleteApp(coolifyProject.CoolifyAppUUID); err != nil {
				slog.Warn("Failed to delete Coolify app during app deletion", "error", err, "app_uuid", coolifyProject.CoolifyAppUUID)
			}
		}
		if c.GiteaService != nil {
			if err := c.GiteaService.DeleteRepo(coolifyProject.GiteaRepoName); err != nil {
				slog.Warn("Failed to delete Gitea repo during app deletion", "error", err, "repo", coolifyProject.GiteaRepoName)
			}
		}
		c.DB.DeleteCoolifyProject(coolifyProject)
	}

	return c.DB.DeleteAppProject(project)
}

func (c *Controller) UpdateAppProjectStatus(project *models.AppProject, status string) (*models.AppProject, error) {
	project.Status = status
	project.UpdatedAt = time.Now()
	return c.DB.UpdateAppProject(project)
}

// Snapshots — backed by git commit history

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
		entries = append(entries, responses.CommitHistoryEntry{
			Hash:        ci.Hash,
			Description: ci.Message,
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

	return project, nil
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

func generateAppSlug(prompt string) string {
	slug := strings.ToLower(prompt)
	slug = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		if r == ' ' || r == '-' {
			return '-'
		}
		return -1
	}, slug)
	// Trim and limit length
	slug = strings.Trim(slug, "-")
	if len(slug) > 50 {
		slug = slug[:50]
	}
	// Add uniqueness
	slug = slug + "-" + uuid.New().String()[:8]
	return slug
}
