# Ship UX — Phase 5c-iii · Email Nudges + Switch-to-Managed + Flag Ramp Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the loop on `pending_connection` deployments. If a user parks a submission and then goes dark, send up to three email nudges over a week (every 2 days) with a one-click escape to the Wemob-managed track. When all three nudges are sent and the user still hasn't acted, the deployment stays pending (they'll see it on the Launchpad) but we stop emailing. Also ramps the `new_ship_ux` PostHog flag to 100%.

**Architecture:** Add a `DeploymentEventNudgeSent` event type. A new periodic goroutine (`internal/services/pendingnudge/`) scans `pending_connection` deployments every 6 hours; for each, it counts prior `nudge_sent` events, and if the Nth nudge is due (N*2 days since creation, max 3), fires a Brevo event `pending_connection_nudge` and appends a `nudge_sent` event. A new backend endpoint `POST /v1/deployments/:id/switch-to-managed` flips a pending deployment to Wemob-managed (clears connections, sets Status=`Queued`, notifies Slack, emits `submitted` with `metadata.switched_to_managed=true`). Frontend surfaces that action as a secondary button on the `needs_connection` store tile. Finally: PostHog flag ramp to 100%.

**Tech stack:** Go 1.22+, Bun, Brevo. Next.js 16, React 19, Tailwind 4. PostHog MCP for flag update.

**Design spec:** `docs/design/2026-04-18-wemob-publish-ux-redesign-design.md` ("Draft submissions are first-class" → nudge + switch-to-managed escape).

---

## Test strategy

Build + vet clean on backend; lint + build clean on frontend. Smoke documented for user.

## File structure overview

### Backend — new

```
internal/services/pendingnudge/nudge.go    — NudgeRunner periodic scan + send
```

### Backend — modified

```
internal/models/deployment_event.go                — add DeploymentEventNudgeSent
internal/constants/brevo.go                         — add BREVO_EVENT_PENDING_CONNECTION_NUDGE
internal/controllers/pending_connection.go         — SwitchToManaged helper + private send logic
internal/views/deployments.go                      — SwitchToManagedToManaged handler
internal/responses/deployment.go                    — no changes expected; confirm DeploymentResponse covers needs
cmd/main.go                                         — register route + start NudgeRunner goroutine
```

### Frontend — modified

```
src/lib/api.ts                                     — switchDeploymentToManaged(deploymentId) method
src/components/ship/store-tile.tsx                 — "Switch to managed" secondary button on needs_connection
src/hooks/use-launchpad.ts                         — expose a refetch after switch
```

### Operational — PostHog flag

Ramp `new_ship_ux` (flag ID 172590) to `rollout_percentage: 100`.

## Why these boundaries

- **Nudge state lives in `deployment_events`, not on `Deployment`.** Counting events is cheap; a new column is a migration and adds a stateful field to a model that should remain lean. Also: the event gives us free observability ("how often do users ignore nudge 2?").
- **6-hour tick cadence.** The design says "every 2 days"; polling every 6 hours means a nudge fires within 6h of its due time without meaningfully raising load. Daily ticks would be fine too but leave long gaps.
- **"Switch to managed" is a distinct endpoint.** Reusing `UpdateDeploymentTask` would require the frontend to reconstruct the full payload for no gain; a tiny dedicated handler is clearer.
- **Flag ramp is orchestration, not code.** Listed as a plan step but executed via PostHog MCP / dashboard. No change to app logic.

---

## Task 1: Event type + Brevo event constant

**Files:**
- Modify: `internal/models/deployment_event.go`
- Modify: `internal/constants/brevo.go`

- [ ] **Step 1: Add `DeploymentEventNudgeSent`**

In `internal/models/deployment_event.go`, append to the event-type const block:

```go
const (
    // … existing
    DeploymentEventFailed             = "failed"
    DeploymentEventNudgeSent          = "nudge_sent" // pending_connection reminder email dispatched
)
```

