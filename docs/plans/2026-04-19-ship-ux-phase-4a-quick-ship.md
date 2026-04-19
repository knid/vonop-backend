# Ship UX — Phase 4a · Quick Ship Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the full 5-step Pilot with a one-screen Quick Ship sheet when the user is submitting an update (not a first-time submission). Quick Ship shows release notes (AI-drafted from the existing Phase 3b endpoint), unchanged-since-last-release summary, binary + connection details, and a rollout track selector. Target UX: 30 seconds from open to submit with `⌘⏎`.

**Architecture:** Pure frontend. Reuses every backend capability we already have: the Phase 3b `POST /v1/apps/:id/listing/draft` with `kind: "release_notes"`, the existing `/v1/deployments` create endpoint, and the Phase 2a/2b Launchpad data. New component sits alongside Pilot; Launchpad routes each store tile's primary action to Pilot OR Quick Ship based on store status.

**Tech stack:** Next.js 16, React 19, Tailwind 4, sonner. No new deps.

**Design spec:** `docs/design/2026-04-18-wemob-publish-ux-redesign-design.md` ("Quick Ship" section).

---

## Test strategy

Same as prior phases: `npm run lint` + `npm run build` clean. Runtime smoke deferred to the user.

## File structure overview

### New files

```
src/components/ship/quick-ship/quick-ship.tsx           — single-screen sheet
src/components/ship/quick-ship/rollout-selector.tsx     — Internal/Closed/Open/Production toggle
src/components/ship/quick-ship/unchanged-summary.tsx    — "5 of 7 unchanged" row
src/components/ship/quick-ship/quick-ship-state.ts      — shared QuickShipState
src/hooks/use-quick-ship.ts                             — open / close / refetch-on-submit
```

### Modified files

```
src/components/ship/launchpad.tsx                       — host Quick Ship overlay alongside Pilot
src/components/ship/store-tile.tsx                      — no change (onAction already wired)
src/lib/ship/launchpad-types.ts                         — add RolloutTrack union
```

## Why these boundaries

- **Quick Ship lives in its own folder** (`components/ship/quick-ship/`), not inside `pilot/`. Per the design spec: "Quick Ship is a *different* component from Pilot, not a cut-down version". Keeps their lifetimes independent and frees each to evolve.
- **`use-quick-ship` hook, separate from `use-pilot`**. Same reason — the two flows have different state shapes and open/close paths. Small duplication, clean semantics.
- **Rollout selector as its own component**. It'll be reused in Phase 4b for the ⌘K palette preview and in Phase 5 for managed-track observability. One source of truth.
- **No backend changes**. The design explicitly reuses `/v1/deployments` + `/listing/draft`. Any new endpoint would be scope creep.

---

## Task 1: Shared state + helper

**Files:**
- Create: `src/components/ship/quick-ship/quick-ship-state.ts`
- Modify: `src/lib/ship/launchpad-types.ts`

- [ ] **Step 1: Add `RolloutTrack` union to launchpad-types**

Append to `src/lib/ship/launchpad-types.ts`:

```ts
export type RolloutTrack = "internal" | "closed" | "open" | "production";
```

- [ ] **Step 2: Define QuickShipState**

Create `src/components/ship/quick-ship/quick-ship-state.ts`:

```ts
import type { LaunchpadResponse, LaunchpadStoreTile, RolloutTrack } from "@/lib/ship/launchpad-types";

export type QuickShipTarget = "play_store" | "app_store";

export interface QuickShipState {
  target: QuickShipTarget;
  releaseNotes: string;
  rollout: RolloutTrack;
}

export function initialQuickShipState(target: QuickShipTarget, data: LaunchpadResponse): QuickShipState {
  return {
    target,
    releaseNotes: "",
    rollout: "production",
  };
}

/**
 * canQuickShip — decides whether a store tile's primary action should
 * open Quick Ship (subsequent submission) or the Pilot (first-time
 * setup). Subsequent = there's a currently-live version OR an
 * update-available state, i.e. the user has already navigated the
 * first-submission ceremony.
 */
export function canQuickShip(tile: LaunchpadStoreTile): boolean {
  return tile.status === "live" || tile.status === "update_available";
}
```

