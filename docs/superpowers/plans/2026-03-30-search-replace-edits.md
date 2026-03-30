# Search/Replace Edit Format Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace full-file output with targeted `<edit>` search/replace blocks during chat code generation, reducing output tokens by ~85%.

**Architecture:** Extend the existing `FileStreamParser` to recognize `<edit>` blocks alongside `<file>` and `<delete>`. A new `applyEdit` function does exact-then-fuzzy matching to apply search/replace pairs to original file content. The system prompt switches from "output complete files" to "output targeted edits". Frontend and E2B are untouched — they still receive full file content via `file_complete`.

**Tech Stack:** Go (streaming parser, string matching), LLM prompt engineering

**Spec:** `docs/superpowers/specs/2026-03-30-search-replace-edits-design.md`

---

### Task 1: Add `applyEdit` function

**Files:**
- Create: `internal/utils/apply_edit.go`

This is the core matching engine. It takes the original file content and a search/replace pair, returns the result.

- [ ] **Step 1: Create `applyEdit` with exact matching**

Create `internal/utils/apply_edit.go`:

```go
package utils

import (
	"strings"
)

// applyEdit finds the search text in original and replaces it with replace.
// Returns the modified string and true on success, or the original and false
// if the search text was not found.
//
// Matching strategy (in order):
//  1. Exact substring match
//  2. Whitespace-normalized match (collapses runs of spaces/tabs)
//  3. Trimmed-lines match (trims leading/trailing whitespace per line)
func ApplyEdit(original, search, replace string) (string, bool) {
	// Strategy 1: exact match
	idx := strings.Index(original, search)
	if idx != -1 {
		return original[:idx] + replace + original[idx+len(search):], true
	}

	// Strategy 2: whitespace-normalized match
	if result, ok := applyEditNormalized(original, search, replace); ok {
		return result, true
	}

	// Strategy 3: trimmed-lines match
	if result, ok := applyEditTrimmedLines(original, search, replace); ok {
		return result, true
	}

	return original, false
}

// applyEditNormalized collapses runs of spaces/tabs (not newlines) in both
// original and search, finds the match in the normalized original, maps the
// match offsets back to the real original, and performs the replacement there.
func applyEditNormalized(original, search, replace string) (string, bool) {
	type mapping struct {
		normIdx int
		origIdx int
	}

	// Build normalized version of original with index mapping
	var normBuf strings.Builder
	var mappings []mapping
	i := 0
	for i < len(original) {
		if original[i] == ' ' || original[i] == '\t' {
			start := i
			for i < len(original) && (original[i] == ' ' || original[i] == '\t') {
				i++
			}
			mappings = append(mappings, mapping{normBuf.Len(), start})
			normBuf.WriteByte(' ')
		} else {
			mappings = append(mappings, mapping{normBuf.Len(), i})
			normBuf.WriteByte(original[i])
			i++
		}
	}
	// Sentinel mapping for end-of-string
	mappings = append(mappings, mapping{normBuf.Len(), len(original)})

	// Normalize search the same way
	var normSearch strings.Builder
	for j := 0; j < len(search); {
		if search[j] == ' ' || search[j] == '\t' {
			for j < len(search) && (search[j] == ' ' || search[j] == '\t') {
				j++
			}
			normSearch.WriteByte(' ')
		} else {
			normSearch.WriteByte(search[j])
			j++
		}
	}

	normOrig := normBuf.String()
	normSearchStr := normSearch.String()

	normIdx := strings.Index(normOrig, normSearchStr)
	if normIdx == -1 {
		return "", false
	}
	normEnd := normIdx + len(normSearchStr)

	// Map normalized offsets back to original offsets
	origStart := -1
	origEnd := -1
	for _, m := range mappings {
		if m.normIdx == normIdx && origStart == -1 {
			origStart = m.origIdx
		}
		if m.normIdx >= normEnd && origEnd == -1 {
			origEnd = m.origIdx
			break
		}
	}
	if origStart == -1 || origEnd == -1 {
		// Fallback: if mapping fails, try the last mapping entry
		if origEnd == -1 {
			origEnd = len(original)
		}
		if origStart == -1 {
			return "", false
		}
	}

	return original[:origStart] + replace + original[origEnd:], true
}

// applyEditTrimmedLines splits both original and search into lines, trims each
// line, and looks for a contiguous sequence of trimmed search lines within the
// trimmed original lines. If found, replaces the corresponding original lines.
func applyEditTrimmedLines(original, search, replace string) (string, bool) {
	origLines := strings.Split(original, "\n")
	searchLines := strings.Split(search, "\n")

	// Remove empty trailing lines from search (LLM often adds trailing newline)
	for len(searchLines) > 0 && strings.TrimSpace(searchLines[len(searchLines)-1]) == "" {
		searchLines = searchLines[:len(searchLines)-1]
	}
	if len(searchLines) == 0 {
		return "", false
	}

	// Build trimmed versions for comparison
	trimmedSearch := make([]string, len(searchLines))
	for i, l := range searchLines {
		trimmedSearch[i] = strings.TrimSpace(l)
	}

	// Slide a window over original lines looking for the match
	for i := 0; i <= len(origLines)-len(searchLines); i++ {
		match := true
		for j := 0; j < len(searchLines); j++ {
			if strings.TrimSpace(origLines[i+j]) != trimmedSearch[j] {
				match = false
				break
			}
		}
		if match {
			// Found the match — replace original lines [i..i+len(searchLines)) with replace
			var result strings.Builder
			// Lines before match
			if i > 0 {
				result.WriteString(strings.Join(origLines[:i], "\n"))
				result.WriteString("\n")
			}
			// Replacement content
			result.WriteString(replace)
			// Lines after match
			afterIdx := i + len(searchLines)
			if afterIdx < len(origLines) {
				if replace != "" {
					result.WriteString("\n")
				}
				result.WriteString(strings.Join(origLines[afterIdx:], "\n"))
			}
			return result.String(), true
		}
	}

	return "", false
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/utils/apply_edit.go
git commit -m "feat: add applyEdit function with exact, normalized, and trimmed-lines matching"
```

