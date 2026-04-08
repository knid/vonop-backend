# Chat Tool-Loop Optimization Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reduce token waste, eliminate redundant tool rounds, and preserve Anthropic prompt cache across tool rounds in the `app_chat` StreamWithTools loop.

**Architecture:** The chat path calls `StreamWithTools` with 10+ tools but no `TrimArgs`, `TrimResults`, `InterceptToolCall`, or `AfterToolRound` hooks. This causes: (1) tool results accumulating without compression, (2) the LLM re-reading the same files across rounds, (3) no progress feedback between rounds, and (4) mid-conversation system messages (nudges) invalidating the Anthropic prompt cache. We fix all six trace issues by wiring up these existing hooks in the chat caller and stabilizing the cache prefix.

**Tech Stack:** Go, Anthropic Messages API, `internal/utils/ai.go` (StreamWithTools), `internal/controllers/app_builder_controller.go` (chat caller)

**Trace reference:** `11d53ae9-17e5-4f54-a78d-70eeb8f5eac2` — 7 LLM calls, 119K input tokens, 1.4K output tokens, $0.16 cost, 47% of cost from a single cache-miss call.

---

## Root Cause Analysis

| # | Issue | Root Cause | Fix |
|---|-------|-----------|-----|
| 1 | 7 LLM calls for 1 user message | LLM reads files one-at-a-time across rounds before producing edits | `AfterToolRound` + lower `NudgeAfter` |
| 2 | 1.19% output/input ratio | Tool results (full file contents) sit uncompressed in history | `TrimResults` compresses prior-round results |
| 3 | 47% cost from Event 7 cache miss | Mid-conversation system nudges change message structure, breaking Anthropic cache prefix | Move nudges to last user message block instead of appending new messages |
| 4 | Event 6 empty generation | LLM called read_file for an already-read file, got cached dedup, wasted a round | `InterceptToolCall` for read_file dedup at caller level |
| 5 | 52% non-API overhead | File operations between rounds + redundant read_file round-trips | `InterceptToolCall` eliminates redundant I/O |
| 6 | Repeated edit attempts (Events 3-5) | No progress feedback telling LLM which files were already modified | `AfterToolRound` injects per-round progress summary |

## File Structure

| File | Change | Responsibility |
|------|--------|---------------|
| `internal/controllers/app_builder_controller.go:835-845` | Modify | Wire up `TrimArgs`, `TrimResults`, `InterceptToolCall`, `AfterToolRound` in `ToolCallConfig` |
| `internal/controllers/app_builder_tools.go` (new function) | Modify | Add `buildChatToolCallConfig()` helper that constructs the optimized config |
| `internal/utils/ai.go:1597-1621` | Modify | Change nudge injection from new system messages to appending to last user message |

---

### Task 1: Add `TrimResults` to compress prior-round tool results

The most impactful fix. After the LLM has seen a `read_file` result for one round, compress it to a path+size stub in the next round. This directly addresses Issues #2 and #3 (token waste and context growth).

**Files:**
- Modify: `internal/controllers/app_builder_controller.go:835-845`

- [ ] **Step 1: Add TrimResults to the ToolCallConfig**

In `app_builder_controller.go`, find the `ToolCallConfig` struct literal at line 835 and add the `TrimResults` field. The function receives `toolName` and `result` (the full tool result string) and returns a compressed version.

```go
utils.ToolCallConfig{
    MaxRounds:        10,
    NudgeAfter:       3,  // was 5 — lower to reduce unnecessary rounds
    StripAfter:       5,  // was 7
    MaxTokens:        65536,
    MaxContinuations: 3,
    NudgeMessage:     "You have gathered enough context. Output your code changes now using <file> and <edit> blocks.",
    OnRoundStart: func(round int) {
        parser.Flush()
    },
    TrimResults: func(toolName, result string) string {
        // Compress read_file results after the model has seen them once.
        // Keep first 200 chars as a preview so the model remembers structure.
        if toolName == "read_file" && len(result) > 300 {
            preview := result[:200]
            return fmt.Sprintf("%s\n...[trimmed, %d chars total — call read_file again if needed]", preview, len(result))
        }
        // Compress list_files results
        if toolName == "list_files" && len(result) > 500 {
            lines := strings.Split(result, "\n")
            if len(lines) > 10 {
                return strings.Join(lines[:10], "\n") + fmt.Sprintf("\n...[%d more files]", len(lines)-10)
            }
        }
        // Trim long tool results generically
        if len(result) > 2000 {
            return result[:500] + fmt.Sprintf("\n...[trimmed, %d chars total]", len(result))
        }
        return result
    },
},
```

