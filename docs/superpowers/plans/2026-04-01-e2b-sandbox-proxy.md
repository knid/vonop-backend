# E2B Sandbox Proxy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add two backend endpoints (`POST /sandbox/start`, `GET /sandbox/status`) so the mobile Flutter app can start and check E2B sandbox previews without the dashboard UI.

**Architecture:** The start endpoint proxies to the dashboard's existing `/api/e2b/start` SSE stream, waits for the terminal event, and returns a JSON response. The status endpoint is a DB lookup. A new `internal/services/e2b/` package handles the dashboard proxy and preview URL construction.

**Tech Stack:** Go, net/http, SSE parsing, chi router (existing), Flutter/Dart (mobile app)

---

## File Structure

| File | Action | Responsibility |
|---|---|---|
| `internal/services/e2b/e2b.go` | Create | E2B service: dashboard proxy with SSE parsing, preview URL construction |
| `internal/controllers/controller.go` | Modify | Add `E2BService` field to `Controller` struct and `NewController` params |
| `internal/views/app_deploy_views.go` | Modify | Add `StartAppSandbox` and `GetAppSandboxStatus` view handlers |
| `cmd/main.go` | Modify | Instantiate E2B service, pass to controller, register routes |
| `../try-with-wemob/lib/config/api_config.dart` | Modify | Add sandbox endpoint paths |
| `../try-with-wemob/lib/screens/home_screen.dart` | Modify | Update `_openPreview` to use sandbox start/status instead of preview-codes |

---

### Task 1: Create the E2B service package

**Files:**
- Create: `internal/services/e2b/e2b.go`

- [ ] **Step 1: Create the service file with types and constructor**

```go
package e2b

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

var httpClient = &http.Client{Timeout: 5 * time.Minute}

// StartResult holds the outcome of a sandbox start request.
type StartResult struct {
	Status     string `json:"status"`      // "ready" or "error"
	PreviewURL string `json:"preview_url"`
	SandboxID  string `json:"sandbox_id"`
	Error      string `json:"error,omitempty"`
}

// Service proxies sandbox operations to the dashboard's E2B API routes.
type Service struct {
	DashboardURL  string // e.g. https://dashboard.wemob.io
	PreviewDomain string // e.g. preview.wemob.app
}

func NewService(dashboardURL, previewDomain string) *Service {
	return &Service{
		DashboardURL:  strings.TrimRight(dashboardURL, "/"),
		PreviewDomain: previewDomain,
	}
}

// PreviewURL constructs the public preview URL for a sandbox.
// Format: https://3000-{sandboxId}.{previewDomain}
func (s *Service) PreviewURL(sandboxID string) string {
	if sandboxID == "" {
		return ""
	}
	return fmt.Sprintf("https://3000-%s.%s", sandboxID, s.PreviewDomain)
}
```

- [ ] **Step 2: Add the StartSandbox method with SSE parsing**

Append to `internal/services/e2b/e2b.go`:

```go
// StartSandbox proxies a sandbox start request to the dashboard and blocks
// until the sandbox is ready or an error occurs. It reads the dashboard's
// SSE stream and returns the terminal event.
func (s *Service) StartSandbox(authToken, appID string) (*StartResult, error) {
	body := fmt.Sprintf(`{"appId":"%s"}`, appID)
	req, err := http.NewRequest("POST", s.DashboardURL+"/api/e2b/start", strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("e2b: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+authToken)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("e2b: dashboard request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("e2b: dashboard returned status %d", resp.StatusCode)
	}

	// Parse SSE stream: each event is "data: {json}\n\n"
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")

		var event struct {
			Type       string `json:"type"`
			Status     string `json:"status"`
			PreviewURL string `json:"previewUrl"`
			SandboxID  string `json:"sandboxId"`
			Message    string `json:"message"`
		}
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			slog.Warn("e2b: failed to parse SSE event", "payload", payload, "error", err)
			continue
		}

		switch event.Type {
		case "ready":
			previewURL := event.PreviewURL
			if previewURL == "" {
				previewURL = s.PreviewURL(event.SandboxID)
			}
			return &StartResult{
				Status:     "ready",
				PreviewURL: previewURL,
				SandboxID:  event.SandboxID,
			}, nil

		case "error":
			return &StartResult{
				Status:     "error",
				PreviewURL: event.PreviewURL,
				SandboxID:  event.SandboxID,
				Error:      event.Message,
			}, nil

		case "status":
			slog.Info("e2b: sandbox start progress", "status", event.Status, "app_id", appID)
			continue
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("e2b: stream read error: %w", err)
	}

	return nil, fmt.Errorf("e2b: stream ended without ready or error event")
}
```

