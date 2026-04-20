# Ship UX — Phase 5a · Deployment Event Stream + Timeline UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the Phase 1 `deployment_events` table into something users actually see. Write events at the key moments (submitted / uploaded_to_store / review_started / live / rejected / failed / canceled), expose them via `GET /v1/deployments/:id/events`, and render a vertical timeline on the Launchpad for any recent deployment. Managed-track deployments get human attribution when a Wemob team member triages them.

**Architecture:** Event writes are append-only `DeploymentEvent` rows. A small helper package writes events from the existing deployment hot paths (create, status update, managed triage) — no new tables, no background workers. One new read endpoint. Frontend adds a `DeploymentTimeline` component that the Launchpad hosts as a new row above the Builds feed when the aggregate endpoint signals there's a recent deployment to track.

**Tech stack:** Go 1.22+, Bun, chi/v5. Next.js 16, React 19, Tailwind 4.

**Design spec:** `docs/design/2026-04-18-wemob-publish-ux-redesign-design.md` ("Managed observability — timeline, not a status string").

---

## Test strategy

Same discipline: `go build` + `go vet` clean, `npm run build` + lint clean, runtime smoke documented for user.

## File structure overview

### Backend — new files

```
internal/database/deployment_events.go           — AppendDeploymentEvent + ListDeploymentEvents helpers
internal/controllers/deployment_events_controller.go  — GetDeploymentEvents
internal/views/deployment_events_views.go        — HTTP handler
internal/responses/deployment_events.go          — response shape
```

### Backend — modified

```
cmd/main.go                                      — register GET /v1/deployments/:id/events
internal/controllers/deployment.go               — write events at create + status update points
internal/controllers/reports.go                  — write events when Codemagic reports deployment status transitions
internal/responses/launchpad.go                  — extend Launchpad with recent-deployment summary
internal/controllers/launchpad_controller.go     — populate that summary
```

### Frontend — new files

```
src/components/ship/deployment-timeline.tsx      — vertical timeline component
src/hooks/use-deployment-events.ts               — fetch + 15s poll while active
src/lib/ship/deployment-event-types.ts           — shared TS types
```

### Frontend — modified

```
src/lib/api.ts                                   — listDeploymentEvents method
src/lib/ship/launchpad-types.ts                  — extend with LaunchpadRecentDeployment
src/components/ship/launchpad.tsx                — host timeline when recent deployment exists
```

## Why these boundaries

- **`internal/database/deployment_events.go`** owns reads + writes against the table; no ORM calls in controllers. Keeps the event semantics in one place so Phase 5b's polling worker reuses the same writers.
- **Events write from existing hot paths** (create, status update, reports). No event bus, no goroutines, no async. Phase 5b introduces async polling if the user owns the credentials.
- **Timeline surface on the Launchpad, not a standalone page.** Users care about "what's happening to this submission" in the context of the app they're shipping; a dedicated page would be out of flow.
- **No new models**. Phase 1 already built `DeploymentEvent` — 5a just lights it up.

---

## Task 1: DB helpers + response types

**Files:**
- Create: `internal/database/deployment_events.go`
- Create: `internal/responses/deployment_events.go`

- [ ] **Step 1: Write helpers**

`internal/database/deployment_events.go`:

```go
package database

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/uptrace/bun"
)

// AppendDeploymentEvent appends a new row to deployment_events.
// actorID is optional — pass uuid.Nil for system-originated events
// that don't have a User to attribute to (the system / store_api
// paths). Metadata may be nil; callers pass a map with context-specific
// keys (e.g., "reviewer_name" for triage events).
func AppendDeploymentEvent(
	ctx context.Context,
	db *bun.DB,
	deploymentID uuid.UUID,
	eventType, actorType string,
	actorID *uuid.UUID,
	metadata map[string]interface{},
) error {
	ev := models.DeploymentEvent{
		DeploymentID: deploymentID,
		EventType:    eventType,
		ActorType:    actorType,
		ActorID:      actorID,
		Metadata:     metadata,
		OccurredAt:   time.Now().UTC(),
	}
	_, err := db.NewInsert().Model(&ev).Exec(ctx)
	return err
}

// ListDeploymentEvents returns all events for a deployment, oldest
// first (matches how a timeline reads top → bottom).
func ListDeploymentEvents(
	ctx context.Context,
	db *bun.DB,
	deploymentID uuid.UUID,
) ([]models.DeploymentEvent, error) {
	var events []models.DeploymentEvent
	err := db.NewSelect().
		Model(&events).
		Where("deployment_id = ?", deploymentID).
		Order("occurred_at ASC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return events, nil
}
```

