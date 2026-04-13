# Chat Event System Fix — Phase 1 (Stop the Bleeding)

> **Context:** This plan lands the 9 showstopper fixes from the chat event system audit (`docs/plans/2026-04-13-chat-audit-findings.md` — in conversation). No architectural change, no protocol change, no schema change. Each fix is independently verifiable and each commit leaves the system working. Phases 2-4 (protocol unification, storage unification, frontend reducer rewrite) are separate plans.

**Goal:** Materially improve chat reliability by eliminating the 7 bugs that make it feel "broken" today, without touching the event vocabulary or storage model.

**Architecture:** Surgical bug fixes. No new abstractions.

**Tech Stack:** Go (chi, bun, pgdriver, Postgres, SSE), Next.js 15, React 19, TypeScript, Streamdown.

**Verification strategy:** This repo has no test suite (`CLAUDE.md`: "No _test.go files"). Each task verifies via:
1. Compile check (`go build ./...` or `tsc --noEmit` / `next build`)
2. `go vet ./...` for Go
3. Manual smoke test of the affected flow in browser after Phase 1 merges

Commits are per-task so rollback is surgical if any one fix regresses.

---

## File Map

Backend (`/Users/knid/Projects/wemob-backend`):
- `internal/views/generation_stream_state.go` — rename agent error/rollback cases
- `internal/views/generation_jobs_view.go` — DB replay fallback in StreamJob; async flushStreamState
- `internal/agent/events.go` — delete dead `sseEmitter`

Frontend (`/Users/knid/Projects/wemob-dashboard-v2`):
- `src/components/studio/chat-timeline/markdown-text.tsx` — lock down Streamdown
- `src/hooks/use-app-chat.ts` — reject concurrent send, abort on cleanup, reset autoReconnect, pass onActivity
- `src/lib/api.ts` — plumb onActivity through chatApp / reconnectAgentRun / streamAgentRun

---

## Task 1 — Fix stream_state reducer typo (C2)

**Why:** `streamStateBuffer.Apply` checks for `"agent_error"`/`"agent_rollback"`, but the agent emits `"error"`/`"rollback"`. On failure, the partial buffer never clears and replays ghost text on every reconnect forever.

**Files:**
- Modify: `internal/views/generation_stream_state.go:299-307`

**Steps:**
- [ ] Edit the switch case labels from `"agent_error", "agent_rollback"` to `"error", "rollback"`.
- [ ] `cd /Users/knid/Projects/wemob-backend && go build ./... && go vet ./...`
- [ ] Commit: `fix(chat): stream_state buffer matches agent error/rollback event names`

---

## Task 2 — StreamJob DB replay fallback when bus is gone (C1)

**Why:** `StreamJob` 404s when the in-process `EventBus` is gone, even though the DB has the complete event history. If the job finishes between `/active-job` and `/stream`, the UI is stuck forever.

**Files:**
- Modify: `internal/views/generation_jobs_view.go:382-425`

**Steps:**
- [ ] Rework the handler so that when `JobEventBuses.Load(jobID)` returns not-ok:
  - If `job.Status IN ("completed", "failed", "cancelled")` → set SSE headers, replay `job.StreamState` + `job.Events`, synthesize a terminal `done`/`error` frame if missing, and close.
  - Otherwise (status still `running` / `awaiting_input`) → keep current 404 response (legitimately orphaned job).
- [ ] Extract the replay block (stream_state emit + milestone loop) into a helper `replayPersistedJobEvents(w, flusher, job)` so both branches share the code.
- [ ] `go build ./... && go vet ./...`
- [ ] Commit: `fix(chat): StreamJob replays persisted events when job already finished`

---

## Task 3 — Delete dead `sseEmitter` and ring buffer (C9)

**Why:** `sseEmitter` in `internal/agent/events.go` is unused — the real adapter is `agentSSEAdapter` in `views/agent_bridge.go`. `sseEmitter.Close()` calls `close(e.ch)` on a channel it doesn't own, which is a latent double-close panic waiting for anyone who wires it up.

**Files:**
- Modify: `internal/agent/events.go` (delete lines 148-205: `sseEmitter` struct, `NewSSEEmitter`, `Emit`, `Buffer`, `Close`)
- Remove unused import `"sync"` and `"time"` and `"github.com/google/uuid"` if no longer referenced after deletion (verify with `go build`).

