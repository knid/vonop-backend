package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
)

func (c *Controller) CreateAppProject(ctx context.Context, user *models.User, req requests.GenerateAppRequest) (*models.AppProject, error) {
	validate := validator.New()
	if err := req.Validate(validate); err != nil {
		return nil, err
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
		AST:         []byte("[]"),
		Theme:       []byte("{}"),
		DataModels:  []byte("[]"),
		Settings:    map[string]any{},
		Status:      "generating",
	}

	project, err := c.DB.CreateAppProject(project)
	if err != nil {
		slog.Error("Failed to create app project", "error", err)
		return nil, fmt.Errorf("failed to create project")
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
		AST:         source.AST,
		Theme:       source.Theme,
		DataModels:  source.DataModels,
		Settings:    source.Settings,
		Status:      "ready",
	}

	clone, err = c.DB.CreateAppProject(clone)
	if err != nil {
		return nil, fmt.Errorf("failed to create mobile project")
	}

	// Copy all generated files from source to clone
	files, err := c.DB.GetAppGeneratedFilesByProjectID(source.ID.String(), "nextjs")
	if err != nil {
		slog.Warn("Failed to fetch source files for cloning", "error", err)
	} else if len(files) > 0 {
		var clonedFiles []models.AppGeneratedFile
		for _, f := range files {
			clonedFiles = append(clonedFiles, models.AppGeneratedFile{
				AppProjectID: clone.ID,
				Platform:     f.Platform,
				FilePath:     f.FilePath,
				Content:      f.Content,
				FileType:     f.FileType,
				IsGenerated:  f.IsGenerated,
			})
		}
		if err := c.DB.BulkCreateAppGeneratedFiles(clonedFiles); err != nil {
			slog.Error("Failed to bulk copy files to mobile clone", "error", err)
			return nil, fmt.Errorf("failed to copy files to mobile project")
		}
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

	// If slug changed and a Vercel project exists, reassign the wemob.app subdomain
	if oldSlug != req.Slug && c.VercelService != nil {
		go func() {
			vp, vpErr := c.DB.GetVercelProjectByAppProjectID(id)
			if vpErr != nil || vp == nil {
				return
			}

			oldDomain := oldSlug + "." + constants.WEMOB_APP_BASE_DOMAIN
			newDomain := req.Slug + "." + constants.WEMOB_APP_BASE_DOMAIN

			if err := c.VercelService.RemoveDomain(vp.VercelProjectID, oldDomain); err != nil {
				slog.Warn("Failed to remove old wemob.app subdomain", "domain", oldDomain, "error", err)
			}
			if err := c.VercelService.AddDomain(vp.VercelProjectID, newDomain); err != nil {
				slog.Warn("Failed to add new wemob.app subdomain", "domain", newDomain, "error", err)
				return
			}

			vp.CustomDomain = newDomain
			vp.ProductionURL = "https://" + newDomain
			c.DB.UpdateVercelProject(vp)
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
		if neonInteg := c.getNeonIntegration(project.ID.String()); neonInteg != nil {
			if neonProjectID, ok := neonInteg.Config["project_id"].(string); ok && neonProjectID != "" {
				if err := c.NeonService.DeleteProject(neonProjectID); err != nil {
					slog.Warn("Failed to delete Neon project during app deletion", "error", err, "neon_project_id", neonProjectID, "app_project_id", project.ID)
				} else {
					slog.Info("Deleted Neon project", "neon_project_id", neonProjectID, "app_project_id", project.ID)
				}
			}
		}
	}

	return c.DB.DeleteAppProject(project)
}

func (c *Controller) UpdateAppProjectStatus(project *models.AppProject, status string) (*models.AppProject, error) {
	project.Status = status
	project.UpdatedAt = time.Now()
	return c.DB.UpdateAppProject(project)
}

// Snapshots

func (c *Controller) GetAppSnapshots(projectID string) ([]models.AppSnapshot, error) {
	return c.DB.GetAppSnapshotsByProjectID(projectID)
}

func (c *Controller) CreateAppSnapshot(project *models.AppProject, description string, source string) (*models.AppSnapshot, error) {
	// Capture current generated files
	files, err := c.DB.GetAppGeneratedFilesByProjectID(project.ID.String(), "nextjs")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch files for snapshot: %w", err)
	}
	var snapshotFiles []models.SnapshotFile
	for _, f := range files {
		snapshotFiles = append(snapshotFiles, models.SnapshotFile{
			FilePath: f.FilePath,
			Content:  f.Content,
			FileType: f.FileType,
		})
	}
	filesJSON, err := json.Marshal(snapshotFiles)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal snapshot files: %w", err)
	}

	snapshot := &models.AppSnapshot{
		AppProjectID: project.ID,
		AST:          project.AST,
		Theme:        project.Theme,
		Files:        filesJSON,
		Description:  description,
		Source:       source,
	}
	return c.DB.CreateAppSnapshot(snapshot)
}

