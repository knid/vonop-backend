# Chat Event System Fix — Phase 2 (Protocol Foundations)

> Phase 1 (`docs/plans/2026-04-13-chat-event-system-phase-1.md`) shipped the surgical bug fixes. Phase 2 introduces the protocol foundations that the rest of the audit needs: monotonic per-event seq, `Last-Event-ID` end-to-end, an explicit terminal frame, and the cheapest cleanups (delete dead `tool_call_delta` + typed `keepalive`, strip `InputPreview` content). This phase intentionally does NOT collapse the event vocabulary or change storage — those are Phase 2.5 and Phase 3.

**Goal:** Make reconnect deterministic and idempotent, kill the worst event duplication, and prepare the wire protocol for the vocabulary collapse that follows.

**Architecture:**
- A single per-job monotonic `seq` is assigned in `handleStreamOrAsync`'s main goroutine (it owns ordering) and stamped into both `responses.SSEEvent` and `models.JobEvent`.
- `WriteSSEEvent` emits an `id: <seq>` line so browsers and our custom parser both honor `Last-Event-ID`.
- `StreamJob` reads `Last-Event-ID` (header or query param fallback) and skips persisted events with `seq <= header`. Live tail dedupes the same way.
- Frontend SSE parser tracks `lastEventId` per stream and forwards it on reconnect.
- After the terminal `done`/`error` event, the server sends `event: end\ndata: {}\n\n` and closes the connection. Belt-and-braces signal so clients can finalise UI state without inferring it from event names.
- Dead vocabulary (`tool_call_delta`, typed `keepalive` event) is deleted in both repos.
- `WriteFile`/`EditFile` tools no longer ship the full file body in `ToolCallStartEvent.InputPreview` — that field is now a small descriptor (`{path, op}`).

**Tech Stack:** Same as Phase 1.

**Verification strategy:** `go build ./... && go vet ./...`, `tsc --noEmit`, manual smoke test of the reconnect-after-finish flow and the long-thinking-pause flow.

**Backwards compat:** Every change is additive on the wire. Old clients ignore unknown SSE fields (`seq`, `id:` lines, `end` event). New clients tolerate servers that don't emit them. The two repos can deploy independently.

---

## File Map

Backend:
- `internal/responses/sse_events.go` — add `Seq` field to `SSEEvent`
- `internal/utils/sse.go` — `WriteSSEEvent` accepts seq, emits `id:` line
- `internal/models/generation_job.go` — rename `JobEvent.Index` → `JobEvent.Seq`
- `internal/views/generation_jobs_view.go` — assign seq, honor `Last-Event-ID`, terminal frame, dedupe
- `internal/views/generation_stream_state.go` — apply doesn't need to know seq for this phase
- `internal/agent/events.go` — delete `ToolCallDeltaEvent`, `EventToolCallDelta`, `KeepaliveEvent`, `EventKeepalive`
- `internal/agent/run.go` — stop emitting `keepalive` event (the SSE comment heartbeat in the view layer already covers it)
- `internal/agent/tools_base.go` — strip `InputPreview` content for write_file/edit_file (emit a small descriptor only)

Frontend:
- `src/lib/api.ts` — `handleSSEStream` parses `id:` lines, exposes `onEventId` callback; `reconnectAgentRun` and friends accept + send `Last-Event-ID` header
- `src/lib/app-builder-types.ts` — drop `AgentToolCallDeltaEvent` and `AgentKeepaliveEvent` from public types (or mark deprecated)
- `src/hooks/use-app-chat.ts` — track `lastEventIdRef`, forward to API client; `onKeepalive` becomes optional (kept as no-op for now)
- (no UI component changes in Phase 2)

---

## Tasks

### Task 1 — Add `Seq` to SSEEvent + JobEvent
- **Why:** Foundation for everything else. Stamps every event with a monotonic id.
- **Files:** `internal/responses/sse_events.go`, `internal/models/generation_job.go`
- **Steps:**
  1. Add `Seq int64 \`json:"seq,omitempty"\`` to `responses.SSEEvent`.
  2. Rename `JobEvent.Index` → `JobEvent.Seq` (`int64`, `json:"seq"`). Verify only `views/generation_jobs_view.go` writes it.
  3. `go build ./...`
- **Commit:** `feat(chat): add monotonic seq field to SSEEvent and JobEvent`