**Steps:**
- [ ] Remove the `sseEmitter` block and the `NewSSEEmitter` constructor.
- [ ] Run `go build ./...`; adjust imports if needed.
- [ ] `go vet ./...`
- [ ] Commit: `refactor(agent): delete unused sseEmitter ring buffer`

---

## Task 4 — Async `flushStreamState` with single-flight (H12)

**Why:** `flushStreamState` runs synchronously on the event loop goroutine. A 2s DB hiccup freezes SSE emission for 2s, fills the channel buffer, and triggers `agent_bridge.Emit`'s 10s timeout path → silent event drops.

**Files:**
- Modify: `internal/views/generation_jobs_view.go:80-145, 193-250` (both `handleStreamOrAsync` and `drainEventsToJob`)

**Steps:**
- [ ] Replace the inline `flushStreamState := func() {...}` closure with a struct-scoped helper that owns a single-flight flag (`sync.Mutex` + `inFlight bool`) and a goroutine pool of 1.
- [ ] When `flushStreamState()` is called and nothing is in flight, mark in-flight and `go func() { defer unmark; Snapshot+UpdateGenerationJobStreamState }`.
- [ ] When called while already in-flight, set a `pendingDirty` flag; the running goroutine re-checks dirtiness on completion and re-runs if needed.
- [ ] At `done:`/`finalize:`, wait for any in-flight flush to complete via a `WaitGroup`, then do the final sync `UpdateGenerationJobStreamState({})`.
- [ ] `go build ./... && go vet ./...`
- [ ] Commit: `perf(chat): flush stream_state async so DB stalls don't block SSE`

---

## Task 5 — Lock down Streamdown sanitization (H29)

**Why:** `<Streamdown>` is rendered with no explicit `allowedLinkPrefixes`/`allowedImagePrefixes`. LLM-controlled tool output (web_search results, etc.) flows through this component and is a plausible XSS vector.

**Files:**
- Modify: `/Users/knid/Projects/wemob-dashboard-v2/src/components/studio/chat-timeline/markdown-text.tsx:31-37`

**Steps:**
- [ ] Add props: `allowedLinkPrefixes={["http://", "https://", "mailto:"]}`, `allowedImagePrefixes={["http://", "https://", "data:image/"]}`, `defaultOrigin="https://wemob.app"`.
- [ ] (Verify via `cd /Users/knid/Projects/wemob-dashboard-v2 && npx tsc --noEmit`.)
- [ ] Commit: `fix(chat): lock down Streamdown link/image allow-list`

---

## Task 6 — Reject concurrent sendMessage while streaming (C6)

**Why:** Double-clicking Send (or triggering "Fix with AI" mid-stream) currently overwrites the abort controller, resets refs, and lets the first stream's callbacks write into the second turn's state. Produces visibly corrupted chat.

**Files:**
- Modify: `/Users/knid/Projects/wemob-dashboard-v2/src/hooks/use-app-chat.ts:666-731`

**Steps:**
- [ ] At the top of `sendMessage`, after `if (!appId || !message.trim()) return`, add an early return:
  ```ts
  if (isStreaming || sendInFlightRef.current) {
    toast({
      title: t("apps.chat.alreadyStreaming"),
      description: t("apps.chat.alreadyStreamingDesc"),
    })
    return
  }
  ```
- [ ] Add the two i18n keys to `src/lib/i18n/locales/en/apps.json` and `src/lib/i18n/locales/tr/apps.json` (or the equivalent file — grep `alreadyStreaming`-style existing keys).
- [ ] Verify `npx tsc --noEmit`.
- [ ] Commit: `fix(chat): reject concurrent sendMessage while streaming`

---

## Task 7 — Abort SSE on appId change and unmount (C8)

**Why:** The `appId` effect cleanup only flips a `cancelled` flag; it never aborts the in-flight SSE fetch. Previous stream's callbacks keep firing against the new appId's state. StrictMode doubles this.

**Files:**
- Modify: `/Users/knid/Projects/wemob-dashboard-v2/src/hooks/use-app-chat.ts:628-664, 758-762`

**Steps:**
- [ ] In the `appId` effect cleanup, call `abortControllerRef.current?.abort(); abortControllerRef.current = null` before the `cancelled = true`.
- [ ] In the unmount effect (currently only clears idle timer), also call `abortControllerRef.current?.abort()`.
- [ ] Verify `npx tsc --noEmit`.
- [ ] Commit: `fix(chat): abort in-flight SSE on appId change and unmount`

---

## Task 8 — Reset `autoReconnectAttemptedRef` on skipped reconnect (C7)

