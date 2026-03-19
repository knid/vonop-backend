# "Try With Wemob" Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a one-time preview code system (backend), update the dashboard QR flow, and create a standalone Flutter app ("Try With Wemob") where users log in, see their mobile apps, and preview them via WebView — either from a live E2B sandbox or the deployed URL.

**Architecture:** The backend adds a `PreviewCode` model with generate/redeem endpoints. The dashboard calls generate on QR click instead of hardcoding URLs. The Flutter app authenticates against the existing Wemob API, lists the user's `mobile_app` projects, and opens a full-screen WebView for preview. QR scanning also redeems one-time codes for team/client sharing.

**Tech Stack:** Go (backend, Bun ORM, chi router), Next.js dashboard (React), Flutter 3.x (mobile app — `flutter_inappwebview`, `mobile_scanner`, `google_sign_in`, `flutter_secure_storage`)

---

## Chunk 1: Backend — Preview Code System

This chunk adds the `PreviewCode` model, database layer, controller logic, view handlers, and route registration to the Go backend. No tests exist in this project (per CLAUDE.md), so verification is done via `go build`.

### File Structure

| Action | File | Responsibility |
|--------|------|---------------|
| Create | `internal/models/preview_code.go` | PreviewCode Bun ORM model |
| Create | `internal/database/preview_codes.go` | CRUD: create, get-by-code, mark-redeemed, cleanup |
| Create | `internal/requests/preview_code.go` | Request structs with validation |
| Create | `internal/responses/preview_code.go` | Response structs |
| Create | `internal/controllers/preview_code_controller.go` | Generate code, redeem code, resolve preview URL |
| Create | `internal/views/preview_code_views.go` | HTTP handlers for generate and redeem |
| Modify | `cmd/main.go` | Add PreviewCode to `createSchema()`, register routes |

---

### Task 1: PreviewCode Model

**Files:**
- Create: `internal/models/preview_code.go`

- [ ] **Step 1: Create the PreviewCode model**

```go
package models

import (
	"time"

	"github.com/google/uuid"
)

type PreviewCode struct {
	ID           uuid.UUID  `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	Code         string     `bun:",notnull,unique"`
	AppProjectID uuid.UUID  `bun:",notnull,type:uuid"`
	AppProject   *AppProject `bun:"rel:belongs-to,join:app_project_id=id"`
	UserID       uuid.UUID  `bun:",notnull,type:uuid"`
	User         *User      `bun:"rel:belongs-to,join:user_id=id"`
	SandboxID    string     `bun:",nullzero"`
	PreviewURL   string     `bun:",notnull"`
	ExpiresAt    time.Time  `bun:",notnull"`
	RedeemedAt   time.Time  `bun:",nullzero"`
	RedeemedBy   uuid.UUID  `bun:",type:uuid,nullzero"`
	CreatedAt    time.Time  `bun:"created_at,nullzero,notnull,default:current_timestamp"`
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/knid/Projects/wemob-backend && go build ./internal/models/`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add internal/models/preview_code.go
git commit -m "feat: add PreviewCode model for one-time preview tokens"
```

---

### Task 2: Database Layer

**Files:**
- Create: `internal/database/preview_codes.go`

- [ ] **Step 1: Create the database access layer**

```go
package database

import (
	"context"
	"time"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) CreatePreviewCode(code *models.PreviewCode) (*models.PreviewCode, error) {
	_, err := db.DB.NewInsert().Model(code).Exec(context.Background())
	return code, err
}

func (db *Database) GetPreviewCodeByCode(code string) (*models.PreviewCode, error) {
	var pc models.PreviewCode
	err := db.DB.NewSelect().Model(&pc).Where("code = ?", code).Scan(context.Background())
	return &pc, err
}

func (db *Database) MarkPreviewCodeRedeemed(code string, redeemedBy string) error {
	_, err := db.DB.NewUpdate().Model((*models.PreviewCode)(nil)).
		Set("redeemed_at = ?", time.Now()).
		Set("redeemed_by = ?", redeemedBy).
		Where("code = ?", code).
		Where("redeemed_at IS NULL").
		Exec(context.Background())
	return err
}

func (db *Database) CleanupExpiredPreviewCodes() (int64, error) {
	res, err := db.DB.NewDelete().Model((*models.PreviewCode)(nil)).
		Where("expires_at < ?", time.Now().Add(-1*time.Hour)).
		Exec(context.Background())
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/knid/Projects/wemob-backend && go build ./internal/database/`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add internal/database/preview_codes.go
git commit -m "feat: add PreviewCode database CRUD and cleanup"
```

---

### Task 3: Request & Response Structs

**Files:**
- Create: `internal/requests/preview_code.go`
- Create: `internal/responses/preview_code.go`

- [ ] **Step 1: Create the request struct**

```go
package requests

import (
	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/utils"
)

type GeneratePreviewCodeRequest struct {
	AppProjectID string `json:"app_project_id" validate:"required,uuid" errormgs:"must be a valid app project UUID"`
}

func (r *GeneratePreviewCodeRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[GeneratePreviewCodeRequest](*r, validate)
}
```

- [ ] **Step 2: Create the response structs**

```go
package responses

import (
	"time"

	"github.com/google/uuid"
)

type PreviewCodeResponse struct {
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expires_at"`
}

type PreviewCodeRedeemResponse struct {
	PreviewURL   string    `json:"preview_url"`
	AppProjectID uuid.UUID `json:"app_project_id"`
	AppName      string    `json:"app_name"`
	AppIcon      string    `json:"app_icon,omitempty"`
	SandboxID    string    `json:"sandbox_id,omitempty"`
}
```

- [ ] **Step 3: Verify they compile**

Run: `cd /Users/knid/Projects/wemob-backend && go build ./internal/requests/ && go build ./internal/responses/`
Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add internal/requests/preview_code.go internal/responses/preview_code.go
git commit -m "feat: add preview code request/response types"
```

---

### Task 4: Controller — Generate & Redeem Logic

**Files:**
- Create: `internal/controllers/preview_code_controller.go`

- [ ] **Step 1: Create the controller with generate and redeem methods**

```go
package controllers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
)

const previewCodeTTL = 5 * time.Minute

// GeneratePreviewCode creates a one-time code for previewing an app.
// It resolves the preview URL by checking for an active E2B sandbox first,
// then falling back to the latest deployed web URL.
func (c *Controller) GeneratePreviewCode(user *models.User, appProjectID string) (*models.PreviewCode, error) {
	project, err := c.DB.GetAppProjectByID(appProjectID)
	if err != nil {
		return nil, fmt.Errorf("project not found")
	}
	if project.UserID != user.ID {
		return nil, fmt.Errorf("project not found")
	}

	previewURL, sandboxID, err := c.resolvePreviewURL(project)
	if err != nil {
		return nil, err
	}

	// Generate a cryptographically random code
	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		return nil, fmt.Errorf("failed to generate code")
	}
	hash := sha256.Sum256(randomBytes)
	code := hex.EncodeToString(hash[:])

	pc := &models.PreviewCode{
		Code:         code,
		AppProjectID: uuid.MustParse(appProjectID),
		UserID:       user.ID,
		SandboxID:    sandboxID,
		PreviewURL:   previewURL,
		ExpiresAt:    time.Now().Add(previewCodeTTL),
	}

	pc, err = c.DB.CreatePreviewCode(pc)
	if err != nil {
		return nil, fmt.Errorf("failed to create preview code")
	}

	return pc, nil
}

// RedeemPreviewCode validates and consumes a one-time preview code.
func (c *Controller) RedeemPreviewCode(user *models.User, code string) (*models.PreviewCode, *models.AppProject, error) {
	pc, err := c.DB.GetPreviewCodeByCode(code)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid preview code")
	}

	if time.Now().After(pc.ExpiresAt) {
		return nil, nil, fmt.Errorf("preview code has expired")
	}

	if !pc.RedeemedAt.IsZero() {
		return nil, nil, fmt.Errorf("preview code already used")
	}

	if err := c.DB.MarkPreviewCodeRedeemed(code, user.ID.String()); err != nil {
		return nil, nil, fmt.Errorf("failed to redeem code")
	}

	project, err := c.DB.GetAppProjectByID(pc.AppProjectID.String())
	if err != nil {
		return nil, nil, fmt.Errorf("project not found")
	}

	return pc, project, nil
}

// resolvePreviewURL determines the best preview URL for a project.
// Priority: 1) Active E2B sandbox  2) Latest deployed web URL
func (c *Controller) resolvePreviewURL(project *models.AppProject) (string, string, error) {
	// Check for active E2B sandbox
	if project.SandboxID != "" {
		// The sandbox preview URL is constructed from the sandbox host.
		// The dashboard stores this, but we need to return it.
		// For now, we store the sandbox ID and let the Flutter app
		// connect via the dashboard's E2B proxy. But the simpler approach:
		// the deployed URL always works as fallback, and the sandbox URL
		// is only valid while the dashboard is open. So for the Flutter app,
		// we primarily use the deployed URL.
		//
		// However, the user wants sandbox preview support. The E2B sandbox
		// URLs are accessible publicly (e.g., https://{subdomain}.preview.wemob.app).
		// We can construct this URL if we know the sandbox ID.
		slog.Info("project has active sandbox", "sandbox_id", project.SandboxID, "project_id", project.ID)
	}

	// Get latest web deployment URL (uses existing DB helper)
	deployment, err := c.DB.GetLatestAppDeployment(project.ID.String(), "web")
	if err == nil && deployment.DeployedURL != "" && deployment.Status == "deployed" {
		return deployment.DeployedURL, project.SandboxID, nil
	}

	// No deployment found — check if sandbox exists as fallback
	if project.SandboxID != "" {
		// Return a marker so the client knows to use sandbox
		return "", project.SandboxID, nil
	}

	return "", "", fmt.Errorf("app has no deployment or active preview — deploy your app first")
}
```

