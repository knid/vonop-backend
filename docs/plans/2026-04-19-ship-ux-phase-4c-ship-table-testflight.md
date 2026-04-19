# Ship UX — Phase 4c · `/ship` Cross-App Table + TestFlight Wiring Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Two related completions of Phase 4:
1. **`/ship` cross-app table** — a power-user home showing every in-flight and recent build + submission across the user's apps. Replaces the legacy `/builds` page (keep it routed for back-compat; redirect to `/ship` when the `new_ship_ux` flag is on).
2. **TestFlight wiring** — the Phase 4a/4b `rollout_track` field finally reaches Codemagic. For iOS deploys, `CM_ROLLOUT_TRACK=internal|closed` routes through `fastlane pilot` (TestFlight), `production` keeps the existing `fastlane deliver` (App Store Review). Android already uses track properly via Phase 3c's Internal-track work.

**Architecture:** Backend adds `GET /v1/ship` returning a merged, sorted list of build + submission events for the current user's apps. The Codemagic deploy trigger (backend side) forwards `rollout_track` as an env var; the ios-deploy workflow branches to `pilot(...)` when the track is TestFlight. Frontend adds a new route + table component; `/builds` route becomes a flag-gated redirect.

**Tech stack:** Go 1.22+, Bun, chi/v5. Next.js 16, React 19, Tailwind 4. codemagic.yaml + fastlane pilot.

**Design spec:** `docs/design/2026-04-18-wemob-publish-ux-redesign-design.md` ("/ship cross-app table", "TestFlight via fastlane pilot").

---

## Test strategy

Same discipline: `go build` + `go vet` clean, `npm run build` + lint clean, `ruby -e "require 'yaml'; YAML.load_file(...)"` for codemagic.yaml, runtime smoke documented for user.

## File structure overview

### Backend — new files

```
internal/responses/ship.go                — ShipItem + ShipResponse types
internal/controllers/ship_controller.go   — GetShipItems aggregator
internal/views/ship_views.go              — HTTP handler
```

### Backend — modified

```
cmd/main.go                                — register GET /v1/ship
internal/tasks/ios.go                      — pass CM_ROLLOUT_TRACK in deploy trigger
internal/controllers/deployment.go         — include rollout_track in Codemagic env
```

### Flutter-template — modified

```
codemagic.yaml                             — ios-deploy branches to pilot when CM_ROLLOUT_TRACK is TestFlight
```

### Frontend — new files

```
src/app/ship/page.tsx                      — Next.js route (existing /builds-style)
src/components/ship/ship-table.tsx         — cross-app table component
src/components/ship/ship-table-filters.tsx — inline filter pills
src/hooks/use-ship-items.ts                — fetch + poll /v1/ship
src/lib/ship/ship-types.ts                 — ShipItem shared TS types
```

### Frontend — modified

```
src/lib/api.ts                             — listShipItems method
src/app/builds/page.tsx                    — redirect to /ship when new_ship_ux flag on
src/lib/ship/commands.ts                   — add "All builds & submissions" global command
```

## Why these boundaries

- **`/ship` as a dedicated backend endpoint rather than extending the Launchpad aggregate.** Launchpad is per-app; `/ship` spans all apps for one user. Different shape, different caller; own endpoint keeps concerns clean.
- **Preserve `/builds` as a redirect for users not on the flag.** Zero-risk rollback path — flip the flag off and the legacy route returns automatically.
- **TestFlight wiring as part of 4c.** The 4b `rollout_track` field is only cosmetic until the deploy trigger passes it through. Closing the loop here prevents the feature from feeling broken during Phase 5 polish.

---

## Task 1: Backend — ShipItem types

**Files:**
- Create: `internal/responses/ship.go`

- [ ] **Step 1: Write the file**

```go
package responses

// ShipItem is a single row in the cross-app /ship table. It can
// represent either an in-flight (or recently-completed) build task
// OR a submission deployment. The frontend renders rows by Kind and
// filters on Status for the pill filter bar.
type ShipItem struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"` // "build" | "deployment"
	AppID      string `json:"app_id"`
	AppName    string `json:"app_name"`
	Store      string `json:"store,omitempty"`      // "play_store" | "app_store" | "" (builds with platform only)
	Platform   string `json:"platform,omitempty"`   // "android" | "ios"
	Version    string `json:"version,omitempty"`    // "1.0.3 (Build 12)"
	Status     string `json:"status"`               // human-readable, e.g. "Live", "Building", "Rejected"
	StatusKind string `json:"status_kind"`          // "success" | "info" | "warn" | "danger" | "neutral"
	ETASeconds int    `json:"eta_seconds,omitempty"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at,omitempty"`
}