- [ ] **Step 3: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-app
npm run build
git add src/lib/ship/launchpad-types.ts src/components/ship/quick-ship/quick-ship-state.ts
git commit -m "feat(ship): add QuickShipState + canQuickShip helper

Shared types + the decision rule that routes a store tile's primary
action to Pilot (first-time) vs Quick Ship (subsequent). Tile states
'live' and 'update_available' trigger Quick Ship; everything else
falls back to the full Pilot flow."
```

---

## Task 2: `useQuickShip` hook

**Files:**
- Create: `src/hooks/use-quick-ship.ts`

- [ ] **Step 1: Write the hook**

```ts
"use client";

import { useCallback, useState } from "react";
import type { LaunchpadResponse } from "@/lib/ship/launchpad-types";
import type { QuickShipState, QuickShipTarget } from "@/components/ship/quick-ship/quick-ship-state";
import { initialQuickShipState } from "@/components/ship/quick-ship/quick-ship-state";

export function useQuickShip() {
  const [state, setState] = useState<QuickShipState | null>(null);

  const open = useCallback((target: QuickShipTarget, data: LaunchpadResponse) => {
    setState(initialQuickShipState(target, data));
  }, []);

  const close = useCallback(() => setState(null), []);

  const patch = useCallback((p: Partial<QuickShipState>) => {
    setState((s) => (s ? { ...s, ...p } : s));
  }, []);

  return { state, isOpen: !!state, open, close, patch };
}
```

- [ ] **Step 2: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-app
npm run build
git add src/hooks/use-quick-ship.ts
git commit -m "feat(ship): add useQuickShip hook

Mirrors usePilot's open/close/patch shape but with a smaller state
surface. Own hook (not shared with Pilot) so the two flows evolve
independently."
```

---

## Task 3: `RolloutSelector` component

**Files:**
- Create: `src/components/ship/quick-ship/rollout-selector.tsx`

- [ ] **Step 1: Write the component**

```tsx
"use client";

import type { RolloutTrack } from "@/lib/ship/launchpad-types";
import { cn } from "@/lib/utils";

interface RolloutSelectorProps {
  target: "play_store" | "app_store";
  value: RolloutTrack;
  onChange: (value: RolloutTrack) => void;
}

const PLAY_TRACKS: { value: RolloutTrack; label: string }[] = [
  { value: "internal", label: "Internal" },
  { value: "closed", label: "Closed" },
  { value: "open", label: "Open" },
  { value: "production", label: "Production" },
];

const APP_TRACKS: { value: RolloutTrack; label: string }[] = [
  { value: "internal", label: "TestFlight · Internal" },
  { value: "closed", label: "TestFlight · External" },
  { value: "production", label: "App Store Review" },
];

/**
 * RolloutSelector — pill selector for the release track. Play Store
 * offers 4 tracks; App Store collapses to 3 (Internal TestFlight,
 * External TestFlight, App Store Review). Phase 4a treats the choice
 * as display-only for App Store — the existing /v1/deployments endpoint
 * publishes to App Store Review for now; Phase 4b wires the TestFlight
 * tracks into fastlane pilot.
 */
export function RolloutSelector({ target, value, onChange }: RolloutSelectorProps) {
  const tracks = target === "play_store" ? PLAY_TRACKS : APP_TRACKS;
  return (
    <div className="flex flex-col gap-1">
      <div className="text-[10px] uppercase tracking-[0.5px] text-[var(--ink-faint)]">
        Rollout
      </div>
      <div className="flex bg-[var(--bg-raised)] rounded-lg p-1 text-[11px]">
        {tracks.map((t) => {
          const active = value === t.value;
          return (
            <button
              key={t.value}
              type="button"
              onClick={() => onChange(t.value)}
              className={cn(
                "flex-1 px-2 py-1 rounded-md text-[11px] font-medium transition-colors",
                active
                  ? "bg-[var(--bg-surface)] text-[var(--ink)] shadow-sm"
                  : "text-[var(--ink-muted)] hover:text-[var(--ink)]",
              )}
            >
              {t.label}
            </button>
          );
        })}
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-app
npm run build
git add src/components/ship/quick-ship/rollout-selector.tsx
git commit -m "feat(ship): add RolloutSelector pill component

Play Store: Internal / Closed / Open / Production (4 tracks).
App Store: TestFlight Internal / TestFlight External / App Store Review.

Phase 4a uses it display-only for App Store — Phase 4b wires the
TestFlight tracks into fastlane pilot."
```

