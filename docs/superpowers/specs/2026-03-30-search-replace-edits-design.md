# Search/Replace Edit Format for Chat Code Generation

**Date:** 2026-03-30
**Status:** Approved
**Problem:** Chat edits regenerate entire files (~37K output tokens) for simple changes that touch a few lines. This wastes ~$0.50/message in output tokens and adds ~25s latency.

## Format

Three mutation types during chat editing:

### `<edit>` — modify existing files (new)
```xml
<edit path="lib/i18n.ts">
<search>
const defaultLanguage = "en";
</search>
<replace>
const defaultLanguage = "tr";
</replace>
</edit>
```

- `<search>` must be an exact substring of the current file content
- Include 2-5 surrounding context lines for unique matching
- Multiple `<edit>` blocks per file allowed — applied sequentially
- Empty `<replace></replace>` deletes the matched section
- Use `<file>` instead when rewriting >50% of a file

### `<file>` — create new files or full rewrites (unchanged)
```xml
<file path="app/new-page.tsx">
complete file content
</file>
```

### `<delete>` — remove files (unchanged)
```xml
<delete path="app/old-page.tsx"/>
```

## Architecture

```
LLM output (streaming)
  ↓
FileStreamParser (extended with <edit> support)
  ↓ OnEdit(path, search, replace)
  ↓ OnFile(path, content)          ← unchanged
  ↓ OnDelete(path)                 ← unchanged
  ↓
Controller: applyEdit()
  → finds search text in fileMap[path]
  → replaces with new text
  → calls saveGeneratedFile() with full result
  ↓
file_complete SSE (full content)   ← unchanged
  ↓
Frontend / E2B sandbox             ← unchanged
```

**Key invariant:** The frontend and E2B sandbox are completely unchanged. They always receive full file content via `file_complete`. The edit format is purely a backend optimization between the LLM and the Go server.

## Implementation Details

### 1. Extend `FileStreamParser` (`internal/utils/file_parser.go`)

Add new parsing states for `<edit>` blocks:

```
States: outside → inEditBlock → inSearch → inReplace
```

New callback:
```go
OnEdit func(path, search, replace string)
```

State machine:
- `<edit path="...">` → enter edit block, store path
- `<search>` → start accumulating search text
- `</search>` → store search text, stop accumulating
- `<replace>` → start accumulating replace text
- `</replace>` → store replace text, stop accumulating
- `</edit>` → emit `OnEdit(path, search, replace)`, reset state