- [ ] **Step 2: Verify it compiles**

Note: No new DB method needed — the controller uses the existing `c.DB.GetLatestAppDeployment(projectID, "web")` from `internal/database/app_deployments.go:21`.



Run: `cd /Users/knid/Projects/wemob-backend && go build ./internal/...`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add internal/controllers/preview_code_controller.go
git commit -m "feat: add preview code generate/redeem controller logic"
```

---

### Task 5: View Handlers

**Files:**
- Create: `internal/views/preview_code_views.go`

- [ ] **Step 1: Create the HTTP view handlers**

```go
package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

// POST /v1/preview-codes
func (v *HttpViews) GeneratePreviewCode(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	var req requests.GeneratePreviewCodeRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := req.Validate(validate); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	pc, err := v.Controller.GeneratePreviewCode(user, req.AppProjectID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, responses.PreviewCodeResponse{
		Code:      pc.Code,
		ExpiresAt: pc.ExpiresAt,
	}, http.StatusCreated)
}

// POST /v1/preview-codes/{code}/redeem
func (v *HttpViews) RedeemPreviewCode(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	code := chi.URLParam(r, "code")
	if code == "" {
		utils.JSONErrorResponse(w, "code is required", http.StatusBadRequest)
		return
	}

	pc, project, err := v.Controller.RedeemPreviewCode(user, code)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Get app icon presigned URL if available
	var iconURL string
	if project.ScreenshotS3Key != "" {
		if presigned, err := v.Controller.AssetsStorage.GetObject(r.Context(), project.ScreenshotS3Key, 3600); err == nil {
			iconURL = presigned.URL
		}
	}

	utils.JSONResponse(w, responses.PreviewCodeRedeemResponse{
		PreviewURL:   pc.PreviewURL,
		AppProjectID: pc.AppProjectID,
		AppName:      project.Name,
		AppIcon:      iconURL,
		SandboxID:    pc.SandboxID,
	}, http.StatusOK)
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/knid/Projects/wemob-backend && go build ./internal/views/`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add internal/views/preview_code_views.go
git commit -m "feat: add preview code HTTP handlers (generate + redeem)"
```

---

### Task 6: Route Registration & Schema

**Files:**
- Modify: `cmd/main.go`

- [ ] **Step 1: Add PreviewCode to createSchema**

In `cmd/main.go`, inside the `createSchema` function's model list (after line 126 `(*models.MobileAppConfig)(nil),`), add:

```go
		// Preview codes for Try With Wemob
		(*models.PreviewCode)(nil),
```

- [ ] **Step 2: Register preview-code routes**

In `cmd/main.go`, inside the `/v1` route block (after the `/admin` route block around line 1043), add:

```go
			r.Route("/preview-codes", func(r chi.Router) {
				r.Post("/", httpViews.GeneratePreviewCode)
				r.Post("/{code}/redeem", httpViews.RedeemPreviewCode)
			})
```

- [ ] **Step 3: Add a startup goroutine for periodic cleanup**

In `cmd/main.go`, after the existing startup goroutines (e.g., `BackfillV1ProjectsForMobileApps`), add:

```go
	// Periodically clean up expired preview codes
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			if n, err := controller.DB.CleanupExpiredPreviewCodes(); err != nil {
				slog.Error("preview code cleanup failed", "error", err)
			} else if n > 0 {
				slog.Info("cleaned up expired preview codes", "count", n)
			}
		}
	}()
```

- [ ] **Step 4: Verify full build**

Run: `cd /Users/knid/Projects/wemob-backend && go build -o /dev/null ./cmd/main.go`
Expected: no errors

- [ ] **Step 5: Commit**

```bash
git add cmd/main.go
git commit -m "feat: register preview code routes and schema + hourly cleanup"
```

---

## Chunk 2: Dashboard — Update QR Code Flow

This chunk updates the dashboard's "Try with Wemob" dialog to call the backend for a one-time code instead of hardcoding the URL.

### File Structure

| Action | File | Responsibility |
|--------|------|---------------|
| Modify | `components/app-studio/workspace-toolbar.tsx` | Call API to generate code, show loading state, use code in QR |

---

### Task 7: Update QR Dialog to Use Preview Codes

**Files:**
- Modify: `/Users/knid/Projects/wemob-dashboard/components/app-studio/workspace-toolbar.tsx`

- [ ] **Step 1: Replace the static QR code value with dynamic code generation**

Currently (around line 125), the component has:
```tsx
const [tryWemobOpen, setTryWemobOpen] = useState(false)
```

Replace the QR dialog logic. Add state for the preview code, and generate it when the dialog opens:

```tsx
const [tryWemobOpen, setTryWemobOpen] = useState(false)
const [previewCode, setPreviewCode] = useState<string | null>(null)
const [previewCodeLoading, setPreviewCodeLoading] = useState(false)
const [previewCodeError, setPreviewCodeError] = useState<string | null>(null)

const handleTryWemobOpen = async () => {
  setTryWemobOpen(true)
  setPreviewCode(null)
  setPreviewCodeError(null)
  setPreviewCodeLoading(true)

  try {
    const token = localStorage.getItem("auth_token")
    const res = await fetch(`${process.env.NEXT_PUBLIC_API_URL}/v1/preview-codes`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        ...(token ? { Authorization: `Bearer ${token}` } : {}),
      },
      body: JSON.stringify({ app_project_id: activeAppId }),
    })
    if (!res.ok) {
      const data = await res.json()
      throw new Error(data.message || "Failed to generate preview code")
    }
    const data = await res.json()
    setPreviewCode(data.code)
  } catch (err: any) {
    setPreviewCodeError(err.message || "Failed to generate preview code")
  } finally {
    setPreviewCodeLoading(false)
  }
}
```

- [ ] **Step 2: Update the button click handler**

Find the button that sets `setTryWemobOpen(true)` (around line 388-398) and change its `onClick` from `() => setTryWemobOpen(true)` to `handleTryWemobOpen`.

- [ ] **Step 3: Update the QR code value in the dialog**

Replace the `QRCodeSVG` value (line 475):

From:
```tsx
value={`wemob://preview?url=${encodeURIComponent(previewUrl || "")}`}
```

To:
```tsx
value={previewCode ? `wemob://preview/${previewCode}` : ""}
```

- [ ] **Step 4: Add loading and error states in the dialog**

Inside the dialog content (after the QRCodeSVG), wrap it in a conditional:

```tsx
<div className="bg-white rounded-2xl p-4 shadow-sm border">
  {previewCodeLoading ? (
    <div className="w-[200px] h-[200px] flex items-center justify-center">
      <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
    </div>
  ) : previewCodeError ? (
    <div className="w-[200px] h-[200px] flex items-center justify-center text-center">
      <p className="text-sm text-destructive">{previewCodeError}</p>
    </div>
  ) : previewCode ? (
    <QRCodeSVG
      value={`wemob://preview/${previewCode}`}
      size={200}
      level="M"
      includeMargin={false}
    />
  ) : null}
</div>
```

- [ ] **Step 5: Add `Loader2` import if not already imported**

Check if `Loader2` is imported from `lucide-react` at the top of the file. If not, add it to the existing lucide-react import line.

- [ ] **Step 6: Verify dashboard builds**

Run: `cd /Users/knid/Projects/wemob-dashboard && npm run build`
Expected: no errors

- [ ] **Step 7: Commit**

```bash
cd /Users/knid/Projects/wemob-dashboard
git add components/app-studio/workspace-toolbar.tsx
git commit -m "feat: use one-time preview codes for Try With Wemob QR"
```

---

## Chunk 3: Flutter App — Project Setup & Auth

This chunk creates the Flutter project from scratch with the core structure, auth flow (email + Google), and API service layer.

### File Structure

| Action | File | Responsibility |
|--------|------|---------------|
| Create | `try_with_wemob/` | Flutter project root (via `flutter create`) |
| Create | `lib/config/api_config.dart` | API base URL and endpoint constants |
| Create | `lib/services/auth_service.dart` | Login (email + Google), token persistence, logout |
| Create | `lib/services/api_service.dart` | Authenticated HTTP client wrapping all API calls |
| Create | `lib/models/user.dart` | User model (from API response) |
| Create | `lib/screens/login_screen.dart` | Email/password form + Google Sign-In button |
| Create | `lib/main.dart` | App entry, MaterialApp, routing, theme |

---

### Task 8: Create Flutter Project

- [ ] **Step 1: Create the Flutter project**

Run: `cd /Users/knid/Projects && flutter create --org io.wemob --project-name try_with_wemob try_with_wemob`
Expected: Flutter project scaffold created

- [ ] **Step 2: Clean default files**

Delete the default test and example counter files:

Run:
```bash
cd /Users/knid/Projects/try_with_wemob
rm -f test/widget_test.dart
rm -f lib/main.dart
```

- [ ] **Step 3: Add dependencies to pubspec.yaml**

Replace the `dependencies` and `dev_dependencies` sections in `pubspec.yaml`:

```yaml
dependencies:
  flutter:
    sdk: flutter
  cupertino_icons: ^1.0.8
  flutter_inappwebview: ^6.0.0
  mobile_scanner: ^6.0.2
  http: ^1.2.2
  flutter_secure_storage: ^9.2.3
  google_sign_in: ^6.2.2
  shared_preferences: ^2.3.1
  cached_network_image: ^3.4.1
  shimmer: ^3.0.0

dev_dependencies:
  flutter_test:
    sdk: flutter
  flutter_lints: ^5.0.0

flutter:
  uses-material-design: true
  assets:
    - assets/
    - assets/images/
```

- [ ] **Step 4: Create asset directories**

Run:
```bash
mkdir -p /Users/knid/Projects/try_with_wemob/assets/images
```

- [ ] **Step 5: Run flutter pub get**

Run: `cd /Users/knid/Projects/try_with_wemob && flutter pub get`
Expected: packages resolved successfully

- [ ] **Step 6: Create directory structure**

Run:
```bash
mkdir -p /Users/knid/Projects/try_with_wemob/lib/{config,services,models,screens,widgets}
```

- [ ] **Step 7: Commit**

```bash
cd /Users/knid/Projects/try_with_wemob
git init
git add .
git commit -m "chore: scaffold Flutter project with dependencies"
```

---

### Task 9: API Config & HTTP Client

**Files:**
- Create: `lib/config/api_config.dart`
- Create: `lib/services/api_service.dart`

- [ ] **Step 1: Create API config**

```dart
class ApiConfig {
  static const String baseUrl = 'https://api.wemob.io';

  // Auth
  static const String loginPath = '/v1/users/login';
  static const String registerPath = '/v1/users/register';
  static const String userMePath = '/v1/users/me';
  static const String googleAuthPath = '/v1/users/auth/google';

  // Apps
  static const String appsPath = '/v2/apps';

  // Preview codes
  static const String previewCodesPath = '/v1/preview-codes';

  // Mobile config (public, no auth)
  static String mobileConfigPath(String projectId) =>
      '/v1/mobile/$projectId/config';
}
```

- [ ] **Step 2: Create API service**

```dart
import 'dart:convert';
import 'package:http/http.dart' as http;
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import '../config/api_config.dart';

class ApiService {
  static const _storage = FlutterSecureStorage();
  static const _tokenKey = 'auth_token';

  static Future<String?> getToken() => _storage.read(key: _tokenKey);
  static Future<void> setToken(String token) =>
      _storage.write(key: _tokenKey, value: token);
  static Future<void> clearToken() => _storage.delete(key: _tokenKey);

  static Future<Map<String, String>> _headers() async {
    final token = await getToken();
    return {
      'Content-Type': 'application/json',
      if (token != null) 'Authorization': 'Bearer $token',
    };
  }

  static Future<Map<String, dynamic>> get(String path) async {
    final res = await http.get(
      Uri.parse('${ApiConfig.baseUrl}$path'),
      headers: await _headers(),
    );
    return _handleResponse(res);
  }

  static Future<Map<String, dynamic>> post(
      String path, Map<String, dynamic> body) async {
    final res = await http.post(
      Uri.parse('${ApiConfig.baseUrl}$path'),
      headers: await _headers(),
      body: jsonEncode(body),
    );
    return _handleResponse(res);
  }

  /// GET that returns a JSON array (e.g., /v2/apps)
  static Future<List<dynamic>> getList(String path) async {
    final res = await http.get(
      Uri.parse('${ApiConfig.baseUrl}$path'),
      headers: await _headers(),
    );
    if (res.statusCode >= 200 && res.statusCode < 300) {
      return jsonDecode(res.body) as List<dynamic>;
    }
    final body = jsonDecode(res.body);
    throw ApiException(
      statusCode: res.statusCode,
      message: body is Map ? (body['message']?.toString() ?? 'Request failed') : 'Request failed',
    );
  }

  static Map<String, dynamic> _handleResponse(http.Response res) {
    final body = jsonDecode(res.body) as Map<String, dynamic>;
    if (res.statusCode >= 200 && res.statusCode < 300) {
      return body;
    }
    throw ApiException(
      statusCode: res.statusCode,
      message: body['message']?.toString() ?? 'Request failed',
    );
  }
}

class ApiException implements Exception {
  final int statusCode;
  final String message;
  ApiException({required this.statusCode, required this.message});

  @override
  String toString() => message;
}
```

- [ ] **Step 3: Verify it compiles**

Run: `cd /Users/knid/Projects/try_with_wemob && flutter analyze lib/config/ lib/services/api_service.dart`
Expected: no issues

- [ ] **Step 4: Commit**

```bash
git add lib/config/api_config.dart lib/services/api_service.dart
git commit -m "feat: add API config and authenticated HTTP client"
```

---

### Task 10: User Model

**Files:**
- Create: `lib/models/user.dart`

- [ ] **Step 1: Create the User model**

```dart
class User {
  final String id;
  final String name;
  final String email;
  final String photo;
  final String subscriptionStatus;
  final String provider;
  final bool verified;

  User({
    required this.id,
    required this.name,
    required this.email,
    required this.photo,
    required this.subscriptionStatus,
    required this.provider,
    required this.verified,
  });

  factory User.fromJson(Map<String, dynamic> json) {
    return User(
      id: json['id'] ?? '',
      name: json['name'] ?? '',
      email: json['email'] ?? '',
      photo: json['profile_photo'] ?? '',
      subscriptionStatus: json['subscription_status'] ?? '',
      provider: json['provider'] ?? '',
      verified: json['verified'] ?? false,
    );
  }
}
```

- [ ] **Step 2: Commit**

```bash
git add lib/models/user.dart
git commit -m "feat: add User model"
```

---

### Task 11: Auth Service

**Files:**
- Create: `lib/services/auth_service.dart`

- [ ] **Step 1: Create the auth service**

```dart
import 'package:google_sign_in/google_sign_in.dart';
import '../config/api_config.dart';
import '../models/user.dart';
import 'api_service.dart';

class AuthService {
  static final GoogleSignIn _googleSignIn = GoogleSignIn(
    scopes: ['email', 'profile'],
  );

  /// Email + password login
  static Future<User> loginWithEmail(String email, String password) async {
    final data = await ApiService.post(ApiConfig.loginPath, {
      'email': email,
      'password': password,
    });
    await ApiService.setToken(data['token']);
    return getUser();
  }

  /// Google Sign-In: gets the Google ID token, sends to backend OAuth callback
  static Future<User> loginWithGoogle() async {
    final account = await _googleSignIn.signIn();
    if (account == null) throw Exception('Google sign-in cancelled');

    final auth = await account.authentication;
    final idToken = auth.idToken;
    if (idToken == null) throw Exception('Failed to get Google ID token');

    // The backend handles Google OAuth via redirect flow.
    // For mobile, we send the ID token directly for verification.
    final data = await ApiService.post('/v1/users/auth/google/mobile', {
      'id_token': idToken,
      'email': account.email,
      'name': account.displayName ?? '',
      'photo': account.photoUrl ?? '',
    });
    await ApiService.setToken(data['token']);
    return getUser();
  }

  /// Get current user from stored token
  static Future<User> getUser() async {
    final data = await ApiService.get(ApiConfig.userMePath);
    return User.fromJson(data);
  }

  /// Check if user is logged in (has valid token)
  static Future<bool> isLoggedIn() async {
    final token = await ApiService.getToken();
    if (token == null) return false;
    try {
      await getUser();
      return true;
    } catch (_) {
      await ApiService.clearToken();
      return false;
    }
  }

  /// Logout
  static Future<void> logout() async {
    await _googleSignIn.signOut();
    await ApiService.clearToken();
  }
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/knid/Projects/try_with_wemob && flutter analyze lib/services/auth_service.dart`
Expected: no issues

- [ ] **Step 3: Commit**

```bash
git add lib/services/auth_service.dart
git commit -m "feat: add auth service with email and Google login"
```

---

### Task 12: Backend — Google Mobile Auth Endpoint

The existing backend uses goth's redirect-based OAuth for Google. The Flutter app sends a Google ID token directly instead. We need a new endpoint. This follows the exact same pattern as the existing `GoogleLogin` in `internal/controllers/users.go:277-347`.

**Files:**
- Create: `internal/requests/oauth_mobile.go` (request struct — per codebase convention, request structs live in `requests/`)
- Create: `internal/controllers/oauth_mobile_controller.go`
- Create: `internal/views/oauth_mobile_views.go`
- Modify: `cmd/main.go` (add route)

- [ ] **Step 1: Create the request struct**

```go
package requests

type GoogleMobileLoginRequest struct {
	IDToken string `json:"id_token" validate:"required"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	Photo   string `json:"photo"`
}
```

- [ ] **Step 2: Create the mobile Google auth controller**

Follow the exact pattern from `GoogleLogin` in `users.go:277-347` — inline Stripe customer creation, use `utils.RandomHex` with two return values, and use `c.DB.CreateToken` (which auto-deletes old tokens).

```go
package controllers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
	"github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/customer"
)

// GoogleMobileLogin verifies a Google ID token from the mobile app and returns a session token.
// Mirrors the existing GoogleLogin flow but accepts a raw ID token instead of goth redirect.
func (c *Controller) GoogleMobileLogin(req requests.GoogleMobileLoginRequest) (*responses.UserLoginResponse, error) {
	// Verify the Google ID token by calling Google's tokeninfo endpoint
	resp, err := http.Get("https://oauth2.googleapis.com/tokeninfo?id_token=" + req.IDToken)
	if err != nil || resp.StatusCode != 200 {
		return nil, fmt.Errorf("invalid Google token")
	}
	defer resp.Body.Close()

	var tokenInfo struct {
		Email         string `json:"email"`
		EmailVerified string `json:"email_verified"`
		Sub           string `json:"sub"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenInfo); err != nil {
		return nil, fmt.Errorf("failed to verify Google token")
	}

	if tokenInfo.Email == "" || tokenInfo.EmailVerified != "true" {
		return nil, fmt.Errorf("invalid Google token: email not verified")
	}

	// Use the verified email from Google's response, not the client-provided one
	email := tokenInfo.Email

	// Check if user exists — auto-register if not (mirrors GoogleLogin at users.go:278-320)
	user, err := c.DB.GetUserByEmail(email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			pass, _ := utils.RandomHex(32)
			hashedPass, _ := utils.HashPassword(pass)
			newUser := models.User{
				Name:     req.Name,
				Email:    email,
				Photo:    req.Photo,
				Password: hashedPass,
				Provider: "Google",
				Verified: true,
			}

			user, err = c.DB.CreateUser(&newUser)
			if err != nil {
				return nil, fmt.Errorf("failed to create user")
			}

			// Create Stripe customer (inlined, same as GoogleLogin)
			params := &stripe.CustomerParams{
				Email: stripe.String(user.Email),
				Metadata: map[string]string{
					"user_id": user.ID.String(),
				},
			}
			stripeCustomer, err := customer.New(params)
			if err != nil {
				return nil, err
			}
			user.CustomerID = stripeCustomer.ID
			c.DB.UpdateUser(user)

			c.assignFreePlan(user)

			go c.CRMService.SendEvent(user, constants.BREVO_EVENT_NEW_USER, map[string]any{
				"provider": "google_mobile",
			})
		} else {
			return nil, fmt.Errorf("failed to look up user")
		}
	}

	// Create session token (CreateToken auto-deletes old tokens for this user)
	tokenStr, err := utils.RandomHex(32)
	if err != nil {
		return nil, err
	}

	token := models.Token{
		UserID:    user.ID,
		String:    tokenStr,
		ExpiredAt: time.Now().AddDate(0, 0, 14),
	}

	createdToken, err := c.DB.CreateToken(&token)
	if err != nil {
		return nil, err
	}

	go c.CRMService.SendEvent(user, constants.BREVO_EVENT_NEW_LOGIN, map[string]any{
		"provider": "google_mobile",
	})

	return &responses.UserLoginResponse{
		Token:     createdToken.String,
		ExpiredAt: createdToken.ExpiredAt,
	}, nil
}
```

- [ ] **Step 3: Create the view handler**

```go
package views

