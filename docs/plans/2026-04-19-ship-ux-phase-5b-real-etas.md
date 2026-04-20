# Ship UX — Phase 5b · Real ETAs from Rolling Medians Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the hardcoded "typically 24 hours" copy with an ETA computed from the user's own data. Take the median elapsed time from `submitted → store_live` across the last N deployments, surface it as `eta_seconds` on the Launchpad's `recent_deployment` and on `/ship` table rows, and render it inline ("Typically ~18h" under the active timeline). Store-API polling worker + `pending_connection` + email nudges defer to Phase 5c.

**Architecture:** A new `internal/database/deployment_eta.go` helper computes medians by joining `deployment_events` pairs. Both the Launchpad aggregate and `/v1/ship` call it when populating their responses. Frontend renders the ETA as a human string (`~18h`, `~2d`, `<1h`) when present. Nothing async — ETAs are computed on read.

**Tech stack:** Go 1.22+, Bun. Next.js 16, React 19, Tailwind 4.

**Design spec:** `docs/design/2026-04-18-wemob-publish-ux-redesign-design.md` ("Real ETAs").

---

## Test strategy

Same discipline: build/vet/lint clean, runtime smoke documented for user.

## File structure overview

### Backend — new file

```
internal/database/deployment_eta.go  — MedianElapsedSeconds(from, to, lookback)
```

### Backend — modified

```
internal/responses/launchpad.go       — LaunchpadRecentDeployment.eta_seconds
internal/responses/ship.go            — ShipItem.eta_seconds already present, just populate it
internal/controllers/launchpad_controller.go  — populate eta_seconds
internal/controllers/ship_controller.go       — populate eta_seconds for deployment items
```

### Frontend — modified

```
src/lib/ship/launchpad-types.ts       — LaunchpadRecentDeployment.eta_seconds
src/components/ship/deployment-timeline.tsx  — render "Typically ~18h" in header
src/components/ship/ship-table.tsx    — show ETA in the Updated column when in-flight
```

## Why these boundaries

- **ETAs computed on read, not precomputed.** The lookback is 30 deployments per direction; the query is cheap (single aggregate on a small event table) and it stays honest as reality changes. A cache layer is overkill for Phase 5b.
- **No new response fields on the backend.** `ShipItem.eta_seconds` was declared in Phase 4c but never populated. Phase 5b just lights it up.
- **No async writes.** Phase 5c's poller is what introduces async work; ETAs in 5b are purely synchronous and pulled from existing event data.

---

## Task 1: ETA helper

**Files:**
- Create: `internal/database/deployment_eta.go`

- [ ] **Step 1: Write the helper**

```go
package database

import (
	"context"
	"sort"
	"time"

	"github.com/knid/wemob/internal/models"
	"github.com/uptrace/bun"
)

// MedianElapsedSeconds computes the median number of seconds between
// a "from" event and a "to" event across recent deployments. Only
// deployments where both events exist contribute. lookbackDays bounds
// the window so old anomalies don't skew the median as the system
// matures. Returns 0 when there aren't enough data points (< 3) —
// callers treat 0 as "no ETA yet, show a static fallback."
func MedianElapsedSeconds(
	ctx context.Context,
	db *bun.DB,
	fromType, toType string,
	lookbackDays int,
) (int, error) {
	since := time.Now().Add(-time.Duration(lookbackDays) * 24 * time.Hour)

	// Pull all relevant events in one query. Filtering in Go rather
	// than SQL keeps the query simple and lets us pair events by
	// deployment_id without a window function.
	var events []models.DeploymentEvent
	if err := db.NewSelect().
		Model(&events).
		Where("event_type IN (?, ?)", fromType, toType).
		Where("occurred_at > ?", since).
		Order("occurred_at ASC").
		Scan(ctx); err != nil {
		return 0, err
	}

	// Group by deployment_id, capturing the earliest fromType + the
	// earliest subsequent toType. If either is missing, skip.
	type pair struct {
		from *time.Time
		to   *time.Time
	}
	byDep := map[string]*pair{}
	for _, ev := range events {
		key := ev.DeploymentID.String()
		p, ok := byDep[key]
		if !ok {
			p = &pair{}
			byDep[key] = p
		}
		if ev.EventType == fromType && p.from == nil {
			t := ev.OccurredAt
			p.from = &t
		}
		if ev.EventType == toType && p.from != nil && p.to == nil {
			t := ev.OccurredAt
			p.to = &t
		}
	}

	elapsed := make([]int, 0, len(byDep))
	for _, p := range byDep {
		if p.from == nil || p.to == nil {
			continue
		}
		d := int(p.to.Sub(*p.from).Seconds())
		if d > 0 {
			elapsed = append(elapsed, d)
		}
	}
	if len(elapsed) < 3 {
		return 0, nil
	}

	sort.Ints(elapsed)
	mid := len(elapsed) / 2
	if len(elapsed)%2 == 1 {
		return elapsed[mid], nil
	}
	return (elapsed[mid-1] + elapsed[mid]) / 2, nil
}
```