- [ ] **Step 2: Verify the build compiles**

Run: `go build ./cmd/main.go`
Expected: Compiles without errors.

- [ ] **Step 3: Commit**

```bash
git add internal/controllers/app_builder_controller.go
git commit -m "perf: add TrimResults to chat tool loop to compress prior-round results"
```

---

### Task 2: Add `TrimArgs` to strip file content from tool-call arguments in history

After the LLM calls a tool, the full arguments (including any file content it passed) remain in the conversation history. For tools that embed file content (like hypothetical write operations), strip the content and keep just the metadata.

**Files:**
- Modify: `internal/controllers/app_builder_controller.go:835-845`

- [ ] **Step 1: Add TrimArgs to the ToolCallConfig**

Add the `TrimArgs` field to the same `ToolCallConfig` struct. This strips large arguments from tool calls in the conversation history after the model has seen them.

```go
TrimArgs: func(toolName, args string) string {
    // Most chat tools have small args (file paths, queries).
    // Only trim if args are unexpectedly large (e.g. execute_sql with large results).
    if len(args) > 1000 {
        // Parse to keep just the keys
        var parsed map[string]any
        if json.Unmarshal([]byte(args), &parsed) == nil {
            // Truncate any string values over 200 chars
            for k, v := range parsed {
                if s, ok := v.(string); ok && len(s) > 200 {
                    parsed[k] = s[:200] + "...[trimmed]"
                }
            }
            if trimmed, err := json.Marshal(parsed); err == nil {
                return string(trimmed)
            }
        }
    }
    return args
},
```

- [ ] **Step 2: Verify the build compiles**

Run: `go build ./cmd/main.go`
Expected: Compiles without errors.

- [ ] **Step 3: Commit**

```bash
git add internal/controllers/app_builder_controller.go
git commit -m "perf: add TrimArgs to chat tool loop to strip large arguments from history"
```

---

### Task 3: Add `AfterToolRound` to inject progress feedback

This directly addresses Issue #6 (repeated edit attempts). After each tool round, inject a message telling the LLM which files have been created/modified so far, so it doesn't repeat the same work.

**Files:**
- Modify: `internal/controllers/app_builder_controller.go:835-845`

- [ ] **Step 1: Add AfterToolRound to the ToolCallConfig**

The `AfterToolRound` callback has access to `toolAppliedOps` (via closure) which tracks all file mutations. Use it to inject a progress summary.

```go
AfterToolRound: func(allIntercepted bool) string {
    if allIntercepted {
        return "" // default dedup message is fine
    }
    // Build a summary of files modified so far
    if len(toolAppliedOps) == 0 {
        return ""
    }
    var paths []string
    seen := make(map[string]bool)
    for _, op := range toolAppliedOps {
        if !seen[op.Path] {
            paths = append(paths, fmt.Sprintf("%s %s", op.Action, op.Path))
            seen[op.Path] = true
        }
    }
    return fmt.Sprintf("Files changed so far: %s. Do NOT re-read or re-edit files you already changed — move on to the next file or output your final response.", strings.Join(paths, ", "))
},
```

- [ ] **Step 2: Verify the build compiles**

Run: `go build ./cmd/main.go`
Expected: Compiles without errors.

- [ ] **Step 3: Commit**

```bash
git add internal/controllers/app_builder_controller.go
git commit -m "perf: add AfterToolRound to chat tool loop for progress feedback"
```

---

### Task 4: Add `InterceptToolCall` to deduplicate read_file at caller level

The `StreamWithTools` loop already has built-in dedup via `toolCallHistory`, but it only deduplicates within the same loop instance. The caller can provide smarter interception: if the LLM calls `read_file` for a file that exists in `fileMap` (which is always up-to-date because `saveGeneratedFile` updates it), serve directly from the in-memory map without a tool round.

**Files:**
- Modify: `internal/controllers/app_builder_controller.go:835-845`

- [ ] **Step 1: Add InterceptToolCall to the ToolCallConfig**

The `builderToolCallbacks` struct already has a thread-safe `getFile` method. Use it.

