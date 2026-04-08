package controllers

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
)

func (c *Controller) CreateAppTemplate(user *models.User, req requests.CreateAppTemplateRequest) (*models.AppTemplate, error) {
	if user.Type != "admin" {
		return nil, errors.New("admin access required")
	}

	validate := validator.New()
	if err := req.Validate(validate); err != nil {
		return nil, err
	}

	themeJSON := []byte("{}")
	if req.Theme != nil {
		if data, err := json.Marshal(req.Theme); err == nil {
			themeJSON = data
		}
	}

	dataModelsJSON := []byte("[]")
	if req.DataModels != nil {
		if data, err := json.Marshal(req.DataModels); err == nil {
			dataModelsJSON = data
		}
	}

	settings := map[string]any{}
	if req.Settings != nil {
		settings = req.Settings
	}

	tags := req.Tags
	if tags == nil {
		tags = []string{}
	}

	template := &models.AppTemplate{
		Name:         req.Name,
		Slug:         req.Slug,
		Description:  req.Description,
		Tags:         tags,
		TemplateType: req.TemplateType,
		ProjectType:  req.ProjectType,
		Prompt:       req.Prompt,
		Theme:        themeJSON,
		DataModels:   dataModelsJSON,
		Settings:     settings,
		Published:    false,
		CreatedByID:  user.ID,
	}

	template, err := c.DB.CreateAppTemplate(template)
	if err != nil {
		slog.Error("Failed to create app template", "error", err)
		return nil, fmt.Errorf("failed to create template")
	}

	// Initialize git repo for this template
	if _, initErr := c.GitRepoService.Init(template.ID.String()); initErr != nil {
		slog.Error("Failed to initialize git repo for template", "error", initErr, "template_id", template.ID)
	} else {
		template.SourceRepoID = template.ID.String()
		if _, updateErr := c.DB.UpdateAppTemplate(template); updateErr != nil {
			slog.Error("Failed to persist SourceRepoID on template", "error", updateErr, "template_id", template.ID)
		}
	}

	return template, nil
}

func (c *Controller) GetAppTemplate(id string, user *models.User) (*models.AppTemplate, error) {
	if user.Type != "admin" {
		return nil, errors.New("admin access required")
	}
	return c.DB.GetAppTemplateByID(id)
}

func (c *Controller) UpdateAppTemplate(id string, user *models.User, req requests.UpdateAppTemplateRequest) (*models.AppTemplate, error) {
	if user.Type != "admin" {
		return nil, errors.New("admin access required")
	}

	validate := validator.New()
	if err := req.Validate(validate); err != nil {
		return nil, err
	}

	template, err := c.DB.GetAppTemplateByID(id)
	if err != nil {
		return nil, fmt.Errorf("template not found")
	}

	if req.Name != "" {
		template.Name = req.Name
	}
	if req.Slug != "" {
		template.Slug = req.Slug
	}
	if req.Description != "" {
		template.Description = req.Description
	}
	if req.Tags != nil {
		template.Tags = req.Tags
	}
	if req.TemplateType != "" {
		template.TemplateType = req.TemplateType
	}
	if req.ProjectType != "" {
		template.ProjectType = req.ProjectType
	}
	if req.Prompt != "" {
		template.Prompt = req.Prompt
	}
	if req.PreviewImageKey != "" {
		template.PreviewImageKey = req.PreviewImageKey
	}
	if req.PreviewURL != "" {
		template.PreviewURL = req.PreviewURL
	}
	if req.Theme != nil {
		if data, err := json.Marshal(req.Theme); err == nil {
			template.Theme = data
		}
	}
	if req.DataModels != nil {
		if data, err := json.Marshal(req.DataModels); err == nil {
			template.DataModels = data
		}
	}
	if req.Settings != nil {
		template.Settings = req.Settings
	}

	return c.DB.UpdateAppTemplate(template)
}

func (c *Controller) DeleteAppTemplate(id string, user *models.User) error {
	if user.Type != "admin" {
		return errors.New("admin access required")
	}
	template, err := c.DB.GetAppTemplateByID(id)
	if err != nil {
		return fmt.Errorf("template not found")
	}
	return c.DB.DeleteAppTemplate(template)
}

func (c *Controller) PublishAppTemplate(id string, user *models.User, req requests.PublishAppTemplateRequest) (*models.AppTemplate, error) {
	if user.Type != "admin" {
		return nil, errors.New("admin access required")
	}
	template, err := c.DB.GetAppTemplateByID(id)
	if err != nil {
		return nil, fmt.Errorf("template not found")
	}
	template.Published = req.Published
	return c.DB.UpdateAppTemplate(template)
}

func (c *Controller) SortAppTemplate(id string, user *models.User, req requests.SortAppTemplateRequest) (*models.AppTemplate, error) {
	if user.Type != "admin" {
		return nil, errors.New("admin access required")
	}
	template, err := c.DB.GetAppTemplateByID(id)
	if err != nil {
		return nil, fmt.Errorf("template not found")
	}
	template.SortOrder = req.SortOrder
	return c.DB.UpdateAppTemplate(template)
}

func (c *Controller) ListAllTemplates(user *models.User) ([]models.AppTemplate, error) {
	if user.Type != "admin" {
		return nil, errors.New("admin access required")
	}
	return c.DB.ListAllTemplates()
}

// Public methods (no auth required)

