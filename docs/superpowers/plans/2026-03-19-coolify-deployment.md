# Coolify Deployment Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace Vercel with Coolify + Gitea for deploying user-generated Next.js apps to self-hosted infrastructure.

**Architecture:** Backend pushes project files to a Gitea git server via `git push`, then calls Coolify's REST API to create/deploy apps. Coolify builds with Nixpacks and serves at `{slug}.wemob.app`. Custom domains managed via Coolify API, DNS verification via Go `net` package.

**Tech Stack:** Go, Coolify REST API, Gitea REST API, git CLI (existing `gitrepo.Service`)

**Spec:** `docs/superpowers/specs/2026-03-19-coolify-deployment-design.md`

---

## Chunk 1: New Services + Model

### Task 1: Create Gitea service

**Files:**
- Create: `internal/services/gitea/gitea.go`

This service manages Gitea repositories via REST API (create/delete repos) and provides clone URLs for `git push`.

- [ ] **Step 1: Create the Gitea service file**

```go
package gitea

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

var httpClient = &http.Client{Timeout: 30 * time.Second}

type Service struct {
	BaseURL string // e.g. https://git.wemob.io
	Token   string
	Org     string // Gitea organization name
}

func NewService(baseURL, token, org string) *Service {
	return &Service{BaseURL: baseURL, Token: token, Org: org}
}

type createRepoRequest struct {
	Name          string `json:"name"`
	AutoInit      bool   `json:"auto_init"` // false — local repo already has commits
	DefaultBranch string `json:"default_branch"`
	Private       bool   `json:"private"`
}

type repoResponse struct {
	ID       int    `json:"id"`
	CloneURL string `json:"clone_url"`
	HTMLURL  string `json:"html_url"`
}

func (s *Service) doRequest(method, path string, body any) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("gitea: marshal: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, s.BaseURL+"/api/v1"+path, reqBody)
	if err != nil {
		return nil, fmt.Errorf("gitea: create request: %w", err)
	}
	req.Header.Set("Authorization", "token "+s.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gitea: do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gitea: read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		bodyStr := string(respBody)
		if len(bodyStr) > 500 {
			bodyStr = bodyStr[:500]
		}
		slog.Error("Gitea API error", "status", resp.StatusCode, "path", path, "body", bodyStr)
		return nil, fmt.Errorf("gitea API error: status %d", resp.StatusCode)
	}

	return respBody, nil
}

// CreateRepo creates a new repository under the configured organization.
// Returns the clone URL with embedded token for authenticated push.
func (s *Service) CreateRepo(name string) (string, error) {
	body, err := s.doRequest("POST", fmt.Sprintf("/orgs/%s/repos", s.Org), createRepoRequest{
		Name:          name,
		AutoInit:      false,
		DefaultBranch: "main",
		Private:       true,
	})
	if err != nil {
		return "", err
	}

	var resp repoResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("gitea: unmarshal create repo: %w", err)
	}

	return s.AuthenticatedCloneURL(name), nil
}

// DeleteRepo deletes a repository.
func (s *Service) DeleteRepo(name string) error {
	_, err := s.doRequest("DELETE", fmt.Sprintf("/repos/%s/%s", s.Org, name), nil)
	return err
}

// AuthenticatedCloneURL returns the clone URL with the API token embedded
// for use with git push. The token is NOT persisted in .git/config because
// PushToURL uses it as a one-shot URL argument.
func (s *Service) AuthenticatedCloneURL(repoName string) string {
	// Parse the base URL to inject token: https://token@git.wemob.io/org/repo.git
	// Simple string manipulation since BaseURL is controlled by us
	scheme := "https://"
	host := s.BaseURL
	if len(host) > 8 && host[:8] == "https://" {
		host = host[8:]
	} else if len(host) > 7 && host[:7] == "http://" {
		scheme = "http://"
		host = host[7:]
	}
	return fmt.Sprintf("%s%s@%s/%s/%s.git", scheme, s.Token, host, s.Org, repoName)
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/services/gitea/`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add internal/services/gitea/gitea.go
git commit -m "feat: add Gitea service for repo management"
```

---

### Task 2: Create Coolify service

**Files:**
- Create: `internal/services/coolify/coolify.go`

This service wraps the Coolify REST API for app lifecycle management.

- [ ] **Step 1: Create the Coolify service file**

```go
package coolify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

