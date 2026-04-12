# AI Codegen Agent Rewrite Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the existing ~23k-LOC `StreamBuildApp` AI codegen system with a single agentic loop under `internal/agent/` backed by the native Anthropic tool API, ~3k LOC of new code, and a paired dashboard PR.

**Architecture:** One entry function `agent.Run(ctx, deps, project, user, message, images, emitter) error` unifies initial generation and chat edit. Model writes via native Anthropic `tool_use` blocks (`read_file`, `write_file`, `edit_file`, `delete_file`, `list_files`, `web_search`, `generate_image`, plus conditional `execute_sql` / `configure_mobile_bridge`). One base prompt + per-variant addenda. File tree in system prompt, contents fetched on demand. In-memory `SafeFileMap` only touches git at pre-run and post-run commits. Rollback = discard the in-memory map + defensive `git reset --hard`. Errors bubble back through the chat UI from the E2B sandbox.

**Tech Stack:** Go 1.22+, Bun ORM, PostgreSQL, Anthropic Messages API (via existing `utils/anthropic.go` adapter), chi/v5, `services/gitrepo` wrapper around shell `git`, `services/neon`, existing `StreamWithTools` in `utils/ai.go`.

**Testing note:** Per `CLAUDE.md`, this project has no Go test suite and adding one is out of scope. Every step verifies with `go build ./...` and `go vet ./...`. The final phase runs a 12-scenario manual verification checklist against staging before merge. Do NOT add `_test.go` files during this plan — it's a deliberate project convention.

**Reference spec:** `docs/design/2026-04-13-ai-codegen-agent-rewrite.md`

**Commit style:** The repo uses Conventional Commits (per recent log: `feat(ai-builder):`, `fix(chat):`, `fix(seed-images):`). Mirror it.

---

## Phase C1 — New models and schema

Adds `AppChatMessage` and `AppAgentRun` Bun models, DB accessors, and `createSchema()` entries. Nothing in the old system is touched.

### Task C1.1: Create `AppChatMessage` Bun model

**Files:**
- Create: `internal/models/app_chat_message.go`

- [ ] **Step 1: Create the file**

```go
package models

import (
	"time"

	"github.com/google/uuid"
)

// AppChatMessage is one turn of the user↔agent conversation for an app project.
// A "user" message is what the user typed (plus any images). An "assistant"
// message is the final narration and tool call trace from one agent run.
type AppChatMessage struct {
	ID         uuid.UUID    `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	ProjectID  uuid.UUID    `bun:",notnull,type:uuid"`
	Project    *AppProject  `bun:"rel:belongs-to,join:project_id=id"`
	RunID      *uuid.UUID   `bun:",type:uuid,nullzero"` // null for user messages not yet tied to a run
	Role       string       `bun:",notnull"`            // "user" | "assistant"
	Content    string       `bun:",type:text,notnull,default:''"`
	Images     []byte       `bun:",type:jsonb,notnull,default:'[]'"`     // [{url, alt?}]
	ToolCalls  []byte       `bun:",type:jsonb,notnull,default:'[]'"`     // []ChatMessageToolCall
	CreatedAt  time.Time    `bun:"created_at,nullzero,notnull,default:current_timestamp"`
}

// ChatMessageToolCall is the JSONB shape embedded in AppChatMessage.ToolCalls.
// Stored as a snapshot of what the agent did — no foreign key to any tool table.
type ChatMessageToolCall struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Input      json.RawMessage `json:"input"`
	Result     string          `json:"result"`
	IsError    bool            `json:"is_error"`
	DurationMs int             `json:"duration_ms"`
}
```

Then add the missing import at the top so the file compiles:

```go
import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)
```

- [ ] **Step 2: Verify it builds**

Run: `go build ./internal/models/...`
Expected: no output, exit 0.

- [ ] **Step 3: Commit**

```bash
git add internal/models/app_chat_message.go
git commit -m "feat(agent): add AppChatMessage model for new chat schema"
```

---

### Task C1.2: Create `AppAgentRun` Bun model

**Files:**
- Create: `internal/models/app_agent_run.go`

- [ ] **Step 1: Create the file**

```go
package models

import (
	"time"

	"github.com/google/uuid"
)

// Agent run statuses.
const (
	AgentRunStatusRunning        = "running"
	AgentRunStatusCompleted      = "completed"
	AgentRunStatusFailed         = "failed"
	AgentRunStatusCanceled       = "canceled"
	AgentRunStatusBudgetExceeded = "budget_exceeded"
)

// AppAgentRun is the execution record of one agent invocation: tokens, cost,
// commit hashes, status, duration. One row per user turn that triggers the
// agent. Referenced by the assistant AppChatMessage via RunID.
type AppAgentRun struct {
	ID               uuid.UUID   `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	ProjectID        uuid.UUID   `bun:",notnull,type:uuid"`
	Project          *AppProject `bun:"rel:belongs-to,join:project_id=id"`
	UserID           uuid.UUID   `bun:",notnull,type:uuid"`
	User             *User       `bun:"rel:belongs-to,join:user_id=id"`
	Status           string      `bun:",notnull"`
	PreCommit        string      `bun:",notnull"`
	FinalCommit      string      `bun:",default:''"`
	UserMessageID    *uuid.UUID  `bun:",type:uuid,nullzero"`
	Model            string      `bun:",notnull"`
	Provider         string      `bun:",notnull"`
	Turns            int         `bun:",notnull,default:0"`
	ToolCallCount    int         `bun:",notnull,default:0"`
	PromptTokens     int         `bun:",notnull,default:0"`
	CompletionTokens int         `bun:",notnull,default:0"`
	CacheReadTokens  int         `bun:",notnull,default:0"`
	CacheWriteTokens int         `bun:",notnull,default:0"`
	CostUSD          float64     `bun:",notnull,default:0"`
	CreditsUsed      int         `bun:",notnull,default:0"`
	DurationMs       int         `bun:",notnull,default:0"`
	Error            string      `bun:",type:text,default:''"`
	CreatedAt        time.Time   `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	CompletedAt      time.Time   `bun:",nullzero"`
}
```

- [ ] **Step 2: Verify build**

Run: `go build ./internal/models/...`
Expected: exit 0.

- [ ] **Step 3: Commit**

```bash
git add internal/models/app_agent_run.go
git commit -m "feat(agent): add AppAgentRun model for run telemetry"
```

---

### Task C1.3: Register new models in `createSchema()`

**Files:**
- Modify: `cmd/main.go` — find the `createSchema()` function, add two `IfNotExists()` calls for the new models

- [ ] **Step 1: Locate `createSchema`**

Run: `grep -n "func createSchema\|NewCreateTable" cmd/main.go | head -30`

Expected: shows the `createSchema` function and a list of `NewCreateTable(...).Model((*models.AppX)(nil)).IfNotExists().Exec(...)` statements.

- [ ] **Step 2: Add the two new models**

Add these two calls right after the existing `models.AppConversation` registration inside `createSchema` (keep alphabetical order if the rest of the file uses it; otherwise append after `AppConversation`):

```go
if _, err := db.DB.NewCreateTable().Model((*models.AppChatMessage)(nil)).IfNotExists().Exec(ctx); err != nil {
	return fmt.Errorf("create app_chat_messages: %w", err)
}
if _, err := db.DB.NewCreateTable().Model((*models.AppAgentRun)(nil)).IfNotExists().Exec(ctx); err != nil {
	return fmt.Errorf("create app_agent_runs: %w", err)
}
```

- [ ] **Step 3: Verify build and start-up schema creation path compiles**

Run: `go build ./cmd/...`
Expected: exit 0.

- [ ] **Step 4: Commit**

```bash
git add cmd/main.go
git commit -m "feat(agent): register AppChatMessage and AppAgentRun in createSchema"
```

---

### Task C1.4: Add DB accessors for `AppChatMessage`

**Files:**
- Create: `internal/database/app_chat_messages.go`

- [ ] **Step 1: Create the accessor file**

```go
package database

import (
	"context"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
)

// InsertChatMessage persists a new chat message (user or assistant).
func (db *Database) InsertChatMessage(m *models.AppChatMessage) (*models.AppChatMessage, error) {
	_, err := db.DB.NewInsert().Model(m).Returning("*").Exec(context.Background())
	return m, err
}

// UpdateChatMessage updates mutable fields on an existing chat message — used
// when the assistant turn is finalised at the end of a run (RunID, content,
// tool calls all become known at shutdown).
func (db *Database) UpdateChatMessage(m *models.AppChatMessage) error {
	_, err := db.DB.NewUpdate().
		Model(m).
		Set("run_id = ?", m.RunID).
		Set("content = ?", m.Content).
		Set("tool_calls = ?", m.ToolCalls).
		Where("id = ?", m.ID).
		Exec(context.Background())
	return err
}

// GetRecentChatMessages returns the N most recent messages for a project,
// oldest-first. Used to hydrate the LLM message window.
func (db *Database) GetRecentChatMessages(projectID uuid.UUID, limit int) ([]models.AppChatMessage, error) {
	var msgs []models.AppChatMessage
	err := db.DB.NewSelect().
		Model(&msgs).
		Where("project_id = ?", projectID).
		Order("created_at DESC").
		Limit(limit).
		Scan(context.Background())
	if err != nil {
		return nil, err
	}
	// Reverse in place so the slice is oldest-first.
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	return msgs, nil
}

// ClearChatMessages deletes all messages for a project. Called by the
// "clear conversation" endpoint.
func (db *Database) ClearChatMessages(projectID uuid.UUID) error {
	_, err := db.DB.NewDelete().
		Model((*models.AppChatMessage)(nil)).
		Where("project_id = ?", projectID).
		Exec(context.Background())
	return err
}
```

- [ ] **Step 2: Verify build**

Run: `go build ./internal/database/...`
Expected: exit 0.

- [ ] **Step 3: Commit**

```bash
git add internal/database/app_chat_messages.go
git commit -m "feat(agent): add AppChatMessage DB accessors"
```

---

### Task C1.5: Add DB accessors for `AppAgentRun`

**Files:**
- Create: `internal/database/app_agent_runs.go`

- [ ] **Step 1: Create the accessor file**

```go
package database

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
)

// InsertAgentRun persists a new agent run in `running` status.
func (db *Database) InsertAgentRun(r *models.AppAgentRun) (*models.AppAgentRun, error) {
	_, err := db.DB.NewInsert().Model(r).Returning("*").Exec(context.Background())
	return r, err
}

// UpdateAgentRun writes terminal state to an existing run row.
func (db *Database) UpdateAgentRun(r *models.AppAgentRun) error {
	_, err := db.DB.NewUpdate().
		Model(r).
		Where("id = ?", r.ID).
		Exec(context.Background())
	return err
}

// GetActiveAgentRun returns the currently running run for a project, or
// (nil, nil) if none. Used to enforce one-run-per-project and to let the
// dashboard reconnect after a refresh.
func (db *Database) GetActiveAgentRun(projectID uuid.UUID) (*models.AppAgentRun, error) {
	var r models.AppAgentRun
	err := db.DB.NewSelect().
		Model(&r).
		Where("project_id = ?", projectID).
		Where("status = ?", models.AgentRunStatusRunning).
		Order("created_at DESC").
		Limit(1).
		Scan(context.Background())
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// GetAgentRun returns a run by ID regardless of status.
func (db *Database) GetAgentRun(runID uuid.UUID) (*models.AppAgentRun, error) {
	var r models.AppAgentRun
	err := db.DB.NewSelect().Model(&r).Where("id = ?", runID).Scan(context.Background())
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// MarkOrphanedRunsFailed is called on server startup: any `running` rows left
// over from a previous process are marked `failed`. We don't resume them.
func (db *Database) MarkOrphanedRunsFailed() (int, error) {
	res, err := db.DB.NewUpdate().
		Model((*models.AppAgentRun)(nil)).
		Set("status = ?", models.AgentRunStatusFailed).
		Set("error = ?", "server restarted").
		Set("completed_at = ?", time.Now()).
		Where("status = ?", models.AgentRunStatusRunning).
		Exec(context.Background())
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
```

- [ ] **Step 2: Verify build**

Run: `go build ./internal/database/...`
Expected: exit 0.

- [ ] **Step 3: Commit**

```bash
git add internal/database/app_agent_runs.go
git commit -m "feat(agent): add AppAgentRun DB accessors"
```

---

### Task C1.6: Wire `MarkOrphanedRunsFailed` into `main()` startup

**Files:**
- Modify: `cmd/main.go` — after `createSchema()` succeeds, before HTTP listener starts

- [ ] **Step 1: Locate the post-schema block**

Run: `grep -n "createSchema\|Listening on\|http.ListenAndServe" cmd/main.go`

- [ ] **Step 2: Add the cleanup call**

Insert right after the `createSchema(...)` call completes:

```go
if n, err := db.MarkOrphanedRunsFailed(); err != nil {
	slog.Warn("failed to mark orphaned agent runs", "error", err)
} else if n > 0 {
	slog.Info("marked orphaned agent runs as failed", "count", n)
}
```

- [ ] **Step 3: Verify build**

Run: `go build ./...`
Expected: exit 0.

- [ ] **Step 4: Commit**

```bash
git add cmd/main.go
git commit -m "feat(agent): mark orphaned agent runs failed on startup"
```

---

## Phase C1 complete — checkpoint

After this phase: `go build ./...` and `go vet ./...` pass. The two new tables exist in `createSchema()` but nothing uses them yet. Safe to merge alone if desired.

---

## Phase C2 — `internal/agent/` package (build the new system, unused)

All tasks in this phase create new files under `internal/agent/`. Nothing outside the package changes until Phase C3. After every task, `go build ./...` must pass.

### Task C2.1: Package skeleton — errors, SafeFileMap, RunContext

**Files:**
- Create: `internal/agent/errors.go`
- Create: `internal/agent/run_context.go`

- [ ] **Step 1: Create `errors.go`**

```go
package agent

import "fmt"

// ErrorCode classifies why a run stopped.
type ErrorCode string

const (
	CodeCreditsExhausted ErrorCode = "CREDITS_EXHAUSTED"
	CodeRateLimited      ErrorCode = "RATE_LIMITED"
	CodeProviderError    ErrorCode = "PROVIDER_ERROR"
	CodeBudgetExceeded   ErrorCode = "BUDGET_EXCEEDED"
	CodeToolFailed       ErrorCode = "TOOL_FAILED"
	CodeGitError         ErrorCode = "GIT_ERROR"
	CodeRunInProgress    ErrorCode = "RUN_IN_PROGRESS"
	CodeInternal         ErrorCode = "INTERNAL"
)

// Error is a typed agent error that carries a phase label and an error code.
// Returned from Run() so callers can map to HTTP status codes and SSE event
// payloads without string-sniffing.
type Error struct {
	Code    ErrorCode
	Phase   string // "setup" | "agent_loop" | "tool" | "commit"
	Message string
	Wrapped error
}

func (e *Error) Error() string {
	if e.Wrapped != nil {
		return fmt.Sprintf("%s: %s: %s: %v", e.Phase, e.Code, e.Message, e.Wrapped)
	}
	return fmt.Sprintf("%s: %s: %s", e.Phase, e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.Wrapped }

// New builds a typed agent error.
func New(code ErrorCode, phase, msg string, wrapped error) *Error {
	return &Error{Code: code, Phase: phase, Message: msg, Wrapped: wrapped}
}
```

- [ ] **Step 2: Create `run_context.go`**

```go
package agent

import (
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/utils"
)

// SafeFileMap is a mutex-protected map[path]content used as the single source
// of truth for the project's files during a run. Tool executors mutate it in
// memory; only the success-path shutdown writes it back to the git working
// tree and commits.
type SafeFileMap struct {
	mu    sync.RWMutex
	files map[string]string
}

func NewSafeFileMap(seed map[string]string) *SafeFileMap {
	m := &SafeFileMap{files: make(map[string]string, len(seed))}
	for k, v := range seed {
		m.files[k] = v
	}
	return m
}

func (m *SafeFileMap) Get(path string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.files[path]
	return v, ok
}

func (m *SafeFileMap) Set(path, content string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[path] = content
}

func (m *SafeFileMap) Delete(path string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.files[path]; !ok {
		return false
	}
	delete(m.files, path)
	return true
}

func (m *SafeFileMap) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.files)
}

// Snapshot returns a shallow copy for read-only use (string values are
// immutable in Go so a shallow copy is safe for rollback callers).
func (m *SafeFileMap) Snapshot() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]string, len(m.files))
	for k, v := range m.files {
		out[k] = v
	}
	return out
}

// Match returns paths matching the glob. Uses `filepath.Match` semantics —
// add `doublestar` later if `**` support becomes necessary.
func (m *SafeFileMap) Match(glob string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if glob == "" {
		paths := make([]string, 0, len(m.files))
		for p := range m.files {
			paths = append(paths, p)
		}
		return paths
	}
	var out []string
	for p := range m.files {
		ok, err := filepath.Match(glob, p)
		if err == nil && ok {
			out = append(out, p)
		}
		// Support simple `foo/**/*.tsx` by stripping the `**/` and matching the tail.
		if strings.Contains(glob, "**/") {
			tail := strings.Split(glob, "**/")[1]
			if ok2, _ := filepath.Match(tail, filepath.Base(p)); ok2 {
				out = append(out, p)
			}
		}
	}
	return out
}

// RunContext is threaded through every tool executor and holds the ambient
// state for a single agent run. It is constructed at the top of Run() and
// never mutated outside the orchestrator.
type RunContext struct {
	RunID        uuid.UUID
	Project      *models.AppProject
	User         *models.User
	FileMap      *SafeFileMap
	Emitter      EventEmitter
	Tracker      *utils.TokenTracker
	Deps         *RunDependencies
}
```

- [ ] **Step 3: Verify build**

Run: `go build ./internal/agent/...`
Expected: the package does not exist yet, so errors about missing types `EventEmitter` and `RunDependencies`. That's expected — we'll add them in the next tasks. Run instead:

```bash
go vet ./internal/agent/... 2>&1 | grep -v "EventEmitter\|RunDependencies" || true
```

We only guarantee a clean build after Task C2.4.

- [ ] **Step 4: Commit**

```bash
git add internal/agent/errors.go internal/agent/run_context.go
git commit -m "feat(agent): package skeleton — errors, SafeFileMap, RunContext"
```

---

### Task C2.2: Event types and emitter interface

**Files:**
- Create: `internal/agent/events.go`

- [ ] **Step 1: Create `events.go`**

```go
package agent

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Event is the SSE envelope the emitter produces. Callers don't construct
// Event directly — they pass the typed payload and Emitter.Emit wraps it.
type Event struct {
	Type string
	Data any
}

// EventEmitter delivers events to the SSE client and maintains a short
// replay buffer so a disconnecting client can catch up via the
// /runs/{runId}/stream reconnect endpoint.
type EventEmitter interface {
	Emit(eventType string, data any) bool
	Buffer() []Event
	Close()
}

// ---- Event payload types ----

type RunStartEvent struct {
	RunID     uuid.UUID `json:"run_id"`
	PreCommit string    `json:"pre_commit"`
	Model     string    `json:"model"`
	Provider  string    `json:"provider"`
	MaxTurns  int       `json:"max_turns"`
	Tools     []string  `json:"tools"`
}