- [ ] **Step 2: Response type**

`internal/responses/deployment_events.go`:

```go
package responses

// DeploymentEventItem is one rendered event in the UI timeline.
// Flattens the model's metadata into a handful of top-level fields
// the frontend actually uses, so the timeline component doesn't need
// to parse a generic JSON object.
type DeploymentEventItem struct {
	ID         string `json:"id"`
	EventType  string `json:"event_type"`   // stable enum, e.g. "submitted", "store_live"
	ActorType  string `json:"actor_type"`   // "user" | "wemob" | "system" | "store_api"
	ActorName  string `json:"actor_name,omitempty"`  // when actor_type is "wemob" — Wemob reviewer name
	Note       string `json:"note,omitempty"`        // freeform annotation from the event
	OccurredAt string `json:"occurred_at"`  // RFC3339
}

type DeploymentEventsResponse struct {
	Items []DeploymentEventItem `json:"items"`
}
```

- [ ] **Step 3: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
go build ./...
go vet ./...
git add internal/database/deployment_events.go internal/responses/deployment_events.go
git commit -m "feat(ship): add DeploymentEvent DB helpers + response types

AppendDeploymentEvent writes one row; ListDeploymentEvents reads the
ordered stream. DeploymentEventItem flattens the actor_name + note
fields from metadata so the frontend timeline renders without
traversing a generic map."
```

---

## Task 2: Write events at the deployment hot paths

**Files:**
- Modify: `internal/controllers/deployment.go` (CreateDeploymentTask + UpdateDeploymentTask)
- Modify: `internal/controllers/reports.go` (UpdateDeploymentStatus report handler)

- [ ] **Step 1: Inspect the hot paths**

Read the full CreateDeploymentTask and UpdateDeploymentTask in `internal/controllers/deployment.go`, plus `UpdateDeploymentStatus` in `internal/controllers/reports.go`. Note:

- CreateDeploymentTask creates a new `Deployment` row and triggers the Codemagic workflow.
- UpdateDeploymentTask updates an existing `Deployment` row (e.g., re-submissions).
- UpdateDeploymentStatus is called by Codemagic with deployment status transitions.

- [ ] **Step 2: Write `submitted` event on create**

In `CreateDeploymentTask`, after the `c.DB.CreateDeployment(deployment)` succeeds, append:

```go
// Event: submitted (actor = user)
actorID := user.ID
if err := database.AppendDeploymentEvent(ctx, c.DB.DB, deployment.ID,
    models.DeploymentEventSubmitted, models.DeploymentEventActorUser, &actorID,
    map[string]interface{}{"method": deployment.Method}); err != nil {
    slog.Warn("append deployment_events.submitted failed", "err", err)
}
```

Import `database` if not already. Use the `models.DeploymentEvent*` constants defined in Phase 1.

Adapt the `user` / `ctx` variables to match the function's actual parameter names. If the function doesn't currently take a context, use `context.Background()` (consistent with other startup hooks in the codebase).

- [ ] **Step 3: Write status-change events in UpdateDeploymentStatus**

In `UpdateDeploymentStatus` (reports.go), when the reported status changes the Deployment row, map the status to an event type and write:

```go
switch strings.ToLower(reportDeployReq.Status) {
case "deployed", "live", "published":
    _ = database.AppendDeploymentEvent(ctx, c.DB.DB, deployment.ID,
        models.DeploymentEventStoreLive, models.DeploymentEventActorSystem, nil,
        map[string]interface{}{"note": reportDeployReq.Message})
case "reviewing", "submitted":
    _ = database.AppendDeploymentEvent(ctx, c.DB.DB, deployment.ID,
        models.DeploymentEventStoreReviewStarted, models.DeploymentEventActorSystem, nil,
        map[string]interface{}{"note": reportDeployReq.Message})
case "rejected":
    _ = database.AppendDeploymentEvent(ctx, c.DB.DB, deployment.ID,
        models.DeploymentEventStoreRejected, models.DeploymentEventActorSystem, nil,
        map[string]interface{}{"note": reportDeployReq.Message})
case "error", "failed":
    _ = database.AppendDeploymentEvent(ctx, c.DB.DB, deployment.ID,
        models.DeploymentEventFailed, models.DeploymentEventActorSystem, nil,
        map[string]interface{}{"note": reportDeployReq.Message})
case "deploying", "queued":
    // Map first "deploying" transition to uploaded_to_store.
    _ = database.AppendDeploymentEvent(ctx, c.DB.DB, deployment.ID,
        models.DeploymentEventUploadedToStore, models.DeploymentEventActorSystem, nil,
        map[string]interface{}{"note": reportDeployReq.Message})
}
```

Adapt to the actual request field names (`reportDeployReq.Status`, `reportDeployReq.Message`) and the actual status vocabulary used by the deploy report endpoint (inspect the function's existing switch / comparison logic and mirror it).

Don't deduplicate events — even if multiple reports come in, the timeline can render all of them. Phase 5b's polling worker will use `only write when the type actually changes` if noise becomes a problem.

- [ ] **Step 4: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
go build ./...
go vet ./...
git add internal/controllers/deployment.go internal/controllers/reports.go
git commit -m "feat(ship): write deployment_events at submit + status-transition hot paths

CreateDeploymentTask emits {submitted, actor=user} with method in
metadata. UpdateDeploymentStatus (the Codemagic report webhook)
emits {uploaded_to_store, store_review_started, store_live,
store_rejected, failed} based on reported status — actor=system.

No deduplication — the timeline can render every event; Phase 5b
tightens when/if noise becomes a problem."
```