var httpClient = &http.Client{Timeout: 60 * time.Second}

type Service struct {
	BaseURL         string // e.g. https://coolify.wemob.io
	Token           string
	ServerUUID      string
	ProjectUUID     string
	EnvironmentName string
}

func NewService(baseURL, token, serverUUID, projectUUID, environmentName string) *Service {
	return &Service{
		BaseURL:         strings.TrimRight(baseURL, "/"),
		Token:           token,
		ServerUUID:      serverUUID,
		ProjectUUID:     projectUUID,
		EnvironmentName: environmentName,
	}
}

func (s *Service) doRequest(method, path string, body any) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("coolify: marshal: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, s.BaseURL+"/api/v1"+path, reqBody)
	if err != nil {
		return nil, fmt.Errorf("coolify: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("coolify: do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("coolify: read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		bodyStr := string(respBody)
		if len(bodyStr) > 500 {
			bodyStr = bodyStr[:500]
		}
		slog.Error("Coolify API error", "status", resp.StatusCode, "path", path, "body", bodyStr)
		return nil, fmt.Errorf("coolify API error: status %d", resp.StatusCode)
	}

	return respBody, nil
}

type createAppRequest struct {
	ProjectUUID       string `json:"project_uuid"`
	ServerUUID        string `json:"server_uuid"`
	EnvironmentName   string `json:"environment_name"`
	GitRepository     string `json:"git_repository"`
	GitBranch         string `json:"git_branch"`
	BuildPack         string `json:"build_pack"`
	PortsExposes      string `json:"ports_exposes"`
	Domains           string `json:"domains"`
	InstantDeploy     bool   `json:"instant_deploy"`
	IsForceHTTPS      bool   `json:"is_force_https_enabled"`
	AutogenerateDomain bool  `json:"autogenerate_domain"`
	Name              string `json:"name"`
}

type createAppResponse struct {
	UUID string `json:"uuid"`
}

type appStatusResponse struct {
	Status string `json:"status"`
}

type logsResponse struct {
	Logs string `json:"logs"`
}

type envVar struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// CreateApp creates a new application from a public git repository.
// Returns the Coolify application UUID.
func (s *Service) CreateApp(name, giteaRepoURL, domain string) (string, error) {
	body, err := s.doRequest("POST", "/applications/public", createAppRequest{
		ProjectUUID:        s.ProjectUUID,
		ServerUUID:         s.ServerUUID,
		EnvironmentName:    s.EnvironmentName,
		GitRepository:      giteaRepoURL,
		GitBranch:          "main",
		BuildPack:          "nixpacks",
		PortsExposes:       "3000",
		Domains:            "https://" + domain,
		InstantDeploy:      true,
		IsForceHTTPS:       true,
		AutogenerateDomain: false,
		Name:               name,
	})
	if err != nil {
		return "", err
	}

	var resp createAppResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("coolify: unmarshal create app: %w", err)
	}
	return resp.UUID, nil
}

// RestartApp triggers a rebuild and redeploy of the application.
func (s *Service) RestartApp(appUUID string) error {
	_, err := s.doRequest("GET", fmt.Sprintf("/applications/%s/restart", appUUID), nil)
	return err
}

// GetAppStatus returns the current status of an application.
func (s *Service) GetAppStatus(appUUID string) (string, error) {
	body, err := s.doRequest("GET", fmt.Sprintf("/applications/%s", appUUID), nil)
	if err != nil {
		return "", err
	}

	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("coolify: unmarshal status: %w", err)
	}

	status, _ := resp["status"].(string)
	return status, nil
}

// GetAppLogs returns the application logs.
func (s *Service) GetAppLogs(appUUID string, lines int) (string, error) {
	if lines <= 0 {
		lines = 100
	}
	body, err := s.doRequest("GET", fmt.Sprintf("/applications/%s/logs?lines=%d", appUUID, lines), nil)
	if err != nil {
		return "", err
	}

	var resp logsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("coolify: unmarshal logs: %w", err)
	}
	return resp.Logs, nil
}

// SyncEnvVars sets environment variables on the application using bulk update.
// Removes stale vars not in the desired set.
func (s *Service) SyncEnvVars(appUUID string, desired map[string]string) error {
	var data []envVar
	for k, v := range desired {
		data = append(data, envVar{Key: k, Value: v})
	}

	_, err := s.doRequest("PATCH", fmt.Sprintf("/applications/%s/envs/bulk", appUUID), map[string]any{
		"data": data,
	})
	return err
}