- [ ] **Step 2: Add Brevo event**

In `internal/constants/brevo.go`, append next to the other deployment events:

```go
BREVO_EVENT_PENDING_CONNECTION_NUDGE = "pending_connection_nudge"
```

(Brevo template for `pending_connection_nudge` is a manual ops step — document in the completion section.)

- [ ] **Step 3: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
go build ./...
go vet ./...
git add internal/models/deployment_event.go internal/constants/brevo.go
git commit -m "feat(ship): add nudge_sent event + pending_connection_nudge Brevo event

nudge_sent records a single pending_connection reminder email
dispatch. Up to three per deployment, spaced 2 days apart. Counting
existing events determines the next attempt number. Brevo event
string matches the CRM template slug."
```

---

## Task 2: Backend — `SwitchToManaged` helper + HTTP endpoint

**Files:**
- Modify: `internal/controllers/pending_connection.go`
- Modify: `internal/views/deployments.go`
- Modify: `cmd/main.go` (route registration)

- [ ] **Step 1: `SwitchToManaged` on Controller**

Append to `internal/controllers/pending_connection.go`:

```go
// SwitchToManaged flips a pending_connection deployment onto the
// Wemob-managed track. Used by the "Set up later?" nudge email's
// escape link + the Launchpad store tile's secondary CTA. Clears the
// user connection IDs (since the deployment is now managed by us),
// sets Status = "Queued", posts a Slack notification (mirrors the
// managed-deploy codepath in CreateDeploymentTask), and emits
// submitted with metadata.switched_to_managed = true for timeline
// attribution.
//
// Idempotent: calling on a non-pending deployment is a no-op.
func (c *Controller) SwitchToManaged(ctx context.Context, user *models.User, deploymentID string) (*models.Deployment, error) {
	deployment, err := c.DB.GetDeploymentByID(deploymentID)
	if err != nil {
		return nil, err
	}
	if deployment.UserID.String() != user.ID.String() {
		return nil, errors.New("you do not have access to this resource")
	}
	if deployment.Status != "pending_connection" {
		return deployment, nil // nothing to do
	}

	deployment.Method = "wemob"
	deployment.AppleConnectionID = uuid.Nil
	deployment.GooglePlayConnectionID = uuid.Nil
	deployment.Status = "Queued"

	updated, err := c.DB.UpdateDeployment(deployment)
	if err != nil {
		return nil, fmt.Errorf("update deployment: %w", err)
	}

	// Emit submitted with attribution metadata.
	actorID := user.ID
	_ = database.AppendDeploymentEvent(ctx, c.DB.DB, updated.ID,
		models.DeploymentEventSubmitted, models.DeploymentEventActorUser, &actorID,
		map[string]interface{}{"switched_to_managed": true})

	// Mirror the Slack notification from CreateDeploymentTask's managed branch.
	go c.SlackService.Client.PostMessage(
		constants.SLACK_CHANNEL_ID_NEW_ANDROID_DEPLOY_REQUEST,
		slack.MsgOptionText(
			"Switched to managed: ("+deployment.ProjectID.String()+") "+
				updated.ID.String()+" platforms: "+fmt.Sprintf("%v", updated.Platforms),
			false,
		),
	)
	return updated, nil
}
```

Required imports to add to `pending_connection.go`:

```go
"errors"
"github.com/slack-go/slack"
"github.com/knid/wemob/internal/constants"
```

(`database`, `models`, `fmt`, `context` likely already imported.)

- [ ] **Step 2: HTTP handler**

In `internal/views/deployments.go`, add:

```go
// SwitchDeploymentToManaged handles POST /v1/deployments/:id/switch-to-managed.
// Flips a pending_connection deployment onto the Wemob-managed track.
func (v *HttpViews) SwitchDeploymentToManaged(w http.ResponseWriter, r *http.Request) {
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
	deploymentID := chi.URLParam(r, "deploymentId")
	if deploymentID == "" {
		utils.JSONErrorResponse(w, "deployment id required", http.StatusBadRequest)
		return
	}
	updated, err := v.Controller.SwitchToManaged(r.Context(), user, deploymentID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	utils.JSONResponse(w, map[string]interface{}{
		"id":     updated.ID.String(),
		"status": updated.Status,
		"method": updated.Method,
	}, http.StatusOK)
}
```

- [ ] **Step 3: Route**

In `cmd/main.go`, inside the `r.Route("/deployments", …)` block at the existing `/v1` level (near line 1333):

```go
r.Post("/{deploymentId}/switch-to-managed", httpViews.SwitchDeploymentToManaged)
```

- [ ] **Step 4: Verify + commit**

```bash
go build ./...
go vet ./...
git add internal/controllers/pending_connection.go internal/views/deployments.go cmd/main.go
git commit -m "feat(ship): switch-to-managed endpoint for pending_connection deployments

POST /v1/deployments/:id/switch-to-managed. Flips method to 'wemob',
clears the user's connection IDs, sets Status='Queued', posts the
same Slack notification as the managed-submit codepath, and emits a
submitted event with metadata.switched_to_managed = true.

Used by the Launchpad store tile secondary CTA on needs_connection
tiles + the pending-connection nudge email escape link."
```

---

## Task 3: Backend — NudgeRunner periodic goroutine

**Files:**
- Create: `internal/services/pendingnudge/nudge.go`
- Modify: `cmd/main.go`

- [ ] **Step 1: Write the runner**

Create `internal/services/pendingnudge/nudge.go`:

```go
package pendingnudge

import (
	"context"
	"log/slog"
	"time"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	crmServices "github.com/knid/wemob/internal/services/crm"
)

// Runner periodically scans pending_connection deployments and fires
// email nudges via the CRM. Up to 3 nudges per deployment, spaced 2
// days apart.
type Runner struct {
	DB      *database.Database
	CRM     crmServices.CRMService
	Every   time.Duration // Default 6h when zero.
	// SpacingDays sets the gap between consecutive nudges. Default 2.
	SpacingDays int
	// MaxNudges caps the total per deployment. Default 3.
	MaxNudges int
}

// Run blocks until ctx is cancelled.
func (r *Runner) Run(ctx context.Context) {
	if r.Every <= 0 {
		r.Every = 6 * time.Hour
	}
	if r.SpacingDays <= 0 {
		r.SpacingDays = 2
	}
	if r.MaxNudges <= 0 {
		r.MaxNudges = 3
	}
	ticker := time.NewTicker(r.Every)
	defer ticker.Stop()

	r.tick(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.tick(ctx)
		}
	}
}

