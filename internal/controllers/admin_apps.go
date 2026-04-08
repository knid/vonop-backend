package controllers

import (
	"fmt"
	"time"

	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
)

// AdminListApps returns a paginated list of app projects with owner info.
func (c *Controller) AdminListApps(params requests.AdminListParams) (*responses.PaginatedResponse, error) {
	params.Defaults()

	dbParams := database.AdminAppListParams{
		Page:     params.Page,
		PageSize: params.PageSize,
		Search:   params.Search,
		Status:   params.Status,
		SortDir:  params.SortDir,
		From:     params.From,
		To:       params.To,
	}

	apps, total, err := c.DB.AdminListApps(dbParams)
	if err != nil {
		return nil, fmt.Errorf("failed to list apps: %w", err)
	}

	items := make([]responses.AdminAppResponse, len(apps))
	for i, a := range apps {
		userEmail := ""
		if a.User != nil {
			userEmail = a.User.Email
		}
		items[i] = responses.AdminAppResponse{
			ID:          a.ID,
			UserID:      a.UserID,
			UserEmail:   userEmail,
			Name:        a.Name,
			Slug:        a.Slug,
			ProjectType: a.ProjectType,
			Status:      a.Status,
			CreatedAt:   a.CreatedAt,
			UpdatedAt:   a.UpdatedAt,
		}
	}

	totalPages := total / params.PageSize
	if total%params.PageSize != 0 {
		totalPages++
	}

	return &responses.PaginatedResponse{
		Data:       items,
		Total:      total,
		Page:       params.Page,
		PageSize:   params.PageSize,
		TotalPages: totalPages,
	}, nil
}

// AdminGetApp returns detailed info for a single app project.
func (c *Controller) AdminGetApp(appID string) (*responses.AdminAppDetailResponse, error) {
	app, err := c.DB.AdminGetAppByID(appID)
	if err != nil {
		return nil, fmt.Errorf("app not found: %w", err)
	}

	userEmail := ""
	if app.User != nil {
		userEmail = app.User.Email
	}

	return &responses.AdminAppDetailResponse{
		AdminAppResponse: responses.AdminAppResponse{
			ID:          app.ID,
			UserID:      app.UserID,
			UserEmail:   userEmail,
			Name:        app.Name,
			Slug:        app.Slug,
			ProjectType: app.ProjectType,
			Status:      app.Status,
			CreatedAt:   app.CreatedAt,
			UpdatedAt:   app.UpdatedAt,
		},
		Prompt:          app.Prompt,
		Settings:        app.Settings,
		ScreenshotS3Key: app.ScreenshotS3Key,
		SandboxID:       app.SandboxID,
		TemplateID:      app.TemplateID,
	}, nil
}

// AdminUpdateApp updates an app project's status and/or settings.
func (c *Controller) AdminUpdateApp(appID string, req requests.AdminUpdateAppRequest) (*responses.AdminAppDetailResponse, error) {
	app, err := c.DB.GetAppProjectByID(appID)
	if err != nil {
		return nil, fmt.Errorf("app not found: %w", err)
	}

	if req.Status != nil {
		app.Status = *req.Status
	}
	if req.Settings != nil {
		app.Settings = req.Settings
	}

	app.UpdatedAt = time.Now()
	if _, err := c.DB.UpdateAppProject(app); err != nil {
		return nil, fmt.Errorf("failed to update app: %w", err)
	}

	// Re-fetch with relations
	return c.AdminGetApp(appID)
}

// AdminDeleteApp soft-deletes an app project.
func (c *Controller) AdminDeleteApp(appID string) error {
	app, err := c.DB.GetAppProjectByID(appID)
	if err != nil {
		return fmt.Errorf("app not found: %w", err)
	}
	if err := c.DB.DeleteAppProject(app); err != nil {
		return fmt.Errorf("failed to delete app: %w", err)
	}
	return nil
}