// UpdateDomains replaces the domains for an application.
// domains should be full URLs like "https://my-app.wemob.app".
func (s *Service) UpdateDomains(appUUID string, domains []string) error {
	domainStr := strings.Join(domains, ",")
	_, err := s.doRequest("PATCH", fmt.Sprintf("/applications/%s", appUUID), map[string]any{
		"domains": domainStr,
	})
	return err
}

// DeleteApp deletes a Coolify application and its resources.
func (s *Service) DeleteApp(appUUID string) error {
	_, err := s.doRequest("DELETE", fmt.Sprintf("/applications/%s", appUUID), nil)
	return err
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/services/coolify/`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add internal/services/coolify/coolify.go
git commit -m "feat: add Coolify service for app deployment"
```

---

### Task 3: Create CoolifyProject model and database layer

**Files:**
- Create: `internal/models/coolify_project.go`
- Create: `internal/database/coolify_projects.go`

- [ ] **Step 1: Create the model**

```go
// internal/models/coolify_project.go
package models

import (
	"time"

	"github.com/google/uuid"
)

type CoolifyProject struct {
	ID             uuid.UUID  `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	AppProjectID   uuid.UUID  `bun:",notnull,type:uuid,unique"`
	AppProject     *AppProject `bun:"rel:belongs-to,join:app_project_id=id"`
	CoolifyAppUUID string     `bun:",notnull"`
	GiteaRepoName  string     `bun:",notnull"`
	ProductionURL  string     `bun:",nullzero"`
	CustomDomain   string     `bun:",nullzero"`
	Framework      string     `bun:",notnull,default:'nextjs'"`
	CreatedAt      time.Time  `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt      time.Time  `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}
```

- [ ] **Step 2: Create the database layer**

```go
// internal/database/coolify_projects.go
package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) GetCoolifyProjectByAppProjectID(appProjectID string) (*models.CoolifyProject, error) {
	var cp models.CoolifyProject
	err := db.DB.NewSelect().Model(&cp).Where("app_project_id = ?", appProjectID).Scan(context.Background())
	return &cp, err
}

func (db *Database) CreateCoolifyProject(cp *models.CoolifyProject) (*models.CoolifyProject, error) {
	_, err := db.DB.NewInsert().Model(cp).Exec(context.Background())
	return cp, err
}

func (db *Database) UpdateCoolifyProject(cp *models.CoolifyProject) (*models.CoolifyProject, error) {
	_, err := db.DB.NewUpdate().Model(cp).Where("id = ?", cp.ID).Exec(context.Background())
	return cp, err
}

func (db *Database) DeleteCoolifyProject(cp *models.CoolifyProject) error {
	_, err := db.DB.NewDelete().Model(cp).Where("id = ?", cp.ID).Exec(context.Background())
	return err
}
```

- [ ] **Step 3: Verify it compiles**

Run: `go build ./internal/models/ && go build ./internal/database/`
Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add internal/models/coolify_project.go internal/database/coolify_projects.go
git commit -m "feat: add CoolifyProject model and database layer"
```

---

## Chunk 2: Controller + Wiring Changes

### Task 4: Add ForcePushToURL to gitrepo.Repo

**Files:**
- Modify: `internal/services/gitrepo/repo.go`

The existing `PushToURL` doesn't use `--force`. Subsequent deploys may rewrite history (e.g., the inject-env commit gets amended each deploy), so we need force push.

- [ ] **Step 1: Add ForcePushToURL method**

After the existing `PushToURL` method (line 327), add:

```go
// ForcePushToURL is like PushToURL but uses --force to overwrite remote history.
// Used for deployment pushes where the local repo is authoritative.
func (r *Repo) ForcePushToURL(url, branch string) error {
	unlock := r.service.lock(r.ProjectID)
	defer unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "push", "--force", url, branch)
	cmd.Dir = r.Path
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("gitrepo: force push: %s: %s", err, out)
	}
	return nil
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/services/gitrepo/`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add internal/services/gitrepo/repo.go
git commit -m "feat: add ForcePushToURL for deployment pushes"
```

---

### Task 5: Update Controller struct — replace VercelService with Coolify + Gitea

**Files:**
- Modify: `internal/controllers/controller.go`

- [ ] **Step 1: Replace Vercel import and field with Coolify + Gitea**

In `internal/controllers/controller.go`:

Replace import:
```go
"github.com/knid/wemob/internal/services/vercel"
```
with:
```go
"github.com/knid/wemob/internal/services/coolify"
"github.com/knid/wemob/internal/services/gitea"
```

Replace field in Controller struct:
```go
VercelService          *vercel.Service
```
with:
```go
CoolifyService         *coolify.Service
GiteaService           *gitea.Service
```

Update `NewController` signature — replace `vercelService *vercel.Service` parameter with `coolifyService *coolify.Service, giteaService *gitea.Service`. Update the struct initialization to set `CoolifyService` and `GiteaService` instead of `VercelService`.

- [ ] **Step 2: Verify it compiles (expect errors in other files — that's fine for now)**

Run: `go build ./internal/controllers/controller.go` — this will show errors in files still referencing `VercelService`. Note them for the next tasks.

- [ ] **Step 3: Commit**

```bash
git add internal/controllers/controller.go
git commit -m "refactor: replace VercelService with CoolifyService + GiteaService in Controller"
```

---

### Task 6: Rewrite DeployToVercel → DeployToCoolify

**Files:**
- Modify: `internal/controllers/app_build_controller.go`

- [ ] **Step 1: Replace the DeployToVercel method**

Replace the entire `DeployToVercel` method (lines 22-153) with `DeployToCoolify`:

```go
// DeployToCoolify deploys the app to Coolify using files pushed to Gitea.
func (c *Controller) DeployToCoolify(project *models.AppProject, user *models.User) (*models.AppDeployment, error) {
	if c.CoolifyService == nil {
		return nil, fmt.Errorf("coolify service not configured")
	}

	// Read files from git repo
	repo, err := c.GitRepoService.Open(project.ID.String())
	if err != nil {
		return nil, fmt.Errorf("no generated files found for deployment")
	}
	repoFiles, err := repo.ReadAllFiles()
	if err != nil || len(repoFiles) == 0 {
		return nil, fmt.Errorf("no generated files found for deployment")
	}

	// Collect all env vars (integrations + custom + auto-injected store vars)
	envVars := c.CollectAllEnvVars(project)

	// Build file map and inject .env.local + analytics
	fileMap := repoFiles
	if dotEnv := BuildDotEnvLocal(envVars); dotEnv != "" {
		fileMap[".env.local"] = dotEnv
	}
	injectAnalyticsScript(fileMap, project.ID.String())

	// Write ALL modified/injected files back to the repo working tree.
	// The Vercel flow uploaded fileMap directly; the Coolify flow needs
	// everything committed to git so it reaches Gitea via push.
	for path, content := range fileMap {
		if repoFiles[path] != content {
			repo.WriteFile(path, []byte(content))
		}
	}
	repo.Commit("deploy: inject env and analytics")

	// Get or create Coolify project
	coolifyProject, err := c.DB.GetCoolifyProjectByAppProjectID(project.ID.String())
	if err != nil {
		// First deploy — create Gitea repo and Coolify app
		giteaRepoName := "wemob-" + project.Slug
		giteaCloneURL, err := c.GiteaService.CreateRepo(giteaRepoName)
		if err != nil {
			slog.Error("Failed to create Gitea repo", "error", err)
			return nil, fmt.Errorf("failed to create git repository")
		}

		// Push files to Gitea
		if err := repo.PushToURL(giteaCloneURL, "main"); err != nil {
			slog.Error("Failed to push to Gitea", "error", err)
			return nil, fmt.Errorf("failed to push files")
		}

		// Create Coolify app pointing to the Gitea repo
		subdomain := project.Slug + "." + constants.WEMOB_APP_BASE_DOMAIN
		// Use the public (non-authenticated) clone URL for Coolify
		publicRepoURL := fmt.Sprintf("%s/%s/%s.git", c.GiteaService.BaseURL, c.GiteaService.Org, giteaRepoName)

		appUUID, err := c.CoolifyService.CreateApp(giteaRepoName, publicRepoURL, subdomain)
		if err != nil {
			slog.Error("Failed to create Coolify app", "error", err)
			return nil, fmt.Errorf("failed to create deployment")
		}

		coolifyProject = &models.CoolifyProject{
			AppProjectID:   project.ID,
			CoolifyAppUUID: appUUID,
			GiteaRepoName:  giteaRepoName,
			Framework:      "nextjs",
		}
		coolifyProject, err = c.DB.CreateCoolifyProject(coolifyProject)
		if err != nil {
			slog.Error("Failed to persist Coolify project to DB", "error", err)
			return nil, fmt.Errorf("failed to save deployment project")
		}
	} else {
		// Subsequent deploy — force push updated files and restart
		giteaCloneURL := c.GiteaService.AuthenticatedCloneURL(coolifyProject.GiteaRepoName)
		if err := repo.ForcePushToURL(giteaCloneURL, "main"); err != nil {
			slog.Error("Failed to push to Gitea", "error", err)
			return nil, fmt.Errorf("failed to push files")
		}

		if err := c.CoolifyService.RestartApp(coolifyProject.CoolifyAppUUID); err != nil {
			slog.Error("Failed to restart Coolify app", "error", err)
			return nil, fmt.Errorf("failed to trigger deployment")
		}
	}

	// Sync env vars on Coolify app
	if err := c.CoolifyService.SyncEnvVars(coolifyProject.CoolifyAppUUID, envVars); err != nil {
		slog.Warn("Failed to sync env vars on Coolify", "error", err)
	}

	subdomain := project.Slug + "." + constants.WEMOB_APP_BASE_DOMAIN
	deployedURL := "https://" + subdomain

	// Track deployment
	deployment := &models.AppDeployment{
		AppProjectID: project.ID,
		UserID:       user.ID,
		Platform:     "web",
		Provider:     "coolify",
		Status:       "building",
		ProviderData: map[string]any{
			"coolify_app_uuid": coolifyProject.CoolifyAppUUID,
		},
		DeployedURL: deployedURL,
		Version:     1,
	}

	// Record commit hash
	if commitHash, headErr := repo.Head(); headErr == nil {
		deployment.CommitHash = commitHash
		repo.Tag(fmt.Sprintf("deploy-%d", time.Now().Unix()))
	}

	// Get latest version
	latestDeploy, err := c.DB.GetLatestAppDeployment(project.ID.String(), "web")
	if err == nil {
		deployment.Version = latestDeploy.Version + 1
	}

	deployment, err = c.DB.CreateAppDeployment(deployment)
	if err != nil {
		slog.Error("Failed to save deployment record", "error", err)
	}

	// Update Coolify project record
	coolifyProject.ProductionURL = deployedURL
	coolifyProject.CustomDomain = subdomain
	c.DB.UpdateCoolifyProject(coolifyProject)

	// Update project status
	c.UpdateAppProjectStatus(project, "deployed")

	// For mobile_app projects, ensure a v1 Project exists
	if project.ProjectType == "mobile_app" {
		if err := c.ensureV1ProjectForMobileApp(project, user, deployedURL); err != nil {
			slog.Error("Failed to create v1 Project for mobile app", "error", err, "app_project_id", project.ID)
		}
	}

	return deployment, nil
}
```

- [ ] **Step 2: Update GetLatestDeploymentStatus to use Coolify API**

Replace the existing `GetLatestDeploymentStatus` method (lines 371-427) with:

```go
func (c *Controller) GetLatestDeploymentStatus(projectID string) (*models.AppDeployment, error) {
	deployment, err := c.DB.GetLatestAppDeployment(projectID, "web")
	if err != nil {
		return nil, err
	}

	// Skip polling for old Vercel deployments — return existing status as-is
	if deployment.Provider == "vercel" {
		return deployment, nil
	}

	// If still building, check Coolify for actual status
	if (deployment.Status == "building" || deployment.Status == "pending") && c.CoolifyService != nil {
		appUUID, _ := deployment.ProviderData["coolify_app_uuid"].(string)
		if appUUID != "" {
			status, err := c.CoolifyService.GetAppStatus(appUUID)
			if err != nil {
				slog.Warn("Failed to check Coolify app status", "error", err, "app_uuid", appUUID)
				return deployment, nil
			}

			var newStatus string
			var errorMessage string
			switch strings.ToLower(status) {
			case "running":
				newStatus = "deployed"
			case "exited", "error", "degraded":
				newStatus = "failed"
				logs, logErr := c.CoolifyService.GetAppLogs(appUUID, 200)
				if logErr != nil {
					errorMessage = "Build failed"
				} else {
					errorMessage = logs
				}
			default:
				// Still building/starting
				return deployment, nil
			}

			deployment.Status = newStatus
			if errorMessage != "" {
				deployment.ErrorMessage = errorMessage
			}
			if _, err := c.DB.UpdateAppDeployment(deployment); err != nil {
				slog.Error("Failed to update deployment status", "error", err)
			}
		}
	}

	return deployment, nil
}
```

Note: add `"strings"` to imports if not already present.

- [ ] **Step 3: Update the view that calls DeployToVercel**

In `internal/views/app_deploy_views.go`:

1. Remove lines 48-52 (the `req` parsing and `target` variable — no longer needed):
```go
	var req requests.DeployWebRequest
	utils.ParseJSONBody(r, &req)

	target := "production"
	if req.Target != "" {
		target = req.Target
	}