- [ ] **Step 2: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
go build ./...
go vet ./...
git add internal/database/deployment_eta.go
git commit -m "feat(ship): add MedianElapsedSeconds helper for real ETAs

Takes {fromType, toType, lookbackDays}. Pairs events by deployment_id,
keeps deployments where both event types exist, computes the median
elapsed seconds. Returns 0 (sentinel 'not enough data yet') when fewer
than 3 deployments qualify."
```

---

## Task 2: Wire ETA into Launchpad + /ship responses

**Files:**
- Modify: `internal/responses/launchpad.go` (add `eta_seconds`)
- Modify: `internal/controllers/launchpad_controller.go`
- Modify: `internal/controllers/ship_controller.go`

- [ ] **Step 1: Extend `LaunchpadRecentDeployment`**

Add a field:

```go
// ETASeconds is the median elapsed time from submitted → store_live
// across the user's last 30 deployments (same store). Zero means
// we don't have enough data yet; frontend falls back to "a few days"
// for managed submissions.
ETASeconds int `json:"eta_seconds,omitempty"`
```

- [ ] **Step 2: Populate in `buildRecentDeployment`**

In `internal/controllers/launchpad_controller.go` `buildRecentDeployment`, right before the `return` that constructs the response:

```go
// Compute ETA from submitted → store_live, 30-day lookback.
etaSec, _ := database.MedianElapsedSeconds(ctx, c.DB.DB,
    models.DeploymentEventSubmitted,
    models.DeploymentEventStoreLive,
    30)

resp := &responses.LaunchpadRecentDeployment{
    ID:          d.ID.String(),
    Store:       store,
    Status:      status,
    StatusKind:  kind,
    SubmittedAt: d.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
    Method:      d.Method,
    ETASeconds:  etaSec,
}
return resp
```

Swap the existing `return` with this form. Only emit a non-zero ETA for submissions that are actually in flight (not `store_live`, not `store_rejected`, not `failed`) — otherwise the ETA is irrelevant:

```go
if kind == "success" || kind == "danger" {
    resp.ETASeconds = 0
}
```

- [ ] **Step 3: Populate in `ship_controller.go`**

For each ShipItem of `kind == "deployment"` where `status_kind == "info"` (in flight), compute ETA. Inside the deployments loop in `GetShipItems`:

```go
var etaForDeployments int
if len(deployments) > 0 {
    etaForDeployments, _ = database.MedianElapsedSeconds(ctx, c.DB.DB,
        models.DeploymentEventSubmitted,
        models.DeploymentEventStoreLive,
        30)
}
```

Then when building the deployment ShipItem:

```go
item := responses.ShipItem{
    // ... existing fields
}
if kind == "info" && etaForDeployments > 0 {
    item.ETASeconds = etaForDeployments
}
```

- [ ] **Step 4: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
go build ./...
go vet ./...
git add internal/responses/launchpad.go internal/controllers/launchpad_controller.go internal/controllers/ship_controller.go
git commit -m "feat(ship): populate eta_seconds on Launchpad + /ship deployment rows

Calls MedianElapsedSeconds across the user's last 30 days of
deployments. Suppresses ETA on already-terminal states (live /
rejected / failed) since the number is meaningless there.

Frontend renders as 'Typically ~18h' when >0, falls back to a static
'a few days' for managed submissions when the median can't be
computed yet (fewer than 3 historical data points)."
```