// ShipResponse wraps the item list. Using a wrapper object (rather
// than a raw array) so we can add counts / paging / deltas later
// without a breaking API change.
type ShipResponse struct {
	Items []ShipItem `json:"items"`
}
```

- [ ] **Step 2: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
go build ./...
go vet ./...
git add internal/responses/ship.go
git commit -m "feat(ship): add ShipItem + ShipResponse types for cross-app table

One row = one build task or one deployment. Status is human-readable;
status_kind is a 5-variant enum the frontend maps to badge colors.
Wrapper response shape leaves room for counts / paging later."
```

---

## Task 2: Backend — `GetShipItems` controller

**Files:**
- Create: `internal/controllers/ship_controller.go`

- [ ] **Step 1: Inspect existing models and relationships**

Understand:
- `AppBuildTask` has ProjectID (v1 Project UUID), Platform, StatusText, Finished, ErrorMessage, relation to `Build` (for version).
- `Deployment` has ProjectID (v1), Platforms []string, Status, Title, ReleaseNotes, Method, plus the new RolloutTrack.
- `AppProject` has the v2 AppProject UUID + Name, with `settings.v1_project_id` linking to v1 Project.

To show `app_name` in the table, we need a bridge from v1 Project → AppProject.Name. The existing Launchpad controller looks up `app_projects.settings.v1_project_id` to find the AppProject — reuse that pattern.

- [ ] **Step 2: Write the aggregator**

Create `/Users/knid/Projects/wemob/wemob-backend/internal/controllers/ship_controller.go`:

