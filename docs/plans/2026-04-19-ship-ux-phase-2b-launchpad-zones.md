# Ship UX — Phase 2b · Launchpad Remaining Zones Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fill the three Launchpad zones stubbed in Phase 2a — Listing tile grid, Builds feed with stage ladder + annotated failures, Identity panel — and wire a one-click "New build" CTA that uses each project's `build_defaults`. Frontend consumes existing backend build-trigger endpoint; no new build endpoints in 2b. The `new_ship_ux` feature flag continues to gate the entire `/apps/:appId/ship` surface.

**Architecture:** Backend extends the existing `GetLaunchpad` aggregate to populate `Listing`, `Builds`, and `Identity`. Frontend replaces the three `ZonePlaceholder`s with real components, each reading from the populated response. "New build" is a frontend-orchestrated call to the existing `POST /v1/projects/{v1ProjectId}/builds` endpoint using the project's defaults, followed by a Launchpad refetch.

**Tech stack:** Go 1.22+, Bun ORM, chi/v5. Next.js 16, React 19, Tailwind 4, posthog-js. No new dependencies.

**Design spec:** `docs/design/2026-04-18-wemob-publish-ux-redesign-design.md` (Launchpad — five zones).

---

## Test strategy

Same as Phase 2a: `go build`/`go vet` clean on backend, `npm run lint`/`npm run build` clean on frontend, Postgres MCP for data, manual browser smoke for runtime behavior (user runs this).

## File structure overview

### Backend (wemob-backend) — modified

```
internal/responses/launchpad.go              — extend Listing (tiles) + LaunchpadApp (build_defaults echo)
internal/controllers/launchpad_controller.go — fill Listing, Builds, Identity; buildListing, buildBuilds,
                                                buildIdentity helpers
```

No new backend files. No new endpoints.

### Frontend (wemob-app) — new files

```
src/components/ship/listing-grid.tsx         — 4×2 tiles grid
src/components/ship/listing-tile.tsx         — individual tile variant
src/components/ship/builds-feed.tsx          — active + recent build entries
src/components/ship/build-stage-ladder.tsx   — preparing → building → uploading → built (→ signing → signed)
src/components/ship/annotated-failure.tsx    — failed-build inline renderer
src/components/ship/identity-panel.tsx       — collapsed one-liner with expand
src/components/ship/new-build-button.tsx     — one-click trigger using project defaults
src/hooks/use-launchpad.ts                   — fetches + exposes refetch for the page
```

### Frontend (wemob-app) — modified

```
src/components/ship/launchpad.tsx            — replace ZonePlaceholder with the three real zones +
                                                host the NewBuildButton
src/app/apps/[appId]/ship/page.tsx           — use useLaunchpad hook instead of inline fetch
src/lib/ship/launchpad-types.ts              — add BuildStage enum + tile detail types
src/lib/api.ts                               — add apiClient.triggerQuickBuild helper
```

## Why these boundaries

- **No new backend endpoints.** The existing `POST /v1/projects/{v1ProjectId}/builds` already accepts app_name / icon / splash / version overrides; we just pass them from `LaunchpadApp.build_defaults` in the frontend. Less surface, less risk.
- **Build-stage ladder as its own component.** It's shared between the active build banner (Builds feed top) and the per-build history row, so pulling it out avoids duplication from day one.
- **Annotated failure as its own component.** It reads `error_message` + `annotation_id` → human message + suggested fix. This will grow in Phase 4 (retry-with-fix button, "Show full log" toggle), so a focused component now pays off.
- **`useLaunchpad` hook.** Phase 2a fetched inline in the page. 2b needs refetch on build-triggered + polling during an active build, so centralize into a hook.

---

## Task 1: Extend `LaunchpadResponse.Listing` with per-tile detail; add `BuildDefaults` echo

**Why:** Give the frontend everything it needs to render 8 tiles and trigger a build with project defaults.

**Files:**
- Modify: `internal/responses/launchpad.go`

- [ ] **Step 1: Extend the `LaunchpadListing` struct**

Replace the existing `LaunchpadListing` stub (from Phase 2a) with:

```go
// LaunchpadListing summarises the store-listing readiness. Each tile
// is one piece of metadata the stores need (icon, title, description,
// screenshots, locale, category, privacy URL, release notes).
type LaunchpadListing struct {
	ReadyItems int                    `json:"ready_items"` // tiles with status "ready"
	TotalItems int                    `json:"total_items"` // always 8 in 2b
	Tiles      []LaunchpadListingTile `json:"tiles"`
}

// LaunchpadListingTile is one of the 8 listing items. Status vocabulary:
// "ready" (green check), "warn" (yellow, needed but not blocking),
// "missing" (amber dashed, blocking), "empty" (grey, optional).
type LaunchpadListingTile struct {
	Key         string `json:"key"`          // "icon" | "title" | "description" | "screenshots"
	                                          // | "locale" | "category" | "privacy_url" | "release_notes"
	Label       string `json:"label"`        // "Icon", "Title", "Description", …
	Status      string `json:"status"`       // "ready" | "warn" | "missing" | "empty"
	Summary     string `json:"summary"`      // e.g. "TaskTracker Pro" | "14 chars" | "1 of 3 needed"
	Remediation string `json:"remediation"`  // optional — "Generate with AI", empty if not applicable
}
```

- [ ] **Step 2: Extend `LaunchpadApp` with `BuildDefaults` echo**

Add a field after `BuildNumber`:

```go
// BuildDefaults echoes AppProject.settings.build_defaults so the
// frontend can compose a "New build" request without a second API
// round-trip. Backfilled by Phase 1's migration; may be empty on
// projects that have never built.
BuildDefaults LaunchpadBuildDefaults `json:"build_defaults"`
```

And add the type below `LaunchpadApp`:

```go
type LaunchpadBuildDefaults struct {
	AppName              string `json:"app_name,omitempty"`
	IconS3Key            string `json:"icon_s3_key,omitempty"`
	SplashColor          string `json:"splash_color,omitempty"`
	NavBarStyle          string `json:"navbar_style,omitempty"`
	RemoveWemobBranding  bool   `json:"remove_wemob_branding,omitempty"`
}
```

- [ ] **Step 3: Extend `LaunchpadBuilds` with real entries**

`LaunchpadBuildSummary` was declared in 2a. Extend it to carry everything the UI needs:

```go
type LaunchpadBuildSummary struct {
	ID              string `json:"id"`
	BuildID         string `json:"build_id"`        // v1 Build id — used for download
	Platform        string `json:"platform"`        // "android" | "ios"
	Status          string `json:"status"`          // from AppBuildTask
	StatusText      string `json:"status_text,omitempty"`
	Finished        bool   `json:"finished"`
	Version         string `json:"version,omitempty"`
	BuildNumber     int    `json:"build_number,omitempty"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at,omitempty"`
	ErrorMessage    string `json:"error_message,omitempty"`
	AnnotationID    string `json:"annotation_id,omitempty"`     // UUID string or empty
	AnnotationName  string `json:"annotation_name,omitempty"`   // for immediate rendering without lookup
	AnnotationFix   string `json:"annotation_suggested_fix,omitempty"`
	AnnotationMsg   string `json:"annotation_human_message,omitempty"`
}
```

- [ ] **Step 4: Extend `LaunchpadIdentity` (no change — spec already has it)**

Keep existing fields. Permissions will come from `MobileAppConfig.Permissions`.

- [ ] **Step 5: Verify compile**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
go build ./...
go vet ./...
```

