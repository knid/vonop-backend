# Agent Runtime Harness Phase 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Refactor the existing Wemob agent runtime into an explicit internal harness while preserving current behavior.

**Architecture:** Keep `agent.Run` as the public entry point, but introduce focused runtime units inside `internal/agent`: run-scoped state, a workflow coordinator, a tool gateway, an agent-loop adapter, and phase telemetry helpers. The default loop still calls `utils.StreamWithTools`; prompts, model selection, tools, SSE payloads, persistence, credit accounting, and finalization behavior must remain unchanged.

**Tech Stack:** Go, existing `internal/agent`, `internal/utils`, `internal/observability`, Bun models, OpenTelemetry/Langfuse, existing Go test suite.

---

## File Structure

- Create `internal/agent/run_execution.go`
  - Owns `RunExecution`, small shared runtime structs, and run-scoped helper methods that do not perform major side effects.
- Create `internal/agent/run_workflow.go`
  - Owns `AgentRunWorkflow`, phase ordering, and behavior-preserving orchestration helpers gradually moved out of `run.go`.
- Create `internal/agent/agent_loop.go`
  - Owns `AgentLoop`, `AgentLoopInput`, `AgentLoopResult`, and the default adapter around `utils.StreamWithTools`.
- Create `internal/agent/tool_gateway.go`
  - Owns model-requested local tool execution: events, risk, registry dispatch, memory persistence, message parts, and Langfuse tool observations.
- Create `internal/agent/runtime_telemetry.go`
  - Owns phase span names and a small helper for starting/ending runtime phase spans.
- Modify `internal/agent/run.go`
  - Gradually delegates to the new files. Keep `Run` as the stable public function.
- Modify `internal/agent/run_test.go`
  - Add unit tests for run-scoped helpers and workflow behavior that does not require full integration setup.
- Create `internal/agent/agent_loop_test.go`
  - Tests the loop adapter inputs/outputs with fakes where possible.
- Create `internal/agent/tool_gateway_test.go`
  - Tests gateway event ordering, message part updates, memory recording, and tool error propagation.
- Create `internal/agent/runtime_telemetry_test.go`
  - Tests phase span naming and phase attribute construction.

Do not move code outside `internal/agent` in Phase 1. Avoid package splits until extracted files prove their shape.

---

### Task 1: Add Runtime Phase Telemetry Helpers

**Files:**
- Create: `internal/agent/runtime_telemetry.go`
- Create: `internal/agent/runtime_telemetry_test.go`

- [ ] **Step 1: Write tests for phase span names**

Create `internal/agent/runtime_telemetry_test.go`:

```go
package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRuntimePhaseSpanNames(t *testing.T) {
	assert.Equal(t, "initialize-run", RuntimePhaseInitializeRun.SpanName())
	assert.Equal(t, "load-project-state", RuntimePhaseLoadProjectState.SpanName())
	assert.Equal(t, "compile-runtime-context", RuntimePhaseCompileRuntimeContext.SpanName())
	assert.Equal(t, "build-runtime-tools", RuntimePhaseBuildRuntimeTools.SpanName())
	assert.Equal(t, "execute-agent-loop", RuntimePhaseExecuteAgentLoop.SpanName())
	assert.Equal(t, "persist-assistant-turn", RuntimePhasePersistAssistantTurn.SpanName())
	assert.Equal(t, "finalize-run", RuntimePhaseFinalizeRun.SpanName())
	assert.Equal(t, "record-usage-and-scores", RuntimePhaseRecordUsageAndScores.SpanName())
}

func TestRuntimePhaseStringFallsBackToRawValue(t *testing.T) {
	phase := RuntimePhase("custom-phase")
	assert.Equal(t, "custom-phase", phase.SpanName())
}
```

- [ ] **Step 2: Run the tests and verify they fail**

Run:

```bash
go test ./internal/agent -run 'TestRuntimePhase' -count=1
```

Expected: compile failure because `RuntimePhase` and constants are not defined.

- [ ] **Step 3: Implement phase constants**

Create `internal/agent/runtime_telemetry.go`:

```go
package agent

import (
	"context"

	"github.com/knid/wemob/internal/observability"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type RuntimePhase string

const (
	RuntimePhaseInitializeRun         RuntimePhase = "initialize-run"
	RuntimePhaseLoadProjectState      RuntimePhase = "load-project-state"
	RuntimePhaseCompileRuntimeContext RuntimePhase = "compile-runtime-context"
	RuntimePhaseBuildRuntimeTools     RuntimePhase = "build-runtime-tools"
	RuntimePhaseExecuteAgentLoop      RuntimePhase = "execute-agent-loop"
	RuntimePhasePersistAssistantTurn  RuntimePhase = "persist-assistant-turn"
	RuntimePhaseFinalizeRun           RuntimePhase = "finalize-run"
	RuntimePhaseRecordUsageAndScores  RuntimePhase = "record-usage-and-scores"
)

func (p RuntimePhase) SpanName() string {
	return string(p)
}

func startRuntimePhase(ctx context.Context, phase RuntimePhase, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	baseAttrs := []attribute.KeyValue{
		attribute.String("langfuse.observation.type", "span"),
		attribute.String("langfuse.observation.metadata.runtime_phase", phase.SpanName()),
	}
	baseAttrs = append(baseAttrs, attrs...)
	return observability.Tracer().Start(ctx, phase.SpanName(), trace.WithAttributes(baseAttrs...))
}
```