```

2. Change line 55:
```go
deployment, err := v.Controller.DeployToVercel(project, user, target)
```
to:
```go
deployment, err := v.Controller.DeployToCoolify(project, user)
```

Coolify doesn't have Vercel's preview/production distinction — all deploys are production.

- [ ] **Step 4: Verify it compiles**

Run: `go build ./internal/controllers/ && go build ./internal/views/`
Expected: no errors (may still have errors in other files referencing Vercel — addressed in next tasks)

- [ ] **Step 5: Commit**

```bash
git add internal/controllers/app_build_controller.go internal/views/app_deploy_views.go
git commit -m "feat: replace DeployToVercel with DeployToCoolify"
```

---

### Task 7: Update app_domain_controller.go — replace Vercel with Coolify + DNS lookup

**Files:**
- Modify: `internal/controllers/app_domain_controller.go`

- [ ] **Step 1: Rewrite AddAppDomain to use Coolify**

Replace lines 69-79 (the Vercel domain block in `AddAppDomain`):

```go
	// If there is a Vercel project, add the domain there too
	vercelProject, vercelErr := c.DB.GetVercelProjectByAppProjectID(project.ID.String())
	if vercelErr == nil && vercelProject != nil && c.VercelService != nil {
		if err := c.VercelService.AddDomain(vercelProject.VercelProjectID, domainName); err != nil {
			slog.Warn("Failed to add domain to Vercel", "domain", domainName, "error", err)
		} else {
			created.Status = "active"
			c.DB.UpdateAppDomain(created)
		}
	}