Both clean.

- [ ] **Step 6: Commit**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
git add internal/responses/launchpad.go
git commit -m "feat(ship): extend LaunchpadResponse with 2b fields

Adds:
- LaunchpadApp.build_defaults (echoes AppProject.settings.build_defaults)
- LaunchpadListing.tiles (8 per-field tile states)
- LaunchpadBuildSummary flattens annotation fields for zero-round-trip
  rendering"
```

---

## Task 2: Fill `Listing` in `GetLaunchpad`

**Why:** Render real tile-by-tile readiness from `AppStoreListing` + `MobileAppConfig` state.

**Files:**
- Modify: `internal/controllers/launchpad_controller.go`

- [ ] **Step 1: Inspect AppStoreListing + MobileAppConfig models**

Read `internal/models/app_store_listing.go` and `internal/models/mobile_app_config.go`. Note which fields feed which tiles:

- `icon` tile ← `AppStoreListing.IconS3Key`
- `title` tile ← `AppStoreListing.Title`
- `description` tile ← `AppStoreListing.Description`
- `screenshots` tile ← `AppStoreListing.Screenshots` (array length)
- `locale` tile ← `AppStoreListing.Locale` (always ready — has a default)
- `privacy_url` tile ← `AppStoreListing.PrivacyPolicyURL`
- `category` tile ← no field exists yet — mark as `empty` with summary "Set in store console"
- `release_notes` tile ← no persistent field yet — mark as `empty` with summary "Drafted per build"

For `privacy_url`:
- If any sensitive permission is set on `MobileAppConfig.Permissions` (camera, microphone, photos, location, contacts, bluetooth), the tile is `missing` (blocking) when URL is empty.
- If no sensitive permissions, the tile is `warn` when URL is empty (recommended but optional).

- [ ] **Step 2: Add a `buildListing` helper**

Add to `launchpad_controller.go`:

```go
func (c *Controller) buildListing(ctx context.Context, appProjectID uuid.UUID) responses.LaunchpadListing {
    listing := responses.LaunchpadListing{
        TotalItems: 8,
        Tiles:      []responses.LaunchpadListingTile{},
    }

    // Load the AppStoreListing (may not exist yet — that's fine, all tiles empty).
    var sl models.AppStoreListing
    _ = c.DB.DB.NewSelect().
        Model(&sl).
        Where("app_project_id = ?", appProjectID).
        Scan(ctx)

    // Load the MobileAppConfig to check sensitive permissions.
    var cfg models.MobileAppConfig
    _ = c.DB.DB.NewSelect().
        Model(&cfg).
        Where("project_id = ?", appProjectID).
        Scan(ctx)

    sensitivePerms := map[string]struct{}{
        "camera": {}, "microphone": {}, "photos": {},
        "location": {}, "contacts": {}, "bluetooth": {},
    }
    hasSensitive := false
    for _, p := range cfg.Permissions {
        if _, ok := sensitivePerms[strings.ToLower(p)]; ok {
            hasSensitive = true
            break
        }
    }

    // Icon
    iconTile := responses.LaunchpadListingTile{Key: "icon", Label: "Icon"}
    if sl.IconS3Key != "" {
        iconTile.Status = "ready"
        iconTile.Summary = "1024×1024 PNG"
    } else {
        iconTile.Status = "missing"
        iconTile.Summary = "Required"
        iconTile.Remediation = "Use project icon"
    }
    listing.Tiles = append(listing.Tiles, iconTile)

    // Title
    titleTile := responses.LaunchpadListingTile{Key: "title", Label: "Title"}
    title := strings.TrimSpace(sl.Title)
    switch {
    case len(title) == 0:
        titleTile.Status = "missing"
        titleTile.Summary = "Required"
        titleTile.Remediation = "Use project name"
    case len(title) < 2:
        titleTile.Status = "warn"
        titleTile.Summary = "Too short"
    case len(title) > 30:
        titleTile.Status = "warn"
        titleTile.Summary = fmt.Sprintf("%d chars (max 30)", len(title))
    default:
        titleTile.Status = "ready"
        titleTile.Summary = fmt.Sprintf("%d chars", len(title))
    }
    listing.Tiles = append(listing.Tiles, titleTile)

    // Description
    descTile := responses.LaunchpadListingTile{Key: "description", Label: "Description"}
    desc := strings.TrimSpace(sl.Description)
    switch {
    case len(desc) == 0:
        descTile.Status = "missing"
        descTile.Summary = "Required"
        descTile.Remediation = "Draft with AI"
    case len(desc) < 80:
        descTile.Status = "warn"
        descTile.Summary = fmt.Sprintf("%d chars (min 80)", len(desc))
    case len(desc) < 170:
        descTile.Status = "warn"
        descTile.Summary = fmt.Sprintf("%d chars (170 recommended)", len(desc))
    default:
        descTile.Status = "ready"
        descTile.Summary = fmt.Sprintf("%d chars", len(desc))
    }
    listing.Tiles = append(listing.Tiles, descTile)

    // Screenshots
    ssTile := responses.LaunchpadListingTile{Key: "screenshots", Label: "Screenshots"}
    count := len(sl.Screenshots)
    switch {
    case count == 0:
        ssTile.Status = "missing"
        ssTile.Summary = "3 needed"
        ssTile.Remediation = "Generate with AI"
    case count < 3:
        ssTile.Status = "missing"
        ssTile.Summary = fmt.Sprintf("%d of 3 needed", count)
        ssTile.Remediation = "Generate with AI"
    case count < 5:
        ssTile.Status = "warn"
        ssTile.Summary = fmt.Sprintf("%d uploaded (5 recommended)", count)
    default:
        ssTile.Status = "ready"
        ssTile.Summary = fmt.Sprintf("%d uploaded", count)
    }
    listing.Tiles = append(listing.Tiles, ssTile)

    // Locale — always ready (has default "en-US")
    localeTile := responses.LaunchpadListingTile{
        Key:     "locale",
        Label:   "Locale",
        Status:  "ready",
        Summary: firstNonEmpty(sl.Locale, "en-US"),
    }
    listing.Tiles = append(listing.Tiles, localeTile)

    // Category — no persistent field; empty means "set in store console"
    catTile := responses.LaunchpadListingTile{
        Key:     "category",
        Label:   "Category",
        Status:  "empty",
        Summary: "Set in store console",
    }
    listing.Tiles = append(listing.Tiles, catTile)

    // Privacy URL
    privacyTile := responses.LaunchpadListingTile{Key: "privacy_url", Label: "Privacy URL"}
    privacy := strings.TrimSpace(sl.PrivacyPolicyURL)
    switch {
    case privacy == "" && hasSensitive:
        privacyTile.Status = "missing"
        privacyTile.Summary = "Required (sensitive permissions in use)"
        privacyTile.Remediation = "Generate for me"
    case privacy == "":
        privacyTile.Status = "warn"
        privacyTile.Summary = "Recommended"
    default:
        privacyTile.Status = "ready"
        privacyTile.Summary = privacy
    }
    listing.Tiles = append(listing.Tiles, privacyTile)

    // Release notes — drafted per build; tile always "empty" in 2b
    rnTile := responses.LaunchpadListingTile{
        Key:     "release_notes",
        Label:   "Release notes",
        Status:  "empty",
        Summary: "Drafted per build",
    }
    listing.Tiles = append(listing.Tiles, rnTile)

    // Count ready tiles.
    for _, t := range listing.Tiles {
        if t.Status == "ready" {
            listing.ReadyItems++
        }
    }

    return listing
}