- [ ] **Step 4: Run the focused tests**

Run:

```bash
go test ./internal/agent -run 'TestRuntimePhase' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/runtime_telemetry.go internal/agent/runtime_telemetry_test.go
git commit -m "refactor(agent): add runtime phase telemetry names"
```

---

### Task 2: Add RunExecution State Container

**Files:**
- Create: `internal/agent/run_execution.go`
- Modify: `internal/agent/run_test.go`

- [ ] **Step 1: Add tests for run execution state**

Append to `internal/agent/run_test.go`:

```go
func TestRunExecutionHasRunID(t *testing.T) {
	runID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	exec := &RunExecution{RunID: runID}

	assert.Equal(t, runID.String(), exec.RunIDString())
}

func TestRunExecutionHasEmptyRunIDStringWhenUnset(t *testing.T) {
	exec := &RunExecution{}

	assert.Equal(t, "", exec.RunIDString())
}
```

- [ ] **Step 2: Run tests and verify failure**

Run:

```bash
go test ./internal/agent -run 'TestRunExecution' -count=1
```

Expected: compile failure because `RunExecution` is not defined.

- [ ] **Step 3: Implement RunExecution**

Create `internal/agent/run_execution.go`:

```go
package agent

import (
	"context"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/gitrepo"
	"github.com/knid/wemob/internal/utils"
)

type RunExecution struct {
	Ctx        context.Context
	Deps       *RunDependencies
	Project    *models.AppProject
	User       *models.User
	Run        *models.AppAgentRun
	RunID      uuid.UUID
	Repo       *gitrepo.Repo
	FileMap    *SafeFileMap
	Tracker    *utils.TokenTracker
	Emitter    EventEmitter
	Registry   *Registry
	Todos      *TodoList
	Memory     *AgentRunMemory
	BuildState *BuildState
}

func (e *RunExecution) RunIDString() string {
	if e == nil || e.RunID == uuid.Nil {
		return ""
	}
	return e.RunID.String()
}
```

- [ ] **Step 4: Run focused tests**

Run:

```bash
go test ./internal/agent -run 'TestRunExecution' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/run_execution.go internal/agent/run_test.go
git commit -m "refactor(agent): add run execution state"
```

---

### Task 3: Introduce AgentLoop Adapter Around StreamWithTools

**Files:**
- Create: `internal/agent/agent_loop.go`
- Create: `internal/agent/agent_loop_test.go`

- [ ] **Step 1: Write adapter construction tests**

Create `internal/agent/agent_loop_test.go`:

```go
package agent

import (
	"testing"

	"github.com/knid/wemob/internal/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultAgentLoopRequiresProvider(t *testing.T) {
	loop := DefaultAgentLoop{}

	_, err := loop.Execute(t.Context(), AgentLoopInput{
		Model:    "claude-sonnet-4-6",
		Messages: []utils.Message{{Role: "user", Content: "hi"}},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing LLM provider configuration")
}

func TestAgentLoopInputCarriesCallbacks(t *testing.T) {
	var chunked string
	input := AgentLoopInput{
		OnChunk: func(chunk string) {
			chunked += chunk
		},
	}

	input.OnChunk("a")
	input.OnChunk("b")

	assert.Equal(t, "ab", chunked)
}
```

- [ ] **Step 2: Run tests and verify failure**

Run:

```bash
go test ./internal/agent -run 'TestDefaultAgentLoop|TestAgentLoopInput' -count=1
```

Expected: compile failure because `DefaultAgentLoop` and `AgentLoopInput` are not defined.

- [ ] **Step 3: Implement AgentLoop adapter**

Create `internal/agent/agent_loop.go`:

```go
package agent

import (
	"context"

	"github.com/knid/wemob/internal/utils"
)

type AgentLoop interface {
	Execute(ctx context.Context, input AgentLoopInput) (AgentLoopResult, error)
}

type AgentLoopInput struct {
	Model          string
	Messages       []utils.Message
	Tools          []utils.Tool
	Provider       *utils.LLMProvider
	ResponseFormat *utils.ResponseFormat
	Reasoning      *utils.ReasoningConfig
	Executor       utils.ToolExecutor
	OnToolCall     func(toolName string, args string)
	OnChunk        func(chunk string)
	OnReasoning    func(chunk string)
	Config         utils.ToolCallConfig
}

type AgentLoopResult struct {
	Text string
}

type DefaultAgentLoop struct{}

func (DefaultAgentLoop) Execute(ctx context.Context, input AgentLoopInput) (AgentLoopResult, error) {
	text, err := utils.StreamWithTools(
		ctx,
		input.Model,
		input.Messages,
		input.Tools,
		input.Provider,
		input.ResponseFormat,
		input.Reasoning,
		input.Executor,
		input.OnToolCall,
		input.OnChunk,
		input.OnReasoning,
		input.Config,
	)
	return AgentLoopResult{Text: text}, err
}
```

