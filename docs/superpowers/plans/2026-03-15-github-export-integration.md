# GitHub Export Integration Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow paid users to connect their GitHub account and push generated web app code to a new GitHub repo they own, preserving full git history.

**Architecture:** GitHub OAuth App for auth, `git push` with transient URL for export (no persistent remotes), `AppGitHubExport` model to track exports. One-way, on-demand push only.

**Tech Stack:** Go, chi/v5, Bun ORM, PostgreSQL, `os/exec` git CLI, GitHub REST API v3

**Spec:** `docs/superpowers/specs/2026-03-15-github-export-integration-design.md`

---

## File Structure

| File | Action | Responsibility |
|------|--------|---------------|
| `internal/utils/encryption.go` | Create | AES-256-GCM encrypt/decrypt for GitHub tokens |
| `internal/utils/github_oauth.go` | Create | HMAC-signed state generation/verification, GitHub API helpers (exchange code, fetch user, create repo) |
| `internal/models/app_github_export.go` | Create | `AppGitHubExport` Bun ORM model |
| `internal/requests/github_requests.go` | Create | `ExportToGitHubRequest` with validation tags |
| `internal/responses/github_responses.go` | Create | Response types for GitHub endpoints |
| `internal/database/app_github_export.go` | Create | CRUD methods for `AppGitHubExport` |
| `internal/controllers/github_controller.go` | Create | Business logic for OAuth, export, push, disconnect |
| `internal/views/github_views.go` | Create | HTTP handlers for all GitHub endpoints |
| `internal/services/gitrepo/repo.go` | Modify | Add `PushToURL()` and `CurrentBranch()` methods |
| `internal/models/users.go` | Modify | Add `GitHubAccessToken`, `GitHubUsername` fields |
| `internal/models/limits.go` | Modify | Add `CanExportToGitHub` field |
| `cmd/main.go` | Modify | Add routes, `createSchema` table + ALTER, fetch new secrets |

---

## Chunk 1: Foundation (Encryption + Git Methods)

### Task 1: Token Encryption Utilities

**Files:**
- Create: `internal/utils/encryption.go`

- [ ] **Step 1: Create encryption utility file**