---

## Task 4: `UnchangedSummary` component

**Files:**
- Create: `src/components/ship/quick-ship/unchanged-summary.tsx`

- [ ] **Step 1: Write the component**

```tsx
"use client";

import type { LaunchpadListing, LaunchpadListingTile } from "@/lib/ship/launchpad-types";

interface UnchangedSummaryProps {
  listing: LaunchpadListing;
}

/**
 * Renders the "5 of 7 unchanged since last release" row. In Quick Ship
 * we don't edit the listing at all — that's what the Pilot or a
 * listing-editor panel is for. This component just surfaces what the
 * user is implicitly keeping the same.
 */
export function UnchangedSummary({ listing }: UnchangedSummaryProps) {
  const displayable = listing.tiles.filter((t) => t.status !== "empty");
  const ready = displayable.filter((t) => t.status === "ready");
  const needsWork = displayable.filter((t) => t.status !== "ready");

  return (
    <div className="rounded-xl border border-[var(--border)] bg-[var(--bg-surface)] px-4 py-3">
      <div className="flex items-center gap-2 mb-2">
        <span className="text-[10px] uppercase tracking-[0.5px] text-[var(--ink-faint)]">
          Listing
        </span>
        <span className="text-[11px] text-[var(--ink-muted)]">
          {ready.length} of {displayable.length} unchanged from last release
        </span>
      </div>
      {needsWork.length > 0 && (
        <div className="text-[11px] text-[#8b6520] leading-[1.55]">
          {needsWork.length} item{needsWork.length === 1 ? "" : "s"} flagged:{" "}
          {needsWork.map((t) => t.label).join(", ")}. Edit in Launchpad → Listing tile to address before shipping.
        </div>
      )}
    </div>
  );
}
```

- [ ] **Step 2: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-app
npm run build
git add src/components/ship/quick-ship/unchanged-summary.tsx
git commit -m "feat(ship): add UnchangedSummary component

Reads the Launchpad's listing state and shows 'X of Y unchanged from
last release' with a flag-callout for any non-ready items. Quick Ship
never edits the listing itself — users drop back to the Launchpad
for that."
```

---

## Task 5: `QuickShip` main component

**Files:**
- Create: `src/components/ship/quick-ship/quick-ship.tsx`

- [ ] **Step 1: Write the component**

```tsx
"use client";

import { useEffect, useState } from "react";
import { X, Sparkles, Loader2 } from "lucide-react";
import { toast } from "sonner";
import { apiClient } from "@/lib/api";
import type { LaunchpadResponse } from "@/lib/ship/launchpad-types";
import type { QuickShipState } from "./quick-ship-state";
import { RolloutSelector } from "./rollout-selector";
import { UnchangedSummary } from "./unchanged-summary";

interface QuickShipProps {
  state: QuickShipState;
  data: LaunchpadResponse;
  onPatch: (p: Partial<QuickShipState>) => void;
  onClose: () => void;
  onSubmitted: () => void;
}

