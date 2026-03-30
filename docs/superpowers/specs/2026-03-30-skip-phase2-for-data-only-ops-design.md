# Skip Phase 2 Code Generation for Data-Only Operations

**Date**: 2026-03-30
**Status**: Approved

## Problem

The app builder chat pipeline always runs Phase 2 (main model code generation) after Phase 1 (fast model tool-calling), even when Phase 1 fully handled the request with data-only tools (store updates, SQL, env vars).

For a simple "change price to $229" request, Phase 1 calls `manage_store` and completes the update. Phase 2 then fires with ~59K input tokens (system prompt + full source code of 6 files) just to produce a 246-token summary. That single call costs $0.227 — 86% of the trace's total cost — for zero value.

## Solution

Let the Phase 1 fast model decide whether code changes are still needed after its tool calls. When they aren't, skip Phase 2 entirely and use Phase 1's streamed text as the final response.

## Design

### 1. Add `SkipPhase2` to `chatToolPhaseResult`

```go
type chatToolPhaseResult struct {
    Context     string
    Screenshots []string
    SkipPhase2  bool   // true when Phase 1 fully handled the request
}
```

### 2. Phase 1 system prompt addition

Add after the existing `NO_TOOLS_NEEDED` instruction block:

```
- After completing ALL tool calls, evaluate whether the user's request ALSO requires
  code file changes (editing .tsx/.ts files). If the tools fully handled the request
  (e.g. store updates, database changes, env var changes, image generation) and NO
  code edits are needed, write a helpful summary response for the user — do NOT
  output CODE_CHANGES_NEEDED. If code changes ARE still needed after the tool calls,
  end your final response with the exact text on its own line: CODE_CHANGES_NEEDED
```

### 3. Result parsing in `runChatToolPhase`

When `collectedContext.Len() > 0` (tools were called and produced context), check the model's final text:

- If result does NOT contain `CODE_CHANGES_NEEDED` → set `SkipPhase2 = true`
- If result contains `CODE_CHANGES_NEEDED` → strip it from the result text, set `SkipPhase2 = false`

When `collectedContext.Len() == 0`, keep existing behavior (`SkipPhase2` stays false).

### 4. Controller change (`app_builder_controller.go`)

After `runChatToolPhase` returns, before Phase 2:

```go
if toolResult.SkipPhase2 {
    slog.Info("Skipping Phase 2: data-only operation handled by Phase 1",
        "project_id", project.ID)
    responseText = "" // Phase 1 already streamed text to user
    // Fall through to conversation save — skip Phase 2 and post-processing
} else {
    // existing Phase 2 code (refresh context, inject tool context, run StreamText)
}
```

The `responseText` for conversation persistence uses the Phase 1 streamed text. Since the existing `phase1TextBuf` / `phase1TextFlushed` mechanism already streams text to the user in real-time, and the `result` from `StreamWithTools` captures the final model output, we use `result` (with `CODE_CHANGES_NEEDED` stripped) as the saved response text.

### 5. Phase 1 text as response text

When `SkipPhase2 == true`, the `result` string from `StreamWithTools` (after stripping `CODE_CHANGES_NEEDED`) becomes `responseText`. This is the text the fast model produced after tool calls — it was already streamed to the user and will be saved in conversation history.

## Edge Cases

| Scenario | Phase 1 behavior | Phase 2? |
|----------|------------------|----------|
| "change price to $229" | Calls `manage_store`, outputs summary | Skipped |
| "add 3 products to the store" | Calls `manage_store`, outputs summary | Skipped |
| "seed the database" | Calls `seed_database`, outputs summary | Skipped |
| "change price and make the card bigger" | Calls `manage_store`, outputs `CODE_CHANGES_NEEDED` | Runs |
| "change background to blue" | No tools → `NO_TOOLS_NEEDED` | Runs |
| "show me a website preview" | Calls `preview_website` → context for code gen | Runs (has context for Phase 2) |
| False skip (model incorrectly skips) | User retries; no worse than a bad Phase 2 response | N/A |
| False continue (model says CODE_CHANGES_NEEDED unnecessarily) | Falls back to current behavior, no regression | Runs |

## Files to Modify

1. `internal/controllers/app_builder_tools.go` — struct + prompt + result parsing
2. `internal/controllers/app_builder_controller.go` — conditional Phase 2 skip

## Cost Impact

- Per store-only operation: ~$0.22 saved (86% of trace cost)
- Latency reduction: ~6.5s saved (the Phase 2 call duration)
- No regression for code-edit requests — identical to current behavior
