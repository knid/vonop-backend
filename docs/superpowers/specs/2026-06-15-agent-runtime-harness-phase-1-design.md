# Agent Runtime Harness Phase 1 Design

## Purpose

Wemob's current agent runtime works, but the engineering shape is too implicit. The main execution path is centered on a large `agent.Run` function and an internal `StreamWithTools` loop. Langfuse is exposing this shape as traces like `app_chat -> stream_with_tools`, which is technically valid but not a professional runtime model.

Phase 1 turns the existing runtime into an explicit agent harness without intentionally changing model behavior, prompts, tool availability, persistence semantics, credit accounting, or user-facing output.

The target is internal engineering quality: clearer ownership, cleaner observability, easier debugging, and safer future changes.

## Design Principles

- Preserve behavior first. The refactor should not change agent decisions, prompts, model selection, available tools, SSE event shape, or final output.
- Make phases explicit. Runtime phases should be named in code and traces.
- Hide implementation helpers. `StreamWithTools` remains usable internally, but it should not be the conceptual runtime boundary.
- Keep boundaries small. Each extracted component should have one job and typed inputs/outputs.
- Improve observability as a consequence of architecture, not as a cosmetic layer.

## Current Shape

The current path is broadly:

```text
agent.Run
  -> enforce active-run invariant
  -> open repo and read files
  -> create AppAgentRun
  -> load and persist chat messages
  -> compile system/context messages
  -> create registry and tracker
  -> create inline tool executor closure
  -> call utils.StreamWithTools
  -> persist assistant turn
  -> finalize success or failure
  -> record usage, scores, telemetry
```

This puts orchestration, state, context assembly, tool governance, model loop callbacks, persistence, memory updates, telemetry, and finalization into one execution surface.

## Target Shape

Phase 1 introduces a Wemob-owned harness:

```text
AgentRunWorkflow
  -> InitializeRun
  -> LoadProjectState
  -> CompileRuntimeContext
  -> BuildRuntimeTools
  -> ExecuteAgentLoop
  -> PersistAssistantTurn
  -> FinalizeSuccess / FinalizeFailure
  -> RecordUsageAndScores
```

`agent.Run` becomes a thin entry point that constructs and invokes this workflow.

## Components

### RunExecution

Introduce an internal run-scoped state object, tentatively:

```go
type RunExecution struct {
    Ctx context.Context
    Deps *RunDependencies
    Project *models.AppProject
    User *models.User
    Run *models.AppAgentRun
    RunID uuid.UUID
    Repo gitrepo.Repository
    FileMap *SafeFileMap
    Tracker *utils.TokenTracker
    Emitter EventEmitter
    Registry *Registry
    Todos *TodoList
    Memory *AgentRunMemory
    BuildState *BuildState
}
```

Exact field names can follow existing types. The purpose is to reduce local-variable sprawl in `Run` and make phase methods explicit.

### AgentRunWorkflow

Owns phase order and error routing. It should be boring and readable:

```go
func (w *AgentRunWorkflow) Run(ctx context.Context, input RunInput) error {
    exec, err := w.InitializeRun(ctx, input)
    if err != nil { return err }

    if err := w.LoadProjectState(exec); err != nil { return w.Fail(exec, err) }
    if err := w.CompileRuntimeContext(exec); err != nil { return w.Fail(exec, err) }
    if err := w.BuildRuntimeTools(exec); err != nil { return w.Fail(exec, err) }

    loopResult, err := w.AgentLoop.Execute(exec.Ctx, loopInput(exec))
    if err != nil { return w.Fail(exec, err) }

    if err := w.PersistAssistantTurn(exec, loopResult); err != nil { return w.Fail(exec, err) }
    return w.Succeed(exec, loopResult)
}
```

This is illustrative, not a required exact API.

### AgentLoop

Wrap the current `utils.StreamWithTools` call behind a Wemob-owned interface:

```go
type AgentLoop interface {
    Execute(ctx context.Context, input AgentLoopInput) (AgentLoopResult, error)
}
```

For Phase 1, the default implementation should still call `utils.StreamWithTools`. This gives future room for planning, multi-agent execution, replay, or alternative loop engines without making `agent.Run` depend on the low-level loop.

### ToolGateway

Extract the inline tool executor closure from `agent.Run`.

Responsibilities:

- Generate tool call IDs.
- Record `tracker.RecordToolUsed`.
- Append and update assistant message parts.
- Emit `tool_call_start` and `tool_call_result` events.
- Classify tool risk.
- Execute the registry tool.
- Record Langfuse `TOOL` observations.
- Persist memory updates.
- Normalize tool errors.

This should be the only path through which model-requested tools execute.

### RuntimeContextCompiler

Wrap the current context assembly path around `CompileAgentContext`.

Responsibilities:

- Build system messages.
- Track required and loaded skills.
- Select prompt links.
- Prepare prompt-part metadata.
- Preserve current prompt fallback behavior.

Phase 1 does not rewrite prompt content or context budgeting. It only creates a clean boundary for later work.

### RunFinalizer

Centralize success and failure finalization.

Responsibilities:

- Persist assistant messages.
- Update `AppAgentRun`.
- Commit success or checkpoint progress according to current rules.
- Record token usage and cost.
- Emit final events.
- Record Langfuse scores.
- Record failure status and error metadata.

Existing `finaliseSuccessV2` and `finaliseFailureV2` behavior should be preserved, but their ownership should become clearer.

## Observability

Phase spans should match the runtime architecture:

- `initialize-run`
- `load-project-state`
- `compile-runtime-context`
- `build-runtime-tools`
- `execute-agent-loop`
- `generate-agent-response`
- `execute-tool`
- `persist-assistant-turn`
- `finalize-run`
- `record-usage-and-scores`

LLM calls remain `GENERATION` observations. Tool calls are `TOOL` observations. Root traces remain `generate_app` or `app_chat`.

This makes Langfuse a faithful view of the runtime rather than a list of helper function names.

## Non-Goals

Phase 1 does not add:

- New planning behavior.
- Subagents.
- Human approval flows.
- A durable workflow engine.
- Prompt rewrites.
- New model selection behavior.
- New customer-facing UI.
- New verification behavior beyond preserving what currently exists.
- Tool schema redesign.
- Context compression changes.

These are later phases once the harness boundaries exist.

## Behavior Preservation Requirements

The refactor must preserve:

- Existing model provider and model resolution.
- Existing prompt composition and fallback behavior.
- Existing tool registry contents.
- Existing tool execution semantics.
- Existing SSE event types and payloads.
- Existing chat message persistence.
- Existing `AppAgentRun` status transitions.
- Existing cost and credit accounting.
- Existing success/failure finalization rules.
- Existing error classification as much as possible.

Any intentional behavior change discovered during implementation must be split into a separate follow-up change.

## Testing Strategy

Use existing tests as regression coverage, then add focused unit tests around new boundaries.

Required verification:

- `go test ./internal/agent`
- `go test ./internal/utils`
- `go test ./internal/observability`

Focused additions:

- `AgentRunWorkflow` phase ordering tests using fakes.
- `ToolGateway` tests for event emission, memory update, error handling, and Langfuse tool observation attributes.
- `AgentLoop` adapter tests proving it passes through `StreamWithTools` inputs and outputs without semantic changes.
- Finalizer tests for success, failure, cancellation, and budget-exceeded paths.

Integration tests should continue to prove:

- First-generation run completes.
- Chat edit run completes.
- Tool matrix still works.
- Tool error run is classified correctly.
- Partial progress/checkpoint behavior is preserved.

## Rollout Plan

1. Add types and interfaces behind the existing runtime.
2. Extract `RunExecution` state without changing behavior.
3. Extract `ToolGateway` from the inline executor closure.
4. Extract `AgentLoop` adapter around `utils.StreamWithTools`.
5. Extract context and finalization ownership.
6. Add phase spans.
7. Run full focused regression tests.

Each step should be small enough to review independently.

## Risks

- Accidental behavior drift from moving code.
- Missing SSE event ordering changes.
- Subtle token accounting or credit accounting changes.
- Context cancellation behavior changing across extracted components.
- More abstractions than needed if interfaces are introduced too early.

Mitigation: extract one boundary at a time, keep interfaces narrow, and run existing integration tests after each meaningful step.

## Open Decisions

- Exact package layout: keep all harness pieces in `internal/agent` initially, then split only if files become too large.
- Exact naming: use product/runtime names rather than framework names.
- Whether phase spans should be emitted from each component directly or via a small runtime tracer helper.

The recommended default is to keep everything in `internal/agent` for Phase 1 and avoid a broad package split until the extracted files prove their shape.