export function QuickShip({ state, data, onPatch, onClose, onSubmitted }: QuickShipProps) {
  const [draftingNotes, setDraftingNotes] = useState(false);
  const [submitting, setSubmitting] = useState(false);

  const platform = state.target === "app_store" ? "ios" : "android";
  const storeTile = state.target === "app_store" ? data.stores.app_store : data.stores.play_store;
  const method = storeTile.connection_kind === "managed" ? "wemob" : "user";
  const v1ProjectID = data.app.v1_project_id ?? "";

  // Pick the latest completed build for this platform — same pattern
  // as StepReview.
  const buildCandidates = [...data.builds.active, ...data.builds.recent].filter(
    (b) => b.platform === platform && b.finished,
  );
  const latestBuild = buildCandidates[0];
  const canSubmit =
    !!latestBuild && !!v1ProjectID && state.releaseNotes.trim().length > 0 && !submitting;

  async function draftNotes() {
    setDraftingNotes(true);
    try {
      const r = await apiClient.draftListing(data.app.id, {
        target: state.target,
        kind: "release_notes",
      });
      onPatch({ releaseNotes: r.value });
    } catch (err) {
      toast.error("Draft failed", {
        description: err instanceof Error ? err.message : String(err),
      });
    } finally {
      setDraftingNotes(false);
    }
  }

  async function submit() {
    if (!canSubmit || !latestBuild || !v1ProjectID) return;
    setSubmitting(true);
    try {
      await apiClient.createDeployment({
        project_id: v1ProjectID,
        build_id: latestBuild.build_id,
        platforms: [platform],
        icon_id: data.app.icon_s3_key ?? "",
        title: "", // unchanged — reuse existing listing
        description: "", // unchanged
        locale: "en-US",
        method,
        google_play_credentials_id: "",
        app_store_credentials_id: "",
        screenshot_paths: [],
      });
      toast.success(`Shipping v${data.app.version_name} to ${state.target === "app_store" ? "App Store" : "Play Store"}`, {
        description:
          method === "wemob"
            ? "Our team will publish within 2–3 business days."
            : `Uploading to the ${state.rollout} track now.`,
      });
      onSubmitted();
      onClose();
    } catch (err) {
      toast.error("Couldn't submit", {
        description: err instanceof Error ? err.message : String(err),
      });
    } finally {
      setSubmitting(false);
    }
  }

  // ⌘⏎ / Ctrl+⏎ submits from anywhere in the sheet.
  useEffect(() => {
    function handleKey(e: KeyboardEvent) {
      if ((e.metaKey || e.ctrlKey) && e.key === "Enter" && canSubmit) {
        e.preventDefault();
        submit();
      }
    }
    window.addEventListener("keydown", handleKey);
    return () => window.removeEventListener("keydown", handleKey);
  }, [canSubmit]); // eslint-disable-line react-hooks/exhaustive-deps

  return (
    <div
      onClick={onClose}
      className="fixed inset-0 bg-[rgba(26,26,26,0.35)] backdrop-blur-[2px] flex items-center justify-center z-[200]"
    >
      <div
        onClick={(e) => e.stopPropagation()}
        className="w-[520px] max-w-[92vw] max-h-[88vh] overflow-y-auto bg-[var(--bg-surface)] border border-[var(--border)] rounded-2xl shadow-[var(--shadow-xl)] flex flex-col"
      >
        <header className="flex items-start px-5 py-4 border-b border-[var(--border)] gap-3">
          <div>
            <div className="font-serif text-[18px]">
              Ship v{data.app.version_name} to{" "}
              {state.target === "play_store" ? "Play Store" : "App Store"}
            </div>
            <div className="text-[11px] text-[var(--ink-muted)] mt-0.5">
              Build {data.app.build_number} · {latestBuild ? `${platform.toUpperCase()} ready` : "no build"}
            </div>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="ml-auto w-7 h-7 rounded-md inline-flex items-center justify-center text-[var(--ink-muted)] hover:bg-[var(--bg-hover)]"
          >
            <X size={14} />
          </button>
        </header>

        <div className="flex-1 px-5 py-4 flex flex-col gap-4">
          <div className="flex flex-col gap-1">
            <div className="flex items-center justify-between">
              <span className="text-[10px] uppercase tracking-[0.5px] text-[var(--ink-faint)]">
                What&apos;s new in this update
              </span>
              <button
                type="button"
                onClick={draftNotes}
                disabled={draftingNotes}
                className="text-[10px] px-2 py-0.5 rounded-md bg-[var(--accent-soft)] text-[var(--accent)] hover:bg-[var(--accent)] hover:text-white disabled:opacity-50 inline-flex items-center gap-1"
              >
                {draftingNotes ? <Loader2 size={10} className="animate-spin" /> : <Sparkles size={10} />}
                {draftingNotes ? "Drafting…" : state.releaseNotes ? "Regenerate" : "Draft with AI"}
              </button>
            </div>
            <textarea
              value={state.releaseNotes}
              onChange={(e) => onPatch({ releaseNotes: e.target.value })}
              rows={4}
              className="bg-[var(--bg)] border border-[var(--border)] rounded-md px-3 py-2 text-[12px] leading-[1.55] resize-y font-mono"
              placeholder={"- Added offline support\n- Fixed profile photo upload"}
            />
          </div>

          <UnchangedSummary listing={data.listing} />

          <RolloutSelector
            target={state.target}
            value={state.rollout}
            onChange={(v) => onPatch({ rollout: v })}
          />
        </div>

        <footer className="flex items-center gap-2 px-5 py-3 border-t border-[var(--border)]">
          <button
            type="button"
            onClick={onClose}
            className="px-3 py-1.5 rounded-md text-[12px] border border-[var(--border-strong)]"
          >
            Cancel
          </button>
          <div className="flex-1" />
          <span className="text-[10px] text-[var(--ink-faint)] mono">⌘⏎ to ship</span>
          <button
            type="button"
            onClick={submit}
            disabled={!canSubmit}
            className="px-3.5 py-2 bg-[var(--accent)] text-white rounded-lg text-[12px] font-medium disabled:opacity-40"
          >
            {submitting ? "Submitting…" : `Ship v${data.app.version_name}`}
          </button>
        </footer>
      </div>
    </div>
  );
}
```

Adapt `createDeployment` signature if it differs. The existing deploy wizard's submit (in `components/ship/pilot/step-review.tsx`) is the reference.

Note: Quick Ship passes empty `title` / `description` / `icon_id` / `screenshot_paths` because these are UNCHANGED from the previous submission. Backend currently reads from `AppStoreListing` on update when these fields are empty. If backend requires them non-empty, adapt to send the values from `data.listing` or the AppStoreListing directly. Inspect `controllers/deployment.go` `CreateDeployment` behavior on empty strings first.

**Before finalizing**, verify by reading `internal/controllers/deployment.go` how `CreateDeployment` treats empty title / description / icon when there's an existing AppStoreListing for the project. If it rejects them as invalid, grab defaults from `data.listing.tiles` / `data.app.icon_s3_key`.

- [ ] **Step 2: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-app
npm run lint
npm run build
git add src/components/ship/quick-ship/quick-ship.tsx
git commit -m "feat(ship): add QuickShip one-screen submission sheet

Subsequent submissions skip the full 5-step Pilot and drop straight
to this sheet: AI-draftable release notes, unchanged-since-last-
release summary, binary + connection summary, rollout selector, big
'Ship vN' button with ⌘⏎ shortcut.

Reuses POST /v1/apps/:id/listing/draft (Phase 3b) for release notes
and POST /v1/deployments (existing) for submit. No new endpoints."
```