### Task 2 — Assign seq in handleStreamOrAsync + drainEventsToJob
- **Why:** Single goroutine ownership of seq guarantees monotonicity. Persist BEFORE publish so DB-snapshot-based reconnect is consistent with live tail.
- **Files:** `internal/views/generation_jobs_view.go`
- **Steps:**
  1. Add `var nextSeq int64 = 0` in both functions.
  2. On every `case event, ok := <-events`, set `nextSeq++` and `event.Seq = nextSeq` BEFORE persistence and publish.
  3. Replace `Index: eventIndex` with `Seq: nextSeq` in `JobEvent` literals; delete the now-unused `eventIndex` counter.
  4. `go build ./... && go vet ./...`
- **Commit:** `feat(chat): assign monotonic seq on every emitted event`

### Task 3 — `WriteSSEEvent` emits `id:` line
- **Why:** Browsers and our custom parser both honor `Last-Event-ID` only when the server emits `id:` lines.
- **Files:** `internal/utils/sse.go`, `internal/views/generation_jobs_view.go`
- **Steps:**
  1. Change `WriteSSEEvent` signature to `(w, flusher, seq int64, eventName string, data any) error`.
  2. Emit `id: <seq>\n` BEFORE `event:` when `seq > 0`.
  3. Update all 6 call sites in `generation_jobs_view.go`. Pass `0` for the bootstrap `job_started` frame, `event.Seq` for live events, `0` for the `stream_state` snapshot, and the persisted `Seq` for milestone replay.
  4. The replay loop's anonymous unmarshal struct gains `Seq int64 \`json:"seq"\``.
  5. `go build ./... && go vet ./...`
- **Commit:** `feat(chat): emit SSE id: line so reconnect can resume by seq`

### Task 4 — `StreamJob` honors `Last-Event-ID`
- **Why:** Reconnects no longer duplicate milestone events.
- **Files:** `internal/views/generation_jobs_view.go`
- **Steps:**
  1. At the top of `StreamJob`, read `Last-Event-ID` header (fallback to `?since=N` query param). Parse to `int64`. Default `0`.
  2. In `replayPersistedJobEvents`, accept the threshold and skip events with `Seq <= threshold`.
  3. While tailing the live bus, track `maxReplayedSeq = max(threshold, max(replayed event seq))`. Drop live events with `seq <= maxReplayedSeq` (covers the race between DB snapshot and bus subscribe).
  4. `go build ./... && go vet ./...`
- **Commit:** `feat(chat): StreamJob honors Last-Event-ID for idempotent reconnect`

### Task 5 — Frontend SSE parser tracks `lastEventId`
- **Why:** Foundation for sending `Last-Event-ID` on reconnect.
- **Files:** `src/lib/api.ts`
- **Steps:**
  1. In `handleSSEStream`, parse `id:` lines into a buffer. On each dispatched event, also call an optional `onEventId(seq: string)` callback with the latest id.
  2. `startSSE` accepts and forwards `onEventId`.
  3. `streamAgentRun`/`reconnectAgentRun`/`chatApp`/`generateAppStream` accept and forward `onEventId`.
  4. `reconnectAgentRun` also accepts an optional `lastEventId` and sets it as a `Last-Event-ID` request header.
  5. `tsc --noEmit`
- **Commit:** `feat(chat): client-side SSE parser tracks and forwards id:/Last-Event-ID`

### Task 6 — Hook tracks lastEventId + sends on reconnect
- **Why:** Closes the loop. Reconnect resumes idempotently.
- **Files:** `src/hooks/use-app-chat.ts`
- **Steps:**
  1. Add `lastEventIdRef = useRef<string | null>(null)`.
  2. In `buildAgentRunCallbacks`, no change — the id flows through `onEventId`.
  3. Pass `(seq) => { lastEventIdRef.current = seq }` as `onEventId` to `chatApp`/`reconnectAgentRun`.
  4. Pass `lastEventIdRef.current ?? undefined` as the new `lastEventId` arg to `reconnectAgentRun`.
  5. Reset `lastEventIdRef.current = null` at the start of `sendMessage` (new run starts at seq 0).
  6. `tsc --noEmit`
- **Commit:** `feat(chat): hook persists lastEventId and resumes from it on reconnect`

