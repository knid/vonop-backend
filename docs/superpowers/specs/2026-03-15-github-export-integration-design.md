# GitHub Export Integration for Generated Web Apps

**Date:** 2026-03-15
**Status:** Approved

## Problem

Users generate web apps in Wemob but have no way to get the source code into their own GitHub repositories. They want to own their code, use their own CI/CD, and collaborate with their team via GitHub.

## Solution

One-way, on-demand GitHub export via OAuth App. Users connect their GitHub account, then push any web app's full git history to a new GitHub repo they own.

## Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Auth method | GitHub OAuth App | Simpler than GitHub App, sufficient for user-level repo access |
| Sync direction | One-way (Wemob → GitHub) | Avoids merge conflict complexity; two-way can be added later |
| Push trigger | On-demand (user clicks button) | User stays in control; auto-sync adds complexity |
| Repo creation | Wemob creates new repo | Avoids risk of overwriting existing repos |
| Git history | Full history preserved | Local repos already have clean commit history; `git push` preserves it |
| Push mechanism | `git push` with transient URL | No persistent remote, token never stored on disk |
| Access restriction | Paid subscribers only (plan-gated via `CanExportToGitHub` limit) | Premium feature |
| Service architecture | Controller methods (no separate service package) | Only 2 GitHub API calls needed; full service is overkill |

## Architecture

### 1. GitHub OAuth Flow

**New Infisical secrets:** `GITHUB_CLIENT_ID`, `GITHUB_CLIENT_SECRET`, `GITHUB_REDIRECT_URI`, `GITHUB_TOKEN_ENCRYPTION_KEY`

**Routes:**
```
GET  /v1/auth/github           → GitHubAuthRedirect
GET  /v1/auth/github/callback  → GitHubAuthCallback
DELETE /v1/auth/github         → DisconnectGitHub
```

**Flow:**
1. Frontend opens `GET /v1/auth/github` (authenticated — bearer token required)
2. Backend generates an HMAC-signed `state` param: `base64(userID + expiry + HMAC(userID + expiry, GITHUB_CLIENT_SECRET))` — stateless, no Redis needed
3. Redirects to `https://github.com/login/oauth/authorize?client_id=...&redirect_uri=...&scope=repo&state=...`
4. GitHub redirects to callback URL with `code` and `state`
5. Backend verifies HMAC signature and expiry on `state`, exchanges `code` for access token via `POST https://github.com/login/oauth/access_token`
6. Fetches GitHub username via `GET https://api.github.com/user`
7. Encrypts token (AES-256 with `GITHUB_TOKEN_ENCRYPTION_KEY`), stores on User model
8. Redirects to frontend with success indicator

**User model additions:**
```go
GitHubAccessToken  string `bun:",nullzero" json:"-"`      // AES-256 encrypted
GitHubUsername     string `bun:",nullzero" json:"github_username"`
```

`json:"-"` on token ensures it never appears in API responses.

### 2. Export to GitHub

**Routes:**
```
POST   /v1/apps/{appId}/export/github  → ExportToGitHub (create repo + first push)
POST   /v1/apps/{appId}/push/github    → PushToGitHub (subsequent pushes)
GET    /v1/apps/{appId}/export/github   → GetGitHubExport (check export status)
DELETE /v1/apps/{appId}/export/github   → RemoveGitHubExport (disconnect repo)
```

**Request type:**
```go
type ExportToGitHubRequest struct {
    RepoName    string `json:"repo_name" validate:"required,min=1,max=100"`
    Description string `json:"description" validate:"max=350"`
    Private     bool   `json:"private"`
}
```

**Export flow (first push):**
1. Auth check + subscription check (`CanExportToGitHub` limit)
2. Verify user has connected GitHub (`GitHubAccessToken` not empty)
3. Decrypt token
4. Create repo via GitHub API: `POST https://api.github.com/user/repos`
5. Acquire per-project lock
6. Detect current branch: `git rev-parse --abbrev-ref HEAD`
7. Push with transient URL: `git push https://x-access-token:{token}@github.com/{owner}/{repo}.git {branch}` — token never persisted to disk
8. Release lock
9. Save `AppGitHubExport` record to DB
10. Return repo URL in response

**Subsequent pushes:**
1. Look up existing `AppGitHubExport` record
2. Decrypt token, acquire lock
3. Push with transient URL (same as above)
4. Update `LastPushedAt` timestamp, release lock

**Re-export to different repo:** User calls `DELETE` first (deletes DB record), then creates new export. No remote cleanup needed since no remotes are persisted.

### 3. New Model: `AppGitHubExport`

```go
type AppGitHubExport struct {
    ID           uuid.UUID  `bun:",pk,type:uuid,default:uuid_generate_v4()"`
    AppProjectID uuid.UUID  `bun:",notnull,type:uuid,unique"`
    UserID       uuid.UUID  `bun:",notnull,type:uuid"`
    RepoFullName string     `bun:",notnull"`   // "owner/repo-name"
    RepoURL      string     `bun:",notnull"`   // "https://github.com/owner/repo-name"
    Private      bool       `bun:",notnull"`
    LastPushedAt time.Time  `bun:",nullzero"`
    CreatedAt    time.Time  `bun:",nullzero,notnull,default:current_timestamp"`
    UpdatedAt    time.Time  `bun:",nullzero,notnull,default:current_timestamp"`

    AppProject *AppProject `bun:"rel:belongs-to,join:app_project_id=id"`
}
```