```go
package controllers

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
)

// GetShipItems aggregates the current user's build tasks + deployments
// across every AppProject they own into one timeline for the /ship
// page. Limits to the last ~60 days of activity to keep the payload
// bounded; older items are not surfaced on this page (they're still in
// the individual Launchpad builds feeds per app).
func (c *Controller) GetShipItems(ctx context.Context, user *models.User) (*responses.ShipResponse, error) {
	if user == nil {
		return nil, errors.New("user required")
	}

	// 1. Load the user's AppProjects; build a lookup of v1 project id -> app name.
	var projects []models.AppProject
	if err := c.DB.DB.NewSelect().
		Model(&projects).
		Where("user_id = ?", user.ID).
		Scan(ctx); err != nil {
		return nil, err
	}

	// Map v1_project_id (string) -> {AppProjectID, AppName}
	type projInfo struct {
		AppID   string
		AppName string
	}
	byV1 := map[string]projInfo{}
	v1IDs := make([]string, 0, len(projects))
	for _, p := range projects {
		v1, ok := p.Settings["v1_project_id"].(string)
		if !ok || v1 == "" {
			continue
		}
		byV1[v1] = projInfo{
			AppID:   p.ID.String(),
			AppName: p.Name,
		}
		v1IDs = append(v1IDs, v1)
	}
	if len(v1IDs) == 0 {
		return &responses.ShipResponse{Items: []responses.ShipItem{}}, nil
	}

	since := time.Now().Add(-60 * 24 * time.Hour)

	// 2. Load recent build tasks across all of the user's v1 projects.
	var tasks []models.AppBuildTask
	if err := c.DB.DB.NewSelect().
		Model(&tasks).
		Relation("Build").
		Where("project_id IN (?)", v1IDs).
		Where("created_at > ?", since).
		Order("created_at DESC").
		Scan(ctx); err != nil {
		return nil, err
	}

	// 3. Load recent deployments.
	var deployments []models.Deployment
	if err := c.DB.DB.NewSelect().
		Model(&deployments).
		Where("project_id IN (?)", v1IDs).
		Where("created_at > ?", since).
		Order("created_at DESC").
		Scan(ctx); err != nil {
		return nil, err
	}

	// 4. Merge into ShipItems.
	items := make([]responses.ShipItem, 0, len(tasks)+len(deployments))

	for _, t := range tasks {
		info, ok := byV1[t.ProjectID.String()]
		if !ok {
			continue
		}
		status, kind := classifyBuildStatus(t.StatusText, t.Finished, t.ErrorMessage)
		version := ""
		if t.Build != nil {
			if t.Build.VersionString != "" {
				version = t.Build.VersionString
				if t.Build.BuildNumber > 0 {
					version = fmt.Sprintf("%s (Build %d)", version, t.Build.BuildNumber)
				}
			}
		}
		items = append(items, responses.ShipItem{
			ID:         t.ID.String(),
			Kind:       "build",
			AppID:      info.AppID,
			AppName:    info.AppName,
			Platform:   t.Platform,
			Version:    version,
			Status:     status,
			StatusKind: kind,
			CreatedAt:  t.CreatedAt.UTC().Format(time.RFC3339),
			UpdatedAt:  t.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}

	for _, d := range deployments {
		info, ok := byV1[d.ProjectID.String()]
		if !ok {
			continue
		}
		store := ""
		platform := ""
		for _, p := range d.Platforms {
			pl := strings.ToLower(p)
			if pl == "ios" {
				store = "app_store"
				platform = "ios"
				break
			}
			if pl == "android" {
				store = "play_store"
				platform = "android"
				break
			}
		}
		status, kind := classifyDeploymentStatus(d.Status)
		items = append(items, responses.ShipItem{
			ID:         d.ID.String(),
			Kind:       "deployment",
			AppID:      info.AppID,
			AppName:    info.AppName,
			Store:      store,
			Platform:   platform,
			Version:    d.Title, // For deployments we don't have a separate version field, reuse title placeholder.
			Status:     status,
			StatusKind: kind,
			CreatedAt:  d.CreatedAt.UTC().Format(time.RFC3339),
			UpdatedAt:  d.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}

	// 5. Sort by UpdatedAt DESC (falls back to CreatedAt when UpdatedAt is zero).
	sort.Slice(items, func(i, j int) bool {
		return items[i].UpdatedAt > items[j].UpdatedAt
	})

	return &responses.ShipResponse{Items: items}, nil
}

// classifyBuildStatus maps the Codemagic status string to a human
// status + status_kind color-key.
func classifyBuildStatus(raw string, finished bool, errorMessage string) (string, string) {
	key := strings.ToLower(strings.TrimSpace(raw))
	if errorMessage != "" {
		return "Failed", "danger"
	}
	switch key {
	case "built", "success", "completed", "done":
		return "Built", "success"
	case "error", "failed":
		return "Failed", "danger"
	case "uploading":
		return "Uploading", "info"
	case "building", "reviewing":
		return "Building", "info"
	case "preparing":
		return "Preparing", "info"
	case "queued":
		return "Queued", "neutral"
	case "canceled", "cancelled":
		return "Canceled", "neutral"
	}
	if !finished {
		return "Building", "info"
	}
	return "Done", "neutral"
}

// classifyDeploymentStatus handles the Deployment.Status vocabulary
// (mixed case from Codemagic + managed-submission values).
func classifyDeploymentStatus(raw string) (string, string) {
	key := strings.ToLower(strings.TrimSpace(raw))
	switch key {
	case "deployed", "published", "live":
		return "Live", "success"
	case "submitted", "reviewing", "in_review":
		return "In review", "info"
	case "queued", "deploying":
		return "Submitting", "info"
	case "rejected":
		return "Rejected", "danger"
	case "error", "failed":
		return "Failed", "danger"
	}
	return raw, "neutral"
}
```

Adapt field names (`VersionString`, `BuildNumber`, etc.) to what's actually in `internal/models/builds.go`.

- [ ] **Step 3: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
go build ./...
go vet ./...
git add internal/controllers/ship_controller.go
git commit -m "feat(ship): GetShipItems aggregates cross-app builds + deployments

Joins user's AppProjects via settings.v1_project_id → v1 Project
scope, then loads AppBuildTask + Deployment rows from the last 60
days, classifies status + kind, sorts by UpdatedAt DESC.

Two small classification helpers map the mixed-case Codemagic status
vocabulary into a {status, status_kind} pair the frontend renders as
badge color."
```

---

## Task 3: Backend — HTTP handler + route

**Files:**
- Create: `internal/views/ship_views.go`
- Modify: `cmd/main.go`

- [ ] **Step 1: Handler**

Create `/Users/knid/Projects/wemob/wemob-backend/internal/views/ship_views.go`:

```go
package views

import (
	"net/http"

	"github.com/knid/wemob/internal/utils"
)