```go
package utils

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
)

// EncryptToken encrypts plaintext using AES-256-GCM.
// key must be a 64-char hex string (32 bytes decoded).
func EncryptToken(plaintext, hexKey string) (string, error) {
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return "", fmt.Errorf("encryption: invalid hex key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("encryption: %w", err)
	}
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("encryption: %w", err)
	}
	nonce := make([]byte, aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("encryption: %w", err)
	}
	ciphertext := aesGCM.Seal(nonce, nonce, []byte(plaintext), nil)
	return hex.EncodeToString(ciphertext), nil
}

// DecryptToken decrypts a hex-encoded AES-256-GCM ciphertext.
func DecryptToken(hexCiphertext, hexKey string) (string, error) {
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return "", fmt.Errorf("decryption: invalid hex key: %w", err)
	}
	ciphertext, err := hex.DecodeString(hexCiphertext)
	if err != nil {
		return "", fmt.Errorf("decryption: invalid hex ciphertext: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("decryption: %w", err)
	}
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("decryption: %w", err)
	}
	nonceSize := aesGCM.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", fmt.Errorf("decryption: ciphertext too short")
	}
	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := aesGCM.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decryption: %w", err)
	}
	return string(plaintext), nil
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/knid/Projects/wemob-backend && go build ./internal/utils/`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add internal/utils/encryption.go
git commit -m "feat: add AES-256-GCM token encryption utilities"
```

---

### Task 2: Git Repo Push and Branch Methods

**Files:**
- Modify: `internal/services/gitrepo/repo.go` (add after existing methods)

- [ ] **Step 1: Add `CurrentBranch` method to repo.go**

Add at the end of the file:

```go
// CurrentBranch returns the name of the current checked-out branch.
func (r *Repo) CurrentBranch() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = r.Path
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("gitrepo: current branch: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// PushToURL pushes the current branch to a remote URL. The URL is not persisted
// as a named remote — this prevents tokens from being stored in .git/config.
// Acquires the per-project lock to prevent concurrent pushes.
func (r *Repo) PushToURL(url, branch string) error {
	unlock := r.service.lock(r.ProjectID)
	defer unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "push", url, branch)
	cmd.Dir = r.Path
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("gitrepo: push: %s: %s", err, out)
	}
	return nil
}
```

- [ ] **Step 2: Add `strings` import if not already present**

Check imports at top of `repo.go`. Add `"strings"` if missing. Also ensure `"time"` is imported.

- [ ] **Step 3: Verify it compiles**

Run: `cd /Users/knid/Projects/wemob-backend && go build ./internal/services/gitrepo/`
Expected: No errors

- [ ] **Step 4: Commit**

```bash
git add internal/services/gitrepo/repo.go
git commit -m "feat: add PushToURL and CurrentBranch to gitrepo.Repo"
```

---

## Chunk 2: Model + DB + Request/Response Types

### Task 3: User Model — Add GitHub Fields

**Files:**
- Modify: `internal/models/users.go:12-34` (User struct)

- [ ] **Step 1: Add GitHub fields to User struct**

Add before `CreatedAt` (line 31):

```go
GitHubAccessToken string `bun:",nullzero" json:"-"`
GitHubUsername    string `bun:",nullzero" json:"github_username"`
```

- [ ] **Step 2: Add ALTER statements to `createSchema()` in `cmd/main.go`**

Find the ALTER TABLE section for `users` and add:

```go
`ALTER TABLE users ADD COLUMN IF NOT EXISTS github_access_token text`,
`ALTER TABLE users ADD COLUMN IF NOT EXISTS github_username text`,
```

- [ ] **Step 3: Verify it compiles**

Run: `cd /Users/knid/Projects/wemob-backend && go build ./...`
Expected: No errors

- [ ] **Step 4: Commit**

```bash
git add internal/models/users.go cmd/main.go
git commit -m "feat: add GitHub fields to User model"
```

---

### Task 4: Limits Model — Add CanExportToGitHub

**Files:**
- Modify: `internal/models/limits.go:9-36` (StandartUserLimits struct)

- [ ] **Step 1: Add CanExportToGitHub field**

Add after `CanBuyDomain` (line 30):

```go
CanExportToGitHub bool `bun:",notnull,default:false"`
```

- [ ] **Step 2: Add ALTER statement to `createSchema()` in `cmd/main.go`**

```go
`ALTER TABLE standart_user_limits ADD COLUMN IF NOT EXISTS can_export_to_github boolean NOT NULL DEFAULT false`,
```

- [ ] **Step 3: Verify it compiles**

Run: `cd /Users/knid/Projects/wemob-backend && go build ./...`
Expected: No errors

- [ ] **Step 4: Commit**

```bash
git add internal/models/limits.go cmd/main.go
git commit -m "feat: add CanExportToGitHub to plan limits"
```

---

### Task 5: AppGitHubExport Model

**Files:**
- Create: `internal/models/app_github_export.go`

- [ ] **Step 1: Create the model file**

```go
package models

import (
	"time"

	"github.com/google/uuid"
)