---

## Task 3: Read endpoint

**Files:**
- Create: `internal/controllers/deployment_events_controller.go`
- Create: `internal/views/deployment_events_views.go`
- Modify: `cmd/main.go`

- [ ] **Step 1: Controller**

`internal/controllers/deployment_events_controller.go`:

```go
package controllers

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
)

// GetDeploymentEvents returns the ordered event stream for a
// deployment. Enforces ownership — user must own the v1 Project the
// deployment belongs to.
func (c *Controller) GetDeploymentEvents(ctx context.Context, user *models.User, deploymentID uuid.UUID) (*responses.DeploymentEventsResponse, error) {
	if user == nil {
		return nil, errors.New("user required")
	}

	// Ownership check: load the Deployment, verify the project is owned
	// by the user.
	var deployment models.Deployment
	if err := c.DB.DB.NewSelect().
		Model(&deployment).
		Where("id = ?", deploymentID).
		Scan(ctx); err != nil {
		return nil, err
	}
	var project models.Project
	if err := c.DB.DB.NewSelect().
		Model(&project).
		Where("id = ?", deployment.ProjectID).
		Scan(ctx); err != nil {
		return nil, err
	}
	if project.UserID != user.ID {
		return nil, errors.New("not authorized")
	}

	events, err := database.ListDeploymentEvents(ctx, c.DB.DB, deploymentID)
	if err != nil {
		return nil, err
	}

	items := make([]responses.DeploymentEventItem, 0, len(events))
	for _, ev := range events {
		item := responses.DeploymentEventItem{
			ID:         ev.ID.String(),
			EventType:  ev.EventType,
			ActorType:  ev.ActorType,
			OccurredAt: ev.OccurredAt.UTC().Format("2006-01-02T15:04:05Z"),
		}
		if name, ok := ev.Metadata["reviewer_name"].(string); ok {
			item.ActorName = name
		}
		if note, ok := ev.Metadata["note"].(string); ok {
			item.Note = note
		}
		items = append(items, item)
	}
	return &responses.DeploymentEventsResponse{Items: items}, nil
}
```

Adapt the `models.Project` field + query if it's different (e.g., if the project-owner join is through v1 `projects` table vs `app_projects`). Inspect existing deployment CRUD ownership checks to match the actual pattern.

- [ ] **Step 2: Handler**

`internal/views/deployment_events_views.go`:

```go
package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/utils"
)

// GetDeploymentEvents handles GET /v1/deployments/{deploymentId}/events.
func (v *HttpViews) GetDeploymentEvents(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}
	idStr := chi.URLParam(r, "deploymentId")
	id, err := uuid.Parse(idStr)
	if err != nil {
		utils.JSONErrorResponse(w, "invalid deployment_id", http.StatusBadRequest)
		return
	}
	resp, err := v.Controller.GetDeploymentEvents(r.Context(), user, id)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}
	utils.JSONResponse(w, resp, http.StatusOK)
}
```