- [ ] **Step 4: Run focused tests**

Run:

```bash
go test ./internal/agent -run 'TestDefaultAgentLoop|TestAgentLoopInput' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/agent_loop.go internal/agent/agent_loop_test.go
git commit -m "refactor(agent): wrap stream loop behind agent loop"
```

---

### Task 4: Route agent.Run Through AgentLoop Without Behavior Change

**Files:**
- Modify: `internal/agent/run.go`
- Modify: `internal/agent/run_integration_test.go`

- [ ] **Step 1: Add or identify integration coverage**

Use the existing integration tests that exercise `Run` through fake LLM streams:

```bash
go test ./internal/agent -run 'TestRunIntegrationWriteFilesExecutesToolAndCommits|TestRunIntegrationToolMatrixReadSearchPatchDelete' -count=1
```

Expected before code changes: PASS. If the tests require external services in the current environment, record the exact failure and use the narrower available `Run` tests instead.

- [ ] **Step 2: Replace direct StreamWithTools call with DefaultAgentLoop**

In `internal/agent/run.go`, locate the current call:

```go
llmText, llmErr := utils.StreamWithTools(
	ctx, model, llmMessages, tools, provider,
	responseFormat, reasoning,
	executor, onToolCall, narrationSink, reasoningSink, config,
)
```

Replace it with:

```go
loop := DefaultAgentLoop{}
loopResult, llmErr := loop.Execute(ctx, AgentLoopInput{
	Model:          model,
	Messages:       llmMessages,
	Tools:          tools,
	Provider:       provider,
	ResponseFormat: responseFormat,
	Reasoning:      reasoning,
	Executor:       executor,
	OnToolCall:     onToolCall,
	OnChunk:        narrationSink,
	OnReasoning:    reasoningSink,
	Config:         config,
})
llmText := loopResult.Text
```

Use the actual local variable names from `run.go`. Do not change callback bodies, config values, or error handling.

- [ ] **Step 3: Run targeted tests**

Run:

```bash
go test ./internal/agent -run 'TestRunIntegrationWriteFilesExecutesToolAndCommits|TestRunIntegrationToolMatrixReadSearchPatchDelete|TestDefaultAgentLoop' -count=1
```

Expected: PASS.

- [ ] **Step 4: Run package tests**

Run:

```bash
go test ./internal/agent -count=1
```

Expected: PASS or the same environment-specific skips/failures observed before this task.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/run.go internal/agent/agent_loop.go internal/agent/agent_loop_test.go
git commit -m "refactor(agent): route runs through agent loop adapter"
```

---

### Task 5: Extract ToolGateway With Tests

**Files:**
- Create: `internal/agent/tool_gateway.go`
- Create: `internal/agent/tool_gateway_test.go`

- [ ] **Step 1: Write tests for basic gateway execution**

Create `internal/agent/tool_gateway_test.go`:

```go
package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type gatewayTestTool struct {
	result  string
	isError bool
	err     error
}

func (gatewayTestTool) Name() string { return "test_tool" }
func (gatewayTestTool) Description() string { return "test tool" }
func (gatewayTestTool) Schema() map[string]any { return map[string]any{"type": "object"} }
func (t gatewayTestTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	return t.result, t.isError, t.err
}

type gatewayTestEmitter struct {
	events []Event
}

func (e *gatewayTestEmitter) Emit(eventType string, data any) bool {
	e.events = append(e.events, Event{Type: eventType, Data: data})
	return true
}
func (e *gatewayTestEmitter) Buffer() []Event { return e.events }
func (e *gatewayTestEmitter) Close() {}

func TestToolGatewayExecuteEmitsStartAndResult(t *testing.T) {
	registry := NewRegistry()
	registry.Register(gatewayTestTool{result: "ok"})
	emitter := &gatewayTestEmitter{}
	tracker := utils.NewTokenTracker()
	runID := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	gateway := ToolGateway{
		RunID:    runID,
		Turn:     2,
		Registry: registry,
		RC:       &RunContext{RunID: runID},
		Emitter:  emitter,
		Tracker:  tracker,
	}

	result, err := gateway.Execute(t.Context(), "test_tool", `{"x":1}`)

	require.NoError(t, err)
	assert.Contains(t, result, "<tool_output tool=\"test_tool\">")
	require.Len(t, emitter.events, 2)
	assert.Equal(t, EventToolCallStart, emitter.events[0].Type)
	assert.Equal(t, EventToolCallResult, emitter.events[1].Type)
	assert.Equal(t, []string{"test_tool"}, tracker.ToolsUsed())
}
```

- [ ] **Step 2: Run tests and verify failure**

Run:

```bash
go test ./internal/agent -run 'TestToolGateway' -count=1
```

Expected: compile failure because `ToolGateway` is not defined.

- [ ] **Step 3: Implement minimal ToolGateway**

Create `internal/agent/tool_gateway.go`:

```go
package agent

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/observability"
	"github.com/knid/wemob/internal/utils"
)