---

## Task 6: Route `onAction` based on `canQuickShip`

**Files:**
- Modify: `src/components/ship/launchpad.tsx`

- [ ] **Step 1: Update `Launchpad` to host both overlays and route per-tile**

```tsx
"use client";

import type { LaunchpadResponse, LaunchpadStoreTile } from "@/lib/ship/launchpad-types";
import { usePilot } from "@/hooks/use-pilot";
import { useQuickShip } from "@/hooks/use-quick-ship";
import { BuildsFeed } from "./builds-feed";
import { IdentityPanel } from "./identity-panel";
import { LaunchpadHeader } from "./launchpad-header";
import { ListingGrid } from "./listing-grid";
import { NewBuildButton } from "./new-build-button";
import { Pilot } from "./pilot/pilot";
import { QuickShip } from "./quick-ship/quick-ship";
import { canQuickShip } from "./quick-ship/quick-ship-state";
import { StoreTile } from "./store-tile";

interface LaunchpadProps {
  data: LaunchpadResponse;
  onRefetch: () => void;
}

export function Launchpad({ data, onRefetch }: LaunchpadProps) {
  const pilot = usePilot();
  const quickShip = useQuickShip();

  function openStoreAction(target: "play_store" | "app_store", tile: LaunchpadStoreTile) {
    if (canQuickShip(tile)) {
      quickShip.open(target, data);
    } else {
      pilot.open(target, data);
    }
  }

  return (
    <div className="flex-1 overflow-y-auto">
      <div className="px-8 py-6 pb-16 max-w-[880px] mx-auto">
        <LaunchpadHeader data={data} />

        <section className="mt-6">
          <div className="text-[10px] uppercase tracking-[1px] text-[var(--ink-faint)] mb-2">
            Stores
          </div>
          <div className="grid grid-cols-2 gap-2.5">
            <StoreTile
              kind="play_store"
              tile={data.stores.play_store}
              onAction={() => openStoreAction("play_store", data.stores.play_store)}
            />
            <StoreTile
              kind="app_store"
              tile={data.stores.app_store}
              onAction={() => openStoreAction("app_store", data.stores.app_store)}
            />
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

      {pilot.state && (
        <Pilot
          state={pilot.state}
          data={data}
          onPatch={pilot.patch}
          onGoto={pilot.goto}
          onClose={pilot.close}
          onSubmitted={onRefetch}
        />
      )}

      {quickShip.state && (
        <QuickShip
          state={quickShip.state}
          data={data}
          onPatch={quickShip.patch}
          onClose={quickShip.close}
          onSubmitted={onRefetch}
        />
      )}
    </div>
  );
}
```

