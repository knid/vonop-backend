# Template Marketplace Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a template marketplace where users can browse, preview, and copy ready-made web/mobile apps as starting points for their own projects.

**Architecture:** New `AppTemplate` model with dedicated CRUD, public listing endpoints, and a copy flow that clones template data + git repo into a new `AppProject`. Admin endpoints behind existing `RequireAdmin` middleware; public endpoints with no auth; copy endpoint with standard user auth.

**Tech Stack:** Go, chi/v5, Bun ORM, PostgreSQL (jsonb for tags), existing `GitRepoService` for repo cloning.

---

### Task 1: AppTemplate Model + TemplateID on AppProject

**Files:**
- Create: `internal/models/app_template.go`
- Modify: `internal/models/app_project.go`
- Modify: `cmd/main.go` (createSchema + alter statements)

- [ ] **Step 1: Create the AppTemplate model**

Create `internal/models/app_template.go`:

```go
package models

import (
	"time"

	"github.com/google/uuid"
)

type AppTemplate struct {
	ID              uuid.UUID      `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	Name            string         `bun:",notnull"`
	Slug            string         `bun:",notnull,unique"`
	Description     string         `bun:",type:text,default:''"`
	Tags            []string       `bun:",type:jsonb,default:'[]'"`
	TemplateType    string         `bun:",notnull,default:'web'"` // "web", "mobile", "both"
	PreviewImageKey string         `bun:",default:''"`
	PreviewURL      string         `bun:",default:''"`
	Theme           []byte         `bun:",type:jsonb,default:'{}'"`
	DataModels      []byte         `bun:",type:jsonb,default:'[]'"`
	Settings        map[string]any `bun:",type:jsonb,default:'{}'"`
	ProjectType     string         `bun:",notnull,default:'app'"` // "app", "ecommerce", "mobile_app"
	Prompt          string         `bun:",type:text,default:''"`
	SourceRepoID    string         `bun:",default:''"`
	Published       bool           `bun:",notnull,default:false"`
	CreatedByID     uuid.UUID      `bun:",notnull,type:uuid"`
	CreatedBy       *User          `bun:"rel:belongs-to,join:created_by_id=id"`
	SortOrder       int            `bun:",notnull,default:0"`
	CreatedAt       time.Time      `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt       time.Time      `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt       time.Time      `bun:",soft_delete,nullzero"`
}
```

- [ ] **Step 2: Add TemplateID to AppProject**

In `internal/models/app_project.go`, add the `TemplateID` field after `Status`:

```go
TemplateID      *uuid.UUID     `bun:",type:uuid,nullzero"` // source template if created from a template
```

- [ ] **Step 3: Register AppTemplate in createSchema**

In `cmd/main.go`, inside the `createSchema` function, add to the models slice after the `(*models.AppProjectMemory)(nil),` line:

```go
// Template marketplace
(*models.AppTemplate)(nil),
```

- [ ] **Step 4: Add alter statement for TemplateID on app_projects**

In `cmd/main.go`, add to the `alterStatements` slice:

```go
// Template marketplace: track which template a project was copied from
`ALTER TABLE app_projects ADD COLUMN IF NOT EXISTS template_id uuid`,
```

- [ ] **Step 5: Commit**

```bash
git add internal/models/app_template.go internal/models/app_project.go cmd/main.go
git commit -m "feat(templates): add AppTemplate model and TemplateID on AppProject"
```

---

### Task 2: Database Layer for Templates

**Files:**
- Create: `internal/database/app_templates.go`

- [ ] **Step 1: Create database queries**

Create `internal/database/app_templates.go`:

```go
package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
	"github.com/uptrace/bun"
)

func (db *Database) CreateAppTemplate(t *models.AppTemplate) (*models.AppTemplate, error) {
	_, err := db.DB.NewInsert().Model(t).Exec(context.Background())
	return t, err
}

func (db *Database) GetAppTemplateByID(id string) (*models.AppTemplate, error) {
	var t models.AppTemplate
	err := db.DB.NewSelect().Model(&t).Where("id = ?", id).Scan(context.Background())
	return &t, err
}

func (db *Database) GetAppTemplateBySlug(slug string) (*models.AppTemplate, error) {
	var t models.AppTemplate
	err := db.DB.NewSelect().Model(&t).Where("slug = ?", slug).Scan(context.Background())
	return &t, err
}

func (db *Database) UpdateAppTemplate(t *models.AppTemplate) (*models.AppTemplate, error) {
	_, err := db.DB.NewUpdate().Model(t).Where("id = ?", t.ID).Exec(context.Background())
	return t, err
}

func (db *Database) DeleteAppTemplate(t *models.AppTemplate) error {
	_, err := db.DB.NewDelete().Model(t).Where("id = ?", t.ID).Exec(context.Background())
	return err
}