type ToolGateway struct {
	RunID    uuid.UUID
	Turn     int
	Registry *Registry
	RC       *RunContext
	Emitter  EventEmitter
	Tracker  *utils.TokenTracker
	Memory   *AgentRunMemory
	Deps     *RunDependencies
}

func (g *ToolGateway) Execute(ctx context.Context, name string, argsJSON string) (string, error) {
	toolStart := time.Now()
	toolCallID := uuid.NewString()
	if g.Tracker != nil {
		g.Tracker.RecordToolUsed(name)
	}
	if g.Emitter != nil {
		g.Emitter.Emit(EventToolCallStart, ToolCallStartEvent{
			RunID:        g.RunID,
			Turn:         g.Turn,
			ToolCallID:   toolCallID,
			ToolName:     name,
			RiskLevel:    ClassifyToolRisk(name),
			InputPreview: stripToolInputForPreview(name, argsJSON),
		})
	}

	result, isErr, execErr := g.Registry.Execute(ctx, g.RC, name, json.RawMessage(argsJSON))
	durationMs := int(time.Since(toolStart).Milliseconds())
	status := "ok"
	if isErr || execErr != nil {
		status = "error"
	}

	if g.Tracker != nil {
		requiredSkillIDs, loadedSkillIDs := g.Tracker.AgentSkills()
		langfuseContext := g.Tracker.LangfuseContext()
		langfuseContext.RequiredSkills = requiredSkillIDs
		langfuseContext.LoadedSkills = loadedSkillIDs
		observability.RecordTool(ctx, name, argsJSON, isErr || execErr != nil, durationMs, toolStart,
			langfuseContext.Attributes("tool")...,
		)
	}

	if g.Emitter != nil {
		g.Emitter.Emit(EventToolCallResult, ToolCallResultEvent{
			RunID:      g.RunID,
			ToolCallID: toolCallID,
			ToolName:   name,
			RiskLevel:  ClassifyToolRisk(name),
			Status:     status,
			DurationMs: durationMs,
			Summary:    firstLine(result, 140),
			IsError:    isErr || execErr != nil,
		})
	}

	if g.Memory != nil {
		g.Memory.RecordTool(name, argsJSON, result, isErr || execErr != nil)
		if g.Deps != nil && g.Deps.DB != nil {
			g.Memory.Persist(ctx, g.Deps.DB, g.RunID)
		}
	}

	if execErr != nil {
		return result, execErr
	}
	return result, nil
}
```

- [ ] **Step 4: Run focused tests**

Run:

```bash
go test ./internal/agent -run 'TestToolGateway' -count=1
```

Expected: PASS.

- [ ] **Step 5: Confirm run.go is unchanged in this task**

Run:

```bash
git diff -- internal/agent/run.go
```

Expected: no diff. Task 5 only adds the gateway and its tests. `run.go` is routed through the gateway in Task 6 after the gateway owns assistant tool-call parts.

- [ ] **Step 6: Run integration tests**

Run:

```bash
go test ./internal/agent -run 'TestToolGateway' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/agent/tool_gateway.go internal/agent/tool_gateway_test.go
git commit -m "refactor(agent): extract tool gateway"
```

---

### Task 6: Move Assistant Tool Parts Into ToolGateway

**Files:**
- Modify: `internal/agent/tool_gateway.go`
- Modify: `internal/agent/tool_gateway_test.go`
- Modify: `internal/agent/run.go`

- [ ] **Step 1: Add tests for tool call parts**

Append to `internal/agent/tool_gateway_test.go`:

```go
func TestToolGatewayUpdatesAssistantParts(t *testing.T) {
	registry := NewRegistry()
	registry.Register(gatewayTestTool{result: "ok"})
	parts := make([]models.ChatMessagePart, 0)

	gateway := ToolGateway{
		RunID:    uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		Turn:     1,
		Registry: registry,
		RC:       &RunContext{},
		Parts:    &parts,
	}

	result, err := gateway.Execute(t.Context(), "test_tool", `{"x":1}`)

	require.NoError(t, err)
	assert.Contains(t, result, "ok")
	require.Len(t, parts, 1)
	assert.Equal(t, "tool_call", parts[0].Type)
	assert.Equal(t, "test_tool", parts[0].Name)
	assert.JSONEq(t, `{"x":1}`, string(parts[0].Input))
	assert.Contains(t, parts[0].Result, "ok")
	assert.False(t, parts[0].IsError)
}
```

Add imports if needed:

```go
import "github.com/knid/wemob/internal/models"
```

- [ ] **Step 2: Run tests and verify failure**

Run:

```bash
go test ./internal/agent -run 'TestToolGatewayUpdatesAssistantParts' -count=1
```

Expected: compile failure because `ToolGateway.Parts` does not exist.

- [ ] **Step 3: Add parts ownership to ToolGateway**

Modify `internal/agent/tool_gateway.go`:

```go
type ToolGateway struct {
	RunID    uuid.UUID
	Turn     int
	Registry *Registry
	RC       *RunContext
	Emitter  EventEmitter
	Tracker  *utils.TokenTracker
	Memory   *AgentRunMemory
	Deps     *RunDependencies
	Parts    *[]models.ChatMessagePart
	PartsMu  *sync.Mutex
}
```

Add imports:

```go
"sync"