Regex for the opening tag:
```go
var editTagRegex = regexp.MustCompile(`<edit\s+path="([^"]+)"\s*>`)
```

Streaming safety: buffer trailing bytes to handle tags split across chunks, same approach as existing `</file>` handling.

### 2. Edit application function (`internal/controllers/app_builder_controller.go`)

```go
func applyEdit(original, search, replace string) (result string, ok bool)
```

**Matching strategy (in order):**

1. **Exact match** — `strings.Index(original, search)`. Fastest, most common case.

2. **Whitespace-normalized match** — Normalize both `original` and `search` by collapsing runs of whitespace (spaces, tabs) to single spaces, then find the match. Map the match position back to the original string's offsets. Handles indentation drift.

3. **Trimmed-lines match** — Split both into lines, trim each line, and find the contiguous line sequence. Handles cases where the LLM copies the search text with slightly different indentation.

If all three fail, return `(original, false)` — the file is unchanged.

**When edit target file doesn't exist in fileMap:**
- Log warning, skip the edit. The LLM was told not to edit files not shown in context.

**When edit fails (search text not found):**
- Log warning: `"applyEdit: search text not found"`, path, first 100 chars of search
- Skip the edit silently (don't crash, don't rollback)
- Other edits for the same or different files still apply

**When edit succeeds:**
- Update `fileMap[path]` with the result
- Call `saveGeneratedFile(path, result, ...)` — same pipeline as `<file>` blocks
- `file_complete` SSE sent to frontend with full content

### 3. Controller integration (`app_builder_controller.go`, chat edit path)

In the chat file-block path (Phase 2), add an `OnEdit` callback to the `FileStreamParser`:

```go
parser := &utils.FileStreamParser{
    DiscardIncomplete: true,
    OnFile: func(path, content string) {
        // existing full-file logic (unchanged)
    },
    OnDelete: func(path string) {
        // existing delete logic (unchanged)
    },
    OnEdit: func(path, search, replace string) {
        original, exists := fileMap[path]
        if !exists {
            slog.Warn("edit target file not found", "path", path)
            return
        }
        result, ok := applyEdit(original, search, replace)
        if !ok {
            slog.Warn("applyEdit: search text not found",
                "path", path, "search_preview", search[:min(100, len(search))])
            return
        }
        // Save the edited file through the normal pipeline
        _, fileType := guessFileType(path)
        if fileType == "tsx" || fileType == "ts" {
            result, _ = stripForbiddenImports(result, path)
        }
        if err := c.saveGeneratedFile(project, repo, path, result, fileType,
            fileMap, sendEvent, &toolAppliedOps, true); err != nil {
            slog.Error("edit file save failed", "path", path, "error", err)
        }
        chatFileCounter++
        sendEvent("progress", responses.ProgressEvent{
            Phase:   "generating_files",
            Message: fmt.Sprintf("Updated %s (%d files)", path, chatFileCounter),
            Current: chatFileCounter,
        })
    },
    OnText: func(text string) {
        // existing text logic (unchanged)
    },
}
```

### 4. System prompt changes (`internal/constants/ai_builder.go`)

Replace `NEXTJS_BUILDER_CHAT_INSTRUCTIONS` output format section.

**Old (current):**
```
1. Output only the files you change using <file path="...">complete file content</file> blocks.
4. Every file block must contain the COMPLETE file — not just the changed parts.
```

**New:**
```
### Output Format — Targeted Edits
Use <edit> blocks to modify existing files. Use <file> blocks ONLY for new files or complete rewrites.

**Editing existing files (PREFERRED):**
<edit path="app/page.tsx">
<search>
exact lines to find in the current file
</search>
<replace>
replacement content
</replace>
</edit>

**Creating new files:**
<file path="app/new-page.tsx">
complete file content
</file>

**Deleting files:**
<delete path="app/old-page.tsx"/>

Rules for <edit> blocks:
- The <search> text must EXACTLY match a substring of the current file (copy it precisely including whitespace).
- Include 2-5 surrounding context lines so the match is unique.
- Multiple <edit> blocks on the same file are applied in order.
- Empty <replace></replace> deletes the matched section.
- If rewriting >50% of a file, use <file> with complete content instead.
- NEVER use <edit> on a file whose content is not shown in the context above.
```

Keep all other rules (scope rule, error fixing, store updates, etc.) unchanged.

## Files Changed

| File | Change |
|------|--------|
| `internal/utils/file_parser.go` | Extend `FileStreamParser` with `<edit>` block parsing and `OnEdit` callback |
| `internal/controllers/app_builder_controller.go` | Add `applyEdit()` function and `OnEdit` handler in chat Phase 2 |
| `internal/constants/ai_builder.go` | Update `NEXTJS_BUILDER_CHAT_INSTRUCTIONS` with edit format |

## What Stays Unchanged

- **Frontend (dashboard)** — zero changes, `file_complete` still carries full content
- **E2B sandbox** — unchanged, receives full files via `/api/e2b/files`
- **Initial generation** — unchanged, uses `<file>` blocks (no existing files)
- **Git repo / saveGeneratedFile** — unchanged, still writes full files
- **Validation/fix pass** — unchanged, uses `<file>` blocks
- **`<file>` and `<delete>` parsing** — unchanged, backward compatible

## Estimated Impact

| Metric | Before | After |
|--------|--------|-------|
| Output tokens (simple edit) | ~37,000 | ~3,000 |
| Output tokens (complex edit) | ~37,000 | ~8,000 |
| Output cost per chat message | ~$0.56 | ~$0.05-0.12 |
| Time to first file update | ~30s | ~5-8s |
| Frontend changes required | — | None |

For a typical 10-message chat session: **~$4-5 savings per session**.