// ListPublishedTemplates returns published templates with optional tag and type filtering.
// Results are ordered by sort_order ASC, then created_at DESC.
func (db *Database) ListPublishedTemplates(tag, templateType string) ([]models.AppTemplate, error) {
	var templates []models.AppTemplate
	q := db.DB.NewSelect().Model(&templates).
		Where("published = ?", true)
	if tag != "" {
		q = q.Where("tags @> ?", bun.Safe("'[\""+tag+"\"]'"))
	}
	if templateType != "" {
		q = q.Where("template_type = ?", templateType)
	}
	err := q.Order("sort_order ASC", "created_at DESC").Scan(context.Background())
	return templates, err
}

// ListAllTemplates returns all templates (including unpublished) for admin use.
func (db *Database) ListAllTemplates() ([]models.AppTemplate, error) {
	var templates []models.AppTemplate
	err := db.DB.NewSelect().Model(&templates).
		Order("sort_order ASC", "created_at DESC").
		Scan(context.Background())
	return templates, err
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/database/...
```

- [ ] **Step 3: Commit**

```bash
git add internal/database/app_templates.go
git commit -m "feat(templates): add database layer for app templates"
```

---

### Task 3: Request and Response Structs

**Files:**
- Create: `internal/requests/app_template_requests.go`
- Create: `internal/responses/app_template_responses.go`

- [ ] **Step 1: Create request structs**

Create `internal/requests/app_template_requests.go`:

```go
package requests

import (
	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/utils"
)

type CreateAppTemplateRequest struct {
	Name         string         `json:"name" validate:"required,min=2,max=100" errormgs:"must be between 2 and 100 characters"`
	Slug         string         `json:"slug" validate:"required,min=2,max=100" errormgs:"must be between 2 and 100 characters"`
	Description  string         `json:"description" validate:"omitempty,max=2000" errormgs:"must be at most 2000 characters"`
	Tags         []string       `json:"tags"`
	TemplateType string         `json:"template_type" validate:"required,oneof=web mobile both" errormgs:"must be 'web', 'mobile', or 'both'"`
	ProjectType  string         `json:"project_type" validate:"required,oneof=app ecommerce mobile_app" errormgs:"must be 'app', 'ecommerce', or 'mobile_app'"`
	Prompt       string         `json:"prompt"`
	Theme        map[string]any `json:"theme"`
	DataModels   []any          `json:"data_models"`
	Settings     map[string]any `json:"settings"`
}

func (r *CreateAppTemplateRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[CreateAppTemplateRequest](*r, validate)
}

type UpdateAppTemplateRequest struct {
	Name            string         `json:"name" validate:"omitempty,min=2,max=100" errormgs:"must be between 2 and 100 characters"`
	Slug            string         `json:"slug" validate:"omitempty,min=2,max=100" errormgs:"must be between 2 and 100 characters"`
	Description     string         `json:"description" validate:"omitempty,max=2000" errormgs:"must be at most 2000 characters"`
	Tags            []string       `json:"tags"`
	TemplateType    string         `json:"template_type" validate:"omitempty,oneof=web mobile both" errormgs:"must be 'web', 'mobile', or 'both'"`
	ProjectType     string         `json:"project_type" validate:"omitempty,oneof=app ecommerce mobile_app" errormgs:"must be 'app', 'ecommerce', or 'mobile_app'"`
	Prompt          string         `json:"prompt"`
	PreviewImageKey string         `json:"preview_image_key"`
	PreviewURL      string         `json:"preview_url"`
	Theme           map[string]any `json:"theme"`
	DataModels      []any          `json:"data_models"`
	Settings        map[string]any `json:"settings"`
}

func (r *UpdateAppTemplateRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[UpdateAppTemplateRequest](*r, validate)
}

type PublishAppTemplateRequest struct {
	Published bool `json:"published"`
}

type SortAppTemplateRequest struct {
	SortOrder int `json:"sort_order" validate:"min=0" errormgs:"must be 0 or greater"`
}

func (r *SortAppTemplateRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[SortAppTemplateRequest](*r, validate)
}
```

- [ ] **Step 2: Create response structs**

Create `internal/responses/app_template_responses.go`:

```go
package responses

import (
	"time"

	"github.com/google/uuid"
)

type AppTemplateResponse struct {
	ID              uuid.UUID      `json:"id"`
	Name            string         `json:"name"`
	Slug            string         `json:"slug"`
	Description     string         `json:"description"`
	Tags            []string       `json:"tags"`
	TemplateType    string         `json:"template_type"`
	PreviewImageKey string         `json:"preview_image_key"`
	PreviewURL      string         `json:"preview_url"`
	ProjectType     string         `json:"project_type"`
	Published       bool           `json:"published,omitempty"`
	SortOrder       int            `json:"sort_order,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
}

type AppTemplateDetailResponse struct {
	ID              uuid.UUID      `json:"id"`
	Name            string         `json:"name"`
	Slug            string         `json:"slug"`
	Description     string         `json:"description"`
	Tags            []string       `json:"tags"`
	TemplateType    string         `json:"template_type"`
	PreviewImageKey string         `json:"preview_image_key"`
	PreviewURL      string         `json:"preview_url"`
	Theme           map[string]any `json:"theme"`
	Settings        map[string]any `json:"settings"`
	ProjectType     string         `json:"project_type"`
	Prompt          string         `json:"prompt"`
	Published       bool           `json:"published,omitempty"`
	SortOrder       int            `json:"sort_order,omitempty"`
	SourceRepoID    string         `json:"source_repo_id,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

type AppTemplateListResponse struct {
	Templates []AppTemplateResponse `json:"templates"`
}
```

- [ ] **Step 3: Verify it compiles**

```bash
go build ./internal/requests/... ./internal/responses/...
```

- [ ] **Step 4: Commit**

```bash
git add internal/requests/app_template_requests.go internal/responses/app_template_responses.go
git commit -m "feat(templates): add request and response structs"
```

---

### Task 4: Controller — Admin CRUD + Publish

**Files:**
- Create: `internal/controllers/app_template_controller.go`

- [ ] **Step 1: Create the controller with admin CRUD methods**

Create `internal/controllers/app_template_controller.go`:

```go
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
		c.DB.UpdateAppTemplate(template)
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
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/controllers/...
```

- [ ] **Step 3: Commit**

```bash
git add internal/controllers/app_template_controller.go
git commit -m "feat(templates): add controller with admin CRUD and public listing"
```

---

### Task 5: Controller — Copy Template Flow

**Files:**
- Modify: `internal/controllers/app_template_controller.go`

- [ ] **Step 1: Add CopyTemplate method**

Add this method to the end of `internal/controllers/app_template_controller.go` (before the closing `ToTemplateResponse` helpers):

```go
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
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/controllers/...
```

- [ ] **Step 3: Commit**

```bash
git add internal/controllers/app_template_controller.go
git commit -m "feat(templates): add copy template flow with limit enforcement"
```

---

### Task 6: View Handlers

**Files:**
- Create: `internal/views/app_template_views.go`

- [ ] **Step 1: Create view handlers**

Create `internal/views/app_template_views.go`:

```go
package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
)

// --- Public endpoints ---

// GET /v2/templates
func (v *HttpViews) ListPublishedTemplates(w http.ResponseWriter, r *http.Request) {
	tag := r.URL.Query().Get("tag")
	templateType := r.URL.Query().Get("type")

	templates, err := v.Controller.ListPublishedTemplates(tag, templateType)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	items := make([]any, len(templates))
	for i := range templates {
		resp := controllers.ToTemplateResponse(&templates[i])
		items[i] = resp
	}

	utils.JSONResponse(w, map[string]any{"templates": items}, http.StatusOK)
}

// GET /v2/templates/{slug}
func (v *HttpViews) GetPublishedTemplate(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")

	template, err := v.Controller.GetPublishedTemplate(slug)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	resp := controllers.ToTemplateDetailResponse(template)
	utils.JSONResponse(w, resp, http.StatusOK)
}

// --- Authenticated user endpoints ---

// POST /v2/templates/{slug}/copy
func (v *HttpViews) CopyTemplate(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if requireActiveSubscription(w, user) {
		return
	}

	slug := chi.URLParam(r, "slug")

	project, err := v.Controller.CopyTemplate(user, slug)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, project, http.StatusCreated)
}