- [ ] **Step 3: Register route**

In `cmd/main.go`, find the `/deployments/{deploymentId}` subtree (already used by `GetDeployment`, `UpdateDeployment`). Add:

```go
r.Get("/{deploymentId}/events", httpViews.GetDeploymentEvents)
```

If there's no per-deployment subroute yet, add one inside the existing `/deployments` route that also carries the `/tools` routes from Phase 1.

- [ ] **Step 4: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
go build ./...
go vet ./...
git add internal/controllers/deployment_events_controller.go internal/views/deployment_events_views.go cmd/main.go
git commit -m "feat(ship): wire GET /v1/deployments/:id/events

Auth-gated, ownership-checked via the deployment's Project ownership.
Returns DeploymentEventItems with actor_name + note flattened from
the event's metadata so the frontend timeline renders without
traversing a generic map."
```

---

## Task 4: Extend Launchpad with recent deployment summary

**Files:**
- Modify: `internal/responses/launchpad.go`
- Modify: `internal/controllers/launchpad_controller.go`

- [ ] **Step 1: Types**

In `internal/responses/launchpad.go`, add near the existing `LaunchpadStoreTile`:

```go
// LaunchpadRecentDeployment surfaces the most-recent in-flight or
// recently-concluded deployment for this app so the Launchpad can
// render a timeline row above the Builds feed.
type LaunchpadRecentDeployment struct {
	ID           string `json:"id"`
	Store        string `json:"store"`           // "play_store" | "app_store"
	Status       string `json:"status"`          // human label, e.g. "In review"
	StatusKind   string `json:"status_kind"`     // 5-variant color key
	SubmittedAt  string `json:"submitted_at"`
	Method       string `json:"method"`          // "wemob" | "user"
}
```

Extend `LaunchpadResponse`:

```go
// Recent is the latest submission worth surfacing on the Launchpad
// (nil means nothing in flight + nothing concluded in the last 7 days).
RecentDeployment *LaunchpadRecentDeployment `json:"recent_deployment,omitempty"`
```

- [ ] **Step 2: Populate in `GetLaunchpad`**

In `internal/controllers/launchpad_controller.go`, after `resp.Builds = c.buildBuilds(...)` and before `resp.Identity = ...`, add a helper call:

```go
resp.RecentDeployment = c.buildRecentDeployment(ctx, v1ProjectID)
```

Define the helper below `buildIdentity`:

```go
func (c *Controller) buildRecentDeployment(ctx context.Context, v1ProjectID string) *responses.LaunchpadRecentDeployment {
    if v1ProjectID == "" {
        return nil
    }
    cutoff := time.Now().Add(-7 * 24 * time.Hour)
    var d models.Deployment
    err := c.DB.DB.NewSelect().
        Model(&d).
        Where("project_id = ?", v1ProjectID).
        Where("created_at > ?", cutoff).
        Order("created_at DESC").
        Limit(1).
        Scan(ctx)
    if err != nil {
        return nil
    }

    store := ""
    for _, p := range d.Platforms {
        pl := strings.ToLower(p)
        if pl == "ios" {
            store = "app_store"
            break
        }
        if pl == "android" {
            store = "play_store"
            break
        }
    }

    status, kind := classifyDeploymentStatus(d.Status)
    return &responses.LaunchpadRecentDeployment{
        ID:          d.ID.String(),
        Store:       store,
        Status:      status,
        StatusKind:  kind,
        SubmittedAt: d.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
        Method:      d.Method,
    }
}
```

`classifyDeploymentStatus` was added in Phase 4c (in `ship_controller.go`). If it's not exported, inline a copy here or export the existing one — your call.

- [ ] **Step 3: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
go build ./...
go vet ./...
git add internal/responses/launchpad.go internal/controllers/launchpad_controller.go
git commit -m "feat(ship): include recent deployment in Launchpad aggregate

Adds LaunchpadResponse.recent_deployment — the most-recent in-flight
or recently-concluded submission for this app (7-day window). Nil
when nothing's in flight.

Frontend hosts a DeploymentTimeline on the Launchpad for any non-nil
recent_deployment."
```

---

## Task 5: Frontend types + API method + hook

