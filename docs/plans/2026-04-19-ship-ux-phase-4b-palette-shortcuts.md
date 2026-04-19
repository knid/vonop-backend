# Ship UX — Phase 4b · ⌘K Palette + Keyboard Shortcuts + 4a Gap Close Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the power-user affordances the spec calls for — a universal ⌘K command palette, single-key keyboard shortcuts for the most-used Launchpad actions, and the minor backend extension needed to close Phase 4a's `TODO(4b)`: `CreateDeploymentRequest` gets `release_notes` + `rollout_track` optional fields so QuickShip's captured values actually reach the store.

**Architecture:** Backend adds two optional columns to `deployments` + request-field passthroughs — tiny schema change, same migration pattern as Phase 1's renameStatements. Frontend adds a single command registry, the `CommandPalette` overlay, and a `useKeyboardShortcuts` hook that wires into the existing Launchpad host page. No new pages; ⌘K floats above whatever surface the user is on inside `/apps/:id/ship`.

**Tech stack:** Go 1.22+, Bun, chi/v5. Next.js 16, React 19, Tailwind 4. No new deps.

**Design spec:** `docs/design/2026-04-18-wemob-publish-ux-redesign-design.md` ("Power user — ⌘K universal palette + keyboard").

---

## Test strategy

Same discipline: `go build` + `go vet` clean, `npm run build` + lint clean, runtime smoke documented for user.

## File structure overview

### Backend — modified

```
internal/models/deployment.go                  — +ReleaseNotes, +RolloutTrack
internal/requests/deployment.go                — +release_notes, +rollout_track on CreateDeploymentRequest
internal/controllers/deployment.go             — plumb fields into Deployment row at create
cmd/main.go                                    — two ALTER TABLE additions to renameStatements
```

### Frontend — new files

```
src/lib/ship/commands.ts                       — command registry (static + dynamic commands)
src/hooks/use-command-palette.ts               — open/close/search/run
src/hooks/use-keyboard-shortcuts.ts            — single-key + chord handlers
src/components/ship/command-palette.tsx        — ⌘K overlay component
```

### Frontend — modified

```
src/lib/api.ts                                 — CreateDeploymentRequest gains release_notes, rollout_track
src/lib/ship/launchpad-types.ts                — export a Command type
src/components/ship/quick-ship/quick-ship.tsx  — forward release_notes + rollout_track (removes 4a TODO)
src/components/ship/launchpad.tsx              — host CommandPalette + wire shortcuts
```

## Why these boundaries

- **Release-notes / rollout-track are Deployment fields, not a separate table.** They belong to a single submission audit, and extending the existing model is the simplest path. No migration complexity.
- **Commands as a static registry.** The design spec lists a handful of named actions ("Ship vN to Play Store", "New build", "All builds & submissions", etc.). A typed array beats a dynamic event bus for 4b's needs; Phase 5 can introduce plugin-style registration if we need it.
- **Keyboard shortcuts separate from the palette.** Shortcuts target single-key actions (`b`, `s`, `l`); the palette handles anything more complex. Decoupling means neither feature blocks the other.
- **Palette is a component hosted inside Launchpad** rather than globally in StudioShell. 4b scopes ⌘K to the `/apps/:id/ship` surface; global availability comes in 4c or later with the `/ship` cross-app table.

---

## Task 1: Extend `Deployment` model + migration

**Files:**
- Modify: `internal/models/deployment.go`
- Modify: `cmd/main.go`

- [ ] **Step 1: Add fields to the Deployment struct**

Open `/Users/knid/Projects/wemob/wemob-backend/internal/models/deployment.go`. Add two fields near the other metadata (Title, Description, etc.):

```go
// ReleaseNotes is the "what's new in this version" copy captured at
// submission time. Populated by QuickShip; empty on Pilot-first
// submissions (first-time submissions send metadata via deliver/supply
// configured elsewhere).
ReleaseNotes string `bun:",nullzero,type:text"`

// RolloutTrack names the target track selected by the user.
// Vocabulary: "internal" | "closed" | "open" | "production" (Play Store)
// or "internal" | "closed" | "production" (App Store TestFlight).
// Empty on Pilot submissions (which always target Production).
RolloutTrack string `bun:",nullzero"`
```