// GetShipItems handles GET /v1/ship.
func (v *HttpViews) GetShipItems(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}
	resp, err := v.Controller.GetShipItems(r.Context(), user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}
	utils.JSONResponse(w, resp, http.StatusOK)
}
```

- [ ] **Step 2: Register route**

In `cmd/main.go`, inside the `r.Route("/v1", ...)` subtree alongside the other top-level routes, add:

```go
r.Get("/ship", httpViews.GetShipItems)
```

Place near other top-level user-scoped routes (e.g., near `r.Get("/tasks", ...)`).

- [ ] **Step 3: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
go build ./...
go vet ./...
git add internal/views/ship_views.go cmd/main.go
git commit -m "feat(ship): wire GET /v1/ship handler

Auth-gated via GetUserFromReq, delegates to Controller.GetShipItems,
returns ShipResponse as 200 JSON. No subscription / credit gates —
this is a read-only observability surface."
```

---

## Task 4: Backend — pass `rollout_track` to Codemagic

**Files:**
- Modify: `internal/controllers/deployment.go` (the method that triggers Codemagic for iOS deploys)
- Modify: `internal/tasks/ios.go` (if the trigger shape is defined there)

- [ ] **Step 1: Inspect the Codemagic deploy trigger path**

```bash
grep -n "CM_PACKAGE_NAME\|ios-deploy\|ios_deploy\|TriggerCodemagic\|workflowId.*ios" /Users/knid/Projects/wemob/wemob-backend/internal/controllers/deployment.go /Users/knid/Projects/wemob/wemob-backend/internal/tasks/ios.go /Users/knid/Projects/wemob/wemob-backend/internal/services/codemagic/*.go 2>/dev/null | head -20
```

Identify the function that builds the Codemagic trigger body (the `environment.variables` map that becomes `CM_*` env vars in the workflow). It may live in a codemagic service, controllers/deployment.go, or tasks/ios.go.

- [ ] **Step 2: Add `CM_ROLLOUT_TRACK` to the env map**

In the ios-deploy trigger code, where `CM_PACKAGE_NAME`, `CM_KEY_ID`, etc. are set, add:

```go
if deployment.RolloutTrack != "" {
    vars["CM_ROLLOUT_TRACK"] = deployment.RolloutTrack
}
```

If vars is a different map name (e.g., `envVars`, `variables`), use that.

- [ ] **Step 3: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
go build ./...
go vet ./...
git add internal/controllers/deployment.go internal/tasks/ios.go internal/services/codemagic/
git commit -m "feat(ship): forward deployment.RolloutTrack as CM_ROLLOUT_TRACK env var

ios-deploy workflow reads CM_ROLLOUT_TRACK to decide between pilot
(TestFlight) and deliver (App Store Review). Backend passes whatever
QuickShip captured; empty/absent means keep the legacy deliver path.

Phase 4c follow-up in flutter-template branches the Fastfile based on
this env var."
```

---

## Task 5: Flutter-template — `pilot` for TestFlight tracks

**Files:**
- Modify: `/Users/knid/Projects/wemob/wemob-flutter-template/codemagic.yaml`

- [ ] **Step 1: Branch ios-deploy Fastfile on `CM_ROLLOUT_TRACK`**

Branch from `feat/ship-ux-phase-3c` (which has the Phase 1 precheck + Phase 3c produce work stacked).

In the ios-deploy workflow's Fastfile generator, find the `deliver(...)` block(s). When `ENV['CM_ROLLOUT_TRACK']` is `"internal"` or `"closed"` (TestFlight), route through `pilot` instead of `deliver`. Otherwise (empty or `"production"`), keep the existing deliver path.

Inside the Fastfile heredoc, replace the unconditional `deliver(...)` blocks with:

```ruby
rollout_track = ENV['CM_ROLLOUT_TRACK'].to_s.downcase

if ['internal', 'closed'].include?(rollout_track)
  # TestFlight via pilot.
  pilot(
    api_key: api_key,
    app_identifier: ENV['CM_PACKAGE_NAME'],
    ipa: "/tmp/signed.ipa",
    skip_submission: rollout_track == 'closed' ? false : true,
    distribute_external: rollout_track == 'closed',
    notify_external_testers: rollout_track == 'closed',
    changelog: ENV['CM_RELEASE_NOTES'] || "Uploaded via Wemob",
  )