type AppGitHubExport struct {
	ID           uuid.UUID   `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	AppProjectID uuid.UUID   `bun:",notnull,type:uuid,unique"`
	AppProject   *AppProject `bun:"rel:belongs-to,join:app_project_id=id"`
	UserID       uuid.UUID   `bun:",notnull,type:uuid"`
	RepoFullName string      `bun:",notnull"`
	RepoURL      string      `bun:",notnull"`
	Private      bool        `bun:",notnull"`
	LastPushedAt time.Time   `bun:",nullzero"`
	CreatedAt    time.Time   `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt    time.Time   `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}
```

- [ ] **Step 2: Add to `createSchema()` in `cmd/main.go`**

Add with other model registrations:

```go
(*models.AppGitHubExport)(nil),
```

And add index:

```go
`CREATE INDEX IF NOT EXISTS idx_app_github_exports_app_project_id ON app_github_exports(app_project_id)`,
```

- [ ] **Step 3: Verify it compiles**

Run: `cd /Users/knid/Projects/wemob-backend && go build ./...`
Expected: No errors

- [ ] **Step 4: Commit**

```bash
git add internal/models/app_github_export.go cmd/main.go
git commit -m "feat: add AppGitHubExport model"
```

---

### Task 6: Database Methods for AppGitHubExport

**Files:**
- Create: `internal/database/app_github_export.go`

- [ ] **Step 1: Create the DB methods file**

Follow the pattern from `internal/database/vercel_projects.go`:

```go
package database

import (
	"context"
	"time"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) CreateAppGitHubExport(export *models.AppGitHubExport) (*models.AppGitHubExport, error) {
	_, err := db.DB.NewInsert().Model(export).Exec(context.Background())
	return export, err
}

func (db *Database) GetAppGitHubExportByProjectID(projectID string) (*models.AppGitHubExport, error) {
	var export models.AppGitHubExport
	err := db.DB.NewSelect().Model(&export).Where("app_project_id = ?", projectID).Scan(context.Background())
	return &export, err
}

func (db *Database) DeleteAppGitHubExport(projectID string) error {
	_, err := db.DB.NewDelete().Model((*models.AppGitHubExport)(nil)).Where("app_project_id = ?", projectID).Exec(context.Background())
	return err
}

func (db *Database) UpdateAppGitHubExportLastPushed(projectID string) error {
	_, err := db.DB.NewUpdate().
		Model((*models.AppGitHubExport)(nil)).
		Set("last_pushed_at = ?", time.Now()).
		Set("updated_at = ?", time.Now()).
		Where("app_project_id = ?", projectID).
		Exec(context.Background())
	return err
}

func (db *Database) GetAppGitHubExportsByUserID(userID string) ([]models.AppGitHubExport, error) {
	var exports []models.AppGitHubExport
	err := db.DB.NewSelect().Model(&exports).Where("user_id = ?", userID).Scan(context.Background())
	return exports, err
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/knid/Projects/wemob-backend && go build ./internal/database/`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add internal/database/app_github_export.go
git commit -m "feat: add database methods for AppGitHubExport"
```

---

### Task 7: Request and Response Types

**Files:**
- Create: `internal/requests/github_requests.go`
- Create: `internal/responses/github_responses.go`

- [ ] **Step 1: Create request types**

```go
package requests

type ExportToGitHubRequest struct {
	RepoName    string `json:"repo_name" validate:"required,min=1,max=100" errormgs:"repo name is required and must be 1-100 characters"`
	Description string `json:"description" validate:"max=350" errormgs:"description must be at most 350 characters"`
	Private     bool   `json:"private"`
}
```

- [ ] **Step 2: Create response types**

```go
package responses

import "time"

type GitHubExportResponse struct {
	ID           string     `json:"id"`
	RepoFullName string     `json:"repo_full_name"`
	RepoURL      string     `json:"repo_url"`
	Private      bool       `json:"private"`
	LastPushedAt *time.Time `json:"last_pushed_at"`
	CreatedAt    time.Time  `json:"created_at"`
}

type GitHubConnectionResponse struct {
	Connected bool   `json:"connected"`
	Username  string `json:"username,omitempty"`
}
```

- [ ] **Step 3: Verify it compiles**

Run: `cd /Users/knid/Projects/wemob-backend && go build ./internal/requests/ ./internal/responses/`
Expected: No errors

- [ ] **Step 4: Commit**

```bash
git add internal/requests/github_requests.go internal/responses/github_responses.go
git commit -m "feat: add GitHub request and response types"
```

---

## Chunk 3: GitHub OAuth Helpers + Controller

### Task 8: GitHub OAuth Utilities

**Files:**
- Create: `internal/utils/github_oauth.go`

- [ ] **Step 1: Create GitHub OAuth helper file**

```go
package utils

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// OAuthState is the payload embedded in the HMAC-signed state param.
type OAuthState struct {
	UserID  string `json:"u"`
	Expires int64  `json:"e"`
}

// GenerateOAuthState creates an HMAC-signed, base64-encoded state string.
func GenerateOAuthState(userID, secret string) string {
	state := OAuthState{
		UserID:  userID,
		Expires: time.Now().Add(10 * time.Minute).Unix(),
	}
	payload, _ := json.Marshal(state)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	sig := mac.Sum(nil)
	// payload + "." + signature
	return base64.URLEncoding.EncodeToString(payload) + "." + base64.URLEncoding.EncodeToString(sig)
}

// VerifyOAuthState verifies the HMAC signature and expiry. Returns the user ID.
func VerifyOAuthState(stateStr, secret string) (string, error) {
	parts := strings.SplitN(stateStr, ".", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid state format")
	}
	payload, err := base64.URLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", fmt.Errorf("invalid state encoding")
	}
	sig, err := base64.URLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("invalid signature encoding")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return "", fmt.Errorf("invalid state signature")
	}
	var state OAuthState
	if err := json.Unmarshal(payload, &state); err != nil {
		return "", fmt.Errorf("invalid state payload")
	}
	if time.Now().Unix() > state.Expires {
		return "", fmt.Errorf("state expired")
	}
	return state.UserID, nil
}

// GitHubTokenResponse is the response from GitHub's OAuth token exchange.
type GitHubTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
}