---

### Task 2: Extend `FileStreamParser` with `<edit>` block support

**Files:**
- Modify: `internal/utils/file_parser.go`

Add new state for parsing `<edit>` blocks with nested `<search>` and `<replace>` sub-blocks. The parser emits `OnEdit(path, search, replace)` when a complete `</edit>` tag is found.

- [ ] **Step 1: Add edit parsing state and regex to `FileStreamParser`**

Add the following to `internal/utils/file_parser.go`:

After the existing `deleteTagRegex` (line 15), add:

```go
// editTagRegex matches <edit path="..."> opening tags.
var editTagRegex = regexp.MustCompile(`<edit\s+path="([^"]+)"\s*>`)
```

Add new fields to the `FileStreamParser` struct (after the existing `fileContent` field at line 31):

```go
	// Edit block state
	inEditBlock    bool
	editPath       string
	inSearch       bool
	inReplace      bool
	searchContent  strings.Builder
	replaceContent strings.Builder
```

Add the new callback to the struct (after `OnText` at line 46):

```go
	// OnEdit is called when a complete </edit> closing tag is found.
	// path is the file to edit, search is the text to find, replace is the replacement.
	OnEdit func(path, search, replace string)
```

- [ ] **Step 2: Add edit block detection in the `Feed` method's "outside any block" branch**

In the `Feed` method, in the `else` branch (line 97, where `!p.inFileBlock`), add edit block detection **before** the existing `<delete>` check. The new code should be inserted right after `} else {` (line 97):

```go
		} else if p.inEditBlock {
			// Inside an <edit> block — look for <search>, </search>, <replace>, </replace>, </edit>
			p.processEditBlock()
			return
		} else {
```

Then right after the `<delete>` check block (after line 113 `continue`), add before the `<file>` check:

```go
			// Check for <edit path="..."> tags
			if editLoc := editTagRegex.FindStringIndex(p.buffer); editLoc != nil {
				// Check if an <edit> tag comes before any <file> tag
				fileLoc := fileTagRegex.FindStringIndex(p.buffer)
				if fileLoc == nil || editLoc[0] < fileLoc[0] {
					// Emit text before the edit tag
					if editLoc[0] > 0 {
						before := p.buffer[:editLoc[0]]
						if p.OnText != nil && before != "" {
							p.OnText(before)
						}
					}
					match := editTagRegex.FindStringSubmatch(p.buffer[editLoc[0]:editLoc[1]])
					if len(match) >= 2 {
						p.editPath = match[1]
						p.inEditBlock = true
						p.searchContent.Reset()
						p.replaceContent.Reset()
						p.inSearch = false
						p.inReplace = false
						p.buffer = p.buffer[editLoc[1]:]
						continue
					}
				}
			}
```

Also update the partial-tag detection (the `isPartialFileTag` check around line 129) to include `<e` and `<E` for `<edit`:

```go
					isPartialFileTag := strings.HasPrefix(tail, "<f") ||
						strings.HasPrefix(tail, "<F") ||
						strings.HasPrefix(tail, "<e") ||
						strings.HasPrefix(tail, "<E") ||
						strings.HasPrefix(tail, "<d") ||
						strings.HasPrefix(tail, "<D") ||
						tail == "<"
```

- [ ] **Step 3: Add the `processEditBlock` method**

Add this method to `file_parser.go` after the `findRealCloseTag` method:

```go
// processEditBlock processes buffer content while inside an <edit> block.
// Handles the <search>...</search> and <replace>...</replace> sub-blocks,
// and emits OnEdit when </edit> is found.
func (p *FileStreamParser) processEditBlock() {
	for {
		if p.inSearch {
			// Look for </search>
			closeIdx := strings.Index(p.buffer, "</search>")
			if closeIdx == -1 {
				// Buffer everything except last 9 chars ("</search" is 9)
				if len(p.buffer) > 9 {
					p.searchContent.WriteString(p.buffer[:len(p.buffer)-9])
					p.buffer = p.buffer[len(p.buffer)-9:]
				}
				return
			}
			p.searchContent.WriteString(p.buffer[:closeIdx])
			p.inSearch = false
			p.buffer = p.buffer[closeIdx+len("</search>"):]
			continue
		}

		if p.inReplace {
			// Look for </replace>
			closeIdx := strings.Index(p.buffer, "</replace>")
			if closeIdx == -1 {
				// Buffer everything except last 10 chars ("</replace" is 10)
				if len(p.buffer) > 10 {
					p.replaceContent.WriteString(p.buffer[:len(p.buffer)-10])
					p.buffer = p.buffer[len(p.buffer)-10:]
				}
				return
			}
			p.replaceContent.WriteString(p.buffer[:closeIdx])
			p.inReplace = false
			p.buffer = p.buffer[closeIdx+len("</replace>"):]
			continue
		}

		// Not inside <search> or <replace> — look for opening tags or </edit>

		// Check for </edit> closing tag
		if idx := strings.Index(p.buffer, "</edit>"); idx != -1 {
			// Emit the edit
			if p.OnEdit != nil {
				search := p.searchContent.String()
				search = strings.TrimPrefix(search, "\n")
				search = strings.TrimSuffix(search, "\n")
				replace := p.replaceContent.String()
				replace = strings.TrimPrefix(replace, "\n")
				replace = strings.TrimSuffix(replace, "\n")
				p.OnEdit(p.editPath, search, replace)
			}
			p.searchContent.Reset()
			p.replaceContent.Reset()
			p.inEditBlock = false
			p.editPath = ""
			p.buffer = p.buffer[idx+len("</edit>"):]
			return
		}

		// Check for <search> opening tag
		if idx := strings.Index(p.buffer, "<search>"); idx != -1 {
			p.inSearch = true
			p.buffer = p.buffer[idx+len("<search>"):]
			continue
		}

		// Check for <replace> opening tag
		if idx := strings.Index(p.buffer, "<replace>"); idx != -1 {
			p.inReplace = true
			p.buffer = p.buffer[idx+len("<replace>"):]
			continue
		}

		// None of the tags found yet — wait for more data.
		// Keep the buffer (it may contain a partial tag).
		return
	}
}
```

- [ ] **Step 4: Update `Flush` to handle incomplete edit blocks**

In the `Flush` method (line 188), add handling for `inEditBlock` before the existing `inFileBlock` check:

```go
func (p *FileStreamParser) Flush() {
	if p.inEditBlock {
		// Stream ended mid-edit — discard
		slog.Warn("FileStreamParser: discarding incomplete edit block (stream truncated)",
			"path", p.editPath)
		p.searchContent.Reset()
		p.replaceContent.Reset()
		p.inEditBlock = false
		p.inSearch = false
		p.inReplace = false
		p.editPath = ""
		// Fall through to flush remaining buffer as text
	}
	if p.inFileBlock {
		// ... existing code unchanged ...
```

- [ ] **Step 5: Commit**

```bash
git add internal/utils/file_parser.go
git commit -m "feat: extend FileStreamParser with <edit> block parsing"
```

---

### Task 3: Wire `OnEdit` into the chat controller

**Files:**
- Modify: `internal/controllers/app_builder_controller.go`

Add the `OnEdit` handler to the `FileStreamParser` in the chat Phase 2 path so that `<edit>` blocks get applied and saved.

- [ ] **Step 1: Add `OnEdit` callback to the parser initialization**

In `app_builder_controller.go`, in the chat file-block path (the `else` branch starting around line 743), add an `OnEdit` callback to the `FileStreamParser` initialization. Insert it after the `OnDelete` callback (after line 776) and before the `OnText` callback:

```go
			OnEdit: func(path, search, replace string) {
				original, ok := fileMap[path]
				if !ok {
					slog.Warn("edit target file not found in fileMap", "path", path)
					return
				}
				result, applied := utils.ApplyEdit(original, search, replace)
				if !applied {
					preview := search
					if len(preview) > 100 {
						preview = preview[:100] + "..."
					}
					slog.Warn("applyEdit: search text not found",
						"path", path, "search_preview", preview)
					return
				}
				result = utils.StripCodeFences(result)
				_, fileType := guessFileType(path)
				if fileType == "tsx" || fileType == "ts" {
					result, _ = stripForbiddenImports(result, path)
				}
				if err := c.saveGeneratedFile(project, repo, path, result, fileType, fileMap, sendEvent, &toolAppliedOps, true); err != nil {
					slog.Error("edit file save failed", "path", path, "error", err)
				}
				chatFileCounter++
				sendEvent("progress", responses.ProgressEvent{
					Phase:   "generating_files",
					Message: fmt.Sprintf("Updated %s (%d files)", path, chatFileCounter),
					Current: chatFileCounter,
				})
			},
```

- [ ] **Step 2: Build and verify compilation**

```bash
go build ./...
```

Expected: clean build, no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/utils/apply_edit.go internal/controllers/app_builder_controller.go
git commit -m "feat: wire OnEdit handler into chat Phase 2 controller"
```

---

### Task 4: Update system prompt to use edit format

**Files:**
- Modify: `internal/constants/ai_builder.go`

Replace the "How to Edit" and "Output Format" sections in `NEXTJS_BUILDER_CHAT_INSTRUCTIONS` to instruct the LLM to prefer `<edit>` blocks for existing files.

- [ ] **Step 1: Replace the output format section**

In `internal/constants/ai_builder.go`, replace the `NEXTJS_BUILDER_CHAT_INSTRUCTIONS` constant. The section from `### How to Edit an Existing App` through `### Output Format` and the example `<file>` blocks gets replaced. Keep everything from `### Rules` onward unchanged.

Replace lines 850-868 (from `### How to Edit an Existing App` through the example file blocks) with:

```go
### How to Edit an Existing App
The context above shows all project file paths and the contents of files most relevant to the current task.

Use **targeted <edit> blocks** to modify existing files — do NOT output the entire file.
Use **<file> blocks** ONLY for creating new files or when rewriting >50% of a file.
Use **<delete path="..."/>** to remove files.

### Output Format
Write a friendly explanation first (2-4 sentences), then output your edit/file blocks, then a warm summary. IMPORTANT: Always respond in the SAME language as the user's message. If the user writes in English, respond in English. If Turkish, respond in Turkish. Never switch languages mid-response.

**Editing existing files (PREFERRED for all modifications):**
<edit path="app/page.tsx">
<search>
exact lines to find in the current file
</search>
<replace>
replacement content
</replace>
</edit>

**Creating new files or full rewrites:**
<file path="app/new-page.tsx">
complete file content here
</file>

### Rules for <edit> blocks
- The <search> text must EXACTLY match a substring of the current file content — copy it precisely, including whitespace and indentation.
- Include 2-5 surrounding context lines in <search> so the match is unique within the file.
- You can use multiple <edit> blocks for the same file — they are applied in order.
- To delete a section, use an empty <replace></replace>.
- If you need to rewrite most of a file (>50% changed), use <file> instead with complete content.
- NEVER use <edit> on a file whose content is not shown in the context above — if the file is not shown, use <file> instead.
```

- [ ] **Step 2: Update the Rules section**

In the `### Rules` section, update the rules that reference `<file>` blocks to also cover `<edit>` blocks. Replace the first four bullet points:

Old:
```
- CRITICAL SCOPE RULE: Make ONLY the changes the user explicitly requested...
- ONLY output files that you are changing. Do NOT re-output unchanged files.
- Skip template-managed files: lib/utils.ts, components/ui/* (button, card, input, badge, skeleton).
- Do NOT wrap file content in code fences inside <file> blocks.
- CRITICAL: NEVER include the literal string </file> inside file content...
- Before changing a file, look at its full content in the project context above.
```

New:
```
- CRITICAL SCOPE RULE: Make ONLY the changes the user explicitly requested. Do NOT remove, add, or rearrange page sections or components that weren't mentioned. Do NOT redesign layouts, rewrite unrelated content, or restructure pages unless specifically asked. If the user asks to update the company name and contact info, update exactly those fields — don't rewrite the entire page or remove existing components. Preserve the current page structure, component composition, and design unless the user asks to change it.
- ONLY output edits/files that you are changing. Do NOT re-output unchanged files.
- Skip template-managed files: lib/utils.ts, components/ui/* (button, card, input, badge, skeleton).
- Do NOT wrap content in code fences inside <file> or <edit> blocks.
- CRITICAL: NEVER include the literal strings </file>, </edit>, </search>, or </replace> inside block content — the system uses these as boundary markers.
- Before editing a file, look at its full content in the project context above.
```

- [ ] **Step 3: Build and verify**

```bash
go build ./...
```

Expected: clean build.

- [ ] **Step 4: Commit**

```bash
git add internal/constants/ai_builder.go
git commit -m "feat: update chat system prompt to prefer <edit> blocks over full-file output"
```

---

### Task 5: Manual integration test

**Files:** None (testing only)

Verify the full pipeline works end-to-end by testing with a real chat edit session.

- [ ] **Step 1: Build and run the server**

```bash
make build && make run
```

Or with docker:
```bash
docker-compose -f docker-compose-dev.yaml up --build
```

- [ ] **Step 2: Test via the dashboard**

1. Open an existing web studio project in the dashboard
2. Send a simple chat edit like "change the background color to blue" or "change the page title to 'Hello World'"
3. Observe the SSE events — the LLM should output `<edit>` blocks instead of full `<file>` blocks
4. Verify the file is correctly updated in the preview
5. Check server logs for any `"applyEdit: search text not found"` warnings

- [ ] **Step 3: Test fallback — full file rewrite still works**

1. Send a chat message that requires creating a new file (e.g. "add a new /about page")
2. Verify the LLM outputs a `<file>` block for the new file
3. Verify it works correctly

- [ ] **Step 4: Test edge case — multiple edits on same file**

1. Send a message that requires multiple changes to one file (e.g. "change the title and the footer text")
2. Verify multiple `<edit>` blocks are applied sequentially
3. Verify the final file content has both changes

- [ ] **Step 5: Compare token usage**

Check PostHog LLM traces for the test session. Compare `$ai_output_tokens` with previous sessions to verify the reduction.