// --- Admin endpoints ---

// POST /v2/admin/templates
func (v *HttpViews) CreateAdminTemplate(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	var req requests.CreateAppTemplateRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	template, err := v.Controller.CreateAppTemplate(user, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	resp := controllers.ToTemplateDetailResponse(template)
	utils.JSONResponse(w, resp, http.StatusCreated)
}

// GET /v2/admin/templates
func (v *HttpViews) ListAdminTemplates(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	templates, err := v.Controller.ListAllTemplates(user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusForbidden)
		return
	}

	items := make([]any, len(templates))
	for i := range templates {
		resp := controllers.ToTemplateResponse(&templates[i])
		items[i] = resp
	}

	utils.JSONResponse(w, map[string]any{"templates": items}, http.StatusOK)
}

// GET /v2/admin/templates/{id}
func (v *HttpViews) GetAdminTemplate(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	id := chi.URLParam(r, "id")
	template, err := v.Controller.GetAppTemplate(id, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	resp := controllers.ToTemplateDetailResponse(template)
	utils.JSONResponse(w, resp, http.StatusOK)
}

// PUT /v2/admin/templates/{id}
func (v *HttpViews) UpdateAdminTemplate(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	id := chi.URLParam(r, "id")
	var req requests.UpdateAppTemplateRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	template, err := v.Controller.UpdateAppTemplate(id, user, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	resp := controllers.ToTemplateDetailResponse(template)
	utils.JSONResponse(w, resp, http.StatusOK)
}

// DELETE /v2/admin/templates/{id}
func (v *HttpViews) DeleteAdminTemplate(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	id := chi.URLParam(r, "id")
	if err := v.Controller.DeleteAppTemplate(id, user); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, map[string]string{"status": "deleted"}, http.StatusOK)
}

// PUT /v2/admin/templates/{id}/publish
func (v *HttpViews) PublishAdminTemplate(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	id := chi.URLParam(r, "id")
	var req requests.PublishAppTemplateRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	template, err := v.Controller.PublishAppTemplate(id, user, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	resp := controllers.ToTemplateDetailResponse(template)
	utils.JSONResponse(w, resp, http.StatusOK)
}

// PUT /v2/admin/templates/{id}/sort
func (v *HttpViews) SortAdminTemplate(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	id := chi.URLParam(r, "id")
	var req requests.SortAppTemplateRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	template, err := v.Controller.SortAppTemplate(id, user, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	resp := controllers.ToTemplateDetailResponse(template)
	utils.JSONResponse(w, resp, http.StatusOK)
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/views/...
```

- [ ] **Step 3: Commit**

```bash
git add internal/views/app_template_views.go
git commit -m "feat(templates): add view handlers for all template endpoints"
```

---

### Task 7: Route Registration

**Files:**
- Modify: `cmd/main.go`

- [ ] **Step 1: Add public template routes**

In `cmd/main.go`, inside the `/v2` route block, after the public asset endpoint (`r.Get("/apps/{appId}/logo", httpViews.GetAppLogo)`), add public template routes:

```go
// Public template marketplace (no auth)
r.Route("/templates", func(r chi.Router) {
	r.Get("/", httpViews.ListPublishedTemplates)
	r.Get("/{slug}", httpViews.GetPublishedTemplate)
})
```

- [ ] **Step 2: Add authenticated copy route**

Inside the `/v2` route block, within the auth-protected section (after the `r.Use(appMiddleware.RequireAuth(controller))` and `r.Use(appMiddleware.RequireSubscription(...))` lines, near the `/apps` route), add:

```go
// Template copy (requires auth + subscription)
r.Post("/templates/{slug}/copy", httpViews.CopyTemplate)
```

- [ ] **Step 3: Add admin template routes**

Inside the existing `/v2` route block, add admin template routes. Find a suitable location after the auth-protected apps section or create a new admin block within `/v2`:

```go
// Admin template management
r.Route("/admin/templates", func(r chi.Router) {
	r.Use(appMiddleware.RequireAuth(controller))
	r.Use(appMiddleware.RequireAdmin())
	r.Post("/", httpViews.CreateAdminTemplate)
	r.Get("/", httpViews.ListAdminTemplates)
	r.Route("/{id}", func(r chi.Router) {
		r.Get("/", httpViews.GetAdminTemplate)
		r.Put("/", httpViews.UpdateAdminTemplate)
		r.Delete("/", httpViews.DeleteAdminTemplate)
		r.Put("/publish", httpViews.PublishAdminTemplate)
		r.Put("/sort", httpViews.SortAdminTemplate)
	})
})
```

- [ ] **Step 4: Verify it compiles and runs**

```bash
go build -o bin/app cmd/main.go
```

- [ ] **Step 5: Commit**

```bash
git add cmd/main.go
git commit -m "feat(templates): register template marketplace routes"
```

---

### Task 8: Build Verification + Manual Smoke Test

**Files:** None (verification only)

- [ ] **Step 1: Full build**

```bash
go build -o bin/app cmd/main.go
```

Expected: Successful build with no errors.

- [ ] **Step 2: Verify all new files exist**

Check that all new files are present:

```bash
ls -la internal/models/app_template.go internal/database/app_templates.go internal/requests/app_template_requests.go internal/responses/app_template_responses.go internal/controllers/app_template_controller.go internal/views/app_template_views.go
```

Expected: All 6 files listed.

- [ ] **Step 3: Verify route registration**

Search for template routes in main.go to confirm they're registered:

```bash
grep -n "template" cmd/main.go
```

Expected: Lines showing `/templates` routes for public, auth, and admin.

- [ ] **Step 4: Commit final state (if any fixes were needed)**

```bash
git add -A
git commit -m "fix(templates): address build issues from template marketplace"
```

Only run this if fixes were made during verification. Skip if the build passed cleanly.