// ExchangeGitHubCode exchanges an authorization code for an access token.
func ExchangeGitHubCode(clientID, clientSecret, code, redirectURI string) (*GitHubTokenResponse, error) {
	data := url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"code":          {code},
		"redirect_uri":  {redirectURI},
	}
	req, _ := http.NewRequest("POST", "https://github.com/login/oauth/access_token", strings.NewReader(data.Encode()))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github token exchange: %w", err)
	}
	defer resp.Body.Close()

	var tokenResp GitHubTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("github token decode: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("github token exchange failed: no access_token in response")
	}
	return &tokenResp, nil
}

// GitHubUser represents the relevant fields from GET /user.
type GitHubUser struct {
	Login string `json:"login"`
	ID    int64  `json:"id"`
}

// FetchGitHubUser fetches the authenticated user's profile.
func FetchGitHubUser(accessToken string) (*GitHubUser, error) {
	req, _ := http.NewRequest("GET", "https://api.github.com/user", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github fetch user: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github fetch user: status %d: %s", resp.StatusCode, body)
	}

	var user GitHubUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("github decode user: %w", err)
	}
	return &user, nil
}

// GitHubCreateRepoRequest is the request body for creating a GitHub repo.
type GitHubCreateRepoRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Private     bool   `json:"private"`
	AutoInit    bool   `json:"auto_init"`
}

// GitHubRepoResponse represents the relevant fields from the create repo response.
type GitHubRepoResponse struct {
	FullName string `json:"full_name"`
	HTMLURL  string `json:"html_url"`
	CloneURL string `json:"clone_url"`
}