func (r *Runner) tick(ctx context.Context) {
	deployments, err := r.DB.ListPendingConnectionDeployments(ctx)
	if err != nil {
		slog.Warn("pendingnudge: list deployments failed", "err", err)
		return
	}
	if len(deployments) == 0 {
		return
	}
	slog.Debug("pendingnudge: ticking", "deployments", len(deployments))

	now := time.Now()
	for i := range deployments {
		d := deployments[i]
		if d.User == nil {
			slog.Warn("pendingnudge: deployment missing user relation", "deployment_id", d.ID)
			continue
		}

		nudgeCount, err := database.CountEventsOfType(ctx, r.DB.DB, d.ID, models.DeploymentEventNudgeSent)
		if err != nil {
			slog.Warn("pendingnudge: count events failed", "deployment_id", d.ID, "err", err)
			continue
		}
		if nudgeCount >= r.MaxNudges {
			continue
		}

		// Nudge N is due once (N * SpacingDays) have passed since the deployment
		// was created. This spaces nudges by SpacingDays regardless of when the
		// prior tick observed them.
		dueAt := d.CreatedAt.Add(time.Duration(nudgeCount+1) * time.Duration(r.SpacingDays) * 24 * time.Hour)
		if now.Before(dueAt) {
			continue
		}

		platform := "ios"
		for _, p := range d.Platforms {
			if p == "android" {
				platform = "android"
				break
			}
		}
		attrs := map[string]any{
			"deployment_id":  d.ID.String(),
			"project_id":     d.ProjectID.String(),
			"platform":       platform,
			"attempt":        nudgeCount + 1,
			"max_attempts":   r.MaxNudges,
			"submitted_at":   d.CreatedAt.UTC().Format(time.RFC3339),
			"switch_url":     "/ship/" + d.ProjectID.String() + "#app-store",
		}
		if err := r.CRM.SendEvent(d.User, constants.BREVO_EVENT_PENDING_CONNECTION_NUDGE, attrs); err != nil {
			slog.Warn("pendingnudge: brevo send failed", "deployment_id", d.ID, "err", err)
			continue
		}

		if err := database.AppendDeploymentEvent(ctx, r.DB.DB, d.ID,
			models.DeploymentEventNudgeSent,
			models.DeploymentEventActorSystem, nil,
			map[string]interface{}{"attempt": nudgeCount + 1, "platform": platform}); err != nil {
			slog.Warn("pendingnudge: append event failed", "deployment_id", d.ID, "err", err)
			continue
		}
		slog.Info("pendingnudge: sent",
			"deployment_id", d.ID,
			"attempt", nudgeCount+1,
			"platform", platform,
			"user_id", d.UserID)
	}
}
```

- [ ] **Step 2: Add `Database.ListPendingConnectionDeployments`**

In `internal/database/deployment.go`, next to `ListInFlightDeployments`:

```go
// ListPendingConnectionDeployments returns all deployments awaiting a
// user-side connection. Includes User relation so the nudge runner
// can address a Brevo email to them.
func (d *Database) ListPendingConnectionDeployments(ctx context.Context) ([]models.Deployment, error) {
	var deployments []models.Deployment
	if err := d.DB.NewSelect().
		Model(&deployments).
		Relation("User").
		Where("deployment.status = ?", "pending_connection").
		Scan(ctx); err != nil {
		return nil, err
	}
	return deployments, nil
}
```

- [ ] **Step 3: Add `database.CountEventsOfType`**

In `internal/database/deployment_events.go`, below `HasEvent`:

```go
// CountEventsOfType returns how many events of the given type exist
// for a deployment. Used by the nudge runner to pick the next attempt
// number.
func CountEventsOfType(ctx context.Context, db *bun.DB, deploymentID uuid.UUID, eventType string) (int, error) {
	return db.NewSelect().
		Model((*models.DeploymentEvent)(nil)).
		Where("deployment_id = ?", deploymentID).
		Where("event_type = ?", eventType).
		Count(ctx)
}
```

- [ ] **Step 4: Wire into `cmd/main.go`**

Next to the storepoll goroutine startup (which already uses `context.WithCancel`), add:

```go
// Phase 5c-iii: periodically email users with pending_connection
// deployments that they should wire up a connection (or switch to
// managed). Max 3 nudges, 2 days apart.
nudgeCtx, cancelNudge := context.WithCancel(context.Background())
nudgeRunner := &pendingnudge.Runner{
    DB:    controller.DB,
    CRM:   controller.CRMService,
    Every: 6 * time.Hour,
}
go nudgeRunner.Run(nudgeCtx)
```

Add the import:

```go
"github.com/knid/wemob/internal/services/pendingnudge"
```

And call `cancelNudge()` in the existing shutdown goroutine alongside `cancelPoller()`:

```go
cancelPoller()
cancelNudge()
```

- [ ] **Step 5: Verify + commit**

```bash
go build ./...
go vet ./...
git add internal/services/pendingnudge/ internal/database/deployment.go internal/database/deployment_events.go cmd/main.go
git commit -m "feat(ship): pendingnudge runner — email reminders for pending_connection