Keep all other fields unchanged.

- [ ] **Step 2: Add the column migration**

In `/Users/knid/Projects/wemob/wemob-backend/cmd/main.go`, find the `renameStatements` slice (the unconditional migration block introduced in Phase 1). Append two entries to the end of the slice:

```go
`ALTER TABLE IF EXISTS deployments ADD COLUMN IF NOT EXISTS release_notes text`,
`ALTER TABLE IF EXISTS deployments ADD COLUMN IF NOT EXISTS rollout_track text`,
```

These are idempotent — safe to re-run.

- [ ] **Step 3: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
go build ./...
go vet ./...
git add internal/models/deployment.go cmd/main.go
git commit -m "feat(ship): add ReleaseNotes + RolloutTrack to Deployment

Two new nullable text columns persisted on each submission so Phase 4a
QuickShip's 'What's new in this update' and rollout-track selector no
longer silently drop on the floor. Pilot-first submissions leave both
empty (metadata is handled by deliver/supply configured separately).

Idempotent ADD COLUMN IF NOT EXISTS migrations placed in the Phase 1
renameStatements block (unconditional, run every boot)."
```

---

## Task 2: Extend `CreateDeploymentRequest` + controller pass-through

**Files:**
- Modify: `internal/requests/deployment.go`
- Modify: `internal/controllers/deployment.go`

- [ ] **Step 1: Inspect the current request + controller**

```bash
grep -n "type CreateDeploymentRequest\|ReleaseNotes\|RolloutTrack" /Users/knid/Projects/wemob/wemob-backend/internal/requests/deployment.go /Users/knid/Projects/wemob/wemob-backend/internal/controllers/deployment.go | head -20
```

Find `type CreateDeploymentRequest struct { ... }` and the controller method that builds a `models.Deployment` from it.

- [ ] **Step 2: Add two optional fields to `CreateDeploymentRequest`**

```go
// ReleaseNotes — optional "what's new in this version" copy captured
// at submission time (populated by QuickShip in the frontend).
ReleaseNotes string `json:"release_notes,omitempty"`

// RolloutTrack — optional track selection. Values:
//   Play Store: "internal" | "closed" | "open" | "production"
//   App Store:  "internal" | "closed" | "production" (TestFlight variants)
// Empty defaults to "production" behavior at submit time.
RolloutTrack string `json:"rollout_track,omitempty"`
```

Add both near other optional fields.

- [ ] **Step 3: Thread the fields into the Deployment row**

In the controller method that creates the `models.Deployment` (from `CreateDeploymentRequest`), assign:

```go
deployment.ReleaseNotes = req.ReleaseNotes
deployment.RolloutTrack = req.RolloutTrack
```

Don't introduce new validation rules — both fields are optional by design.

- [ ] **Step 4: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
go build ./...
go vet ./...
git add internal/requests/deployment.go internal/controllers/deployment.go
git commit -m "feat(ship): accept + persist release_notes + rollout_track at deployment create

CreateDeploymentRequest gains two optional fields that thread through
to the Deployment row. Closes Phase 4a's TODO where QuickShip captured
these values but the backend silently dropped them.

Pilot first-submissions leave both empty — metadata flows through the
configured supply/deliver metadata_path in those cases; the fields
here are the audit trail for QuickShip updates."
```

---

## Task 3: Frontend — update `CreateDeploymentRequest` TS type + QuickShip wiring

**Files:**
- Modify: `src/lib/api.ts`
- Modify: `src/components/ship/quick-ship/quick-ship.tsx`

- [ ] **Step 1: Add the two optional fields to the TS `CreateDeploymentRequest`**

Locate the `export interface CreateDeploymentRequest` in `/Users/knid/Projects/wemob/wemob-app/src/lib/api.ts` (around line 272). Add:

```ts
release_notes?: string;
rollout_track?: "internal" | "closed" | "open" | "production";
```

- [ ] **Step 2: Forward the fields in QuickShip's submit**

