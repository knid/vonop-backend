package controllers

import (
	"context"
	"fmt"
	"time"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/sandbox"
)

func (c *Controller) SandboxStatus(ctx context.Context, project *models.AppProject) (*sandbox.Status, error) {
	if c.SandboxManager == nil {
		return nil, fmt.Errorf("sandbox manager is not configured")
	}
	return c.SandboxManager.Status(ctx, project)
}

func (c *Controller) StartSandbox(ctx context.Context, project *models.AppProject) (*sandbox.Status, error) {
	if c.SandboxManager == nil {
		return nil, fmt.Errorf("sandbox manager is not configured")
	}
	files, err := c.sandboxSourceFiles(project)
	if err != nil {
		return nil, err
	}
	envVars := c.CollectAllEnvVars(project)
	status, err := c.SandboxManager.Start(ctx, project, files, envVars)
	if err != nil {
		return nil, err
	}
	if project.SandboxID != "" {
		_ = c.DB.UpdateAppProjectSandboxID(project.ID.String(), project.SandboxID)
	}
	return status, nil
}

func (c *Controller) SyncSandbox(ctx context.Context, project *models.AppProject) error {
	if c.SandboxManager == nil {
		return fmt.Errorf("sandbox manager is not configured")
	}
	files, err := c.sandboxSourceFiles(project)
	if err != nil {
		return err
	}
	return c.SandboxManager.Sync(ctx, project, files)
}

func (c *Controller) OpenSandboxRoute(ctx context.Context, project *models.AppProject, route, setBy string) (*sandbox.PreviewTarget, error) {
	if c.SandboxManager == nil {
		return nil, fmt.Errorf("sandbox manager is not configured")
	}
	target, err := c.SandboxManager.OpenRoute(ctx, project, route, setBy)
	if err != nil {
		return nil, err
	}
	if project.Settings == nil {
		project.Settings = map[string]any{}
	}
	stored := map[string]any{
		"url":        target.URL,
		"label":      target.Label,
		"kind":       string(target.Kind),
		"set_by":     target.SetBy,
		"updated_at": target.UpdatedAt.Format(time.RFC3339),
	}
	project.Settings["preview_target"] = stored
	return target, c.DB.UpdateAppProjectPreviewTarget(project.ID.String(), stored)
}

func (c *Controller) sandboxSourceFiles(project *models.AppProject) (map[string]string, error) {
	if project == nil {
		return nil, fmt.Errorf("project is required")
	}
	repo, err := c.GitRepoService.Open(project.ID.String())
	if err != nil {
		return nil, fmt.Errorf("no generated files found")
	}
	fileMap, err := repo.ReadAllFiles()
	if err != nil || len(fileMap) == 0 {
		return nil, fmt.Errorf("no generated files found")
	}
	if project.ProjectType == models.ProjectTypeMobileAppNative {
		mobileFiles := stripPreviewSubtree(fileMap, "mobile/")
		if len(mobileFiles) == 0 {
			return nil, fmt.Errorf("no mobile files found")
		}
		return mobileFiles, nil
	}
	return fileMap, nil
}