```

with:

```go
	// If there is a Coolify project, add the domain there too
	coolifyProject, cpErr := c.DB.GetCoolifyProjectByAppProjectID(project.ID.String())
	if cpErr == nil && coolifyProject != nil && c.CoolifyService != nil {
		// Fetch current domains, append new one
		newDomain := "https://" + domainName
		existingDomain := "https://" + coolifyProject.CustomDomain
		if err := c.CoolifyService.UpdateDomains(coolifyProject.CoolifyAppUUID, []string{existingDomain, newDomain}); err != nil {
			slog.Warn("Failed to add domain to Coolify", "domain", domainName, "error", err)
		} else {
			created.Status = "active"
			c.DB.UpdateAppDomain(created)
		}
	}
```

- [ ] **Step 2: Rewrite RemoveAppDomain to use Coolify**

Replace lines 101-109 (the Vercel domain block in `RemoveAppDomain`):

```go
	if c.VercelService != nil {
		vercelProject, vercelErr := c.DB.GetVercelProjectByAppProjectID(project.ID.String())
		if vercelErr == nil && vercelProject != nil {
			if err := c.VercelService.RemoveDomain(vercelProject.VercelProjectID, domain.Domain); err != nil {
				slog.Warn("Failed to remove domain from Vercel", "domain", domain.Domain, "error", err)
			}
		}
	}
```

with:

```go
	if c.CoolifyService != nil {
		coolifyProject, cpErr := c.DB.GetCoolifyProjectByAppProjectID(project.ID.String())
		if cpErr == nil && coolifyProject != nil {
			// Keep only the default subdomain
			defaultDomain := "https://" + coolifyProject.CustomDomain
			if err := c.CoolifyService.UpdateDomains(coolifyProject.CoolifyAppUUID, []string{defaultDomain}); err != nil {
				slog.Warn("Failed to remove domain from Coolify", "domain", domain.Domain, "error", err)
			}
		}
	}