func firstNonEmpty(a, b string) string {
    if a != "" {
        return a
    }
    return b
}
```

Ensure `"fmt"` is imported at the top of the file.

- [ ] **Step 3: Call the helper in `GetLaunchpad`**

In `GetLaunchpad`, after computing `resp.HeroStatus` or before (placement-insensitive), add:

```go
resp.Listing = c.buildListing(ctx, project.ID)
```

Place it BEFORE `computeHero(resp)` — the Listing completeness doesn't currently affect the hero, but having it populated before hero computation keeps the door open for Phase 5 priorities like "listing incomplete — blocking store submission".

- [ ] **Step 4: Echo `build_defaults` onto `resp.App`**

In the existing `if bd, ok := project.Settings["build_defaults"]` block, set every field:

```go
if bd, ok := project.Settings["build_defaults"].(map[string]interface{}); ok {
    if s, _ := bd["app_name"].(string); s != "" {
        resp.App.BuildDefaults.AppName = s
    }
    if s, _ := bd["icon_s3_key"].(string); s != "" {
        resp.App.BuildDefaults.IconS3Key = s
        resp.App.IconS3Key = s // existing behavior
    }
    if s, _ := bd["splash_color"].(string); s != "" {
        resp.App.BuildDefaults.SplashColor = s
    }
    if s, _ := bd["navbar_style"].(string); s != "" {
        resp.App.BuildDefaults.NavBarStyle = s
    }
    if b, _ := bd["remove_wemob_brand"].(bool); b {
        resp.App.BuildDefaults.RemoveWemobBranding = b
    }
}
```

- [ ] **Step 5: Verify compile**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
go build ./...
go vet ./...
```

Clean.

- [ ] **Step 6: Commit**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
git add internal/controllers/launchpad_controller.go
git commit -m "feat(ship): populate Launchpad Listing from AppStoreListing + MobileAppConfig

Reads the durable AppStoreListing (icon, title, description, screenshots,
locale, privacy URL) and MobileAppConfig (permissions) to produce per-tile
status for the 4×2 grid. Privacy URL tile goes 'missing' (blocking) when
sensitive permissions are on, 'warn' (optional) otherwise.

Also echoes AppProject.settings.build_defaults onto LaunchpadApp so the
frontend's new-build trigger can compose a request without a second
round-trip."
```

---

## Task 3: Fill `Builds` in `GetLaunchpad`

**Files:**
- Modify: `internal/controllers/launchpad_controller.go`

- [ ] **Step 1: Inspect `AppBuildTask` + related**

The model has: `ID`, `ProjectID`, `BuildID`, `Finished`, `Platform`, `AutoDeploy`, `StatusText`, `ErrorMessage`, `AnnotationID` (Phase 1), `ExecuteTime`, `CreatedAt`, `UpdatedAt`, plus relations to `Build` and `BuildAnnotation`.

Active builds = `Finished = false`. Recent = `Finished = true`, last 5.

- [ ] **Step 2: Add `buildBuilds` helper**

```go
func (c *Controller) buildBuilds(ctx context.Context, v1ProjectID string) responses.LaunchpadBuilds {
    builds := responses.LaunchpadBuilds{
        Active: []responses.LaunchpadBuildSummary{},
        Recent: []responses.LaunchpadBuildSummary{},
    }
    if v1ProjectID == "" {
        return builds
    }

    // Active: not finished.
    var active []models.AppBuildTask
    if err := c.DB.DB.NewSelect().
        Model(&active).
        Relation("Annotation").
        Relation("Build").
        Where("project_id = ?", v1ProjectID).
        Where("finished = ?", false).
        Order("created_at DESC").
        Scan(ctx); err == nil {
        for _, t := range active {
            builds.Active = append(builds.Active, buildTaskToSummary(t))
        }
    }

    // Recent: last 5 finished.
    var recent []models.AppBuildTask
    if err := c.DB.DB.NewSelect().
        Model(&recent).
        Relation("Annotation").
        Relation("Build").
        Where("project_id = ?", v1ProjectID).
        Where("finished = ?", true).
        Order("created_at DESC").
        Limit(5).
        Scan(ctx); err == nil {
        for _, t := range recent {
            builds.Recent = append(builds.Recent, buildTaskToSummary(t))
        }
    }

    return builds
}

func buildTaskToSummary(t models.AppBuildTask) responses.LaunchpadBuildSummary {
    s := responses.LaunchpadBuildSummary{
        ID:         t.ID.String(),
        BuildID:    t.BuildID.String(),
        Platform:   t.Platform,
        Status:     t.StatusText,
        StatusText: t.StatusText,
        Finished:   t.Finished,
        CreatedAt:  t.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
        UpdatedAt:  t.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
    }
    if t.Build != nil {
        s.Version = t.Build.VersionString
        s.BuildNumber = t.Build.BuildNumber
    }
    if t.ErrorMessage != "" {
        s.ErrorMessage = t.ErrorMessage
    }
    if t.AnnotationID != nil && t.Annotation != nil {
        s.AnnotationID = t.AnnotationID.String()
        s.AnnotationName = t.Annotation.Name
        s.AnnotationMsg = t.Annotation.HumanMessage
        s.AnnotationFix = t.Annotation.SuggestedFix
    }
    return s
}
```

- [ ] **Step 3: Call the helper**

In `GetLaunchpad`, after `buildListing`:

```go
resp.Builds = c.buildBuilds(ctx, v1ProjectID)
```

- [ ] **Step 4: Verify compile**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
go build ./...
go vet ./...
```

Clean.

- [ ] **Step 5: Commit**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
git add internal/controllers/launchpad_controller.go
git commit -m "feat(ship): populate Launchpad Builds from AppBuildTask history