type TurnStartEvent struct {
	RunID uuid.UUID `json:"run_id"`
	Turn  int       `json:"turn"`
}

type NarrationEvent struct {
	RunID uuid.UUID `json:"run_id"`
	Turn  int       `json:"turn"`
	Delta string    `json:"delta"`
}

type ToolCallStartEvent struct {
	RunID        uuid.UUID       `json:"run_id"`
	Turn         int             `json:"turn"`
	ToolCallID   string          `json:"tool_call_id"`
	ToolName     string          `json:"tool_name"`
	InputPreview json.RawMessage `json:"input_preview,omitempty"`
}

type ToolCallDeltaEvent struct {
	RunID      uuid.UUID `json:"run_id"`
	ToolCallID string    `json:"tool_call_id"`
	Field      string    `json:"field"`
	Delta      string    `json:"delta"`
}

type ToolCallResultEvent struct {
	RunID      uuid.UUID `json:"run_id"`
	ToolCallID string    `json:"tool_call_id"`
	ToolName   string    `json:"tool_name"`
	Status     string    `json:"status"`
	DurationMs int       `json:"duration_ms"`
	Summary    string    `json:"summary"`
	IsError    bool      `json:"is_error"`
}

type FileWrittenEvent struct {
	RunID     uuid.UUID `json:"run_id"`
	Path      string    `json:"path"`
	Operation string    `json:"operation"` // "create" | "update"
	Bytes     int       `json:"bytes"`
	Content   string    `json:"content"`
}

type FileDeletedEvent struct {
	RunID uuid.UUID `json:"run_id"`
	Path  string    `json:"path"`
}

type TurnEndEvent struct {
	RunID      uuid.UUID     `json:"run_id"`
	Turn       int           `json:"turn"`
	StopReason string        `json:"stop_reason"`
	Tokens     TokenUsage    `json:"tokens"`
}

type TokenUsage struct {
	Input      int `json:"input"`
	Output     int `json:"output"`
	CacheRead  int `json:"cache_read"`
	CacheWrite int `json:"cache_write"`
}

type RollbackEvent struct {
	RunID          uuid.UUID `json:"run_id"`
	PreCommit      string    `json:"pre_commit"`
	Reason         string    `json:"reason"`
	FilesReverted  int       `json:"files_reverted"`
}

type ErrorEvent struct {
	RunID   uuid.UUID `json:"run_id"`
	Phase   string    `json:"phase"`
	Message string    `json:"message"`
	Code    ErrorCode `json:"code"`
}

type DoneEvent struct {
	RunID        uuid.UUID  `json:"run_id"`
	FinalCommit  string     `json:"final_commit"`
	Turns        int        `json:"turns"`
	ToolCalls    int        `json:"tool_calls"`
	FilesChanged int        `json:"files_changed"`
	Tokens       TokenUsage `json:"tokens"`
	CreditsUsed  int        `json:"credits_used"`
	DurationMs   int        `json:"duration_ms"`
}

type KeepaliveEvent struct {
	RunID uuid.UUID `json:"run_id"`
	TS    int64     `json:"ts"`
}

// Event type constants — single source of truth for event names.
const (
	EventRunStart       = "run_start"
	EventTurnStart      = "turn_start"
	EventNarration      = "narration"
	EventToolCallStart  = "tool_call_start"
	EventToolCallDelta  = "tool_call_delta"
	EventToolCallResult = "tool_call_result"
	EventFileWritten    = "file_written"
	EventFileDeleted    = "file_deleted"
	EventTurnEnd        = "turn_end"
	EventRollback       = "rollback"
	EventError          = "error"
	EventDone           = "done"
	EventKeepalive      = "keepalive"
)

// ---- SSE-backed emitter with ring buffer ----

type sseEmitter struct {
	runID    uuid.UUID
	ch       chan<- Event
	mu       sync.Mutex
	closed   bool
	ring     []Event
	ringCap  int
}

// NewSSEEmitter wraps a send-only channel into an EventEmitter. It also
// maintains a bounded replay buffer (default 500 events) for reconnects.
func NewSSEEmitter(runID uuid.UUID, ch chan<- Event, ringCap int) EventEmitter {
	if ringCap <= 0 {
		ringCap = 500
	}
	return &sseEmitter{runID: runID, ch: ch, ringCap: ringCap}
}

func (e *sseEmitter) Emit(eventType string, data any) bool {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return false
	}
	ev := Event{Type: eventType, Data: data}
	// Append to ring buffer with cap eviction.
	if len(e.ring) >= e.ringCap {
		e.ring = e.ring[1:]
	}
	e.ring = append(e.ring, ev)
	e.mu.Unlock()

	select {
	case e.ch <- ev:
		return true
	case <-time.After(10 * time.Second):
		return false
	}
}

func (e *sseEmitter) Buffer() []Event {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]Event, len(e.ring))
	copy(out, e.ring)
	return out
}

func (e *sseEmitter) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.closed {
		e.closed = true
		close(e.ch)
	}
}
```

- [ ] **Step 2: Verify build**

Run: `go build ./internal/agent/...`

The package still doesn't compile because `RunDependencies` is referenced from `RunContext` but not yet defined. That's fine — we'll close the loop in Task C2.4. Move on.

- [ ] **Step 3: Commit**

```bash
git add internal/agent/events.go
git commit -m "feat(agent): SSE event schema and ring-buffered emitter"
```

---

### Task C2.3: `RunDependencies` struct

**Files:**
- Create: `internal/agent/dependencies.go`

- [ ] **Step 1: Create the file**

```go
package agent

import (
	"context"

	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/services/gitrepo"
	"github.com/knid/wemob/internal/services/neon"
	"github.com/knid/wemob/internal/services/remoteconfig"
	"github.com/knid/wemob/internal/services/secret"
	"github.com/knid/wemob/internal/services/storage"
	"github.com/knid/wemob/internal/utils"
	"github.com/posthog/posthog-go"
)

// CreditChecker is a function that returns true while the user still has
// credit budget for the current run. The orchestrator calls it between turns.
type CreditChecker func() bool

// CreditDeductor is called on successful run completion with the total credit
// cost. Never called on failure (fixes a known bug in the old system).
type CreditDeductor func(ctx context.Context, credits int, action, projectID string) error

// EventTracker posts a PostHog event. Matches the shape of the existing
// Controller.TrackUserEvent helper.
type EventTracker func(event string, props map[string]any)

// RunDependencies holds the external services the agent package needs.
// Constructed from *Controller once per request via Controller.AgentDeps().
type RunDependencies struct {
	DB            *database.Database
	GitRepo       *gitrepo.Service
	AssetsStorage *storage.S3Storage
	SecretManager secret.SecretManager
	NeonService   *neon.Service
	LLMProvider   *utils.LLMProvider
	PostHog       posthog.Client
	RemoteConfig  *remoteconfig.Service

	// Credit lifecycle (set per-run by the caller).
	CreditChecker  CreditChecker
	DeductCredits  CreditDeductor
	TrackUserEvent EventTracker
}
```

- [ ] **Step 2: Verify build**

Run: `go build ./internal/agent/...`
Expected: errors go away except for references to types we haven't defined yet (Registry, tool types). We're closer. Move on.

- [ ] **Step 3: Commit**

```bash
git add internal/agent/dependencies.go
git commit -m "feat(agent): RunDependencies struct for service wiring"
```

---

### Task C2.4: Tool executor interface and registry

**Files:**
- Create: `internal/agent/registry.go`

- [ ] **Step 1: Create the file**

```go
package agent

import (
	"context"
	"encoding/json"
	"fmt"
)

// ToolExecutor implements one agent-callable tool.
//
// Schema() must return a JSON-schema object acceptable to the Anthropic tools
// API. Execute() receives the raw input bytes from the model, validates/parses
// them, performs the operation, and returns a human-readable result string
// plus an is_error flag. Returning err != nil is reserved for infrastructure
// errors (DB down, etc.) — tool-usage errors (invalid path, not found) should
// return isError=true with a descriptive result instead.
type ToolExecutor interface {
	Name() string
	Description() string
	Schema() map[string]any
	Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (result string, isError bool, err error)
}

// Registry holds the active tool set for one run.
type Registry struct {
	order []string
	tools map[string]ToolExecutor
}

func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]ToolExecutor)}
}

func (r *Registry) Register(t ToolExecutor) {
	if _, exists := r.tools[t.Name()]; exists {
		return
	}
	r.order = append(r.order, t.Name())
	r.tools[t.Name()] = t
}

// Names returns tool names in registration order. Used by RunStartEvent.Tools.
func (r *Registry) Names() []string {
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

// AnthropicSchemas returns schemas for ExecUTor tools only (not server tools —
// those are opaque to us, the provider handles them). Shape matches the
// native Anthropic tools API: { name, description, input_schema }.
func (r *Registry) AnthropicSchemas() []map[string]any {
	out := make([]map[string]any, 0, len(r.tools))
	for _, name := range r.order {
		t, ok := r.tools[name]
		if !ok {
			continue // server tool — skip
		}
		out = append(out, map[string]any{
			"name":         t.Name(),
			"description":  t.Description(),
			"input_schema": t.Schema(),
		})
	}
	return out
}

// Execute dispatches to the named tool. Unknown names return isError=true.
func (r *Registry) Execute(ctx context.Context, rc *RunContext, name string, input json.RawMessage) (string, bool, error) {
	t, ok := r.tools[name]
	if !ok {
		return fmt.Sprintf("unknown tool: %s", name), true, nil
	}
	return t.Execute(ctx, rc, input)
}
```

- [ ] **Step 2: Verify build**

Run: `go build ./internal/agent/...`
Expected: the package builds now — all types referenced by `run_context.go`, `events.go`, and `dependencies.go` exist.

- [ ] **Step 3: Commit**

```bash
git add internal/agent/registry.go
git commit -m "feat(agent): ToolExecutor interface and Registry"
```

---

### Task C2.5: Base tools — read_file, list_files, delete_file

**Files:**
- Create: `internal/agent/tools_base.go`

These three are the simplest: pure reads/mutations against `SafeFileMap`, no streaming, no external calls.

- [ ] **Step 1: Create the file with all three tools**

```go
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/knid/wemob/internal/utils"
)

const maxReadBytes = 200 * 1024 // 200 KB

// protectedPaths cannot be written to or deleted by the agent.
var protectedPaths = map[string]bool{
	"package.json":      true,
	"tsconfig.json":     true,
	"next.config.ts":    true,
	"next.config.js":    true,
	"postcss.config.js": true,
	".env.local":        true,
}

func sanitizePath(raw string) (string, error) {
	clean, err := utils.SanitizeFilePath(raw)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}
	return clean, nil
}

// ---------- read_file ----------

type ReadFileTool struct{}

func (ReadFileTool) Name() string        { return "read_file" }
func (ReadFileTool) Description() string { return "Read a file from the project by path." }
func (ReadFileTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "The project-relative path to the file.",
			},
		},
		"required": []string{"path"},
	}
}

func (ReadFileTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return fmt.Sprintf("invalid input: %v", err), true, nil
	}
	path, err := sanitizePath(args.Path)
	if err != nil {
		return err.Error(), true, nil
	}
	content, ok := rc.FileMap.Get(path)
	if !ok {
		return fmt.Sprintf("file not found: %s", path), true, nil
	}
	if len(content) > maxReadBytes {
		return fmt.Sprintf("<truncated: file is %d bytes, max %d>\n\n%s",
			len(content), maxReadBytes, content[:maxReadBytes]), false, nil
	}
	return content, false, nil
}

// ---------- list_files ----------

type ListFilesTool struct{}

func (ListFilesTool) Name() string        { return "list_files" }
func (ListFilesTool) Description() string { return "List files in the project, optionally filtered by glob." }
func (ListFilesTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"glob": map[string]any{
				"type":        "string",
				"description": "Optional glob pattern, e.g. 'app/**/*.tsx'. Leave empty to list everything.",
			},
		},
	}
}

func (ListFilesTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var args struct {
		Glob string `json:"glob"`
	}
	if len(input) > 0 {
		_ = json.Unmarshal(input, &args)
	}
	matches := rc.FileMap.Match(args.Glob)
	if len(matches) == 0 {
		return "no files match", false, nil
	}
	var sb strings.Builder
	for _, p := range matches {
		content, _ := rc.FileMap.Get(p)
		fmt.Fprintf(&sb, "%s (%d bytes)\n", p, len(content))
	}
	return sb.String(), false, nil
}

// ---------- delete_file ----------

type DeleteFileTool struct{}

func (DeleteFileTool) Name() string        { return "delete_file" }
func (DeleteFileTool) Description() string { return "Delete a file from the project." }
func (DeleteFileTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":   map[string]any{"type": "string"},
			"reason": map[string]any{"type": "string"},
		},
		"required": []string{"path"},
	}
}

func (DeleteFileTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var args struct {
		Path   string `json:"path"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return fmt.Sprintf("invalid input: %v", err), true, nil
	}
	path, err := sanitizePath(args.Path)
	if err != nil {
		return err.Error(), true, nil
	}
	if protectedPaths[filepath.Base(path)] || protectedPaths[path] {
		return fmt.Sprintf("protected file, cannot delete: %s", path), true, nil
	}
	if !rc.FileMap.Delete(path) {
		return fmt.Sprintf("file not found: %s", path), true, nil
	}
	rc.Emitter.Emit(EventFileDeleted, FileDeletedEvent{RunID: rc.RunID, Path: path})
	return fmt.Sprintf("deleted %s", path), false, nil
}
```

- [ ] **Step 2: Verify `utils.SanitizeFilePath` exists**

Run: `grep -n "func SanitizeFilePath" internal/utils/prompt.go internal/utils/*.go`
Expected: a single hit in `utils/prompt.go` (already exists — the spec rescue list keeps `utils/prompt.go` intact).

If the grep returns no results, add this minimal helper to `internal/utils/prompt.go`:

```go
func SanitizeFilePath(p string) (string, error) {
	cleaned := filepath.Clean(p)
	if strings.HasPrefix(cleaned, "..") || strings.HasPrefix(cleaned, "/") {
		return "", fmt.Errorf("path traversal not allowed")
	}
	return cleaned, nil
}
```

- [ ] **Step 3: Build**

Run: `go build ./internal/agent/...`
Expected: exit 0.

- [ ] **Step 4: Commit**

```bash
git add internal/agent/tools_base.go
git commit -m "feat(agent): read_file, list_files, delete_file base tools"
```

---

### Task C2.6: Base tool — write_file

**Files:**
- Modify: `internal/agent/tools_base.go` — append `WriteFileTool`

- [ ] **Step 1: Append to `tools_base.go`**

```go
// ---------- write_file ----------

const maxWriteBytes = 200 * 1024 // 200 KB

type WriteFileTool struct{}

func (WriteFileTool) Name() string { return "write_file" }
func (WriteFileTool) Description() string {
	return "Create a new file or fully overwrite an existing file. Prefer edit_file for targeted changes to existing files."
}
func (WriteFileTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":        map[string]any{"type": "string"},
			"content":     map[string]any{"type": "string"},
			"description": map[string]any{"type": "string"},
		},
		"required": []string{"path", "content"},
	}
}

func (WriteFileTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var args struct {
		Path        string `json:"path"`
		Content     string `json:"content"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return fmt.Sprintf("invalid input: %v", err), true, nil
	}
	path, err := sanitizePath(args.Path)
	if err != nil {
		return err.Error(), true, nil
	}
	if protectedPaths[filepath.Base(path)] || protectedPaths[path] {
		return fmt.Sprintf("protected file, cannot overwrite: %s", path), true, nil
	}
	if len(args.Content) > maxWriteBytes {
		return fmt.Sprintf("content too large: %d bytes (max %d)", len(args.Content), maxWriteBytes), true, nil
	}

	op := "update"
	if _, exists := rc.FileMap.Get(path); !exists {
		op = "create"
	}
	rc.FileMap.Set(path, args.Content)
	rc.Emitter.Emit(EventFileWritten, FileWrittenEvent{
		RunID:     rc.RunID,
		Path:      path,
		Operation: op,
		Bytes:     len(args.Content),
		Content:   args.Content,
	})
	return fmt.Sprintf("%sd %s (%d bytes)", op, path, len(args.Content)), false, nil
}
```

- [ ] **Step 2: Build**

Run: `go build ./internal/agent/...`
Expected: exit 0.

- [ ] **Step 3: Commit**

```bash
git add internal/agent/tools_base.go
git commit -m "feat(agent): write_file tool"
```

---

### Task C2.7: Base tool — edit_file

**Files:**
- Modify: `internal/agent/tools_base.go` — append `EditFileTool`

- [ ] **Step 1: Append to `tools_base.go`**

```go
// ---------- edit_file ----------

type EditFileTool struct{}

func (EditFileTool) Name() string { return "edit_file" }
func (EditFileTool) Description() string {
	return "Surgically replace exactly one occurrence of old_string with new_string in an existing file. Fails if old_string does not appear or appears more than once — in that case, add surrounding context to old_string and retry."
}
func (EditFileTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":        map[string]any{"type": "string"},
			"old_string":  map[string]any{"type": "string"},
			"new_string":  map[string]any{"type": "string"},
			"description": map[string]any{"type": "string"},
		},
		"required": []string{"path", "old_string", "new_string"},
	}
}