```

- [ ] **Step 3: Rewrite VerifyAppDomain to use DNS lookup instead of Vercel API**

Replace the entire `VerifyAppDomain` method (lines 114-170) with:

```go
func (c *Controller) VerifyAppDomain(domainID string, project *models.AppProject) (*responses.AppDomainResponse, error) {
	domain, err := c.DB.GetAppDomainByID(domainID)
	if err != nil {
		return nil, errors.New("domain not found")
	}

	if domain.AppProjectID != project.ID {
		return nil, errors.New("domain does not belong to this project")
	}

	// Check DNS via Go net package instead of Vercel API
	cnames, err := net.LookupCNAME(domain.Domain)
	if err == nil && strings.Contains(cnames, constants.WEMOB_CNAME_TARGET) {
		domain.Status = "active"
	} else {
		// Try A record lookup as fallback
		ips, err := net.LookupHost(domain.Domain)
		if err == nil && len(ips) > 0 {
			domain.Status = "active"
		}
		// If neither resolves, keep current status (pending)
	}

	c.DB.UpdateAppDomain(domain)

	// If domain just became active and Coolify project exists, add domain
	if domain.Status == "active" && c.CoolifyService != nil {
		coolifyProject, cpErr := c.DB.GetCoolifyProjectByAppProjectID(project.ID.String())
		if cpErr == nil && coolifyProject != nil {
			newDomain := "https://" + domain.Domain
			existingDomain := "https://" + coolifyProject.CustomDomain
			if err := c.CoolifyService.UpdateDomains(coolifyProject.CoolifyAppUUID, []string{existingDomain, newDomain}); err != nil {
				slog.Warn("Failed to add verified domain to Coolify", "domain", domain.Domain, "error", err)
			}
		}
	}

	return &responses.AppDomainResponse{
		ID:        domain.ID,
		Domain:    domain.Domain,
		Status:    domain.Status,
		DNSRecord: constants.WEMOB_CNAME_TARGET,
		CreatedAt: domain.CreatedAt,
	}, nil
}
```

Add `"net"` to imports.

- [ ] **Step 4: Verify it compiles**

Run: `go build ./internal/controllers/`
Expected: no errors

- [ ] **Step 5: Commit**

```bash
git add internal/controllers/app_domain_controller.go
git commit -m "refactor: replace Vercel domain management with Coolify + DNS lookup"
```

---

### Task 8: Update app_project_controller.go — slug change + project deletion cleanup

**Files:**
- Modify: `internal/controllers/app_project_controller.go`

- [ ] **Step 1: Update slug-change goroutine in UpdateAppProject**

Replace lines 163-186 in `UpdateAppProject`:

```go
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
```

with:

```go
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
```

- [ ] **Step 2: Add Coolify + Gitea cleanup to DeleteAppProject**

In `DeleteAppProject`, after the git repo cleanup block (after line 216), add:

```go
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
		// Remove the DB record
		c.DB.DeleteCoolifyProject(coolifyProject)
	}
```

- [ ] **Step 3: Verify it compiles**

Run: `go build ./internal/controllers/`
Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add internal/controllers/app_project_controller.go
git commit -m "refactor: update slug change + deletion cleanup for Coolify/Gitea"
```

---

## Chunk 3: Wiring, Constants, Cleanup

### Task 9: Update constants and limits model

**Files:**
- Modify: `internal/constants/settings.go`
- Modify: `internal/models/limits.go`

- [ ] **Step 1: Update WEMOB_CNAME_TARGET**

In `internal/constants/settings.go`, the `WEMOB_CNAME_TARGET` value needs to point to the Coolify server. For now, keep the constant name but change the comment to reflect it points to the Coolify server. The actual value will be updated when the Coolify server is set up (it depends on the server IP/hostname). Leave the value as-is for now — it will be configured during infrastructure setup.

No code change needed yet — the hostname will be set post-infra-setup.

- [ ] **Step 2: Rename CanDeployToVercel → CanDeployWeb in limits model**

In `internal/models/limits.go`, change line 26:
```go
CanDeployToVercel  bool `bun:",notnull,default:false"`
```
to:
```go
CanDeployWeb       bool `bun:"can_deploy_to_vercel,notnull,default:false"`
```

Note: the `bun` tag keeps the old column name `can_deploy_to_vercel` to avoid a DB migration. The Go field name is what matters for code references.

- [ ] **Step 3: Update all references to CanDeployToVercel in cmd/main.go**

In `cmd/main.go`, replace all 4 occurrences of `CanDeployToVercel` with `CanDeployWeb` (lines 414, 445, 476, 537).

- [ ] **Step 4: Verify it compiles**