// CreateGitHubRepo creates a new repository for the authenticated user.
func CreateGitHubRepo(accessToken, name, description string, private bool) (*GitHubRepoResponse, error) {
	body := GitHubCreateRepoRequest{
		Name:        name,
		Description: description,
		Private:     private,
		AutoInit:    false, // we push our own history
	}
	jsonBody, _ := json.Marshal(body)

	req, _ := http.NewRequest("POST", "https://api.github.com/user/repos", strings.NewReader(string(jsonBody)))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github create repo: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == 422 {
		return nil, fmt.Errorf("GITHUB_REPO_EXISTS")
	}
	if resp.StatusCode != 201 {
		return nil, fmt.Errorf("github create repo: status %d: %s", resp.StatusCode, respBody)
	}

	var repoResp GitHubRepoResponse
	if err := json.Unmarshal(respBody, &repoResp); err != nil {
		return nil, fmt.Errorf("github decode repo: %w", err)
	}
	return &repoResp, nil
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/knid/Projects/wemob-backend && go build ./internal/utils/`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add internal/utils/github_oauth.go
git commit -m "feat: add GitHub OAuth helpers (state, token exchange, API calls)"
```

---

### Task 9: Controller — GitHub Methods

**Files:**
- Create: `internal/controllers/github_controller.go`

- [ ] **Step 1: Create the controller file**

```go
package controllers

import (
	"fmt"
	"log/slog"

	"strings"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
)

// ConnectGitHub exchanges an OAuth code for a token and saves it on the user.
func (c *Controller) ConnectGitHub(user *models.User, code string) error {
	clientID := c.SecretManager.GetSecret("GITHUB_CLIENT_ID")
	clientSecret := c.SecretManager.GetSecret("GITHUB_CLIENT_SECRET")
	redirectURI := c.SecretManager.GetSecret("GITHUB_REDIRECT_URI")
	encryptionKey := c.SecretManager.GetSecret("GITHUB_TOKEN_ENCRYPTION_KEY")

	tokenResp, err := utils.ExchangeGitHubCode(clientID, clientSecret, code, redirectURI)
	if err != nil {
		return fmt.Errorf("failed to exchange GitHub code: %w", err)
	}

	ghUser, err := utils.FetchGitHubUser(tokenResp.AccessToken)
	if err != nil {
		return fmt.Errorf("failed to fetch GitHub user: %w", err)
	}

	encrypted, err := utils.EncryptToken(tokenResp.AccessToken, encryptionKey)
	if err != nil {
		return fmt.Errorf("failed to encrypt GitHub token: %w", err)
	}

	user.GitHubAccessToken = encrypted
	user.GitHubUsername = ghUser.Login

	if err := c.DB.UpdateUserGitHubFields(user); err != nil {
		return fmt.Errorf("failed to save GitHub credentials: %w", err)
	}

	slog.Info("GitHub connected", "user", user.Email, "github_username", ghUser.Login)
	return nil
}

// DisconnectGitHub removes the user's GitHub token and clears all export records.
func (c *Controller) DisconnectGitHub(user *models.User) error {
	user.GitHubAccessToken = ""
	user.GitHubUsername = ""
	if err := c.DB.UpdateUserGitHubFields(user); err != nil {
		return fmt.Errorf("failed to clear GitHub credentials: %w", err)
	}

	// Clean up all export records for this user
	exports, err := c.DB.GetAppGitHubExportsByUserID(user.ID.String())
	if err == nil {
		for _, export := range exports {
			c.DB.DeleteAppGitHubExport(export.AppProjectID.String())
		}
	}

	slog.Info("GitHub disconnected", "user", user.Email)
	return nil
}

// decryptGitHubToken is a helper to decrypt the user's stored GitHub token.
func (c *Controller) decryptGitHubToken(user *models.User) (string, error) {
	if user.GitHubAccessToken == "" {
		return "", fmt.Errorf("GitHub not connected")
	}
	encryptionKey := c.SecretManager.GetSecret("GITHUB_TOKEN_ENCRYPTION_KEY")
	return utils.DecryptToken(user.GitHubAccessToken, encryptionKey)
}

// ExportToGitHub creates a new GitHub repo and pushes the project's full git history.
func (c *Controller) ExportToGitHub(user *models.User, projectID uuid.UUID, req requests.ExportToGitHubRequest) (*models.AppGitHubExport, error) {
	token, err := c.decryptGitHubToken(user)
	if err != nil {
		return nil, err
	}

	// Check if already exported
	existing, _ := c.DB.GetAppGitHubExportByProjectID(projectID.String())
	if existing != nil && existing.ID != uuid.Nil {
		return nil, fmt.Errorf("project already exported to GitHub as %s — disconnect first to re-export", existing.RepoFullName)
	}

	// Create the repo on GitHub
	repoResp, err := utils.CreateGitHubRepo(token, req.RepoName, req.Description, req.Private)
	if err != nil {
		if err.Error() == "GITHUB_REPO_EXISTS" {
			return nil, fmt.Errorf("GITHUB_REPO_EXISTS")
		}
		return nil, fmt.Errorf("failed to create GitHub repo: %w", err)
	}

	// Open the local git repo
	repo, err := c.GitRepoService.Open(projectID.String())
	if err != nil {
		return nil, fmt.Errorf("failed to open project repo: %w", err)
	}

	// Detect current branch
	branch, err := repo.CurrentBranch()
	if err != nil {
		branch = "main" // fallback
	}

	// Push with transient URL (token not persisted to disk)
	pushURL := fmt.Sprintf("https://x-access-token:%s@github.com/%s.git", token, repoResp.FullName)
	if err := repo.PushToURL(pushURL, branch); err != nil {
		return nil, fmt.Errorf("failed to push to GitHub: %w", err)
	}

	// Save export record
	export := &models.AppGitHubExport{
		AppProjectID: projectID,
		UserID:       user.ID,
		RepoFullName: repoResp.FullName,
		RepoURL:      repoResp.HTMLURL,
		Private:      req.Private,
	}
	if _, err := c.DB.CreateAppGitHubExport(export); err != nil {
		return nil, fmt.Errorf("failed to save export record: %w", err)
	}

	// Update last pushed timestamp
	c.DB.UpdateAppGitHubExportLastPushed(projectID.String())

	slog.Info("GitHub export completed", "user", user.Email, "repo", repoResp.FullName)
	return export, nil
}

// PushToGitHub pushes the latest changes to an already-exported GitHub repo.
func (c *Controller) PushToGitHub(user *models.User, projectID uuid.UUID) (*models.AppGitHubExport, error) {
	token, err := c.decryptGitHubToken(user)
	if err != nil {
		return nil, err
	}

	export, err := c.DB.GetAppGitHubExportByProjectID(projectID.String())
	if err != nil {
		return nil, fmt.Errorf("project not exported to GitHub — export first")
	}
	if export.UserID != user.ID {
		return nil, fmt.Errorf("not authorized")
	}

	repo, err := c.GitRepoService.Open(projectID.String())
	if err != nil {
		return nil, fmt.Errorf("failed to open project repo: %w", err)
	}

	branch, err := repo.CurrentBranch()
	if err != nil {
		branch = "main"
	}

	pushURL := fmt.Sprintf("https://x-access-token:%s@github.com/%s.git", token, export.RepoFullName)
	if err := repo.PushToURL(pushURL, branch); err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "could not read Username") || strings.Contains(errMsg, "401") || strings.Contains(errMsg, "403") {
			return nil, fmt.Errorf("GITHUB_AUTH_EXPIRED")
		}
		if strings.Contains(errMsg, "not found") || strings.Contains(errMsg, "404") {
			// Repo was deleted on GitHub — clean up
			c.DB.DeleteAppGitHubExport(projectID.String())
			return nil, fmt.Errorf("GITHUB_REPO_NOT_FOUND")
		}
		if strings.Contains(errMsg, "non-fast-forward") || strings.Contains(errMsg, "rejected") {
			return nil, fmt.Errorf("GITHUB_PUSH_REJECTED")
		}
		if strings.Contains(errMsg, "signal: killed") || strings.Contains(errMsg, "context deadline exceeded") {
			return nil, fmt.Errorf("GITHUB_PUSH_TIMEOUT")
		}
		return nil, fmt.Errorf("failed to push to GitHub: %w", err)
	}

	c.DB.UpdateAppGitHubExportLastPushed(projectID.String())

	// Refresh the export record to return updated LastPushedAt
	export, _ = c.DB.GetAppGitHubExportByProjectID(projectID.String())
	return export, nil
}

// RemoveGitHubExport deletes the export record for a project.
func (c *Controller) RemoveGitHubExport(user *models.User, projectID uuid.UUID) error {
	export, err := c.DB.GetAppGitHubExportByProjectID(projectID.String())
	if err != nil {
		return fmt.Errorf("no GitHub export found for this project")
	}
	if export.UserID != user.ID {
		return fmt.Errorf("not authorized")
	}
	return c.DB.DeleteAppGitHubExport(projectID.String())
}

// GetGitHubExport returns the export record for a project.
func (c *Controller) GetGitHubExport(projectID uuid.UUID) (*models.AppGitHubExport, error) {
	return c.DB.GetAppGitHubExportByProjectID(projectID.String())
}

```

- [ ] **Step 2: Add `UpdateUserGitHubFields` DB method**

Add to the appropriate user database file (find existing user DB methods):

```go
func (db *Database) UpdateUserGitHubFields(user *models.User) error {
	user.UpdatedAt = time.Now()
	_, err := db.DB.NewUpdate().Model(user).
		Column("github_access_token", "github_username", "updated_at").
		Where("id = ?", user.ID).
		Exec(context.Background())
	return err
}
```

- [ ] **Step 3: Verify it compiles**

Run: `cd /Users/knid/Projects/wemob-backend && go build ./...`
Expected: No errors

- [ ] **Step 4: Commit**

```bash
git add internal/controllers/github_controller.go internal/database/
git commit -m "feat: add GitHub controller methods (connect, disconnect, export, push)"
```

---

## Chunk 4: View Handlers + Routes

### Task 10: View Handlers

**Files:**
- Create: `internal/views/github_views.go`

- [ ] **Step 1: Create the views file**

```go
package views

import (
	"fmt"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

// toGitHubExportResponse converts a model to a response struct.
func toGitHubExportResponse(export *models.AppGitHubExport) responses.GitHubExportResponse {
	resp := responses.GitHubExportResponse{
		ID:           export.ID.String(),
		RepoFullName: export.RepoFullName,
		RepoURL:      export.RepoURL,
		Private:      export.Private,
		CreatedAt:    export.CreatedAt,
	}
	if !export.LastPushedAt.IsZero() {
		resp.LastPushedAt = &export.LastPushedAt
	}
	return resp
}

// GET /v1/auth/github — redirects to GitHub OAuth authorization page.
func (v *HttpViews) GitHubAuthRedirect(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	clientID := v.Controller.SecretManager.GetSecret("GITHUB_CLIENT_ID")
	clientSecret := v.Controller.SecretManager.GetSecret("GITHUB_CLIENT_SECRET")
	redirectURI := v.Controller.SecretManager.GetSecret("GITHUB_REDIRECT_URI")

	state := utils.GenerateOAuthState(user.ID.String(), clientSecret)
	authURL := fmt.Sprintf(
		"https://github.com/login/oauth/authorize?client_id=%s&redirect_uri=%s&scope=repo&state=%s",
		url.QueryEscape(clientID), url.QueryEscape(redirectURI), url.QueryEscape(state),
	)
	http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
}

// GET /v1/auth/github/callback — handles the OAuth callback from GitHub.
func (v *HttpViews) GitHubAuthCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	stateStr := r.URL.Query().Get("state")

	if code == "" || stateStr == "" {
		utils.JSONErrorResponse(w, "missing code or state parameter", http.StatusBadRequest)
		return
	}

	clientSecret := v.Controller.SecretManager.GetSecret("GITHUB_CLIENT_SECRET")
	userID, err := utils.VerifyOAuthState(stateStr, clientSecret)
	if err != nil {
		utils.JSONErrorResponse(w, "invalid or expired OAuth state", http.StatusBadRequest)
		return
	}

	user, err := v.Controller.GetUserById(userID)
	if err != nil {
		utils.JSONErrorResponse(w, "user not found", http.StatusNotFound)
		return
	}

	if err := v.Controller.ConnectGitHub(user, code); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Redirect to frontend with success
	frontendURL := v.Controller.SecretManager.GetSecret("FRONTEND_URL")
	http.Redirect(w, r, frontendURL+"/settings?github=connected", http.StatusTemporaryRedirect)
}

// DELETE /v1/auth/github — disconnects GitHub account.
func (v *HttpViews) DisconnectGitHub(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if err := v.Controller.DisconnectGitHub(user); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}
	utils.JSONResponse(w, responses.SuccessResponse{Success: true}, http.StatusOK)
}

// GET /v1/auth/github/status — returns GitHub connection status.
func (v *HttpViews) GetGitHubConnectionStatus(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}
	resp := responses.GitHubConnectionResponse{
		Connected: user.GitHubUsername != "",
		Username:  user.GitHubUsername,
	}
	utils.JSONResponse(w, resp, http.StatusOK)
}

// POST /v2/apps/{appId}/export/github — creates a new GitHub repo and pushes.
func (v *HttpViews) ExportToGitHub(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if requireActiveSubscription(w, user) {
		return
	}

	// Check plan limit
	limits, limitsErr := v.Controller.DB.GetStandartUserLimitsByUser(user)
	if limitsErr == nil && !limits.CanExportToGitHub {
		utils.JSONCodedErrorResponse(w, "GITHUB_EXPORT_NOT_AVAILABLE",
			"GitHub export is not available on your plan", http.StatusForbidden, nil)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	var req requests.ExportToGitHubRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	validate := validator.New()
	if err := utils.ValidateFunc[requests.ExportToGitHubRequest](req, validate); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	export, err := v.Controller.ExportToGitHub(user, project.ID, req)
	if err != nil {
		errMsg := err.Error()
		if errMsg == "GITHUB_REPO_EXISTS" {
			utils.JSONCodedErrorResponse(w, "GITHUB_REPO_EXISTS",
				"a repository with this name already exists on your GitHub account", http.StatusConflict, nil)
			return
		}
		utils.JSONErrorResponse(w, errMsg, http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, toGitHubExportResponse(export), http.StatusCreated)
}

// POST /v2/apps/{appId}/push/github — pushes latest changes to existing GitHub repo.
func (v *HttpViews) PushToGitHub(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if requireActiveSubscription(w, user) {
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	export, err := v.Controller.PushToGitHub(user, project.ID)
	if err != nil {
		errMsg := err.Error()
		switch errMsg {
		case "GITHUB_AUTH_EXPIRED":
			utils.JSONCodedErrorResponse(w, "GITHUB_AUTH_EXPIRED",
				"your GitHub token has expired — please reconnect", http.StatusUnauthorized, nil)
		case "GITHUB_REPO_NOT_FOUND":
			utils.JSONCodedErrorResponse(w, "GITHUB_REPO_NOT_FOUND",
				"the GitHub repository was deleted — export again to create a new one", http.StatusNotFound, nil)
		case "GITHUB_PUSH_REJECTED":
			utils.JSONCodedErrorResponse(w, "GITHUB_PUSH_REJECTED",
				"push rejected by GitHub — the remote history has diverged. Remove the export and re-export.", http.StatusConflict, nil)
		case "GITHUB_PUSH_TIMEOUT":
			utils.JSONCodedErrorResponse(w, "GITHUB_PUSH_TIMEOUT",
				"push timed out — try again later", http.StatusGatewayTimeout, nil)
		default:
			utils.JSONErrorResponse(w, errMsg, http.StatusInternalServerError)
		}
		return
	}

	utils.JSONResponse(w, toGitHubExportResponse(export), http.StatusOK)
}

// GET /v2/apps/{appId}/export/github — returns the export record for a project.
func (v *HttpViews) GetGitHubExport(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	_, err = v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	parsedID, _ := uuid.Parse(appID)
	export, err := v.Controller.GetGitHubExport(parsedID)
	if err != nil {
		utils.JSONErrorResponse(w, "no GitHub export for this project", http.StatusNotFound)
		return
	}

	utils.JSONResponse(w, toGitHubExportResponse(export), http.StatusOK)
}

// DELETE /v2/apps/{appId}/export/github — removes the GitHub export for a project.
func (v *HttpViews) RemoveGitHubExport(w http.ResponseWriter, r *http.Request) {
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

	if err := v.Controller.RemoveGitHubExport(user, project.ID); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}
	utils.JSONResponse(w, responses.SuccessResponse{Success: true}, http.StatusOK)
}
```

- [ ] **Step 2: Add `GetUserById` controller wrapper if missing**

`GetUserById` already exists in `internal/database/users.go`. Check if a controller wrapper exists:

Run: `grep -r "func.*Controller.*GetUserById" internal/controllers/`

If missing, add to `internal/controllers/github_controller.go`:
```go
func (c *Controller) GetUserById(id string) (*models.User, error) {
	return c.DB.GetUserById(id)
}
```

- [ ] **Step 3: Verify it compiles**

Run: `cd /Users/knid/Projects/wemob-backend && go build ./...`
Expected: No errors

- [ ] **Step 5: Commit**

```bash
git add internal/views/github_views.go
git commit -m "feat: add GitHub view handlers (auth, export, push)"
```

---

### Task 11: Route Registration

**Files:**
- Modify: `cmd/main.go`

- [ ] **Step 1: Add GitHub auth routes**

Find the v1 or v2 route section. Add GitHub auth routes outside the `/apps` group (they need auth but not the apps middleware):

```go
// GitHub OAuth
r.Get("/auth/github", httpViews.GitHubAuthRedirect)
r.Get("/auth/github/callback", httpViews.GitHubAuthCallback)
r.Delete("/auth/github", httpViews.DisconnectGitHub)
r.Get("/auth/github/status", httpViews.GetGitHubConnectionStatus)
```

- [ ] **Step 2: Add GitHub export routes inside the `/{appId}` route group**

Find the `r.Route("/{appId}"` section (around line 1094) and add after the deploy routes:

```go
// GitHub export
r.Route("/github", func(r chi.Router) {
	r.Get("/", httpViews.GetGitHubExport)
	r.Post("/export", httpViews.ExportToGitHub)
	r.Post("/push", httpViews.PushToGitHub)
	r.Delete("/", httpViews.RemoveGitHubExport)
})
```

- [ ] **Step 3: Verify it compiles**

Run: `cd /Users/knid/Projects/wemob-backend && go build ./...`
Expected: No errors

- [ ] **Step 4: Verify the app starts**

Run: `cd /Users/knid/Projects/wemob-backend && go build -o bin/app cmd/main.go`
Expected: Binary built successfully

- [ ] **Step 5: Commit**

```bash
git add cmd/main.go
git commit -m "feat: register GitHub auth and export routes"
```

---

## Chunk 5: Secrets + Final Verification

### Task 12: Add Infisical Secrets Loading

**Files:**
- Modify: `cmd/main.go`

- [ ] **Step 1: Add secret fetches in main.go**

Find where other secrets are loaded (near Vercel service init, around line 654). Add comments noting the required secrets:

```go
// GitHub OAuth secrets (required for GitHub export feature)
// GITHUB_CLIENT_ID — OAuth App client ID
// GITHUB_CLIENT_SECRET — OAuth App client secret
// GITHUB_REDIRECT_URI — OAuth callback URL (e.g. https://api.wemob.io/v2/auth/github/callback)
// GITHUB_TOKEN_ENCRYPTION_KEY — 64-char hex string for AES-256-GCM token encryption
```

These are fetched on-demand via `c.SecretManager.GetSecret()` in the controller — no explicit loading needed at startup. Just document them.

- [ ] **Step 2: Commit**

```bash
git add cmd/main.go
git commit -m "docs: document required GitHub OAuth secrets"
```

---

### Task 13: End-to-End Verification

- [ ] **Step 1: Verify full project compiles**

Run: `cd /Users/knid/Projects/wemob-backend && go build ./...`
Expected: No errors

- [ ] **Step 2: Verify binary builds**

Run: `cd /Users/knid/Projects/wemob-backend && go build -o bin/app cmd/main.go`
Expected: Binary at `bin/app`

- [ ] **Step 3: Review all new files exist**

```bash
ls -la internal/utils/encryption.go
ls -la internal/utils/github_oauth.go
ls -la internal/models/app_github_export.go
ls -la internal/requests/github_requests.go
ls -la internal/responses/github_responses.go
ls -la internal/database/app_github_export.go
ls -la internal/controllers/github_controller.go
ls -la internal/views/github_views.go
```

- [ ] **Step 4: Final commit if any loose changes**

```bash
git status
```