Added to `createSchema()` in `cmd/main.go`.

### 4. gitrepo.Repo Additions

```go
func (r *Repo) PushToURL(url, branch string) error      // git push <url> <branch> (60s timeout, acquires lock)
func (r *Repo) CurrentBranch() (string, error)           // git rev-parse --abbrev-ref HEAD
```

`PushToURL` uses a transient URL — the token is passed as an argument and never written to `.git/config`. This avoids storing plaintext tokens on the filesystem. The method acquires the per-project lock to prevent concurrent pushes or pushes during generation.

Note: the token will briefly appear in process args (`/proc/{pid}/cmdline`). Acceptable for a server environment with short-lived push operations (60s max).

### 5. Controller Methods

```go
func (c *Controller) ConnectGitHub(code, state string) (*models.User, error)
func (c *Controller) DisconnectGitHub(user *models.User) error
func (c *Controller) ExportToGitHub(user *models.User, projectID uuid.UUID, req requests.ExportToGitHubRequest) (*models.AppGitHubExport, error)
func (c *Controller) PushToGitHub(user *models.User, projectID uuid.UUID) (*models.AppGitHubExport, error)
func (c *Controller) RemoveGitHubExport(user *models.User, projectID uuid.UUID) error
func (c *Controller) GetGitHubExport(user *models.User, projectID uuid.UUID) (*models.AppGitHubExport, error)
```

### 6. DB Methods

```go
func (d *Database) CreateAppGitHubExport(export *AppGitHubExport) error
func (d *Database) GetAppGitHubExportByProjectID(projectID uuid.UUID) (*AppGitHubExport, error)
func (d *Database) DeleteAppGitHubExport(projectID uuid.UUID) error
func (d *Database) UpdateAppGitHubExportLastPushed(projectID uuid.UUID) error
```

### 7. View Handlers

```go
func (v *HttpViews) GitHubAuthRedirect(w, r)
func (v *HttpViews) GitHubAuthCallback(w, r)
func (v *HttpViews) DisconnectGitHub(w, r)
func (v *HttpViews) ExportToGitHub(w, r)
func (v *HttpViews) PushToGitHub(w, r)
func (v *HttpViews) GetGitHubExport(w, r)
func (v *HttpViews) RemoveGitHubExport(w, r)
```

All follow the standard request flow pattern: extract token → authenticate → parse body → validate → controller method → JSON response.

## Security

| Concern | Mitigation |
|---------|-----------|
| Token storage | AES-256 encrypted with `GITHUB_TOKEN_ENCRYPTION_KEY` from Infisical |
| Token in API responses | `json:"-"` tag on `GitHubAccessToken` |
| OAuth CSRF | HMAC-signed stateless `state` with embedded expiry (10min) |
| Token on disk | Never persisted — transient URL passed directly to `git push` |
| Project ownership | Export/push verify authenticated user owns the project |
| Subscription gate | All export endpoints require active subscription |
| `.env` files | Already excluded by `.gitignore` in repo (`.env*` pattern) |

## Error Handling

| Scenario | Handling |
|----------|---------|
| GitHub token expired/revoked | `git push` returns 401 → `GITHUB_AUTH_EXPIRED` error code |
| Repo name already exists | GitHub API returns 422 → `GITHUB_REPO_EXISTS` error |
| User deleted repo on GitHub | `git push` fails → remove export record, `GITHUB_REPO_NOT_FOUND` |
| Push during active generation | Per-project mutex in gitrepo.Service prevents concurrent access |
| Disconnect with active exports | Delete all `AppGitHubExport` records (no remotes to clean up) |
| Network timeout on push | `exec.CommandContext` 60s timeout → `GITHUB_PUSH_TIMEOUT` |
| Non-fast-forward push rejected | GitHub repo was force-pushed/reset → `GITHUB_PUSH_REJECTED`, user should DELETE export and re-export |

No retry logic. If push fails, user clicks the button again.

## Token Encryption

New utility functions in `internal/utils/`:

```go
func EncryptGitHubToken(plaintext, key string) (string, error)   // AES-256-GCM
func DecryptGitHubToken(ciphertext, key string) (string, error)  // AES-256-GCM
```

Key is 32-byte hex string from Infisical `GITHUB_TOKEN_ENCRYPTION_KEY`.

## Files to Create/Modify

| File | Action |
|------|--------|
| `internal/models/app_github_export.go` | Create — new model |
| `internal/requests/github_requests.go` | Create — ExportToGitHubRequest |
| `internal/responses/github_responses.go` | Create — response types |
| `internal/database/app_github_export.go` | Create — DB methods |
| `internal/controllers/github_controller.go` | Create — controller methods |
| `internal/views/github_views.go` | Create — view handlers |
| `internal/utils/encryption.go` | Create — token encrypt/decrypt |
| `internal/services/gitrepo/repo.go` | Modify — add PushToURL, CurrentBranch |
| `internal/models/limits.go` | Modify — add CanExportToGitHub to StandartUserLimits |
| `internal/models/user.go` | Modify — add GitHubAccessToken, GitHubUsername fields |
| `cmd/main.go` | Modify — add routes, add AppGitHubExport to createSchema |