Run: `go build ./...`
Expected: no errors

- [ ] **Step 5: Commit**

```bash
git add internal/models/limits.go internal/constants/settings.go cmd/main.go
git commit -m "refactor: rename CanDeployToVercel to CanDeployWeb"
```

---

### Task 10: Update cmd/main.go — init Coolify + Gitea services, register model

**Files:**
- Modify: `cmd/main.go`

- [ ] **Step 1: Replace Vercel service init with Coolify + Gitea**

Replace the Vercel service initialization (around line 676):
```go
vercelService := vercel.NewService(sm.GetSecret("VERCEL_API_TOKEN"), sm.GetSecret("VERCEL_TEAM_ID"))
```
with:
```go
coolifyService := coolify.NewService(
    sm.GetSecret("COOLIFY_API_URL"),
    sm.GetSecret("COOLIFY_API_TOKEN"),
    sm.GetSecret("COOLIFY_SERVER_UUID"),
    sm.GetSecret("COOLIFY_PROJECT_UUID"),
    sm.GetSecret("COOLIFY_ENVIRONMENT_NAME"),
)
giteaService := gitea.NewService(
    sm.GetSecret("GITEA_URL"),
    sm.GetSecret("GITEA_API_TOKEN"),
    sm.GetSecret("GITEA_ORG"),
)
```

Update import: replace `"github.com/knid/wemob/internal/services/vercel"` with:
```go
"github.com/knid/wemob/internal/services/coolify"
"github.com/knid/wemob/internal/services/gitea"
```

- [ ] **Step 2: Update NewController call**

Replace `vercelService` in the `NewController` call (around line 739) with `coolifyService, giteaService`.

- [ ] **Step 3: Add CoolifyProject to createSchema**

In the `createSchema` function, add `(*models.CoolifyProject)(nil)` to the models list.

- [ ] **Step 4: Add index for coolify_projects**

In the performance indexes section, add:
```go
`CREATE INDEX IF NOT EXISTS idx_coolify_projects_app_project_id ON coolify_projects(app_project_id)`,
```

- [ ] **Step 5: Verify full build**

Run: `go build ./...`
Expected: no errors

- [ ] **Step 6: Commit**

```bash
git add cmd/main.go
git commit -m "feat: wire Coolify + Gitea services, register CoolifyProject model"
```

---

### Task 11: Remove Vercel code

**Files:**
- Delete: `internal/services/vercel/vercel.go`
- Delete: `internal/models/vercel_project.go`
- Delete: `internal/database/vercel_projects.go`

- [ ] **Step 1: Verify no remaining references to Vercel service**

Run: `grep -r "VercelService\|vercel\.Service\|vercel\.NewService\|GetVercelProject\|VercelProject\|vercel_project" internal/ cmd/ --include="*.go" | grep -v "_test.go"`

Expected: no results (all references should have been replaced in previous tasks). If any remain, fix them before proceeding.

- [ ] **Step 2: Delete the files**

```bash
rm internal/services/vercel/vercel.go
rm internal/models/vercel_project.go
rm internal/database/vercel_projects.go
rmdir internal/services/vercel/
```

- [ ] **Step 3: Remove the Vercel index from cmd/main.go**

In `cmd/main.go`, remove the line:
```go
`CREATE INDEX IF NOT EXISTS idx_vercel_projects_app_project_id ON vercel_projects(app_project_id)`,
```

- [ ] **Step 4: Remove VercelProject from createSchema model list in cmd/main.go**

Remove `(*models.VercelProject)(nil)` from the createSchema models slice.

- [ ] **Step 5: Verify full build**

Run: `go build ./...`
Expected: no errors

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "chore: remove Vercel service, model, and database layer"
```

---

### Task 12: Final verification

- [ ] **Step 1: Full build**

Run: `go build -o /dev/null ./...`
Expected: clean build, no errors

- [ ] **Step 2: Check for any remaining Vercel references**

Run: `grep -ri "vercel" internal/ cmd/ --include="*.go" | grep -v "_test.go" | grep -v "// "`

Expected: no functional references. Comments mentioning Vercel in migration context are acceptable.

- [ ] **Step 3: Verify binary builds**

Run: `go build -o bin/app cmd/main.go`
Expected: binary created successfully

- [ ] **Step 4: Commit final state**

If any fixups were needed:
```bash
git add -A
git commit -m "fix: address remaining Vercel references"
```