**Files:**
- Create: `src/lib/ship/deployment-event-types.ts`
- Create: `src/hooks/use-deployment-events.ts`
- Modify: `src/lib/api.ts`
- Modify: `src/lib/ship/launchpad-types.ts`

- [ ] **Step 1: Types**

`src/lib/ship/deployment-event-types.ts`:

```ts
export type DeploymentEventType =
  | "submitted"
  | "triaged"
  | "uploaded_to_store"
  | "store_review_started"
  | "store_live"
  | "store_rejected"
  | "canceled"
  | "failed";

export type DeploymentActorType = "user" | "wemob" | "system" | "store_api";

export interface DeploymentEventItem {
  id: string;
  event_type: DeploymentEventType;
  actor_type: DeploymentActorType;
  actor_name?: string;
  note?: string;
  occurred_at: string;
}

export interface DeploymentEventsResponse {
  items: DeploymentEventItem[];
}
```

- [ ] **Step 2: Extend LaunchpadResponse**

In `src/lib/ship/launchpad-types.ts`, add:

```ts
export interface LaunchpadRecentDeployment {
  id: string;
  store: "play_store" | "app_store" | "";
  status: string;
  status_kind: "success" | "info" | "warn" | "danger" | "neutral";
  submitted_at: string;
  method: "wemob" | "user" | string;
}
```

And add to `LaunchpadResponse`:

```ts
recent_deployment?: LaunchpadRecentDeployment;
```

- [ ] **Step 3: API method**

After `listShipItems` in `src/lib/api.ts`:

```ts
async listDeploymentEvents(
  deploymentId: string,
): Promise<import("./ship/deployment-event-types").DeploymentEventsResponse> {
  return this.request<import("./ship/deployment-event-types").DeploymentEventsResponse>(
    `/v1/deployments/${deploymentId}/events`,
  );
}
```

- [ ] **Step 4: Hook**

`src/hooks/use-deployment-events.ts`:

```ts
"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { apiClient } from "@/lib/api";
import type { DeploymentEventsResponse } from "@/lib/ship/deployment-event-types";

/**
 * useDeploymentEvents polls /v1/deployments/:id/events every 15s while
 * the most-recent event isn't terminal (live / rejected / failed /
 * canceled). Polling stops once a terminal event arrives.
 */
export function useDeploymentEvents(deploymentId: string | null | undefined, enabled: boolean) {
  const [data, setData] = useState<DeploymentEventsResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);
  const cancelledRef = useRef(false);

  const fetchOnce = useCallback(async () => {
    if (!deploymentId || !enabled) return;
    try {
      const res = await apiClient.listDeploymentEvents(deploymentId);
      if (cancelledRef.current) return;
      setData(res);
      setError(null);
    } catch (err) {
      if (cancelledRef.current) return;
      setError(err instanceof Error ? err : new Error(String(err)));
    } finally {
      if (!cancelledRef.current) setLoading(false);
    }
  }, [deploymentId, enabled]);

  useEffect(() => {
    cancelledRef.current = false;
    setLoading(true);
    fetchOnce();
    return () => {
      cancelledRef.current = true;
    };
  }, [fetchOnce]);

  useEffect(() => {
    if (!data || data.items.length === 0) return;
    const last = data.items[data.items.length - 1];
    const terminal = ["store_live", "store_rejected", "failed", "canceled"].includes(last.event_type);
    if (terminal) return;
    const handle = setInterval(() => fetchOnce(), 15_000);
    return () => clearInterval(handle);
  }, [data, fetchOnce]);

  return { data, loading, error, refetch: fetchOnce };
}
```

- [ ] **Step 5: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-app
npm run build
git add src/lib/ship/deployment-event-types.ts src/hooks/use-deployment-events.ts src/lib/api.ts src/lib/ship/launchpad-types.ts
git commit -m "feat(ship): add deployment-event types + hook + Launchpad recent_deployment type

Mirrors backend responses. Hook polls every 15s until a terminal
event (live / rejected / failed / canceled) arrives, then stops.
Same cancelledRef pattern as useLaunchpad / useShipItems."
```

---

## Task 6: DeploymentTimeline component

**Files:**
- Create: `src/components/ship/deployment-timeline.tsx`

- [ ] **Step 1: Write the component**

```tsx
"use client";

import { Check, Clock, AlertTriangle, X } from "lucide-react";
import type { DeploymentEventItem, DeploymentEventType } from "@/lib/ship/deployment-event-types";
import { useDeploymentEvents } from "@/hooks/use-deployment-events";
import { cn } from "@/lib/utils";