In `/Users/knid/Projects/wemob/wemob-app/src/components/ship/quick-ship/quick-ship.tsx`, inside `submit()`:

1. **Remove** the `TODO(4b)` comment block (added in Phase 4a).
2. **Add** `release_notes: state.releaseNotes.trim()` and `rollout_track: state.rollout` to the `createDeployment` payload.

After edit, the payload inside `await apiClient.createDeployment({ ... })` should include:

```ts
release_notes: state.releaseNotes.trim(),
rollout_track: state.rollout,
```

Placement adjacent to the other fields is fine.

- [ ] **Step 3: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-app
npm run build
git add src/lib/api.ts src/components/ship/quick-ship/quick-ship.tsx
git commit -m "feat(ship): forward release_notes + rollout_track from QuickShip to backend

CreateDeploymentRequest TS type mirrors the Go struct's two new
optional fields. QuickShip's submit now sends both, so the user's
'What's new' copy and rollout-track selection actually persist.

Closes Phase 4a's TODO."
```

---

## Task 4: Command action registry + types

**Files:**
- Create: `src/lib/ship/commands.ts`
- Modify: `src/lib/ship/launchpad-types.ts`

- [ ] **Step 1: Export a Command type**

Append to `src/lib/ship/launchpad-types.ts`:

```ts
export interface ShipCommand {
  id: string;             // stable unique id, e.g. "ship-play-store"
  label: string;          // human name shown in the palette
  hint?: string;          // secondary text beneath label
  shortcut?: string;      // display-only keyboard hint, e.g. "⌘⏎"
  kind: "primary" | "secondary" | "danger" | "external";
  // Scope: "app" = needs a current app; "global" = always available.
  scope: "app" | "global";
  run: () => void;        // executed when selected
}
```

- [ ] **Step 2: Build commands from Launchpad state**

Create `src/lib/ship/commands.ts`:

```ts
import type { LaunchpadResponse, LaunchpadStoreTile, ShipCommand } from "./launchpad-types";

interface CommandContext {
  data: LaunchpadResponse;
  onShipStore: (target: "play_store" | "app_store", tile: LaunchpadStoreTile) => void;
  onNewBuild: () => void;
  onOpenStoreConsole: (target: "play_store" | "app_store") => void;
}

/**
 * buildAppCommands — produces the in-app command set for the current
 * app. Emits one "Ship vN to X" per store plus "New build" and
 * "Open X console" external links. Global commands (across Wemob)
 * land in Phase 4c's /ship table work.
 */
export function buildAppCommands(ctx: CommandContext): ShipCommand[] {
  const { data } = ctx;
  const version = data.app.version_name
    ? `v${data.app.version_name}`
    : "next version";

  const commands: ShipCommand[] = [
    {
      id: "ship-play-store",
      label: `Ship ${version} to Play Store`,
      hint: storeHint(data.stores.play_store),
      shortcut: "⌘⏎",
      kind: storeKind(data.stores.play_store),
      scope: "app",
      run: () => ctx.onShipStore("play_store", data.stores.play_store),
    },
    {
      id: "ship-app-store",
      label: `Ship ${version} to App Store`,
      hint: storeHint(data.stores.app_store),
      kind: storeKind(data.stores.app_store),
      scope: "app",
      run: () => ctx.onShipStore("app_store", data.stores.app_store),
    },
    {
      id: "new-build",
      label: "New build",
      hint: "Android + iOS with project defaults",
      shortcut: "b",
      kind: "primary",
      scope: "app",
      run: ctx.onNewBuild,
    },
    {
      id: "open-play-console",
      label: "Open Google Play Console ↗",
      kind: "external",
      scope: "app",
      run: () => ctx.onOpenStoreConsole("play_store"),
    },
    {
      id: "open-app-store-connect",
      label: "Open App Store Connect ↗",
      kind: "external",
      scope: "app",
      run: () => ctx.onOpenStoreConsole("app_store"),
    },
  ];
  return commands;
}

