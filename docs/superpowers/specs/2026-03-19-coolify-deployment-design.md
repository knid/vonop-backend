# Self-Hosted App Deployment: Vercel → Coolify

**Date:** 2026-03-19
**Status:** Approved
**Scope:** Replace Vercel with Coolify + Gitea for deploying user-generated Next.js apps

## Problem

User-generated AI apps are currently deployed to Vercel via their API. This creates a dependency on Vercel's infrastructure and pricing. We want to move to self-hosted infrastructure using Coolify as the deployment platform.

## Decision

Replace Vercel with **Coolify + Gitea** (Approach A: Gitea + Nixpacks). This is the closest to the current Vercel flow — push code, platform builds and deploys. Clean cutover, no migration of existing apps.

## Architecture

### Infrastructure

```
┌─────────────────────────┐       ┌──────────────────────────────────┐
│   Backend Server        │       │   Coolify Server                 │
│                         │       │                                  │
│  Go API (:8001)         │──────▶│  Gitea (git repos)               │
│  Caddy (reverse proxy)  │       │  Coolify (build + run apps)      │
│  Local git repos        │       │  Traefik (routing, TLS)          │
│                         │       │                                  │
│  Pushes files to Gitea  │       │  *.wemob.app wildcard domain     │
│  Calls Coolify API      │       │  Auto TLS via Let's Encrypt      │
└─────────────────────────┘       └──────────────────────────────────┘
```

**DNS:** `*.wemob.app` A record points to the Coolify server IP.

**Gitea** runs as a one-click service inside Coolify. It serves as the git remote that Coolify clones from to build apps.

**Coolify** uses Nixpacks to auto-detect Next.js, build standalone output, and run the app in a container.

### Deployment Flow

```
Current (Vercel):
  git repo → upload files to Vercel blob store → Vercel builds → {slug}.wemob.app

New (Coolify):
  git repo → push to Gitea → Coolify builds via Nixpacks → {slug}.wemob.app
```

Detailed steps:

1. User clicks Deploy
2. Backend reads files from local git repo (unchanged)
3. Backend injects `.env.local` + analytics script (unchanged)
4. Backend pushes files to Gitea:
   - The local git repo already exists via `GitRepoService`
   - First deploy: create repo on Gitea via API → add Gitea as remote → `git push`
   - Subsequent deploys: `git push` to existing Gitea remote (force push to handle rebases)
   - This avoids per-file REST calls — a single `git push` syncs all files efficiently
5. Backend calls Coolify API:
   - First deploy: `POST /api/v1/applications/public`
     - `project_uuid`: from `COOLIFY_PROJECT_UUID` secret
     - `server_uuid`: from `COOLIFY_SERVER_UUID` secret
     - `environment_name`: `"production"`
     - `git_repository`: Gitea repo URL
     - `git_branch`: `main`
     - `build_pack`: `nixpacks`
     - `domains`: `https://{slug}.wemob.app`
     - `ports_exposes`: `3000`
     - `instant_deploy`: `true`
     - `is_force_https_enabled`: `true`
     - `autogenerate_domain`: `false`
   - Subsequent deploys: `POST /api/v1/applications/{uuid}/restart` (triggers full rebuild from git)
6. Backend syncs env vars via `PATCH /api/v1/applications/{uuid}/envs/bulk`
7. Backend saves `CoolifyProject` record to DB
8. Backend creates `AppDeployment` record with `provider: "coolify"`

### Status Polling

`GetLatestDeploymentStatus` changes:

- `GET /api/v1/applications/{uuid}` to check app status
- Map Coolify states to internal states: `building` / `deployed` / `failed`
- `GET /api/v1/applications/{uuid}/logs` to fetch build logs on failure

### Custom Domains

- Add domain: update app's `domains` field via `PATCH /api/v1/applications/{uuid}` (comma-separated FQDNs, append new domain to existing list)
- Remove domain: same PATCH, remove domain from the comma-separated list
- Coolify handles TLS for custom domains automatically via Let's Encrypt
- Domain verification: replace Vercel's `GetDomainConfig` API with direct DNS lookups from the backend using Go's `net.LookupCNAME` / `net.LookupHost` to check if the domain resolves to the Coolify server IP
- `WEMOB_CNAME_TARGET` constant in `internal/constants/settings.go` must be updated to point to the Coolify server hostname/IP