- [ ] **Step 2: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-app
npm run lint
npm run build
git add src/components/ship/launchpad.tsx
git commit -m "feat(ship): route store tile action to Quick Ship or Pilot based on status

Live / update-available tiles open Quick Ship; any other status falls
back to the full 5-step Pilot. Both overlays coexist in Launchpad —
only one is open at a time because they're mutually exclusive state."
```

---

## Task 7: End-to-end smoke (documented for user)

- [ ] **Step 1: Documented**

```
# Backend
cd /Users/knid/Projects/wemob/wemob-backend
docker-compose -f docker-compose-dev.yaml up -d
go run cmd/main.go &

# Frontend
cd /Users/knid/Projects/wemob/wemob-app && npm run dev
# http://localhost:3000/apps/<appId>/ship
```

Test cases:
1. **First-time submission** (tile status = `not_configured` or `ready_to_ship`): click store action → full Pilot opens (unchanged from Phase 3a/3b).
2. **Subsequent submission** (tile status = `live` or `update_available`): click store action → Quick Ship sheet opens with release-notes textarea, unchanged summary, rollout selector.
3. Click "Draft with AI" on release notes → sees drafted bullet list populate.
4. Hit ⌘⏎ → submission fires; toast; Launchpad refetches.
5. Verify that the full Pilot path still works for first-timers by submitting a deployment for a never-shipped app.

---

## Phase 4a completion criteria

- Build + lint clean.
- `canQuickShip(tile)` returns `true` only for `live` / `update_available` tiles.
- Quick Ship opens instead of Pilot for those tiles.
- Release notes field supports "Draft with AI" and regenerate.
- Submit calls `createDeployment` and refetches.
- ⌘⏎ submits from anywhere in the sheet.
- No regressions in Pilot / Launchpad / Builds / Listing / Identity from prior phases.

Phase 4b (⌘K palette + `/ship` cross-app table + TestFlight wiring) unblocked.

## Out of scope (4b)

- ⌘K universal command palette + keyboard navigation.
- `/ship` cross-app table (replaces `/builds`).
- TestFlight actually wired through `fastlane pilot` — Phase 4a only surfaces the selector UI.
- Phase 5 (managed observability) work.