func (c *Controller) RestoreAppSnapshot(project *models.AppProject, snapshotID string) (*models.AppProject, error) {
	snapshot, err := c.DB.GetAppSnapshotByID(snapshotID)
	if err != nil {
		return nil, fmt.Errorf("snapshot not found")
	}
	if snapshot.AppProjectID != project.ID {
		return nil, fmt.Errorf("snapshot does not belong to this project")
	}

	// Create snapshot of current state before restoring (B5: abort if auto-save fails)
	_, err = c.CreateAppSnapshot(project, "Auto-save before restore", "auto")
	if err != nil {
		return nil, fmt.Errorf("failed to create auto-save before restore: %w", err)
	}
	// Clean up old auto-snapshots (B7: keep only latest 20)
	c.DB.DeleteOldAutoSnapshots(project.ID.String(), 20)

	project.AST = snapshot.AST
	project.Theme = snapshot.Theme
	project.UpdatedAt = time.Now()

	// Restore files if the snapshot captured them
	if len(snapshot.Files) > 2 { // more than just "[]"
		var snapshotFiles []models.SnapshotFile
		if err := json.Unmarshal(snapshot.Files, &snapshotFiles); err == nil && len(snapshotFiles) > 0 {
			// Delete current generated files in a single query (B2)
			if err := c.DB.DeleteAppGeneratedFilesByProjectPlatform(project.ID.String(), "nextjs"); err != nil {
				return nil, fmt.Errorf("failed to delete current files during restore: %w", err)
			}
			// Recreate from snapshot (B4: check errors, log failures)
			var failedFiles []string
			for _, sf := range snapshotFiles {
				if sf.FilePath == "" {
					continue // skip entries with empty paths
				}
				file := &models.AppGeneratedFile{
					AppProjectID: project.ID,
					Platform:     "nextjs",
					FilePath:     sf.FilePath,
					Content:      sf.Content,
					FileType:     sf.FileType,
					IsGenerated:  true,
				}
				if _, err := c.DB.CreateAppGeneratedFile(file); err != nil {
					slog.Error("Failed to restore file from snapshot", "file_path", sf.FilePath, "error", err)
					failedFiles = append(failedFiles, sf.FilePath)
				}
			}
			slog.Info("Restored files from snapshot", "project_id", project.ID, "snapshot_id", snapshotID,
				"file_count", len(snapshotFiles), "failed_count", len(failedFiles))
			// If more than half the files failed, the snapshot is effectively
			// corrupted — return an error so the caller knows.
			if len(failedFiles) > 0 && len(failedFiles) > len(snapshotFiles)/2 {
				return nil, fmt.Errorf("snapshot restore failed: %d of %d files could not be recreated", len(failedFiles), len(snapshotFiles))
			}
		}
	}

	updated, err := c.DB.UpdateAppProject(project)
	if err != nil {
		return nil, err
	}

	return updated, nil
}

func (c *Controller) DeleteAppSnapshot(project *models.AppProject, snapshotID string) error {
	snapshot, err := c.DB.GetAppSnapshotByID(snapshotID)
	if err != nil {
		return fmt.Errorf("snapshot not found")
	}
	if snapshot.AppProjectID != project.ID {
		return fmt.Errorf("snapshot does not belong to this project")
	}
	return c.DB.DeleteAppSnapshot(snapshotID)
}

// Generated Files

func (c *Controller) GetAppGeneratedFiles(projectID string, platform string) ([]models.AppGeneratedFile, error) {
	return c.DB.GetAppGeneratedFilesByProjectID(projectID, platform)
}

// File CRUD

func (c *Controller) CreateAppFile(project *models.AppProject, filePath string, content string, fileType string) (*models.AppGeneratedFile, error) {
	// Check if file already exists at this path
	if _, err := c.DB.GetAppGeneratedFileByPath(project.ID.String(), "nextjs", filePath); err == nil {
		return nil, fmt.Errorf("a file already exists at this path")
	}

	file := &models.AppGeneratedFile{
		AppProjectID: project.ID,
		Platform:     "nextjs",
		FilePath:     filePath,
		Content:      content,
		FileType:     fileType,
		IsGenerated:  false,
	}
	return c.DB.CreateAppGeneratedFile(file)
}

func (c *Controller) UpdateAppFile(project *models.AppProject, fileID string, content string) (*models.AppGeneratedFile, error) {
	file, err := c.DB.GetAppGeneratedFileByID(fileID)
	if err != nil {
		return nil, fmt.Errorf("file not found")
	}
	if file.AppProjectID != project.ID {
		return nil, fmt.Errorf("file does not belong to this project")
	}

	file.Content = content
	file.UpdatedAt = time.Now()
	return c.DB.UpdateAppGeneratedFile(file)
}

func (c *Controller) DeleteAppFile(project *models.AppProject, fileID string) error {
	file, err := c.DB.GetAppGeneratedFileByID(fileID)
	if err != nil {
		return fmt.Errorf("file not found")
	}
	if file.AppProjectID != project.ID {
		return fmt.Errorf("file does not belong to this project")
	}
	return c.DB.DeleteAppGeneratedFile(file)
}

// Export

func (c *Controller) ExportAppAsZIP(projectID string, platform string) ([]models.AppGeneratedFile, error) {
	return c.DB.GetAppGeneratedFilesByProjectID(projectID, platform)
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