### Slug Changes

When a user renames their project slug, `app_project_controller.go` has a goroutine that reassigns the `*.wemob.app` subdomain. This currently calls `VercelService.RemoveDomain` / `AddDomain`. It must be updated to call `CoolifyService.UpdateDomains` — fetching the current domains list, replacing the old subdomain with the new one.

### Environment Variables

- Collected the same way as now (`CollectAllEnvVars`)
- Synced via `PATCH /api/v1/applications/{uuid}/envs/bulk` with full env var list
- Stale env vars removed by comparing with `GET /api/v1/applications/{uuid}/envs`
- `.env.local` still injected into file map for `NEXT_PUBLIC_*` build-time vars

## Code Changes

### New Files

**`internal/services/coolify/coolify.go`** — Coolify API client

Mirrors the Vercel service interface:
- `CreateApp(name, giteaRepoURL, domain string) (appUUID string, error)`
- `DeployApp(appUUID string) error` (triggers restart/redeploy)
- `GetAppStatus(appUUID string) (status string, error)`
- `GetAppLogs(appUUID string) (string, error)`
- `SetEnvVars(appUUID string, envVars map[string]string) error`
- `SyncEnvVars(appUUID string, desired map[string]string) error`
- `UpdateDomains(appUUID string, domains []string) error`
- `DeleteApp(appUUID string) error`

API base: Coolify instance URL + `/api/v1`
Auth: Bearer token via `Authorization` header

**`internal/services/gitea/gitea.go`** — Gitea API client

- `CreateRepo(name string) (cloneURL string, error)` — `POST /api/v1/orgs/{org}/repos`
- `DeleteRepo(repoName string) error` — `DELETE /api/v1/repos/{owner}/{repo}`
- `GetRepoCloneURL(repoName string) string` — returns the authenticated clone URL

File pushing uses `git push` from the existing local repo (via `GitRepoService`) to the Gitea remote, not per-file REST calls. The `GitRepoService` already manages local git repos for each project — we just add Gitea as a remote and push. This is efficient (single operation for all files) and handles binary files, deletions, and large changesets correctly.

**`internal/models/coolify_project.go`** — replaces `VercelProject`

```go
type CoolifyProject struct {
    ID             uuid.UUID         `bun:",pk,type:uuid,default:uuid_generate_v4()"`
    AppProjectID   uuid.UUID         `bun:",notnull,type:uuid,unique"`
    AppProject     *AppProject       `bun:"rel:belongs-to,join:app_project_id=id"`
    CoolifyAppUUID string            `bun:",notnull"`
    GiteaRepoName  string            `bun:",notnull"`
    ProductionURL  string            `bun:",nullzero"`
    CustomDomain   string            `bun:",nullzero"`
    Framework      string            `bun:",notnull,default:'nextjs'"`
    CreatedAt      time.Time         `bun:"created_at,nullzero,notnull,default:current_timestamp"`
    UpdatedAt      time.Time         `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}