"github.com/knid/wemob/internal/models"
```

At the start of `Execute`, after `toolCallID`, add this exact locking shape. Do not hold `PartsMu` while the registry tool executes:

```go
partIdx := -1
if g.Parts != nil {
	if g.PartsMu != nil {
		g.PartsMu.Lock()
	}
	*g.Parts = append(*g.Parts, models.ChatMessagePart{
		Type:       "tool_call",
		ToolCallID: toolCallID,
		Name:       name,
		Input:      json.RawMessage(argsJSON),
	})
	partIdx = len(*g.Parts) - 1
	if g.PartsMu != nil {
		g.PartsMu.Unlock()
	}
}
```

and after execution:

```go
if g.Parts != nil && partIdx >= 0 {
	if g.PartsMu != nil {
		g.PartsMu.Lock()
	}
	if partIdx < len(*g.Parts) {
		(*g.Parts)[partIdx].Result = result
		(*g.Parts)[partIdx].IsError = isErr || execErr != nil
		(*g.Parts)[partIdx].DurationMs = durationMs
	}
	if g.PartsMu != nil {
		g.PartsMu.Unlock()
	}
}
```

- [ ] **Step 4: Run focused tests**

Run:

```bash
go test ./internal/agent -run 'TestToolGateway' -count=1
```

Expected: PASS.

- [ ] **Step 5: Remove duplicated parts mutation from run.go**

In `internal/agent/run.go`, remove the manual placeholder append/update from the inline `executor` closure. Inside the `executor` closure, replace the removed block with:

```go
gateway := &ToolGateway{
	RunID:    runID,
	Turn:     turn,
	Registry: registry,
	RC:       rc,
	Emitter:  emitter,
	Tracker:  tracker,
	Memory:   memory,
	Deps:     deps,
	Parts:    &parts,
	PartsMu:  &partsMu,
}
result, execErr := gateway.Execute(ctx, name, argsJSON)

partsMu.Lock()
totalToolCalls++
partsMu.Unlock()
```

- [ ] **Step 6: Run integration tests**

Run:

```bash
go test ./internal/agent -run 'TestRunIntegrationWriteFilesExecutesToolAndCommits|TestRunIntegrationToolMatrixReadSearchPatchDelete|TestConvertHistory_AssistantTextOnly' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/agent/tool_gateway.go internal/agent/tool_gateway_test.go internal/agent/run.go
git commit -m "refactor(agent): move tool message parts into gateway"
```

---

### Task 7: Introduce AgentRunWorkflow Skeleton

**Files:**
- Create: `internal/agent/run_workflow.go`
- Modify: `internal/agent/run_test.go`

- [ ] **Step 1: Add workflow constructor tests**

Append to `internal/agent/run_test.go`:

```go
func TestNewAgentRunWorkflowUsesDefaultLoop(t *testing.T) {
	workflow := NewAgentRunWorkflow(nil)

	_, ok := workflow.AgentLoop.(DefaultAgentLoop)
	assert.True(t, ok)
}

func TestNewAgentRunWorkflowKeepsProvidedLoop(t *testing.T) {
	custom := fakeAgentLoop{}
	workflow := NewAgentRunWorkflow(custom)

	assert.Equal(t, custom, workflow.AgentLoop)
}

type fakeAgentLoop struct{}

func (fakeAgentLoop) Execute(ctx context.Context, input AgentLoopInput) (AgentLoopResult, error) {
	return AgentLoopResult{Text: "ok"}, nil
}
```

Add `context` to imports if missing.

- [ ] **Step 2: Run tests and verify failure**

Run:

```bash
go test ./internal/agent -run 'TestNewAgentRunWorkflow' -count=1
```

Expected: compile failure because `NewAgentRunWorkflow` is not defined.

- [ ] **Step 3: Implement workflow skeleton**

Create `internal/agent/run_workflow.go`:

```go
package agent

type AgentRunWorkflow struct {
	AgentLoop AgentLoop
}

func NewAgentRunWorkflow(loop AgentLoop) *AgentRunWorkflow {
	if loop == nil {
		loop = DefaultAgentLoop{}
	}
	return &AgentRunWorkflow{AgentLoop: loop}
}
```

- [ ] **Step 4: Run focused tests**

Run:

```bash
go test ./internal/agent -run 'TestNewAgentRunWorkflow' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/run_workflow.go internal/agent/run_test.go
git commit -m "refactor(agent): add run workflow skeleton"
```

---

### Task 8: Move Loop Execution Into AgentRunWorkflow

**Files:**
- Modify: `internal/agent/run_workflow.go`
- Modify: `internal/agent/run.go`
- Modify: `internal/agent/run_test.go`

- [ ] **Step 1: Add workflow loop delegation test**

Append to `internal/agent/run_test.go`:

```go
type recordingAgentLoop struct {
	input AgentLoopInput
}