```go
InterceptToolCall: func(toolName, toolArgs string) (string, bool) {
    // Fast-path read_file from in-memory fileMap (always current)
    if toolName == "read_file" {
        var args struct {
            FilePath string `json:"file_path"`
        }
        if json.Unmarshal([]byte(toolArgs), &args) == nil && args.FilePath != "" {
            if content, ok := fileMap[args.FilePath]; ok {
                return content, true // skip tool execution, serve from memory
            }
        }
    }
    // Fast-path list_files — always available from fileMap
    if toolName == "list_files" {
        var lines []string
        for path, content := range fileMap {
            lines = append(lines, fmt.Sprintf("%s (%d chars)", path, len(content)))
        }
        sort.Strings(lines)
        return strings.Join(lines, "\n"), true
    }
    return "", false // proceed with normal execution
},
```

- [ ] **Step 2: Add missing `sort` import if not already present**

Check the import block of `app_builder_controller.go`. It already imports `"sort"` — verify. If not, add it.

- [ ] **Step 3: Verify the build compiles**

Run: `go build ./cmd/main.go`
Expected: Compiles without errors.

- [ ] **Step 4: Commit**

```bash
git add internal/controllers/app_builder_controller.go
git commit -m "perf: add InterceptToolCall to serve read_file/list_files from memory"
```

---

### Task 5: Stabilize Anthropic prompt cache by moving nudges into last user message

This is the most technically subtle fix. Currently, `StreamWithTools` injects nudge/dedup system messages by appending new `Message{Role: "system"}` entries. In `convertToAnthropicRequest`, mid-conversation system messages are converted to `[System] ...` user-role messages. This creates new message entries that change the conversation structure, invalidating Anthropic's prefix-based prompt cache.

The fix: instead of appending new messages, modify `StreamWithTools` to append nudge text to the last user message's content. This preserves the message array length and all prior messages remain cache-eligible.

**Files:**
- Modify: `internal/utils/ai.go:1597-1630` (nudge injection section)

- [ ] **Step 1: Create helper to append to last user message**

Add a helper function near the top of the `StreamWithTools` function (after the `currentMessages` variable):

```go
// appendToLastUserMessage appends text to the last user message in the slice,
// or appends a new user message if none exists. This preserves message array
// structure for Anthropic prompt caching (new messages invalidate the cache prefix).
func appendToLastUserMessage(messages []Message, text string) []Message {
    for i := len(messages) - 1; i >= 0; i-- {
        if messages[i].Role == "user" {
            messages[i].Content += "\n\n[System] " + text
            return messages
        }
    }
    // Fallback: no user message found, append as system (cache-breaking but rare)
    return append(messages, Message{Role: "system", Content: text})
}
```

- [ ] **Step 2: Replace nudge injection to use the helper**

Find the nudge injection block at approximately lines 1604-1621:

```go
// BEFORE (cache-breaking):
if consecutiveToolRounds >= stripAfter {
    currentMessages = append(currentMessages, Message{
        Role:    "system",
        Content: "FINAL WARNING: You MUST generate your complete response NOW. All tools have been removed. Use the information you have already gathered.",
    })
    roundTools = nil
    slog.Warn("Stripping tools to force final response", "round", round, "consecutive_tool_rounds", consecutiveToolRounds)
} else if consecutiveToolRounds >= nudgeAfter && !nudged {
    nudgeContent := "You have already gathered sufficient context from tool calls. Please wrap up your research and generate your final complete response. You may make one or two more targeted tool calls if absolutely necessary, but prioritize producing your response."
    if len(opts) > 0 && opts[0].NudgeMessage != "" {
        nudgeContent = opts[0].NudgeMessage
    }
    currentMessages = append(currentMessages, Message{
        Role:    "system",
        Content: nudgeContent,
    })
    nudged = true
    slog.Warn("Injecting stop-researching nudge (tools still available)", "round", round, "consecutive_tool_rounds", consecutiveToolRounds)
}
```

Replace with:

```go
// AFTER (cache-preserving): append to last user message instead of new system message
if consecutiveToolRounds >= stripAfter {
    currentMessages = appendToLastUserMessage(currentMessages,
        "FINAL WARNING: You MUST generate your complete response NOW. All tools have been removed. Use the information you have already gathered.")
    roundTools = nil
    slog.Warn("Stripping tools to force final response", "round", round, "consecutive_tool_rounds", consecutiveToolRounds)
} else if consecutiveToolRounds >= nudgeAfter && !nudged {
    nudgeContent := "You have already gathered sufficient context from tool calls. Please wrap up your research and generate your final complete response. You may make one or two more targeted tool calls if absolutely necessary, but prioritize producing your response."
    if len(opts) > 0 && opts[0].NudgeMessage != "" {
        nudgeContent = opts[0].NudgeMessage
    }
    currentMessages = appendToLastUserMessage(currentMessages, nudgeContent)
    nudged = true
    slog.Warn("Injecting stop-researching nudge (tools still available)", "round", round, "consecutive_tool_rounds", consecutiveToolRounds)
}
```