func (EditFileTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var args struct {
		Path        string `json:"path"`
		OldString   string `json:"old_string"`
		NewString   string `json:"new_string"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return fmt.Sprintf("invalid input: %v", err), true, nil
	}
	path, err := sanitizePath(args.Path)
	if err != nil {
		return err.Error(), true, nil
	}
	if protectedPaths[filepath.Base(path)] || protectedPaths[path] {
		return fmt.Sprintf("protected file, cannot edit: %s", path), true, nil
	}
	content, ok := rc.FileMap.Get(path)
	if !ok {
		return fmt.Sprintf("file not found: %s — use write_file to create it", path), true, nil
	}
	if args.OldString == "" {
		return "old_string must not be empty", true, nil
	}

	count := strings.Count(content, args.OldString)
	switch count {
	case 0:
		return fmt.Sprintf("old_string not found in %s — the file content may have drifted. call read_file to resync and retry", path), true, nil
	case 1:
		// ok
	default:
		return fmt.Sprintf("old_string appears %d times in %s — add more surrounding context to disambiguate", count, path), true, nil
	}
	newContent := strings.Replace(content, args.OldString, args.NewString, 1)
	if len(newContent) > maxWriteBytes {
		return fmt.Sprintf("resulting file too large: %d bytes (max %d)", len(newContent), maxWriteBytes), true, nil
	}
	rc.FileMap.Set(path, newContent)
	rc.Emitter.Emit(EventFileWritten, FileWrittenEvent{
		RunID:     rc.RunID,
		Path:      path,
		Operation: "update",
		Bytes:     len(newContent),
		Content:   newContent,
	})
	return fmt.Sprintf("edited %s (1 replacement, %d bytes)", path, len(newContent)), false, nil
}
```

- [ ] **Step 2: Build**

Run: `go build ./internal/agent/...`
Expected: exit 0.

- [ ] **Step 3: Commit**

```bash
git add internal/agent/tools_base.go
git commit -m "feat(agent): edit_file tool with single-occurrence replacement"
```

---

## Phase C2 continues in the next section of this plan.

### Task C2.8: Base tool — web_search (via Anthropic server tool)

Good news: the existing codebase does NOT run a custom web-search HTTP client. It uses Anthropic's built-in server-side `web_search` tool via `(*utils.LLMProvider).WebSearchTool()` in `internal/utils/llm_provider.go:56`. The agent registry needs a thin wrapper that injects the provider's server tool into the tools array passed to `StreamWithTools`. No executor logic runs on our server — Anthropic handles the query internally and returns results as part of the model's context.

**Files:**
- Modify: `internal/agent/registry.go` — extend tool shape to carry server tools
- Modify: `internal/agent/tools_base.go` — nothing to append (no executor needed)

- [ ] **Step 1: Extend `Registry` to carry server tools**

In `internal/agent/registry.go`, add the server-tool support. This modifies Task C2.4's Registry type:

```go
// Add this import if not already present:
import "github.com/knid/wemob/internal/utils"

// ServerToolRef is an Anthropic server-side tool (e.g. web_search) that the
// provider executes internally. No local executor runs.
type ServerToolRef struct {
	Tool        *utils.Tool // the utils.Tool with ServerTool set
	DisplayName string      // surfaced in RunStartEvent.Tools
}

// Replace the existing Registry struct with this version:
type Registry struct {
	order       []string
	tools       map[string]ToolExecutor
	serverTools []ServerToolRef
}

// AddServerTool registers an Anthropic server-side tool.
func (r *Registry) AddServerTool(ref ServerToolRef) {
	// Dedupe by display name.
	for _, existing := range r.order {
		if existing == ref.DisplayName {
			return
		}
	}
	r.serverTools = append(r.serverTools, ref)
	r.order = append(r.order, ref.DisplayName)
}

// ServerTools returns the server-side tools for StreamWithTools composition.
func (r *Registry) ServerTools() []ServerToolRef {
	out := make([]ServerToolRef, len(r.serverTools))
	copy(out, r.serverTools)
	return out
}
```

The existing `AnthropicSchemas` method from Task C2.4 keeps working — it still returns schemas only for executor tools. The `registryToUtilsTools` helper in Task C2.19 is the spot where both executor tools AND server tools get merged into the `[]utils.Tool` slice handed to `StreamWithTools`.

- [ ] **Step 2: Note — `BuildRegistryFor` is created in Task C2.12**

This task only adds the `ServerToolRef` type and the `AddServerTool` / `ServerTools` methods to `Registry`. The actual wiring of `deps.LLMProvider.WebSearchTool()` into the registry happens in Task C2.12 where `BuildRegistryFor` is first defined with the server-tool call already included. Do NOT edit `registry.go` to modify a function that doesn't exist yet.

- [ ] **Step 3: Note on non-Anthropic providers**

`(*utils.LLMProvider).WebSearchTool()` returns `nil` for non-Anthropic providers (OpenRouter, Google, Moonshot, ZAI). When the provider is non-Anthropic, the agent runs without web search. That's acceptable — the project mostly targets Anthropic, and the alternative (running our own search client) is strictly more complexity than scope B warrants.

- [ ] **Step 4: Build**

Run: `go build ./internal/agent/...`
Expected: exit 0 (the new `ServerToolRef`, `AddServerTool`, `ServerTools` methods + updated Registry struct compile cleanly).

- [ ] **Step 5: Commit**

```bash
git add internal/agent/registry.go
git commit -m "feat(agent): ServerToolRef type on Registry for Anthropic-native tools"
```

---

### Task C2.9: Base tool — generate_image

**Files:**
- Modify: `internal/agent/tools_base.go` — append `GenerateImageTool`

- [ ] **Step 1: Find the existing image-gen client**

```bash
grep -rn "executeChatGenerateImage\|IMAGE_GEN\|generate.*image" internal/ | head -20
```

Note the secret name, endpoint, and request shape used by the old `executeChatGenerateImage` in `app_builder_tools.go`. The new tool reuses the same client path.

- [ ] **Step 2: Append `GenerateImageTool` to `tools_base.go`**

```go
// ---------- generate_image ----------

type GenerateImageTool struct {
	Deps *RunDependencies
}

func (GenerateImageTool) Name() string { return "generate_image" }
func (GenerateImageTool) Description() string {
	return "Generate an image and return a public URL. Use for hero imagery, product photos, illustrations. Never use stock URLs (picsum, unsplash, etc.)."
}
func (GenerateImageTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prompt": map[string]any{"type": "string", "description": "Detailed visual description"},
			"width":  map[string]any{"type": "integer", "minimum": 256, "maximum": 2048},
			"height": map[string]any{"type": "integer", "minimum": 256, "maximum": 2048},
			"style":  map[string]any{"type": "string", "enum": []string{"photo", "illustration", "icon"}},
		},
		"required": []string{"prompt"},
	}
}

func (g GenerateImageTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var args struct {
		Prompt string `json:"prompt"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
		Style  string `json:"style"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return fmt.Sprintf("invalid input: %v", err), true, nil
	}
	if strings.TrimSpace(args.Prompt) == "" {
		return "prompt is required", true, nil
	}
	if args.Width == 0 {
		args.Width = 1024
	}
	if args.Height == 0 {
		args.Height = 1024
	}
	// Delegate to the rescued helper (see Task C4.1-alt below — image client moves to
	// services/imagegen during the rescue phase). For now, call through a deps shim.
	url, err := g.Deps.GenerateImage(ctx, args.Prompt, args.Width, args.Height, args.Style, rc.Project.ID.String())
	if err != nil {
		return fmt.Sprintf("image generation failed: %v", err), true, nil
	}
	resp, _ := json.Marshal(map[string]any{
		"url":    url,
		"width":  args.Width,
		"height": args.Height,
	})
	return string(resp), false, nil
}
```

- [ ] **Step 3: Add the `GenerateImage` method to `RunDependencies`**

Modify `internal/agent/dependencies.go`:

```go
// Add at the bottom of the file:

// ImageGenerator is the signature of the image-gen helper the orchestrator wires
// into RunDependencies. Implementations should call the existing image service,
// upload to S3 via AssetsStorage, and return the public URL.
type ImageGenerator func(ctx context.Context, prompt string, w, h int, style, projectID string) (string, error)
```

And add a field to the struct:

```go
type RunDependencies struct {
	// ... existing fields ...
	GenerateImage ImageGenerator
}
```

Remove the `(d *RunDependencies) GenerateImage` method call from the tool and instead call `g.Deps.GenerateImage(...)` as a function field. The tool code above already does this.

- [ ] **Step 4: Build**

Run: `go build ./internal/agent/...`
Expected: exit 0.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/tools_base.go internal/agent/dependencies.go
git commit -m "feat(agent): generate_image tool with ImageGenerator dep"
```

---

### Task C2.10: Conditional tool — execute_sql

**Files:**
- Create: `internal/agent/tools_db.go`

- [ ] **Step 1: Create the file**

```go
package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type ExecuteSQLTool struct {
	Deps *RunDependencies
}

func (ExecuteSQLTool) Name() string { return "execute_sql" }
func (ExecuteSQLTool) Description() string {
	return "Run a SQL query against the project's database. Set readonly=true for SELECT queries; readonly=false for INSERT/UPDATE/DELETE/CREATE/ALTER/DROP. Timeout 10s, max 50 rows returned."
}
func (ExecuteSQLTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query":    map[string]any{"type": "string"},
			"readonly": map[string]any{"type": "boolean"},
		},
		"required": []string{"query", "readonly"},
	}
}

func (e ExecuteSQLTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var args struct {
		Query    string `json:"query"`
		ReadOnly bool   `json:"readonly"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return fmt.Sprintf("invalid input: %v", err), true, nil
	}
	if strings.TrimSpace(args.Query) == "" {
		return "query is required", true, nil
	}

	// Get the Neon connection string for this project.
	connStr, err := e.Deps.NeonConnString(rc.Project.ID.String())
	if err != nil || connStr == "" {
		return "this project does not have a database connected", true, nil
	}
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return fmt.Sprintf("could not connect: %v", err), true, nil
	}
	defer db.Close()

	qCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if args.ReadOnly {
		tx, err := db.BeginTx(qCtx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			return fmt.Sprintf("begin read-only tx: %v", err), true, nil
		}
		defer tx.Rollback()
		rows, err := tx.QueryContext(qCtx, args.Query)
		if err != nil {
			return fmt.Sprintf("query error: %v", err), true, nil
		}
		defer rows.Close()
		return formatRows(rows, 50)
	}

	// Write path — plain exec, no tx wrapping.
	res, err := db.ExecContext(qCtx, args.Query)
	if err != nil {
		return fmt.Sprintf("exec error: %v", err), true, nil
	}
	affected, _ := res.RowsAffected()
	return fmt.Sprintf("ok (%d rows affected)", affected), false, nil
}

// formatRows serialises up to maxRows into a JSON array string for the tool result.
func formatRows(rows *sql.Rows, maxRows int) (string, bool, error) {
	cols, err := rows.Columns()
	if err != nil {
		return fmt.Sprintf("columns error: %v", err), true, nil
	}
	var out []map[string]any
	count := 0
	truncated := false
	for rows.Next() {
		if count >= maxRows {
			truncated = true
			break
		}
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return fmt.Sprintf("scan error: %v", err), true, nil
		}
		row := make(map[string]any, len(cols))
		for i, c := range cols {
			row[c] = vals[i]
		}
		out = append(out, row)
		count++
	}
	result := map[string]any{
		"rows":      out,
		"row_count": count,
		"truncated": truncated,
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return string(b), false, nil
}
```

- [ ] **Step 2: Add `NeonConnString` to `RunDependencies`**

Modify `internal/agent/dependencies.go`:

```go
// Add next to GenerateImage:
type NeonConnResolver func(projectID string) (string, error)

type RunDependencies struct {
	// ... existing ...
	NeonConnString NeonConnResolver
}
```

Wire this in Task C4 rescue — the rescued `neon_controller.go` will provide the concrete function.

- [ ] **Step 3: Build**

Run: `go build ./internal/agent/...`
Expected: exit 0. (You may need to import `_ "github.com/lib/pq"` somewhere — check whether the project already does so via `database/database.go`; if not, add the underscore import in `tools_db.go`.)

- [ ] **Step 4: Commit**

```bash
git add internal/agent/tools_db.go internal/agent/dependencies.go
git commit -m "feat(agent): execute_sql conditional tool"
```

---

### Task C2.11: Conditional tool — configure_mobile_bridge

**Files:**
- Create: `internal/agent/tools_mobile.go`

- [ ] **Step 1: Find the current bridge method enum**

Run: `grep -n "ALL_BRIDGE_METHODS" internal/constants/ai_builder.go`

Note the exact slice definition — we'll reference it via a constants package once the rescue phase (C4) moves it out of `ai_builder.go`.

- [ ] **Step 2: Create `tools_mobile.go`**

```go
package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/knid/wemob/internal/constants"
)

type ConfigureMobileBridgeTool struct {
	Deps *RunDependencies
}

func (ConfigureMobileBridgeTool) Name() string { return "configure_mobile_bridge" }
func (ConfigureMobileBridgeTool) Description() string {
	return "Enable or disable a native mobile bridge method (camera, geolocation, push, etc.) or update its config."
}
func (ConfigureMobileBridgeTool) Schema() map[string]any {
	methods := make([]string, 0, len(constants.ALL_BRIDGE_METHODS))
	methods = append(methods, constants.ALL_BRIDGE_METHODS...)
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"method":  map[string]any{"type": "string", "enum": methods},
			"enabled": map[string]any{"type": "boolean"},
			"config":  map[string]any{"type": "object"},
		},
		"required": []string{"method", "enabled"},
	}
}

func (c ConfigureMobileBridgeTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var args struct {
		Method  string         `json:"method"`
		Enabled bool           `json:"enabled"`
		Config  map[string]any `json:"config"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return fmt.Sprintf("invalid input: %v", err), true, nil
	}
	found := false
	for _, m := range constants.ALL_BRIDGE_METHODS {
		if m == args.Method {
			found = true
			break
		}
	}
	if !found {
		return fmt.Sprintf("unknown bridge method: %s", args.Method), true, nil
	}
	if err := c.Deps.UpdateMobileBridgeMethod(ctx, rc.Project.ID.String(), args.Method, args.Enabled, args.Config); err != nil {
		return fmt.Sprintf("update failed: %v", err), true, nil
	}
	return fmt.Sprintf("mobile bridge method '%s' set to enabled=%v", args.Method, args.Enabled), false, nil
}
```

- [ ] **Step 3: Add `UpdateMobileBridgeMethod` to `RunDependencies`**

```go
type MobileBridgeUpdater func(ctx context.Context, projectID, method string, enabled bool, config map[string]any) error

type RunDependencies struct {
	// ...
	UpdateMobileBridgeMethod MobileBridgeUpdater
}
```

The concrete function will be wired from `controllers/controller.go` via `Controller.AgentDeps()` — it performs a Bun update on `models.MobileAppConfig`.

- [ ] **Step 4: Note for rescue phase**

After Task C4.1 (which moves `ALL_BRIDGE_METHODS` to `constants/mobile_bridge.go`), this file compiles. Until then it may reference `ai_builder.go`'s export — temporarily update the import to `ai_builder.go`'s package path. The easiest path is: do Task C4.1's bridge-method move BEFORE this task's build step if it's blocking. The phase ordering in this plan is C2 → C3 → C4 → C5, but the rescue-and-use order inside phases can be flexible.

- [ ] **Step 5: Build**

Run: `go build ./internal/agent/...`
Expected: exit 0 once `ALL_BRIDGE_METHODS` lives in `constants` (it currently does — it's exported from `ai_builder.go`'s `constants` package, so the import works directly).

- [ ] **Step 6: Commit**

```bash
git add internal/agent/tools_mobile.go internal/agent/dependencies.go
git commit -m "feat(agent): configure_mobile_bridge conditional tool"
```

---

### Task C2.12: Registry builder — `BuildRegistryFor`

**Files:**
- Modify: `internal/agent/registry.go` — append `BuildRegistryFor`

- [ ] **Step 1: Append**

```go
// BuildRegistryFor returns a registry populated with the base tools plus any
// conditional tools appropriate for the project's state.
//
// Note: web_search is not a local executor — it's an Anthropic server-side
// tool exposed via deps.LLMProvider.WebSearchTool(). If the provider is
// not Anthropic (OpenRouter, Google, etc.) the server tool returns nil and
// the agent runs without web search for that project.
func BuildRegistryFor(project *models.AppProject, deps *RunDependencies) *Registry {
	r := NewRegistry()
	r.Register(ReadFileTool{})
	r.Register(ListFilesTool{})
	r.Register(WriteFileTool{})
	r.Register(EditFileTool{})
	r.Register(DeleteFileTool{})
	r.Register(GenerateImageTool{Deps: deps})

	if ws := deps.LLMProvider.WebSearchTool(); ws != nil {
		r.AddServerTool(ServerToolRef{Tool: ws, DisplayName: "web_search"})
	}

	if projectHasDatabase(project) {
		r.Register(ExecuteSQLTool{Deps: deps})
	}
	if project.ProjectType == "mobile_app" {
		r.Register(ConfigureMobileBridgeTool{Deps: deps})
	}
	return r
}

// projectHasDatabase returns true if the project has a connected Neon integration.
// The actual lookup runs through deps during tool execution — this predicate is a
// lightweight flag based on project.Settings. If Settings is missing the flag,
// the agent simply doesn't get the execute_sql tool this run.
func projectHasDatabase(project *models.AppProject) bool {
	if project.Settings == nil {
		return false
	}
	if v, ok := project.Settings["has_database"].(bool); ok {
		return v
	}
	return false
}
```

Add the missing import at the top of `registry.go`:

```go
import (
	// ... existing imports ...
	"github.com/knid/wemob/internal/models"
)
```

- [ ] **Step 2: Build**

Run: `go build ./internal/agent/...`
Expected: exit 0.

- [ ] **Step 3: Commit**

```bash
git add internal/agent/registry.go
git commit -m "feat(agent): BuildRegistryFor with conditional tool gating"
```

---

## Phase C2 continues with context builders, templates, prompt, and orchestrator in the next section.

### Task C2.13: Template injection — `templates.go`

**Files:**
- Create: `internal/agent/templates.go`

- [ ] **Step 1: Locate existing template registries**

```bash
grep -rn "BaseFilePaths\|UIFilePaths\|EcommerceFilePaths" internal/templates/ internal/controllers/
```

The old `app_builder_controller.go` used `templates.BaseFilePaths`, `templates.UIFilePaths`, and `templates.EcommerceFilePaths` arrays plus `getTemplateContentWithLang` / `getUITemplateContent` / `getEcommerceTemplateContent` to materialise each file. These helpers are **rescued in Task C4.2** into `internal/templates/content.go`. For Task C2.13 we assume they exist there under the same names.

- [ ] **Step 2: Create `templates.go`**

```go
package agent

import (
	"fmt"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/gitrepo"
	"github.com/knid/wemob/internal/templates"
)

// InjectTemplates writes the project-type-appropriate template files into an
// empty repo. Called exactly once, during setup of a draft project's first run.
// Returns the number of files written.
func InjectTemplates(repo *gitrepo.Repo, project *models.AppProject) (int, error) {
	lang := constants.DetectLanguage(project.Prompt)
	written := 0

	// Base (package.json, tsconfig, next.config, etc.) — skip app/globals.css
	// so the LLM writes its own theme.
	for _, p := range templates.BaseFilePaths {
		if p == "app/globals.css" {
			continue
		}
		content := templates.GetContentWithLang(p, nil, nil, project.ProjectType, nil, lang)
		if content == "" {
			continue
		}
		if err := repo.WriteFile(p, []byte(content)); err != nil {
			return written, fmt.Errorf("write %s: %w", p, err)
		}
		written++
	}

	// UI components (lib/utils, components/ui/*)
	for _, p := range templates.UIFilePaths {
		content := templates.GetUIContent(p)
		if content == "" {
			continue
		}
		if err := repo.WriteFile(p, []byte(content)); err != nil {
			return written, fmt.Errorf("write %s: %w", p, err)
		}
		written++
	}

	// Ecommerce templates (when applicable)
	if project.ProjectType == "ecommerce" {
		needsAuth := false
		if v, ok := project.Settings["needs_auth"].(bool); ok {
			needsAuth = v
		}
		for _, p := range templates.EcommerceFilePaths {
			var content string
			if p == "components/ecommerce/providers.tsx" {
				content = templates.BuildEcommerceProviders(needsAuth)
			} else {
				content = templates.GetEcommerceContent(p, lang)
			}
			if content == "" {
				continue
			}
			if err := repo.WriteFile(p, []byte(content)); err != nil {
				return written, fmt.Errorf("write %s: %w", p, err)
			}
			written++
		}
	}

	return written, nil
}
```

- [ ] **Step 3: Note on function names**

The rescued helpers in `templates/content.go` MUST use these exact names: `GetContentWithLang`, `GetUIContent`, `GetEcommerceContent`, `BuildEcommerceProviders`. Task C4.2 creates them as wrappers around the original `getTemplateContentWithLang` / `getUITemplateContent` / `getEcommerceTemplateContent` / `buildEcommerceProviders` helpers to keep naming consistent.

- [ ] **Step 4: Build**

Run: `go build ./internal/agent/...`
Expected: failing on the missing `templates.GetContentWithLang` et al — that's expected, Task C4.2 fixes it. Add a `// TEMP STUB` file if you need an interim build:

```go
// internal/agent/templates_stub_tmp.go (delete after Task C4.2)
//go:build tmpstub

package agent
```

Or just leave it broken and accept that `agent/` only compiles after C4.2 when that task lands.

- [ ] **Step 5: Commit (allow broken agent build temporarily)**

```bash
git add internal/agent/templates.go
git commit -m "feat(agent): InjectTemplates — first-run template copy"
```

---

### Task C2.14: Context builders — system prompt + file tree block

**Files:**
- Create: `internal/agent/context.go`

- [ ] **Step 1: Create the file**

```go
package agent

import (
	"fmt"
	"sort"
	"strings"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/utils"
)

// BuildSystemMessages returns the three cached system messages for a run:
//
//   1. BasePrompt           (stable across all projects of this type)
//   2. BasePrompt + addenda (stable across this project's runs)
//   3. File tree            (stable within this run, reused across turns)
//
// anthropic.go adds cache_control to the first and last system blocks
// automatically; Task C2.26 extends it to mark all three when present.
func BuildSystemMessages(project *models.AppProject, fileMap *SafeFileMap) []utils.Message {
	base := BasePrompt
	addendum := buildAddendum(project)

	var msgs []utils.Message
	msgs = append(msgs, utils.Message{Role: "system", Content: base})
	if addendum != "" {
		msgs = append(msgs, utils.Message{Role: "system", Content: addendum})
	}
	msgs = append(msgs, utils.Message{Role: "system", Content: buildFileTreeBlock(fileMap)})
	return msgs
}

// buildAddendum composes project-specific prompt blocks.
func buildAddendum(project *models.AppProject) string {
	var parts []string
	switch project.ProjectType {
	case "ecommerce":
		parts = append(parts, EcommerceAddendum)
	case "mobile_app":
		parts = append(parts, MobileAddendum)
	}
	if projectHasAuth(project) {
		parts = append(parts, AuthAddendum)
	}
	if projectHasDatabase(project) {
		parts = append(parts, DatabaseAddendum)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n\n")
}

// buildFileTreeBlock produces a deterministic "FILE TREE" listing.
func buildFileTreeBlock(fileMap *SafeFileMap) string {
	files := fileMap.Snapshot()
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var sb strings.Builder
	sb.WriteString("FILE TREE\n")
	sb.WriteString("=========\n\n")
	if len(paths) == 0 {
		sb.WriteString("(empty — this is a new project, start by creating files)\n")
		return sb.String()
	}
	for _, p := range paths {
		fmt.Fprintf(&sb, "%s (%d bytes)\n", p, len(files[p]))
	}
	return sb.String()
}

// projectHasAuth consults project.Settings for the auth flag.
func projectHasAuth(project *models.AppProject) bool {
	if project.Settings == nil {
		return false
	}
	if v, ok := project.Settings["needs_auth"].(bool); ok {
		return v
	}
	return false
}
```

- [ ] **Step 2: Build (will fail on missing prompt constants)**

Run: `go build ./internal/agent/...`
Expected: errors for `BasePrompt`, `EcommerceAddendum`, `MobileAddendum`, `AuthAddendum`, `DatabaseAddendum` — we create them next.

- [ ] **Step 3: Commit**

```bash
git add internal/agent/context.go
git commit -m "feat(agent): system message and file tree builders"
```

---

### Task C2.15: Prompt constants — `prompt.go`

**Files:**
- Create: `internal/agent/prompt.go`

This task defines the structure of the base prompt and a minimal seed version. The actual prose is written by condensing the old constants as listed in the spec's §7 content seed table. The implementer should do this extraction manually.

- [ ] **Step 1: Create the file with skeleton sections**

```go
package agent

// BasePrompt is the single base system prompt used for every agent run,
// regardless of project type. It's designed for prompt caching — its text
// does NOT change between runs. Project-specific content lives in
// EcommerceAddendum / MobileAddendum / AuthAddendum / DatabaseAddendum.
//
// The prompt is organised into labelled sections. Keep each section
// focused and avoid cross-references — the model reads top-to-bottom.
const BasePrompt = `# ROLE
You are a senior Next.js engineer building production web applications for
non-technical users. You write clean, modern TypeScript and Tailwind CSS v4
with shadcn/ui. You are decisive, tasteful, and terse.