---

## Task 3: Frontend — extend types + render ETA on timeline

**Files:**
- Modify: `src/lib/ship/launchpad-types.ts`
- Modify: `src/components/ship/deployment-timeline.tsx`

- [ ] **Step 1: TS type**

In `src/lib/ship/launchpad-types.ts`, update `LaunchpadRecentDeployment`:

```ts
export interface LaunchpadRecentDeployment {
  id: string;
  store: "play_store" | "app_store" | "";
  status: string;
  status_kind: "success" | "info" | "warn" | "danger" | "neutral";
  submitted_at: string;
  method: "wemob" | "user" | string;
  eta_seconds?: number;
}
```

- [ ] **Step 2: Update `DeploymentTimeline` to accept + render ETA**

In `src/components/ship/deployment-timeline.tsx`, extend props:

```tsx
interface DeploymentTimelineProps {
  deploymentId: string;
  store: "play_store" | "app_store" | "";
  method: string;
  submittedAt: string;
  etaSeconds?: number;
}
```

Modify the destructure in the component signature to accept `etaSeconds`.

In the header, add the ETA as a second line when present:

```tsx
<header className="flex items-start gap-2.5 px-4 py-3 border-b border-[var(--border)]">
  <div>
    <div className="text-[10px] uppercase tracking-[1px] text-[var(--ink-faint)]">
      {method === "wemob" ? "Managed submission" : "Submission"}
    </div>
    <div className="text-[12px] text-[var(--ink)] mt-0.5">
      {storeLabel} · submitted {formatRelative(submittedAt)}
    </div>
  </div>
  {etaSeconds !== undefined && etaSeconds > 0 && (
    <div className="ml-auto text-right">
      <div className="text-[10px] uppercase tracking-[0.5px] text-[var(--ink-faint)]">
        Typically
      </div>
      <div className="text-[12px] font-mono text-[var(--ink)] mt-0.5">
        ~{formatETA(etaSeconds)}
      </div>
    </div>
  )}
</header>
```

Add a `formatETA` helper at the bottom of the file alongside `formatRelative`:

```tsx
function formatETA(seconds: number): string {
  if (seconds < 3600) return `${Math.max(1, Math.round(seconds / 60))}m`;
  if (seconds < 86400) return `${Math.round(seconds / 3600)}h`;
  const days = Math.round(seconds / 86400);
  return `${days}d`;
}
```

- [ ] **Step 3: Pass `etaSeconds` from `launchpad.tsx`**

In `src/components/ship/launchpad.tsx`, update the `<DeploymentTimeline />` render:

```tsx
<DeploymentTimeline
  deploymentId={data.recent_deployment.id}
  store={data.recent_deployment.store}
  method={data.recent_deployment.method}
  submittedAt={data.recent_deployment.submitted_at}
  etaSeconds={data.recent_deployment.eta_seconds}
/>
```

- [ ] **Step 4: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-app
npm run lint
npm run build
git add src/lib/ship/launchpad-types.ts src/components/ship/deployment-timeline.tsx src/components/ship/launchpad.tsx
git commit -m "feat(ship): render 'Typically ~18h' ETA on DeploymentTimeline header

Shows a right-aligned 'Typically ~Nh' column when eta_seconds is
present and >0. Formats as minutes under an hour, hours under a day,
days otherwise. Omitted for terminal submissions (backend sends 0 in
those cases)."
```

---

## Task 4: Frontend — ETA in /ship table

**Files:**
- Modify: `src/components/ship/ship-table.tsx`

- [ ] **Step 1: Show ETA inline on in-progress deployment rows**

In `ShipTable`, update the `Updated` column cell so in-progress rows with an ETA show "~Nh left" below the relative timestamp:

```tsx
<td className="px-4 py-2.5 text-[var(--ink-faint)]">
  <div>{formatRelative(i.updated_at ?? i.created_at)}</div>
  {i.status_kind === "info" && i.eta_seconds !== undefined && i.eta_seconds > 0 && (
    <div className="text-[10px] text-[var(--ink-muted)] mt-0.5 font-mono">
      ~{formatETA(i.eta_seconds)} left
    </div>
  )}
