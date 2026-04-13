package agent

import (
	"encoding/json"

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
	RunID      uuid.UUID  `json:"run_id"`
	Turn       int        `json:"turn"`
	StopReason string     `json:"stop_reason"`
	Tokens     TokenUsage `json:"tokens"`
}

type TokenUsage struct {
	Input      int `json:"input"`
	Output     int `json:"output"`
	CacheRead  int `json:"cache_read"`
	CacheWrite int `json:"cache_write"`
}

type RollbackEvent struct {
	RunID         uuid.UUID `json:"run_id"`
	PreCommit     string    `json:"pre_commit"`
	Reason        string    `json:"reason"`
	FilesReverted int       `json:"files_reverted"`
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

// The SSE-backed emitter lives in internal/views/agent_bridge.go as
// agentSSEAdapter; that's the only production emitter. An earlier version of
// this file exported an sseEmitter with its own 500-entry ring buffer and a
// Close() that closed the underlying channel. It was never wired up —
// handleStreamOrAsync owns the channel — and the double-close vector was a
// loaded gun for any future refactor, so it has been removed.