Queries AppBuildTask with Annotation + Build relations, splits into
active (finished=false) and recent (last 5 finished). Each summary
carries status text, version + build number, error_message, and
flattened annotation fields so the frontend renders the annotated
failure without a second lookup."
```

---

## Task 4: Fill `Identity` in `GetLaunchpad`

**Files:**
- Modify: `internal/controllers/launchpad_controller.go`

- [ ] **Step 1: Add `buildIdentity` helper**

```go
func (c *Controller) buildIdentity(ctx context.Context, appProjectID uuid.UUID, app responses.LaunchpadApp) responses.LaunchpadIdentity {
    id := responses.LaunchpadIdentity{
        BundleID:    app.BundleID,
        Permissions: []string{},
    }
    if app.VersionName != "" {
        id.Version = fmt.Sprintf("%s (Build %d)", app.VersionName, app.BuildNumber)
    } else {
        id.Version = "not yet built"
    }

    var cfg models.MobileAppConfig
    if err := c.DB.DB.NewSelect().
        Model(&cfg).
        Where("project_id = ?", appProjectID).
        Scan(ctx); err == nil {
        if cfg.Permissions != nil {
            id.Permissions = cfg.Permissions
        }
    }

    return id
}
```

- [ ] **Step 2: Call the helper**

In `GetLaunchpad`, after `buildBuilds`:

```go
resp.Identity = c.buildIdentity(ctx, project.ID, resp.App)
```

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
git add internal/controllers/launchpad_controller.go
git commit -m "feat(ship): populate Launchpad Identity from MobileAppConfig

Bundle ID and version inherit from the LaunchpadApp we already
computed. Permissions come from MobileAppConfig.Permissions (may be
empty)."
```

---

## Task 5: Backend smoke

**Why:** Confirm the response JSON is complete before the frontend consumes it.

- [ ] **Step 1: Pick an app with real data**

Use Postgres MCP:

```sql
SELECT p.id, p.name, p.settings ? 'v1_project_id' AS has_v1,
       (SELECT count(*) FROM builds WHERE project_id = (p.settings->>'v1_project_id')::uuid) AS build_count,
       (SELECT count(*) FROM app_build_tasks WHERE project_id = (p.settings->>'v1_project_id')::uuid) AS task_count,
       (SELECT 1 FROM app_store_listings WHERE app_project_id = p.id LIMIT 1) AS has_listing,
       (SELECT 1 FROM mobile_app_configs WHERE project_id = p.id LIMIT 1) AS has_config
FROM app_projects p
WHERE p.settings ? 'v1_project_id'
ORDER BY p.updated_at DESC
LIMIT 5;
```

Pick the one with `has_listing` and `has_config` set. Call its id `$APP_ID`.

- [ ] **Step 2: Document the manual curl check**