else
  # App Store Review — existing path.
  #   (keep existing deliver(...) blocks here unchanged)
  if UPLOAD_BINARY == "true"
    deliver(
      api_key: api_key,
      force: true,
      ipa: "/tmp/signed.ipa",
      skip_metadata: true,
      skip_screenshots: true,
      app_identifier: ENV['CM_PACKAGE_NAME'],
      precheck_include_in_app_purchases: false,
    )
  end
  # The metadata-only deliver call also stays in the else branch:
  deliver(
    api_key: api_key,
    force: true,
    skip_binary_upload: true,
    app_identifier: ENV['CM_PACKAGE_NAME'],
    team_id: ENV['CM_TEAM_ID'],
    precheck_include_in_app_purchases: false,
    skip_metadata: SKIP_META,
    skip_screenshots: SKIP_SS,
  )
end
```

Preserve the bash-interpolated variables ($UPLOAD_BINARY, $SKIP_META, $SKIP_SS) exactly as they appear in the existing heredoc — they come from the earlier bash lines that compute these flags based on CM_NEW_APP etc. The Ruby `if ['internal', 'closed'].include?(rollout_track)` wraps those blocks.

Note: the pilot call references `ENV['CM_RELEASE_NOTES']` for the TestFlight changelog. If the backend currently passes release notes via a different env var name, adapt.

- [ ] **Step 2: Pass `CM_RELEASE_NOTES` from backend**

If Task 4's env map doesn't yet include release notes, add it:

```go
if deployment.ReleaseNotes != "" {
    vars["CM_RELEASE_NOTES"] = deployment.ReleaseNotes
}
```

Put this alongside the RolloutTrack assignment from Task 4.

- [ ] **Step 3: Validate YAML + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-flutter-template
ruby -e "require 'yaml'; YAML.load_file('codemagic.yaml'); puts 'ok'"
git add codemagic.yaml
git commit -m "feat(ci): branch ios-deploy between pilot (TestFlight) and deliver (App Store)

When CM_ROLLOUT_TRACK is 'internal' or 'closed', the lane publishes
via fastlane pilot to the TestFlight Internal / External tracks —
distribute_external + notify_external_testers toggle on External.
Otherwise keeps the existing deliver (App Store Review) path.

CM_RELEASE_NOTES threads through as the TestFlight changelog; empty
defaults to a generic 'Uploaded via Wemob' marker."
```

Then go back to wemob-backend and include `CM_RELEASE_NOTES` in its deploy-trigger env vars. Commit that:

```bash
cd /Users/knid/Projects/wemob/wemob-backend
go build ./...
go vet ./...
git add internal/controllers/deployment.go internal/tasks/ios.go
git commit -m "feat(ship): pass CM_RELEASE_NOTES to Codemagic deploy trigger

Paired with flutter-template's new pilot() block that expects this
env var as the TestFlight changelog. Empty/absent falls back to the
generic 'Uploaded via Wemob' marker in the Fastfile."
```

---

## Task 6: Frontend — API method + shared types

**Files:**
- Create: `src/lib/ship/ship-types.ts`
- Modify: `src/lib/api.ts`

- [ ] **Step 1: Shared types**

`src/lib/ship/ship-types.ts`:

```ts
export type ShipItemKind = "build" | "deployment";
export type ShipItemStatusKind = "success" | "info" | "warn" | "danger" | "neutral";

export interface ShipItem {
  id: string;
  kind: ShipItemKind;
  app_id: string;
  app_name: string;
  store?: "play_store" | "app_store" | "";
  platform?: "android" | "ios";
  version?: string;
  status: string;
  status_kind: ShipItemStatusKind;
  eta_seconds?: number;
  created_at: string;
  updated_at?: string;
}

export interface ShipResponse {
  items: ShipItem[];
}
```

- [ ] **Step 2: API method**

Add after `draftListing` in `src/lib/api.ts`:

```ts
async listShipItems(): Promise<import("./ship/ship-types").ShipResponse> {
  return this.request<import("./ship/ship-types").ShipResponse>("/v1/ship");
}
```

- [ ] **Step 3: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-app
npm run build
git add src/lib/ship/ship-types.ts src/lib/api.ts
git commit -m "feat(ship): add listShipItems API method + ShipItem types

Mirrors GET /v1/ship response. Used by the /ship cross-app table."
```

---

## Task 7: Frontend — `useShipItems` hook

**Files:**
- Create: `src/hooks/use-ship-items.ts`

- [ ] **Step 1: Write the hook**

```ts
"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { apiClient } from "@/lib/api";
import type { ShipResponse } from "@/lib/ship/ship-types";

