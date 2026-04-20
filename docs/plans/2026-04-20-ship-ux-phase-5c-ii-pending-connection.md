# Ship UX — Phase 5c-ii · Pending-Connection Draft + Auto-Resume Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a Pilot user who's chosen DIY ship ("use my own Apple / Google account") but doesn't yet have a connection set up **park their submission** with a single click instead of being blocked or force-switched to Wemob-managed. The deployment persists as `status = "pending_connection"`; when the user later adds a matching connection (Pilot → Connections or Settings → Connections), the backend auto-resumes — attaches the new connection, flips status to `"Queued"`, and triggers Codemagic as if they'd submitted just now.

**Architecture:** Adds a boolean `defer_connection` to `CreateDeploymentRequest`. When true + method=`"user"`, `CreateDeploymentTask` bypasses the connection-ID requirement, skips the Codemagic trigger, sets `Status = "pending_connection"`, and emits a `submitted` event with `metadata.pending_connection = true`. A new `Controller.ResumePendingDeployments(ctx, user, platform, connectionID)` runs at the tail of `CreateAppleConnection` / `CreateGooglePlayConnection`: scans the user's deployments for ones in `pending_connection` on the matching platform, attaches the connection, triggers Codemagic, and re-emits `submitted` (actor = system, metadata.resumed = true). Launchpad classifies the new status as `warn` with an inline "Add connection" CTA.

**Tech stack:** Go 1.22+, Bun, chi. Next.js 16, React 19, Tailwind 4.

**Design spec:** `docs/design/2026-04-18-wemob-publish-ux-redesign-design.md` ("Draft submissions are first-class").

---

## Test strategy

Build + vet clean on backend; lint + build clean on frontend. Smoke steps documented for user.

## File structure overview

### Backend — new

```
(none — all changes are to existing files)
```

### Backend — modified

```
internal/requests/deployment.go                        — add DeferConnection field
internal/controllers/deployment.go                     — pending_connection path
internal/controllers/pending_connection.go  (NEW)      — ResumePendingDeployments helper
internal/controllers/credentials.go                    — call Resume* at end of Create*Connection
internal/controllers/ship_controller.go                — classify pending_connection as warn
internal/controllers/launchpad_controller.go           — StoreTile + hero for pending_connection
```

### Frontend — modified

```
src/lib/ship/launchpad-types.ts                        — add "needs_connection" tile status
src/lib/api.ts                                         — defer_connection on deployment payload
src/components/ship/pilot/pilot-state.ts               — add deferConnection field
src/components/ship/pilot/step-connection.tsx          — "Set up later" affordance
src/components/ship/pilot/step-review.tsx              — callout instead of blocking checklist
src/components/ship/store-tile.tsx                     — render needs_connection
```

## Why these boundaries

- **Status, not a new model.** Re-using `Deployment` keeps the timeline, events, and polling unchanged. A "pending" deployment is just a deployment whose connection will be wired up later.
- **Resume on `Create*Connection`, not on every request.** Auto-resume runs exactly once per connection add. That's the event that makes a pending deployment shippable. Alternatives (polling for pending deployments, running on every request) add complexity for zero UX gain.
- **`pending_connection` = `warn`, not `info`.** The user has an action to take. `warn` is the correct kind both for the Launchpad hero ladder and for `/ship` filters.
- **Frontend keeps one request path.** `defer_connection: true` is a flag on the existing submit payload, not a separate endpoint. Less to keep in sync.

---

## Task 1: Backend — `pending_connection` path on submit

**Files:**
- Modify: `internal/requests/deployment.go`
- Modify: `internal/controllers/deployment.go`
- Modify: `internal/controllers/ship_controller.go`

- [ ] **Step 1: Add `DeferConnection` to the request**

In `internal/requests/deployment.go`, append a field:

```go
// DeferConnection — when true with method = "user", the deployment is
// created in status "pending_connection" instead of being submitted.
// Codemagic is not triggered; the user is telling us they'll set up
// their Apple / Google connection later. The deployment auto-resumes
// when the matching connection is added (see ResumePendingDeployments).
DeferConnection bool `json:"defer_connection,omitempty"`
```

- [ ] **Step 2: Branch in `CreateDeploymentTask` for the pending path**

In `internal/controllers/deployment.go`, `CreateDeploymentTask`:

Replace the strict validation that requires connection IDs when method=`"user"`:

```go
appStoreCredId, err := uuid.Parse(deploymentReq.AppleConnectionID)
if err != nil {
    if deploymentReq.Method == "wemob" || deploymentReq.DeferConnection {
        appStoreCredId = uuid.Nil
    } else {
        if slices.Contains(deploymentReq.Platforms, "ios") {
            return nil, errors.New("invalid app store credentials id")
        }
    }
}
googlePlayCredId, err := uuid.Parse(deploymentReq.GooglePlayConnectionID)
if err != nil {
    if deploymentReq.Method == "wemob" || deploymentReq.DeferConnection {
        googlePlayCredId = uuid.Nil
    } else {
        if slices.Contains(deploymentReq.Platforms, "android") {
            return nil, errors.New("invalid google play credentials id")
        }
    }
}
```

Compute the initial status:

```go
initialStatus := "Queued"
if deploymentReq.DeferConnection && deploymentReq.Method == "user" {
    initialStatus = "pending_connection"
}
```

And use it in the `CreateDeployment` call (replace the hard-coded `"Queued"`):

```go
deployment, err := c.DB.CreateDeployment(&models.Deployment{
    // ... existing fields …
    Status: initialStatus,
    // ... rest unchanged
})
```

Adjust the submitted-event metadata to flag the pending nature:

```go
submittedMeta := map[string]interface{}{"method": deployment.Method}
if initialStatus == "pending_connection" {
    submittedMeta["pending_connection"] = true
}
if err := database.AppendDeploymentEvent(context.Background(), c.DB.DB, deployment.ID,
    models.DeploymentEventSubmitted, models.DeploymentEventActorUser, &actorID,
    submittedMeta); err != nil {
    slog.Warn("append deployment_events.submitted failed", "err", err)
}
```

Guard the Codemagic trigger + CRM events inside `if deploymentReq.Method == "user"` so the pending path returns early after the CreateDeployment:

```go
if deploymentReq.Method == "user" && !deploymentReq.DeferConnection {
    // ... existing iOS / Android Codemagic trigger code unchanged ...
} else if deploymentReq.Method == "wemob" {
    // ... existing Slack notify unchanged ...
}
// Pending-connection path falls through to the response below.
```

- [ ] **Step 3: Classify `pending_connection` as warn**

In `internal/controllers/ship_controller.go`, `classifyDeploymentStatus`:

```go
case "pending_connection":
    return "Waiting for connection", "warn"
```

Insert it before the default return.

- [ ] **Step 4: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
go build ./...
go vet ./...
git add internal/requests/deployment.go internal/controllers/deployment.go internal/controllers/ship_controller.go
git commit -m "feat(ship): add pending_connection path on CreateDeploymentTask

When defer_connection is true + method = 'user', skip the Codemagic
trigger, create the deployment with Status = 'pending_connection',
and emit submitted with metadata.pending_connection = true.

Resume logic lives in a follow-up commit so the changes to credentials
handlers land in one cohesive diff."
```

---

## Task 2: Backend — `ResumePendingDeployments` + call sites

**Files:**
- Create: `internal/controllers/pending_connection.go`
- Modify: `internal/controllers/credentials.go`

- [ ] **Step 1: Write the helper**

Create `internal/controllers/pending_connection.go`:

```go
package controllers

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/codemagic"
)

// ResumePendingDeployments attaches the newly-added connection to any
// pending_connection deployments the user has for the matching platform,
// flips their status to Queued, triggers Codemagic, and re-emits a
// submitted event with actor_type = system + metadata.resumed = true.
//
// platform is "ios" or "android". connectionID is the uuid of the
// connection just created. User scoping prevents cross-user pollution
// if a staff member ever adds a connection on another user's behalf.
//
// All errors are logged but do not abort the outer handler — the user
// has their connection and can manually submit if auto-resume hiccups.
func (c *Controller) ResumePendingDeployments(ctx context.Context, user *models.User, platform string, connectionID uuid.UUID) {
	deployments, err := c.GetDeployments(user)
	if err != nil {
		slog.Warn("resume: list deployments failed", "user_id", user.ID, "err", err)
		return
	}

	for _, d := range deployments {
		if d.Status != "pending_connection" {
			continue
		}
		if !slices.Contains(d.Platforms, platform) {
			continue
		}
		// Skip if that platform already has a connection set (happens
		// on multi-platform pending deployments when the second
		// connection is added).
		if platform == "ios" && d.AppleConnectionID != uuid.Nil {
			continue
		}
		if platform == "android" && d.GooglePlayConnectionID != uuid.Nil {
			continue
		}

		if err := c.resumeOne(ctx, user, d, platform, connectionID); err != nil {
			slog.Warn("resume: resume deployment failed",
				"user_id", user.ID,
				"deployment_id", d.ID,
				"platform", platform,
				"err", err)
			continue
		}
		slog.Info("resume: pending_connection resumed",
			"deployment_id", d.ID,
			"platform", platform,
			"connection_id", connectionID)
	}
}

