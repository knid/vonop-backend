# LLM Continuation Cost Optimization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prevent runaway LLM continuation loops in `StreamWithTools` that waste tokens and money when Phase 1 detects `NO_TOOLS_NEEDED` but the model keeps generating.

**Architecture:** Add `MaxContinuations` and `ShouldContinue` fields to `ToolCallConfig` in `ai.go`. Wire them into the continuation block of `StreamWithTools`. Update Phase 1 and ecommerce setup callers to use the new fields and bump `MaxTokens` from 4096 to 8192.

**Tech Stack:** Go, no new dependencies

**Spec:** `docs/superpowers/specs/2026-04-02-llm-continuation-cost-optimization-design.md`

---

## File Map

| File | Action | Responsibility |
|------|--------|----------------|
| `internal/utils/ai.go` | Modify | Add `MaxContinuations` + `ShouldContinue` to `ToolCallConfig`, wire into `StreamWithTools` loop |
| `internal/controllers/app_builder_tools.go` | Modify | Set `MaxTokens: 8192`, `MaxContinuations: -1`, `ShouldContinue` callback in Phase 1 config |
| `internal/controllers/app_builder_generation.go` | Modify | Set `MaxTokens: 8192`, `MaxContinuations: 1` in ecommerce setup config |

---

### Task 1: Add `MaxContinuations` and `ShouldContinue` to `ToolCallConfig`

**Files:**
- Modify: `internal/utils/ai.go:1503-1517` (struct definition)

- [ ] **Step 1: Add the two new fields to `ToolCallConfig`**

In `internal/utils/ai.go`, add two fields at the end of the `ToolCallConfig` struct (before the closing brace on line 1517):

```go
	MaxContinuations  int            // Max finish_reason=length continuation rounds (default 3). -1 = disable continuations entirely.
	ShouldContinue    func() bool    // Called before each round/continuation. Return false to abort. nil = always continue.
```

The struct after editing should look like:

```go
type ToolCallConfig struct {
	MaxRounds         int                                                    // Maximum tool-calling rounds (default 10)
	NudgeAfter        int                                                    // Inject nudge after this many consecutive tool rounds (default 3)
	StripAfter        int                                                    // Strip tools entirely after this many consecutive rounds (default 5)
	MaxTokens         int                                                    // Max completion tokens per round (default 16384)
	TrimArgs          func(toolName, args string) string                     // Trim tool call arguments in history to reduce token usage
	TrimResults       func(toolName, result string) string                   // Trim tool result messages in history to reduce token usage
	InterceptToolCall func(toolName, toolArgs string) (result string, skip bool) // Intercept tool calls before execution; if skip=true, use result and skip executor
	NudgeMessage      string                                                 // Custom nudge message (replaces default "stop researching" text)
	AfterToolRound    func(allIntercepted bool) string                       // Called after each tool round; allIntercepted=true when every call was intercepted; non-empty return is injected as a system message
	FilterTools       func(tools []Tool) []Tool                              // Called each round to filter/modify tools; return nil to strip all tools
	OnToolCallStart   func(toolName string)                                  // Called when a tool call name is first detected during SSE streaming (before args are complete)
	OnRoundStart      func(round int)                                       // Called at the start of each round (useful for resetting per-round state like text buffers)
	MaxContinuations  int            // Max finish_reason=length continuation rounds (default 3). -1 = disable continuations entirely.
	ShouldContinue    func() bool    // Called before each round/continuation. Return false to abort. nil = always continue.
}
```

- [ ] **Step 2: Verify build**

Run: `go build ./...`
Expected: Success (new fields are zero-value compatible — no callers break)

- [ ] **Step 3: Commit**

```bash
git add internal/utils/ai.go
git commit -m "feat: add MaxContinuations and ShouldContinue to ToolCallConfig"
```

---

### Task 2: Wire `MaxContinuations` into `StreamWithTools` loop

**Files:**
- Modify: `internal/utils/ai.go:1538-1554` (option parsing), `1565-1569` (loop variables), `2164-2189` (continuation block)

- [ ] **Step 1: Parse `MaxContinuations` from opts**

In `internal/utils/ai.go`, inside the `if len(opts) > 0 {` block (after line 1554 where `maxTokens` is set), add:

```go
		if opts[0].MaxContinuations != 0 {
			maxContinuations = opts[0].MaxContinuations
		}
```

And declare the `maxContinuations` default alongside the other defaults. After line 1541 (`maxTokens := 16384`), add:

```go
	maxContinuations := 3
```

The full defaults block should read:

```go
	maxRounds := 10
	nudgeAfter := 3
	stripAfter := 5
	maxTokens := 16384
	maxContinuations := 3
	if len(opts) > 0 {
		if opts[0].MaxRounds > 0 {
			maxRounds = opts[0].MaxRounds
		}
		if opts[0].NudgeAfter > 0 {
			nudgeAfter = opts[0].NudgeAfter
		}
		if opts[0].StripAfter > 0 {
			stripAfter = opts[0].StripAfter
		}
		if opts[0].MaxTokens > 0 {
			maxTokens = opts[0].MaxTokens
		}
		if opts[0].MaxContinuations != 0 {
			maxContinuations = opts[0].MaxContinuations
		}
	}
```

- [ ] **Step 2: Add continuation counter to loop variables**

After line 1567 (`prevToolResultRange := [2]int{0, 0}`), add:

```go
	continuationCount := 0
```

- [ ] **Step 3: Replace the continuation block**

Replace the continuation logic at lines 2164-2189 (the `// No tool calls — return final response` block). The current code is:

```go
		// Chunks were already streamed in real-time via onChunk above.
		// No tool calls — return final response
		if lastFinishReason == "length" && round < maxRounds-1 {
			if fullText.Len() > maxResponseBufferSize {
				slog.Warn("response exceeded max size during continuation, returning partial", "size", fullText.Len())
				return fullText.String(), nil
			}
			// Response was truncated — request continuation by appending the partial
			// response as assistant message and asking the LLM to continue.
			slog.Warn("LLM response truncated (finish_reason=length), requesting continuation", "response_length", fullText.Len(), "round", round)
			currentMessages = append(currentMessages, Message{
				Role:    "assistant",
				Content: fullText.String(),
			})
			currentMessages = append(currentMessages, Message{
				Role:    "user",
				Content: "Your response was cut off. Continue EXACTLY where you left off. Do not repeat any content.",
			})
			continue
		}

		if lastFinishReason == "length" {
			slog.Warn("LLM response truncated (finish_reason=length), no rounds left", "response_length", fullText.Len(), "round", round)
		}

		return fullText.String(), nil
```

Replace with:

```go
		// Chunks were already streamed in real-time via onChunk above.
		// No tool calls — return final response
		if lastFinishReason == "length" && round < maxRounds-1 {
			// Check caller abort signal before continuing
			if len(opts) > 0 && opts[0].ShouldContinue != nil && !opts[0].ShouldContinue() {
				slog.Info("Caller aborted continuation via ShouldContinue", "round", round, "response_length", fullText.Len())
				return fullText.String(), nil
			}
			// Check continuation limit
			if maxContinuations < 0 || continuationCount >= maxContinuations {
				slog.Warn("LLM response truncated, continuation limit reached",
					"continuations", continuationCount, "max", maxContinuations, "response_length", fullText.Len())
				return fullText.String(), ErrResponseTruncated
			}
			if fullText.Len() > maxResponseBufferSize {
				slog.Warn("response exceeded max size during continuation, returning partial", "size", fullText.Len())
				return fullText.String(), nil
			}
			// Response was truncated — request continuation by appending the partial
			// response as assistant message and asking the LLM to continue.
			continuationCount++
			slog.Warn("LLM response truncated (finish_reason=length), requesting continuation",
				"response_length", fullText.Len(), "round", round, "continuation", continuationCount, "max", maxContinuations)
			currentMessages = append(currentMessages, Message{
				Role:    "assistant",
				Content: fullText.String(),
			})
			currentMessages = append(currentMessages, Message{
				Role:    "user",
				Content: "Your response was cut off. Continue EXACTLY where you left off. Do not repeat any content.",
			})
			continue
		}

		if lastFinishReason == "length" {
			slog.Warn("LLM response truncated (finish_reason=length), no rounds left", "response_length", fullText.Len(), "round", round)
		}

		return fullText.String(), nil
```

- [ ] **Step 4: Verify build**

Run: `go build ./...`
Expected: Success

- [ ] **Step 5: Commit**

```bash
git add internal/utils/ai.go
git commit -m "feat: enforce MaxContinuations limit in StreamWithTools continuation loop"
```

---

### Task 3: Wire `ShouldContinue` into the round loop top

**Files:**
- Modify: `internal/utils/ai.go:1569-1573` (top of the for loop)

- [ ] **Step 1: Add `ShouldContinue` check at the start of each round**

Right after line 1569 (`for round := 0; round < maxRounds; round++ {`), before the existing `OnRoundStart` callback, add:

```go
		// Check caller abort signal before starting any round
		if len(opts) > 0 && opts[0].ShouldContinue != nil && !opts[0].ShouldContinue() {
			slog.Info("Caller aborted StreamWithTools via ShouldContinue", "round", round)
			return fullText.String(), nil
		}
```

The top of the loop should now read:

```go
	for round := 0; round < maxRounds; round++ {
		// Check caller abort signal before starting any round
		if len(opts) > 0 && opts[0].ShouldContinue != nil && !opts[0].ShouldContinue() {
			slog.Info("Caller aborted StreamWithTools via ShouldContinue", "round", round)
			return fullText.String(), nil
		}
		// Notify caller that a new round is starting (e.g. to reset per-round state).
		if len(opts) > 0 && opts[0].OnRoundStart != nil {
			opts[0].OnRoundStart(round)
		}
```

- [ ] **Step 2: Verify build**

Run: `go build ./...`
Expected: Success

- [ ] **Step 3: Commit**

```bash
git add internal/utils/ai.go
git commit -m "feat: check ShouldContinue at round start to allow early abort"
```

---

### Task 4: Update Phase 1 chat tool config

**Files:**
- Modify: `internal/controllers/app_builder_tools.go:397-400` (ToolCallConfig), `334-340` (variable declarations)

- [ ] **Step 1: Update the `ToolCallConfig` in `runChatToolPhase`**

In `internal/controllers/app_builder_tools.go`, replace the `ToolCallConfig` at lines 397-400:

```go
		utils.ToolCallConfig{
			MaxRounds:  c.RemoteConfig.GetInt(remoteconfig.AIBuilderToolMaxRounds, remoteconfig.Global),
			MaxTokens:  4096,
			StripAfter: 28, // Strip tools near the end so model is forced to respond
```

With:

```go
		utils.ToolCallConfig{
			MaxRounds:        c.RemoteConfig.GetInt(remoteconfig.AIBuilderToolMaxRounds, remoteconfig.Global),
			MaxTokens:        8192,
			MaxContinuations: -1, // Phase 1 should never continue — text is discarded on NO_TOOLS_NEEDED
			ShouldContinue: func() bool {
				phase1Mu.Lock()
				defer phase1Mu.Unlock()
				return !phase1Suppressed
			},
			StripAfter: 28, // Strip tools near the end so model is forced to respond
```

- [ ] **Step 2: Verify build**

Run: `go build ./...`
Expected: Success

- [ ] **Step 3: Commit**

```bash
git add internal/controllers/app_builder_tools.go
git commit -m "fix: prevent Phase 1 continuation waste — bump MaxTokens to 8192, disable continuations, add ShouldContinue abort"
```

---

### Task 5: Update ecommerce setup config

**Files:**
- Modify: `internal/controllers/app_builder_generation.go:647-656` (ToolCallConfig)

- [ ] **Step 1: Update the `ToolCallConfig` in the ecommerce setup phase**

In `internal/controllers/app_builder_generation.go`, replace the config at lines 647-656:

```go
	setupConfig := utils.ToolCallConfig{
		MaxRounds:  c.RemoteConfig.GetInt(remoteconfig.AIGenerationMaxRounds, remoteconfig.Global),
		NudgeAfter: 5,
		StripAfter: 6,
		MaxTokens:  4096,
		NudgeMessage: "Finish populating the store and provide a brief summary.",
		OnToolCallStart: func(toolName string) {
			sendEvent("tool_start", map[string]any{"tool": toolName})
		},
	}
```

With:

```go
	setupConfig := utils.ToolCallConfig{
		MaxRounds:        c.RemoteConfig.GetInt(remoteconfig.AIGenerationMaxRounds, remoteconfig.Global),
		NudgeAfter:       5,
		StripAfter:       6,
		MaxTokens:        8192,
		MaxContinuations: 1,
		NudgeMessage:     "Finish populating the store and provide a brief summary.",
		OnToolCallStart: func(toolName string) {
			sendEvent("tool_start", map[string]any{"tool": toolName})
		},
	}
```

- [ ] **Step 2: Verify build**

Run: `go build ./...`
Expected: Success

- [ ] **Step 3: Commit**

```bash
git add internal/controllers/app_builder_generation.go
git commit -m "fix: bump ecommerce setup MaxTokens to 8192, limit continuations to 1"
```

---

### Task 6: Final verification

**Files:**
- Read: all three modified files

- [ ] **Step 1: Full build check**

Run: `go build ./...`
Expected: Success with zero errors

- [ ] **Step 2: Verify no other callers of `StreamWithTools` need updating**

Run: `grep -n 'StreamWithTools' internal/controllers/*.go internal/utils/*.go`

Check each call site. The only callers with `MaxTokens: 4096` should already be fixed (Phase 1 and ecommerce setup). Other callers either use the default (16384) or set higher values — they're fine with the default `MaxContinuations: 3`.

- [ ] **Step 3: Grep for any remaining hardcoded 4096 max tokens**

Run: `grep -n 'MaxTokens.*4096' internal/`

Expected: No results (both instances should now be 8192).

- [ ] **Step 4: Commit (if any fixups needed)**

Only if previous steps revealed issues that needed fixing.
