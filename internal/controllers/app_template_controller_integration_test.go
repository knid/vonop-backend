//go:build integration

package controllers

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/services/gitrepo"
	"github.com/knid/wemob/internal/services/remoteconfig"
)

func TestCreateTemplateFromAppCopiesRepoWithoutPrompt(t *testing.T) {
	c, user, _ := newTemplateIntegrationController(t)

	project := createTemplateSourceProject(t, c, user, "sensitive seed prompt")
	writeSourceRepoFile(t, c.GitRepoService, project.ID.String(), "frontend/package.json", []byte(`{"scripts":{"dev":"vite"}}`))

	template, err := c.CreateTemplateFromApp(user, project.ID.String(), requests.CreateTemplateFromAppRequest{
		Name:         "Catalog Starter",
		Slug:         "catalog-starter",
		Description:  "Template created from an app",
		Tags:         []string{"starter"},
		TemplateType: "web",
	})
	if err != nil {
		t.Fatalf("CreateTemplateFromApp: %v", err)
	}
	if template.SourceRepoID != template.ID.String() {
		t.Fatalf("SourceRepoID = %q, want template id %q", template.SourceRepoID, template.ID)
	}
	if _, err := c.GitRepoService.Open(template.ID.String()); err != nil {
		t.Fatalf("open template repo: %v", err)
	}
	if _, err := os.Stat(filepath.Join(c.GitRepoService.BasePath, template.ID.String(), "frontend/package.json")); err != nil {
		t.Fatalf("template repo did not receive app file: %v", err)
	}
}

func TestCopyTemplateClonesRepoWithoutInheritingPrompt(t *testing.T) {
	c, user, _ := newTemplateIntegrationController(t)

	templateID := uuid.New()
	templateRepo := writeSourceRepoFile(t, c.GitRepoService, templateID.String(), "frontend/package.json", []byte(`{"scripts":{"dev":"vite"}}`))
	if _, err := c.DB.CreateAppTemplate(&models.AppTemplate{
		ID:           templateID,
		Name:         "Catalog Starter",
		Slug:         "catalog-starter",
		Description:  "Template created from code",
		Tags:         []string{"starter"},
		TemplateType: "web",
		ProjectType:  models.ProjectTypeApp,
		Settings:     map[string]any{},
		Theme:        []byte("{}"),
		DataModels:   []byte("[]"),
		SourceRepoID: templateRepo.ProjectID,
		Published:    true,
		CreatedByID:  user.ID,
	}); err != nil {
		t.Fatalf("create template: %v", err)
	}

	project, err := c.CopyTemplate(context.Background(), user, "catalog-starter")
	if err != nil {
		t.Fatalf("CopyTemplate: %v", err)
	}
	if project.Prompt != "" {
		t.Fatalf("copied project prompt = %q, want empty", project.Prompt)
	}
	if _, err := os.Stat(filepath.Join(c.GitRepoService.BasePath, project.ID.String(), "frontend/package.json")); err != nil {
		t.Fatalf("copied project repo did not receive template file: %v", err)
	}
}

func TestCopyTemplateRequiresSourceRepo(t *testing.T) {
	c, user, _ := newTemplateIntegrationController(t)

	if _, err := c.DB.CreateAppTemplate(&models.AppTemplate{
		ID:           uuid.New(),
		Name:         "Metadata Only",
		Slug:         "metadata-only",
		Description:  "Template without code",
		Tags:         []string{"starter"},
		TemplateType: "web",
		ProjectType:  models.ProjectTypeApp,
		Settings:     map[string]any{},
		Theme:        []byte("{}"),
		DataModels:   []byte("[]"),
		Published:    true,
		CreatedByID:  user.ID,
	}); err != nil {
		t.Fatalf("create template: %v", err)
	}

	if _, err := c.CopyTemplate(context.Background(), user, "metadata-only"); err == nil {
		t.Fatal("expected CopyTemplate to fail for template without source repo")
	}
}

func newTemplateIntegrationController(t *testing.T) (*Controller, *models.User, *models.Organization) {
	t.Helper()
	t.Setenv("GIT_AUTHOR_NAME", "Wemob Tests")
	t.Setenv("GIT_AUTHOR_EMAIL", "tests@wemob.local")
	t.Setenv("GIT_COMMITTER_NAME", "Wemob Tests")
	t.Setenv("GIT_COMMITTER_EMAIL", "tests@wemob.local")

	db := database.NewTestDB(t,
		(*models.User)(nil),
		(*models.Organization)(nil),
		(*models.OrganizationMember)(nil),
		(*models.StandardUserLimits)(nil),
		(*models.StandardUserSubscription)(nil),
		(*models.AICreditsBalance)(nil),
		(*models.AppProject)(nil),
		(*models.AppTemplate)(nil),
	)
	c, _ := NewTestController(t, db, TestControllerOptions{})
	c.GitRepoService = gitrepo.NewService(t.TempDir())
	c.RemoteConfig = remoteconfig.New(nil)

	user, org := seedUserOrgAndCredits(t, c, 100)
	if _, err := c.DB.DB.NewUpdate().
		Model((*models.StandardUserLimits)(nil)).
		Set("monthly_app_count = ?", 10).
		Set("max_mobile_apps = ?", 10).
		Where("1 = 1").
		Exec(context.Background()); err != nil {
		t.Fatalf("raise app limits: %v", err)
	}
	user.Type = "admin"
	if _, err := c.DB.UpdateUser(user); err != nil {
		t.Fatalf("promote user to admin: %v", err)
	}
	return c, user, org
}

func createTemplateSourceProject(t *testing.T, c *Controller, user *models.User, prompt string) *models.AppProject {
	t.Helper()
	orgID, err := c.RequireOrgIDForUser(user, nil)
	if err != nil {
		t.Fatalf("RequireOrgIDForUser: %v", err)
	}
	project, err := c.DB.CreateAppProject(&models.AppProject{
		ID:              uuid.New(),
		UserID:          user.ID,
		OrganizationID:  orgID,
		Name:            "Source App",
		Slug:            "source-app-" + uuid.NewString(),
		Prompt:          prompt,
		ProjectType:     models.ProjectTypeApp,
		FrontendRuntime: models.FrontendRuntimeViteReact,
		FrontendHosting: models.FrontendHostingS3CDN,
		BackendRuntime:  models.BackendRuntimeNone,
		Theme:           []byte("{}"),
		DataModels:      []byte("[]"),
		Settings:        map[string]any{},
		Status:          "ready",
	})
	if err != nil {
		t.Fatalf("create app project: %v", err)
	}
	return project
}

func writeSourceRepoFile(t *testing.T, svc *gitrepo.Service, repoID string, path string, content []byte) *gitrepo.Repo {
	t.Helper()
	repo, err := svc.Init(repoID)
	if err != nil {
		t.Fatalf("init repo: %v", err)
	}
	if err := repo.WriteFile(path, content); err != nil {
		t.Fatalf("write repo file: %v", err)
	}
	if _, err := repo.Commit("test: seed repo"); err != nil {
		t.Fatalf("commit repo file: %v", err)
	}
	return repo
}