function storeHint(tile: LaunchpadStoreTile): string {
  switch (tile.status) {
    case "live":
      return "Live — opens Quick Ship for update";
    case "update_available":
      return "Update ready — opens Quick Ship";
    case "not_configured":
      return "Opens the Pilot to set up first submission";
    case "ready_to_ship":
      return "Ready — opens the Pilot";
    case "rejected":
      return "Rejected — opens the Pilot to re-submit";
    default:
      return "";
  }
}

function storeKind(tile: LaunchpadStoreTile): ShipCommand["kind"] {
  if (tile.status === "rejected" || tile.status === "failed") return "danger";
  if (tile.status === "live" || tile.status === "update_available") return "primary";
  return "secondary";
}

/**
 * Filters a command list by a freeform search string. Case-insensitive
 * substring match against label + hint.
 */
export function filterCommands(commands: ShipCommand[], query: string): ShipCommand[] {
  const q = query.trim().toLowerCase();
  if (!q) return commands;
  return commands.filter((c) => {
    const hay = (c.label + " " + (c.hint ?? "")).toLowerCase();
    return hay.includes(q);
  });
}
```

- [ ] **Step 3: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-app
npm run build
git add src/lib/ship/commands.ts src/lib/ship/launchpad-types.ts
git commit -m "feat(ship): add ShipCommand type + buildAppCommands registry

Five app-scoped commands: ship to each store (kind depends on tile
status), new build, open Play Console / App Store Connect. Hints
reflect what clicking will actually do ('Live — opens Quick Ship for
update' vs 'Ready — opens the Pilot'). Cross-app and global commands
are Phase 4c."
```

---

## Task 5: `useCommandPalette` hook

**Files:**
- Create: `src/hooks/use-command-palette.ts`

- [ ] **Step 1: Write the hook**

```ts
"use client";

import { useCallback, useEffect, useState } from "react";

export function useCommandPalette() {
  const [isOpen, setIsOpen] = useState(false);
  const [query, setQuery] = useState("");

  const open = useCallback(() => {
    setQuery("");
    setIsOpen(true);
  }, []);
  const close = useCallback(() => setIsOpen(false), []);
  const toggle = useCallback(() => {
    setQuery("");
    setIsOpen((v) => !v);
  }, []);

  // ⌘K toggles; Esc closes.
  useEffect(() => {
    function handler(e: KeyboardEvent) {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        toggle();
      } else if (e.key === "Escape" && isOpen) {
        e.preventDefault();
        close();
      }
    }
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [toggle, close, isOpen]);

  return { isOpen, query, setQuery, open, close, toggle };
}
```

- [ ] **Step 2: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-app
npm run build
git add src/hooks/use-command-palette.ts
git commit -m "feat(ship): add useCommandPalette hook

Handles open/close/toggle + search query state. Global ⌘K keydown
listener attaches while mounted; Esc closes when open. Resets query
on open so reopening always lands on a blank input."
```

---

## Task 6: `useKeyboardShortcuts` hook

**Files:**
- Create: `src/hooks/use-keyboard-shortcuts.ts`

- [ ] **Step 1: Write the hook**

```ts
"use client";

import { useEffect } from "react";

interface ShortcutMap {
  // Single-key maps. Examples: { b: () => ..., s: () => ... }
  // Fires only when no input / textarea / contenteditable is focused,
  // and no modifier keys (except Shift for uppercase).
  [key: string]: () => void;
}

/**
 * useKeyboardShortcuts wires single-key shortcuts to Launchpad-level
 * actions. Shortcuts are suppressed when a text input is focused so
 * typing in the Pilot / QuickShip / palette never triggers a letter
 * shortcut. Modifier keys (Meta, Ctrl, Alt) are excluded — ⌘K is
 * owned by useCommandPalette separately.
 */