Periodic goroutine (6h cadence) that counts prior nudge_sent events
per deployment, fires the pending_connection_nudge Brevo event when
the next attempt is due (attempt N at N*2 days after creation), caps
at 3 attempts, and appends nudge_sent back into deployment_events for
attribution.

Cancelled on SIGINT/SIGTERM next to the store poller."
```

---

## Task 4: Frontend — "Switch to managed" secondary CTA + API method

**Files:**
- Modify: `src/lib/api.ts`
- Modify: `src/components/ship/store-tile.tsx`

- [ ] **Step 1: Add API method**

In `src/lib/api.ts`, add near `createDeployment`:

```ts
async switchDeploymentToManaged(deploymentId: string): Promise<{ id: string; status: string; method: string }> {
  const response = await this.request<{ id: string; status: string; method: string }>(
    `/v1/deployments/${deploymentId}/switch-to-managed`,
    { method: "POST" }
  );
  return response;
}
```

- [ ] **Step 2: Add the secondary CTA on needs_connection tiles**

In `src/components/ship/store-tile.tsx`, below the primary action button, conditional render:

```tsx
{tile.status === "needs_connection" && tile.latest_deployment_id && onSwitchToManaged && (
  <button
    type="button"
    onClick={() => onSwitchToManaged(tile.latest_deployment_id!)}
    className="mt-2 text-[11px] text-[var(--ink-muted)] hover:text-[var(--ink)] underline underline-offset-2"
  >
    Or let Wemob ship it instead
  </button>
)}
```

Add `onSwitchToManaged?: (deploymentId: string) => void` to the component props.

- [ ] **Step 3: Surface `latest_deployment_id` in the response type**

Check `src/lib/ship/launchpad-types.ts` for `LaunchpadStoreTile`. If `latest_deployment_id` isn't already a field, add it:

```ts
export interface LaunchpadStoreTile {
  // … existing fields
  latest_deployment_id?: string;
}
```

Then in `internal/responses/launchpad.go`, add the matching Go field:

```go
LatestDeploymentID string `json:"latest_deployment_id,omitempty"`
```

and populate in `buildStoreTile` right after `if latestDep == nil` check:

```go
tile.LatestDeploymentID = latestDep.ID.String()
```

(This is a minor backend change — include it in this task's commit since it's tiny and directly supports the frontend UX.)

- [ ] **Step 4: Thread `onSwitchToManaged` from `launchpad.tsx`**

In `src/components/ship/launchpad.tsx`, pass a handler to each `<StoreTile />` that calls the API method and refetches the launchpad:

```tsx
const handleSwitchToManaged = async (deploymentId: string) => {
  try {
    await apiClient.switchDeploymentToManaged(deploymentId);
    await refetch();
  } catch (err) {
    console.error("switch-to-managed failed", err);
  }
};