export function useShipItems() {
  const [data, setData] = useState<ShipResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);
  const cancelledRef = useRef(false);

  const fetchOnce = useCallback(async () => {
    try {
      const res = await apiClient.listShipItems();
      if (cancelledRef.current) return;
      setData(res);
      setError(null);
    } catch (err) {
      if (cancelledRef.current) return;
      setError(err instanceof Error ? err : new Error(String(err)));
    } finally {
      if (!cancelledRef.current) setLoading(false);
    }
  }, []);

  useEffect(() => {
    cancelledRef.current = false;
    setLoading(true);
    fetchOnce();
    return () => {
      cancelledRef.current = true;
    };
  }, [fetchOnce]);

  // Poll every 10s while any item is in an in-flight state.
  useEffect(() => {
    if (!data) return;
    const inflight = data.items.some((i) => i.status_kind === "info");
    if (!inflight) return;
    const handle = setInterval(() => fetchOnce(), 10_000);
    return () => clearInterval(handle);
  }, [data, fetchOnce]);

  return { data, loading, error, refetch: fetchOnce };
}
```

- [ ] **Step 2: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-app
npm run build
git add src/hooks/use-ship-items.ts
git commit -m "feat(ship): add useShipItems hook

Fetches /v1/ship on mount, polls every 10s while any item is in an
in-flight (info-kind) state. Same pattern as useLaunchpad but for the
cross-app surface."
```

---

## Task 8: Frontend — `ShipTable` + `ShipTableFilters`

**Files:**
- Create: `src/components/ship/ship-table.tsx`
- Create: `src/components/ship/ship-table-filters.tsx`

- [ ] **Step 1: ShipTableFilters**

```tsx
"use client";

import type { ShipItem, ShipItemStatusKind } from "@/lib/ship/ship-types";
import { cn } from "@/lib/utils";

export type ShipFilterKey = "all" | "in_progress" | "live" | "failed";

interface ShipTableFiltersProps {
  items: ShipItem[];
  value: ShipFilterKey;
  onChange: (f: ShipFilterKey) => void;
}

const FILTERS: { key: ShipFilterKey; label: string; kinds: ShipItemStatusKind[] }[] = [
  { key: "all", label: "All", kinds: ["success", "info", "warn", "danger", "neutral"] },
  { key: "in_progress", label: "In progress", kinds: ["info"] },
  { key: "live", label: "Live", kinds: ["success"] },
  { key: "failed", label: "Failed", kinds: ["danger"] },
];

export function ShipTableFilters({ items, value, onChange }: ShipTableFiltersProps) {
  return (
    <div className="flex items-center gap-1.5">
      {FILTERS.map((f) => {
        const count = items.filter((i) => f.kinds.includes(i.status_kind)).length;
        const active = value === f.key;
        return (
          <button
            key={f.key}
            type="button"
            onClick={() => onChange(f.key)}
            className={cn(
              "px-2.5 py-1 rounded-full text-[11px] font-medium transition-colors",
              active
                ? "bg-[var(--ink)] text-white"
                : "bg-[var(--bg-raised)] text-[var(--ink-muted)] hover:bg-[var(--bg-hover)]",
            )}
          >
            {f.label} · {count}
          </button>
        );
      })}
    </div>
  );
}

export function applyShipFilter(items: ShipItem[], key: ShipFilterKey): ShipItem[] {
  const filter = FILTERS.find((f) => f.key === key);
  if (!filter || key === "all") return items;
  return items.filter((i) => filter.kinds.includes(i.status_kind));
}
```

- [ ] **Step 2: ShipTable**