# TECH STACK
- Next.js 16 App Router
- TypeScript (strict)
- Tailwind CSS v4
- shadcn/ui components (already installed, read via read_file to see what's available)
- Server Components by default; add "use client" only when necessary
- No external CSS-in-JS libraries (no styled-components, emotion, etc.)
- No placeholder image services (no picsum.photos, unsplash, stock URLs)

# OUTPUT PROTOCOL
You work by calling tools. Narrate briefly (1-2 sentences) before each tool
call so the user can follow along, but never paste file content in narration.

Use the tools in this order of preference:
- edit_file for small, targeted changes to an existing file. Always prefer this.
- write_file only when creating a new file OR when a rewrite is genuinely simpler than a diff.
- read_file BEFORE edit_file if you haven't seen the current content this run.
- list_files with a glob to discover structure. The system prompt already shows the full file tree.
- delete_file when a file is no longer needed.
- web_search for current information the model wasn't trained on.
- generate_image for any imagery. NEVER link to external stock photo URLs.

When you are finished, simply stop calling tools and output a brief wrap-up
sentence. Do not say "done" — your final narration is the signal.

# DESIGN PRINCIPLES
- Honest typography and generous spacing beat decoration.
- Prefer gradients, SVG, icons, and CSS patterns over images for decoration.
- Never hardcode placeholder data where real data should come from code.
- Every page must be responsive. Test mental model: phone → tablet → desktop.
- Use shadcn/ui components (Button, Card, Dialog, etc.) instead of custom ones.
- Respect the existing design language when editing — match the codebase.

# CODE STYLE
- File naming: kebab-case for files, PascalCase for component symbols.
- Component co-location: components/ui/* is shadcn; components/* is app-specific.
- Server/client split: keep components server by default. "use client" goes at the top when needed.
- Use async Server Components for data fetching; no useEffect for data loads.
- Extract shared types to lib/types.ts. Extract shared utilities to lib/utils.ts.
- No unused imports. No console.log in committed code. No TODO comments.

# CONSTRAINTS
- package.json, tsconfig.json, next.config.*, postcss.config.js, .env.local are PROTECTED. You cannot write, edit, or delete them.
- The available dependency set is pinned. Don't add new packages.
- Env vars live in .env.local (managed by the user). Reference them by name in code (process.env.VAR_NAME). Never hardcode secrets.
- Images come from generate_image or user-uploaded assets. Never generate broken links.
- After editing an existing file you haven't read this run, read_file first to verify your edit target matches reality.
- Keep file size under 200 KB. Split large files into smaller modules if they grow.

# LANGUAGE
Match the project's locale. Detect from the user's message and the existing
file content. Default to English unless there's a clear signal otherwise.
All user-facing text (headings, labels, CTAs, microcopy) must be in the
detected language. Code identifiers stay in English.
`
```

- [ ] **Step 2: Seed the prompt from old constants**

Open `internal/constants/ai_builder.go` and extract the prose from these constants, condensing them into the sections above:

| Old constant | Target section |
|---|---|
| `builderRole` (`ai_builder.go:21`) | `# ROLE` |
| `builderDesignPhilosophy` (`ai_builder.go:32`) | `# DESIGN PRINCIPLES` |
| `builderFrameworkRulesBase` (`ai_builder.go:112`) | `# TECH STACK` + `# CODE STYLE` |
| `builderImageRulesStandard` (`ai_builder.go:183`) | `# DESIGN PRINCIPLES` (image bullets) |
| `builderLanguageRules` (`ai_builder.go:204`) | `# LANGUAGE` |
| `builderToneRules` (`ai_builder.go:211`) | fold into `# ROLE` |
| `builderIntegrationRules` (`ai_builder.go:219`) | `# CONSTRAINTS` |

DO NOT copy these old constants verbatim:
- `NEXTJS_BUILDER_TOOL_INSTRUCTIONS` — replaced by `# OUTPUT PROTOCOL` above
- `NEXTJS_BUILDER_SINGLESHOT_INSTRUCTIONS` / `_CHAT_INSTRUCTIONS` / `_UNIFIED_CHAT_INSTRUCTIONS` — all discarded (one mode now)

After extraction, the `BasePrompt` should be ~400–600 lines of plain text. Run `wc -l internal/agent/prompt.go` to sanity-check length.

- [ ] **Step 3: Build**

Run: `go build ./internal/agent/...`
Expected: `context.go` still fails on missing addenda — we create them next.

- [ ] **Step 4: Commit**

```bash
git add internal/agent/prompt.go
git commit -m "feat(agent): BasePrompt skeleton (condensed from old ai_builder constants)"
```

---

### Task C2.16: Prompt addenda — `prompt_addenda.go`

**Files:**
- Create: `internal/agent/prompt_addenda.go`

- [ ] **Step 1: Create the file with four named constants**

```go
package agent

// EcommerceAddendum — appended to BasePrompt for project.ProjectType == "ecommerce".
// Seed from: builderEcommerceDataRules, ecommerceStoreRules,
// builderFrameworkRulesEcommerce, builderImageRulesEcommerce (all in ai_builder.go).
// Target length: ~40–60 lines.
const EcommerceAddendum = `# ECOMMERCE ADDENDUM
This project is an ecommerce storefront. Additional rules apply:

## Store SDK
The lib/store-sdk.ts helpers (fetchProducts, fetchProductBySlug, fetchCategories,
searchProducts) are the ONLY way to read store data. Never hardcode products,
categories, or prices. Never "mock" data for demos — the store is real.

## Cart and checkout are templates
components/ecommerce/* files (cart/page.tsx, checkout/page.tsx, success/page.tsx,
cancel/page.tsx, providers.tsx) are pre-provided templates. Do NOT rewrite them.
Your job is to build the marketing and browsing surfaces — home, category,
product detail, about, contact, shipping, returns, privacy, terms, FAQ.

## Product imagery
Use generate_image for product hero shots when seed data needs images.
Users attach real assets via the dashboard — reference those by URL.

## Required pages
Every ecommerce project must have: /, /cart, /checkout, /checkout/success,
/checkout/cancel, /about, /contact, /faq, /policies/shipping, /policies/returns,
/policies/privacy, /policies/terms. The cart/checkout/success/cancel routes
come from templates — don't duplicate them.
`

// MobileAddendum — for project.ProjectType == "mobile_app".
// Seed from: NEXTJS_BUILDER_MOBILE_RULES, NEXTJS_BUILDER_MOBILE_CONFIG_TOOL_RULES.
// Target length: ~40–60 lines.
const MobileAddendum = `# MOBILE APP ADDENDUM
This project renders inside a native mobile shell (React Native WebView at
~430px max width). Treat it as a mobile-first single-column app, not a
responsive website.

## Layout rules
- Single column. No multi-column grids above 1fr.
- Bottom tab navigation (not top nav) for 3+ primary sections.
- Touch targets minimum 44×44.
- Avoid hover-only interactions.
- No desktop-oriented patterns: no sidebars, no hero images above the fold,
  no multi-step wizards unless the flow genuinely requires them.

## Native capabilities via configure_mobile_bridge
Use the configure_mobile_bridge tool to enable camera, geolocation, push
notifications, biometrics, haptics, share, and other native capabilities.
Enabled methods expose a global window.wemobBridge API the app code can call.

## Viewport
Assume max width 430px and use vh-relative heights sparingly — mobile
browsers resize chrome and break vh calculations. Prefer min-h-screen and
manual height controls.
`

// AuthAddendum — for projects with needs_auth.
// Seed from: NEXTJS_BUILDER_AUTH_RULES.
// Target length: ~30–40 lines.
const AuthAddendum = `# AUTH ADDENDUM
This project has authentication enabled. Additional rules:

- Session helpers live in lib/auth/session.ts. Use getSession() in Server Components.
- Protected routes check session at the top of the page component. Redirect to /login if null.
- Never put auth logic on the client.
- Signup and login forms are Server Actions.
- User fields: { id, email, name }. Don't assume more until you read lib/auth/types.ts.
`

// DatabaseAddendum — for projects with has_database.
// Seed from: NEXTJS_BUILDER_DATABASE_RULES, builderStandardDataRules.
// Target length: ~30–50 lines.
const DatabaseAddendum = `# DATABASE ADDENDUM
This project has a Postgres database (Neon) connected via process.env.DATABASE_URL.
Additional rules:

## Query via execute_sql, read via drizzle
- Run ad-hoc queries via the execute_sql tool (read + write allowed).
- Read data from pages using drizzle queries in lib/db/queries.ts.
- Schema lives in lib/db/schema.ts. Read it before writing queries.
- Use drizzle's select/insert/update/delete — NEVER raw SQL in TypeScript code.

## Schema changes
To alter tables, call execute_sql with readonly=false and a CREATE TABLE /
ALTER TABLE / CREATE INDEX statement. Then update lib/db/schema.ts to match.

## Seed data
When the user asks for "sample products" or "seed data", call execute_sql with
INSERT statements. Use generate_image for imagery referenced from seed rows.
`
```

- [ ] **Step 2: Seed each addendum from old constants**

Condense the listed old constants into each addendum. Target total: ~150–200 lines across all four addenda.

- [ ] **Step 3: Build**

Run: `go build ./internal/agent/...`
Expected: exit 0 for `context.go`, `prompt.go`, `prompt_addenda.go`. Orchestrator file is not yet written so there may still be stub references — that's fine.

- [ ] **Step 4: Commit**

```bash
git add internal/agent/prompt_addenda.go
git commit -m "feat(agent): prompt addenda for ecommerce/mobile/auth/database"
```

---

### Task C2.17: Snapshot helpers — `snapshot.go`

**Files:**
- Create: `internal/agent/snapshot.go`
- Modify: `internal/services/gitrepo/repo.go` — add `WriteFileMap` and `Reset` methods

- [ ] **Step 1: Add `WriteFileMap` to `gitrepo.Repo`**

Open `internal/services/gitrepo/repo.go` and append:

```go
// WriteFileMap atomically replaces the working tree with the provided map.
// Files not in the map are deleted from disk. Called at the end of a
// successful agent run before committing.
func (r *Repo) WriteFileMap(fileMap map[string]string) error {
	return r.WithLock(func() error {
		// Collect current files on disk (via ListFiles) so we can compute deletions.
		existing, err := r.ListFiles()
		if err != nil {
			return fmt.Errorf("list files: %w", err)
		}
		existingSet := make(map[string]bool, len(existing))
		for _, p := range existing {
			existingSet[p] = true
		}

		// Write every file in the map.
		for path, content := range fileMap {
			if err := writeFileAtomic(filepath.Join(r.dir, path), []byte(content)); err != nil {
				return fmt.Errorf("write %s: %w", path, err)
			}
			delete(existingSet, path)
		}
		// Delete files that existed but aren't in the new map.
		for path := range existingSet {
			full := filepath.Join(r.dir, path)
			if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("delete %s: %w", path, err)
			}
		}
		return nil
	})
}

// Reset performs a hard reset of the working tree to the given commit hash.
// Used by the agent's failure path as a defensive clean-up.
func (r *Repo) Reset(commitHash string) error {
	return r.WithLock(func() error {
		cmd := exec.Command("git", "-C", r.dir, "reset", "--hard", commitHash)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("git reset: %w: %s", err, string(out))
		}
		return nil
	})
}

// writeFileAtomic writes data to path via tempfile + rename.
func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}
```

Ensure the imports at the top of `repo.go` include `os`, `os/exec`, `path/filepath`, `fmt`.

- [ ] **Step 2: Create `agent/snapshot.go`**

```go
package agent

import (
	"fmt"

	"github.com/knid/wemob/internal/services/gitrepo"
)

// PreRunCommit captures a baseline commit before the agent makes changes.
// Returns the commit SHA (or the HEAD SHA if there were no pending changes).
func PreRunCommit(repo *gitrepo.Repo, runID string) (string, error) {
	msg := fmt.Sprintf("agent: pre-run %s", runID)
	hash, err := repo.Commit(msg)
	if err != nil {
		// If there's nothing to commit, HEAD is already the pre-run snapshot.
		head, headErr := repo.Head()
		if headErr != nil {
			return "", fmt.Errorf("pre-commit: %w (head: %v)", err, headErr)
		}
		return head, nil
	}
	return hash, nil
}

// FinaliseRun writes the fileMap to disk and commits with the given message.
// Returns the final commit SHA.
func FinaliseRun(repo *gitrepo.Repo, fileMap *SafeFileMap, userMessage string) (string, error) {
	snap := fileMap.Snapshot()
	if err := repo.WriteFileMap(snap); err != nil {
		return "", fmt.Errorf("write file map: %w", err)
	}
	msg := fmt.Sprintf("agent: %s", firstLine(userMessage, 80))
	hash, err := repo.Commit(msg)
	if err != nil {
		return "", fmt.Errorf("post-commit: %w", err)
	}
	return hash, nil
}

// RollbackTo hard-resets to the given commit. Called on run failure as a
// defensive measure — in practice tool mutations only touch the in-memory
// fileMap so there's nothing on disk to undo.
func RollbackTo(repo *gitrepo.Repo, preCommit string) error {
	if preCommit == "" {
		return nil
	}
	return repo.Reset(preCommit)
}

func firstLine(s string, max int) string {
	for i, r := range s {
		if r == '\n' || r == '\r' {
			s = s[:i]
			break
		}
	}
	if len(s) > max {
		s = s[:max]
	}
	if s == "" {
		return "chat turn"
	}
	return s
}
```

- [ ] **Step 3: Build**

Run: `go build ./internal/services/gitrepo/... ./internal/agent/...`
Expected: exit 0.

- [ ] **Step 4: Commit**

```bash
git add internal/services/gitrepo/repo.go internal/agent/snapshot.go
git commit -m "feat(agent): WriteFileMap, Reset, and snapshot helpers"
```

---

### Task C2.18: Orchestrator — `run.go` setup phase

**Files:**
- Create: `internal/agent/run.go`

This is the largest file in the package. We split its creation into three tasks: setup (C2.18), loop (C2.19), and shutdown/rollback (C2.20).

- [ ] **Step 1: Create `run.go` with setup phase**

```go
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"runtime/debug"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/utils"
)

// Budget limits (package-level defaults; override via RunOptions if needed later).
const (
	defaultMaxTurns           = 25
	defaultMaxToolCallsPerTurn = 20
	defaultMaxTokensPerRun    = 500_000
	defaultMaxWallclockMs     = 10 * 60 * 1000
)

// Run is the single entry point for both initial generation and chat edit.
//
// When project.Status == "draft" and the repo is empty, Run injects templates.
// Otherwise it loads the existing fileMap from the repo.
//
// On success: files are committed to git, AppAgentRun is finalised, credits
// are deducted, events are emitted, and nil is returned.
//
// On failure: the in-memory fileMap is discarded, git is defensively reset to
// pre-commit, AppAgentRun is marked failed, NO credits are deducted, error
// events are emitted, and a *Error is returned.
func Run(
	ctx context.Context,
	deps *RunDependencies,
	project *models.AppProject,
	user *models.User,
	message string,
	images []string,
	emitter EventEmitter,
) error {
	runStart := time.Now()

	// Wall-clock deadline: wrap ctx with a max-lifetime timer.
	ctx, cancelDeadline := context.WithTimeout(ctx, time.Duration(defaultMaxWallclockMs)*time.Millisecond)
	defer cancelDeadline()

	// --- Step 1: enforce single-run-per-project invariant ---
	existing, _ := deps.DB.GetActiveAgentRun(project.ID)
	if existing != nil {
		return New(CodeRunInProgress, "setup", "another run is already in progress for this project", nil)
	}

	// --- Step 2: open git repo (creates if missing) ---
	repo, err := deps.GitRepo.OpenOrInit(project.ID.String())
	if err != nil {
		return New(CodeGitError, "setup", "failed to open project repo", err)
	}

	// --- Step 3: first-run template injection (draft projects only) ---
	if project.Status == "draft" {
		existingFiles, _ := repo.ListFiles()
		if len(existingFiles) == 0 {
			if n, injErr := InjectTemplates(repo, project); injErr != nil {
				return New(CodeGitError, "setup", fmt.Sprintf("template injection failed after %d files", n), injErr)
			}
		}
	}

	// --- Step 4: load fileMap from disk ---
	rawFiles, err := repo.ReadAllFiles()
	if err != nil {
		return New(CodeGitError, "setup", "failed to read project files", err)
	}
	fileMap := NewSafeFileMap(rawFiles)

	// --- Step 5: pre-run commit baseline ---
	runID := uuid.New()
	preCommit, err := PreRunCommit(repo, runID.String())
	if err != nil {
		return New(CodeGitError, "setup", "failed to create pre-run snapshot", err)
	}

	// --- Step 6: insert AppAgentRun row ---
	provider := deps.LLMProvider
	model := resolveModel(deps.SecretManager, project)
	run := &models.AppAgentRun{
		ID:        runID,
		ProjectID: project.ID,
		UserID:    user.ID,
		Status:    models.AgentRunStatusRunning,
		PreCommit: preCommit,
		Model:     model,
		Provider:  provider.Name,
	}
	if _, err := deps.DB.InsertAgentRun(run); err != nil {
		return New(CodeInternal, "setup", "failed to record run", err)
	}

	// --- Step 7: persist the user message ---
	imagesJSON := mustJSON(normaliseImages(images))
	userMsgRow := &models.AppChatMessage{
		ProjectID: project.ID,
		RunID:     &runID,
		Role:      "user",
		Content:   message,
		Images:    imagesJSON,
		ToolCalls: []byte("[]"),
	}
	if _, err := deps.DB.InsertChatMessage(userMsgRow); err != nil {
		return finaliseFailure(ctx, deps, repo, run, emitter, "setup", CodeInternal, "failed to persist user message", err, preCommit)
	}
	run.UserMessageID = &userMsgRow.ID
	if err := deps.DB.UpdateAgentRun(run); err != nil {
		slog.Warn("failed to update run user_message_id", "error", err, "run_id", runID)
	}

	// --- Step 8: build LLM message array ---
	history, err := deps.DB.GetRecentChatMessages(project.ID, 30)
	if err != nil {
		slog.Warn("failed to load history, starting fresh", "error", err, "project_id", project.ID)
		history = nil
	}
	llmMessages := BuildSystemMessages(project, fileMap)
	llmMessages = append(llmMessages, convertHistory(history)...)
	// The user message we just inserted is the last history row; convertHistory already included it.

	// --- Step 9: build tool registry ---
	registry := BuildRegistryFor(project, deps)

	// --- Step 10: create RunContext and tracker ---
	tracker := utils.NewTokenTracker()
	tracker.UserID = user.ID.String()
	tracker.ProjectID = project.ID.String()
	ctx = utils.WithTokenTracker(ctx, tracker)

	rc := &RunContext{
		RunID:   runID,
		Project: project,
		User:    user,
		FileMap: fileMap,
		Emitter: emitter,
		Tracker: tracker,
		Deps:    deps,
	}

	// --- Step 11: emit run_start ---
	emitter.Emit(EventRunStart, RunStartEvent{
		RunID:     runID,
		PreCommit: preCommit,
		Model:     model,
		Provider:  provider.Name,
		MaxTurns:  defaultMaxTurns,
		Tools:     registry.Names(),
	})

	// --- Step 12: panic guard ---
	defer func() {
		if r := recover(); r != nil {
			slog.Error("agent.Run panicked", "error", r, "project_id", project.ID, "stack", string(debug.Stack()))
			_ = finaliseFailure(ctx, deps, repo, run, emitter, "agent_loop",
				CodeInternal, fmt.Sprintf("panic: %v", r), nil, preCommit)
		}
	}()

	// The loop body continues in Task C2.19. For now, a placeholder:
	_ = llmMessages
	_ = registry
	_ = rc
	_ = runStart
	return fmt.Errorf("run loop not yet implemented")
}

// ---- helpers used above ----

func resolveModel(sm secret.SecretManager, project *models.AppProject) string {
	// For initial generation, prefer WEBSITE_BUILDER_LLM_MODEL; for chat, fall back to CHAT_MODEL.
	if project.Status == "draft" {
		if m := sm.GetSecret("WEBSITE_BUILDER_LLM_MODEL"); m != "" {
			return m
		}
	}
	if m := sm.GetSecret("WEBSITE_BUILDER_CHAT_MODEL"); m != "" {
		return m
	}
	if m := sm.GetSecret("WEBSITE_BUILDER_LLM_MODEL"); m != "" {
		return m
	}
	return "claude-sonnet-4"
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	if len(b) == 0 {
		return []byte("null")
	}
	return b
}

func normaliseImages(images []string) []map[string]string {
	var out []map[string]string
	for _, img := range images {
		if img == "" {
			continue
		}
		out = append(out, map[string]string{"url": img})
	}
	return out
}

func convertHistory(history []models.AppChatMessage) []utils.Message {
	var out []utils.Message
	for _, m := range history {
		out = append(out, utils.Message{Role: m.Role, Content: m.Content})
	}
	return out
}
```

Add the missing secret import at the top:

```go
import (
	// ...
	"github.com/knid/wemob/internal/services/secret"
)
```

- [ ] **Step 2: Add a placeholder `finaliseFailure` stub at the bottom of `run.go`**

```go
// finaliseFailure will be completed in Task C2.20.
func finaliseFailure(ctx context.Context, deps *RunDependencies, repo interface{ Reset(string) error }, run *models.AppAgentRun, emitter EventEmitter, phase string, code ErrorCode, msg string, err error, preCommit string) error {
	_ = ctx; _ = deps; _ = repo; _ = run; _ = emitter; _ = phase; _ = code; _ = msg; _ = err; _ = preCommit
	return New(code, phase, msg, err)
}
```

- [ ] **Step 3: Build**

Run: `go build ./internal/agent/...`
Expected: exit 0.

- [ ] **Step 4: Commit**

```bash
git add internal/agent/run.go
git commit -m "feat(agent): Run setup phase — repo, snapshot, run row, registry"
```

---

## Phase C2 continues with the loop and shutdown phases in the next section.

### Task C2.19: Orchestrator — loop phase

**Files:**
- Modify: `internal/agent/run.go` — replace the placeholder return at the end of `Run()` with the loop body.

- [ ] **Step 1: Replace the placeholder**

Find the block near the end of `Run()`:
```go
	// The loop body continues in Task C2.19. For now, a placeholder:
	_ = llmMessages
	_ = registry
	_ = rc
	_ = runStart
	return fmt.Errorf("run loop not yet implemented")
```

Replace with the loop body:

```go
	// --- Step 13: agent loop ---
	var (
		turn             = 0
		totalToolCalls   = 0
		narrationBuf     = ""
		toolCallLog      []ChatMessageToolCall
		lastStopReason   string
	)

	// Build the executor adapter that StreamWithTools expects.
	executor := func(name string, argsJSON string) (string, error) {
		toolStart := time.Now()
		toolCallID := uuid.NewString()
		rc.Emitter.Emit(EventToolCallStart, ToolCallStartEvent{
			RunID:        runID,
			Turn:         turn,
			ToolCallID:   toolCallID,
			ToolName:     name,
			InputPreview: json.RawMessage(argsJSON),
		})
		result, isErr, execErr := registry.Execute(ctx, rc, name, json.RawMessage(argsJSON))
		durationMs := int(time.Since(toolStart).Milliseconds())
		status := "ok"
		if isErr || execErr != nil {
			status = "error"
		}
		rc.Emitter.Emit(EventToolCallResult, ToolCallResultEvent{
			RunID:      runID,
			ToolCallID: toolCallID,
			ToolName:   name,
			Status:     status,
			DurationMs: durationMs,
			Summary:    firstLine(result, 140),
			IsError:    isErr || execErr != nil,
		})
		toolCallLog = append(toolCallLog, ChatMessageToolCall{
			ID:         toolCallID,
			Name:       name,
			Input:      json.RawMessage(argsJSON),
			Result:     result,
			IsError:    isErr || execErr != nil,
			DurationMs: durationMs,
		})
		totalToolCalls++
		if execErr != nil {
			return result, execErr
		}
		return result, nil
	}

	narrationSink := func(chunk string) {
		if chunk == "" {
			return
		}
		narrationBuf += chunk
		rc.Emitter.Emit(EventNarration, NarrationEvent{RunID: runID, Turn: turn, Delta: chunk})
	}

	// Keepalive goroutine — emits every 15s until the run finishes.
	keepaliveDone := make(chan struct{})
	go func() {
		tick := time.NewTicker(15 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-keepaliveDone:
				return
			case <-tick.C:
				rc.Emitter.Emit(EventKeepalive, KeepaliveEvent{RunID: runID, TS: time.Now().Unix()})
			}
		}
	}()
	defer close(keepaliveDone)

	for turn = 1; turn <= defaultMaxTurns; turn++ {
		if ctx.Err() != nil {
			return finaliseFailureV2(ctx, deps, repo, run, emitter, "agent_loop", CodeBudgetExceeded, "run canceled or deadline exceeded", ctx.Err(), preCommit, tracker, runStart)
		}

		// Budget check: tokens and credits.
		if deps.CreditChecker != nil && !deps.CreditChecker() {
			return finaliseFailureV2(ctx, deps, repo, run, emitter, "agent_loop", CodeCreditsExhausted, "AI credits exhausted mid-run", nil, preCommit, tracker, runStart)
		}
		if prompt, completion, total, _ := tracker.Totals(); total > defaultMaxTokensPerRun {
			_ = prompt; _ = completion
			return finaliseFailureV2(ctx, deps, repo, run, emitter, "agent_loop", CodeBudgetExceeded,
				fmt.Sprintf("token budget exceeded (%d > %d)", total, defaultMaxTokensPerRun), nil, preCommit, tracker, runStart)
		}

		emitter.Emit(EventTurnStart, TurnStartEvent{RunID: runID, Turn: turn})

		// Reset per-turn narration buffer so NarrationEvent.Turn stays accurate.
		narrationBuf = ""

		toolSchemas := registryToUtilsTools(registry)
		narrationText, llmErr := utils.StreamWithTools(
			ctx,
			model,
			llmMessages,
			toolSchemas,
			provider,
			nil, // responseFormat
			nil, // reasoning
			executor,
			func(toolName, args string) {
				// onToolCall — already handled inside the executor adapter
			},
			narrationSink,
			nil, // onReasoning
		)
		if llmErr != nil {
			return finaliseFailureV2(ctx, deps, repo, run, emitter, "agent_loop", CodeProviderError,
				fmt.Sprintf("LLM call failed on turn %d", turn), llmErr, preCommit, tracker, runStart)
		}

		emitter.Emit(EventTurnEnd, TurnEndEvent{
			RunID:      runID,
			Turn:       turn,
			StopReason: "end_turn",
			Tokens:     trackerDelta(tracker),
		})
		lastStopReason = "end_turn"

		// If there were no tool calls this turn, the model is done.
		if narrationText != "" && len(toolCallLog) == 0 {
			break
		}
		// StreamWithTools returns when the round completes — we treat that as
		// "model finished this turn"; next iteration starts a fresh turn.
		break
	}
	_ = lastStopReason

	// --- Step 14: success shutdown (continued in C2.20) ---
	return finaliseSuccess(ctx, deps, repo, run, emitter, fileMap, narrationBuf, toolCallLog, tracker, runStart, totalToolCalls, turn, message, user, project)
}

// registryToUtilsTools maps the agent Registry to the []utils.Tool shape
// StreamWithTools expects. Executor tools become OpenAI-style function wrappers;
// server tools (Anthropic-native, e.g. web_search) are passed through as-is.
func registryToUtilsTools(r *Registry) []utils.Tool {
	out := make([]utils.Tool, 0, len(r.tools)+len(r.serverTools))
	for _, name := range r.order {
		if t, ok := r.tools[name]; ok {
			out = append(out, utils.Tool{
				Type: "function",
				Function: utils.ToolFunction{
					Name:        t.Name(),
					Description: t.Description(),
					Parameters:  t.Schema(),
				},
			})
		}
	}
	for _, st := range r.serverTools {
		if st.Tool != nil {
			out = append(out, *st.Tool)
		}
	}
	return out
}

// trackerDelta returns the latest per-turn token counts from the tracker.
// We approximate by reading totals; the TokenTracker API in utils/ai.go
// supports per-entry breakdowns — use Entries() if finer granularity is needed.
func trackerDelta(t *utils.TokenTracker) TokenUsage {
	p, c, _, _ := t.Totals()
	return TokenUsage{Input: p, Output: c}
}
```

**NOTE** on loop semantics: `StreamWithTools` in `utils/ai.go` already handles the multi-round tool loop INTERNALLY (see its `maxRounds` constant). For the agent orchestrator, one call to `StreamWithTools` typically equals one "agent run" from the model's perspective. The `for turn` loop here is defensive — in practice it executes once. Future work can restructure this if we want per-turn SSE events truly segmented, but the current shape matches the existing utility.

- [ ] **Step 2: Add missing imports at the top of `run.go`**

```go
import (
	// existing imports ...
	"encoding/json"
)
```

- [ ] **Step 3: Add placeholder `finaliseSuccess` and `finaliseFailureV2` at the bottom of the file**

These are stubs for the next task:

```go
func finaliseSuccess(
	ctx context.Context,
	deps *RunDependencies,
	repo interface{ Reset(string) error },
	run *models.AppAgentRun,
	emitter EventEmitter,
	fileMap *SafeFileMap,
	narration string,
	toolCalls []ChatMessageToolCall,
	tracker *utils.TokenTracker,
	runStart time.Time,
	totalToolCalls int,
	turns int,
	userMessage string,
	user *models.User,
	project *models.AppProject,
) error {
	_ = ctx; _ = deps; _ = repo; _ = run; _ = emitter; _ = fileMap
	_ = narration; _ = toolCalls; _ = tracker; _ = runStart
	_ = totalToolCalls; _ = turns; _ = userMessage; _ = user; _ = project
	return nil
}

func finaliseFailureV2(
	ctx context.Context,
	deps *RunDependencies,
	repo interface{ Reset(string) error },
	run *models.AppAgentRun,
	emitter EventEmitter,
	phase string,
	code ErrorCode,
	message string,
	wrapped error,
	preCommit string,
	tracker *utils.TokenTracker,
	runStart time.Time,
) error {
	_ = ctx; _ = deps; _ = repo; _ = run; _ = emitter
	_ = phase; _ = code; _ = message; _ = wrapped
	_ = preCommit; _ = tracker; _ = runStart
	return New(code, phase, message, wrapped)
}
```

- [ ] **Step 4: Build**

Run: `go build ./internal/agent/...`
Expected: exit 0.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/run.go
git commit -m "feat(agent): Run loop phase with StreamWithTools integration"
```

---

### Task C2.20: Orchestrator — shutdown phases (success + failure)

**Files:**
- Modify: `internal/agent/run.go` — replace the `finaliseSuccess` and `finaliseFailureV2` stubs with real implementations, delete the old `finaliseFailure` stub.

- [ ] **Step 1: Delete the old `finaliseFailure` stub**

Remove the placeholder from Task C2.18.

- [ ] **Step 2: Replace `finaliseSuccess` with the real implementation**

```go
func finaliseSuccess(
	ctx context.Context,
	deps *RunDependencies,
	repo *gitrepo.Repo,
	run *models.AppAgentRun,
	emitter EventEmitter,
	fileMap *SafeFileMap,
	narration string,
	toolCalls []ChatMessageToolCall,
	tracker *utils.TokenTracker,
	runStart time.Time,
	totalToolCalls int,
	turns int,
	userMessage string,
	user *models.User,
	project *models.AppProject,
) error {
	// 1. Write fileMap to working tree and commit.
	finalCommit, err := FinaliseRun(repo, fileMap, userMessage)
	if err != nil {
		return finaliseFailureV2(ctx, deps, repo, run, emitter, "commit",
			CodeGitError, "failed to write and commit files", err,
			run.PreCommit, tracker, runStart)
	}

	// 2. Persist the assistant chat message.
	toolCallsJSON, _ := json.Marshal(toolCalls)
	assistantMsg := &models.AppChatMessage{
		ProjectID: project.ID,
		RunID:     &run.ID,
		Role:      "assistant",
		Content:   narration,
		Images:    []byte("[]"),
		ToolCalls: toolCallsJSON,
	}
	if _, insErr := deps.DB.InsertChatMessage(assistantMsg); insErr != nil {
		slog.Warn("failed to persist assistant chat message", "error", insErr, "run_id", run.ID)
	}

	// 3. Finalise the AppAgentRun row.
	promptTok, completionTok, totalTok, callCount := tracker.Totals()
	_ = callCount
	durationMs := int(time.Since(runStart).Milliseconds())
	credits := tokensToCredits(promptTok, completionTok)
	costUSD := estimateCost(run.Model, promptTok, completionTok)

	run.Status = models.AgentRunStatusCompleted
	run.FinalCommit = finalCommit
	run.Turns = turns
	run.ToolCallCount = totalToolCalls
	run.PromptTokens = promptTok
	run.CompletionTokens = completionTok
	run.CostUSD = costUSD
	run.CreditsUsed = credits
	run.DurationMs = durationMs
	run.CompletedAt = time.Now()
	if err := deps.DB.UpdateAgentRun(run); err != nil {
		slog.Warn("failed to finalise agent run row", "error", err, "run_id", run.ID)
	}

	// 4. Deduct credits.
	if deps.DeductCredits != nil && credits > 0 {
		action := "app_chat"
		if project.Status == "draft" {
			action = "generate_app"
		}
		if err := deps.DeductCredits(ctx, credits, action, project.ID.String()); err != nil {
			slog.Warn("failed to deduct credits", "error", err, "credits", credits)
		}
	}

	// 5. Emit `done`.
	emitter.Emit(EventDone, DoneEvent{
		RunID:        run.ID,
		FinalCommit:  finalCommit,
		Turns:        turns,
		ToolCalls:    totalToolCalls,
		FilesChanged: countFileOps(toolCalls),
		Tokens:       TokenUsage{Input: promptTok, Output: completionTok},
		CreditsUsed:  credits,
		DurationMs:   durationMs,
	})

	// 6. Track PostHog.
	if deps.TrackUserEvent != nil {
		action := "app_chat_completed"
		if project.Status == "draft" {
			action = "generate_app_completed"
		}
		deps.TrackUserEvent(action, map[string]any{
			"app_id":            project.ID.String(),
			"duration_seconds":  float64(durationMs) / 1000.0,
			"total_tokens":      totalTok,
			"prompt_tokens":     promptTok,
			"completion_tokens": completionTok,
			"credits_used":      credits,
			"turns":             turns,
			"tool_calls":        totalToolCalls,
			"model":             run.Model,
		})
	}

	return nil
}

func countFileOps(calls []ChatMessageToolCall) int {
	n := 0
	for _, c := range calls {
		if c.IsError {
			continue
		}
		switch c.Name {
		case "write_file", "edit_file", "delete_file":
			n++
		}
	}
	return n
}

// tokensToCredits mirrors the existing Controller.TokensToCredits helper.
// Until the rescue phase moves that helper, duplicate the formula here.
// 1 credit ≈ 1000 input tokens OR 250 output tokens (Sonnet approximate).
func tokensToCredits(input, output int) int {
	cr := input/1000 + output/250
	if cr < 1 {
		return 1
	}
	return cr
}

// estimateCost mirrors utils.EstimateInputOutputCostUSD for Sonnet.
func estimateCost(model string, input, output int) float64 {
	in, out := utils.EstimateInputOutputCostUSD(model, input, output, 0, 0)
	return in + out
}
```

- [ ] **Step 3: Replace `finaliseFailureV2` with the real implementation**

```go
func finaliseFailureV2(
	ctx context.Context,
	deps *RunDependencies,
	repo *gitrepo.Repo,
	run *models.AppAgentRun,
	emitter EventEmitter,
	phase string,
	code ErrorCode,
	message string,
	wrapped error,
	preCommit string,
	tracker *utils.TokenTracker,
	runStart time.Time,
) error {
	// 1. Defensive git reset (usually a no-op — tools only touched fileMap).
	filesReverted := 0
	if preCommit != "" && repo != nil {
		if err := RollbackTo(repo, preCommit); err != nil {
			slog.Warn("rollback reset failed", "error", err, "run_id", run.ID)
		} else {
			filesReverted = 1 // we don't count individual files here
		}
	}

	// 2. Emit rollback event.
	emitter.Emit(EventRollback, RollbackEvent{
		RunID:         run.ID,
		PreCommit:     preCommit,
		Reason:        string(code),
		FilesReverted: filesReverted,
	})

	// 3. Emit error event.
	emitter.Emit(EventError, ErrorEvent{
		RunID:   run.ID,
		Phase:   phase,
		Message: message,
		Code:    code,
	})

	// 4. Persist "sorry" assistant message so the chat UI isn't orphaned.
	orphanMsg := &models.AppChatMessage{
		ProjectID: run.ProjectID,
		RunID:     &run.ID,
		Role:      "assistant",
		Content:   "Sorry, that turn failed — your files are unchanged. Please try again.",
		Images:    []byte("[]"),
		ToolCalls: []byte("[]"),
	}
	if _, err := deps.DB.InsertChatMessage(orphanMsg); err != nil {
		slog.Warn("failed to persist failure message", "error", err, "run_id", run.ID)
	}

	// 5. Finalise the run row (no credit deduction).
	promptTok, completionTok, _, _ := tracker.Totals()
	run.Status = mapCodeToStatus(code)
	run.Error = fmt.Sprintf("%s: %s", code, message)
	run.PromptTokens = promptTok
	run.CompletionTokens = completionTok
	run.DurationMs = int(time.Since(runStart).Milliseconds())
	run.CompletedAt = time.Now()
	if err := deps.DB.UpdateAgentRun(run); err != nil {
		slog.Warn("failed to finalise failed agent run row", "error", err, "run_id", run.ID)
	}

	// 6. Track PostHog.
	if deps.TrackUserEvent != nil {
		action := "app_chat_failed"
		if run.Model != "" && run.Status == models.AgentRunStatusFailed {
			action = "app_chat_failed"
		}
		deps.TrackUserEvent(action, map[string]any{
			"app_id":   run.ProjectID.String(),
			"code":     string(code),
			"phase":    phase,
			"duration": run.DurationMs,
		})
	}

	return New(code, phase, message, wrapped)
}

func mapCodeToStatus(code ErrorCode) string {
	switch code {
	case CodeBudgetExceeded:
		return models.AgentRunStatusBudgetExceeded
	case CodeCreditsExhausted:
		return models.AgentRunStatusCanceled
	default:
		return models.AgentRunStatusFailed
	}
}
```

- [ ] **Step 4: Update the panic defer in `Run()` to pass the real repo type**

In the `defer func()` panic handler inside `Run()`, replace the `finaliseFailure` call with `finaliseFailureV2`, passing `repo` (the `*gitrepo.Repo` from step 2) instead of the interface stub. Also update the interface-typed parameter in the function signatures — `finaliseSuccess` and `finaliseFailureV2` now take `*gitrepo.Repo` directly, not `interface{ Reset(string) error }`.

Add the import:

```go
import (
	// ...
	"github.com/knid/wemob/internal/services/gitrepo"
)
```

- [ ] **Step 5: Build**

Run: `go build ./internal/agent/...`
Expected: exit 0.

- [ ] **Step 6: Commit**

```bash
git add internal/agent/run.go
git commit -m "feat(agent): Run shutdown phases — success commit + failure rollback"
```

---

### Task C2.21: Extend `anthropic.go` to cache all three system blocks

**Files:**
- Modify: `internal/utils/anthropic.go`

The current `convertToAnthropicRequest` sets `cache_control` only on the first and last system blocks. The new agent sends three system blocks (base, addendum, file tree) and we want each one cached as its own breakpoint.

- [ ] **Step 1: Locate the block-cache logic**

Run: `grep -n "CacheControl = &AnthropicCacheControl" internal/utils/anthropic.go`

- [ ] **Step 2: Edit to mark all system blocks (up to 4, Anthropic's max)**

Replace the block that sets cache on first + last with:

```go
// Mark up to 4 system blocks with cache_control (Anthropic's max breakpoints).
// Empty or very short blocks don't earn a breakpoint — caching them wastes
// one of our four slots.
if !req.DisablePromptCache {
	cacheBudget := 4
	for i := range systemBlocks {
		if cacheBudget == 0 {
			break
		}
		if len(systemBlocks[i].Text) < 500 {
			continue
		}
		systemBlocks[i].CacheControl = &AnthropicCacheControl{Type: "ephemeral"}
		cacheBudget--
	}
}
```

Remove the previous "first + last" lines.

- [ ] **Step 3: Build**

Run: `go build ./internal/utils/...`
Expected: exit 0.

- [ ] **Step 4: Commit**

```bash
git add internal/utils/anthropic.go
git commit -m "feat(llm): cache up to 4 system blocks for agent multi-block prompts"
```

---

## Phase C2 complete — checkpoint

After this phase: `go build ./internal/agent/... ./internal/utils/... ./internal/models/... ./internal/database/...` all pass. The `internal/agent/` package compiles but is unused. Nothing in `views/` or `controllers/` calls `agent.Run` yet. This is the biggest phase in the plan — consider running the 12-scenario spot check on staging AFTER Phase C3 lands, not during C2.


---

## Phase C3 — Rewire views to call `agent.Run`

After this phase, `ChatApp`, `GenerateAppStream`, `mobile_chat_views`, and the generation jobs view all route through `agent.Run`. The old `Controller.StreamBuildApp` still exists (and still compiles) — nothing calls it. Phase C5 deletes it.

### Task C3.1: Add `Controller.AgentDeps()` constructor

**Files:**
- Modify: `internal/controllers/controller.go`

- [ ] **Step 1: Append `AgentDeps` method**

```go
// AgentDeps builds a per-run RunDependencies bundle for the agent package.
// Credit lifecycle callbacks are bound to the specific user at call time.
func (c *Controller) AgentDeps(user *models.User) *agent.RunDependencies {
	provider := c.llmProvider()
	creditChecker := c.MakeCreditBudgetChecker(user, nil) // tracker bound inside Run
	return &agent.RunDependencies{
		DB:            c.DB,
		GitRepo:       c.GitRepoService,
		AssetsStorage: c.AssetsStorage,
		SecretManager: c.SecretManager,
		NeonService:   c.NeonService,
		LLMProvider:   provider,
		PostHog:       c.PostHog,
		RemoteConfig:  c.RemoteConfig,

		CreditChecker: creditChecker,
		DeductCredits: func(ctx context.Context, credits int, action, projectID string) error {
			return c.DeductAICreditsToFloor(user, credits, action, projectID)
		},
		TrackUserEvent: func(event string, props map[string]any) {
			p := posthog.NewProperties()
			for k, v := range props {
				p.Set(k, v)
			}
			c.TrackUserEvent(user, event, p)
		},
		GenerateImage: func(ctx context.Context, prompt string, w, h int, style, projectID string) (string, error) {
			// Delegate to the rescued image client in Task C4.x. Until then,
			// wrap the old executeChatGenerateImage helper. Concretely: call
			// the image-gen endpoint via the same HTTP client and upload to S3.
			return c.generateImageForAgent(ctx, prompt, w, h, style, projectID)
		},
		NeonConnString: func(projectID string) (string, error) {
			return c.neonConnString(projectID), nil
		},
		UpdateMobileBridgeMethod: func(ctx context.Context, projectID, method string, enabled bool, config map[string]any) error {
			cfg, err := c.DB.GetMobileAppConfig(projectID)
			if err != nil {
				return err
			}
			if cfg.BridgeMethods == nil {
				cfg.BridgeMethods = map[string]any{}
			}
			cfg.BridgeMethods[method] = map[string]any{
				"enabled": enabled,
				"config":  config,
			}
			return c.DB.UpdateMobileAppConfig(cfg)
		},
	}
}
```

Add missing imports at the top:

```go
import (
	// ...
	"context"
	"github.com/knid/wemob/internal/agent"
	"github.com/knid/wemob/internal/models"
)
```

- [ ] **Step 2: Add a stub `generateImageForAgent` method**

Until the image client is rescued in C4, wire it through whatever helper the old `executeChatGenerateImage` used. Minimal stub:

```go
// generateImageForAgent is a temporary shim — replace in Task C4 when the
// image client moves out of app_builder_tools.go.
func (c *Controller) generateImageForAgent(ctx context.Context, prompt string, w, h int, style, projectID string) (string, error) {
	// Call the existing image generation endpoint using SecretManager-provided
	// credentials. The exact implementation is lifted from executeChatGenerateImage
	// in internal/controllers/app_builder_tools.go.
	return "", fmt.Errorf("generateImageForAgent: not yet wired — see Task C4.3")
}
```

- [ ] **Step 3: Build**

Run: `go build ./internal/controllers/... ./internal/agent/...`
Expected: exit 0 (with the stub returning an error — tests/manual verification will catch it in the checklist).

- [ ] **Step 4: Commit**

```bash
git add internal/controllers/controller.go
git commit -m "feat(agent): Controller.AgentDeps() constructor"
```

---

### Task C3.2: Rewire `ChatApp` view to call `agent.Run`

**Files:**
- Modify: `internal/views/app_chat_views.go`

- [ ] **Step 1: Replace `ChatApp`'s body**

Keep all the auth/credit/project checks. Replace the `StreamFunc` closure with an `agent.Run` call:

```go
// POST /v2/apps/{appId}/chat
func (v *HttpViews) ChatApp(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if user == nil {
		utils.JSONErrorResponse(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if requireActiveSubscription(w, user) {
		return
	}
	if v.checkMinimumCredits(w, user) {
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil || project == nil {
		utils.JSONErrorResponse(w, "project not found", http.StatusNotFound)
		return
	}

	var req requests.AppChatRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	if r.Context().Err() != nil {
		return
	}

	// Track the chat event.
	v.Controller.TrackUserEvent(user, "app_chat_sent", posthog.NewProperties().
		Set("app_id", project.ID.String()).
		Set("project_type", project.ProjectType).
		Set("has_images", len(req.Images) > 0))

	// Stream via the new agent.
	v.handleStreamOrAsync(w, r, user, StreamConfig{
		JobType: "app_chat",
		AppID:   project.ID,
		StreamFunc: func(ctx context.Context, events chan<- responses.SSEEvent, jobID string) {
			deps := v.Controller.AgentDeps(user)
			emitter := agent.NewSSEEmitterFromChannel(uuid.MustParse(jobID), events, 500)
			_ = agent.Run(ctx, deps, project, user, req.Message, req.Images, emitter)
			emitter.Close()
		},
	})
}
```

- [ ] **Step 2: Add an SSE-event-channel adapter**

The view passes `chan<- responses.SSEEvent` (the existing SSE stream channel type). The agent emitter needs to publish its typed `agent.Event` into that channel. Add a thin adapter in `internal/agent/events.go`:

```go
// NewSSEEmitterFromChannel adapts an agent.EventEmitter onto an existing
// responses.SSEEvent channel. Each agent.Event becomes one SSEEvent frame.
func NewSSEEmitterFromChannel(runID uuid.UUID, ch chan<- responses.SSEEvent, ringCap int) EventEmitter {
	bridge := make(chan Event, 64)
	emitter := NewSSEEmitter(runID, bridge, ringCap)
	go func() {
		for ev := range bridge {
			ch <- responses.SSEEvent{Event: ev.Type, Data: ev.Data}
		}
		close(ch)
	}()
	return emitter
}
```

Add the import in `events.go`:

```go
import (
	// ...
	"github.com/knid/wemob/internal/responses"
)
```

- [ ] **Step 3: Build**

Run: `go build ./internal/views/... ./internal/agent/...`
Expected: exit 0.

- [ ] **Step 4: Commit**

```bash
git add internal/views/app_chat_views.go internal/agent/events.go
git commit -m "feat(agent): rewire ChatApp view to call agent.Run"
```

---

### Task C3.3: Rewire `GenerateAppStream` view

**Files:**
- Modify: `internal/views/app_generate_stream_views.go`

- [ ] **Step 1: Replace the body**

```go
// POST /v2/apps/generate/stream
func (v *HttpViews) GenerateAppStream(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if requireActiveSubscription(w, user) {
		return
	}
	if v.checkMinimumCredits(w, user) {
		return
	}

	var req requests.GenerateAppRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Reject concurrent runs.
	activeJobs, _ := v.Controller.GetActiveJobs(user.ID)
	for _, j := range activeJobs {
		if j.Type == "generate_app" {
			utils.JSONErrorResponse(w, "a generation is already in progress", http.StatusTooManyRequests)
			return
		}
	}

	project, err := v.Controller.CreateAppProject(r.Context(), user, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	v.Controller.TrackUserEvent(user, "app_generation_started", posthog.NewProperties().
		Set("project_type", req.ProjectType).
		Set("app_id", project.ID.String()))

	v.handleStreamOrAsync(w, r, user, StreamConfig{
		JobType: "generate_app",
		AppID:   project.ID,
		StreamFunc: func(ctx context.Context, events chan<- responses.SSEEvent, jobID string) {
			deps := v.Controller.AgentDeps(user)
			emitter := agent.NewSSEEmitterFromChannel(uuid.MustParse(jobID), events, 500)
			_ = agent.Run(ctx, deps, project, user, req.Prompt, req.Images, emitter)
			emitter.Close()
		},
	})
}
```

- [ ] **Step 2: Build**

Run: `go build ./internal/views/...`
Expected: exit 0.

- [ ] **Step 3: Commit**

```bash
git add internal/views/app_generate_stream_views.go
git commit -m "feat(agent): rewire GenerateAppStream view to call agent.Run"
```

---

### Task C3.4: Rewire mobile chat view

**Files:**
- Modify: `internal/views/mobile_chat_views.go`

- [ ] **Step 1: Inspect the existing handler**

Run: `grep -n "^func (v \*HttpViews)" internal/views/mobile_chat_views.go`

There is typically one `MobileChatApp` handler with similar flow to `ChatApp`. Replace its `StreamFunc` closure with the `agent.Run` call — identical structure to Task C3.2.

- [ ] **Step 2: Build**

Run: `go build ./internal/views/...`
Expected: exit 0.

- [ ] **Step 3: Commit**

```bash
git add internal/views/mobile_chat_views.go
git commit -m "feat(agent): rewire mobile chat view to call agent.Run"
```

---

### Task C3.5: Rewrite generation jobs view to use `AppAgentRun`

**Files:**
- Modify: `internal/views/generation_jobs_view.go`

The old file has `StreamJob`, `ClarifyJob`, and `ReplyJob`. The clarification flow goes away entirely. `StreamJob` becomes `StreamRun`, backed by `AppAgentRun` lookups and the agent's ring buffer.

- [ ] **Step 1: Delete `ClarifyJob` and `ReplyJob` handlers**

These are the endpoints for the conversation clarification loop. Delete them outright. Also delete any helper methods they call (`v.Controller.PendingClarifications`, `ClarificationResult`, etc.). Mark the helpers for deletion in Phase C5 if they're not otherwise referenced.

- [ ] **Step 2: Rewrite `StreamJob` as `StreamRun`**

```go
// GET /v2/apps/{appId}/runs/{runId}/stream
// Reconnects to a running agent run. Replays the buffer, then tees live events.
func (v *HttpViews) StreamRun(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil || user == nil {
		utils.JSONErrorResponse(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil || project == nil {
		utils.JSONErrorResponse(w, "project not found", http.StatusNotFound)
		return
	}

	runIDStr := chi.URLParam(r, "runId")
	runID, err := uuid.Parse(runIDStr)
	if err != nil {
		utils.JSONErrorResponse(w, "invalid run id", http.StatusBadRequest)
		return
	}

	run, err := v.Controller.DB.GetAgentRun(runID)
	if err != nil || run == nil || run.ProjectID != project.ID {
		utils.JSONErrorResponse(w, "run not found", http.StatusNotFound)
		return
	}

	// If the run already completed, return the final state as JSON.
	if run.Status != models.AgentRunStatusRunning {
		utils.JSONResponse(w, map[string]any{
			"run_id":       run.ID,
			"status":       run.Status,
			"final_commit": run.FinalCommit,
			"turns":        run.Turns,
			"tool_calls":   run.ToolCallCount,
		}, http.StatusOK)
		return
	}

	// Live run — fetch the per-run event bus from Controller.JobEventBuses
	// (same sync.Map the old StreamJob used). Replay the buffer, then tee.
	bus, ok := v.Controller.JobEventBuses.Load(runID.String())
	if !ok {
		utils.JSONErrorResponse(w, "run event bus not found (server may have restarted)", http.StatusGone)
		return
	}
	eventBus := bus.(*controllers.EventBus)

	// Set SSE headers + stream.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, _ := w.(http.Flusher)

	sub := eventBus.Subscribe()
	defer eventBus.Unsubscribe(sub)

	// Replay buffer first.
	for _, ev := range eventBus.Replay() {
		writeSSE(w, ev)
		if flusher != nil {
			flusher.Flush()
		}
	}

	// Live tee.
	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-sub:
			if !ok {
				return
			}
			writeSSE(w, ev)
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
}

func writeSSE(w http.ResponseWriter, ev responses.SSEEvent) {
	b, _ := json.Marshal(ev.Data)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Event, b)
}
```

- [ ] **Step 3: Add `GetAppActiveRun` (replaces `GetAppActiveJob`)**

```go
// GET /v2/apps/{appId}/active-run
func (v *HttpViews) GetAppActiveRun(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil || user == nil {
		utils.JSONErrorResponse(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil || project == nil {
		utils.JSONErrorResponse(w, "project not found", http.StatusNotFound)
		return
	}
	run, err := v.Controller.DB.GetActiveAgentRun(project.ID)
	if err != nil || run == nil {
		utils.JSONResponse(w, map[string]any{"run_id": nil}, http.StatusOK)
		return
	}
	utils.JSONResponse(w, map[string]any{
		"run_id": run.ID,
		"status": run.Status,
	}, http.StatusOK)
}
```

- [ ] **Step 4: Note: `EventBus` requires a minor extension**

The current `controllers.EventBus` needs a `Replay()` method to return buffered events for late subscribers. If it doesn't have one, add it:

```go
// internal/controllers/event_bus.go — append
func (b *EventBus) Replay() []responses.SSEEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]responses.SSEEvent, len(b.buffer))
	copy(out, b.buffer)
	return out
}
```

If `EventBus` doesn't already have a `buffer` field, add one with a ring-buffer append in the existing `Publish` method. Refer to `internal/controllers/event_bus.go` for the current shape.

- [ ] **Step 5: Build**

Run: `go build ./internal/views/... ./internal/controllers/...`
Expected: exit 0.

- [ ] **Step 6: Commit**

```bash
git add internal/views/generation_jobs_view.go internal/controllers/event_bus.go
git commit -m "feat(agent): StreamRun/GetAppActiveRun views, delete clarification handlers"
```

---

### Task C3.6: Simplify `generation_stream_state.go`

**Files:**
- Modify: `internal/views/generation_stream_state.go`

The old file is 312 LOC of conversation-job state machine. Post-rewrite, it shrinks to ~100 LOC of SSE plumbing around `agent.Run`.

- [ ] **Step 1: Audit what's still used**

Run: `grep -rn "handleStreamOrAsync\|StreamConfig" internal/views/`

Identify the minimal surface still needed: the `StreamConfig` struct and `handleStreamOrAsync` helper. Everything else (clarification state, job-recovery maps, pending-clarification routes) is deleted.

- [ ] **Step 2: Replace the file with the slimmed-down version**

```go
package views

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

// StreamConfig carries what handleStreamOrAsync needs to spin up a stream.
type StreamConfig struct {
	JobType    string
	AppID      uuid.UUID
	StreamFunc func(ctx context.Context, events chan<- responses.SSEEvent, jobID string)
}

// handleStreamOrAsync starts a stream in foreground mode: writes SSE headers,
// spawns StreamFunc in a goroutine with a new EventBus, publishes events to
// the HTTP response as they arrive. The EventBus is stored on
// Controller.JobEventBuses so reconnect can find it.
func (v *HttpViews) handleStreamOrAsync(w http.ResponseWriter, r *http.Request, user *models.User, cfg StreamConfig) {
	runID := uuid.New()
	bus := controllers.NewEventBus(500)
	v.Controller.JobEventBuses.Store(runID.String(), bus)
	defer v.Controller.JobEventBuses.Delete(runID.String())

	// Wire the SSE response.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, _ := w.(http.Flusher)

	events := make(chan responses.SSEEvent, 64)
	go func() {
		defer close(events)
		cfg.StreamFunc(r.Context(), events, runID.String())
	}()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			bus.Publish(ev)
			writeSSEEvent(w, ev)
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
}

func writeSSEEvent(w http.ResponseWriter, ev responses.SSEEvent) {
	b, _ := json.Marshal(ev.Data)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Event, b)
}

// Shared auth gates used by generate/chat views.

func requireActiveSubscription(w http.ResponseWriter, user *models.User) bool {
	if user == nil || (user.Subscription != "active" && user.Subscription != "trial" && user.Subscription != "free") {
		utils.JSONErrorResponse(w, "subscription required", http.StatusPaymentRequired)
		return true
	}
	return false
}

func (v *HttpViews) checkMinimumCredits(w http.ResponseWriter, user *models.User) bool {
	// Same min-credits check as before — delegate to the Controller helper.
	if !v.Controller.UserHasMinimumCredits(user) {
		utils.JSONErrorResponse(w, "insufficient credits", http.StatusPaymentRequired)
		return true
	}
	return false
}
```

Remove all the old clarification-state helpers and job-recovery logic.

- [ ] **Step 3: Build**

Run: `go build ./internal/views/...`
Expected: exit 0.

- [ ] **Step 4: Commit**

```bash
git add internal/views/generation_stream_state.go
git commit -m "refactor(agent): slim generation_stream_state.go to SSE plumbing"
```

---

### Task C3.7: Update routes in `cmd/main.go`

**Files:**
- Modify: `cmd/main.go`

- [ ] **Step 1: Find the existing routes**

Run: `grep -n "ClarifyJob\|ReplyJob\|StreamJob\|GetAppActiveJob" cmd/main.go`

- [ ] **Step 2: Replace the clarification + job routes**

Remove:
```go
r.Post("/jobs/{jobId}/clarify", httpViews.ClarifyJob)
r.Post("/jobs/{jobId}/reply", httpViews.ReplyJob)
r.Get("/jobs/{jobId}/stream", httpViews.StreamJob)
```

Add (replace `{appId}` scope):
```go
r.Get("/active-run", httpViews.GetAppActiveRun)
r.Get("/runs/{runId}/stream", httpViews.StreamRun)
r.Get("/runs/{runId}", httpViews.GetRun)
```

The `/runs/{runId}` GET endpoint fetches the final state of a run. Add a simple handler in `generation_jobs_view.go`:

```go
// GET /v2/apps/{appId}/runs/{runId}
func (v *HttpViews) GetRun(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil || user == nil {
		utils.JSONErrorResponse(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil || project == nil {
		utils.JSONErrorResponse(w, "project not found", http.StatusNotFound)
		return
	}
	runIDStr := chi.URLParam(r, "runId")
	runID, err := uuid.Parse(runIDStr)
	if err != nil {
		utils.JSONErrorResponse(w, "invalid run id", http.StatusBadRequest)
		return
	}
	run, err := v.Controller.DB.GetAgentRun(runID)
	if err != nil || run == nil || run.ProjectID != project.ID {
		utils.JSONErrorResponse(w, "run not found", http.StatusNotFound)
		return
	}
	utils.JSONResponse(w, run, http.StatusOK)
}
```

- [ ] **Step 3: Keep `GetAppActiveJob` as an alias for `GetAppActiveRun`**

For one release, keep the `GET /active-job` route pointing at `GetAppActiveRun` so clients with stale URLs still work. Delete the alias in the final deletion commit (C5).

- [ ] **Step 4: Build**

Run: `go build ./...`
Expected: exit 0.

- [ ] **Step 5: Commit**

```bash
git add cmd/main.go internal/views/generation_jobs_view.go
git commit -m "feat(agent): replace clarification routes with /runs and /active-run"
```

---

## Phase C3 complete — checkpoint

After this phase: every chat/generate entry point routes through `agent.Run`. The old controllers still exist and compile but are unreachable from HTTP. Manual scenario verification (12 scenarios from spec §10) is possible at this point if the team wants to smoke-test the new path before deleting the old one. If anything breaks, revert is one commit.


---

## Phase C4 — Rescue helpers out of the deletion zone

`app_codegen_controller.go` (3802 LOC) contains helpers that non-AI code still needs (theme CSS builders, template content, SQL generation, Neon provisioning, project classification). Move them to their proper homes before deleting the file. Each task in this phase is a move+verify cycle.

General approach:
1. Create the new file and copy the helpers into it (package-qualified).
2. Add an export for the name if it needs to be public to a new caller.
3. Update callers that currently reach into `controllers.*helperName` → new path.
4. Verify `go build ./...` passes.
5. Commit.

### Task C4.1: Move `classifyProjectType`, `looksLikeEcommerce`, `fontWeightParam`, and `ALL_BRIDGE_METHODS` to new homes

**Files:**
- Create: `internal/controllers/project_classify.go`
- Create: `internal/constants/mobile_bridge.go`
- Modify: `internal/controllers/app_codegen_controller.go` — remove the moved functions
- Modify: `internal/constants/ai_builder.go` — remove `ALL_BRIDGE_METHODS`

- [ ] **Step 1: Create `project_classify.go`**

Copy `classifyProjectType`, `looksLikeEcommerce`, and `fontWeightParam` verbatim from `app_codegen_controller.go` (lines 457, 493, 285) into the new file, preserving the `controllers` package declaration and imports they need. Any helpers they depend on go with them.

```go
package controllers

// (copy the three functions verbatim here, adjusting imports as needed)
```

- [ ] **Step 2: Create `constants/mobile_bridge.go`**

```go
package constants

// ALL_BRIDGE_METHODS enumerates every native capability the mobile bridge
// can expose to webview JS. Moved out of ai_builder.go during the agent
// rewrite so the agent's configure_mobile_bridge tool can reference it
// without depending on the old prompt constants.
var ALL_BRIDGE_METHODS = []string{
	// (copy the exact slice contents from ai_builder.go:672)
}
```

- [ ] **Step 3: Delete the originals**

Remove the copied functions from `app_codegen_controller.go` and the `ALL_BRIDGE_METHODS` var from `ai_builder.go`.

- [ ] **Step 4: Fix broken imports / references**

Any remaining code that called `c.classifyProjectType(...)` or `c.looksLikeEcommerce(...)` from `app_codegen_controller.go` now finds the function in the new file — same package, so no import changes. If any external caller refers to `constants.ALL_BRIDGE_METHODS` it keeps working — the identifier is unchanged, only the file it lives in.

- [ ] **Step 5: Build**

Run: `go build ./...`
Expected: exit 0.

- [ ] **Step 6: Commit**

```bash
git add internal/controllers/project_classify.go internal/constants/mobile_bridge.go internal/controllers/app_codegen_controller.go internal/constants/ai_builder.go
git commit -m "refactor(agent): rescue project classify helpers and ALL_BRIDGE_METHODS"
```

---

### Task C4.2: Move template content helpers to `internal/templates/`

**Files:**
- Create: `internal/templates/content.go`
- Create: `internal/templates/utils.go`
- Modify: `internal/controllers/app_codegen_controller.go` — remove moved functions

- [ ] **Step 1: Create `templates/content.go` with the rescued helpers**

Copy these from `app_codegen_controller.go` into the new file, renaming per the agent package's expectations (Task C2.13):

| Old name | New name | Source line |
|---|---|---|
| `getTemplateContent` | `GetContent` | 875 |
| `getTemplateContentWithLang` | `GetContentWithLang` | 879 |
| `getUITemplateContent` | `GetUIContent` | 934 |
| `getEcommerceTemplateContent` | `GetEcommerceContent` | 920 |
| `buildEcommerceProviders` | `BuildEcommerceProviders` | 897 |
| `buildGlobalsCSSTheme` | `BuildGlobalsCSSTheme` | 550 |
| `buildEcommerceGlobalsCSSTheme` | `BuildEcommerceGlobalsCSSTheme` | 685 |
| `buildEcommerceLayout` | `BuildEcommerceLayout` | 773 |

The `templates` package should already exist (per `BaseFilePaths`, `UIFilePaths`, `EcommerceFilePaths` being in it). Verify with `ls internal/templates/`.

All the rescued functions keep their bodies identical — this is pure motion. Change the receiver (they're package-level, not methods) if any are currently `(c *Controller)` methods.

- [ ] **Step 2: Create `templates/utils.go`**

Copy these helpers:
- `isTemplateFile` → `IsTemplate`
- `isProtectedTemplateFile` → `IsProtected`
- `isConfigFile` → `IsConfig`
- `isPascalCase` → `IsPascalCase`
- `toCamelCase` → `ToCamelCase`
- `augmentPackageJSON` → `AugmentPackageJSON`
- `getColor` → `GetColor`
- `fontWeightParam` → `FontWeightParam` (already moved in C4.1 to `project_classify.go` — reconsider placement; it fits better here with templates)

- [ ] **Step 3: Delete the originals from `app_codegen_controller.go`**

- [ ] **Step 4: Fix imports and references**

`app_build_controller.go` (which we're NOT deleting) may call some of these. Update those callers to the new `templates.GetContent` / `templates.BuildGlobalsCSSTheme` / etc. paths.

Run: `grep -rn "getTemplateContent\|buildGlobalsCSSTheme\|buildEcommerceProviders" internal/ | grep -v app_codegen_controller`

Update each hit to the new package-qualified name.

- [ ] **Step 5: Build**

Run: `go build ./...`
Expected: exit 0.

- [ ] **Step 6: Commit**

```bash
git add internal/templates/content.go internal/templates/utils.go internal/controllers/app_codegen_controller.go internal/controllers/app_build_controller.go
git commit -m "refactor(agent): move template content helpers to internal/templates"
```

---

### Task C4.3: Move `DataModel`, SQL schema generation, and Neon helpers

**Files:**
- Create: `internal/models/data_model.go`
- Create: `internal/database/schema_gen.go`
- Create: `internal/controllers/neon_controller.go`
- Modify: `internal/controllers/app_codegen_controller.go` — remove moved code

- [ ] **Step 1: Create `models/data_model.go`**

Copy the `DataModel` struct from `app_codegen_controller.go` (search for `type DataModel struct`) plus helpers `parseDataModels`, `marshalDataModels`, `mergeDataModelColumns`. Make them exported: `ParseDataModels`, `MarshalDataModels`, `MergeDataModelColumns`.

```go
package models

import "encoding/json"

type DataModel struct {
	// ... (copy struct fields verbatim)
}

type DataModelColumn struct {
	// ...
}

func ParseDataModels(raw []byte) ([]DataModel, error) {
	// ... (copy body)
}

func MarshalDataModels(ms []DataModel) ([]byte, error) {
	// ...
}

func MergeDataModelColumns(auth, llm DataModel) DataModel {
	// ...
}
```

- [ ] **Step 2: Create `database/schema_gen.go`**

Copy:
- `buildCreateTableSQL` → `BuildCreateTableSQL`
- `buildDrizzleSchema` → `BuildDrizzleSchema`
- `validateSeedSQL` → `ValidateSeedSQL`
- `splitSQLStatements` → `SplitSQLStatements`
- `stripSQLStringLiterals`, `stripLeadingSQLComments`, `fixUnescapedSQLQuotes`, `isRawSQLDefault`, `neonConnStrWithEndpoint`

Lowercase helpers can stay lowercase if only the package uses them.

```go
package database

import "github.com/knid/wemob/internal/models"

func BuildCreateTableSQL(ms []models.DataModel) string {
	// ... (copy, updating DataModel references)
}
```

- [ ] **Step 3: Create `controllers/neon_controller.go`**

Copy:
- `provisionNeonDatabase`
- `storeNeonIntegration`
- `getNeonIntegration`
- `seedNeonDatabase`
- `generateAndStoreSeedImages`
- `neonConnString` (internal helper used by AgentDeps — keep on Controller as a method)

Keep them as `(c *Controller)` methods in the new file since they use Controller dependencies (`c.DB`, `c.NeonService`, `c.AssetsStorage`).

- [ ] **Step 4: Delete originals from `app_codegen_controller.go`**

- [ ] **Step 5: Update callers**

Run: `grep -rn "parseDataModels\|buildCreateTableSQL\|provisionNeonDatabase" internal/ | grep -v neon_controller | grep -v schema_gen | grep -v data_model`

Fix every hit to use the new package-qualified names.

- [ ] **Step 6: Build**

Run: `go build ./...`
Expected: exit 0.

- [ ] **Step 7: Commit**

```bash
git add internal/models/data_model.go internal/database/schema_gen.go internal/controllers/neon_controller.go internal/controllers/app_codegen_controller.go
git commit -m "refactor(agent): rescue DataModel, schema_gen, and Neon helpers"
```

---

### Task C4.4: Move env var detection and image generation helpers

**Files:**
- Create: `internal/controllers/env_detect.go`
- Create: `internal/controllers/image_gen.go`
- Modify: `internal/controllers/app_codegen_controller.go`

- [ ] **Step 1: Create `env_detect.go`**

Copy `detectEnvVarsFromCode`, `computeMissingEnvVars`, and the `RequiredEnvVar` type. Keep them on `Controller` where they need DB access; make pure functions package-level.

- [ ] **Step 2: Create `image_gen.go` with the rescued Fal client**

This is a direct port of `executeChatGenerateImage` at `internal/controllers/app_builder_tools.go:971`, stripped of the SSE/conversation bits that don't belong in a pure helper:

```go
package controllers

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/knid/wemob/internal/services/fal"
	"github.com/knid/wemob/internal/utils"
)

// generateImageForAgent generates an image via Fal and uploads it to S3.
// Returns the permanent public URL the model can embed in JSX.
// Ported from executeChatGenerateImage in app_builder_tools.go.
func (c *Controller) generateImageForAgent(ctx context.Context, prompt string, w, h int, style, projectID string) (string, error) {
	apiKey := c.SecretManager.GetSecret("FAL_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("FAL_API_KEY not configured")
	}
	falModel := c.SecretManager.GetSecret("FAL_IMAGE_MODEL")
	if falModel == "" {
		falModel = "fal-ai/flux/schnell"
	}

	cdnURL, err := fal.GenerateImage(ctx, apiKey, falModel, prompt, "")
	if err != nil {
		return "", fmt.Errorf("fal generate: %w", err)
	}

	imgData, err := utils.DownloadImage(ctx, cdnURL)
	if err != nil {
		// Fall back to the temporary CDN URL. It will eventually 404 but works
		// for the current session — better than nothing.
		slog.Warn("failed to download generated image, returning CDN URL", "error", err, "project_id", projectID)
		return cdnURL, nil
	}

	// Filename derived from timestamp to avoid collisions.
	filename := fmt.Sprintf("generated-%d.jpg", time.Now().UnixNano())
	s3Key := fmt.Sprintf("apps/%s/images/%s", projectID, filename)

	// Detached context for the S3 upload so the image isn't lost if the
	// parent tool-execution context is near its deadline.
	uploadCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var publicURL string
	var uploadErr error
	for attempt := 0; attempt < 3; attempt++ {
		publicURL, uploadErr = c.AssetsStorage.UploadFileBytePublic(uploadCtx, s3Key, imgData, "image/jpeg")
		if uploadErr == nil {
			break
		}
		slog.Warn("S3 upload failed for generated image, retrying",
			"project_id", projectID, "s3_key", s3Key,
			"attempt", attempt+1, "error", uploadErr)
		time.Sleep(time.Duration(attempt+1) * time.Second)
	}
	if uploadErr != nil {
		return cdnURL, nil // fall back to CDN URL
	}
	return publicURL, nil
}
```

- [ ] **Step 2a: Ensure `utils.DownloadImage` exists**

The old tool used a private helper `downloadImage(ctx, cdnURL)`. Promote it to `utils.DownloadImage`:

```bash
grep -n "func downloadImage" internal/controllers/
```

Move the function to `internal/utils/image.go` (if an image helper file exists) or create a new `internal/utils/image_download.go`. Make it exported.

- [ ] **Step 3: Delete originals**

Remove the env-detect helpers and `executeChatGenerateImage` entry from `app_codegen_controller.go` / `app_builder_tools.go` respectively. Task C5 handles the bulk deletion of those files; here we just pull the live parts forward.

- [ ] **Step 4: Build**

Run: `go build ./...`
Expected: exit 0.

- [ ] **Step 5: Commit**

```bash
git add internal/controllers/env_detect.go internal/controllers/image_gen.go internal/controllers/app_codegen_controller.go internal/controllers/app_builder_tools.go
git commit -m "refactor(agent): rescue env_detect and image_gen helpers"
```

---

## Phase C4 complete — checkpoint

All helpers that non-AI code still needs now live in their new homes. `go build ./...` passes. The old `app_*.go` AI files are fully orphan-able — no non-deleted file references them. Safe to proceed to C5.

---

## Phase C5 — Delete the old system

Pure deletion phase. Every commit removes files (or dead code within files); nothing adds behaviour. Each commit must end with `go build ./...` clean.

### Task C5.1: Delete the six `app_builder_*.go` files

**Files:**
- Delete: `internal/controllers/app_builder_controller.go`
- Delete: `internal/controllers/app_builder_context.go`
- Delete: `internal/controllers/app_builder_conversation.go`
- Delete: `internal/controllers/app_builder_generation.go`
- Delete: `internal/controllers/app_builder_tools.go`
- Delete: `internal/controllers/app_builder_validation.go`

- [ ] **Step 1: Sweep for remaining references**

```bash
grep -rn "StreamBuildApp\|streamSingleShotGeneration\|streamEcommerceGeneration\|buildUnifiedChatTools\|executeChat\|FileStreamParser\|runConversationLoop\|classifyPrompt\|runPreGenerationSetup\|runPreGenWebSearch\|buildFileListContext\|selectContextFiles\|isErrorReport\|stopShortSentinel\|leakedXMLRegex\|sanitizeLLMLeakedXML" internal/ cmd/
```

Every hit must be in one of the files being deleted in this commit. If a hit is OUTSIDE those files, stop and fix the caller first (it should have been rewired in C3 or rescued in C4).

- [ ] **Step 2: Delete the files**

```bash
git rm internal/controllers/app_builder_controller.go \
       internal/controllers/app_builder_context.go \
       internal/controllers/app_builder_conversation.go \
       internal/controllers/app_builder_generation.go \
       internal/controllers/app_builder_tools.go \
       internal/controllers/app_builder_validation.go
```

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: exit 0. If not, the sweep in Step 1 missed something — fix the remaining caller.

- [ ] **Step 4: Commit**

```bash
git commit -m "refactor(agent): delete six app_builder_*.go files (−7996 LOC)"
```

---

### Task C5.2: Delete `app_codegen_controller.go`

**Files:**
- Delete: `internal/controllers/app_codegen_controller.go`

- [ ] **Step 1: Sweep for remaining references**

```bash
grep -rn "PlanAppFileStructure\|GenerateClarificationQuestions\|ApplyStructureToProject\|ClassifyMobileIntent\|GenerateURLWrapperWelcome\|runDeterministicChecks\|autoFix\|splitLayoutIntoServerAndClient\|extractInnerJSXContent\|findClosingBrace\|stripForbiddenImports\|resolveFileContent\|repairTruncatedStructureJSON" internal/ cmd/
```

Every hit must be inside `app_codegen_controller.go` itself. If anything survived rescue, move it now (or accept that it dies with the delete).

- [ ] **Step 2: Delete the file**

```bash
git rm internal/controllers/app_codegen_controller.go
```

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: exit 0. Any failure means C4 rescues missed something — roll back this commit, fix C4, retry.

- [ ] **Step 4: Commit**

```bash
git commit -m "refactor(agent): delete app_codegen_controller.go (−3802 LOC)"
```

---

### Task C5.3: Delete the `constants/ai_*.go` prompt files

**Files:**
- Delete: `internal/constants/ai_builder.go`
- Delete: `internal/constants/ai_conversation.go`
- Delete: `internal/constants/ai_structure.go`
- Delete: `internal/constants/ai_templates.go`
- Delete: `internal/constants/ai_ecommerce.go`
- Delete: `internal/constants/ai_ecommerce_i18n.go`
- Delete: `internal/constants/ai_validation.go`

Keep `internal/constants/ai_gateway.go` and `internal/constants/ai_dashboard.go` — they're used by non-agent features (billing, dashboard chat).

- [ ] **Step 1: Sweep**

```bash
grep -rn "NEXTJS_BUILDER_\|NEXTJS_ECOMMERCE_\|builderRole\|builderDesignPhilosophy\|builderFrameworkRules\|builderImageRules\|builderLanguageRules\|builderToneRules\|chatEditRole\|NEXTJS_MOBILE_OPTIMIZE_PROMPT\|NEXTJS_VALIDATE_PROMPT\|NEXTJS_BUILDER_SYSTEM_PROMPT\|NEXTJS_ECOMMERCE_SETUP_INSTRUCTIONS" internal/ cmd/
```

All hits should be inside the files being deleted. Task C4.1 already moved `ALL_BRIDGE_METHODS` out of `ai_builder.go` — verify that delete is clean.

- [ ] **Step 2: Delete**

```bash
git rm internal/constants/ai_builder.go \
       internal/constants/ai_conversation.go \
       internal/constants/ai_structure.go \
       internal/constants/ai_templates.go \
       internal/constants/ai_ecommerce.go \
       internal/constants/ai_ecommerce_i18n.go \
       internal/constants/ai_validation.go
```

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: exit 0.

- [ ] **Step 4: Commit**

```bash
git commit -m "refactor(agent): delete legacy ai_*.go prompt constants (−2877 LOC)"
```

---

### Task C5.4: Delete `utils/file_parser.go` and `utils/apply_edit.go`

**Files:**
- Delete: `internal/utils/file_parser.go`
- Delete: `internal/utils/apply_edit.go`

- [ ] **Step 1: Sweep**

```bash
grep -rn "FileStreamParser\|apply_edit\|ApplyEdit" internal/ cmd/
```

Should only find the files themselves.

- [ ] **Step 2: Delete**

```bash
git rm internal/utils/file_parser.go internal/utils/apply_edit.go
```

- [ ] **Step 3: Build + commit**

```bash
go build ./... && git commit -m "refactor(agent): delete file_parser.go and apply_edit.go (−722 LOC)"
```

---

### Task C5.5: Trim dead code in `utils/ai.go`

**Files:**
- Modify: `internal/utils/ai.go`

Several entry points become unused after the rewrite. Delete only the ones no longer called.

- [ ] **Step 1: Find unused helpers**

Run: `grep -rn "GetJSONFromLLMWithMaxTokens\|GetJSONFromLLMWithVision\|StreamJSONFromLLMWithMessages\|StreamTextFromLLMWithMessages" internal/ cmd/`

For each helper, if every remaining reference is inside `ai.go` itself (self-referential) or zero matches, delete it. If a rescued helper (e.g. something in `neon_controller.go`) still calls one, leave that one alone.

- [ ] **Step 2: Delete the unused functions**

Remove the function definitions, any helper structs they uniquely reference, and any imports that become unused.

- [ ] **Step 3: Build**

Run: `go build ./... && go vet ./...`
Expected: both exit 0.

- [ ] **Step 4: Commit**

```bash
git add internal/utils/ai.go
git commit -m "refactor(agent): trim dead LLM entry points from utils/ai.go"
```

---

### Task C5.6: Delete legacy conversation models

**Files:**
- Delete: `internal/models/app_conversation.go`
- Delete: `internal/database/app_conversations.go`
- Modify: `cmd/main.go` — remove the `AppConversation` entry from `createSchema()`
- Modify: any view that still references `GetAppConversation` / `ClearAppConversation` — repoint to the new `AppChatMessage` helpers

- [ ] **Step 1: Sweep**

```bash
grep -rn "AppConversation\|AppConversationMessage\|AppChatFileOp\|AppChatToolCall\b" internal/ cmd/
```

- [ ] **Step 2: Repoint `GetAppConversation` / `ClearAppConversation` view handlers**

The existing routes `GET /v2/apps/{appId}/conversation` and `DELETE /v2/apps/{appId}/conversation` must still work (dashboard depends on them) — repoint their handlers to serve `AppChatMessage` rows:

```go
func (v *HttpViews) GetAppConversation(w http.ResponseWriter, r *http.Request) {
	// ... auth ...
	msgs, err := v.Controller.DB.GetRecentChatMessages(project.ID, 100)
	if err != nil {
		utils.JSONErrorResponse(w, "failed to load messages", http.StatusInternalServerError)
		return
	}
	utils.JSONResponse(w, map[string]any{"messages": msgs}, http.StatusOK)
}

func (v *HttpViews) ClearAppConversation(w http.ResponseWriter, r *http.Request) {
	// ... auth ...
	if err := v.Controller.DB.ClearChatMessages(project.ID); err != nil {
		utils.JSONErrorResponse(w, "failed to clear", http.StatusInternalServerError)
		return
	}
	utils.JSONResponse(w, map[string]any{"success": true}, http.StatusOK)
}
```

- [ ] **Step 3: Delete the files and the schema entry**

```bash
git rm internal/models/app_conversation.go internal/database/app_conversations.go
```

Remove from `createSchema()` in `cmd/main.go`:
```go
// DELETE this block:
if _, err := db.DB.NewCreateTable().Model((*models.AppConversation)(nil)).IfNotExists().Exec(ctx); err != nil {
	return fmt.Errorf("create app_conversations: %w", err)
}
```

- [ ] **Step 4: Build**

Run: `go build ./... && go vet ./...`
Expected: exit 0.

- [ ] **Step 5: Commit**

```bash
git add cmd/main.go internal/views/app_chat_views.go
git commit -m "refactor(agent): delete AppConversation model and repoint conversation endpoints"
```

---

### Task C5.7: Write the SQL migration and revert scripts

**Files:**
- Create: `scripts/migrate-to-agent.sql`
- Create: `scripts/revert-to-legacy.sql`

- [ ] **Step 1: Create the forward migration**

```sql
-- scripts/migrate-to-agent.sql
-- One-shot migration for the agent rewrite (2026-04-13).
-- Run manually against production after deploying the backend PR.
-- Dev environment regenerates via createSchema() on first boot.

BEGIN;

-- The new tables are created by createSchema() with IfNotExists(), so this
-- migration focuses on cleaning up legacy state.

-- Drop legacy table: AppConversation (replaced by app_chat_messages).
DROP TABLE IF EXISTS app_conversations;

-- If your prior codebase had a separate app_chat_tool_calls table at any
-- point, drop it here too. In the current state it's JSONB inside
-- app_conversations.messages so the DROP above covers it.

COMMIT;
```

- [ ] **Step 2: Create the revert script**

```sql
-- scripts/revert-to-legacy.sql
-- Emergency revert script for the agent rewrite (2026-04-13).
-- Recreates the legacy tables empty so the old code can start up.
-- Your users will lose all chat history on revert.

BEGIN;

CREATE TABLE IF NOT EXISTS app_conversations (
    id             uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    app_project_id uuid NOT NULL UNIQUE,
    messages       jsonb NOT NULL DEFAULT '[]',
    summary        text NOT NULL DEFAULT '',
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);

-- If your prior codebase had app_chat_tool_calls, recreate it here with
-- its original schema. Based on the audit, it was JSONB inside
-- app_conversations.messages — no separate table existed.

COMMIT;
```

- [ ] **Step 3: Commit**

```bash
git add scripts/migrate-to-agent.sql scripts/revert-to-legacy.sql
git commit -m "chore(agent): migration and revert SQL scripts"
```

---

## Phase C5 complete — checkpoint

The full legacy system is deleted. `go build ./...` and `go vet ./...` pass. The new `internal/agent/` package is the only AI codegen in the project. Net LOC delta is −13,000 or better.

---

## Phase C6 — Final verification

### Task C6.1: Full build + vet

- [ ] **Step 1: Clean build**

```bash
rm -rf bin
go clean -cache
go build -o bin/app ./cmd/...
```
Expected: `bin/app` exists, no errors.

- [ ] **Step 2: Full vet**

```bash
go vet ./...
```
Expected: no output.

- [ ] **Step 3: Grep for forbidden relics**

```bash
grep -rn "wemob_file\|wemob_delete\|wemob_edit\|leakedXMLRegex\|stopShortSentinel\|NO_CODE_NEEDED\|autoFix\|FileStreamParser" internal/ cmd/
```
Expected: zero hits.

- [ ] **Step 4: LOC sanity**

```bash
cloc --include-lang=Go internal/ cmd/
```
Expected: total Go LOC dropped by roughly 13,000 compared to pre-rewrite baseline.

- [ ] **Step 5: No commit — this is a check task**

---

### Task C6.2: Deploy to staging and run the 12-scenario checklist

This is manual verification against a real environment. Each scenario is described in the design spec §10.

- [ ] **Scenario 1: Draft web project initial generation**

1. Create a new draft web project from the dashboard.
2. Send first message: "build me a minimal portfolio site for a photographer with home, gallery, about, and contact pages."
3. Watch the SSE stream in the dashboard console.
4. Verify: `run_start` → several `turn_start` / `tool_call_*` → `file_written` events → `done`.
5. Verify: E2B preview loads without errors.
6. Check `app_agent_runs` row: `status=completed`, non-empty `final_commit`, non-zero `tokens`, `credits_used`.

- [ ] **Scenario 2: Chat edit with `edit_file`**

1. On the portfolio project from Scenario 1, send: "make the hero headline blue and a bit larger."
2. Verify: agent uses `edit_file` tool (NOT `write_file`). Watch the SSE stream for `tool_call_start` with `tool_name: "edit_file"`.
3. Verify: E2B preview reflects the change.
4. Verify: `app_agent_runs.tool_call_count` is small (≤ 3), indicating a targeted edit.

- [ ] **Scenario 3: Deliberately broken request → rollback**

1. Send: "delete package.json and all config files."
2. Verify: agent responds with a protected-file error in narration.
3. Verify: NO rollback fires (the agent handled it cleanly).
4. Send: "now write a file that imports from a non-existent package like `@does-not-exist/foo`."
5. Simulate a build failure by posting an error report through the chat UI.
6. Verify: agent proposes a fix in the next turn.
7. Verify: `credits_used` is present only on successful turns.

- [ ] **Scenario 4: Ecommerce initial generation**

1. Create a draft ecommerce project.
2. Send: "build an ecommerce storefront for a ceramics shop."
3. Verify: template files `components/ecommerce/providers.tsx`, `app/cart/page.tsx`, etc. are injected BEFORE the agent starts writing.
4. Verify: the agent references the store SDK in generated product/category code.
5. Verify: generated pages include home, product listing, product detail, about, contact, FAQ, policies.

- [ ] **Scenario 5: Mobile app initial generation**

1. Create a draft mobile project.
2. Send: "build a fitness tracker app with home, workout, history, profile tabs."
3. Verify: mobile bridge SDK files are injected.
4. Verify: generated layout is single-column with bottom tabs (not desktop responsive grid).
5. Verify: the agent may call `configure_mobile_bridge` if it needs native capabilities.

- [ ] **Scenario 6: Database project with `execute_sql`**

1. Create a draft web project with the "database" flag in settings.
2. Verify: Neon provisioning runs synchronously before the agent starts.
3. Send: "build a task tracker with a tasks table and CRUD pages."
4. Verify: agent uses `execute_sql` to CREATE TABLE.
5. Verify: agent writes drizzle schema to `lib/db/schema.ts`.
6. Verify: generated pages query via drizzle, not raw SQL in TSX.

- [ ] **Scenario 7: Sandbox error auto-repair**

1. On any existing project, manually introduce a syntax error via the file CRUD UI (e.g., `export default function Home() {` with no closing brace).
2. Open the preview — E2B surfaces a build error.
3. Dashboard auto-posts an error report message.
4. Verify: agent receives the error as a normal chat message and fixes it.

- [ ] **Scenario 8: Cancel mid-run**

1. Start a fresh generation.
2. Close the browser tab mid-stream.
3. Wait 30s.
4. Reopen the project.
5. Verify: `app_agent_runs` row for that run has `status=canceled` or `failed`.
6. Verify: project files are unchanged (pre-commit is HEAD).

- [ ] **Scenario 9: Force `budget_exceeded`**

1. Temporarily lower `defaultMaxTurns` to 2 in `internal/agent/run.go` (staging only).
2. Start a fresh generation.
3. Verify: agent stops with `budget_exceeded` status.
4. Revert the constant change before merging.

- [ ] **Scenario 10: Reconnect during a live run**

1. Start a generation.
2. Disconnect the SSE stream (network tab → offline) after ~5 events.
3. Hit `GET /v2/apps/{appId}/runs/{runId}/stream` from a new connection.
4. Verify: the buffer replays all missed events and then streams live events until `done`.

- [ ] **Scenario 11: `app_agent_runs` telemetry sanity**

Run in `psql` against staging:

```sql
SELECT id, status, turns, tool_call_count, prompt_tokens, completion_tokens,
       cost_usd, credits_used, duration_ms, pre_commit, final_commit
FROM app_agent_runs
ORDER BY created_at DESC LIMIT 20;
```

Verify: every completed row has non-zero token counts, non-empty commits, sensible duration.

- [ ] **Scenario 12: Credit accounting on failure**

1. Force a failure (e.g., kill the LLM API key in SecretManager temporarily, run a chat, restore the key).
2. Verify: the failed run's row has `credits_used = 0`.
3. Verify: the user's credit balance is unchanged.

- [ ] **Sign-off**

If all 12 scenarios pass, the PR is mergeable. Any failure → fix in the PR branch, re-run that scenario, then proceed.

---

### Task C6.3: Coordinate the paired `wemob-dashboard` PR

**Files:** (separate repo — not in this plan, but noted here as a dependency)

- [ ] **Step 1: Verify the dashboard PR is open**

Before merging the backend PR, confirm the dashboard PR is ready:
1. Implements the 13 new SSE event types from spec §9.
2. Renders `tool_call_start` → `tool_call_delta` → `tool_call_result` → `file_written` as collapsible tool cards.
3. Reconnects via `GET /v2/apps/{appId}/runs/{runId}/stream`.
4. Deletes old event handlers (`file_start`, `file_chunk`, `file_complete`, `conversation_chunk`, etc.).

- [ ] **Step 2: Merge both within a tight window**

Merge backend → merge dashboard within ~5 minutes of each other. Other clients (mobile app, third-party) see a broken chat until the dashboard is up. Coordinate a maintenance window if needed.

---

## Plan complete.

**Expected final state:**
- `git log --oneline` shows ~40 new commits on `main`.
- `go build ./...` and `go vet ./...` pass.
- `cloc internal/ cmd/` shows a net drop of ~13,000 Go LOC.
- `internal/agent/` is ~3,000 LOC, 13 files.
- The dashboard renders via the new event schema.
- `app_agent_runs` populates on every chat/generate call.
- The 12-scenario verification checklist passed on staging.