// <StoreTile ... onSwitchToManaged={handleSwitchToManaged} />
```

- [ ] **Step 5: Verify + commit (backend first)**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
go build ./...
go vet ./...
git add internal/responses/launchpad.go internal/controllers/launchpad_controller.go
git commit -m "feat(ship): expose latest_deployment_id on LaunchpadStoreTile

Needed by the frontend switch-to-managed secondary action so it can
POST to /v1/deployments/:id/switch-to-managed without a separate
lookup."
```

Then frontend:

```bash
cd /Users/knid/Projects/wemob/wemob-app
npm run lint
npm run build
git add src/lib/api.ts src/lib/ship/launchpad-types.ts src/components/ship/store-tile.tsx src/components/ship/launchpad.tsx
git commit -m "feat(ship): 'Or let Wemob ship it instead' secondary CTA

Renders under the primary 'Add connection' CTA on needs_connection
store tiles. One click POSTs to /v1/deployments/:id/switch-to-managed
and refetches the launchpad — the tile flips to 'submitted' and the
hero stops nagging."
```

---

## Task 5: PostHog flag ramp to 100%

This is an ops step, not a code change. Executed via the PostHog MCP or dashboard.

- [ ] **Step 1: Flip `new_ship_ux` rollout to 100%**

```
PostHog Project: Wemob
Flag: new_ship_ux  (ID 172590)
Action: set all filter groups' rollout_percentage to 100
```