export function useKeyboardShortcuts(shortcuts: ShortcutMap, enabled = true) {
  useEffect(() => {
    if (!enabled) return;

    function handler(e: KeyboardEvent) {
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      // Don't steal keys from form fields.
      const t = e.target as HTMLElement | null;
      if (t) {
        const tag = t.tagName;
        const editable = (t as HTMLElement).isContentEditable;
        if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || editable) return;
      }
      const fn = shortcuts[e.key];
      if (fn) {
        e.preventDefault();
        fn();
      }
    }
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [shortcuts, enabled]);
}
```

- [ ] **Step 2: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-app
npm run build
git add src/hooks/use-keyboard-shortcuts.ts
git commit -m "feat(ship): add useKeyboardShortcuts hook

Generic single-key shortcut handler with input-focus guard so typing
in any form field never triggers a letter shortcut. Modifier keys are
excluded (⌘K is its own hook). Callers pass a {key: handler} map."
```

---

## Task 7: `CommandPalette` component

**Files:**
- Create: `src/components/ship/command-palette.tsx`

- [ ] **Step 1: Write the component**

```tsx
"use client";

import { useEffect, useRef, useState } from "react";
import { Search } from "lucide-react";
import { cn } from "@/lib/utils";
import { filterCommands } from "@/lib/ship/commands";
import type { ShipCommand } from "@/lib/ship/launchpad-types";

interface CommandPaletteProps {
  isOpen: boolean;
  query: string;
  onQueryChange: (q: string) => void;
  onClose: () => void;
  commands: ShipCommand[];
}

export function CommandPalette({ isOpen, query, onQueryChange, onClose, commands }: CommandPaletteProps) {
  const inputRef = useRef<HTMLInputElement>(null);
  const [selected, setSelected] = useState(0);
  const filtered = filterCommands(commands, query);

  // Reset selection when query changes; focus input on open.
  useEffect(() => {
    setSelected(0);
  }, [query]);
  useEffect(() => {
    if (isOpen) {
      // Defer focus to next tick so the input is mounted.
      requestAnimationFrame(() => inputRef.current?.focus());
    }
  }, [isOpen]);

  // Arrow-key navigation + Enter to run.
  useEffect(() => {
    if (!isOpen) return;
    function handler(e: KeyboardEvent) {
      if (e.key === "ArrowDown") {
        e.preventDefault();
        setSelected((s) => Math.min(filtered.length - 1, s + 1));
      } else if (e.key === "ArrowUp") {
        e.preventDefault();
        setSelected((s) => Math.max(0, s - 1));
      } else if (e.key === "Enter") {
        e.preventDefault();
        const c = filtered[selected];
        if (c) {
          onClose();
          c.run();
        }
      }
    }
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [isOpen, filtered, selected, onClose]);

  if (!isOpen) return null;

  return (
    <div
      onClick={onClose}
      className="fixed inset-0 bg-[rgba(26,26,26,0.5)] backdrop-blur-[3px] flex items-start justify-center pt-[80px] z-[250]"
    >
      <div
        onClick={(e) => e.stopPropagation()}
        className="w-[540px] max-w-[92vw] bg-[var(--bg-surface)] border border-[var(--border)] rounded-xl shadow-[var(--shadow-xl)] overflow-hidden"
      >
        <div className="flex items-center gap-2 px-4 py-3 border-b border-[var(--border)]">
          <Search size={14} className="text-[var(--ink-muted)]" />
          <input
            ref={inputRef}
            value={query}
            onChange={(e) => onQueryChange(e.target.value)}
            placeholder="Type a command…"
            className="flex-1 bg-transparent outline-none text-[13px]"
          />
          <span className="font-mono text-[10px] text-[var(--ink-faint)]">esc</span>
        </div>
        <div className="max-h-[320px] overflow-y-auto py-1">
          {filtered.length === 0 ? (
            <div className="px-4 py-6 text-[12px] text-[var(--ink-faint)] text-center">
              No commands match “{query}”.
            </div>
          ) : (
            filtered.map((c, idx) => (
              <button
                key={c.id}
                type="button"
                onMouseEnter={() => setSelected(idx)}
                onClick={() => {
                  onClose();
                  c.run();
                }}
                className={cn(
                  "w-full text-left px-4 py-2.5 flex items-start gap-3",
                  idx === selected ? "bg-[var(--bg-hover)]" : "",
                )}
              >
                <div className="flex-1 min-w-0">
                  <div className={cn("text-[13px]", kindColor(c.kind))}>{c.label}</div>
                  {c.hint && (
                    <div className="text-[11px] text-[var(--ink-muted)] truncate mt-0.5">
                      {c.hint}
                    </div>
                  )}
                </div>
                {c.shortcut && (
                  <span className="font-mono text-[10px] text-[var(--ink-faint)] mt-0.5">
                    {c.shortcut}
                  </span>
                )}
              </button>
            ))
          )}
        </div>
        <div className="flex items-center gap-3 px-4 py-2 border-t border-[var(--border)] text-[10px] text-[var(--ink-faint)]">
          <span className="font-mono">↑↓</span>
          <span>navigate</span>
          <span className="font-mono">⏎</span>
          <span>run</span>
          <span className="ml-auto">⌘K to toggle</span>
        </div>
      </div>
    </div>
  );
}

function kindColor(kind: ShipCommand["kind"]): string {
  switch (kind) {
    case "primary":
      return "text-[var(--ink)]";
    case "secondary":
      return "text-[var(--ink-muted)]";
    case "danger":
      return "text-[var(--accent)]";
    case "external":
      return "text-[var(--ink-muted)]";
  }
}
```