interface DeploymentTimelineProps {
  deploymentId: string;
  store: "play_store" | "app_store" | "";
  method: string; // "wemob" | "user"
  submittedAt: string;
}

export function DeploymentTimeline({ deploymentId, store, method, submittedAt }: DeploymentTimelineProps) {
  const { data, loading, error } = useDeploymentEvents(deploymentId, true);

  const storeLabel = store === "app_store" ? "App Store" : store === "play_store" ? "Play Store" : "Store";
  const events = data?.items ?? [];

  return (
    <section className="mt-6 border border-[var(--border)] rounded-xl bg-[var(--bg-surface)]">
      <header className="flex items-center gap-2.5 px-4 py-3 border-b border-[var(--border)]">
        <div className="text-[10px] uppercase tracking-[1px] text-[var(--ink-faint)]">
          {method === "wemob" ? "Managed submission" : "Submission"}
        </div>
        <div className="text-[12px] text-[var(--ink)]">
          {storeLabel} · submitted {formatRelative(submittedAt)}
        </div>
      </header>

      <div className="px-4 py-4">
        {loading && !data ? (
          <div className="text-[12px] text-[var(--ink-faint)]">Loading timeline…</div>
        ) : error ? (
          <div className="text-[12px] text-[var(--accent)]">Couldn&apos;t load timeline: {error.message}</div>
        ) : events.length === 0 ? (
          <div className="text-[12px] text-[var(--ink-faint)]">No events yet.</div>
        ) : (
          <ol className="flex flex-col">
            {events.map((e, idx) => (
              <TimelineRow
                key={e.id}
                event={e}
                isLast={idx === events.length - 1}
              />
            ))}
          </ol>
        )}
      </div>
    </section>
  );
}

function TimelineRow({ event, isLast }: { event: DeploymentEventItem; isLast: boolean }) {
  const meta = eventMeta(event.event_type);
  return (
    <li className="flex gap-3 pb-4 last:pb-0">
      <div className="flex flex-col items-center">
        <div className={cn("w-6 h-6 rounded-full flex items-center justify-center", meta.iconBg)}>
          {meta.icon}
        </div>
        {!isLast && <div className="flex-1 w-px bg-[var(--border)] min-h-[16px] mt-1" />}
      </div>
      <div className="flex-1 min-w-0 pb-1">
        <div className="text-[12px] font-medium text-[var(--ink)]">{meta.label}</div>
        <div className="text-[11px] text-[var(--ink-muted)] mt-0.5">
          {actorLine(event)} · {formatRelative(event.occurred_at)}
        </div>
        {event.note && (
          <div className="mt-1 text-[11px] text-[var(--ink-muted)] bg-[var(--bg-raised)] rounded px-2 py-1 leading-[1.55]">
            {event.note}
          </div>
        )}
      </div>
    </li>
  );
}

function actorLine(event: DeploymentEventItem): string {
  switch (event.actor_type) {
    case "wemob":
      return event.actor_name ? `${event.actor_name} at Wemob` : "Wemob team";
    case "user":
      return "You";
    case "system":
      return "Wemob system";
    case "store_api":
      return "Store API";
    default:
      return event.actor_type;
  }
}

