# LLM Continuation Cost Optimization

**Date:** 2026-04-02
**Status:** Draft
**Triggered by:** PostHog trace `7fbc9c06-192c-478f-a27a-be929cee04a1` — $0.75 wasted on Phase 1 continuation loop

## Problem

When `StreamWithTools` receives `finish_reason=length` (output hit `MaxTokens`), it auto-continues by appending the partial response + a "Continue EXACTLY where you left off" user message and re-entering the loop. This continuation shares the same `maxRounds` counter as tool-calling rounds.

In the traced incident, Phase 1 (chat tool phase) was configured with `MaxTokens: 4096`. The model output `NO_TOOLS_NEEDED` followed by a full code rewrite, hit the 4096 cap, and triggered 8 continuation rounds. All generated text was discarded by `runChatToolPhase` because `NO_TOOLS_NEEDED` was detected. Input tokens grew from 19K to 48K across the loop (quadratic accumulation). **87% of the total cost ($0.65 of $0.75) was wasted.**

The same `MaxTokens: 4096` configuration exists in the ecommerce setup phase (`app_builder_generation.go:651`), which is vulnerable to the same pattern.

## Root Causes

1. **No separate continuation limit** — tool rounds and continuation rounds share `maxRounds`, allowing many continuations before exhaustion.
2. **No caller abort mechanism** — once Phase 1 detects `NO_TOOLS_NEEDED` via `onChunk`, it sets `phase1Suppressed = true` but has no way to tell `StreamWithTools` to stop looping.
3. **`MaxTokens` too low for text responses** — 4096 tokens is sufficient for tool-call JSON but not for text responses (NO_CODE_NEEDED answers, planning messages). Truncation triggers the continuation loop.

## Design

### Change 1: Add `MaxContinuations` to `ToolCallConfig`

**File:** `internal/utils/ai.go`

Add a new field to `ToolCallConfig`:

```go
type ToolCallConfig struct {
    // ... existing fields ...
    MaxContinuations  int  // Max finish_reason=length continuation rounds (default 3). 0 = no continuations. -1 = unlimited (use maxRounds).
}
```

Semantics:
- Unset (0): use default of 3 continuations
- Positive: use that value as the limit
- -1: disable continuations entirely (never continue on `finish_reason=length`)

In `StreamWithTools`, track continuations with a separate counter:

In the continuation block (`ai.go:2166`):

```go
if lastFinishReason == "length" && round < maxRounds-1 {
    if maxContinuations < 0 || continuationCount >= maxContinuations {
        slog.Warn("LLM response truncated, continuation limit reached",
            "continuations", continuationCount, "max", maxContinuations)
        return fullText.String(), ErrResponseTruncated
    }
    continuationCount++
    // ... existing continuation logic ...
}
```

**Callers:**

| Caller | Current | New |
|--------|---------|-----|
| Phase 1 chat tools (`app_builder_tools.go:399`) | (default) | `MaxContinuations: -1` (disabled) |
| Ecommerce setup (`app_builder_generation.go:651`) | (default) | `MaxContinuations: 1` |
| All other callers | (default) | No change (default 3) |

Phase 1 gets `-1` (disabled) because:
- If it outputs `NO_TOOLS_NEEDED`, the text is discarded anyway — continuation is pure waste.
- If it outputs `NO_CODE_NEEDED`, the response should fit in the bumped 8192 token limit. If it doesn't, a truncated friendly response is better than burning 8 continuation rounds.
- Tool-call JSON never needs continuation — it's always compact.

### Change 2: Add `ShouldContinue` callback to `ToolCallConfig`

**File:** `internal/utils/ai.go`

```go
type ToolCallConfig struct {
    // ... existing fields ...
    ShouldContinue func() bool // Called before each continuation. Return false to abort. nil = always continue.
}
```

Checked in the continuation block, BEFORE the `MaxContinuations` check:

```go
if lastFinishReason == "length" && round < maxRounds-1 {
    // Check caller abort signal first
    if len(opts) > 0 && opts[0].ShouldContinue != nil && !opts[0].ShouldContinue() {
        slog.Info("Caller aborted continuation via ShouldContinue callback")
        return fullText.String(), nil // not an error — caller chose to stop
    }
    if maxContinuations < 0 || continuationCount >= maxContinuations {
        // ... limit reached ...
    }
    // ... continue ...
}
```