func (c *Controller) resumeOne(ctx context.Context, user *models.User, d models.Deployment, platform string, connectionID uuid.UUID) error {
	// Attach connection.
	if platform == "ios" {
		d.AppleConnectionID = connectionID
	} else {
		d.GooglePlayConnectionID = connectionID
	}

	// Flip status. If it has BOTH platforms pending and only one was just
	// fulfilled, keep it at pending_connection until the other platform's
	// connection arrives.
	stillPending := false
	for _, p := range d.Platforms {
		switch p {
		case "ios":
			if d.AppleConnectionID == uuid.Nil {
				stillPending = true
			}
		case "android":
			if d.GooglePlayConnectionID == uuid.Nil {
				stillPending = true
			}
		}
	}
	if !stillPending {
		d.Status = "Queued"
	}
	if _, err := c.DB.UpdateDeployment(&d); err != nil {
		return fmt.Errorf("update deployment: %w", err)
	}

	// If there are still missing connections, don't trigger Codemagic yet.
	if stillPending {
		return nil
	}

	project, err := c.DB.GetProjectById(d.ProjectID.String())
	if err != nil {
		return fmt.Errorf("get project: %w", err)
	}
	build, err := c.DB.GetProjectBuildById(d.BuildID.String())
	if err != nil {
		return fmt.Errorf("get build: %w", err)
	}

	// Trigger per-platform. Multi-platform deployments fire both.
	if slices.Contains(d.Platforms, "ios") {
		cred, err := c.DB.GetAppleConnectionById(d.AppleConnectionID.String())
		if err != nil {
			return fmt.Errorf("get apple connection: %w", err)
		}
		if _, err := c.CodemagicService.TriggerIOSDeploy(codemagic.IOSDeployParams{
			BuildID:            d.BuildID.String(),
			DeploymentID:       d.ID.String(),
			PackageName:        project.PackageName,
			TeamID:             cred.TeamID,
			KeyID:              cred.KeyID,
			IssuerID:           cred.IssuerID,
			P8KeyS3Path:        cred.P8KeyPath,
			Title:              d.Title,
			Description:        d.Description,
			Screenshots:        d.ScreenshotPaths,
			Icon:               build.Icon,
			Locale:             d.Locale,
			NewApp:             true,
			ChangedBuild:       true,
			TitleChanged:       true,
			DescChanged:        true,
			ScreenshotsChanged: true,
			RolloutTrack:       d.RolloutTrack,
			ReleaseNotes:       d.ReleaseNotes,
		}); err != nil {
			return fmt.Errorf("trigger iOS deploy: %w", err)
		}
		go c.CRMService.SendEvent(user, constants.BREVO_EVENT_USER_APPLE_DEPLOYMENT_STARTED, map[string]interface{}{
			"project_id":       project.ID.String(),
			"deployment_id":    d.ID.String(),
			"build_id":         build.ID.String(),
			"deployment_title": d.Title,
		})
	}
	if slices.Contains(d.Platforms, "android") {
		cred, err := c.DB.GetGooglePlayConnectionById(d.GooglePlayConnectionID.String())
		if err != nil {
			return fmt.Errorf("get google connection: %w", err)
		}
		if _, err := c.CodemagicService.TriggerAndroidDeploy(codemagic.AndroidDeployParams{
			BuildID:              d.BuildID.String(),
			DeploymentID:         d.ID.String(),
			PackageName:          project.PackageName,
			Version:              build.BuildNumber,
			Title:                d.Title,
			Description:          d.Description,
			Screenshots:          d.ScreenshotPaths,
			IconPath:             d.IconPath,
			Locale:               d.Locale,
			ServiceAccountS3Path: cred.ServiceAccountPath,
			NewApp:               true,
			ChangedBuild:         true,
			TitleChanged:         true,
			DescChanged:          true,
			ScreenshotsChanged:   true,
			IconChanged:          true,
		}); err != nil {
			return fmt.Errorf("trigger Android deploy: %w", err)
		}
		go c.CRMService.SendEvent(user, constants.BREVO_EVENT_USER_ANDROID_DEPLOYMENT_STARTED, map[string]interface{}{
			"project_id":       project.ID.String(),
			"deployment_id":    d.ID.String(),
			"build_id":         build.ID.String(),
			"deployment_title": d.Title,
		})
	}

	// Re-emit a submitted event for the resume. System actor because the
	// user's action was adding the connection, not re-clicking submit.
	_ = database.AppendDeploymentEvent(ctx, c.DB.DB, d.ID,
		models.DeploymentEventSubmitted, models.DeploymentEventActorSystem, nil,
		map[string]interface{}{"resumed": true, "platform": platform})
	return nil
}
```

- [ ] **Step 2: Call from `CreateAppleConnection`**

In `internal/controllers/credentials.go` `CreateAppleConnection`, after `cred, err := c.DB.CreateAppleConnection(credential)` success:

```go
go c.ResumePendingDeployments(context.Background(), user, "ios", cred.ID)
```

Use a goroutine because the caller's response shouldn't block on Codemagic triggers. The helper is already log-safe on error.

- [ ] **Step 3: Call from `CreateGooglePlayConnection`**

Same pattern after the successful `CreateGooglePlayConnection` DB call:

```go
go c.ResumePendingDeployments(context.Background(), user, "android", cred.ID)
```

- [ ] **Step 4: Ensure `context` imported in credentials.go**

Add `"context"` to the import block if not already present.

- [ ] **Step 5: Verify + commit**

```bash
go build ./...
go vet ./...
git add internal/controllers/pending_connection.go internal/controllers/credentials.go
git commit -m "feat(ship): auto-resume pending_connection deployments on Create*Connection