**Why:** If `reconnectToChatJob` is re-entered while `isReconnectingRef` is set, it early-returns without resetting `autoReconnectAttemptedRef`. Combined with the idle timer never firing a second attempt, the chat becomes permanently dead until page refresh.

**Files:**
- Modify: `/Users/knid/Projects/wemob-dashboard-v2/src/hooks/use-app-chat.ts:497-502`

**Steps:**
- [ ] Change the early guard from
  ```ts
  if (isReconnectingRef.current) return
  ```
  to
  ```ts
  if (isReconnectingRef.current) {
    // Another reconnect is already in flight; let the idle watchdog try again
    // later if THIS call came from an auto-reconnect that would otherwise
    // latch the "tried once" flag.
    autoReconnectAttemptedRef.current = false
    return
  }
  ```
- [ ] Verify `npx tsc --noEmit`.
- [ ] Commit: `fix(chat): unlatch auto-reconnect when a reconnect is already running`

---

## Task 9 — Wire `onActivity` through SSE parser to reset idle timer on real activity (Issue ⑤)

**Why:** The 60s idle timer is only reset by `onKeepalive` events. During long LLM thinking pauses the server streams narration deltas but not keepalives, so the timer fires and yanks the client into a "disconnected" reconnect — while the real stream is still producing data. `handleSSEStream` already has an `onActivity` parameter; `startSSE` already forwards it; nothing in the chat path actually passes one.

**Files:**
- Modify: `/Users/knid/Projects/wemob-dashboard-v2/src/lib/api.ts:2071-2135` (`streamAgentRun`, `reconnectAgentRun`, `chatApp`, `generateAppStream`)
- Modify: `/Users/knid/Projects/wemob-dashboard-v2/src/hooks/use-app-chat.ts:558, 699`

**Steps:**
- [ ] Add optional `onActivity?: () => void` parameter to `streamAgentRun`, `chatApp`, `generateAppStream`, `reconnectAgentRun` in `api.ts`. Pass it to `startSSE` as the 6th positional arg.
- [ ] In `use-app-chat.ts` `sendMessage`, pass `resetIdleTimer` as an additional `onActivity` arg on the `apiClient.chatApp(...)` call.
- [ ] Same in `reconnectToChatJob` for `apiClient.reconnectAgentRun(...)`.
- [ ] Keep the `onKeepalive: () => resetIdleTimerRef.current()` handler — it's harmless and still correct for servers that emit keepalives.
- [ ] Verify `npx tsc --noEmit`.
- [ ] Commit: `fix(chat): reset idle watchdog on actual SSE activity, not just keepalive events`

---

## Task 10 — Phase 1 smoke test + final commit

**Steps:**
- [ ] `cd /Users/knid/Projects/wemob-backend && go build ./... && go vet ./...` → expect clean.
- [ ] `cd /Users/knid/Projects/wemob-dashboard-v2 && npx tsc --noEmit` → expect clean. (Or `npm run build` if tsc alone skips project references.)
- [ ] Manual smoke test checklist (user-run):
  1. Send a chat message → assistant streams normally → terminal event arrives → UI settles.
  2. Refresh mid-stream → `/active-job` hits → stream resumes without duplicated text.
  3. Send a chat, close the tab, re-open the tab after it finishes → StreamJob replays from DB, user sees final state.
  4. Double-click Send twice quickly → second click is rejected with a toast.
  5. Switch between two apps in the studio mid-stream → stream aborts cleanly, no cross-poisoning.
  6. Kill network for 70s during a long thinking pause → reconnect pill shows; recovers without wiping the timeline.
  7. Paste a prompt with `<img src=x onerror=alert(1)>` → renders as text, not HTML.
- [ ] After smoke test passes, either: create a PR for Phase 1, or move straight on to Phase 2 plan.

---

## Out of scope for Phase 1 (intentionally)

These are Phase 2-4 concerns — don't touch them in this batch:
- Event protocol unification / monotonic seq / Last-Event-ID
- Schema changes (`parts` streaming write, `status` column, FKs with cascade)
- `generation_jobs` → `app_agent_runs` collapse
- `use-app-chat.ts` reducer rewrite
- `chat-panel.tsx` split
- UX polish (empty state, jump-to-latest, tool result summaries, buy-credits wiring, /clear confirm)
- Deleting `generation_jobs.events` / `stream_state` columns
- Deleting `chat-timeline.ts` reducer

These are all tracked in the audit and will land in follow-up phases.