**Also check before each tool-calling round** (not just continuations) — if the caller knows the output is being discarded, there's no point in further tool rounds either:

```go
for round := 0; round < maxRounds; round++ {
    // Check abort before starting any round
    if len(opts) > 0 && opts[0].ShouldContinue != nil && !opts[0].ShouldContinue() {
        slog.Info("Caller aborted StreamWithTools via ShouldContinue", "round", round)
        return fullText.String(), nil
    }
    // ... rest of round logic ...
}
```

**Phase 1 caller** (`app_builder_tools.go`):

```go
utils.ToolCallConfig{
    MaxTokens:        8192,
    MaxContinuations: -1,
    ShouldContinue: func() bool {
        phase1Mu.Lock()
        defer phase1Mu.Unlock()
        return !phase1Suppressed
    },
    // ... rest ...
}
```

This provides defense-in-depth: even if `MaxContinuations` were misconfigured, the `ShouldContinue` callback would stop the loop once `NO_TOOLS_NEEDED` is detected.

### Change 3: Bump Phase 1 `MaxTokens` to 8192

**File:** `internal/controllers/app_builder_tools.go:399`

```go
// Before:
MaxTokens: 4096,

// After:
MaxTokens: 8192,
```

Rationale:
- Tool-call JSON: typically 100-500 tokens. 8192 is more than enough.
- `NO_CODE_NEEDED` conversational responses: typically 200-2000 tokens. 8192 covers virtually all cases.
- `NO_TOOLS_NEEDED` signal: only needs a few tokens but the model sometimes adds text after. With `MaxContinuations: -1`, any truncation is clean.
- Phase 2 uses 65536. Phase 1 doesn't need anywhere near that since it shouldn't be generating code.

### Change 4: Bump ecommerce setup `MaxTokens` to 8192

**File:** `internal/controllers/app_builder_generation.go:651`

```go
// Before:
MaxTokens: 4096,

// After:
MaxTokens: 8192,
```

The ecommerce setup phase calls `setup_ecommerce` and `manage_store` tools. Tool-call JSON fits easily in 4096, but the model sometimes generates summary text between tool calls. 8192 prevents truncation without being wasteful. Combined with `MaxContinuations: 1`, at most one continuation is allowed if the summary is long.

## Files Changed

| File | Change |
|------|--------|
| `internal/utils/ai.go` | Add `MaxContinuations` and `ShouldContinue` to `ToolCallConfig`. Add continuation counter and abort checks in `StreamWithTools`. |
| `internal/controllers/app_builder_tools.go` | Bump `MaxTokens` to 8192, set `MaxContinuations: -1`, add `ShouldContinue` callback. |
| `internal/controllers/app_builder_generation.go` | Bump `MaxTokens` to 8192, set `MaxContinuations: 1`. |

## Expected Impact

For the traced incident pattern:
- **Before:** 13 LLM calls, 317K input tokens, $0.75, 820 seconds
- **After:** 6 LLM calls (5 tool rounds + 1 text response that gets truncated but not continued), ~60K input tokens, ~$0.10, ~20 seconds

The `ShouldContinue` callback would abort even earlier — as soon as `NO_TOOLS_NEEDED` is detected in the `onChunk` stream (mid-response), the next round check would exit. In practice this means Phase 1 would stop after the first text response that contains `NO_TOOLS_NEEDED`, regardless of whether it was truncated.

## Non-Goals

- Changing the `NO_TOOLS_NEEDED` signal mechanism (keeping magic text, not converting to tool call)
- Adding a token budget system to `StreamWithTools`
- Splitting `maxRounds` into `MaxToolRounds` + `MaxContinuationRounds` (the combination of `MaxContinuations` + `ShouldContinue` achieves the same result with less API churn)
- Modifying `StreamTextFromLLMWithMessages` (Phase 2) — it doesn't have the continuation loop issue since it returns `ErrResponseTruncated` and lets the caller decide