- [ ] **Step 3: Verify it compiles**

Run: `cd /home/knid/Projects/wemob/wemob-backend && go build ./internal/services/e2b/`
Expected: No errors.

- [ ] **Step 4: Commit**

```bash
git add internal/services/e2b/e2b.go
git commit -m "feat: add E2B service for dashboard sandbox proxy"
```

---

### Task 2: Wire the E2B service into the Controller

**Files:**
- Modify: `internal/controllers/controller.go`
- Modify: `cmd/main.go`

- [ ] **Step 1: Add E2BService to Controller struct and NewController**

In `internal/controllers/controller.go`, add the import:

```go
e2bService "github.com/knid/wemob/internal/services/e2b"
```

Add field to `Controller` struct (after `RemoteConfig`):

```go
E2BService *e2bService.Service
```

Update `NewController` — add `e2bService *e2bService.Service` as the last parameter and assign it inside the returned struct:

```go
func NewController(db *database.Database, concurrentBuildLimiter chan struct{},
	slackService *services.SlackService, emailService emailServices.EmailService,
	crmService crmServices.CRMService, taskService *asynq.Client, assetsStorage *storage.S3Storage,
	userApiKeysStorage *storage.S3Storage, secretManager secret.SecretManager,
	coolifyService *coolify.Service, giteaService *gitea.Service,
	neonService *neon.Service,
	posthogClient posthog.Client,
	gitRepoService *gitrepo.Service,
	codemagicService *codemagic.Service,
	remoteConfigService *remoteconfig.Service,
	e2b *e2bService.Service) *Controller {

	return &Controller{
		DB:                     db,
		SlackService:           slackService,
		EmailService:           emailService,
		CRMService:             crmService,
		ConcurrentBuildLimiter: concurrentBuildLimiter,
		TaskService:            taskService,
		AssetsStorage:          assetsStorage,
		UserApiKeysStorage:     userApiKeysStorage,
		SecretManager:          secretManager,
		CodemagicService:       codemagicService,
		CoolifyService:         coolifyService,
		GiteaService:           giteaService,
		NeonService:            neonService,
		GitRepoService:         gitRepoService,
		PostHog:                posthogClient,
		RemoteConfig:           remoteConfigService,
		E2BService:             e2b,
	}
}
```

- [ ] **Step 2: Instantiate E2B service in main.go and pass to NewController**

In `cmd/main.go`, add the import:

```go
e2bService "github.com/knid/wemob/internal/services/e2b"
```

Before the `controller := controllers.NewController(...)` call (around line 788), add:

```go
e2b := e2bService.NewService(
	sm.GetSecret("DASHBOARD_URL"),
	sm.GetSecret("E2B_PREVIEW_DOMAIN"),
)
```

Then add `e2b` as the last argument to `controllers.NewController(...)`:

```go
controller := controllers.NewController(
	db,
	make(chan struct{}, constants.MAX_CONCURRENT_BUILDS),
	services.NewSlackService(sm.GetSecret("SLACK_API_KEY")),
	emailServices.NewBrevoEmailService(sm.GetSecret("BREVO_API_KEY")),
	crmServices.NewBrevoCRMService(sm.GetSecret("BREVO_API_KEY")),
	asynq.NewClient(asynq.RedisClientOpt{
		Addr:      sm.GetSecret("REDIS_ADDR"),
		Username:  sm.GetSecret("REDIS_USERNAME"),
		Password:  sm.GetSecret("REDIS_PASSWORD"),
		TLSConfig: &tls.Config{},
	}),
	assetsStorage,
	userApiKeysStorage,
	sm,
	coolifyService,
	giteaService,
	neonService,
	posthogClient,
	gitRepoService,
	codemagicService,
	remoteConfigService,
	e2b,
)
```

- [ ] **Step 3: Verify it compiles**

Run: `cd /home/knid/Projects/wemob/wemob-backend && go build ./cmd/main.go`
Expected: No errors.

- [ ] **Step 4: Commit**

```bash
git add internal/controllers/controller.go cmd/main.go
git commit -m "feat: wire E2B service into Controller"
```

---

### Task 3: Add view handlers and routes

**Files:**
- Modify: `internal/views/app_deploy_views.go`
- Modify: `cmd/main.go`

- [ ] **Step 1: Add StartAppSandbox handler**

Add to `internal/views/app_deploy_views.go` (after the existing `SetAppSandbox` handler):

```go
// POST /v2/apps/{appId}/sandbox/start
func (v *HttpViews) StartAppSandbox(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	token := utils.ExtractTokenFromHeader(r)
	result, err := v.Controller.E2BService.StartSandbox(token, appID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadGateway)
		return
	}

	// Save sandbox ID if we got one
	if result.SandboxID != "" {
		_ = v.Controller.DB.UpdateAppProjectSandboxID(project.ID.String(), result.SandboxID)
	}

	if result.Status == "error" {
		utils.JSONResponse(w, result, http.StatusBadGateway)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}
```

- [ ] **Step 2: Add GetAppSandboxStatus handler**

Add to `internal/views/app_deploy_views.go` (after `StartAppSandbox`):

```go
// GET /v2/apps/{appId}/sandbox/status
func (v *HttpViews) GetAppSandboxStatus(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	if project.SandboxID == "" {
		utils.JSONResponse(w, map[string]string{
			"status":      "stopped",
			"preview_url": "",
			"sandbox_id":  "",
		}, http.StatusOK)
		return
	}

	utils.JSONResponse(w, map[string]string{
		"status":      "ready",
		"preview_url": v.Controller.E2BService.PreviewURL(project.SandboxID),
		"sandbox_id":  project.SandboxID,
	}, http.StatusOK)
}
```

- [ ] **Step 3: Register the routes in main.go**

In `cmd/main.go`, find the existing sandbox routes (around line 1239-1241):

```go
// Sandbox ID persistence for E2B preview
r.Get("/sandbox", httpViews.GetAppSandbox)
r.Put("/sandbox", httpViews.SetAppSandbox)
```

Add the new routes directly after:

```go
// Sandbox lifecycle for mobile preview
r.Post("/sandbox/start", httpViews.StartAppSandbox)
r.Get("/sandbox/status", httpViews.GetAppSandboxStatus)
```

- [ ] **Step 4: Verify it compiles**

Run: `cd /home/knid/Projects/wemob/wemob-backend && go build ./cmd/main.go`
Expected: No errors.

- [ ] **Step 5: Commit**

```bash
git add internal/views/app_deploy_views.go cmd/main.go
git commit -m "feat: add sandbox start and status endpoints for mobile preview"
```

---

### Task 4: Update Flutter app to use sandbox endpoints

**Files:**
- Modify: `../try-with-wemob/lib/config/api_config.dart`
- Modify: `../try-with-wemob/lib/screens/home_screen.dart`

- [ ] **Step 1: Add sandbox endpoint paths to api_config.dart**

Add these two lines to the `ApiConfig` class in `../try-with-wemob/lib/config/api_config.dart`:

```dart
  // Sandbox
  static String sandboxStartPath(String appId) => '/v2/apps/$appId/sandbox/start';
  static String sandboxStatusPath(String appId) => '/v2/apps/$appId/sandbox/status';
```