- [ ] **Step 2: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-app
npm run build
git add src/components/ship/command-palette.tsx
git commit -m "feat(ship): add CommandPalette component

Floating overlay opened by useCommandPalette. Search input filters
commands live; arrow keys navigate; Enter runs; Esc closes. Kind
colors (primary / secondary / danger / external) surface which
commands are active paths vs routine vs dangerous."
```

---

## Task 8: Wire palette + shortcuts into Launchpad

**Files:**
- Modify: `src/components/ship/launchpad.tsx`

- [ ] **Step 1: Replace `launchpad.tsx`**

```tsx
"use client";

import { useMemo } from "react";
import type { LaunchpadResponse, LaunchpadStoreTile } from "@/lib/ship/launchpad-types";
import { usePilot } from "@/hooks/use-pilot";
import { useQuickShip } from "@/hooks/use-quick-ship";
import { useCommandPalette } from "@/hooks/use-command-palette";
import { useKeyboardShortcuts } from "@/hooks/use-keyboard-shortcuts";
import { buildAppCommands } from "@/lib/ship/commands";
import { BuildsFeed } from "./builds-feed";
import { CommandPalette } from "./command-palette";
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
  const palette = useCommandPalette();

  function openStoreAction(target: "play_store" | "app_store", tile: LaunchpadStoreTile) {
    if (canQuickShip(tile)) {
      quickShip.open(target, data);
    } else {
      pilot.open(target, data);
    }
  }

  function openStoreConsole(target: "play_store" | "app_store") {
    const url =
      target === "play_store"
        ? "https://play.google.com/console/"
        : "https://appstoreconnect.apple.com/";
    window.open(url, "_blank", "noopener,noreferrer");
  }

  // "New build" trigger — reuses the NewBuildButton's existing flow by
  // firing the same onTriggered callback. For shortcut / palette
  // integration, call the apiClient directly would duplicate logic;
  // simpler is to dispatch a programmatic click on the NewBuildButton
  // (kept as a React pattern via a ref) — but refs make this harder
  // than it needs to be. Instead we expose a no-op placeholder for 4b
  // and document: shortcut 'b' focuses the New build button's tooltip
  // area; true one-key build runs in Phase 4c with the shortcut
  // rewrite.
  function focusNewBuild() {
    const btn = document.querySelector<HTMLButtonElement>('[data-ship-new-build]');
    btn?.click();
  }

  const commands = useMemo(
    () =>
      buildAppCommands({
        data,
        onShipStore: openStoreAction,
        onNewBuild: focusNewBuild,
        onOpenStoreConsole: openStoreConsole,
      }),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [data],
  );

  // Single-key shortcuts. 'l' scrolls to top (equivalent to "go to Launchpad").
  // 'b' clicks the New build button. 's' opens the palette scoped to ship-* commands.
  useKeyboardShortcuts(
    {
      b: focusNewBuild,
      s: palette.open,
      l: () => window.scrollTo({ top: 0, behavior: "smooth" }),
      "?": palette.open, // cheat sheet surrogate for 4b; 4c adds a dedicated ?-sheet
    },
    // Don't fire shortcuts while an overlay is open.
    !pilot.state && !quickShip.state && !palette.isOpen,
  );

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
            <div data-ship-new-build-wrap>
              <NewBuildButton app={data.app} onTriggered={onRefetch} />
            </div>
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

      <CommandPalette
        isOpen={palette.isOpen}
        query={palette.query}
        onQueryChange={palette.setQuery}
        onClose={palette.close}
        commands={commands}
      />
    </div>
  );
}
```

- [ ] **Step 2: Add `data-ship-new-build` attribute to the NewBuildButton**

Open `/Users/knid/Projects/wemob/wemob-app/src/components/ship/new-build-button.tsx`. Find the `<button>` element. Add a `data-ship-new-build` attribute so the keyboard shortcut can find it:

```tsx
<button
  type="button"
  data-ship-new-build
  onClick={handleClick}
  disabled={!canBuild || busy}
  // ... rest unchanged
