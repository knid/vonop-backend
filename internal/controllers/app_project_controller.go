package controllers

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
)

func (c *Controller) CreateAppProject(user *models.User, req requests.GenerateAppRequest) (*models.AppProject, error) {
	validate := validator.New()
	if err := req.Validate(validate); err != nil {
		return nil, err
	}

	projectType := "app"
	if req.ProjectType != "" {
		projectType = req.ProjectType
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

func (c *Controller) GetAppProjectsByUser(user *models.User) ([]models.AppProject, error) {
	return c.DB.GetAppProjectsByUserID(user.ID.String())
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

	project.Name = req.Name
	project.Slug = req.Slug
	project.UpdatedAt = time.Now()

	return c.DB.UpdateAppProject(project)
}

func (c *Controller) DeleteAppProject(id string, user *models.User) error {
	project, err := c.GetAppProject(id, user)
	if err != nil {
		return err
	}
	return c.DB.DeleteAppProject(project)
}

func (c *Controller) UpdateAppProjectAST(project *models.AppProject, ast []byte) (*models.AppProject, error) {
	project.AST = ast
	project.UpdatedAt = time.Now()
	return c.DB.UpdateAppProject(project)
}

func (c *Controller) UpdateAppProjectTheme(project *models.AppProject, theme []byte) (*models.AppProject, error) {
	project.Theme = theme
	project.UpdatedAt = time.Now()
	return c.DB.UpdateAppProject(project)
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
	snapshot := &models.AppSnapshot{
		AppProjectID: project.ID,
		AST:          project.AST,
		Theme:        project.Theme,
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

	// Create snapshot of current state before restoring
	c.CreateAppSnapshot(project, "Auto-save before restore", "auto")

	project.AST = snapshot.AST
	project.Theme = snapshot.Theme
	project.UpdatedAt = time.Now()
	return c.DB.UpdateAppProject(project)
}

// Generated Files

func (c *Controller) GetAppGeneratedFiles(projectID string, platform string) ([]models.AppGeneratedFile, error) {
	return c.DB.GetAppGeneratedFilesByProjectID(projectID, platform)
}

func (c *Controller) SaveGeneratedFiles(projectID uuid.UUID, platform string, files []struct {
	Path     string
	Content  string
	FileType string
}) error {
	// Delete existing files for this platform
	c.DB.DeleteAppGeneratedFilesByProjectPlatform(projectID.String(), platform)

	var dbFiles []models.AppGeneratedFile
	for _, f := range files {
		dbFiles = append(dbFiles, models.AppGeneratedFile{
			AppProjectID: projectID,
			Platform:     platform,
			FilePath:     f.Path,
			Content:      f.Content,
			FileType:     f.FileType,
			IsGenerated:  true,
		})
	}

	return c.DB.BulkCreateAppGeneratedFiles(dbFiles)
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
