# Unified Chat Code Generation

## Problem

The current chat editing system uses a multi-phase pipeline (4-8 LLM calls per message) that is slow, expensive, and fragile:

- **Phase 1** (tool phase): Fast model with 13+ tools, 1-6 rounds. Often outputs `NO_TOOLS_NEEDED` after burning 10-80K tokens.
- **Phase 2** (code gen): Main model outputs `<file>`/`<edit>` blocks. Separate context, separate call.
- **File selection**: LLM call to pick which files to inline.
- **Summarization**: LLM call to summarize old conversation messages.

Evidence from PostHog traces (last 3 hours before design):
- Average 4.6 LLM calls per chat message
- Average 136 seconds latency, $0.22 cost per chat
- Phase 1 consumed 450K input tokens ($0.71) across 16 chats
- `</search>` tags leaked into generated code (trace ebb884ef)
- Summarization generated 14K tokens of React code instead of a 3-sentence summary (trace 35a3ecdd)
- Phase 1 tool loops ran 6 rounds for simple SQL diagnostics (traces 8e7767f8, 5959ec4a)

## Solution

Replace the multi-phase pipeline with a single `StreamWithTools` call using Sonnet for everything. The model's text output flows through `FileStreamParser` for `<file>`/`<edit>`/`<delete>` blocks while tools remain available for database ops, image generation, etc.

### New flow

```
1. Load file list from git (paths + sizes only, no content)
2. Build: unified system prompt + file list + last N messages + user message
3. Single StreamWithTools call:
   - Model calls read_file when it needs to see a file
   - Model calls execute_sql, generate_image, etc. when needed
   - Model outputs <file>/<edit>/<delete> blocks in text for code changes
   - Model outputs plain text for explanations (streamed to user)
4. Post-processing (auto-fixes, package augment, etc.)
5. Save to git, done
```

### What gets deleted

- `selectContextFiles()` LLM call and `contextFileSelection` struct
- `summarizeConversation()` LLM call
- `runChatToolPhase()` function and `chatToolPhaseResult` struct
- `NO_TOOLS_NEEDED` / `NO_CODE_NEEDED` / `CODE_CHANGES_NEEDED` signal system
- `phase_change` SSE event
- Phase 1 text buffering/suppression logic (`phase1TextBuf`, `phase1Suppressed`, `phase1TextFlushed`, `noCodeNeeded`)
- File selection LLM prompt and import graph expansion for selection
- Opus model switching for high reasoning effort

## Architecture

### Single StreamWithTools Call

```go
StreamWithTools(
    ctx,
    model,           // always Sonnet
    messages,        // unified prompt + file list + history + user msg
    tools,           // all 16 tools including read_file
    provider,
    nil,             // no response format
    reasoning,       // ReasoningConfig with effort level (no Opus switch)
    executor,        // same tool executors as current Phase 1
    onToolCall,      // → sendEvent("tool_status", ...)
    onChunk,         // → FileStreamParser.Feed()
    onReasoning,     // → sendEvent("chat_thinking", ...)
    ToolCallConfig{
        MaxRounds:        10,
        NudgeAfter:       5,
        StripAfter:       7,
        MaxTokens:        65536,
        MaxContinuations: 3,
        NudgeMessage:     "You have gathered enough context. Output your code changes now using <file> and <edit> blocks.",
        OnRoundStart:     func(round int) { parser.Flush() },
    },
)
```

The `onChunk` callback wires directly to `FileStreamParser`:

```go
parser := &FileStreamParser{
    DiscardIncomplete: true,
    OnFile:   func(path, content string) { /* save file, sendEvent("file_complete") */ },
    OnEdit:   func(path, search, replace string) { /* apply edit, track failures */ },
    OnDelete: func(path string) { /* delete file */ },
    OnText:   func(text string) { /* sendEvent("chat_text") */ },
}

// onChunk callback:
func(chunk string) { parser.Feed(chunk) }
```

### System Prompt Structure

One unified prompt combining current Phase 1 tool instructions and Phase 2 code gen rules:

```
1. NEXTJS_BUILDER_COMMON_RULES (CSS, Next.js, component rules)
2. Code output format (<file>, <edit>, <delete> rules)
3. Tool usage instructions (when to call each tool)
4. "Call read_file to see file contents. File list shows what exists."
5. "If an edit fails, you'll get an error. Retry or use <file> for full rewrite."
6. DB schema (if project has database)
7. Ecommerce rules (if ecommerce project)
8. Mobile rules (if mobile app)
9. Auth rules (if needs_auth)
```

Key prompt changes:
- Remove all `NO_TOOLS_NEEDED` / `NO_CODE_NEEDED` / `CODE_CHANGES_NEEDED` signals. The model responds naturally — tools when needed, `<file>`/`<edit>` blocks for code, plain text for conversation.
- Add `read_file` usage instructions: "The project context shows file paths and sizes. Call read_file to see a file's content before editing it."
- Add edit failure instructions: "If an edit fails, you'll receive an error message. Retry with corrected search text or use <file> for full rewrite."
- Language rule stays: respond in user's language.

### Project Context (System Message 2)

Same as current `buildProjectContextForChat` but without inlined file contents:

```
App: {name} (slug: {slug})
Project type: {type}
[Project memory if exists]
[Logo URL if exists]
Theme: {theme}
[Installed UI components list]

All project files:
- app/page.tsx (860 chars)
- app/layout.tsx (1636 chars)
- components/home/HeroSection.tsx (8320 chars)
- ... (paths + sizes only)

[User-uploaded assets]
[Available imports from lib/ files]
```