```tsx
"use client";

import Link from "next/link";
import type { ShipItem, ShipItemStatusKind } from "@/lib/ship/ship-types";
import { cn } from "@/lib/utils";

interface ShipTableProps {
  items: ShipItem[];
}

export function ShipTable({ items }: ShipTableProps) {
  if (items.length === 0) {
    return (
      <div className="border border-dashed border-[var(--border)] rounded-xl p-10 text-center text-[13px] text-[var(--ink-muted)]">
        Nothing to ship right now.
      </div>
    );
  }

  return (
    <div className="border border-[var(--border)] rounded-xl overflow-hidden">
      <table className="w-full text-[12px]">
        <thead className="bg-[var(--bg-raised)]">
          <tr className="text-[10px] uppercase tracking-[0.5px] text-[var(--ink-faint)]">
            <th className="text-left px-4 py-2 font-medium">App</th>
            <th className="text-left px-4 py-2 font-medium">Target</th>
            <th className="text-left px-4 py-2 font-medium">Version</th>
            <th className="text-left px-4 py-2 font-medium">Status</th>
            <th className="text-left px-4 py-2 font-medium">Updated</th>
          </tr>
        </thead>
        <tbody>
          {items.map((i) => (
            <tr key={i.id} className="border-t border-[var(--border)] hover:bg-[var(--bg-hover)]">
              <td className="px-4 py-2.5">
                <Link
                  href={`/apps/${i.app_id}/ship`}
                  className="text-[var(--ink)] hover:underline"
                >
                  {i.app_name}
                </Link>
              </td>
              <td className="px-4 py-2.5 text-[var(--ink-muted)]">{targetLabel(i)}</td>
              <td className="px-4 py-2.5 font-mono text-[11px] text-[var(--ink-muted)]">
                {i.version ?? "—"}
              </td>
              <td className="px-4 py-2.5">
                <span
                  className={cn(
                    "inline-flex items-center px-2 py-0.5 rounded-full text-[10px] font-medium whitespace-nowrap",
                    statusBadge(i.status_kind),
                  )}
                >
                  {i.status}
                </span>
              </td>
              <td className="px-4 py-2.5 text-[var(--ink-faint)]">{formatRelative(i.updated_at ?? i.created_at)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function targetLabel(i: ShipItem): string {
  if (i.kind === "deployment") {
    if (i.store === "play_store") return "Play Store";
    if (i.store === "app_store") return "App Store";
    return "Store";
  }
  if (i.platform === "android") return "Android build";
  if (i.platform === "ios") return "iOS build";
  return "Build";
}

function statusBadge(kind: ShipItemStatusKind): string {
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

- [ ] **Step 3: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-app
npm run build
git add src/components/ship/ship-table.tsx src/components/ship/ship-table-filters.tsx
git commit -m "feat(ship): add ShipTable + filters for the /ship cross-app surface

Table columns: App · Target · Version · Status · Updated. Row click
jumps to the app's Launchpad. Filters are pills with per-filter
counts (All / In progress / Live / Failed) mapped from status_kind."
```

---

## Task 9: Frontend — `/ship` page + flag-gated `/builds` redirect

**Files:**
- Create: `src/app/ship/page.tsx`
- Modify: `src/app/builds/page.tsx`

- [ ] **Step 1: `/ship` page**

```tsx
"use client";

import { useState } from "react";
import { AuthGuard } from "@/components/auth/auth-guard";
import { StudioShell } from "@/components/layout/studio-shell";
import { ShipTable } from "@/components/ship/ship-table";
import {
  ShipTableFilters,
  type ShipFilterKey,
  applyShipFilter,
} from "@/components/ship/ship-table-filters";
import { useFeatureFlag } from "@/hooks/use-feature-flag";
import { useShipItems } from "@/hooks/use-ship-items";
import { useRouter } from "next/navigation";
import { useEffect } from "react";

export default function ShipPage() {
  return (
    <AuthGuard>
      <StudioShell>
        <ShipInner />
      </StudioShell>
    </AuthGuard>
  );
}

function ShipInner() {
  const router = useRouter();
  const flagEnabled = useFeatureFlag("new_ship_ux");
  const { data, loading, error } = useShipItems();
  const [filter, setFilter] = useState<ShipFilterKey>("all");

  useEffect(() => {
    if (flagEnabled === false) {
      router.replace("/builds");
    }
  }, [flagEnabled, router]);

  if (flagEnabled === undefined) {
    return (
      <div className="flex-1 flex items-center justify-center text-[13px] text-[var(--ink-faint)]">
        Loading…
      </div>
    );
  }
  if (flagEnabled === false) return null;

  const items = data?.items ?? [];
  const filtered = applyShipFilter(items, filter);

  return (
    <div className="flex-1 overflow-y-auto">
      <div className="px-8 py-6 pb-16 max-w-[1120px] mx-auto">
        <header className="flex items-center gap-4 mb-4">
          <h1 className="font-serif text-[26px] tracking-tight">Ship</h1>
          <p className="text-[12px] text-[var(--ink-muted)]">
            Every build and submission across your apps.
          </p>
        </header>

        <div className="mb-3">
          <ShipTableFilters items={items} value={filter} onChange={setFilter} />
        </div>

        {loading && !data ? (
          <div className="text-[12px] text-[var(--ink-faint)]">Loading ship items…</div>
        ) : error ? (
          <div className="text-[12px] text-[var(--accent)]">Couldn&apos;t load: {error.message}</div>
        ) : (
          <ShipTable items={filtered} />
        )}
      </div>
    </div>
  );
}
```

