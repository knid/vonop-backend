package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/deepagents"
	"github.com/knid/wemob/internal/services/gitrepo"
	"github.com/knid/wemob/internal/utils"
)

// AGENT_RUNTIME selects which execution engine powers agent runs.
const envAgentRuntime = "AGENT_RUNTIME"

type RuntimeKind string

// AgentRuntimeKind and runtime constants are kept for backwards
// compatibility with existing references in integration tests and call sites
// that haven't been migrated yet.
type AgentRuntimeKind = RuntimeKind

const (
	RuntimeNativeGo   RuntimeKind = "native_go"
	RuntimeDeepAgents RuntimeKind = "deepagents"
)

const (
	AgentRuntimeNativeGo   AgentRuntimeKind = RuntimeNativeGo
	AgentRuntimeDeepAgents AgentRuntimeKind = RuntimeDeepAgents
)

// RuntimeLimits configures per-run budgets in the runtime abstraction.
type RuntimeLimits struct {
	MaxWallClockMs         int
	MaxModelCalls          int
	MaxToolCalls           int
	MaxOutputTokensPerCall int
}

// RuntimeRequest carries runtime-agnostic configuration for a single run.
type RuntimeRequest struct {
	RunID           string
	JobID           string
	Project         *models.AppProject
	User            *models.User
	Message         string
	ExpandedMessage string
	IsFirstGen      bool
	History         []utils.Message
	Attachments     TurnAttachments
	Profile         *AgentProfile
	ContextPacket   AgentContextPacket
	Registry        *Registry
	Repo            *gitrepo.Repo
	PreCommit       string
	BuildState      *BuildState
	Todos           *TodoList
	FileMap         *SafeFileMap
	Tracker         *utils.TokenTracker
	Model           string
	Provider        *utils.LLMProvider
	Limits          RuntimeLimits
	Deps            *RunDependencies
}

// RuntimeResult captures the canonical run output.
type RuntimeResult struct {
	Parts      []models.ChatMessagePart
	ToolCalls  int
	Turns      int
	StopReason string
}

// Runtime is the pluggable execution engine for an agent run.
type Runtime interface {
	// Kind is the runtime identifier used for diagnostics and logging.
	Kind() RuntimeKind
	// Run executes one agent turn through the configured runtime.
	Run(ctx context.Context, req RuntimeRequest, emitter EventEmitter) (*RuntimeResult, error)
}

// AgentRuntime keeps backward compatibility with existing references while
// retaining the same execution contract as Runtime.
type AgentRuntime = Runtime

// ResolveAgentRuntimeKind reads AGENT_RUNTIME and maps known values to a runtime
// kind. Unknown values fall back to native_go for safety and backward
// compatibility.
func ResolveAgentRuntimeKind() RuntimeKind {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(envAgentRuntime))) {
	case "", string(RuntimeNativeGo):
		return RuntimeNativeGo
	case string(RuntimeDeepAgents):
		return RuntimeDeepAgents
	default:
		return RuntimeNativeGo
	}
}

// NewAgentRuntime builds the configured runtime implementation.
func NewAgentRuntime(kind RuntimeKind) (Runtime, error) {
	switch kind {
	case RuntimeDeepAgents:
		return NewDeepAgentsRuntime()
	case RuntimeNativeGo:
		return NewNativeGoRuntime(), nil
	default:
		return NewNativeGoRuntime(), nil
	}
}

// NewRuntimeFromEnv loads AGENT_RUNTIME and resolves the corresponding runtime
// implementation.
func NewRuntimeFromEnv() (AgentRuntime, error) {
	return NewAgentRuntime(ResolveAgentRuntimeKind())
}

// Runtime limits used by adapter implementations that expose a single source of
// truth for wall-clock and tool budget controls.
func defaultRuntimeLimits() RuntimeLimits {
	return RuntimeLimits{
		MaxWallClockMs:         45 * 60 * 1000,
		MaxModelCalls:          80,
		MaxToolCalls:           200,
		MaxOutputTokensPerCall: 64000,
	}
}

func NewDeepAgentsRuntime() (AgentRuntime, error) {
	baseURL := strings.TrimSpace(os.Getenv("DEEPAGENTS_RUNTIME_URL"))
	token := strings.TrimSpace(os.Getenv("DEEPAGENTS_INTERNAL_TOKEN"))
	if baseURL == "" {
		return nil, fmt.Errorf("DEEPAGENTS_RUNTIME_URL is required when AGENT_RUNTIME=%s", RuntimeDeepAgents)
	}
	if token == "" {
		return nil, fmt.Errorf("DEEPAGENTS_INTERNAL_TOKEN is required when AGENT_RUNTIME=%s", RuntimeDeepAgents)
	}
	return &DeepAgentsRuntime{client: deepagents.NewClient(baseURL, token)}, nil
}

// NewNativeGoRuntime returns the in-process legacy runtime implementation.
func NewNativeGoRuntime() AgentRuntime {
	return NativeGoRuntime{}
}

func ValidateRuntimeStartup() {
	kind := ResolveAgentRuntimeKind()
	if kind != RuntimeDeepAgents {
		return
	}
	url := strings.TrimSpace(os.Getenv("DEEPAGENTS_RUNTIME_URL"))
	token := strings.TrimSpace(os.Getenv("DEEPAGENTS_INTERNAL_TOKEN"))
	if url == "" {
		slog.Error("AGENT_RUNTIME=deepagents but DEEPAGENTS_RUNTIME_URL is not set")
		os.Exit(1)
	}
	if token == "" {
		slog.Error("AGENT_RUNTIME=deepagents but DEEPAGENTS_INTERNAL_TOKEN is not set")
		os.Exit(1)
	}
	slog.Info("agent runtime configured", "kind", string(kind), "url", url)
}