func (c *Controller) ListPublishedTemplates(tag, templateType string) ([]models.AppTemplate, error) {
	return c.DB.ListPublishedTemplates(tag, templateType)
}

func (c *Controller) GetPublishedTemplate(slug string) (*models.AppTemplate, error) {
	template, err := c.DB.GetAppTemplateBySlug(slug)
	if err != nil {
		return nil, fmt.Errorf("template not found")
	}
	if !template.Published {
		return nil, fmt.Errorf("template not found")
	}
	return template, nil
}

func (c *Controller) CopyTemplate(user *models.User, slug string) (*models.AppProject, error) {
	template, err := c.DB.GetAppTemplateBySlug(slug)
	if err != nil || !template.Published {
		return nil, fmt.Errorf("template not found")
	}

	// Enforce app project limit (same logic as CreateAppProject)
	isMobile := template.ProjectType == "mobile_app"
	limits, limErr := c.GetUserLimits(user)
	if limErr != nil {
		slog.Warn("CopyTemplate: failed to get user limits", "error", limErr, "user_id", user.ID)
	} else {
		var maxApps int
		var countFilter string
		if isMobile {
			maxApps = limits.MaxMobileApps
			countFilter = "mobile_app"
		} else {
			maxApps = limits.MaxWebApps
			countFilter = ""
		}

		if maxApps == 0 {
			if isMobile {
				return nil, fmt.Errorf("mobile app creation is not available on your plan")
			}
			return nil, fmt.Errorf("web app creation is not available on your plan")
		}

		appCount, countErr := c.DB.CountAppProjectsByUserIDAndType(user.ID.String(), countFilter)
		if countErr != nil {
			slog.Warn("CopyTemplate: failed to count projects", "error", countErr, "user_id", user.ID)
		} else if appCount >= maxApps {
			if isMobile {
				return nil, fmt.Errorf("you have reached your mobile app limit (%d)", maxApps)
			}
			return nil, fmt.Errorf("you have reached your web app limit (%d)", maxApps)
		}
	}

	projectSlug := generateAppSlug(template.Name)

	project := &models.AppProject{
		UserID:      user.ID,
		Name:        template.Name,
		Slug:        projectSlug,
		Prompt:      template.Prompt,
		ProjectType: template.ProjectType,
		Theme:       template.Theme,
		DataModels:  template.DataModels,
		Settings:    template.Settings,
		Status:      "ready",
		TemplateID:  &template.ID,
	}

	project, err = c.DB.CreateAppProject(project)
	if err != nil {
		slog.Error("Failed to create project from template", "error", err)
		return nil, fmt.Errorf("failed to create project")
	}

	// Clone template git repo into new project
	if template.SourceRepoID != "" {
		if _, cloneErr := c.GitRepoService.Clone(template.SourceRepoID, project.ID.String()); cloneErr != nil {
			slog.Error("Failed to clone template git repo", "error", cloneErr,
				"template_id", template.ID, "project_id", project.ID)
			return nil, fmt.Errorf("failed to copy template files")
		}
	} else {
		// Initialize empty repo if template has no source
		if _, initErr := c.GitRepoService.Init(project.ID.String()); initErr != nil {
			slog.Error("Failed to init git repo for template copy", "error", initErr, "project_id", project.ID)
		}
	}

	return project, nil
}

// ToTemplateResponse converts an AppTemplate to a list response item.
func ToTemplateResponse(t *models.AppTemplate) responses.AppTemplateResponse {
	tags := t.Tags
	if tags == nil {
		tags = []string{}
	}
	return responses.AppTemplateResponse{
		ID:              t.ID,
		Name:            t.Name,
		Slug:            t.Slug,
		Description:     t.Description,
		Tags:            tags,
		TemplateType:    t.TemplateType,
		PreviewImageKey: t.PreviewImageKey,
		PreviewURL:      t.PreviewURL,
		ProjectType:     t.ProjectType,
		Published:       t.Published,
		SortOrder:       t.SortOrder,
		CreatedAt:       t.CreatedAt,
	}
}

// ToTemplateDetailResponse converts an AppTemplate to a detail response.
func ToTemplateDetailResponse(t *models.AppTemplate) responses.AppTemplateDetailResponse {
	tags := t.Tags
	if tags == nil {
		tags = []string{}
	}
	theme := map[string]any{}
	if len(t.Theme) > 0 {
		json.Unmarshal(t.Theme, &theme)
	}
	var dataModels []any
	if len(t.DataModels) > 0 {
		json.Unmarshal(t.DataModels, &dataModels)
	}
	if dataModels == nil {
		dataModels = []any{}
	}
	settings := t.Settings
	if settings == nil {
		settings = map[string]any{}
	}
	return responses.AppTemplateDetailResponse{
		ID:              t.ID,
		Name:            t.Name,
		Slug:            t.Slug,
		Description:     t.Description,
		Tags:            tags,
		TemplateType:    t.TemplateType,
		PreviewImageKey: t.PreviewImageKey,
		PreviewURL:      t.PreviewURL,
		Theme:           theme,
		DataModels:      dataModels,
		Settings:        settings,
		ProjectType:     t.ProjectType,
		Prompt:          t.Prompt,
		Published:       t.Published,
		SortOrder:       t.SortOrder,
		SourceRepoID:    t.SourceRepoID,
		CreatedAt:       t.CreatedAt,
		UpdatedAt:       t.UpdatedAt,
	}
}