No file contents inlined. The model calls `read_file` to get content for files it needs.

### Conversation History

- Keep last **8 messages** with full content
- Older messages: truncate content to first 200 characters, keep `fileOperations` list
- Strip `ThinkingText` and `ToolCalls` from older messages
- No LLM summarization call
- Existing `conv.Summary` field: still included if it exists (backwards compatible), but never regenerated

LLM messages array:
```
[system] unified prompt
[system] project context (file list + metadata)
[user]   "[Earlier conversation summary]\n{summary}"  (if conv.Summary exists)
[asst]   "I understand the earlier context."           (if conv.Summary exists)
...older messages (truncated to 200 chars)...
...last 8 messages (full content)...
```

### Tools (All 16 Kept)

| Tool | Purpose |
|------|---------|
| `read_file` | Read file content on-demand (replaces file selection) |
| `list_files` | List files matching a pattern |
| `diff_files` | Show git diff for a file |
| `git_log` | Show recent commits |
| `preview_website` | Analyze URL structure + screenshot |
| `take_screenshot` | Screenshot a URL |
| `execute_sql` | Run SQL queries (SELECT, INSERT, UPDATE) |
| `seed_database` | Populate with demo data |
| `run_migrations` | ALTER TABLE / CREATE TABLE |
| `manage_env_vars` | Set/remove env vars |
| `install_package` | Install npm packages |
| `generate_image` | AI image generation |
| `search_components` | Find and inject UI components |
| `manage_store` | Ecommerce CRUD |
| `configure_mobile_app` | Mobile bridge config |
| `manage_assets` | List/delete/rename assets |

### Edit Failure Feedback

When `ApplyEdit` fails (search text not found):

1. Track failure in a shared slice: `{path, searchPreview}`
2. After `StreamWithTools` returns, check for failures
3. If failures exist: append error as system message, call `StreamWithTools` again (1 retry max)

```go
if len(failedEdits) > 0 {
    var errMsg strings.Builder
    for _, f := range failedEdits {
        errMsg.WriteString(fmt.Sprintf(
            "Edit failed for %q: search text not found (preview: %q). "+
            "Fix the <search> text or use <file> for full rewrite.\n",
            f.path, f.searchPreview,
        ))
    }
    messages = append(messages, Message{Role: "system", Content: errMsg.String()})
    // Retry once
    StreamWithTools(ctx, model, messages, tools, ...)
}
```

### Tag Leak Prevention

In `saveGeneratedFile`, strip parser boundary tags from content before writing:

```go
for _, tag := range []string{"</search>", "</replace>", "</edit>", "</file>"} {
    content = strings.ReplaceAll(content, tag, "")
}
```

### Reasoning Effort

- Always use Sonnet (no model switching)
- `reasoningEffort` maps to `ReasoningConfig.Effort`:
  - `"none"` → no reasoning
  - `"low"` / default → `ReasoningConfig{Effort: "low"}`
  - `"high"` → `ReasoningConfig{Effort: "high"}`
  - `"xhigh"` → `ReasoningConfig{Effort: "high"}` (same as high, no Opus)

### What Stays Unchanged

- Initial generation path (`isInitialGeneration=true`) — untouched
- `StreamWithTools` in `utils/ai.go` — no changes
- `FileStreamParser` in `utils/file_parser.go` — no changes
- All 16 tool definitions and executor functions
- Git snapshots/rollback
- Post-processing (auto-fixes, package augment, env var detection, mobile bridge detection)
- Token tracking and credit deduction
- SSE events: `chat_text`, `chat_thinking`, `file_complete`, `progress`, `error`, `done`
- Conversation persistence in DB
- Project memory extraction (async)
- Views layer (`app_chat_views.go`)

### SSE Event Changes

- **Removed**: `phase_change` event (no more phases)
- **Added**: tool status events via `onToolCall` callback (model calling `read_file`, `execute_sql`, etc.)
- Everything else stays the same

## Expected Impact

Based on the PostHog trace data:

| Metric | Before | After |
|--------|--------|-------|
| LLM calls per chat | 4.6 avg (up to 8) | 1 (with tool rounds) |
| Avg latency | 136 seconds | ~30-60 seconds |
| Avg cost per chat | $0.22 | ~$0.08-0.12 |
| Phase 1 token waste | 450K tokens/3h | $0 (eliminated) |
| Edit block bugs | `</search>` leaks, silent drops | Failures fed back to model, tags sanitized |
| Summarization bugs | 14K token code output | No LLM summarization |

## Files to Modify

1. **`internal/controllers/app_builder_controller.go`** — Replace Phase 1 → Phase 2 pipeline with single `StreamWithTools` call in the chat path
2. **`internal/controllers/app_builder_context.go`** — Simplify `buildProjectContextForChat` to output file list only (no content inlining), remove `selectContextFiles`, simplify conversation history building
3. **`internal/controllers/app_builder_tools.go`** — Remove `runChatToolPhase`, remove Phase 1 system prompt, remove signal detection logic
4. **`internal/constants/ai_builder.go`** — New `NEXTJS_BUILDER_UNIFIED_CHAT_PROMPT` constant merging tool + codegen instructions
5. **`internal/controllers/app_builder_conversation.go`** — Remove `summarizeConversation` function, simplify conversation history truncation