ResumePendingDeployments scans the user's deployments for any in
pending_connection on the matching platform (ios/android), attaches
the newly-created connection, and triggers Codemagic as if the user
had re-submitted. Runs asynchronously so the connection-create
response doesn't wait on Codemagic round-trips.

Multi-platform pending deployments (rare) stay pending until both
sides are wired — only then does status flip to Queued and
Codemagic trigger."
```

---

## Task 3: Backend — Launchpad store tile + hero for pending_connection

**Files:**
- Modify: `internal/controllers/launchpad_controller.go`

- [ ] **Step 1: Extend `buildStoreTile` switch**

In `buildStoreTile`, the `switch strings.ToLower(latestDep.Status)` block, add a case before `default`:

```go
case "pending_connection":
    tile.Status = "needs_connection"
    tile.PrimaryAction = "Add connection"
    // Advertise missing pieces so the tile body surfaces specifics.
    switch platform {
    case "ios":
        tile.MissingItems = []string{"App Store Connect API key"}
    case "android":
        tile.MissingItems = []string{"Google Play service account"}
    }
```

- [ ] **Step 2: Add a pending_connection branch to `computeHero`**

Insert BEFORE the rejected branches (pending_connection takes highest priority — a user-actionable block beats a store-rejected block because the user has something concrete and pending with their own hands):

```go
if r.Stores.AppStore.Status == "needs_connection" {
    return responses.LaunchpadHero{
        Summary:     "Waiting for your Apple connection — add it to ship.",
        PrimaryCTA:  "Add App Store connection",
        PrimaryHref: "#app-store",
    }
}
if r.Stores.PlayStore.Status == "needs_connection" {
    return responses.LaunchpadHero{
        Summary:     "Waiting for your Google Play connection — add it to ship.",
        PrimaryCTA:  "Add Play Store connection",
        PrimaryHref: "#play-store",
    }
}
```

- [ ] **Step 3: Verify + commit**

```bash
go build ./...
go vet ./...
git add internal/controllers/launchpad_controller.go
git commit -m "feat(ship): surface pending_connection on Launchpad hero + store tile

