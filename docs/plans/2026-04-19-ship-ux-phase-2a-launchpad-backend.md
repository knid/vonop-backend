# Ship UX — Phase 2a · Launchpad Aggregate + Skeleton Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the backend aggregate endpoint `GET /v1/apps/:id/launchpad` and a minimal feature-flagged frontend route `/apps/:appId/ship` that renders the Status hero + Stores zone with real data. Listing tiles, Builds feed, Identity panel, and one-click new build are reserved for Phase 2b. Nothing user-visible unless the PostHog feature flag `new_ship_ux` is enabled for the current user.

**Architecture:** Backend adds one read-only aggregate endpoint that joins across AppProject, MobileAppConfig, Deployment, Build, AppBuildTask, apple_connections, and google_play_connections to return a single JSON shape the frontend can render without further round-trips. Frontend adds a Next.js app-router route + a thin Launchpad component tree (2 of 5 zones implemented in 2a; the other 3 stubbed) + a `useFeatureFlag` hook wrapping posthog-js.

**Tech stack:** Go 1.22+, Bun ORM, chi/v5. Next.js 16 (app router), React 19, Tailwind 4, posthog-js.

**Design spec:** `docs/design/2026-04-18-wemob-publish-ux-redesign-design.md` (see "Launchpad — five zones").

---

## Test strategy