- [ ] **Step 2: Update `_openPreview` in home_screen.dart**

Replace the existing `_openPreview` method in `../try-with-wemob/lib/screens/home_screen.dart` with:

```dart
  Future<void> _openPreview(AppProject app) async {
    // Show loading overlay
    showDialog(
      context: context,
      barrierDismissible: false,
      builder: (_) => const Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            CircularProgressIndicator(color: Colors.white),
            SizedBox(height: 16),
            Text('Starting preview...', style: TextStyle(color: Colors.white, fontSize: 14, decoration: TextDecoration.none)),
          ],
        ),
      ),
    );

    try {
      // Check sandbox status first
      final status = await ApiService.get(ApiConfig.sandboxStatusPath(app.id));

      String previewUrl;
      String? sandboxId;

      if (status['status'] == 'ready') {
        previewUrl = status['preview_url'];
        sandboxId = status['sandbox_id'];
      } else {
        // Sandbox not running — start it
        final result = await ApiService.post(ApiConfig.sandboxStartPath(app.id), {});
        if (result['status'] != 'ready') {
          throw ApiException(
            statusCode: 502,
            message: result['error']?.toString() ?? 'Failed to start preview',
          );
        }
        previewUrl = result['preview_url'];
        sandboxId = result['sandbox_id'];
      }

      if (!mounted) return;
      Navigator.of(context).pop(); // dismiss loading
      Navigator.of(context).push(MaterialPageRoute(
        builder: (_) => PreviewScreen(
          previewUrl: previewUrl,
          appName: app.name,
          sandboxId: sandboxId,
        ),
      ));
    } on ApiException catch (e) {
      if (!mounted) return;
      Navigator.of(context).pop(); // dismiss loading
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(e.message), backgroundColor: Colors.redAccent),
      );
    } catch (e) {
      if (!mounted) return;
      Navigator.of(context).pop(); // dismiss loading
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(e.toString()), backgroundColor: Colors.redAccent),
      );
    }
  }
```

- [ ] **Step 3: Verify it compiles**

Run: `cd /home/knid/Projects/wemob/try-with-wemob && flutter analyze`
Expected: No errors.

- [ ] **Step 4: Commit**

```bash
cd /home/knid/Projects/wemob/try-with-wemob
git add lib/config/api_config.dart lib/screens/home_screen.dart
git commit -m "feat: use sandbox start/status endpoints for preview"
```

---

### Task 5: Manual smoke test

- [ ] **Step 1: Verify the secrets are documented**

Ensure `DASHBOARD_URL` and `E2B_PREVIEW_DOMAIN` are set in Infisical for your environment. Example values:
- `DASHBOARD_URL` = `https://dashboard.wemob.io`
- `E2B_PREVIEW_DOMAIN` = `preview.wemob.app`

- [ ] **Step 2: Test status endpoint with curl**

```bash
curl -s -H "Authorization: Bearer <token>" \
  http://localhost:8001/v2/apps/<appId>/sandbox/status | jq .
```

Expected (no active sandbox):
```json
{
  "status": "stopped",
  "preview_url": "",
  "sandbox_id": ""
}
```

- [ ] **Step 3: Test start endpoint with curl**

```bash
curl -s -H "Authorization: Bearer <token>" \
  -X POST http://localhost:8001/v2/apps/<appId>/sandbox/start | jq .
```

Expected (after ~1-3 minutes):
```json
{
  "status": "ready",
  "preview_url": "https://3000-<id>.preview.wemob.app",
  "sandbox_id": "<id>"
}
```

- [ ] **Step 4: Verify status now returns ready**

```bash
curl -s -H "Authorization: Bearer <token>" \
  http://localhost:8001/v2/apps/<appId>/sandbox/status | jq .
```

Expected:
```json
{
  "status": "ready",
  "preview_url": "https://3000-<id>.preview.wemob.app",
  "sandbox_id": "<id>"
}
```