Hero takes top priority when a store is blocked on a user-owned
connection, because adding the key is a concrete two-minute action
the user can do right now. Store tile flips to 'needs_connection'
with an 'Add connection' primary CTA and a MissingItems chip listing
the specific key type required."
```

---

## Task 4: Frontend — Pilot "Set up later" affordance

**Files:**
- Modify: `src/components/ship/pilot/pilot-state.ts`
- Modify: `src/components/ship/pilot/step-connection.tsx`
- Modify: `src/components/ship/pilot/step-review.tsx`
- Modify: `src/lib/api.ts`

- [ ] **Step 1: Extend `PilotState`**

In `pilot-state.ts`, add:

```ts
export interface PilotState {
  // ... existing fields …
  connectionValidated: boolean;
  deferConnection: boolean;   // new
  // ... rest unchanged
}
```

And in `initialPilotState`:

```ts
return {
  // ...
  connectionValidated: false,
  deferConnection: false,
  // ...
};
```

- [ ] **Step 2: "Set up later" UI in `step-connection.tsx`**

Below the two existing `MethodRadio` options, add a conditional option that appears only when `state.method === "user"`:

```tsx
{state.method === "user" && (
  <label className="mt-3 flex items-start gap-2 text-[12px] cursor-pointer">
    <input
      type="checkbox"
      checked={state.deferConnection}
      onChange={(e) => onPatch({ deferConnection: e.target.checked })}
      className="mt-0.5"
    />
    <span className="text-[var(--ink)]">
      Set up my {state.target === "app_store" ? "Apple" : "Google Play"} account later.
      <span className="block text-[var(--ink-muted)] mt-0.5 text-[11px] leading-[1.5]">
        We&apos;ll save your listing and auto-submit the moment your connection lands.
      </span>
    </span>
  </label>
)}
```

Also: when `deferConnection` is true, hide the `AppleConnectionValidator` / `GoogleConnectionValidator` block. Change the existing conditional:

```tsx
{state.method === "user" && !state.deferConnection && (
  <div className="mt-5 border-t border-[var(--border)] pt-4">
    {state.target === "app_store" ? (
      <AppleConnectionValidator onPatch={onPatch} />
    ) : (
      <GoogleConnectionValidator onPatch={onPatch} />
    )}
  </div>
)}
```

- [ ] **Step 3: Review step — callout instead of blocker**

In `step-review.tsx`, the existing checklist item:

```ts
{ label: "Connection selected or validated", ok: state.method === "wemob" || state.connectionValidated },
```

Change to:

```ts
{
  label: state.deferConnection
    ? "Will auto-submit when your connection lands"
    : "Connection selected or validated",
  ok: state.method === "wemob" || state.connectionValidated || state.deferConnection,
},
```

If the review renders a callout panel (check the existing JSX — if not present, skip this substep), append when `state.deferConnection`:

```tsx
{state.deferConnection && (
  <div className="mt-4 px-3 py-2 rounded-md text-[12px] bg-[var(--accent-soft)] border border-[var(--accent)]/30 text-[var(--ink)]">
    We&apos;ll hold this submission until you add your {state.target === "app_store" ? "Apple" : "Google"} connection.
    You&apos;ll see a reminder on the Launchpad. Add the connection any time to ship.
  </div>
)}
```

- [ ] **Step 4: Submit payload**

In `pilot.tsx` (or wherever `apiClient.createDeployment` is called from the Pilot), include `defer_connection: state.deferConnection` on the payload.

If the payload shape is typed in `src/lib/api.ts`, add the field there:

```ts
export interface CreateDeploymentPayload {
  // ... existing fields
  defer_connection?: boolean;
}
```

- [ ] **Step 5: Also update the Pilot's completion gate**

In `pilot.tsx`, the `canSubmit` / step-4 gate currently requires `method === "wemob" || (method === "user" && connectionValidated)`. Extend to:

```ts
return s.method === "wemob" || (s.method === "user" && (s.connectionValidated || s.deferConnection));
```

- [ ] **Step 6: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-app
npm run lint
npm run build
git add src/components/ship/pilot/ src/lib/api.ts
git commit -m "feat(ship): add 'Set up later' affordance to Pilot step-connection

Checkbox under the 'Ship from your own account' option. When checked,
the connection validator is hidden, the review checklist switches to
a friendly 'Will auto-submit when your connection lands' note, and
the submit gate opens. Frontend sends defer_connection: true; the
backend persists the deployment in pending_connection status."
```

---

## Task 5: Frontend — Launchpad pending_connection tile rendering

**Files:**
- Modify: `src/lib/ship/launchpad-types.ts`
- Modify: `src/components/ship/store-tile.tsx`

- [ ] **Step 1: Extend the TS status union**

In `launchpad-types.ts`, find `LaunchpadStoreTile.status` and add `"needs_connection"`:

```ts
export interface LaunchpadStoreTile {
  platform: "ios" | "android";
  status:
    | "not_configured"
    | "ready_to_ship"
    | "submitted"
    | "in_review"
    | "live"
    | "rejected"
    | "needs_connection";    // new
  // ... rest unchanged
}
```

- [ ] **Step 2: Render the new status in `store-tile.tsx`**

Add a branch in whatever status → label / color map drives the component. For example, if the file has a `statusMeta` lookup:

```ts
const statusMeta: Record<LaunchpadStoreTile["status"], { label: string; kind: string }> = {
  // ... existing entries
  needs_connection: { label: "Waiting for connection", kind: "warn" },
};
```

And surface `MissingItems[0]` prominently when status is `needs_connection` — something like:

```tsx
{tile.status === "needs_connection" && tile.missing_items?.[0] && (
  <div className="text-[11px] text-[var(--ink-muted)] mt-1">
    Needs: {tile.missing_items[0]}
  </div>
)}
```

The exact JSX depends on the current StoreTile structure — mirror the "rejected" / "not_configured" branches.

- [ ] **Step 3: Verify + commit**

```bash
npm run lint
npm run build
git add src/lib/ship/launchpad-types.ts src/components/ship/store-tile.tsx
git commit -m "feat(ship): render needs_connection store tile with warn styling

Tile shows 'Waiting for connection' label, warn color, and a 'Needs:
<key type>' subtext. Primary action routes to Pilot's connection
step so the user can complete setup in one click."
```

---

## Task 6: End-to-end smoke

Live-only — documented for user. No commit.

```
cd /Users/knid/Projects/wemob/wemob-backend && go run cmd/main.go &
cd /Users/knid/Projects/wemob/wemob-app && npm run dev
```

Test flow:

1. **Defer flow**
   - Open Pilot on an app that has no Apple connection.
   - Pick "Ship from your Apple Developer account".
   - Check "Set up my Apple account later".
   - Advance to review. The checklist says "Will auto-submit when your connection lands". Submit.
   - Launchpad status hero: "Waiting for your Apple connection — add it to ship." Store tile: `needs_connection` warn color.
   - `/ship` table: row has status_kind = warn, label "Waiting for connection".

2. **Auto-resume**
   - Settings → Connections → add an Apple connection that validates.
   - Within seconds, the deployment flips to `Queued` → `Deploying` (Codemagic triggered). Timeline shows a second `submitted` event with `metadata.resumed = true` attributed to `system`.

3. **Multi-platform (optional)**
   - Deploy to both iOS and Android with `defer_connection: true` and neither connection set.
   - Add Apple connection → deployment still shows `pending_connection` (Android still missing).
   - Add Google Play connection → deployment flips to `Queued` and both Codemagic triggers fire.

```sql
-- Useful:
SELECT id, status, apple_connection_id, google_play_connection_id FROM deployments
  WHERE status = 'pending_connection';

SELECT event_type, actor_type, metadata FROM deployment_events
  WHERE deployment_id = '<id>' ORDER BY occurred_at;
```

- [ ] **Step 1: No commit — user-run smoke.**

---

## Phase 5c-ii completion criteria

- `CreateDeploymentRequest` has `DeferConnection` field.
- `CreateDeploymentTask`: method=user + DeferConnection → Status="pending_connection", no Codemagic trigger, no connection IDs required.
- `Controller.ResumePendingDeployments(ctx, user, platform, connectionID)` exists and runs on connection add.
- `classifyDeploymentStatus` maps `pending_connection` → `("Waiting for connection", "warn")`.
- `buildStoreTile` emits `status = "needs_connection"` for `pending_connection` latest deployment.
- `computeHero` prioritizes `needs_connection` store above rejected branches.
- Pilot has a "Set up later" checkbox that bypasses connection validation and unblocks submit.
- Pilot submit sends `defer_connection: true`.
- StoreTile renders `needs_connection` with warn styling.
- Backend build + vet clean. Frontend lint + build clean.

Phase 5c-iii (email nudges + flag ramp) unblocked.

## Out of scope

- Email nudges — every 2 days, max 3, "switch to managed" escape (Phase 5c-iii).
- "Switch to Wemob-managed" escape on the pending Launchpad tile (Phase 5c-iii).
- Admin UI to see pending_connection deployments across users.
- Per-store pending granularity — if iOS resumes but Android is still pending, we currently use `stillPending` locally but don't emit granular events.