- [ ] **Step 2: `/builds` redirect when flag on**

Open `/Users/knid/Projects/wemob/wemob-app/src/app/builds/page.tsx`. At the TOP of the component body, add a flag check that redirects to `/ship`:

```tsx
"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { useFeatureFlag } from "@/hooks/use-feature-flag";
// ... existing imports

export default function BuildsPage() {
  const router = useRouter();
  const flagEnabled = useFeatureFlag("new_ship_ux");

  useEffect(() => {
    if (flagEnabled === true) {
      router.replace("/ship");
    }
  }, [flagEnabled, router]);

  if (flagEnabled === true) return null;

  // ... existing JSX
}
```

Preserve all existing `BuildsPage` behavior when the flag is off.

- [ ] **Step 3: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-app
npm run lint
npm run build
git add src/app/ship/page.tsx src/app/builds/page.tsx
git commit -m "feat(ship): add /ship page + flag-gated /builds redirect

/ship is the new cross-app table. It's gated by new_ship_ux — users
without the flag get redirected back to the legacy /builds page.
Conversely, users with the flag on who land on /builds are bounced
to /ship so the two routes never coexist for the same user."
```

---

## Task 10: Frontend — add "All builds & submissions" to the command palette

**Files:**
- Modify: `src/lib/ship/commands.ts`

- [ ] **Step 1: Add the global command**

Open `/Users/knid/Projects/wemob/wemob-app/src/lib/ship/commands.ts`. The `buildAppCommands` function currently returns 5 app-scoped entries; append one more that navigates to `/ship`:

```ts
{
  id: "all-builds-submissions",
  label: "All builds & submissions",
  hint: "/ship — cross-app table",
  shortcut: "g b",
  kind: "secondary",
  scope: "global",
  run: () => {
    if (typeof window !== "undefined") {
      window.location.href = "/ship";
    }
  },
},
```

- [ ] **Step 2: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-app
npm run build
git add src/lib/ship/commands.ts
git commit -m "feat(ship): add 'All builds & submissions' global command

Sixth command in the ⌘K palette — jumps to the new /ship cross-app
table. Labeled 'g b' as the intended shortcut (gb = 'go to builds');
the keyboard shortcut wiring follows in 4d if chord shortcuts land."
```

---

## Task 11: End-to-end smoke (documented)

```
# Backend
cd /Users/knid/Projects/wemob/wemob-backend
docker-compose -f docker-compose-dev.yaml up -d
go run cmd/main.go &

# Frontend
cd /Users/knid/Projects/wemob/wemob-app && npm run dev

# Browser tests
# 1. Navigate to /ship (with new_ship_ux flag on) — see cross-app table with filters.
# 2. Click a row — land on that app's Launchpad.
# 3. Navigate to /builds (with flag on) — auto-redirect to /ship.
# 4. ⌘K → search "builds" → hit Enter on "All builds & submissions" — lands on /ship.
# 5. With the flag off, /ship redirects to /builds and the legacy page renders.
```

TestFlight smoke (live only — requires a real Apple Developer account):
```
# With QuickShip, pick Rollout: TestFlight · Internal. Ship.
# Codemagic workflow runs; fastlane pilot publishes the IPA to TestFlight internal.
# Verify in App Store Connect → TestFlight → Internal Testing.
```

- [ ] **Step 1: No commit — smoke is user-run.**

---

## Phase 4c completion criteria

- Backend: `GET /v1/ship` returns merged build + deployment items; `CM_ROLLOUT_TRACK` and `CM_RELEASE_NOTES` passed to the ios-deploy Codemagic trigger.
- Flutter-template: ios-deploy Fastfile branches on `CM_ROLLOUT_TRACK` between `pilot` (TestFlight) and `deliver` (App Store).
- Frontend: `/ship` page lists cross-app items with filters; row click navigates to Launchpad. `/builds` redirects to `/ship` when flag is on. Command palette has "All builds & submissions" entry.
- Build / vet / lint clean.
- No regressions.

Phase 5 — managed observability timeline, real ETAs, email nudges, store-polling worker — unblocked.

## Out of scope

- Chord keyboard shortcuts (`g b` to actually trigger navigation from keyboard). Phase 4d if needed.
- Paginating `/v1/ship` past 60 days.
- Live-updating rows via server-sent events (polling at 10s is fine for 4c).
- Phase 5 managed observability work.