function eventMeta(type: DeploymentEventType): {
  label: string;
  icon: React.ReactNode;
  iconBg: string;
} {
  switch (type) {
    case "submitted":
      return {
        label: "Submitted",
        icon: <Check size={12} className="text-white" />,
        iconBg: "bg-[var(--ink)]",
      };
    case "triaged":
      return {
        label: "Picked up by Wemob reviewer",
        icon: <Check size={12} className="text-white" />,
        iconBg: "bg-[#3d6890]",
      };
    case "uploaded_to_store":
      return {
        label: "Uploaded to store",
        icon: <Check size={12} className="text-white" />,
        iconBg: "bg-[#3d6890]",
      };
    case "store_review_started":
      return {
        label: "Store review started",
        icon: <Clock size={12} className="text-white" />,
        iconBg: "bg-[#3d6890]",
      };
    case "store_live":
      return {
        label: "Live on store",
        icon: <Check size={12} className="text-white" />,
        iconBg: "bg-[var(--success)]",
      };
    case "store_rejected":
      return {
        label: "Rejected by store",
        icon: <AlertTriangle size={12} className="text-white" />,
        iconBg: "bg-[var(--accent)]",
      };
    case "canceled":
      return {
        label: "Canceled",
        icon: <X size={12} className="text-white" />,
        iconBg: "bg-[var(--ink-muted)]",
      };
    case "failed":
      return {
        label: "Failed",
        icon: <AlertTriangle size={12} className="text-white" />,
        iconBg: "bg-[var(--accent)]",
      };
    default:
      return {
        label: type,
        icon: <Clock size={12} className="text-white" />,
        iconBg: "bg-[var(--ink-muted)]",
      };
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
  if (days < 7) return `${days}d ago`;
  return new Date(iso).toLocaleDateString();
}
```

- [ ] **Step 2: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-app
npm run lint
npm run build
git add src/components/ship/deployment-timeline.tsx
git commit -m "feat(ship): add DeploymentTimeline component

Vertical list of DeploymentEventItems with per-event icon + label +
actor + relative timestamp + optional note. Uses useDeploymentEvents
for fetch + 15s poll. Managed-track events show the Wemob reviewer
name when present ('Sam K. at Wemob')."
```

---

## Task 7: Host timeline on Launchpad

**Files:**
- Modify: `src/components/ship/launchpad.tsx`

- [ ] **Step 1: Render the timeline when data.recent_deployment is set**

Open `/Users/knid/Projects/wemob/wemob-app/src/components/ship/launchpad.tsx`. Add an import:

```tsx
import { DeploymentTimeline } from "./deployment-timeline";
```

Inside the main Launchpad layout (inside the `max-w-[880px]` wrapper), insert the timeline between the Listing grid and the Builds section:

```tsx
{data.recent_deployment && (
  <DeploymentTimeline
    deploymentId={data.recent_deployment.id}
    store={data.recent_deployment.store}
    method={data.recent_deployment.method}
    submittedAt={data.recent_deployment.submitted_at}
  />
)}
```

- [ ] **Step 2: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-app
npm run lint
npm run build
git add src/components/ship/launchpad.tsx
git commit -m "feat(ship): host DeploymentTimeline on Launchpad

Renders above the Builds feed when the aggregate endpoint signals a
recent deployment (7-day window). Empty state when no recent submission."
```

---

## Task 8: End-to-end smoke

```
# Backend
cd /Users/knid/Projects/wemob/wemob-backend
docker-compose -f docker-compose-dev.yaml up -d
go run cmd/main.go &

# Frontend
cd /Users/knid/Projects/wemob/wemob-app && npm run dev
```

Test flow:
1. Open /apps/<appId>/ship → QuickShip → submit a deployment.
2. Back on the Launchpad, the recent-deployment timeline appears above the Builds feed with a single "Submitted" event attributed to "You".
3. Trigger a `/v1/reports/deploy` POST with status=`Deploying` — a second "Uploaded to store" event appears after refresh / auto-poll.
4. Status=`Deployed` → "Live on store" in green. Polling stops (terminal state).

Postgres MCP check:
```sql
SELECT event_type, actor_type, occurred_at
FROM deployment_events
WHERE deployment_id = '<uuid>'
ORDER BY occurred_at;
```

Should show the sequence of events.

- [ ] **Step 1: No commit — user-run smoke.**

---

## Phase 5a completion criteria

- Backend + frontend build/vet/lint clean.
- `deployment_events` rows written at create + status-transition hot paths.
- `GET /v1/deployments/:id/events` returns ordered events with actor/note flattened.
- LaunchpadResponse includes `recent_deployment` when one exists in the last 7 days.
- DeploymentTimeline renders on the Launchpad and polls every 15s until terminal.
- No regressions in /ship, /builds, Pilot, QuickShip, or any prior phase.

Phase 5b (real ETAs + store-API polling worker + email nudges) unblocked.

## Out of scope (5b / 5c)

- Real ETA computation from rolling medians.
- Store-Connect / Play Publishing poll worker that writes `store_api` events.
- Email nudges for `pending_connection` draft submissions.
- Human-attributed `triaged` events from a Wemob admin action (manual attribution; the event type + actor_type are wired, the admin surface that writes one is Phase 5c or later).
- `pending_connection` draft status + first-class persistence (Phase 5c).
- Flag ramp to 100% (Phase 5c).