func (l *recordingAgentLoop) Execute(ctx context.Context, input AgentLoopInput) (AgentLoopResult, error) {
	l.input = input
	return AgentLoopResult{Text: "loop text"}, nil
}

func TestAgentRunWorkflowExecuteLoopDelegatesToAgentLoop(t *testing.T) {
	loop := &recordingAgentLoop{}
	workflow := NewAgentRunWorkflow(loop)
	input := AgentLoopInput{
		Model:    "model-1",
		Messages: []utils.Message{{Role: "user", Content: "hello"}},
	}

	result, err := workflow.ExecuteAgentLoop(t.Context(), input)

	require.NoError(t, err)
	assert.Equal(t, "loop text", result.Text)
	assert.Equal(t, "model-1", loop.input.Model)
	assert.Equal(t, "hello", loop.input.Messages[0].Content)
}
```

Add `github.com/knid/wemob/internal/utils` to imports if missing.

- [ ] **Step 2: Run test and verify failure**

Run:

```bash
go test ./internal/agent -run 'TestAgentRunWorkflowExecuteLoopDelegatesToAgentLoop' -count=1
```

Expected: compile failure because `ExecuteAgentLoop` is not defined.

- [ ] **Step 3: Implement ExecuteAgentLoop with telemetry**

Modify `internal/agent/run_workflow.go`:

```go
package agent

import "context"

type AgentRunWorkflow struct {
	AgentLoop AgentLoop
}

func NewAgentRunWorkflow(loop AgentLoop) *AgentRunWorkflow {
	if loop == nil {
		loop = DefaultAgentLoop{}
	}
	return &AgentRunWorkflow{AgentLoop: loop}
}

func (w *AgentRunWorkflow) ExecuteAgentLoop(ctx context.Context, input AgentLoopInput) (AgentLoopResult, error) {
	phaseCtx, span := startRuntimePhase(ctx, RuntimePhaseExecuteAgentLoop)
	defer span.End()
	return w.AgentLoop.Execute(phaseCtx, input)
}
```

- [ ] **Step 4: Route run.go loop through workflow**

In `internal/agent/run.go`, replace:

```go
loop := DefaultAgentLoop{}
```

with:

```go
workflow := NewAgentRunWorkflow(nil)
```

Keep the existing `loopResult, llmErr := ...` call's `AgentLoopInput` literal exactly as it was after Task 4; only change the receiver from `loop.Execute` to `workflow.ExecuteAgentLoop`.

- [ ] **Step 5: Run focused and integration tests**

Run:

```bash
go test ./internal/agent -run 'TestAgentRunWorkflowExecuteLoopDelegatesToAgentLoop|TestRunIntegrationWriteFilesExecutesToolAndCommits' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/agent/run_workflow.go internal/agent/run.go internal/agent/run_test.go
git commit -m "refactor(agent): delegate loop execution through workflow"
```

---

### Task 9: Add Phase Spans Around Existing Run Blocks

**Files:**
- Modify: `internal/agent/run.go`
- Modify: `internal/agent/runtime_telemetry.go`
- Modify: `internal/agent/runtime_telemetry_test.go`

- [ ] **Step 1: Add helper test for phase wrapper**

Append to `internal/agent/runtime_telemetry_test.go`:

```go
func TestWithRuntimePhaseReturnsFunctionResult(t *testing.T) {
	called := false
	err := withRuntimePhase(t.Context(), RuntimePhaseLoadProjectState, func(ctx context.Context) error {
		called = true
		return nil
	})

	assert.NoError(t, err)
	assert.True(t, called)
}
```

Add `context` import if needed.

- [ ] **Step 2: Run test and verify failure**

Run:

```bash
go test ./internal/agent -run 'TestWithRuntimePhaseReturnsFunctionResult' -count=1
```

Expected: compile failure because `withRuntimePhase` is not defined.

- [ ] **Step 3: Implement withRuntimePhase**

Modify `internal/agent/runtime_telemetry.go`:

```go
func withRuntimePhase(ctx context.Context, phase RuntimePhase, fn func(context.Context) error) error {
	phaseCtx, span := startRuntimePhase(ctx, phase)
	defer span.End()
	return fn(phaseCtx)
}
```

- [ ] **Step 4: Wrap existing code blocks in run.go**

Add phase wrappers around existing blocks without moving logic:

```go
if err := withRuntimePhase(ctx, RuntimePhaseLoadProjectState, func(phaseCtx context.Context) error {
	ctx = phaseCtx
	// existing repo open/template/read file/pre-run commit block
	return nil
}); err != nil {
	return err
}
```

Use this pattern only where it does not disrupt existing early returns. For blocks with many typed errors, prefer:

```go
phaseCtx, phaseSpan := startRuntimePhase(ctx, RuntimePhaseCompileRuntimeContext)
ctx = phaseCtx
// existing code
phaseSpan.End()
```

Required phase coverage in `run.go`:

- repo/project state load: `RuntimePhaseLoadProjectState`
- context compiler block: `RuntimePhaseCompileRuntimeContext`
- registry construction: `RuntimePhaseBuildRuntimeTools`
- assistant message persistence block: `RuntimePhasePersistAssistantTurn`
- success/failure finalization entry points: `RuntimePhaseFinalizeRun`

Do not wrap the whole function in a way that changes cancellation or defer ordering.

- [ ] **Step 5: Run tests**

Run:

```bash
go test ./internal/agent -run 'TestRuntimePhase|TestWithRuntimePhase|TestRunIntegrationWriteFilesExecutesToolAndCommits' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/agent/run.go internal/agent/runtime_telemetry.go internal/agent/runtime_telemetry_test.go
git commit -m "refactor(agent): add runtime phase spans"
```

---

### Task 10: Extract Context Compilation Boundary

**Files:**
- Create: `internal/agent/runtime_context.go`
- Modify: `internal/agent/run.go`
- Modify: `internal/agent/context_compiler_test.go`

- [ ] **Step 1: Add a boundary test**

Append to `internal/agent/context_compiler_test.go`:

```go
func TestRuntimeContextCompilerReturnsContextPacket(t *testing.T) {
	project := &models.AppProject{ProjectType: models.ProjectTypeApp, Status: "ready"}
	compiler := RuntimeContextCompiler{}

	packet := compiler.Compile(AgentContextInput{
		Context:    context.Background(),
		Project:    project,
		Message:    "make it nicer",
		IsFirstGen: false,
	})

	require.NotNil(t, packet.Profile)
	require.NotEmpty(t, packet.SystemMessages)
}
```

Add imports if missing:

```go
import (
	"context"
	"github.com/knid/wemob/internal/models"
	"github.com/stretchr/testify/require"
)
```

- [ ] **Step 2: Run test and verify failure**

Run:

```bash
go test ./internal/agent -run 'TestRuntimeContextCompilerReturnsContextPacket' -count=1
```

Expected: compile failure because `RuntimeContextCompiler` is not defined.

- [ ] **Step 3: Implement compiler wrapper**

Create `internal/agent/runtime_context.go`:

```go
package agent