### Task 7 — Strip `ToolCallStartEvent.InputPreview` content for file tools
- **Why:** Today every `write_file`/`edit_file` ships full content THREE times (input_preview → file_written.content → persisted milestone). The InputPreview duplication is the easiest to remove and gives the biggest bandwidth win without breaking the FE optimistic-write path (which reads `file_written.content`).
- **Files:** `internal/agent/tools_base.go`, `internal/agent/run.go`
- **Steps:**
  1. Find the call site in `agent/run.go` that constructs `ToolCallStartEvent` and check whether it's there or in `tools_base.go`.
  2. For tools `write_file` / `edit_file` / `delete_file`, replace `InputPreview` with a stripped JSON: `{"path": "..."}` (drop `content`, `old_string`, `new_string`).
  3. `go build ./...`
- **Commit:** `perf(chat): strip file content from ToolCallStartEvent.InputPreview`

### Task 8 — Explicit terminal SSE frame
- **Why:** Belt-and-braces. Clients no longer have to infer "stream is over" from event names.
- **Files:** `internal/views/generation_jobs_view.go`, `src/lib/api.ts`
- **Steps:**
  1. In `handleStreamOrAsync` after `done:`, before the FinalizeJob/cancel/Close block, send `WriteSSEEvent(w, flusher, 0, "end", map[string]any{})` if not `clientGone`. Do the same in `StreamJob`'s tail loop on bus close.
  2. In `replayPersistedJobEvents` for terminal jobs, also write the `end` frame after the milestone replay.
  3. Frontend: in the SSE dispatch table, add `end: () => {}` so the unknown-event warning doesn't fire. (The browser closes the stream naturally.)
  4. `go build && tsc --noEmit`
- **Commit:** `feat(chat): emit explicit terminal SSE frame after done/error`

### Task 9 — Delete dead `tool_call_delta` + typed `keepalive`
- **Why:** Both events are dead weight. `tool_call_delta` is never emitted; `keepalive` duplicates the SSE comment heartbeat.
- **Files:**
  - Backend: `internal/agent/events.go`, `internal/agent/run.go`
  - Frontend: `src/lib/api.ts` (dispatch table), `src/lib/app-builder-types.ts`, `src/hooks/use-app-chat.ts` (`onKeepalive`)
- **Steps:**
  1. Delete `ToolCallDeltaEvent`, `EventToolCallDelta`, `KeepaliveEvent`, `EventKeepalive` from `internal/agent/events.go`.
  2. Delete the `agent.Run` keepalive ticker goroutine that emits the typed event (find via grep). The view-layer `:keepalive\n\n` SSE comment ticker stays.
  3. Frontend: drop `tool_call_delta` and `keepalive` from `buildAgentDispatch`. Drop matching types from `app-builder-types.ts`. Make `onKeepalive` and `onToolCallDelta` callbacks optional (already are) and the hook's `onKeepalive` becomes a no-op since `onActivity` (Phase 1) drives the watchdog.
  4. `go build && tsc --noEmit`
- **Commit:** `refactor(chat): delete dead tool_call_delta and typed keepalive events`

### Task 10 — Phase 2 verification
- **Steps:**
  1. `cd /Users/knid/Projects/wemob-backend && go build ./... && go vet ./...`
  2. `cd /Users/knid/Projects/wemob-dashboard-v2 && npx tsc --noEmit`
  3. Manual smoke test additions (on top of Phase 1):
     - Reconnect: Browser dev tools → Network → throttle to "Offline" for 5s mid-stream → reconnect should NOT replay narration text or duplicate tool cards.
     - File events: Make a generation that writes 5 files. Confirm SSE size is materially smaller (no full file content in `tool_call_start`).
     - Multi-tab: Open the same app in two tabs. Both should rehydrate consistently with no duplicates.
     - Terminal: Confirm `event: end` frame appears in dev tools after `done` / `error`.

---

## Out of scope for Phase 2 (intentionally)

- **Vocabulary collapse** (delete legacy `chat_*`/`file_operation`/`file_complete`, drop `turn_start`/`turn_end`, rename `done` → `run_complete`) — that's Phase 2.5.
- **Lazy file fetch endpoint** (replace `FileWrittenEvent.Content` with `GET /v2/apps/{id}/files/{path}?at=<commit>`) — Phase 3, paired with the storage rewrite.
- **Schema changes** (status column, FK cascades, collapse `generation_jobs` → `app_agent_runs`) — Phase 3.
- **Frontend reducer rewrite** — Phase 4.
- **UX polish** — Phase 4.

These tracks unblock Phase 3/4 but are sized larger and benefit from landing the seq foundation first.