- [ ] **Step 3: Also fix the dedup/intercept system messages (lines ~2120-2131)**

Find the dedup system messages:

```go
// BEFORE:
if interceptCount == len(toolCalls) {
    currentMessages = append(currentMessages, Message{
        Role:    "system",
        Content: "All your tool calls in this round were served from memory. Move on to files that have NOT been created yet, or provide a summary to the user if all files are done.",
    })
```

Replace both dedup messages with `appendToLastUserMessage`:

```go
if interceptCount == len(toolCalls) {
    currentMessages = appendToLastUserMessage(currentMessages,
        "All your tool calls in this round were served from memory. Move on to files that have NOT been created yet, or provide a summary to the user if all files are done.")
```

And similarly for the other dedup message at ~line 2128:

```go
} else {
    currentMessages = appendToLastUserMessage(currentMessages,
        "All your tool calls in this round returned cached results — you already have this information. Stop making redundant tool calls and produce your final response.")
```

- [ ] **Step 4: Fix AfterToolRound injection (line ~2163)**

```go
// BEFORE:
if msg := opts[0].AfterToolRound(allIntercepted); msg != "" {
    currentMessages = append(currentMessages, Message{
        Role:    "system",
        Content: msg,
    })
}
```

Replace with:

```go
if msg := opts[0].AfterToolRound(allIntercepted); msg != "" {
    currentMessages = appendToLastUserMessage(currentMessages, msg)
}
```

- [ ] **Step 5: Fix empty response system message (line ~1953)**

```go
// BEFORE:
currentMessages = append(currentMessages, Message{
    Role:    "system",
    Content: "Your previous response was empty. Please generate your final response as plain text now.",
})
```

Replace with:

```go
currentMessages = appendToLastUserMessage(currentMessages,
    "Your previous response was empty. Please generate your final response as plain text now.")
```

- [ ] **Step 6: Verify the build compiles**

Run: `go build ./cmd/main.go`
Expected: Compiles without errors.

- [ ] **Step 7: Commit**

```bash
git add internal/utils/ai.go
git commit -m "perf: stabilize Anthropic prompt cache by appending nudges to last user message"
```

---

### Task 6: Lower NudgeAfter/StripAfter thresholds

For simple edit requests, 5 rounds of tool calling before nudging is too many. The trace showed 7 rounds for "I want a new logo." Lower the thresholds.

**Files:**
- Modify: `internal/controllers/app_builder_controller.go:835-845` (already done in Task 1)

- [ ] **Step 1: Verify NudgeAfter=3, StripAfter=5 are set**

Already set in Task 1. Verify by reading the file.

- [ ] **Step 2: Also lower the retry path thresholds**

Find the retry `ToolCallConfig` at line ~887:

```go
utils.ToolCallConfig{
    MaxRounds:        3,
    NudgeAfter:       2,
    StripAfter:       3,
    // ... rest stays the same
},
```

These are already good. No change needed.

- [ ] **Step 3: Commit (if any change was needed)**

---

### Task 7: Verify end-to-end

- [ ] **Step 1: Build the binary**

Run: `make build`
Expected: Builds successfully.

- [ ] **Step 2: Review the changes**

Run: `git diff HEAD~5..HEAD --stat` to see all changed files and verify scope is limited to:
- `internal/controllers/app_builder_controller.go`
- `internal/utils/ai.go`

- [ ] **Step 3: Manual verification**

Deploy to staging and send the same request ("yeni logo istiyorum" on a project with existing files). Monitor PostHog traces to verify:
- Fewer LLM rounds (target: 3-4 instead of 7)
- Higher cache hit rate (target: >80% on all rounds)
- Lower total input tokens (target: <60K instead of 120K)
- Lower cost (target: <$0.08 instead of $0.16)

---

## Expected Impact

| Metric | Before | Target After |
|--------|--------|-------------|
| LLM rounds per chat | 7 | 3-4 |
| Input tokens | 119,783 | <60,000 |
| Cache hit rate | 75% avg (0% on Event 7) | >85% all rounds |
| Cost per trace | $0.16 | <$0.08 |
| Non-API overhead | 52% (34s) | <30% |
| Output/Input ratio | 1.19% | >3% |