type RuntimeContextCompiler struct{}

func (RuntimeContextCompiler) Compile(input AgentContextInput) AgentContextPacket {
	return CompileAgentContext(input)
}
```

- [ ] **Step 4: Use compiler wrapper in run.go**

Replace:

```go
contextPacket := CompileAgentContext(AgentContextInput{
	Context:         ctx,
	Project:         project,
	Deps:            deps,
	AttachmentsPool: turnAttachments.Pool,
	Message:         message,
	IsFirstGen:      isFirstGen,
})
```

with:

```go
contextCompiler := RuntimeContextCompiler{}
contextPacket := contextCompiler.Compile(AgentContextInput{
	Context:         ctx,
	Project:         project,
	Deps:            deps,
	AttachmentsPool: turnAttachments.Pool,
	Message:         message,
	IsFirstGen:      isFirstGen,
})
```

Do not change any fields passed to `AgentContextInput`.

- [ ] **Step 5: Run tests**

Run:

```bash
go test ./internal/agent -run 'TestRuntimeContextCompilerReturnsContextPacket|TestCompileAgentContextUsesLangfusePromptResolver|TestRunIntegrationWriteFilesExecutesToolAndCommits' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/agent/runtime_context.go internal/agent/run.go internal/agent/context_compiler_test.go
git commit -m "refactor(agent): wrap runtime context compilation"
```

---

### Task 11: Extract Success Finalizer Facade

**Files:**
- Create: `internal/agent/run_finalizer.go`
- Modify: `internal/agent/run.go`
- Modify: `internal/agent/run_test.go`

- [ ] **Step 1: Add finalizer construction test**

Append to `internal/agent/run_test.go`:

```go
func TestRunFinalizerStoresDependencies(t *testing.T) {
	deps := &RunDependencies{}
	finalizer := NewRunFinalizer(deps)

	assert.Same(t, deps, finalizer.Deps)
}
```

- [ ] **Step 2: Run test and verify failure**

Run:

```bash
go test ./internal/agent -run 'TestRunFinalizerStoresDependencies' -count=1
```

Expected: compile failure because `NewRunFinalizer` is not defined.

- [ ] **Step 3: Implement finalizer facade**

Create `internal/agent/run_finalizer.go`:

```go
package agent

type RunFinalizer struct {
	Deps *RunDependencies
}

func NewRunFinalizer(deps *RunDependencies) RunFinalizer {
	return RunFinalizer{Deps: deps}
}
```

- [ ] **Step 4: Move success finalizer call behind a facade method**

Replace `internal/agent/run_finalizer.go` with:

```go
package agent

import (
	"context"
	"time"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/gitrepo"
	"github.com/knid/wemob/internal/utils"
)

type RunFinalizer struct {
	Deps *RunDependencies
}