import (
	"net/http"

	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
)

// POST /v1/users/auth/google/mobile
func (v *HttpViews) GoogleMobileLogin(w http.ResponseWriter, r *http.Request) {
	var req requests.GoogleMobileLoginRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.IDToken == "" {
		utils.JSONErrorResponse(w, "id_token is required", http.StatusBadRequest)
		return
	}

	loginRes, err := v.Controller.GoogleMobileLogin(req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	utils.JSONResponse(w, loginRes, http.StatusOK)
}
```

- [ ] **Step 4: Register the route**

In `cmd/main.go`, inside the `/v1/users` route block (after the existing `r.Get("/auth/{provider}", httpViews.OAuthBegin)` around line 898), add:

```go
				r.Post("/auth/google/mobile", httpViews.GoogleMobileLogin)
```

- [ ] **Step 5: Verify full build**

Run: `cd /Users/knid/Projects/wemob-backend && go build -o /dev/null ./cmd/main.go`
Expected: no errors

- [ ] **Step 6: Commit**

```bash
cd /Users/knid/Projects/wemob-backend
git add internal/requests/oauth_mobile.go internal/views/oauth_mobile_views.go internal/controllers/oauth_mobile_controller.go cmd/main.go
git commit -m "feat: add Google mobile auth endpoint for Flutter app"
```

---

### Task 13: Login Screen

**Files:**
- Create: `/Users/knid/Projects/try_with_wemob/lib/screens/login_screen.dart`

- [ ] **Step 1: Create the login screen**

```dart
import 'package:flutter/material.dart';
import '../services/auth_service.dart';
import '../services/api_service.dart';
import 'home_screen.dart';

class LoginScreen extends StatefulWidget {
  const LoginScreen({super.key});

  @override
  State<LoginScreen> createState() => _LoginScreenState();
}

class _LoginScreenState extends State<LoginScreen> {
  final _emailController = TextEditingController();
  final _passwordController = TextEditingController();
  bool _loading = false;
  bool _obscurePassword = true;
  String? _error;

  Future<void> _loginWithEmail() async {
    final email = _emailController.text.trim();
    final password = _passwordController.text;
    if (email.isEmpty || password.isEmpty) {
      setState(() => _error = 'Please enter email and password');
      return;
    }

    setState(() {
      _loading = true;
      _error = null;
    });

    try {
      await AuthService.loginWithEmail(email, password);
      if (!mounted) return;
      Navigator.of(context).pushReplacement(
        MaterialPageRoute(builder: (_) => const HomeScreen()),
      );
    } on ApiException catch (e) {
      setState(() => _error = e.message);
    } catch (e) {
      setState(() => _error = 'Login failed. Please try again.');
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  Future<void> _loginWithGoogle() async {
    setState(() {
      _loading = true;
      _error = null;
    });

    try {
      await AuthService.loginWithGoogle();
      if (!mounted) return;
      Navigator.of(context).pushReplacement(
        MaterialPageRoute(builder: (_) => const HomeScreen()),
      );
    } catch (e) {
      setState(() => _error = e.toString());
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: Colors.black,
      body: SafeArea(
        child: Center(
          child: SingleChildScrollView(
            padding: const EdgeInsets.symmetric(horizontal: 32),
            child: Column(
              mainAxisAlignment: MainAxisAlignment.center,
              children: [
                // Logo
                Container(
                  width: 80,
                  height: 80,
                  decoration: BoxDecoration(
                    color: Colors.white,
                    borderRadius: BorderRadius.circular(20),
                  ),
                  child: const Center(
                    child: Text('W',
                        style: TextStyle(
                            fontSize: 36,
                            fontWeight: FontWeight.w800,
                            color: Colors.black)),
                  ),
                ),
                const SizedBox(height: 24),
                const Text(
                  'Try with Wemob',
                  style: TextStyle(
                    color: Colors.white,
                    fontSize: 28,
                    fontWeight: FontWeight.w700,
                    letterSpacing: -0.5,
                  ),
                ),
                const SizedBox(height: 8),
                Text(
                  'Preview your apps on device',
                  style: TextStyle(
                    color: Colors.white.withValues(alpha: 0.5),
                    fontSize: 16,
                  ),
                ),
                const SizedBox(height: 48),

                // Email field
                TextField(
                  controller: _emailController,
                  keyboardType: TextInputType.emailAddress,
                  style: const TextStyle(color: Colors.white),
                  decoration: _inputDecoration('Email'),
                ),
                const SizedBox(height: 12),

                // Password field
                TextField(
                  controller: _passwordController,
                  obscureText: _obscurePassword,
                  style: const TextStyle(color: Colors.white),
                  decoration: _inputDecoration('Password').copyWith(
                    suffixIcon: IconButton(
                      icon: Icon(
                        _obscurePassword
                            ? Icons.visibility_off_outlined
                            : Icons.visibility_outlined,
                        color: Colors.white38,
                        size: 20,
                      ),
                      onPressed: () =>
                          setState(() => _obscurePassword = !_obscurePassword),
                    ),
                  ),
                  onSubmitted: (_) => _loginWithEmail(),
                ),
                const SizedBox(height: 8),

                // Error message
                if (_error != null) ...[
                  const SizedBox(height: 8),
                  Text(_error!,
                      style: const TextStyle(color: Colors.redAccent, fontSize: 13),
                      textAlign: TextAlign.center),
                ],

                const SizedBox(height: 20),

                // Login button
                SizedBox(
                  width: double.infinity,
                  height: 52,
                  child: ElevatedButton(
                    onPressed: _loading ? null : _loginWithEmail,
                    style: ElevatedButton.styleFrom(
                      backgroundColor: Colors.white,
                      foregroundColor: Colors.black,
                      shape: RoundedRectangleBorder(
                        borderRadius: BorderRadius.circular(14),
                      ),
                      elevation: 0,
                    ),
                    child: _loading
                        ? const SizedBox(
                            width: 20,
                            height: 20,
                            child: CircularProgressIndicator(
                                strokeWidth: 2, color: Colors.black))
                        : const Text('Sign In',
                            style: TextStyle(
                                fontSize: 16, fontWeight: FontWeight.w600)),
                  ),
                ),

                const SizedBox(height: 20),

                // Divider
                Row(children: [
                  Expanded(
                      child:
                          Divider(color: Colors.white.withValues(alpha: 0.15))),
                  Padding(
                    padding: const EdgeInsets.symmetric(horizontal: 16),
                    child: Text('or',
                        style: TextStyle(
                            color: Colors.white.withValues(alpha: 0.4),
                            fontSize: 13)),
                  ),
                  Expanded(
                      child:
                          Divider(color: Colors.white.withValues(alpha: 0.15))),
                ]),

                const SizedBox(height: 20),

                // Google button
                SizedBox(
                  width: double.infinity,
                  height: 52,
                  child: OutlinedButton.icon(
                    onPressed: _loading ? null : _loginWithGoogle,
                    icon: const Text('G',
                        style: TextStyle(
                            fontSize: 20, fontWeight: FontWeight.w700)),
                    label: const Text('Continue with Google',
                        style: TextStyle(fontSize: 15)),
                    style: OutlinedButton.styleFrom(
                      foregroundColor: Colors.white,
                      side: BorderSide(
                          color: Colors.white.withValues(alpha: 0.2)),
                      shape: RoundedRectangleBorder(
                        borderRadius: BorderRadius.circular(14),
                      ),
                    ),
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }

  InputDecoration _inputDecoration(String hint) {
    return InputDecoration(
      hintText: hint,
      hintStyle: TextStyle(color: Colors.white.withValues(alpha: 0.3)),
      filled: true,
      fillColor: Colors.white.withValues(alpha: 0.08),
      border: OutlineInputBorder(
        borderRadius: BorderRadius.circular(14),
        borderSide: BorderSide.none,
      ),
      contentPadding: const EdgeInsets.symmetric(horizontal: 20, vertical: 16),
    );
  }

  @override
  void dispose() {
    _emailController.dispose();
    _passwordController.dispose();
    super.dispose();
  }
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/knid/Projects/try_with_wemob && flutter analyze lib/screens/login_screen.dart`
Expected: no issues (HomeScreen reference will resolve in next task)

- [ ] **Step 3: Commit**

```bash
cd /Users/knid/Projects/try_with_wemob
git add lib/screens/login_screen.dart
git commit -m "feat: add login screen with email and Google sign-in"
```

---

## Chunk 4: Flutter App — Home Screen & App List

### File Structure

| Action | File | Responsibility |
|--------|------|---------------|
| Create | `lib/models/app_project.dart` | AppProject model for API response |
| Create | `lib/screens/home_screen.dart` | Lists user's mobile apps, pull-to-refresh, FAB for QR scanner |
| Create | `lib/widgets/app_card.dart` | Individual app card in the list |

---

### Task 14: AppProject Model

**Files:**
- Create: `lib/models/app_project.dart`

- [ ] **Step 1: Create the model**

```dart
class AppProject {
  final String id;
  final String name;
  final String slug;
  final String projectType;
  final String status;
  final String screenshotUrl;
  final Map<String, dynamic> theme;
  final DateTime createdAt;

  AppProject({
    required this.id,
    required this.name,
    required this.slug,
    required this.projectType,
    required this.status,
    required this.screenshotUrl,
    required this.theme,
    required this.createdAt,
  });

  factory AppProject.fromJson(Map<String, dynamic> json) {
    return AppProject(
      id: json['id'] ?? '',
      name: json['name'] ?? '',
      slug: json['slug'] ?? '',
      projectType: json['project_type'] ?? 'app',
      status: json['status'] ?? 'draft',
      screenshotUrl: json['screenshot_url'] ?? '',
      theme: json['theme'] is Map ? Map<String, dynamic>.from(json['theme']) : {},
      createdAt: DateTime.tryParse(json['created_at'] ?? '') ?? DateTime.now(),
    );
  }

  /// Extract primary color from theme for card accent
  Color? get primaryColor {
    try {
      final scheme = theme['color_scheme'] as Map<String, dynamic>?;
      if (scheme == null) return null;
      final primary = scheme['primary']?.toString();
      if (primary == null || !primary.startsWith('#')) return null;
      return Color(int.parse('FF${primary.substring(1)}', radix: 16));
    } catch (_) {
      return null;
    }
  }
}
```

Add `import 'package:flutter/material.dart';` at the top for `Color`.

- [ ] **Step 2: Commit**

```bash
git add lib/models/app_project.dart
git commit -m "feat: add AppProject model"
```

---

### Task 15: App Card Widget

**Files:**
- Create: `lib/widgets/app_card.dart`

- [ ] **Step 1: Create the app card widget**

```dart
import 'package:flutter/material.dart';
import 'package:cached_network_image/cached_network_image.dart';
import '../models/app_project.dart';

class AppCard extends StatelessWidget {
  final AppProject app;
  final VoidCallback onTap;

  const AppCard({super.key, required this.app, required this.onTap});

  @override
  Widget build(BuildContext context) {
    final accent = app.primaryColor ?? Colors.white24;

    return GestureDetector(
      onTap: onTap,
      child: Container(
        decoration: BoxDecoration(
          color: Colors.white.withValues(alpha: 0.06),
          borderRadius: BorderRadius.circular(16),
          border: Border.all(color: Colors.white.withValues(alpha: 0.08)),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            // Screenshot or gradient placeholder
            ClipRRect(
              borderRadius:
                  const BorderRadius.vertical(top: Radius.circular(16)),
              child: AspectRatio(
                aspectRatio: 16 / 10,
                child: app.screenshotUrl.isNotEmpty
                    ? CachedNetworkImage(
                        imageUrl: app.screenshotUrl,
                        fit: BoxFit.cover,
                        placeholder: (_, __) => Container(
                          decoration: BoxDecoration(
                            gradient: LinearGradient(
                              colors: [
                                accent.withValues(alpha: 0.3),
                                accent.withValues(alpha: 0.1),
                              ],
                              begin: Alignment.topLeft,
                              end: Alignment.bottomRight,
                            ),
                          ),
                        ),
                        errorWidget: (_, __, ___) =>
                            _gradientPlaceholder(accent),
                      )
                    : _gradientPlaceholder(accent),
              ),
            ),
            // Info
            Padding(
              padding: const EdgeInsets.all(14),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    app.name,
                    style: const TextStyle(
                      color: Colors.white,
                      fontSize: 15,
                      fontWeight: FontWeight.w600,
                    ),
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                  ),
                  const SizedBox(height: 4),
                  Row(
                    children: [
                      Container(
                        width: 6,
                        height: 6,
                        decoration: BoxDecoration(
                          color: app.status == 'deployed'
                              ? Colors.green
                              : Colors.orange,
                          shape: BoxShape.circle,
                        ),
                      ),
                      const SizedBox(width: 6),
                      Text(
                        app.status == 'deployed' ? 'Live' : app.status,
                        style: TextStyle(
                          color: Colors.white.withValues(alpha: 0.4),
                          fontSize: 12,
                        ),
                      ),
                    ],
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _gradientPlaceholder(Color accent) {
    return Container(
      decoration: BoxDecoration(
        gradient: LinearGradient(
          colors: [
            accent.withValues(alpha: 0.3),
            accent.withValues(alpha: 0.1),
          ],
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
        ),
      ),
      child: Center(
        child: Text(
          app.name.isNotEmpty ? app.name[0].toUpperCase() : 'W',
          style: TextStyle(
            color: Colors.white.withValues(alpha: 0.3),
            fontSize: 40,
            fontWeight: FontWeight.w800,
          ),
        ),
      ),
    );
  }
}
```

- [ ] **Step 2: Commit**

```bash
git add lib/widgets/app_card.dart
git commit -m "feat: add AppCard widget for app grid"
```

---

### Task 16: Home Screen

**Files:**
- Create: `lib/screens/home_screen.dart`

- [ ] **Step 1: Create the home screen**

```dart
import 'package:flutter/material.dart';
import '../config/api_config.dart';
import '../models/app_project.dart';
import '../models/user.dart';
import '../services/api_service.dart';
import '../services/auth_service.dart';
import '../widgets/app_card.dart';
import 'login_screen.dart';
import 'preview_screen.dart';
import 'scanner_screen.dart';

class HomeScreen extends StatefulWidget {
  const HomeScreen({super.key});

  @override
  State<HomeScreen> createState() => _HomeScreenState();
}

class _HomeScreenState extends State<HomeScreen> {
  List<AppProject> _apps = [];
  User? _user;
  bool _loading = true;
  String? _error;

  @override
  void initState() {
    super.initState();
    _loadData();
  }

  Future<void> _loadData() async {
    setState(() {
      _loading = true;
      _error = null;
    });

    try {
      final userData = await ApiService.get(ApiConfig.userMePath);
      // /v2/apps returns a JSON array directly — use getList
      final appsList =
          await ApiService.getList('${ApiConfig.appsPath}?type=mobile_app');

      setState(() {
        _user = User.fromJson(userData);
        _apps = appsList
            .map((a) => AppProject.fromJson(a as Map<String, dynamic>))
            .toList();
        _loading = false;
      });
    } catch (e) {
      setState(() {
        _error = e.toString();
        _loading = false;
      });
    }
  }

  Future<void> _openPreview(AppProject app) async {
    // Generate a preview code for this app
    try {
      final data = await ApiService.post(ApiConfig.previewCodesPath, {
        'app_project_id': app.id,
      });

      if (!mounted) return;

      // Redeem it immediately (we're the owner)
      final redeemData = await ApiService.post(
        '${ApiConfig.previewCodesPath}/${data['code']}/redeem',
        {},
      );

      if (!mounted) return;

      Navigator.of(context).push(MaterialPageRoute(
        builder: (_) => PreviewScreen(
          previewUrl: redeemData['preview_url'],
          appName: redeemData['app_name'] ?? app.name,
          sandboxId: redeemData['sandbox_id'],
        ),
      ));
    } on ApiException catch (e) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text(e.message),
          backgroundColor: Colors.redAccent,
        ),
      );
    }
  }

  void _openScanner() async {
    final result = await Navigator.of(context).push<Map<String, dynamic>>(
      MaterialPageRoute(builder: (_) => const ScannerScreen()),
    );

    if (result != null && mounted) {
      Navigator.of(context).push(MaterialPageRoute(
        builder: (_) => PreviewScreen(
          previewUrl: result['preview_url'],
          appName: result['app_name'] ?? 'Preview',
          sandboxId: result['sandbox_id'],
        ),
      ));
    }
  }

  Future<void> _logout() async {
    await AuthService.logout();
    if (!mounted) return;
    Navigator.of(context).pushReplacement(
      MaterialPageRoute(builder: (_) => const LoginScreen()),
    );
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: Colors.black,
      body: SafeArea(
        child: Column(
          children: [
            // Header
            Padding(
              padding: const EdgeInsets.fromLTRB(20, 16, 20, 0),
              child: Row(
                children: [
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          'My Apps',
                          style: const TextStyle(
                            color: Colors.white,
                            fontSize: 28,
                            fontWeight: FontWeight.w700,
                            letterSpacing: -0.5,
                          ),
                        ),
                        if (_user != null)
                          Text(
                            _user!.email,
                            style: TextStyle(
                              color: Colors.white.withValues(alpha: 0.4),
                              fontSize: 14,
                            ),
                          ),
                      ],
                    ),
                  ),
                  IconButton(
                    onPressed: _logout,
                    icon: Icon(Icons.logout_rounded,
                        color: Colors.white.withValues(alpha: 0.5)),
                  ),
                ],
              ),
            ),
            const SizedBox(height: 20),

            // Content
            Expanded(child: _buildContent()),
          ],
        ),
      ),
      floatingActionButton: FloatingActionButton(
        onPressed: _openScanner,
        backgroundColor: Colors.white,
        foregroundColor: Colors.black,
        child: const Icon(Icons.qr_code_scanner_rounded),
      ),
    );
  }

  Widget _buildContent() {
    if (_loading) {
      return const Center(
          child: CircularProgressIndicator(color: Colors.white));
    }

    if (_error != null) {
      return Center(
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            Text(_error!,
                style: const TextStyle(color: Colors.redAccent),
                textAlign: TextAlign.center),
            const SizedBox(height: 16),
            TextButton(
              onPressed: _loadData,
              child: const Text('Retry',
                  style: TextStyle(color: Colors.white)),
            ),
          ],
        ),
      );
    }

    if (_apps.isEmpty) {
      return Center(
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            Icon(Icons.phone_android_rounded,
                size: 64, color: Colors.white.withValues(alpha: 0.2)),
            const SizedBox(height: 16),
            Text('No mobile apps yet',
                style: TextStyle(
                    color: Colors.white.withValues(alpha: 0.5),
                    fontSize: 16)),
            const SizedBox(height: 8),
            Text('Create one at wemob.io',
                style: TextStyle(
                    color: Colors.white.withValues(alpha: 0.3),
                    fontSize: 14)),
          ],
        ),
      );
    }

    return RefreshIndicator(
      onRefresh: _loadData,
      color: Colors.white,
      backgroundColor: Colors.grey.shade900,
      child: GridView.builder(
        padding: const EdgeInsets.fromLTRB(16, 0, 16, 100),
        gridDelegate: const SliverGridDelegateWithFixedCrossAxisCount(
          crossAxisCount: 2,
          childAspectRatio: 0.78,
          crossAxisSpacing: 12,
          mainAxisSpacing: 12,
        ),
        itemCount: _apps.length,
        itemBuilder: (_, i) => AppCard(
          app: _apps[i],
          onTap: () => _openPreview(_apps[i]),
        ),
      ),
    );
  }
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/knid/Projects/try_with_wemob && flutter analyze lib/screens/home_screen.dart`
Expected: no issues (PreviewScreen and ScannerScreen references resolve in next tasks)

- [ ] **Step 3: Commit**

```bash
git add lib/models/app_project.dart lib/widgets/app_card.dart lib/screens/home_screen.dart
git commit -m "feat: add home screen with app grid and preview flow"
```

---

## Chunk 5: Flutter App — QR Scanner, WebView Preview, Main Entry

### File Structure

| Action | File | Responsibility |
|--------|------|---------------|
| Create | `lib/screens/scanner_screen.dart` | QR code scanner, redeem code, return result |
| Create | `lib/screens/preview_screen.dart` | Full-screen WebView with bridge SDK, back nav |
| Create | `lib/main.dart` | App entry, theme, auth check, initial route |

---

### Task 17: Scanner Screen

**Files:**
- Create: `lib/screens/scanner_screen.dart`

- [ ] **Step 1: Create the QR scanner screen**

```dart
import 'package:flutter/material.dart';
import 'package:mobile_scanner/mobile_scanner.dart';
import '../config/api_config.dart';
import '../services/api_service.dart';

class ScannerScreen extends StatefulWidget {
  const ScannerScreen({super.key});

  @override
  State<ScannerScreen> createState() => _ScannerScreenState();
}

class _ScannerScreenState extends State<ScannerScreen> {
  final MobileScannerController _scannerController = MobileScannerController();
  bool _processing = false;
  String? _error;

  Future<void> _onDetect(BarcodeCapture capture) async {
    if (_processing) return;

    final barcode = capture.barcodes.firstOrNull;
    if (barcode == null || barcode.rawValue == null) return;

    final value = barcode.rawValue!;

    // Expected format: wemob://preview/{code}
    final uri = Uri.tryParse(value);
    if (uri == null ||
        uri.scheme != 'wemob' ||
        uri.host != 'preview' ||
        uri.pathSegments.isEmpty) {
      setState(() => _error = 'Invalid QR code. Use a Wemob preview code.');
      return;
    }

    final code = uri.pathSegments.first;
    setState(() {
      _processing = true;
      _error = null;
    });

    try {
      final data = await ApiService.post(
        '${ApiConfig.previewCodesPath}/$code/redeem',
        {},
      );

      if (!mounted) return;
      Navigator.of(context).pop(data);
    } on ApiException catch (e) {
      setState(() {
        _error = e.message;
        _processing = false;
      });
    } catch (e) {
      setState(() {
        _error = 'Failed to redeem code. Try again.';
        _processing = false;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: Colors.black,
      appBar: AppBar(
        backgroundColor: Colors.transparent,
        foregroundColor: Colors.white,
        elevation: 0,
        title: const Text('Scan QR Code',
            style: TextStyle(fontSize: 18, fontWeight: FontWeight.w600)),
        centerTitle: true,
      ),
      body: Stack(
        children: [
          MobileScanner(
            controller: _scannerController,
            onDetect: _onDetect,
          ),
          // Overlay with cutout
          _buildOverlay(),
          // Status area
          if (_processing || _error != null)
            Positioned(
              bottom: 80,
              left: 32,
              right: 32,
              child: Container(
                padding:
                    const EdgeInsets.symmetric(horizontal: 20, vertical: 14),
                decoration: BoxDecoration(
                  color: _error != null
                      ? Colors.redAccent.withValues(alpha: 0.9)
                      : Colors.black.withValues(alpha: 0.8),
                  borderRadius: BorderRadius.circular(14),
                ),
                child: _processing
                    ? const Row(
                        mainAxisAlignment: MainAxisAlignment.center,
                        children: [
                          SizedBox(
                            width: 18,
                            height: 18,
                            child: CircularProgressIndicator(
                                strokeWidth: 2, color: Colors.white),
                          ),
                          SizedBox(width: 12),
                          Text('Opening preview...',
                              style: TextStyle(color: Colors.white)),
                        ],
                      )
                    : Text(
                        _error!,
                        style: const TextStyle(color: Colors.white),
                        textAlign: TextAlign.center,
                      ),
              ),
            ),
        ],
      ),
    );
  }

  Widget _buildOverlay() {
    return LayoutBuilder(builder: (context, constraints) {
      final scanArea = constraints.maxWidth * 0.65;
      final top = (constraints.maxHeight - scanArea) / 2;
      final left = (constraints.maxWidth - scanArea) / 2;

      return Stack(
        children: [
          // Dim everything except the scan area
          ColorFiltered(
            colorFilter: ColorFilter.mode(
                Colors.black.withValues(alpha: 0.6), BlendMode.srcOut),
            child: Stack(
              children: [
                Container(
                  decoration:
                      const BoxDecoration(backgroundBlendMode: BlendMode.dstOut),
                ),
                Positioned(
                  top: top,
                  left: left,
                  child: Container(
                    width: scanArea,
                    height: scanArea,
                    decoration: BoxDecoration(
                      color: Colors.white,
                      borderRadius: BorderRadius.circular(20),
                    ),
                  ),
                ),
              ],
            ),
          ),
          // Corner brackets
          Positioned(
            top: top,
            left: left,
            child: _cornerBracket(scanArea),
          ),
        ],
      );
    });
  }

  Widget _cornerBracket(double size) {
    return SizedBox(
      width: size,
      height: size,
      child: CustomPaint(painter: _CornerPainter()),
    );
  }

  @override
  void dispose() {
    _scannerController.dispose();
    super.dispose();
  }
}

class _CornerPainter extends CustomPainter {
  @override
  void paint(Canvas canvas, Size size) {
    final paint = Paint()
      ..color = Colors.white
      ..strokeWidth = 3
      ..style = PaintingStyle.stroke
      ..strokeCap = StrokeCap.round;

    const len = 30.0;
    const r = 20.0;

    // Top-left
    canvas.drawArc(Rect.fromLTWH(0, 0, r * 2, r * 2), 3.14, 1.57, false, paint);
    // Top-right
    canvas.drawArc(
        Rect.fromLTWH(size.width - r * 2, 0, r * 2, r * 2), -1.57, 1.57, false, paint);
    // Bottom-left
    canvas.drawArc(
        Rect.fromLTWH(0, size.height - r * 2, r * 2, r * 2), 1.57, 1.57, false, paint);
    // Bottom-right
    canvas.drawArc(
        Rect.fromLTWH(size.width - r * 2, size.height - r * 2, r * 2, r * 2),
        0, 1.57, false, paint);
  }

  @override
  bool shouldRepaint(covariant CustomPainter oldDelegate) => false;
}
```

- [ ] **Step 2: Commit**

```bash
git add lib/screens/scanner_screen.dart
git commit -m "feat: add QR scanner screen with code redemption"
```

---

### Task 18: Preview Screen (WebView)

**Files:**
- Create: `lib/screens/preview_screen.dart`

- [ ] **Step 1: Create the preview screen**

This is adapted from the worker template's `main.dart` but with a dynamic URL and simplified config.

```dart
import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_inappwebview/flutter_inappwebview.dart';
import 'package:http/http.dart' as http;
import '../config/api_config.dart';

class PreviewScreen extends StatefulWidget {
  final String previewUrl;
  final String appName;
  final String? sandboxId;

  const PreviewScreen({
    super.key,
    required this.previewUrl,
    required this.appName,
    this.sandboxId,
  });

  @override
  State<PreviewScreen> createState() => _PreviewScreenState();
}

class _PreviewScreenState extends State<PreviewScreen> {
  InAppWebViewController? _controller;
  bool _isLoading = true;
  bool _hasError = false;
  String? _errorMessage;
  double _progress = 0;
  late PullToRefreshController _pullToRefreshController;

  @override
  void initState() {
    super.initState();
    _pullToRefreshController = PullToRefreshController(
      settings: PullToRefreshSettings(color: Colors.white),
      onRefresh: () async => _controller?.reload(),
    );
  }

  void _injectBridgeSDK(InAppWebViewController controller) {
    controller.evaluateJavascript(source: '''
      if (!window.WemobBridge) {
        (function() {
          var isNative = function() { return !!(window.flutter_inappwebview && window.flutter_inappwebview.callHandler); };
          var call = function(method, args) {
            if (!isNative()) return Promise.resolve(null);
            return window.flutter_inappwebview.callHandler("WemobBridge", { method: method, args: args || {} });
          };
          window.WemobBridge = {
            isNative: isNative,
            call: call,
            getDeviceInfo: function() { return call("getDeviceInfo"); },
            share: function(text, url) { return call("share", { text: text, url: url }); },
            haptic: function(type) { return call("haptic", { type: type || "light" }); },
            openExternal: function(url) { return call("openExternal", { url: url }); },
            getConnectionStatus: function() { return call("getConnectionStatus"); },
            getTheme: function() { return call("getTheme"); },
            requestReview: function() { return call("requestReview"); },
            storage: {
              get: function(key) { return call("storage.get", { key: key }); },
              set: function(key, value) { return call("storage.set", { key: key, value: value }); },
              remove: function(key) { return call("storage.remove", { key: key }); }
            }
          };
        })();
      }
    ''');
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: Colors.black,
      appBar: AppBar(
        backgroundColor: Colors.black,
        foregroundColor: Colors.white,
        elevation: 0,
        title: Text(
          widget.appName,
          style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w600),
        ),
        centerTitle: true,
        leading: IconButton(
          icon: const Icon(Icons.close_rounded),
          onPressed: () => Navigator.of(context).pop(),
        ),
        actions: [
          IconButton(
            icon: const Icon(Icons.refresh_rounded, size: 20),
            onPressed: () => _controller?.reload(),
          ),
        ],
      ),
      body: Stack(
        children: [
          if (!_hasError)
            InAppWebView(
              initialUrlRequest:
                  URLRequest(url: WebUri(widget.previewUrl)),
              initialSettings: InAppWebViewSettings(
                javaScriptEnabled: true,
                allowsInlineMediaPlayback: true,
                mediaPlaybackRequiresUserGesture: false,
                allowsBackForwardNavigationGestures: true,
                useShouldOverrideUrlLoading: true,
                supportZoom: false,
                cacheEnabled: true,
                cacheMode: CacheMode.LOAD_DEFAULT,
                userAgent: 'WemobApp/1.0 TryWithWemob',
              ),
              pullToRefreshController: _pullToRefreshController,
              onWebViewCreated: (controller) => _controller = controller,
              onProgressChanged: (_, progress) {
                if (!mounted) return;
                setState(() => _progress = progress / 100);
              },
              onLoadStop: (controller, _) {
                if (!mounted) return;
                setState(() => _isLoading = false);
                _pullToRefreshController.endRefreshing();
                _injectBridgeSDK(controller);
              },
              onReceivedError: (_, request, error) {
                if (_isLoading) {
                  setState(() {
                    _isLoading = false;
                    _hasError = true;
                    _errorMessage = _describeError(error);
                  });
                  _pullToRefreshController.endRefreshing();
                }
              },
            ),
          // Progress bar
          if (_isLoading && _progress > 0 && _progress < 1.0)
            Positioned(
              top: 0,
              left: 0,
              right: 0,
              child: LinearProgressIndicator(
                value: _progress,
                minHeight: 2,
                backgroundColor: Colors.transparent,
                valueColor: const AlwaysStoppedAnimation(Colors.white),
              ),
            ),
          // Loading overlay
          if (_isLoading)
            const Center(
                child: CircularProgressIndicator(color: Colors.white)),
          // Error state
          if (_hasError)
            Center(
              child: Column(
                mainAxisAlignment: MainAxisAlignment.center,
                children: [
                  Icon(Icons.wifi_off_rounded,
                      size: 48,
                      color: Colors.white.withValues(alpha: 0.3)),
                  const SizedBox(height: 16),
                  Text(
                    _errorMessage ?? 'Unable to load preview',
                    textAlign: TextAlign.center,
                    style: TextStyle(
                        color: Colors.white.withValues(alpha: 0.6)),
                  ),
                  const SizedBox(height: 20),
                  TextButton(
                    onPressed: () {
                      setState(() {
                        _hasError = false;
                        _isLoading = true;
                        _progress = 0;
                      });
                      _controller?.reload();
                    },
                    child: const Text('Retry',
                        style: TextStyle(color: Colors.white)),
                  ),
                ],
              ),
            ),
        ],
      ),
    );
  }

  String _describeError(WebResourceError error) {
    final type = error.type;
    if (type == WebResourceErrorType.HOST_LOOKUP) {
      return 'Could not find the server.\nCheck your internet connection.';
    } else if (type == WebResourceErrorType.TIMEOUT) {
      return 'Connection timed out.\nPlease try again.';
    } else if (type == WebResourceErrorType.NOT_CONNECTED_TO_INTERNET ||
        type == WebResourceErrorType.NETWORK_CONNECTION_LOST) {
      return 'No internet connection.\nCheck your network settings.';
    }
    return 'Unable to load preview.\nPlease try again.';
  }
}
```

- [ ] **Step 2: Commit**

```bash
git add lib/screens/preview_screen.dart
git commit -m "feat: add preview screen with WebView and bridge SDK"
```

---

### Task 19: App Entry Point (main.dart)

**Files:**
- Create: `lib/main.dart`

- [ ] **Step 1: Create the app entry point**

```dart
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'screens/login_screen.dart';
import 'screens/home_screen.dart';
import 'services/auth_service.dart';

void main() {
  WidgetsFlutterBinding.ensureInitialized();
  SystemChrome.setPreferredOrientations([DeviceOrientation.portraitUp]);
  SystemChrome.setSystemUIOverlayStyle(
    const SystemUiOverlayStyle(
      statusBarColor: Colors.transparent,
      statusBarIconBrightness: Brightness.light,
      systemNavigationBarColor: Colors.black,
    ),
  );
  runApp(const TryWithWemobApp());
}

class TryWithWemobApp extends StatelessWidget {
  const TryWithWemobApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'Try with Wemob',
      debugShowCheckedModeBanner: false,
      theme: ThemeData(
        brightness: Brightness.dark,
        scaffoldBackgroundColor: Colors.black,
        colorScheme: const ColorScheme.dark(
          primary: Colors.white,
          surface: Colors.black,
        ),
        fontFamily: 'SF Pro Display',
      ),
      home: const AuthGate(),
    );
  }
}

class AuthGate extends StatefulWidget {
  const AuthGate({super.key});

  @override
  State<AuthGate> createState() => _AuthGateState();
}

class _AuthGateState extends State<AuthGate> {
  bool _checking = true;
  bool _loggedIn = false;

  @override
  void initState() {
    super.initState();
    _checkAuth();
  }

  Future<void> _checkAuth() async {
    final loggedIn = await AuthService.isLoggedIn();
    if (!mounted) return;
    setState(() {
      _loggedIn = loggedIn;
      _checking = false;
    });
  }

  @override
  Widget build(BuildContext context) {
    if (_checking) {
      return const Scaffold(
        backgroundColor: Colors.black,
        body: Center(
            child: CircularProgressIndicator(color: Colors.white)),
      );
    }
    return _loggedIn ? const HomeScreen() : const LoginScreen();
  }
}
```

- [ ] **Step 2: Verify the full app compiles**

Run: `cd /Users/knid/Projects/try_with_wemob && flutter analyze`
Expected: no issues

- [ ] **Step 3: Commit**

```bash
git add lib/main.dart
git commit -m "feat: add app entry point with auth gate and dark theme"
```

---

### Task 20: Deep Link Handling

**Files:**
- Modify: `lib/main.dart`
- Modify: Platform-specific configs

- [ ] **Step 1: Add deep link handling to `main.dart`**

Add `app_links` to `pubspec.yaml` dependencies:

```yaml
  app_links: ^6.3.3
```

Then add to `_AuthGateState`:

```dart
import 'dart:async';
import 'package:app_links/app_links.dart';

// Add to _AuthGateState class:
late AppLinks _appLinks;
StreamSubscription<Uri>? _deepLinkSub;

@override
void initState() {
  super.initState();
  _appLinks = AppLinks();
  _deepLinkSub = _appLinks.uriLinkStream.listen(_handleDeepLink);
  _checkAuth();
}

@override
void dispose() {
  _deepLinkSub?.cancel();
  super.dispose();
}

void _handleDeepLink(Uri uri) {
  // wemob://preview/{code}
  if (uri.scheme == 'wemob' && uri.host == 'preview' && uri.pathSegments.isNotEmpty) {
    final code = uri.pathSegments.first;
    _redeemAndPreview(code);
  }
}

Future<void> _redeemAndPreview(String code) async {
  if (!_loggedIn) return; // Must be logged in

  try {
    final data = await ApiService.post(
      '${ApiConfig.previewCodesPath}/$code/redeem',
      {},
    );
    if (!mounted) return;
    Navigator.of(context).push(MaterialPageRoute(
      builder: (_) => PreviewScreen(
        previewUrl: data['preview_url'],
        appName: data['app_name'] ?? 'Preview',
        sandboxId: data['sandbox_id'],
      ),
    ));
  } catch (e) {
    // Show error snackbar
    if (mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(e.toString()), backgroundColor: Colors.redAccent),
      );
    }
  }
}
```

- [ ] **Step 2: Configure iOS URL scheme**

Add to `ios/Runner/Info.plist` inside the top-level `<dict>`:

```xml
<key>CFBundleURLTypes</key>
<array>
  <dict>
    <key>CFBundleURLSchemes</key>
    <array>
      <string>wemob</string>
    </array>
  </dict>
</array>
```

- [ ] **Step 3: Configure Android URL scheme**

Add to `android/app/src/main/AndroidManifest.xml` inside the `<activity>` tag:

```xml
<intent-filter>
  <action android:name="android.intent.action.VIEW" />
  <category android:name="android.intent.category.DEFAULT" />
  <category android:name="android.intent.category.BROWSABLE" />
  <data android:scheme="wemob" android:host="preview" />
</intent-filter>
```

- [ ] **Step 4: Run flutter pub get and verify**

Run: `cd /Users/knid/Projects/try_with_wemob && flutter pub get && flutter analyze`
Expected: no issues

- [ ] **Step 5: Commit**

```bash
git add .
git commit -m "feat: add deep link handling for wemob://preview/{code}"
```

---

### Task 21: iOS & Android Platform Configuration

- [ ] **Step 1: iOS — Add camera permission for QR scanner**

Add to `ios/Runner/Info.plist`:

```xml
<key>NSCameraUsageDescription</key>
<string>Camera access is needed to scan QR codes for app preview</string>
```

- [ ] **Step 2: Android — Set minSdkVersion**

In `android/app/build.gradle`, set:

```
minSdkVersion 21
```

- [ ] **Step 3: Set app display name**

In `ios/Runner/Info.plist`, update:
```xml
<key>CFBundleDisplayName</key>
<string>Try with Wemob</string>
```

In `android/app/src/main/AndroidManifest.xml`, update:
```xml
android:label="Try with Wemob"
```

- [ ] **Step 4: Verify iOS build**

Run: `cd /Users/knid/Projects/try_with_wemob && flutter build ios --no-codesign`
Expected: build succeeds

- [ ] **Step 5: Verify Android build**

Run: `cd /Users/knid/Projects/try_with_wemob && flutter build apk --debug`
Expected: build succeeds

- [ ] **Step 6: Commit**

```bash
git add .
git commit -m "chore: configure iOS/Android platform settings and permissions"
```

---

## Implementation Summary

| Chunk | Tasks | Repository | Description |
|-------|-------|-----------|-------------|
| 1 | 1-6 | wemob-backend | PreviewCode model, DB, controller, views, routes, cleanup |
| 2 | 7 | wemob-dashboard | Update QR dialog to use one-time codes |
| 3 | 8-13 | try_with_wemob + wemob-backend | Flutter project setup, auth flow, Google mobile endpoint |
| 4 | 14-16 | try_with_wemob | Home screen, app list, app card widget |
| 5 | 17-21 | try_with_wemob | QR scanner, WebView preview, main entry, deep links, platform config |

### Dependencies Between Chunks

```
Chunk 1 (Backend: preview codes) ──┬── Chunk 2 (Dashboard: QR update)
                                   └── Chunk 3 (Flutter: auth) ── Chunk 4 (Flutter: home) ── Chunk 5 (Flutter: preview)
```

Chunks 1 is prerequisite for everything. Chunks 2 and 3 can run in parallel. Chunks 4-5 are sequential after 3.

### Open Items (not in scope but needed before App Store submission)

- Wemob logo/icon assets for the Flutter app (currently uses text placeholder "W")
- Google Sign-In OAuth client IDs (iOS + Android) — requires Google Cloud Console setup
- App Store / Google Play listing (screenshots, description, etc.)
- `flutter_launcher_icons` configuration for custom app icon
- CI/CD pipeline (Fastlane or Codemagic) for automated builds