</td>
```

Add the same `formatETA` helper at the bottom of the file (same body as in deployment-timeline.tsx):

```tsx
function formatETA(seconds: number): string {
  if (seconds < 3600) return `${Math.max(1, Math.round(seconds / 60))}m`;
  if (seconds < 86400) return `${Math.round(seconds / 3600)}h`;
  const days = Math.round(seconds / 86400);
  return `${days}d`;
}
```

- [ ] **Step 2: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-app
npm run lint
npm run build
git add src/components/ship/ship-table.tsx
git commit -m "feat(ship): show ETA beneath Updated column on in-progress /ship rows

Only renders for info-kind rows (in-flight deployments) when eta_seconds
is present and >0. Matches the format used by DeploymentTimeline so
users see the same numbers on both surfaces."
```

---

## Task 5: End-to-end smoke

Live-only — documented for user:

```
# Backend
cd /Users/knid/Projects/wemob/wemob-backend
docker-compose -f docker-compose-dev.yaml up -d
go run cmd/main.go &

# Frontend
cd /Users/knid/Projects/wemob/wemob-app && npm run dev
```

Test flow:
1. With fewer than 3 historical deployments, open /ship/:appId — the timeline header does NOT show "Typically ~Nh" (eta_seconds is 0).
2. Seed enough historical deployment_events via Postgres MCP or the app to have ≥3 submitted→store_live pairs. Refresh; the header shows "Typically ~{median}h".
3. Visit /ship — in-progress deployment rows show "~{median}h left" beneath "Updated".
4. A terminal (live) deployment on /ship: no ETA subtext (because status_kind is "success", not "info").

```sql
-- Seed helper (for testing):
INSERT INTO deployment_events (deployment_id, event_type, actor_type, occurred_at)
VALUES
  ('<dep-id-1>', 'submitted',  'user',   '2026-04-10 10:00:00+00'),
  ('<dep-id-1>', 'store_live', 'system', '2026-04-10 16:00:00+00'),
  ('<dep-id-2>', 'submitted',  'user',   '2026-04-11 09:00:00+00'),
  ('<dep-id-2>', 'store_live', 'system', '2026-04-11 15:30:00+00'),
  ('<dep-id-3>', 'submitted',  'user',   '2026-04-12 11:00:00+00'),
  ('<dep-id-3>', 'store_live', 'system', '2026-04-12 17:45:00+00');
-- Median of 21600, 23400, 24300 seconds = 23400s = ~6.5h → renders as "~7h"
```

- [ ] **Step 1: No commit — user-run smoke.**

---

## Phase 5b completion criteria

- Backend + frontend build/vet/lint clean.
- `MedianElapsedSeconds(ctx, db, fromType, toType, lookbackDays)` exists and returns 0 for <3 data points.
- Launchpad `recent_deployment` carries `eta_seconds` when the deployment is in-flight.
- `/v1/ship` items carry `eta_seconds` for in-flight deployments.
- DeploymentTimeline header shows "Typically ~Nh" when non-zero.
- ShipTable Updated column shows "~Nh left" for in-progress rows.
- Zero regressions in Launchpad / Pilot / QuickShip / /ship / Builds.

Phase 5c (store-API polling worker + `pending_connection` flow + email nudges + flag ramp to 100%) unblocked.

## Out of scope

- Store-API polling worker that writes `store_api`-actored events (Phase 5c).
- `pending_connection` draft flow for submissions where a connection isn't ready yet (Phase 5c).
- Email nudges for pending connections (Phase 5c).
- Admin triage action that writes `triaged` events with reviewer attribution (Phase 5c or later).
- Flag ramp from current rollout to 100% (Phase 5c).
- Caching the ETA median (premature — measure first).