Neither repo has a test suite. Verification is by:
- `go build ./...` and `go vet ./...` clean (backend).
- `npm run lint` and `npm run build` clean (frontend).
- Postgres MCP for DB state.
- Manual `curl` + browser for runtime behavior (the user will run these; we can't boot the backend in the sandbox without Infisical creds).

Every task has explicit `Verify:` commands.

## File structure overview

### Backend (wemob-backend) — new files

```
internal/responses/launchpad.go      — LaunchpadResponse struct (single aggregate shape)
internal/controllers/launchpad_controller.go  — GetLaunchpad method
internal/views/launchpad_views.go    — GetLaunchpad HTTP handler
```

### Backend — modified

```
cmd/main.go                          — register GET /v1/apps/:appId/launchpad route
```

### Frontend (wemob-app) — new files

```
src/app/apps/[appId]/ship/page.tsx               — new route, AuthGuard + StudioShell + flag gate
src/components/ship/launchpad.tsx                — container, 5 zones (2 real + 3 stubs in 2a)
src/components/ship/launchpad-header.tsx         — Status hero zone
src/components/ship/store-tile.tsx               — Play / App Store tile variants
src/hooks/use-launchpad.ts                       — fetches aggregate via apiClient
src/hooks/use-feature-flag.ts                    — thin wrapper around posthog.isFeatureEnabled
src/lib/ship/launchpad-types.ts                  — shared TS types mirroring backend response
```

### Frontend — modified

```
src/lib/api.ts                       — add apiClient.getLaunchpad(appId)
```

## Why these boundaries

- **`launchpad_controller.go` separate from `app_dashboard_controller.go`** — the dashboard controller is already crowded with query methods serving the existing v2 dashboard; Launchpad is a distinct surface with its own aggregation logic that will accrete helpers in Phase 2b–5 (timelines, ETAs, event streams).
- **`launchpad-types.ts` separate from the backend response** — frontend doesn't need to mirror every backend field. Keeping a hand-rolled TS shape gives us a place to add frontend-computed derived values in 2b.
- **`use-feature-flag.ts`** — no existing wrapper exists (we checked). One helper file now, reused across Phase 2b–5 for flag-gating every new surface.
- **2a implements only 2 of 5 zones** — Status hero + Stores tiles render real data. Listing / Builds / Identity render as explicit "Coming in Phase 2b" placeholders so the route is openable and the feature flag ladder can ramp cautiously.

---

## Task 1: Define `LaunchpadResponse` struct

**Why:** Lock the aggregate shape before anyone depends on it.

**Files:**
- Create: `internal/responses/launchpad.go`

- [ ] **Step 1: Create the response file**

Write `/Users/knid/Projects/wemob/wemob-backend/internal/responses/launchpad.go`:

```go
package responses

// LaunchpadResponse is the single-call aggregate shape served by
// GET /v1/apps/:appId/launchpad. It powers the Ship tab's Launchpad
// surface, combining data from AppProject, MobileAppConfig, Deployment,
// Build, AppBuildTask, apple_connections, and google_play_connections
// so the frontend renders without additional round-trips.
//
// Phase 2a fills HeroStatus, WebPreview, and Stores. Listing, Builds,
// and Identity are declared here but returned zero/empty in 2a; they
// are filled in Phase 2b.
type LaunchpadResponse struct {
	App        LaunchpadApp        `json:"app"`
	HeroStatus LaunchpadHero       `json:"hero_status"`
	WebPreview LaunchpadWebPreview `json:"web_preview"`
	Stores     LaunchpadStores     `json:"stores"`

	// Phase 2b — declared so the frontend type is stable, but filled
	// with zero values in 2a. Populating these is Phase 2b's job.
	Listing  LaunchpadListing  `json:"listing"`
	Builds   LaunchpadBuilds   `json:"builds"`
	Identity LaunchpadIdentity `json:"identity"`
}

type LaunchpadApp struct {
	ID          string `json:"id"`           // AppProject ID (uuid string)
	Name        string `json:"name"`
	BundleID    string `json:"bundle_id"`    // e.g. "com.tasktracker.pro"
	IconS3Key   string `json:"icon_s3_key"`  // build_defaults.icon_s3_key
	VersionName string `json:"version_name"` // "1.0.3"
	BuildNumber int    `json:"build_number"` // monotonic
}

// LaunchpadHero is the computed one-sentence summary + single primary
// CTA that answers "what do I do now?".
type LaunchpadHero struct {
	Summary     string `json:"summary"`      // "Live on Play Store — App Store needs 3 things"
	PrimaryCTA  string `json:"primary_cta"`  // human label, e.g. "Finish App Store setup"
	PrimaryHref string `json:"primary_href"` // deep link within /apps/:id/ship
}

type LaunchpadWebPreview struct {
	Status      string `json:"status"`       // "deployed" | "building" | "failed" | "none"
	URL         string `json:"url,omitempty"`
	DeployedAt  string `json:"deployed_at,omitempty"` // ISO 8601
}

type LaunchpadStores struct {
	PlayStore LaunchpadStoreTile `json:"play_store"`
	AppStore  LaunchpadStoreTile `json:"app_store"`
}

type LaunchpadStoreTile struct {
	// Status vocabulary: "not_configured" | "configuring" | "ready_to_ship"
	// | "building" | "needs_review" | "submitted" | "in_review" | "live"
	// | "update_available" | "rejected" | "failed"
	Status         string   `json:"status"`
	LiveVersion    string   `json:"live_version,omitempty"`   // "1.0.2"
	LiveBuildNum   int      `json:"live_build_num,omitempty"` // 11
	InstallCount   int      `json:"install_count,omitempty"`  // only when live, Play Store only (iOS API is paid)
	Rating         float64  `json:"rating,omitempty"`         // Play Store only
	MissingItems   []string `json:"missing_items,omitempty"`  // e.g. ["Screenshots (3 needed)", "Privacy URL"]
	PrimaryAction  string   `json:"primary_action,omitempty"` // "Submit update" | "Start setup" | "View listing"
	ConnectionKind string   `json:"connection_kind,omitempty"` // "managed" | "user" | ""
}

// Stubs for Phase 2b — declared for type stability.

type LaunchpadListing struct {
	ReadyItems int `json:"ready_items"` // out of 8 tiles
	TotalItems int `json:"total_items"`
}

type LaunchpadBuilds struct {
	Active []LaunchpadBuildSummary `json:"active"`
	Recent []LaunchpadBuildSummary `json:"recent"`
}

type LaunchpadBuildSummary struct {
	ID       string `json:"id"`
	Platform string `json:"platform"` // "android" | "ios"
	Status   string `json:"status"`
	Version  string `json:"version,omitempty"`
	CreatedAt string `json:"created_at"`
}

type LaunchpadIdentity struct {
	BundleID    string   `json:"bundle_id"`
	Version     string   `json:"version"` // "1.0.3 (Build 12)"
	Permissions []string `json:"permissions"`
}
```

- [ ] **Step 2: Verify compile**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
go build ./...
go vet ./...
```

Both clean.

- [ ] **Step 3: Commit**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
git add internal/responses/launchpad.go
git commit -m "feat(ship): define LaunchpadResponse aggregate shape

Single-call response served by GET /v1/apps/:appId/launchpad. Phase 2a
fills HeroStatus, WebPreview, Stores; Listing / Builds / Identity are
declared with empty defaults so the frontend type is stable while 2b
fills them in."
```

---

## Task 2: Add `GetLaunchpad` controller — skeleton

**Why:** Wire a working endpoint that returns an empty Launchpad for a given app, so we can hang routes off it and validate the shape before filling logic in.

**Files:**
- Create: `internal/controllers/launchpad_controller.go`

- [ ] **Step 1: Create the controller file**

Write `/Users/knid/Projects/wemob/wemob-backend/internal/controllers/launchpad_controller.go`:

```go
package controllers

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
)

// GetLaunchpad assembles the single aggregate response for the Ship
// tab's Launchpad surface. Callers supply the AppProject ID and the
// authenticated user; the controller enforces ownership before building
// the response.
//
// Returns (nil, error) when the app doesn't exist or the user doesn't
// own it; callers translate that to 404 or 403.
func (c *Controller) GetLaunchpad(ctx context.Context, user *models.User, appID uuid.UUID) (*responses.LaunchpadResponse, error) {
	if user == nil {
		return nil, errors.New("user required")
	}
	// Load the project (and verify ownership).
	var project models.AppProject
	if err := c.DB.DB.NewSelect().
		Model(&project).
		Where("id = ?", appID).
		Scan(ctx); err != nil {
		return nil, err
	}
	if project.UserID != user.ID {
		// Organization-shared apps land here too; Phase 2a doesn't
		// cover org membership checks — that's Phase 4/5.
		return nil, errors.New("not authorized")
	}

	resp := &responses.LaunchpadResponse{
		App: responses.LaunchpadApp{
			ID:   project.ID.String(),
			Name: project.Name,
		},
	}

	// Phase 2a populates HeroStatus, WebPreview, Stores. Task 3 fills
	// the real values; this skeleton returns zero data so Task 5's
	// smoke test proves the route is wired before full logic lands.
	return resp, nil
}
```

- [ ] **Step 2: Verify compile**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
go build ./...
go vet ./...
```

Clean.

- [ ] **Step 3: Commit**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
git add internal/controllers/launchpad_controller.go
git commit -m "feat(ship): scaffold GetLaunchpad controller

Returns the AppProject's name + id inside a LaunchpadResponse; all
other fields are zero for now. Task 3 fills Hero/WebPreview/Stores."
```

---

## Task 3: Fill `GetLaunchpad` — Hero, WebPreview, Stores

**Why:** Turn the skeleton into a real aggregate.

**Files:**
- Modify: `internal/controllers/launchpad_controller.go`

- [ ] **Step 1: Load build_defaults and fill `App`**

After the project load, read `project.Settings["build_defaults"]` (the Phase 1 backfill). Populate:

```go
if bd, ok := project.Settings["build_defaults"].(map[string]interface{}); ok {
    if iconKey, _ := bd["icon_s3_key"].(string); iconKey != "" {
        resp.App.IconS3Key = iconKey
    }
}

// Bundle ID is derived from project.Slug — the backend's current
// convention is "com.wemob." + slug. Phase 2b adds a proper override
// field; 2a matches existing Codemagic CM_PACKAGE_NAME behavior.
if project.Slug != "" {
    resp.App.BundleID = "com.wemob." + project.Slug
}
```

- [ ] **Step 2: Read v1 project ID for downstream queries**

```go
var v1ProjectID string
if v, ok := project.Settings["v1_project_id"].(string); ok {
    v1ProjectID = v
}
```

If `v1ProjectID` is empty, skip all version/build/deployment lookups — the project hasn't been provisioned yet. Keep `resp.App.VersionName = ""`, `BuildNumber = 0`, and render zero stores tiles.

- [ ] **Step 3: Load the latest build for version info**

If `v1ProjectID != ""`:

```go
var latestBuild models.Build
err := c.DB.DB.NewSelect().
    Model(&latestBuild).
    Where("project_id = ?", v1ProjectID).
    Order("created_at DESC").
    Limit(1).
    Scan(ctx)
if err == nil {
    resp.App.VersionName = latestBuild.VersionString
    resp.App.BuildNumber = latestBuild.BuildNumber
}
```

`err != nil` (no rows) is fine — leaves fields empty.

- [ ] **Step 4: Fill `WebPreview`**

Call the existing deploy controller helper to get the web preview URL. (Look up what `app_deploy_views.go` / `AppDeployController` uses — likely something like `c.GetAppDeployStatus(projectID)` or similar. Match the existing pattern rather than re-implementing.)

```go
if deployed, _ := c.GetAppDeployStatus(ctx, project.ID); deployed != nil {
    resp.WebPreview.Status = deployed.Status    // "deployed" | "building" | etc.
    resp.WebPreview.URL = deployed.DeployedURL
    if !deployed.DeployedAt.IsZero() {
        resp.WebPreview.DeployedAt = deployed.DeployedAt.Format(time.RFC3339)
    }
} else {
    resp.WebPreview.Status = "none"
}
```

If the existing controller method has a different name or return shape, adapt to what's actually there — grep `controllers/` for `DeployStatus`, `AppDeploy`, or look at what `use-app-deploy.ts` calls on the frontend.

- [ ] **Step 5: Compute Play Store tile**

```go
resp.Stores.PlayStore = c.buildStoreTile(ctx, user.ID, v1ProjectID, "android")
resp.Stores.AppStore = c.buildStoreTile(ctx, user.ID, v1ProjectID, "ios")
```

Add a private helper at the bottom of the file:

```go
func (c *Controller) buildStoreTile(ctx context.Context, userID uuid.UUID, v1ProjectID, platform string) responses.LaunchpadStoreTile {
    tile := responses.LaunchpadStoreTile{Status: "not_configured"}
    if v1ProjectID == "" {
        return tile
    }

    // Is there a connection for this platform on the user?
    hasConnection := false
    switch platform {
    case "ios":
        var count int
        c.DB.DB.NewSelect().
            Model((*models.AppleConnection)(nil)).
            Where("user_id = ?", userID).
            ColumnExpr("count(*)").
            Scan(ctx, &count)
        hasConnection = count > 0
    case "android":
        var count int
        c.DB.DB.NewSelect().
            Model((*models.GooglePlayConnection)(nil)).
            Where("user_id = ?", userID).
            ColumnExpr("count(*)").
            Scan(ctx, &count)
        hasConnection = count > 0
    }

    // Latest deployment for this platform.
    var latestDep models.Deployment
    err := c.DB.DB.NewSelect().
        Model(&latestDep).
        Where("project_id = ?", v1ProjectID).
        Where("? = ANY(platforms)", platform).
        Order("created_at DESC").
        Limit(1).
        Scan(ctx)

    if err != nil {
        // No deployment yet — are there missing pre-reqs?
        missing := []string{}
        if !hasConnection {
            switch platform {
            case "ios":
                missing = append(missing, "Apple Developer account")
            case "android":
                missing = append(missing, "Google Play account")
            }
        }
        tile.MissingItems = missing
        if hasConnection {
            tile.Status = "ready_to_ship"
            tile.PrimaryAction = "Start setup"
        } else {
            tile.Status = "not_configured"
            tile.PrimaryAction = "Start setup"
        }
        return tile
    }

    // A deployment exists — map its status to the tile status.
    switch latestDep.Status {
    case "Deployed", "live", "published":
        tile.Status = "live"
        tile.PrimaryAction = "Submit update"
    case "reviewing", "submitted":
        tile.Status = "in_review"
        tile.PrimaryAction = "View listing"
    case "error", "rejected":
        tile.Status = "rejected"
        tile.PrimaryAction = "Fix"
    case "queued", "building":
        tile.Status = "submitted"
        tile.PrimaryAction = "View listing"
    default:
        tile.Status = "submitted"
        tile.PrimaryAction = "View listing"
    }

    // Determine the user-vs-managed track.
    if latestDep.Method == "wemob" {
        tile.ConnectionKind = "managed"
    } else {
        tile.ConnectionKind = "user"
    }

    return tile
}
```

Verify the status strings against what the existing Deployment model actually stores. Look at `internal/models/deployment.go` and `internal/controllers/deployment.go` for the real vocabulary. The plan's mapping above is a best-effort; adapt to reality.

- [ ] **Step 6: Compute the Hero**

At the bottom of `GetLaunchpad` (before the `return`):

```go
resp.HeroStatus = computeHero(resp)
```

Add a package-private helper:

```go
func computeHero(r *responses.LaunchpadResponse) responses.LaunchpadHero {
    // Priority ladder: biggest blocker wins the hero slot.

    // 1. App Store rejected → fix.
    if r.Stores.AppStore.Status == "rejected" {
        return responses.LaunchpadHero{
            Summary:     "App Store rejected the last submission.",
            PrimaryCTA:  "Fix App Store rejection",
            PrimaryHref: "#app-store",
        }
    }
    // 2. Play Store rejected → fix.
    if r.Stores.PlayStore.Status == "rejected" {
        return responses.LaunchpadHero{
            Summary:     "Play Store rejected the last submission.",
            PrimaryCTA:  "Fix Play Store rejection",
            PrimaryHref: "#play-store",
        }
    }
    // 3. Either store needs setup and the other is live → finish the gap.
    if r.Stores.PlayStore.Status == "live" && r.Stores.AppStore.Status == "not_configured" {
        return responses.LaunchpadHero{
            Summary:    "Live on Play Store — App Store not yet configured.",
            PrimaryCTA: "Start App Store setup",
            PrimaryHref: "#app-store",
        }
    }
    if r.Stores.AppStore.Status == "live" && r.Stores.PlayStore.Status == "not_configured" {
        return responses.LaunchpadHero{
            Summary:    "Live on App Store — Play Store not yet configured.",
            PrimaryCTA: "Start Play Store setup",
            PrimaryHref: "#play-store",
        }
    }
    // 4. Both live.
    if r.Stores.PlayStore.Status == "live" && r.Stores.AppStore.Status == "live" {
        return responses.LaunchpadHero{
            Summary:    "Live on both stores.",
            PrimaryCTA: "View listings",
            PrimaryHref: "#stores",
        }
    }
    // 5. No web preview yet → deploy it first.
    if r.WebPreview.Status == "none" {
        return responses.LaunchpadHero{
            Summary:    "Deploy the web preview to unlock mobile builds.",
            PrimaryCTA: "Deploy web preview",
            PrimaryHref: "#web",
        }
    }
    // 6. Default — ready to ship.
    return responses.LaunchpadHero{
        Summary:    "Ready to ship. Pick a store below.",
        PrimaryCTA: "",
        PrimaryHref: "",
    }
}
```

Add `"time"` to the imports at the top (used in Step 4) if not already present.

- [ ] **Step 7: Verify compile**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
go build ./...
go vet ./...
```

Clean.

- [ ] **Step 8: Commit**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
git add internal/controllers/launchpad_controller.go
git commit -m "feat(ship): populate Launchpad with real Hero + WebPreview + Stores

Reads v1_project_id + build_defaults from AppProject.settings (Phase 1
backfill), resolves the latest Build for version info, looks up the
current web preview deploy status, and computes per-store tiles
(Play Store, App Store) from deployments + connection presence.

The Hero summary + primary CTA is computed from a priority ladder
(rejections first, gaps between stores next, default 'ready to ship')."
```

---

## Task 4: Add HTTP handler + route

**Why:** Wire the controller into the HTTP router.

**Files:**
- Create: `internal/views/launchpad_views.go`
- Modify: `cmd/main.go`

- [ ] **Step 1: Create the handler file**

Write `/Users/knid/Projects/wemob/wemob-backend/internal/views/launchpad_views.go`:

```go
package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/utils"
)

// GetLaunchpad handles GET /v1/apps/{appId}/launchpad. Returns the
// single aggregate response powering the Ship tab's Launchpad surface.
func (v *HttpViews) GetLaunchpad(w http.ResponseWriter, r *http.Request) {
	token, err := utils.ExtractTokenFromHeader(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}
	user, err := v.Controller.GetUser(token)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appIDStr := chi.URLParam(r, "appId")
	if appIDStr == "" {
		utils.JSONErrorResponse(w, "app_id is required", http.StatusBadRequest)
		return
	}
	appID, err := uuid.Parse(appIDStr)
	if err != nil {
		utils.JSONErrorResponse(w, "invalid app_id", http.StatusBadRequest)
		return
	}

	resp, err := v.Controller.GetLaunchpad(r.Context(), user, appID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}
	utils.JSONResponse(w, resp, http.StatusOK)
}
```

Match the existing pattern in `internal/views/*.go` — if handlers use a different helper for token extraction or error responses, use that.

- [ ] **Step 2: Register the route**

Open `/Users/knid/Projects/wemob/wemob-backend/cmd/main.go`. Find the route registration block that handles `/v1/apps/{appId}/...` or similar — if there isn't one yet, find where other `/v1/*` routes live and add a sibling block:

```go
r.Route("/apps", func(r chi.Router) {
    r.Route("/{appId}", func(r chi.Router) {
        r.Get("/launchpad", httpViews.GetLaunchpad)
    })
})
```

If the existing `/v1` block already has an `/apps/{appId}` subtree, add `r.Get("/launchpad", httpViews.GetLaunchpad)` inside it.

- [ ] **Step 3: Verify compile**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
go build ./...
go vet ./...
```

Clean.

- [ ] **Step 4: Commit**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
git add internal/views/launchpad_views.go cmd/main.go
git commit -m "feat(ship): wire GET /v1/apps/:appId/launchpad HTTP route

Extracts the bearer token, resolves the user, parses the app UUID, and
delegates to Controller.GetLaunchpad. Returns 401 on auth failure, 400
on bad UUID, 404 on unknown app / ownership mismatch, and the
LaunchpadResponse JSON on success."
```

---

## Task 5: Backend smoke — inspect an app row and verify the aggregate

**Why:** Prove the endpoint renders correctly for an existing app before the frontend consumes it.

- [ ] **Step 1: Pick a real AppProject to test against**

Use the Postgres MCP (`mcp__postgres__execute_sql`):

```sql
SELECT
    id,
    name,
    slug,
    settings ? 'v1_project_id' AS has_v1,
    settings ? 'build_defaults' AS has_defaults
FROM app_projects
WHERE settings ? 'v1_project_id'
  AND settings ? 'build_defaults'
LIMIT 3;
```

Pick one — call its id `$APP_ID`.

- [ ] **Step 2: Ask the user to run the server and hit the endpoint**

The sandbox can't boot the backend (Infisical creds absent). Leave the verification as a printed instruction for the user to run manually:

```
# In the user's terminal:
cd /Users/knid/Projects/wemob/wemob-backend
docker-compose -f docker-compose-dev.yaml up -d
go run cmd/main.go &
sleep 6

TOKEN="7a72ce76171a432d511aa5351b91c6c4e674e01c2ce1798b372b1ad42e6444ae"
APP_ID="<paste one from Step 1>"

curl -s -H "Authorization: Bearer $TOKEN" \
    "http://localhost:8001/v1/apps/$APP_ID/launchpad" | jq .
```

Expected: a JSON object with `app`, `hero_status`, `web_preview`, `stores`, and zero `listing` / `builds` / `identity` (these are 2b). The `app.name` should match the project name in the DB; `app.bundle_id` should be `com.wemob.<slug>`; `stores.play_store` and `stores.app_store` should have plausible status values.

- [ ] **Step 3: No separate commit — this is a runtime verification, logged in the final Phase 2a summary (Task 11).**

---

## Task 6: Add API client method + shared TS types

**Why:** Give the frontend a typed way to call the new endpoint.

**Repo:** `/Users/knid/Projects/wemob/wemob-app`
**Branch:** `feat/ship-ux-phase-2a` (create it — branched from `main`)

**Files:**
- Create: `src/lib/ship/launchpad-types.ts`
- Modify: `src/lib/api.ts`

- [ ] **Step 1: Create the branch**

```bash
cd /Users/knid/Projects/wemob/wemob-app
git checkout main
git checkout -b feat/ship-ux-phase-2a
```

- [ ] **Step 2: Write the shared types**

Create `src/lib/ship/launchpad-types.ts`:

```ts
// Launchpad response types — mirror of backend LaunchpadResponse in
// internal/responses/launchpad.go. Keep the two in sync manually until
// we codegen from OpenAPI.

export type StoreStatus =
  | "not_configured"
  | "configuring"
  | "ready_to_ship"
  | "building"
  | "needs_review"
  | "submitted"
  | "in_review"
  | "live"
  | "update_available"
  | "rejected"
  | "failed";

export type WebPreviewStatus = "deployed" | "building" | "failed" | "none";

export interface LaunchpadApp {
  id: string;
  name: string;
  bundle_id: string;
  icon_s3_key: string;
  version_name: string;
  build_number: number;
}

export interface LaunchpadHero {
  summary: string;
  primary_cta: string;
  primary_href: string;
}

export interface LaunchpadWebPreview {
  status: WebPreviewStatus;
  url?: string;
  deployed_at?: string;
}

export interface LaunchpadStoreTile {
  status: StoreStatus;
  live_version?: string;
  live_build_num?: number;
  install_count?: number;
  rating?: number;
  missing_items?: string[];
  primary_action?: string;
  connection_kind?: "managed" | "user" | "";
}

export interface LaunchpadStores {
  play_store: LaunchpadStoreTile;
  app_store: LaunchpadStoreTile;
}

export interface LaunchpadListing {
  ready_items: number;
  total_items: number;
}

export interface LaunchpadBuildSummary {
  id: string;
  platform: "android" | "ios";
  status: string;
  version?: string;
  created_at: string;
}

export interface LaunchpadBuilds {
  active: LaunchpadBuildSummary[];
  recent: LaunchpadBuildSummary[];
}

export interface LaunchpadIdentity {
  bundle_id: string;
  version: string;
  permissions: string[];
}

export interface LaunchpadResponse {
  app: LaunchpadApp;
  hero_status: LaunchpadHero;
  web_preview: LaunchpadWebPreview;
  stores: LaunchpadStores;
  listing: LaunchpadListing;
  builds: LaunchpadBuilds;
  identity: LaunchpadIdentity;
}
```

- [ ] **Step 3: Add the API client method**

Open `src/lib/api.ts`. Find a location near `getApp` (around line 2133 per earlier scan) and add:

```ts
async getLaunchpad(appId: string): Promise<import("./ship/launchpad-types").LaunchpadResponse> {
  const res = await fetch(`${this.baseUrl}/v1/apps/${appId}/launchpad`, {
    headers: this.authHeaders(),
  });
  if (!res.ok) {
    throw new Error(`getLaunchpad ${appId}: ${res.status} ${await res.text()}`);
  }
  return res.json();
}
```

If the existing ApiClient uses a different fetch helper (e.g., `this.fetchJson`) or a different auth header pattern, match that. Open the file to check how `getApp` is structured and mirror it.

- [ ] **Step 4: Verify lint + build**

```bash
cd /Users/knid/Projects/wemob/wemob-app
npm run lint
npm run build
```

Both must succeed.

- [ ] **Step 5: Commit**

```bash
cd /Users/knid/Projects/wemob/wemob-app
git add src/lib/ship/launchpad-types.ts src/lib/api.ts
git commit -m "feat(ship): add Launchpad types + apiClient.getLaunchpad

Mirrors the backend LaunchpadResponse shape in src/lib/ship/launchpad-types.ts
and adds getLaunchpad(appId) to the ApiClient. Used by the new
/apps/:appId/ship route in Task 8."
```

---

## Task 7: Add `useFeatureFlag` hook

**Why:** A single helper used by the `/ship` route and every subsequent Phase 2b–5 surface to check PostHog flags without re-implementing the pattern.

**Files:**
- Create: `src/hooks/use-feature-flag.ts`

- [ ] **Step 1: Write the hook**

```ts
"use client";

import { useEffect, useState } from "react";
import posthog from "posthog-js";

/**
 * useFeatureFlag returns whether the given PostHog feature flag is
 * enabled for the current user.
 *
 * The initial render returns `undefined` so the component can show a
 * loading placeholder (avoiding a FOUC where flag-gated UI flashes off
 * then on). Once PostHog responds, the value flips to boolean.
 *
 * If PostHog isn't initialized (e.g., CI / tests), returns false
 * deterministically.
 */
export function useFeatureFlag(flag: string): boolean | undefined {
  const [enabled, setEnabled] = useState<boolean | undefined>(undefined);

  useEffect(() => {
    if (typeof window === "undefined") return;

    // posthog.isFeatureEnabled returns boolean | undefined. We wait
    // until the flags are loaded before resolving.
    let cancelled = false;
    const check = () => {
      if (cancelled) return;
      const v = posthog.isFeatureEnabled(flag);
      if (v === undefined) {
        // Flags not loaded yet — retry on next flags-loaded callback.
        return;
      }
      setEnabled(!!v);
    };

    posthog.onFeatureFlags(check);
    check(); // in case flags are already loaded

    return () => {
      cancelled = true;
    };
  }, [flag]);

  return enabled;
}
```

- [ ] **Step 2: Verify lint**

```bash
cd /Users/knid/Projects/wemob/wemob-app
npm run lint
```

Clean.

- [ ] **Step 3: Commit**

```bash
cd /Users/knid/Projects/wemob/wemob-app
git add src/hooks/use-feature-flag.ts
git commit -m "feat(ship): add useFeatureFlag hook

Thin wrapper around posthog-js isFeatureEnabled that returns undefined
while flags load and boolean once they do. Used by the new /ship route
in Task 8 to gate behind new_ship_ux."
```

---

## Task 8: Add `/apps/:appId/ship` route with feature-flag gate

**Why:** Create the new route under the flag, without replacing the existing `/publish` route.

**Files:**
- Create: `src/app/apps/[appId]/ship/page.tsx`

- [ ] **Step 1: Write the page**

```tsx
"use client";

import { useEffect, useState } from "react";
import { notFound, useParams, useRouter } from "next/navigation";
import { AuthGuard } from "@/components/auth/auth-guard";
import { StudioShell } from "@/components/layout/studio-shell";
import { Launchpad } from "@/components/ship/launchpad";
import { useFeatureFlag } from "@/hooks/use-feature-flag";
import { apiClient } from "@/lib/api";
import type { LaunchpadResponse } from "@/lib/ship/launchpad-types";

export default function ShipPage() {
  const params = useParams<{ appId: string }>();
  const appId = params?.appId;

  return (
    <AuthGuard>
      <StudioShell>{appId ? <ShipLoader appId={appId} /> : null}</StudioShell>
    </AuthGuard>
  );
}

function ShipLoader({ appId }: { appId: string }) {
  const router = useRouter();
  const flagEnabled = useFeatureFlag("new_ship_ux");
  const [data, setData] = useState<LaunchpadResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);

  useEffect(() => {
    if (flagEnabled === false) {
      // Flag off for this user — bounce to legacy publish route.
      router.replace(`/apps/${appId}/publish`);
    }
  }, [flagEnabled, appId, router]);

  useEffect(() => {
    if (flagEnabled !== true) return; // wait until flag confirmed on
    let cancelled = false;
    setLoading(true);
    apiClient
      .getLaunchpad(appId)
      .then((res) => {
        if (!cancelled) setData(res);
      })
      .catch((err) => {
        if (!cancelled) setError(err instanceof Error ? err : new Error(String(err)));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [appId, flagEnabled]);

  if (flagEnabled === undefined) {
    return (
      <div className="flex-1 flex items-center justify-center text-[13px] text-[var(--ink-faint)]">
        Loading…
      </div>
    );
  }

  if (flagEnabled === false) {
    // Redirect pending — render nothing in the flash.
    return null;
  }

  if (loading) {
    return (
      <div className="flex-1 flex items-center justify-center text-[13px] text-[var(--ink-faint)]">
        Loading ship surface…
      </div>
    );
  }

  if (error || !data) {
    if (error?.message?.includes("404")) notFound();
    return (
      <div className="flex-1 flex items-center justify-center">
        <div className="text-[13px] text-[var(--accent)] max-w-md text-center">
          Can't load this app.
          {error && <div className="mt-2 text-[var(--ink-muted)]">{error.message}</div>}
        </div>
      </div>
    );
  }

  return <Launchpad data={data} />;
}
```

- [ ] **Step 2: Verify lint**

```bash
cd /Users/knid/Projects/wemob/wemob-app
npm run lint
```

Will likely flag the missing `Launchpad` component import (Task 9 creates it). If so, proceed to Task 9 first and come back. Easier: do Task 9 first, then lint passes in Task 8.

Actually — let this step go through Task 9 as well. Only commit after Task 9 is done. Update Task 8 commit in the later step.

- [ ] **Step 3: Don't commit yet — proceed to Task 9.**

---

## Task 9: Create `Launchpad` container + `LaunchpadHeader`

**Why:** Render the status hero from real data.

**Files:**
- Create: `src/components/ship/launchpad.tsx`
- Create: `src/components/ship/launchpad-header.tsx`

- [ ] **Step 1: Write the container**

`src/components/ship/launchpad.tsx`:

```tsx
"use client";

import type { LaunchpadResponse } from "@/lib/ship/launchpad-types";
import { LaunchpadHeader } from "./launchpad-header";
import { StoreTile } from "./store-tile";

interface LaunchpadProps {
  data: LaunchpadResponse;
}

export function Launchpad({ data }: LaunchpadProps) {
  return (
    <div className="flex-1 overflow-y-auto">
      <div className="px-8 py-6 pb-16 max-w-[880px] mx-auto">
        <LaunchpadHeader data={data} />

        <section className="mt-6">
          <div className="text-[10px] uppercase tracking-[1px] text-[var(--ink-faint)] mb-2">
            Stores
          </div>
          <div className="grid grid-cols-2 gap-2.5">
            <StoreTile kind="play_store" tile={data.stores.play_store} />
            <StoreTile kind="app_store" tile={data.stores.app_store} />
          </div>
        </section>

        {/* Phase 2b zones — placeholders for now. */}
        <ZonePlaceholder title="Listing" />
        <ZonePlaceholder title="Builds" />
        <ZonePlaceholder title="Identity" />
      </div>
    </div>
  );
}

function ZonePlaceholder({ title }: { title: string }) {
  return (
    <section className="mt-6 border border-dashed border-[var(--border)] rounded-xl p-6 text-center">
      <div className="text-[10px] uppercase tracking-[1px] text-[var(--ink-faint)] mb-1.5">
        {title}
      </div>
      <div className="text-[12px] text-[var(--ink-muted)]">
        Coming in Phase 2b
      </div>
    </section>
  );
}
```

- [ ] **Step 2: Write the header**

`src/components/ship/launchpad-header.tsx`:

```tsx
"use client";

import type { LaunchpadResponse } from "@/lib/ship/launchpad-types";

interface LaunchpadHeaderProps {
  data: LaunchpadResponse;
}

export function LaunchpadHeader({ data }: LaunchpadHeaderProps) {
  const { app, hero_status } = data;
  return (
    <header className="rounded-2xl bg-gradient-to-b from-[var(--bg-surface)] to-[var(--bg)] border border-[var(--border)] px-6 py-5">
      <div className="flex items-center gap-3.5 mb-3.5">
        <div className="w-12 h-12 rounded-xl bg-[var(--accent)] inline-flex items-center justify-center font-serif text-white text-lg tracking-tight">
          {app.name.charAt(0).toUpperCase()}
        </div>
        <div className="flex-1 min-w-0">
          <div className="font-serif text-[22px] tracking-tight truncate">
            {app.name}
          </div>
          <div className="font-mono text-[11px] text-[var(--ink-faint)] truncate">
            {app.bundle_id || "—"}
            {app.version_name && <> · v{app.version_name}</>}
            {app.build_number > 0 && <> (Build {app.build_number})</>}
          </div>
        </div>
      </div>

      {hero_status.summary && (
        <div className="text-[13px] text-[var(--ink)] leading-[1.5] mb-3.5">
          {hero_status.summary}
        </div>
      )}

      {hero_status.primary_cta && (
        <a
          href={hero_status.primary_href || "#"}
          className="inline-flex items-center gap-2 px-3.5 py-2 bg-[var(--accent)] text-white rounded-lg text-[12px] font-medium hover:bg-[var(--accent-hover)] transition-colors"
        >
          {hero_status.primary_cta} →
        </a>
      )}
    </header>
  );
}
```

- [ ] **Step 3: Don't commit yet — proceed to Task 10.**

---

## Task 10: Create `StoreTile` component

**Files:**
- Create: `src/components/ship/store-tile.tsx`

- [ ] **Step 1: Write the component**

```tsx
"use client";

import type { LaunchpadStoreTile } from "@/lib/ship/launchpad-types";

interface StoreTileProps {
  kind: "play_store" | "app_store";
  tile: LaunchpadStoreTile;
}

const LABEL: Record<"play_store" | "app_store", string> = {
  play_store: "Play Store",
  app_store: "App Store",
};

const DOT_COLOR: Record<"play_store" | "app_store", string> = {
  play_store: "#01875f",
  app_store: "#000",
};

export function StoreTile({ kind, tile }: StoreTileProps) {
  const statusLabel = humanStatus(tile.status);
  const statusKind = statusColorKind(tile.status);
  return (
    <div className="border border-[var(--border)] rounded-xl p-3.5 bg-[var(--bg-surface)]">
      <div className="flex items-center gap-2 mb-2.5">
        <div
          className="w-5 h-5 rounded"
          style={{ background: DOT_COLOR[kind] }}
          aria-hidden
        />
        <div className="text-[13px] font-medium">{LABEL[kind]}</div>
        <div
          className={`ml-auto text-[10px] px-2 py-0.5 rounded-full font-medium whitespace-nowrap ${statusBadgeClasses(statusKind)}`}
        >
          {statusLabel}
        </div>
      </div>

      {tile.live_version && (
        <div className="font-mono text-[11px] text-[var(--ink-muted)] mb-1">
          v{tile.live_version}
          {typeof tile.live_build_num === "number" && tile.live_build_num > 0 && (
            <> (Build {tile.live_build_num})</>
          )}
        </div>
      )}

      {tile.install_count !== undefined && tile.install_count > 0 && (
        <div className="text-[12px] text-[var(--ink)] mb-2.5">
          {tile.install_count.toLocaleString()} installs
          {tile.rating !== undefined && tile.rating > 0 && (
            <> · {tile.rating.toFixed(1)}★</>
          )}
        </div>
      )}

      {tile.missing_items && tile.missing_items.length > 0 && (
        <ul className="text-[11px] text-[var(--ink-muted)] pl-4 list-disc leading-[1.55] mb-2.5">
          {tile.missing_items.map((item) => (
            <li key={item}>{item}</li>
          ))}
        </ul>
      )}

      {tile.primary_action && (
        <button
          type="button"
          className="px-3 py-1.5 bg-[var(--ink)] text-white rounded-md text-[11px] font-medium hover:opacity-90"
        >
          {tile.primary_action}
        </button>
      )}
    </div>
  );
}

function humanStatus(s: LaunchpadStoreTile["status"]): string {
  switch (s) {
    case "not_configured":
      return "Needs setup";
    case "configuring":
      return "Configuring";
    case "ready_to_ship":
      return "Ready";
    case "building":
      return "Building";
    case "needs_review":
      return "Needs review";
    case "submitted":
      return "Submitted";
    case "in_review":
      return "In review";
    case "live":
      return "Live";
    case "update_available":
      return "Update ready";
    case "rejected":
      return "Rejected";
    case "failed":
      return "Failed";
    default:
      return s;
  }
}

function statusColorKind(s: LaunchpadStoreTile["status"]): "success" | "info" | "warn" | "danger" | "neutral" {
  switch (s) {
    case "live":
      return "success";
    case "building":
    case "submitted":
    case "in_review":
    case "configuring":
      return "info";
    case "not_configured":
    case "needs_review":
    case "update_available":
      return "warn";
    case "rejected":
    case "failed":
      return "danger";
    default:
      return "neutral";
  }
}

function statusBadgeClasses(kind: "success" | "info" | "warn" | "danger" | "neutral"): string {
  switch (kind) {
    case "success":
      return "bg-[rgba(122,155,118,0.12)] text-[var(--success)]";
    case "info":
      return "bg-[rgba(107,140,174,0.14)] text-[#3d6890]";
    case "warn":
      return "bg-[rgba(227,176,75,0.16)] text-[#8b6520]";
    case "danger":
      return "bg-[var(--accent-soft)] text-[var(--accent)]";
    case "neutral":
      return "bg-[var(--bg-raised)] text-[var(--ink-faint)] border border-[var(--border)]";
  }
}
```

- [ ] **Step 2: Verify lint + build**

```bash
cd /Users/knid/Projects/wemob/wemob-app
npm run lint
npm run build
```

Both must succeed.

- [ ] **Step 3: Commit Task 8 + 9 + 10 together**

Three tasks' output commit as one coherent unit — the route, the Launchpad container with a stubbed 2b zones, the header, and the store tile all hang together as "the 2a surface" and are not usable individually:

```bash
cd /Users/knid/Projects/wemob/wemob-app
git add src/app/apps/\[appId\]/ship/page.tsx src/components/ship/
git commit -m "feat(ship): add /apps/:appId/ship route with Launchpad skeleton

New Next.js app-router route gated behind the new_ship_ux PostHog flag.
When the flag is on, loads the /v1/apps/:id/launchpad aggregate and
renders:

- Status hero: app icon + name + bundle ID + version + one-sentence
  summary and primary CTA computed by the backend.
- Stores grid: Play Store + App Store tiles with live status, version,
  install count, missing-items list, and primary action.
- Listing / Builds / Identity zones rendered as 'Coming in Phase 2b'
  placeholders so the route is openable while the feature ladder ramps.

Users without the flag are redirected back to /apps/:id/publish."
```

---

## Task 11: Final integration smoke

**Why:** Verify the feature flag path end-to-end.

- [ ] **Step 1: User sets up the PostHog flag**

The `new_ship_ux` feature flag must exist in PostHog. Instruct the user (in the final summary) to:

1. Open PostHog for the Wemob project.
2. Create a boolean feature flag named `new_ship_ux`.
3. Set the rollout to either "100% of users" (for self-dogfooding) or a specific user filter (e.g., the current user's email) to test.

- [ ] **Step 2: User runs the backend + frontend locally**

Provided as a documented step:

```
# Terminal 1 — backend
cd /Users/knid/Projects/wemob/wemob-backend
docker-compose -f docker-compose-dev.yaml up -d
go run cmd/main.go

# Terminal 2 — frontend
cd /Users/knid/Projects/wemob/wemob-app
npm run dev

# Browser: open http://localhost:3000/apps/<appId>/ship
# Expected:
#   - If flag off for user: redirected to /apps/<appId>/publish (existing Mission Control)
#   - If flag on: Launchpad renders with app name, bundle id, version, hero summary, and two store tiles.
#     The three stub zones show "Coming in Phase 2b".
```

- [ ] **Step 3: No code change here — this is the final hand-off step.**

---

## Phase 2a completion criteria

Phase 2a is done when all of the following are true:

- `go build ./...` + `go vet ./...` clean on wemob-backend `feat/ship-ux-phase-2a`.
- `npm run lint` + `npm run build` clean on wemob-app `feat/ship-ux-phase-2a`.
- `GET /v1/apps/:appId/launchpad` returns a populated JSON object for an existing app whose owner matches the bearer token.
- `new_ship_ux` feature flag gates `/apps/:appId/ship` — off → redirect to `/publish`, on → render Launchpad with real data.
- Status hero shows the backend-computed summary and primary CTA.
- Play Store and App Store tiles render per-store status and action with correct visual state.
- Listing / Builds / Identity zones render as explicit 2b placeholders.
- Existing `/apps/:id/publish` continues to work unchanged for users who don't have the flag.
- Zero regressions in `/builds`, `/credentials`, Studio → Deploy tab, or any other existing surface.

Phase 2b (full Listing tiles, Builds feed, Identity panel, one-click New build) is unblocked.

## Out of scope (2b)

- Listing tile grid (icon / title / description / screenshots / privacy / release notes).
- Builds feed with stage ladder + ETAs + annotated failures.
- Identity panel (bundle ID, version, permissions).
- One-click "New build" button using project build_defaults.
- ⌘K command palette.
- `/ship` cross-app table.
- Deleting the legacy `/publish` route and `/builds`, `/credentials` pages.