func NewRunFinalizer(deps *RunDependencies) RunFinalizer {
	return RunFinalizer{Deps: deps}
}

type FinalizeSuccessInput struct {
	Ctx             context.Context
	Repo            *gitrepo.Repo
	Run             *models.AppAgentRun
	Emitter         EventEmitter
	FileMap         *SafeFileMap
	Parts           []models.ChatMessagePart
	Tracker         *utils.TokenTracker
	RunStart        time.Time
	TotalToolCalls  int
	Turns           int
	UserMessage     string
	User            *models.User
	Project         *models.AppProject
	LangfuseTraceID string
}

func (f RunFinalizer) Success(input FinalizeSuccessInput) error {
	return finaliseSuccess(
		input.Ctx,
		f.Deps,
		input.Repo,
		input.Run,
		input.Emitter,
		input.FileMap,
		input.Parts,
		input.Tracker,
		input.RunStart,
		input.TotalToolCalls,
		input.Turns,
		input.UserMessage,
		input.User,
		input.Project,
		input.LangfuseTraceID,
	)
}
```

- [ ] **Step 5: Route success path through RunFinalizer**

In `internal/agent/run.go`, replace the direct `finaliseSuccess(...)` call near the end of `Run`:

```go
return finaliseSuccess(ctx, deps, repo, run, emitter, fileMap,
	parts, tracker, runStart, totalToolCalls, turn, message, user, project, langfuseTraceID)
```

with:

```go
finalizer := NewRunFinalizer(deps)
return finalizer.Success(FinalizeSuccessInput{
	Ctx:             ctx,
	Repo:            repo,
	Run:             run,
	Emitter:         emitter,
	FileMap:         fileMap,
	Parts:           parts,
	Tracker:         tracker,
	RunStart:        runStart,
	TotalToolCalls:  totalToolCalls,
	Turns:           turn,
	UserMessage:     message,
	User:            user,
	Project:         project,
	LangfuseTraceID: langfuseTraceID,
})
```

- [ ] **Step 6: Run tests**

Run:

```bash
go test ./internal/agent -run 'TestRunFinalizerStoresDependencies|TestRunIntegrationWriteFilesExecutesToolAndCommits|TestRunIntegrationToolMatrixReadSearchPatchDelete' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/agent/run_finalizer.go internal/agent/run.go internal/agent/run_test.go
git commit -m "refactor(agent): introduce success finalizer facade"
```

---

### Task 12: Final Regression and Trace Vocabulary Review

**Files:**
- Modify only if tests reveal issues.

- [ ] **Step 1: Run focused package tests**

Run:

```bash
go test ./internal/agent ./internal/utils ./internal/observability -count=1
```

Expected: PASS.

- [ ] **Step 2: Run broader backend tests**

Run:

```bash
go test ./internal/... -count=1
```

Expected: PASS. If unrelated integration tests require unavailable services, document exact failures and rerun the focused packages from Step 1.

- [ ] **Step 3: Inspect trace vocabulary in code**

Run:

```bash
rg -n 'stream_with_tools|execute-agent-loop|compile-runtime-context|execute-tool|RuntimePhase' internal/agent internal/utils internal/observability
```

Expected:

- `stream_with_tools` remains only as low-level metadata or internal `utils` span source.
- runtime phase names appear in `internal/agent/runtime_telemetry.go` and `run.go`.
- tool observations are emitted through `ToolGateway` and `observability.RecordTool`.

- [ ] **Step 4: Inspect git diff**

Run:

```bash
git diff --stat
git diff -- internal/agent internal/utils internal/observability
```

Expected:

- Changes are refactor-only.
- No prompt text changes.
- No model selection changes.
- No SSE event type renames.
- No tool schema changes.

- [ ] **Step 5: Commit final cleanup if needed**

If Step 4 shows any formatting or small cleanup changes:

```bash
git add internal/agent internal/utils internal/observability
git commit -m "refactor(agent): finalize runtime harness phase 1"
```

If there are no additional changes, do not create an empty commit.

---

## Implementation Notes

- Keep each commit behavior-preserving.
- Prefer `go test ./internal/agent -run '<focused tests>' -count=1` after every extraction.
- Do not rename SSE event constants in Phase 1.
- Do not rewrite `utils.StreamWithTools`; wrap it.
- Do not change prompt text or prompt selection.
- Do not change `AppAgentRun` schema in Phase 1.
- If an extraction requires changing behavior to pass tests, stop and split that behavior change into a separate design.

## Self-Review Checklist

- Spec coverage: This plan covers runtime phases, `RunExecution`, `AgentLoop`, `ToolGateway`, context compiler boundary, finalizer facade, and phase telemetry.
- Non-goals preserved: The plan does not add planning, subagents, HITL, durable workflows, prompt rewrites, model changes, new UX, verification behavior, tool schema redesign, or context compression.
- Behavior preservation: Each task has focused tests and integration checks before committing.
- Known risk: Task 11 only routes the success path through `RunFinalizer`. Failure path extraction stays out of scope for this plan unless the success-path facade lands cleanly and tests stay stable.