Using the MCP: call `update-feature-flag` with the flag key and the full filters payload updated to `rollout_percentage: 100`.

- [ ] **Step 2: Verify in the app**

```
Open https://app.wemob.io/ship  (prod) and  /apps/:id/ship  as a non-admin user.
Expected: Launchpad renders (not the legacy Publish surface).
```

- [ ] **Step 3: No commit.**

---

## Task 6: End-to-end smoke

Live-only — no commit.

```
cd /Users/knid/Projects/wemob/wemob-backend && go run cmd/main.go &
cd /Users/knid/Projects/wemob/wemob-app && npm run dev
```

1. **Nudge cadence** — create a pending_connection deployment with a backdated `created_at` (3 days ago):

```sql
UPDATE deployments
SET created_at = NOW() - INTERVAL '3 days'
WHERE id = '<your pending deployment id>';
```

Wait one tick cycle (or trigger `r.tick(ctx)` manually via a test port). Expected: `pendingnudge: sent attempt=1 …` log + Brevo event + a `nudge_sent` event appended.

2. **Max-3 cap** — backdate 10 days; three ticks later, the 4th tick skips (no new event, no new email). Verify `SELECT COUNT(*) FROM deployment_events WHERE event_type='nudge_sent' AND deployment_id='<id>'` returns `3`.

3. **Switch-to-managed button** — on a `needs_connection` tile, click "Or let Wemob ship it instead". Expected: tile flips to `submitted`, `Slack` channel receives the notification, `deployments.method = 'wemob'`, `deployments.apple_connection_id = NULL`, new `submitted` event with `metadata.switched_to_managed = true`.

4. **Flag ramp** — confirm a freshly created user (not in the original 50% cohort) hits `/apps/:id/ship` and sees the new Launchpad. Rollback path: revert PostHog flag to previous rollout in the dashboard.

- [ ] **Step 1: No commit — user-run smoke.**

---

## Phase 5c-iii completion criteria

- `DeploymentEventNudgeSent = "nudge_sent"` exists.
- `BREVO_EVENT_PENDING_CONNECTION_NUDGE` exists.
- `Database.ListPendingConnectionDeployments` + `database.CountEventsOfType` exist.
- `pendingnudge.Runner` ticks every 6h, caps at 3 nudges per deployment, spaces at 2 days.
- `Controller.SwitchToManaged` + POST `/v1/deployments/:id/switch-to-managed` route exist.
- `LaunchpadStoreTile` exposes `latest_deployment_id`.
- `StoreTile` renders "Or let Wemob ship it instead" secondary CTA on `needs_connection`.
- Backend build + vet clean; frontend lint + build clean.
- PostHog `new_ship_ux` flag is at 100% rollout.

Ops notes:
- Brevo template for `pending_connection_nudge` needs to be set up in the Brevo dashboard with event attributes: `deployment_id, project_id, platform, attempt, max_attempts, submitted_at, switch_url`. The template copy should CC the switch-URL as a primary CTA.

## Out of scope

- Precise per-user nudge throttling (a user with 10 pending deployments gets 10 nudges over 2 days — acceptable at current scale, revisit if volume grows).
- Rich templating for different platforms (copy can be branched on the `platform` attribute inside Brevo).
- "Nudge dismissed" recording (if user opens Launchpad but does nothing, we keep nudging; dismissed state is a future idea).
- Admin triage / reviewer UI — out of scope, possibly never needed if the data flow stays healthy.
- Migrating the existing `new_ship_ux` cohort once the flag is at 100% (flag stays registered for easy rollback).