```

**`internal/database/coolify_projects.go`** — replaces `vercel_projects.go`

- `GetCoolifyProjectByAppProjectID(appProjectID string) (*CoolifyProject, error)`
- `CreateCoolifyProject(cp *CoolifyProject) (*CoolifyProject, error)`
- `UpdateCoolifyProject(cp *CoolifyProject) (*CoolifyProject, error)`

### Modified Files

**`internal/controllers/app_build_controller.go`**
- `DeployToVercel` → `DeployToCoolify` — new flow using Gitea + Coolify services
- `GetLatestDeploymentStatus` — poll Coolify API instead of Vercel. Must handle old `AppDeployment` records where `Provider == "vercel"` gracefully (skip polling, return existing status)
- `BuildAppMobile` — unchanged (still wraps the deployed URL)
- Analytics injection, env var collection — unchanged

**`internal/controllers/controller.go`**
- Replace `VercelService` field with `CoolifyService *coolify.Service` and `GiteaService *gitea.Service`

**`internal/controllers/app_project_controller.go`**
- `DeleteAppProject`: add cleanup for Coolify app (`CoolifyService.DeleteApp`) and Gitea repo (`GiteaService.DeleteRepo`) alongside existing Neon/git cleanup
- Slug-change goroutine: replace `VercelService.RemoveDomain` / `AddDomain` with `CoolifyService.UpdateDomains`

**`internal/controllers/app_domain_controller.go`**
- Domain add/remove uses Coolify API instead of Vercel
- Domain verification: replace `VercelService.GetDomainConfig()` with Go `net.LookupCNAME` / `net.LookupHost` DNS lookups

**`internal/models/limits.go`**
- Rename `CanDeployToVercel` → `CanDeployWeb` (provider-agnostic)

**`internal/constants/settings.go`**
- Update `WEMOB_CNAME_TARGET` to point to the Coolify server hostname

**`cmd/main.go`**
- Init `coolify.NewService()` and `gitea.NewService()` from Infisical secrets
- Inject into Controller
- Add `CoolifyProject` to `createSchema()`
- Remove Vercel service init
- Update subscription seed data for renamed `CanDeployWeb` field

### Removed Files

- `internal/services/vercel/vercel.go`
- `internal/models/vercel_project.go`
- `internal/database/vercel_projects.go`

### Git Push Integration

The `GitRepoService` already manages local git repos. To push to Gitea:

1. First deploy: `GitRepoService.AddRemote(projectID, "coolify", giteaCloneURL)`
2. Every deploy: `GitRepoService.Push(projectID, "coolify", "main")` — force push to ensure Gitea matches local state
3. On delete: remote is cleaned up when the local repo is deleted (existing behavior)

This requires adding `AddRemote` and `Push` methods to `GitRepoService` (thin wrappers around `go-git` or shell `git` commands).

### Unchanged

- `internal/views/app_deploy_views.go` — view layer stays the same, calls renamed controller methods
- `internal/models/app_deployment.go` — same structure, `Provider` field value changes from `"vercel"` to `"coolify"`
- Mobile build flow — unchanged, wraps the deployed URL
- Analytics injection (`injectAnalyticsScript`) — unchanged
- Env var collection (`CollectAllEnvVars`, `BuildDotEnvLocal`) — unchanged

## Secrets (Infisical)

| Secret | Description |
|--------|-------------|
| `COOLIFY_API_URL` | Coolify instance URL (e.g., `https://coolify.wemob.io`) |
| `COOLIFY_API_TOKEN` | Coolify API bearer token |
| `COOLIFY_SERVER_UUID` | Target server UUID in Coolify |
| `COOLIFY_PROJECT_UUID` | Coolify project to group all user apps under |
| `COOLIFY_ENVIRONMENT_UUID` | Environment UUID (e.g., production) |
| `GITEA_URL` | Gitea instance URL (e.g., `https://git.wemob.io`) |
| `GITEA_API_TOKEN` | Gitea API token |
| `GITEA_ORG` | Gitea organization to create repos under |

## Migration Strategy

**Clean cutover.** No dual-provider logic, no background migration job.

- All new deploys go through Coolify
- Existing Vercel apps stay as-is — users redeploy to move them to Coolify naturally
- Vercel code is removed from the codebase
- Vercel secrets can be removed from Infisical once all apps have been redeployed

## Health Checks & Reliability

- Coolify apps configured with health check on `GET /` port `3000`
- Container restart policy: `unless-stopped` (Coolify default)
- Build timeout: Nixpacks builds typically complete in 1-3 minutes for Next.js; Coolify handles timeout internally
- `AppDeployment.ProviderData` stores `{"coolify_app_uuid": "..."}` for Coolify deploys (old Vercel records keep their existing `deployment_id` data)

## Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| Coolify server overloaded by hundreds of concurrent builds | Set resource limits per app (`limits_memory`, `limits_cpus`). Coolify supports multiple servers — add capacity as needed. |
| Gitea becomes a bottleneck | Gitea is lightweight. Can be scaled or replaced with any git server. |
| Nixpacks build failures for edge-case Next.js configs | Can fall back to Dockerfile-based builds for specific apps. Nixpacks handles standard Next.js well. |
| Domain propagation delays | Same as current — DNS changes take time regardless of provider. |
| Build logs less detailed than Vercel | Coolify provides container logs. Sufficient for debugging. |
| Old Vercel deployments in DB | Status polling checks `Provider` field — skips Coolify polling for `"vercel"` records, returns existing status as-is. |