The user will run this locally (sandbox can't boot backend):

```bash
cd /Users/knid/Projects/wemob/wemob-backend
docker-compose -f docker-compose-dev.yaml up -d
go run cmd/main.go &

TOKEN="7a72ce76171a432d511aa5351b91c6c4e674e01c2ce1798b372b1ad42e6444ae"
APP_ID="<uuid>"

curl -s -H "Authorization: Bearer $TOKEN" \
    "http://localhost:8001/v1/apps/$APP_ID/launchpad" | jq '{
      app: .app,
      listing_counts: {ready: .listing.ready_items, total: .listing.total_items},
      listing_tiles: [.listing.tiles[] | {key, status, summary}],
      builds: {active_count: (.builds.active|length), recent_count: (.builds.recent|length)},
      identity: .identity
    }'
```

Expected:
- `app.build_defaults` is populated (from Phase 1 backfill).
- `listing.total` = 8; `listing_tiles` shows 8 entries with sensible status per the real listing state.
- `builds.active_count` + `recent_count` match the DB counts.
- `identity.bundle_id` = `com.wemob.<slug>`, `identity.version` has "Build N", `identity.permissions` is a list (may be empty).

- [ ] **Step 3: No commit — this is a runtime-verified step handed to the user in the Phase 2b summary.**

---

## Task 6: Frontend — `useLaunchpad` hook

**Why:** Centralize fetch + refetch + polling so the page is thin.

**Repo:** `/Users/knid/Projects/wemob/wemob-app`
**Branch:** `feat/ship-ux-phase-2b` (create from `main` which now has 2a merged)

- [ ] **Step 1: Create the branch**

```bash
cd /Users/knid/Projects/wemob/wemob-app
git checkout main
git pull --ff-only origin main || true
git checkout -b feat/ship-ux-phase-2b
```

The `|| true` lets the pull fail silently if the remote hasn't been pushed to.

- [ ] **Step 2: Write the hook**

`src/hooks/use-launchpad.ts`:

```ts
"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { apiClient } from "@/lib/api";
import type { LaunchpadResponse } from "@/lib/ship/launchpad-types";

interface UseLaunchpadState {
  data: LaunchpadResponse | null;
  loading: boolean;
  error: Error | null;
  refetch: () => Promise<void>;
}

/**
 * Fetches the Launchpad aggregate for an app. Polls every 6s while
 * there's an active build (to surface stage-ladder progress); polling
 * stops once builds.active.length === 0.
 */
export function useLaunchpad(appId: string | null | undefined, enabled: boolean): UseLaunchpadState {
  const [data, setData] = useState<LaunchpadResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);
  const cancelledRef = useRef(false);

  const fetchOnce = useCallback(async () => {
    if (!appId || !enabled) return;
    try {
      const res = await apiClient.getLaunchpad(appId);
      if (cancelledRef.current) return;
      setData(res);
      setError(null);
    } catch (err) {
      if (cancelledRef.current) return;
      setError(err instanceof Error ? err : new Error(String(err)));
    } finally {
      if (!cancelledRef.current) setLoading(false);
    }
  }, [appId, enabled]);

  // Initial + manual-refetch-triggered fetches.
  useEffect(() => {
    cancelledRef.current = false;
    setLoading(true);
    fetchOnce();
    return () => {
      cancelledRef.current = true;
    };
  }, [fetchOnce]);

  // Poll while there's an active build.
  useEffect(() => {
    if (!data || data.builds.active.length === 0) return;
    const handle = setInterval(() => {
      fetchOnce();
    }, 6000);
    return () => clearInterval(handle);
  }, [data, fetchOnce]);

  return { data, loading, error, refetch: fetchOnce };
}
```

- [ ] **Step 3: Verify lint**

```bash
cd /Users/knid/Projects/wemob/wemob-app
npm run lint
```

No new errors (pre-existing noise fine).

- [ ] **Step 4: Commit**

```bash
cd /Users/knid/Projects/wemob/wemob-app
git add src/hooks/use-launchpad.ts
git commit -m "feat(ship): add useLaunchpad hook

Fetches the /v1/apps/:id/launchpad aggregate with an automatic 6-second
poll while an active build is in-flight. Stops polling once all active
builds finish. Exposes refetch() so trigger-and-reload is one call."
```

---

## Task 7: Frontend — `ListingGrid` + `ListingTile`

**Files:**
- Create: `src/components/ship/listing-grid.tsx`
- Create: `src/components/ship/listing-tile.tsx`

- [ ] **Step 1: Extend shared types**

Add to `src/lib/ship/launchpad-types.ts`:

```ts
export type ListingTileStatus = "ready" | "warn" | "missing" | "empty";

export interface LaunchpadListingTile {
  key: string;
  label: string;
  status: ListingTileStatus;
  summary: string;
  remediation?: string;
}

export interface LaunchpadBuildDefaults {
  app_name?: string;
  icon_s3_key?: string;
  splash_color?: string;
  navbar_style?: string;
  remove_wemob_branding?: boolean;
}
```

Update `LaunchpadApp` to include `build_defaults`:

```ts
export interface LaunchpadApp {
  id: string;
  name: string;
  bundle_id: string;
  icon_s3_key: string;
  version_name: string;
  build_number: number;
  build_defaults: LaunchpadBuildDefaults;
}
```

Update `LaunchpadListing`:

```ts
export interface LaunchpadListing {
  ready_items: number;
  total_items: number;
  tiles: LaunchpadListingTile[];
}
```

Update `LaunchpadBuildSummary`:

```ts
export interface LaunchpadBuildSummary {
  id: string;
  build_id: string;
  platform: "android" | "ios";
  status: string;
  status_text?: string;
  finished: boolean;
  version?: string;
  build_number?: number;
  created_at: string;
  updated_at?: string;
  error_message?: string;
  annotation_id?: string;
  annotation_name?: string;
  annotation_suggested_fix?: string;
  annotation_human_message?: string;
}
```

- [ ] **Step 2: Write `ListingTile`**

`src/components/ship/listing-tile.tsx`:

```tsx
"use client";

import type { LaunchpadListingTile } from "@/lib/ship/launchpad-types";
import { cn } from "@/lib/utils";

export function ListingTile({ tile }: { tile: LaunchpadListingTile }) {
  const style = tileStyle(tile.status);
  return (
    <div
      className={cn(
        "aspect-[1.4/1] rounded-xl p-3 flex flex-col justify-between",
        style.bg,
        style.border,
      )}
    >
      <div className="text-[9px] uppercase tracking-[0.5px] text-[var(--ink-faint)]">
        {tile.label}
      </div>
      <div>
        <div className={cn("text-[11px] font-medium leading-[1.3]", style.summary)}>
          {tile.summary}
        </div>
        {tile.remediation && (
          <div className="text-[10px] text-[var(--accent)] mt-1 cursor-pointer hover:underline">
            {tile.remediation} →
          </div>
        )}
      </div>
    </div>
  );
}

function tileStyle(status: LaunchpadListingTile["status"]) {
  switch (status) {
    case "ready":
      return {
        bg: "bg-[var(--bg-surface)]",
        border: "border border-[var(--border)]",
        summary: "text-[var(--ink)]",
      };
    case "warn":
      return {
        bg: "bg-[#fffdf5]",
        border: "border border-[#f0d9a5]",
        summary: "text-[#8b6520]",
      };
    case "missing":
      return {
        bg: "bg-[#fffdf5]",
        border: "border-2 border-dashed border-[#d8a540]",
        summary: "text-[#8b6520]",
      };
    case "empty":
      return {
        bg: "bg-[var(--bg-raised)]",
        border: "border border-[var(--border)]",
        summary: "text-[var(--ink-muted)]",
      };
  }
}
```

- [ ] **Step 3: Write `ListingGrid`**

`src/components/ship/listing-grid.tsx`:

```tsx
"use client";

import type { LaunchpadListing } from "@/lib/ship/launchpad-types";
import { ListingTile } from "./listing-tile";

export function ListingGrid({ listing }: { listing: LaunchpadListing }) {
  return (
    <section className="mt-6">
      <div className="flex items-center justify-between mb-2">
        <div className="text-[10px] uppercase tracking-[1px] text-[var(--ink-faint)]">
          Listing
        </div>
        <div className="text-[11px] text-[var(--ink-muted)]">
          {listing.ready_items} of {listing.total_items} ready
        </div>
      </div>
      <div className="grid grid-cols-4 gap-2">
        {listing.tiles.map((t) => (
          <ListingTile key={t.key} tile={t} />
        ))}
      </div>
    </section>
  );
}
```

- [ ] **Step 4: Verify lint + build**

```bash
cd /Users/knid/Projects/wemob/wemob-app
npm run lint
npm run build
```

Clean (no new errors; pre-existing noise OK).

- [ ] **Step 5: Commit**

```bash
cd /Users/knid/Projects/wemob/wemob-app
git add src/lib/ship/launchpad-types.ts src/components/ship/listing-grid.tsx src/components/ship/listing-tile.tsx
git commit -m "feat(ship): add ListingGrid + ListingTile components

4-column tile grid showing the 8 store-listing items (icon, title,
description, screenshots, locale, category, privacy URL, release notes)
with per-tile status colors (ready / warn / missing / empty).

Missing tiles surface a remediation CTA inline ('Generate with AI',
'Use project icon', etc.) for Phase 3's Pilot to pick up."
```

---

## Task 8: Frontend — `BuildStageLadder` + `AnnotatedFailure` + `BuildsFeed`

**Files:**
- Create: `src/components/ship/build-stage-ladder.tsx`
- Create: `src/components/ship/annotated-failure.tsx`
- Create: `src/components/ship/builds-feed.tsx`

- [ ] **Step 1: `BuildStageLadder`**

```tsx
"use client";

import { cn } from "@/lib/utils";

type Stage = "Preparing" | "Building" | "Uploading" | "Signing" | "Built";

interface BuildStageLadderProps {
  status: string; // raw status from AppBuildTask
  platform: "android" | "ios";
}

export function BuildStageLadder({ status, platform }: BuildStageLadderProps) {
  const stages: Stage[] = platform === "ios"
    ? ["Preparing", "Building", "Uploading", "Signing", "Built"]
    : ["Preparing", "Building", "Uploading", "Built"];

  const key = status.toLowerCase();
  let currentIndex = -1;
  if (key === "queued" || key === "preparing") currentIndex = 0;
  else if (key === "building" || key === "reviewing") currentIndex = 1;
  else if (key === "uploading") currentIndex = 2;
  else if (key === "signing") currentIndex = 3;
  else if (key === "built" || key === "signed") currentIndex = stages.length - 1;

  return (
    <ol className="flex items-center gap-1 mt-2.5 mb-1">
      {stages.map((name, idx) => {
        const done = idx < currentIndex || currentIndex === stages.length - 1;
        const active = idx === currentIndex && currentIndex < stages.length - 1;
        return (
          <li key={name} className="flex items-center gap-1 flex-1 min-w-0">
            <div className="flex flex-col items-center gap-0.5 min-w-0">
              <div
                className={cn(
                  "w-5 h-5 rounded-full inline-flex items-center justify-center text-[9px]",
                  done
                    ? "bg-[var(--success)] text-white"
                    : active
                      ? "bg-[#3d6890] text-white animate-pulse"
                      : "bg-[var(--bg-raised)] border border-[var(--border)] text-[var(--ink-faint)]",
                )}
              >
                {done ? "✓" : active ? "●" : idx + 1}
              </div>
              <span
                className={cn(
                  "text-[9.5px] mono truncate max-w-full",
                  active
                    ? "text-[var(--ink)] font-medium"
                    : done
                      ? "text-[var(--ink-muted)]"
                      : "text-[var(--ink-faint)]",
                )}
              >
                {name}
              </span>
            </div>
            {idx < stages.length - 1 && (
              <div
                className={cn(
                  "flex-1 h-px mb-3",
                  idx < currentIndex ? "bg-[var(--success)]" : "bg-[var(--border)]",
                )}
              />
            )}
          </li>
        );
      })}
    </ol>
  );
}
```

- [ ] **Step 2: `AnnotatedFailure`**

```tsx
"use client";

import { useState } from "react";
import type { LaunchpadBuildSummary } from "@/lib/ship/launchpad-types";

interface AnnotatedFailureProps {
  build: LaunchpadBuildSummary;
}

export function AnnotatedFailure({ build }: AnnotatedFailureProps) {
  const [showRaw, setShowRaw] = useState(false);
  const hasAnnotation = !!build.annotation_id;

  return (
    <div className="mt-2 rounded-lg border border-[var(--accent)]/20 bg-[var(--accent-soft)] px-3 py-2.5">
      {hasAnnotation ? (
        <>
          <div className="text-[12px] text-[var(--ink)] font-medium mb-1">
            {build.annotation_human_message}
          </div>
          {build.annotation_suggested_fix && (
            <div className="text-[11px] text-[var(--ink-muted)] leading-[1.55] mb-2">
              {build.annotation_suggested_fix}
            </div>
          )}
        </>
      ) : (
        <div className="text-[12px] text-[var(--accent)] mb-1">
          Build failed — no known pattern matched.
        </div>
      )}
      <button
        type="button"
        onClick={() => setShowRaw((v) => !v)}
        className="text-[11px] text-[var(--ink-muted)] hover:text-[var(--ink)] underline"
      >
        {showRaw ? "Hide raw log" : "Show raw log"}
      </button>
      {showRaw && build.error_message && (
        <pre className="mt-2 max-h-48 overflow-auto rounded-md bg-[var(--bg)] border border-[var(--border)] px-2 py-2 text-[10px] font-mono leading-[1.5] whitespace-pre-wrap break-words">
          {build.error_message}
        </pre>
      )}
    </div>
  );
}
```

- [ ] **Step 3: `BuildsFeed`**

```tsx
"use client";

import { Download } from "lucide-react";
import type { LaunchpadBuildSummary, LaunchpadBuilds } from "@/lib/ship/launchpad-types";
import { AnnotatedFailure } from "./annotated-failure";
import { BuildStageLadder } from "./build-stage-ladder";
import { cn } from "@/lib/utils";

interface BuildsFeedProps {
  builds: LaunchpadBuilds;
}

export function BuildsFeed({ builds }: BuildsFeedProps) {
  const entries = [...builds.active, ...builds.recent];
  return (
    <section className="mt-6">
      <div className="text-[10px] uppercase tracking-[1px] text-[var(--ink-faint)] mb-2">
        Builds
      </div>
      {entries.length === 0 ? (
        <div className="border border-dashed border-[var(--border)] rounded-xl p-6 text-center text-[12px] text-[var(--ink-muted)]">
          No builds yet.
        </div>
      ) : (
        <div className="flex flex-col gap-2">
          {entries.map((b) => (
            <BuildRow key={b.id} build={b} />
          ))}
        </div>
      )}
    </section>
  );
}

function BuildRow({ build }: { build: LaunchpadBuildSummary }) {
  const failed = /^(error|failed)$/i.test(build.status);
  const active = !build.finished;
  const done = build.finished && !failed;

  return (
    <div
      className={cn(
        "rounded-xl border p-3",
        failed
          ? "border-[var(--accent)]/20 bg-[var(--accent-soft)]"
          : active
            ? "border-[#c2d0e0] bg-[#f0f5fa]"
            : "border-[var(--border)] bg-[var(--bg-surface)]",
      )}
    >
      <div className="flex items-center gap-2.5">
        <div className="text-[12px] font-medium capitalize">{build.platform}</div>
        {build.version && (
          <div className="font-mono text-[11px] text-[var(--ink-muted)]">
            v{build.version}
            {typeof build.build_number === "number" && build.build_number > 0 && (
              <> (Build {build.build_number})</>
            )}
          </div>
        )}
        <div className="text-[11px] text-[var(--ink-muted)]">· {build.status}</div>
        <div className="ml-auto text-[11px] text-[var(--ink-faint)]">
          {formatRelative(build.created_at)}
        </div>
      </div>

      {active && <BuildStageLadder status={build.status} platform={build.platform} />}
      {failed && <AnnotatedFailure build={build} />}
      {done && (
        <div className="mt-2">
          <button
            type="button"
            className="px-2.5 py-1 rounded-md text-[11px] border border-[var(--border-strong)] inline-flex items-center gap-1 hover:bg-[var(--bg-hover)]"
          >
            <Download size={11} />
            Download {build.platform === "android" ? "AAB" : "IPA"}
          </button>
        </div>
      )}
    </div>
  );
}

function formatRelative(iso: string): string {
  const d = new Date(iso).getTime();
  if (!Number.isFinite(d)) return "";
  const diff = Date.now() - d;
  const mins = Math.floor(diff / 60000);
  if (mins < 1) return "just now";
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  return `${days}d ago`;
}
```

- [ ] **Step 4: Verify lint + build**

```bash
cd /Users/knid/Projects/wemob/wemob-app
npm run lint
npm run build
```

Clean.

- [ ] **Step 5: Commit**

```bash
cd /Users/knid/Projects/wemob/wemob-app
git add src/components/ship/build-stage-ladder.tsx src/components/ship/annotated-failure.tsx src/components/ship/builds-feed.tsx
git commit -m "feat(ship): add Builds feed with stage ladder + annotated failures

- BuildStageLadder: horizontal 4-stage (5 for iOS) progress indicator
  using the same stage vocabulary the Codemagic yaml reports.
- AnnotatedFailure: renders the Phase 1 pattern-matched human message +
  suggested fix, with 'Show raw log' toggle for the full error.
- BuildsFeed: merges active + recent builds, shows download button on
  done Android/iOS builds, stage ladder while active."
```

---

## Task 9: Frontend — `IdentityPanel`

**Files:**
- Create: `src/components/ship/identity-panel.tsx`

- [ ] **Step 1: Write the component**

```tsx
"use client";

import { useState } from "react";
import { ChevronDown, ChevronRight } from "lucide-react";
import type { LaunchpadIdentity } from "@/lib/ship/launchpad-types";

export function IdentityPanel({ identity }: { identity: LaunchpadIdentity }) {
  const [open, setOpen] = useState(false);
  return (
    <section className="mt-6 border border-[var(--border)] rounded-xl px-4 py-3 bg-[var(--bg-surface)]">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="w-full flex items-center gap-2 text-left"
      >
        <span className="text-[10px] uppercase tracking-[1px] text-[var(--ink-faint)]">
          Identity
        </span>
        <span className="font-mono text-[11px] text-[var(--ink-muted)] truncate">
          {identity.bundle_id || "—"} · {identity.version}
          {identity.permissions.length > 0 && (
            <> · {identity.permissions.slice(0, 3).join(", ")}
              {identity.permissions.length > 3 && ` +${identity.permissions.length - 3}`}
            </>
          )}
        </span>
        <span className="ml-auto text-[var(--ink-faint)]">
          {open ? <ChevronDown size={13} /> : <ChevronRight size={13} />}
        </span>
      </button>
      {open && (
        <div className="mt-3 pt-3 border-t border-[var(--border)] text-[12px] leading-[1.6]">
          <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1.5">
            <dt className="text-[var(--ink-muted)]">Bundle ID</dt>
            <dd className="font-mono">{identity.bundle_id || "—"}</dd>
            <dt className="text-[var(--ink-muted)]">Version</dt>
            <dd className="font-mono">{identity.version}</dd>
            <dt className="text-[var(--ink-muted)] align-top">Permissions</dt>
            <dd>
              {identity.permissions.length === 0 ? (
                <span className="text-[var(--ink-faint)]">none</span>
              ) : (
                identity.permissions.join(", ")
              )}
            </dd>
          </dl>
        </div>
      )}
    </section>
  );
}
```

- [ ] **Step 2: Verify lint + build**

```bash
cd /Users/knid/Projects/wemob/wemob-app
npm run lint
npm run build
```

Clean.

- [ ] **Step 3: Commit**

```bash
cd /Users/knid/Projects/wemob/wemob-app
git add src/components/ship/identity-panel.tsx
git commit -m "feat(ship): add IdentityPanel component

Collapsed one-line summary with bundle ID, version, first 3 permissions
inline. Expands to show full dl with all permissions listed. Rarely
touched by users after first build."
```

---

## Task 10: Frontend — `NewBuildButton`

**Why:** One-click trigger using project defaults.

**Files:**
- Modify: `src/lib/api.ts` — add `triggerQuickBuild`
- Create: `src/components/ship/new-build-button.tsx`

- [ ] **Step 1: Inspect the existing build trigger path**

Find how the legacy UI triggers builds:

```bash
grep -rn "buildMobile\|POST.*builds\|createBuild\|projects/.*/builds" /Users/knid/Projects/wemob/wemob-app/src/lib/api.ts | head -10
```

Note the exact method name (likely `buildMobile` or similar), its signature (v1 project id + config), and the Codemagic call chain. DO NOT create a new backend endpoint.

- [ ] **Step 2: Add `triggerQuickBuild` to `apiClient`**

If there's an existing `buildMobile` / `createBuild` method that takes platforms + config, use it. Otherwise add a thin wrapper that calls the existing endpoint:

```ts
async triggerQuickBuild(
  appId: string,
  v1ProjectId: string,
  platforms: Array<"android" | "ios">,
  defaults: import("./ship/launchpad-types").LaunchpadBuildDefaults,
): Promise<void> {
  // Use the existing build trigger — same endpoint the legacy UI calls.
  // If the legacy method already takes the right shape, call that directly
  // instead of duplicating the endpoint path.
  const body = {
    app_name: defaults.app_name ?? "",
    icon: defaults.icon_s3_key ?? "",
    splash_screen_color: defaults.splash_color ?? "#ffffff",
    version_name: "1.0.0",
    version_code: "1",
    build_type: "release",
    platforms,
  };
  return this.request(`/v1/projects/${v1ProjectId}/builds`, {
    method: "POST",
    body: JSON.stringify(body),
  });
}
```

Adapt to the `apiClient.request<T>` signature — look at how other POST methods use it (`createDeployment`, `buildMobile`, etc.).

**Key:** the v1 project ID is needed. It's not directly on `LaunchpadApp`, so the component will need to read it from the AppProject settings. Easier option: add `v1_project_id` to `LaunchpadApp` explicitly.

Actually — add it. Update:

```go
// backend: internal/responses/launchpad.go, LaunchpadApp
V1ProjectID string `json:"v1_project_id,omitempty"`
```

And in `launchpad_controller.go`:

```go
resp.App.V1ProjectID = v1ProjectID
```

Update the TS type:

```ts
export interface LaunchpadApp {
  // ... existing fields
  v1_project_id?: string;
}
```

Do this in a separate commit on the backend side if easier, or fold into this task's commit — just note it in the message.

Actually — do this right now as part of Task 10 since it's a small cross-repo change. Make the backend change first, then the frontend.

- [ ] **Step 3: Backend — add `V1ProjectID` to `LaunchpadApp`**

Modify `internal/responses/launchpad.go`:

```go
type LaunchpadApp struct {
	ID            string                  `json:"id"`
	Name          string                  `json:"name"`
	BundleID      string                  `json:"bundle_id"`
	IconS3Key     string                  `json:"icon_s3_key"`
	VersionName   string                  `json:"version_name"`
	BuildNumber   int                     `json:"build_number"`
	V1ProjectID   string                  `json:"v1_project_id,omitempty"`
	BuildDefaults LaunchpadBuildDefaults  `json:"build_defaults"`
}
```

And in `launchpad_controller.go`, inside `GetLaunchpad` right after the `v1ProjectID` variable is set:

```go
resp.App.V1ProjectID = v1ProjectID
```

Commit on the backend with a tiny message — this will be squashed-looking but matches Phase 1's commit cadence:

```bash
cd /Users/knid/Projects/wemob/wemob-backend
go build ./... && go vet ./...
git add internal/responses/launchpad.go internal/controllers/launchpad_controller.go
git commit -m "feat(ship): echo v1_project_id on LaunchpadApp

Needed by the frontend's new-build trigger to POST to
/v1/projects/:v1ProjectId/builds without a second round-trip."
```

- [ ] **Step 4: Frontend — write `NewBuildButton`**

```tsx
"use client";

import { useState } from "react";
import { Plus, Loader2 } from "lucide-react";
import { toast } from "sonner";
import { apiClient } from "@/lib/api";
import type { LaunchpadApp } from "@/lib/ship/launchpad-types";

interface NewBuildButtonProps {
  app: LaunchpadApp;
  onTriggered: () => void;
}

export function NewBuildButton({ app, onTriggered }: NewBuildButtonProps) {
  const [busy, setBusy] = useState(false);

  const canBuild = !!app.v1_project_id && !!app.build_defaults.app_name && !!app.build_defaults.icon_s3_key;
  const disabledReason = !app.v1_project_id
    ? "This app isn't fully set up yet."
    : !app.build_defaults.app_name || !app.build_defaults.icon_s3_key
      ? "Project missing app name or icon — set them in Settings first."
      : "";

  async function handleClick() {
    if (!canBuild || busy) return;
    setBusy(true);
    try {
      await apiClient.triggerQuickBuild(
        app.id,
        app.v1_project_id as string,
        ["android", "ios"],
        app.build_defaults,
      );
      toast.success("Build started", { description: "We'll show progress here as it runs." });
      onTriggered();
    } catch (err) {
      toast.error("Couldn't start build", {
        description: err instanceof Error ? err.message : String(err),
      });
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex items-center gap-3">
      <button
        type="button"
        onClick={handleClick}
        disabled={!canBuild || busy}
        className="inline-flex items-center gap-1.5 px-3.5 py-2 rounded-lg text-[12px] font-medium bg-[var(--accent)] text-white hover:bg-[var(--accent-hover)] disabled:opacity-60 disabled:cursor-not-allowed transition-colors"
        title={disabledReason || undefined}
      >
        {busy ? <Loader2 size={12} className="animate-spin" /> : <Plus size={12} />}
        {busy ? "Starting…" : "New build"}
      </button>
      {disabledReason && !busy && (
        <span className="text-[11px] text-[var(--ink-faint)]">{disabledReason}</span>
      )}
    </div>
  );
}
```

- [ ] **Step 5: Verify lint + build**

```bash
cd /Users/knid/Projects/wemob/wemob-app
npm run lint
npm run build
```

Clean.

- [ ] **Step 6: Commit the frontend side**

```bash
cd /Users/knid/Projects/wemob/wemob-app
git add src/lib/api.ts src/components/ship/new-build-button.tsx src/lib/ship/launchpad-types.ts
git commit -m "feat(ship): add NewBuildButton + triggerQuickBuild helper

One-click 'New build' that uses the project's build_defaults (from
Phase 1 backfill) to POST the existing /v1/projects/:v1/builds
endpoint — no new backend endpoint needed.

Disabled with an inline hint when build_defaults are incomplete
(missing app_name or icon) so users understand why the button is off."
```

---

## Task 11: Frontend — update `Launchpad` to render all 5 zones

**Files:**
- Modify: `src/components/ship/launchpad.tsx`
- Modify: `src/app/apps/[appId]/ship/page.tsx`

- [ ] **Step 1: Replace placeholders in `Launchpad`**

```tsx
"use client";

import type { LaunchpadResponse } from "@/lib/ship/launchpad-types";
import { BuildsFeed } from "./builds-feed";
import { IdentityPanel } from "./identity-panel";
import { LaunchpadHeader } from "./launchpad-header";
import { ListingGrid } from "./listing-grid";
import { NewBuildButton } from "./new-build-button";
import { StoreTile } from "./store-tile";

interface LaunchpadProps {
  data: LaunchpadResponse;
  onRefetch: () => void;
}

export function Launchpad({ data, onRefetch }: LaunchpadProps) {
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

        <ListingGrid listing={data.listing} />

        <section className="mt-6">
          <div className="flex items-center justify-between mb-2">
            <div className="text-[10px] uppercase tracking-[1px] text-[var(--ink-faint)]">
              Builds
            </div>
            <NewBuildButton app={data.app} onTriggered={onRefetch} />
          </div>
          <BuildsFeed builds={data.builds} />
        </section>

        <IdentityPanel identity={data.identity} />
      </div>
    </div>
  );
}
```

Note that `BuildsFeed` renders its own section heading. Slight duplication — keep the heading above the `NewBuildButton` in the outer section, and have `BuildsFeed` NOT render the "Builds" label. Update `builds-feed.tsx` to remove its `<div>Builds</div>` header (it's now owned by the parent).

Adjust `builds-feed.tsx` Step 3 accordingly: drop the header line from the section — leave only the entries:

```tsx
export function BuildsFeed({ builds }: BuildsFeedProps) {
  const entries = [...builds.active, ...builds.recent];
  if (entries.length === 0) {
    return (
      <div className="border border-dashed border-[var(--border)] rounded-xl p-6 text-center text-[12px] text-[var(--ink-muted)]">
        No builds yet.
      </div>
    );
  }
  return (
    <div className="flex flex-col gap-2">
      {entries.map((b) => (
        <BuildRow key={b.id} build={b} />
      ))}
    </div>
  );
}
```

- [ ] **Step 2: Update `page.tsx` to use `useLaunchpad`**

Replace the inline fetch with the hook:

```tsx
"use client";

import { notFound, useParams, useRouter } from "next/navigation";
import { useEffect } from "react";
import { AuthGuard } from "@/components/auth/auth-guard";
import { StudioShell } from "@/components/layout/studio-shell";
import { Launchpad } from "@/components/ship/launchpad";
import { useFeatureFlag } from "@/hooks/use-feature-flag";
import { useLaunchpad } from "@/hooks/use-launchpad";

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
  const { data, loading, error, refetch } = useLaunchpad(appId, flagEnabled === true);

  useEffect(() => {
    if (flagEnabled === false) {
      router.replace(`/apps/${appId}/publish`);
    }
  }, [flagEnabled, appId, router]);

  if (flagEnabled === undefined) {
    return (
      <div className="flex-1 flex items-center justify-center text-[13px] text-[var(--ink-faint)]">
        Loading…
      </div>
    );
  }
  if (flagEnabled === false) return null;

  if (loading && !data) {
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
          Can&apos;t load this app.
          {error && <div className="mt-2 text-[var(--ink-muted)]">{error.message}</div>}
        </div>
      </div>
    );
  }

  return <Launchpad data={data} onRefetch={refetch} />;
}
```

- [ ] **Step 3: Verify lint + build**

```bash
cd /Users/knid/Projects/wemob/wemob-app
npm run lint
npm run build
```

Clean.

- [ ] **Step 4: Commit**

```bash
cd /Users/knid/Projects/wemob/wemob-app
git add src/components/ship/launchpad.tsx src/app/apps/\[appId\]/ship/page.tsx src/components/ship/builds-feed.tsx
git commit -m "feat(ship): wire all five Launchpad zones + useLaunchpad hook

ZonePlaceholder stubs replaced with real components. Page consumes
useLaunchpad, which handles initial fetch + 6-second polling while
builds are active + refetch-on-trigger for NewBuildButton."
```

---

## Task 12: End-to-end smoke

**Why:** Full browser check before declaring 2b done.

- [ ] **Step 1: Documented smoke**

```
# Backend
cd /Users/knid/Projects/wemob/wemob-backend
docker-compose -f docker-compose-dev.yaml up -d
go run cmd/main.go

# Frontend
cd /Users/knid/Projects/wemob/wemob-app && npm run dev

# Browser: http://localhost:3000/apps/<appId>/ship
```

Expected:
1. Hero renders with app icon, name, bundle ID, version.
2. Stores tiles show real state per platform.
3. Listing grid shows 8 tiles with correct per-tile status.
4. Builds feed shows real history; download buttons on completed builds; stage ladder on active builds.
5. Identity panel collapsed by default; expand shows bundle/version/permissions.
6. "New build" button: click, toast confirms started, polling kicks in; active build appears; stage ladder advances as Codemagic reports; polling stops on finish.

- [ ] **Step 2: No commit — Phase 2b completion summary to user includes this checklist.**

---

## Phase 2b completion criteria

- Backend + frontend build+vet+lint clean on `feat/ship-ux-phase-2b` branches.
- `GET /v1/apps/:appId/launchpad` populates Listing.tiles (8 items), Builds.active + Builds.recent, Identity (bundle/version/permissions), and App.build_defaults + App.v1_project_id.
- Frontend `/apps/:appId/ship` renders all 5 zones with real data.
- New build button uses project defaults and the existing `/v1/projects/:v1/builds` endpoint.
- `new_ship_ux` flag continues to gate the whole route.
- No regressions on the legacy `/publish` route.

Phase 3 (Pilot slide-over + listing preview + produce/download-metadata + connection walkthroughs) is unblocked.

## Out of scope

- `⌘K` command palette (Phase 4).
- `/ship` cross-app table (Phase 4).
- Quick-ship single-screen submission (Phase 4).
- Deleting legacy routes / files (Phase 5).
