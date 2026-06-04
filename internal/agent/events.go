package agent

import (
	"encoding/json"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
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
	RunID           uuid.UUID `json:"run_id"`
	PreCommit       string    `json:"pre_commit"`
	Model           string    `json:"model"`
	Provider        string    `json:"provider"`
	MaxTurns        int       `json:"max_turns"`
	Tools           []string  `json:"tools"`
	AgentID         string    `json:"agent_id,omitempty"`
	AgentName       string    `json:"agent_name,omitempty"`
	Skills          []string  `json:"skills,omitempty"`
	RequiredSkills  []string  `json:"required_skills,omitempty"`
	SuggestedSkills []string  `json:"suggested_skills,omitempty"`
	AvailableSkills []string  `json:"available_skills,omitempty"`
	LoadedSkills    []string  `json:"loaded_skills,omitempty"`
	Handoffs        []string  `json:"handoffs,omitempty"`
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
	RiskLevel    string          `json:"risk_level,omitempty"`
	InputPreview json.RawMessage `json:"input_preview,omitempty"`
}

// (ToolCallDeltaEvent existed in an earlier draft of the agent rewrite for
// streaming partial tool args. It was never emitted by any production code
// path and the dashboard never consumed it — removed in Phase 2.)

type ToolCallResultEvent struct {
	RunID      uuid.UUID `json:"run_id"`
	ToolCallID string    `json:"tool_call_id"`
	ToolName   string    `json:"tool_name"`
	RiskLevel  string    `json:"risk_level,omitempty"`
	Status     string    `json:"status"`
	DurationMs int       `json:"duration_ms"`
	Summary    string    `json:"summary"`
	IsError    bool      `json:"is_error"`
}

type ToolApprovalRequiredEvent struct {
	RunID        uuid.UUID       `json:"run_id"`
	ApprovalID   string          `json:"approval_id"`
	ToolCallID   string          `json:"tool_call_id,omitempty"`
	ToolName     string          `json:"tool_name"`
	ConnectionID string          `json:"connection_id"`
	RiskLevel    string          `json:"risk_level"`
	InputPreview json.RawMessage `json:"input_preview,omitempty"`
	ExpiresAt    int64           `json:"expires_at"`
}

type ToolApprovalResolvedEvent struct {
	RunID        uuid.UUID `json:"run_id"`
	ApprovalID   string    `json:"approval_id"`
	ToolName     string    `json:"tool_name"`
	ConnectionID string    `json:"connection_id"`
	Status       string    `json:"status"`
	Approved     bool      `json:"approved"`
}

type SkillLoadedEvent struct {
	RunID    uuid.UUID `json:"run_id"`
	SkillID  string    `json:"skill_id"`
	Name     string    `json:"name,omitempty"`
	Active   []string  `json:"active,omitempty"`
	Loaded   []string  `json:"loaded,omitempty"`
	Required []string  `json:"required,omitempty"`
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

// CreditsUpdateEvent is emitted after each turn so the frontend can render
// a live credit meter mid-run instead of only at done. CreditsUsed is the
// cumulative billed credits for the run so far (matches the final
// DoneEvent.CreditsUsed when the run completes cleanly). Tokens is this
// turn's input+output token total — informational, not used for billing.
type CreditsUpdateEvent struct {
	RunID       uuid.UUID `json:"run_id"`
	Turn        int       `json:"turn"`
	CreditsUsed int       `json:"credits_used"`
	Tokens      int       `json:"tokens,omitempty"`
}

type TodoSnapshotEvent struct {
	RunID uuid.UUID          `json:"run_id"`
	Todos []models.AgentTodo `json:"todos"`
}

type TokenUsage struct {
	Input      int `json:"input"`
	Output     int `json:"output"`
	CacheRead  int `json:"cache_read"`
	CacheWrite int `json:"cache_write"`
}

type ErrorEvent struct {
	RunID   uuid.UUID `json:"run_id"`
	Phase   string    `json:"phase"`
	Message string    `json:"message"`
	Code    ErrorCode `json:"code"`
}

type CheckpointEvent struct {
	RunID  uuid.UUID `json:"run_id"`
	Commit string    `json:"commit"`
	Reason string    `json:"reason"`
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

// (KeepaliveEvent was a typed 15s heartbeat from the agent. It duplicated
// the view layer's `:keepalive\n\n` SSE comment and gave clients an event
// they had to special-case as a no-op. Removed in Phase 2 — the SSE comment
// ticker still keeps proxies alive, and the dashboard's idle watchdog now
// resets on every wire byte instead of every typed keepalive.)

// Event type constants — single source of truth for event names.
const (
	EventRunStart             = "run_start"
	EventTurnStart            = "turn_start"
	EventNarration            = "narration"
	EventToolCallStart        = "tool_call_start"
	EventToolCallResult       = "tool_call_result"
	EventToolApprovalRequired = "tool_approval_required"
	EventToolApprovalResolved = "tool_approval_resolved"
	EventSkillLoaded          = "skill_loaded"
	EventFileWritten          = "file_written"
	EventFileDeleted          = "file_deleted"
	EventTurnEnd              = "turn_end"
	EventCreditsUpdate        = "credits_update"
	EventTodoSnapshot         = "todo_snapshot"
	EventCheckpoint           = "checkpoint"
	EventError                = "error"
	EventDone                 = "done"
)

// The SSE-backed emitter lives in internal/views/agent_bridge.go as
// agentSSEAdapter; that's the only production emitter. An earlier version of
// this file exported an sseEmitter with its own 500-entry ring buffer and a
// Close() that closed the underlying channel. It was never wired up —
// handleStreamOrAsync owns the channel — and the double-close vector was a
// loaded gun for any future refactor, so it has been removed.