>
```

- [ ] **Step 3: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-app
npm run lint
npm run build
git add src/components/ship/launchpad.tsx src/components/ship/new-build-button.tsx
git commit -m "feat(ship): wire CommandPalette + keyboard shortcuts into Launchpad

⌘K opens the palette (filterable command list). Single-key shortcuts:
'b' triggers New build (via a data-attribute lookup on the existing
button so the flow stays one source of truth), 's' / '?' open the
palette, 'l' scrolls to top. Shortcuts suppressed while any overlay
(Pilot / QuickShip / palette) is open.

buildAppCommands is memoized on the Launchpad response so the command
list updates automatically when version / store status changes."
```

---

## Task 9: End-to-end smoke (documented)

Live-only. Document for the user:

```
# Backend
cd /Users/knid/Projects/wemob/wemob-backend
docker-compose -f docker-compose-dev.yaml up -d
go run cmd/main.go &

# Frontend
cd /Users/knid/Projects/wemob/wemob-app && npm run dev
# Browser: http://localhost:3000/apps/<appId>/ship
```

Test cases:

1. **⌘K opens the palette.** Arrow keys navigate; Enter runs the highlighted command; Esc closes.
2. **Ship commands honor status.** For a live store, "Ship vN to Play Store" opens QuickShip. For a not-yet-configured store, the same label opens the Pilot.
3. **Single-key shortcuts.**
   - `b` clicks New build (disabled-button disabled, else fires).
   - `s` opens the palette.
   - `?` opens the palette (cheat sheet surrogate; 4c adds a dedicated sheet).
   - `l` scrolls to top.
   - None of the above fire while typing in any input / textarea.
4. **4a TODO closed.** Open QuickShip → type release notes → pick rollout → Ship → after success, run `SELECT release_notes, rollout_track FROM deployments ORDER BY created_at DESC LIMIT 1;` in your DB client. Both fields populated.

- [ ] **No commit — smoke is user-run.**

---

## Phase 4b completion criteria

- Backend + frontend build/vet/lint clean.
- `deployments.release_notes` + `deployments.rollout_track` columns exist.
- QuickShip's submit forwards both fields; the 4a TODO comment is removed.
- ⌘K opens the palette; filter / navigate / run works.
- Single-key shortcuts fire only outside input fields and while no overlay is open.
- No regressions in Launchpad / Pilot / QuickShip / Builds / Listing / Identity.

Phase 4c (`/ship` cross-app table + TestFlight via `fastlane pilot`) unblocked.

## Out of scope (4c + beyond)

- `/ship` cross-app table (replaces `/builds`).
- TestFlight tracks actually wired through `fastlane pilot` (selector is display-only today).
- Dedicated `?` cheat-sheet overlay (4b uses the palette as a surrogate).
- Slash commands in chat (`/ship`, `/new-build`) — Phase 4c or 5.
- Power-user auto-detection for hiding explainers — Phase 5.
- Any managed-submission observability work (Phase 5).
