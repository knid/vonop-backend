# Skip Phase 2 for Data-Only Operations — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Skip the expensive Phase 2 code-generation LLM call when Phase 1 already fully handled the user's request via data-only tools (store updates, SQL, etc.), saving ~$0.22 and ~6.5s per data-only chat message.

**Architecture:** Add a `CODE_CHANGES_NEEDED` signal to the Phase 1 fast model prompt. When Phase 1 tools handle the request and the model does NOT output this signal, skip Phase 2 entirely and use Phase 1's streamed text as the response. The result struct gets a `SkipPhase2` bool and a `ResponseText` string to carry the Phase 1 output back to the controller.

**Tech Stack:** Go, existing LLM pipeline (`utils.StreamWithTools`, `utils.StreamTextFromLLMWithMessages`)

**Spec:** `docs/superpowers/specs/2026-03-30-skip-phase2-for-data-only-ops-design.md`

---

### Task 1: Extend `chatToolPhaseResult` and result construction

**Files:**
- Modify: `internal/controllers/app_builder_tools.go:38-41` (struct)
- Modify: `internal/controllers/app_builder_tools.go:324-325` (mkResult helper)

- [ ] **Step 1: Add `SkipPhase2` and `ResponseText` fields to the struct**

```go
type chatToolPhaseResult struct {
	Context      string   // Text context gathered by tools
	Screenshots  []string // Base64 data URIs of screenshots captured (for vision)
	SkipPhase2   bool     // true when Phase 1 fully handled the request (no code edits needed)
	ResponseText string   // Phase 1's final text response (used as assistant message when SkipPhase2)
}
```

- [ ] **Step 2: Update `mkResult` helper to accept optional fields**

No change needed — `mkResult` only sets `Context` and `Screenshots`. The new fields will be set explicitly at the call sites that need them (Task 3). Verify `mkResult` still works:

```go
mkResult := func(ctx string) chatToolPhaseResult {
	return chatToolPhaseResult{Context: ctx, Screenshots: screenshots}
}
```

This is unchanged. `SkipPhase2` defaults to `false` and `ResponseText` defaults to `""`.

- [ ] **Step 3: Commit**

```bash
git add internal/controllers/app_builder_tools.go
git commit -m "feat: add SkipPhase2 and ResponseText fields to chatToolPhaseResult"
```

---

### Task 2: Add `CODE_CHANGES_NEEDED` instruction to Phase 1 system prompt

**Files:**
- Modify: `internal/controllers/app_builder_tools.go:161-165` (system prompt)

- [ ] **Step 1: Add the instruction after the existing `NO_TOOLS_NEEDED` block**

Find this line (line 165, end of the prompt addition):

```go
- IMPORTANT: Before calling any tool, ALWAYS write a brief message (1-2 sentences, in the same language as the user's message) explaining what you're about to do. For example: "Let me check what's currently in the database...", "I'll generate a custom image for this...", "Looking at the website to understand the design...". This text is streamed to the user in real time so they know what's happening. Never call a tool without explaining first.`
```

Replace with:

```go
- IMPORTANT: Before calling any tool, ALWAYS write a brief message (1-2 sentences, in the same language as the user's message) explaining what you're about to do. For example: "Let me check what's currently in the database...", "I'll generate a custom image for this...", "Looking at the website to understand the design...". This text is streamed to the user in real time so they know what's happening. Never call a tool without explaining first.
- IMPORTANT: After completing ALL tool calls, decide whether the user's request ALSO needs code file changes (editing .tsx/.ts/.css files). If the tools fully handled everything (e.g. product/price updates, database inserts, env var changes, image generation with no code to update) and NO code edits are needed, write a helpful summary response for the user in their language — do NOT include CODE_CHANGES_NEEDED. If the user's request ALSO requires code changes beyond what the tools did, end your final message with CODE_CHANGES_NEEDED on its own line.`
```

- [ ] **Step 2: Commit**

```bash
git add internal/controllers/app_builder_tools.go
git commit -m "feat: add CODE_CHANGES_NEEDED signal to Phase 1 system prompt"
```

---

### Task 3: Detect `CODE_CHANGES_NEEDED` and set `SkipPhase2` in result parsing

**Files:**
- Modify: `internal/controllers/app_builder_tools.go:338-353` (result parsing after StreamWithTools)

- [ ] **Step 1: Update the result parsing block**

Replace the current block at lines 338-353:

```go
	if collectedContext.Len() == 0 {
		trimmed := strings.TrimSpace(result)
		if trimmed == "" || strings.EqualFold(trimmed, "NO_TOOLS_NEEDED") || strings.Contains(strings.ToUpper(trimmed), "NO_TOOLS_NEEDED") || !toolCalled {
			// Discard Phase 1 text when no tools were actually called.
			// Without this guard, a chatty Haiku response (e.g. "I'll build
			// your website!") leaks into Phase 2 as "Web search context" and
			// can cause the main model to over-generate or rewrite the project.
			slog.Info("Chat tool phase: no tools called, discarding phase 1 text",
				"result_preview", truncate(result, 80), "tool_called", toolCalled)
			return mkResult("")
		}
		// Server-side tool (e.g. web_search) produced a useful response
		return mkResult("Web search context:\n" + trimmed)
	}

	return mkResult(collectedContext.String())
```

With:

```go
	if collectedContext.Len() == 0 {
		trimmed := strings.TrimSpace(result)
		if trimmed == "" || strings.EqualFold(trimmed, "NO_TOOLS_NEEDED") || strings.Contains(strings.ToUpper(trimmed), "NO_TOOLS_NEEDED") || !toolCalled {
			// Discard Phase 1 text when no tools were actually called.
			// Without this guard, a chatty Haiku response (e.g. "I'll build
			// your website!") leaks into Phase 2 as "Web search context" and
			// can cause the main model to over-generate or rewrite the project.
			slog.Info("Chat tool phase: no tools called, discarding phase 1 text",
				"result_preview", truncate(result, 80), "tool_called", toolCalled)
			return mkResult("")
		}
		// Server-side tool (e.g. web_search) produced a useful response
		return mkResult("Web search context:\n" + trimmed)
	}

	// Tools were called and produced context. Check if the fast model
	// determined that code changes are still needed after the tool calls.
	codeChangesNeeded := strings.Contains(strings.ToUpper(result), "CODE_CHANGES_NEEDED")
	if codeChangesNeeded {
		// Strip the signal from the result text
		cleaned := strings.TrimSpace(
			strings.ReplaceAll(strings.ReplaceAll(result, "CODE_CHANGES_NEEDED", ""), "code_changes_needed", ""),
		)
		// Stream any remaining text that wasn't flushed yet
		if cleaned != "" {
			phase1Mu.Lock()
			if !phase1TextFlushed {
				sendEvent("chat_text", responses.ChatTextEvent{Chunk: cleaned})
			}
			phase1Mu.Unlock()
		}
		return mkResult(collectedContext.String())
	}

	// Phase 1 fully handled the request — skip Phase 2.
	slog.Info("Chat tool phase: data-only operation, skipping Phase 2",
		"result_preview", truncate(result, 120),
		"context_len", collectedContext.Len(),
	)
	return chatToolPhaseResult{
		Context:      collectedContext.String(),
		Screenshots:  screenshots,
		SkipPhase2:   true,
		ResponseText: strings.TrimSpace(result),
	}
```

- [ ] **Step 2: Verify `sendEvent` and `phase1Mu` are accessible in this scope**

Both `sendEvent` and `phase1Mu` are declared earlier in the same function (`runChatToolPhase`) — `sendEvent` is a parameter (line 51), `phase1Mu` is a local var (line 251). They are in scope at line 338+. No changes needed.

- [ ] **Step 3: Commit**

```bash
git add internal/controllers/app_builder_tools.go
git commit -m "feat: detect CODE_CHANGES_NEEDED signal and set SkipPhase2 in result"
```

---

### Task 4: Conditionally skip Phase 2 in the controller

**Files:**
- Modify: `internal/controllers/app_builder_controller.go:830-957` (Phase 1 result handling + Phase 2 call)

- [ ] **Step 1: Wrap Phase 2 in a conditional**

After line 830 (`toolResult := c.runChatToolPhase(...)`), replace the block from line 831 to the closing `}` of Phase 2 (line 957).

Current code (lines 831-957) starts with:

```go
		if toolResult.Context != "" {
```

And the Phase 2 block starts at line 858:

```go
		// --- Phase 2: main model single-shot code generation ---
```

Replace the entire block from line 831 to line 957 with:

```go
		if toolResult.SkipPhase2 {
			// Phase 1 fully handled the request (data-only operation).
			// Use Phase 1's streamed text as the final response.
			slog.Info("Skipping Phase 2: data-only operation handled by Phase 1",
				"project_id", project.ID,
				"response_len", len(toolResult.ResponseText),
			)
			responseText = toolResult.ResponseText
		} else {
			if toolResult.Context != "" {
				// Phase 1 tools may have modified files (e.g. run_migrations regenerates
				// lib/schema.ts, manage_env_vars updates .env.local, install_package
				// modifies package.json). Rebuild the project context so Phase 2 sees
				// the latest file state instead of the stale pre-Phase-1 snapshot.
				refreshedContext := c.buildProjectContextForChat(project, fileMap, selectedFiles)
				llmMessages[1] = utils.Message{Role: "system", Content: refreshedContext}

				// Inject tool-gathered context as a system message before the user message
				insertIdx := len(llmMessages) - 1 // before last user message
				if insertIdx < 0 {
					insertIdx = 0
				}
				contextMsg := utils.Message{
					Role:    "system",
					Content: toolResult.Context,
				}
				llmMessages = append(llmMessages[:insertIdx], append([]utils.Message{contextMsg}, llmMessages[insertIdx:]...)...)
			}
			// Attach screenshots as vision images on the last user message
			if len(toolResult.Screenshots) > 0 && provider.SupportsVision() {
				lastIdx := len(llmMessages) - 1
				if lastIdx >= 0 && llmMessages[lastIdx].Role == "user" {
					llmMessages[lastIdx].Images = append(llmMessages[lastIdx].Images, toolResult.Screenshots...)
				}
			}

			// --- Phase 2: main model single-shot code generation ---
			// Signal phase transition so the frontend can clear Phase 1 streaming text
			// and avoid mixing explanatory text with code-gen output.
			sendEvent("phase_change", map[string]string{"phase": "codegen"})

			slog.Info("Calling LLM for chat (file-block path)",
				"model", model,
				"project_id", project.ID,
				"message_count", len(llmMessages),
				"has_tool_context", toolResult.Context != "",
			)

			_, chatErr := utils.StreamTextFromLLMWithMessages(
				ctx, model, llmMessages, provider,
				func(chunk string) { parser.Feed(chunk) },
				func(chunk string) {
					if thinkingStart.IsZero() {
						thinkingStart = time.Now()
					}
					thinkingBuf.WriteString(chunk)
					sendEvent("chat_thinking", responses.ChatThinkingEvent{Chunk: chunk})
				},
				utils.StreamTextConfig{
					MaxTokens: 65536,
					Reasoning: reasoning,
					OnRetry: func(attempt int, reason string) {
						sendEvent("progress", responses.ProgressEvent{
							Phase:   "generating_files",
							Message: fmt.Sprintf("Retrying (attempt %d)...", attempt+1),
						})
					},
				},
			)
			parser.Flush()
			responseText = responseBuilder.String()

			// If the LLM only output file blocks with no surrounding text,
			// generate a minimal summary so the message isn't empty on reload.
			if strings.TrimSpace(responseText) == "" && len(toolAppliedOps) > 0 {
				var changedPaths []string
				for _, op := range toolAppliedOps {
					changedPaths = append(changedPaths, op.Path)
				}
				responseText = fmt.Sprintf("Updated %d file(s): %s", len(changedPaths), strings.Join(changedPaths, ", "))
			}

			if chatErr != nil && !errors.Is(chatErr, utils.ErrResponseTruncated) {
				lastGenErr = chatErr
				slog.Error("LLM chat call failed", "error", chatErr, "project_id", project.ID)

				// Auto-rollback on failure
				if preSnapshotID != "" && len(toolAppliedOps) > 0 {
					slog.Info("Auto-rollback: restoring pre-snapshot after failed chat",
						"project_id", project.ID,
						"snapshot_id", preSnapshotID,
						"partial_ops", len(toolAppliedOps),
					)
					if _, rollbackErr := repo.RevertAndCommit(preSnapshotID, "auto: rollback after failure"); rollbackErr != nil {
						slog.Error("Auto-rollback failed", "error", rollbackErr, "project_id", project.ID)
					} else {
						sendEvent("rollback", map[string]any{
							"snapshot_id": preSnapshotID,
							"reason":      "generation_failed",
							"rolled_back":     len(toolAppliedOps),
							"resync_required": true,
						})
					}
				}

				messages = append(messages, models.AppConversationMessage{
					ID:        uuid.New().String(),
					Role:      "assistant",
					Content:   "Sorry, I couldn't process your request. Please try again.",
					Timestamp: time.Now().Unix(),
				})
				if msgJSON, err := json.Marshal(messages); err != nil {
					slog.Error("failed to marshal messages after LLM error", "error", err, "conv_id", conv.ID)
				} else {
					conv.Messages = msgJSON
					if _, err := c.DB.UpdateAppConversation(conv); err != nil {
						slog.Error("failed to save conversation after LLM error", "error", err, "conv_id", conv.ID)
					}
				}
				sendError("ai", "failed to generate changes. try again please")
				return
			}
			if errors.Is(chatErr, utils.ErrResponseTruncated) {
				slog.Warn("Chat codegen truncated (output token limit reached)",
					"project_id", project.ID,
					"files_generated", len(toolAppliedOps),
					"truncated_file", parser.TruncatedPath,
				)
				if parser.TruncatedPath != "" {
					sendEvent("truncation_warning", map[string]any{
						"truncated_file":  parser.TruncatedPath,
						"files_completed": len(toolAppliedOps),
					})
				}
			}
		}
```

- [ ] **Step 2: Verify build compiles**

Run: `go build ./...`
Expected: Clean build with no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/controllers/app_builder_controller.go
git commit -m "feat: skip Phase 2 code generation for data-only operations

When Phase 1 tools fully handle the user's request (e.g. store updates,
database changes) and the fast model determines no code edits are needed,
skip the expensive Phase 2 LLM call (~59K tokens, ~$0.22 per request).
Phase 1's streamed text becomes the final response."
```

---

### Task 5: Verify build and manual smoke test

**Files:** None (verification only)

- [ ] **Step 1: Build the project**

```bash
make build
```

Expected: Clean build, binary at `bin/app`.

- [ ] **Step 2: Verify no regressions in code paths**

Check that the three chat paths still work:
1. **Data-only** (e.g. "change price to $229") — Phase 1 calls `manage_store`, no `CODE_CHANGES_NEEDED` → Phase 2 skipped
2. **Mixed** (e.g. "change price and make card bigger") — Phase 1 calls `manage_store`, outputs `CODE_CHANGES_NEEDED` → Phase 2 runs
3. **Code-only** (e.g. "change background to blue") — No tools called → `NO_TOOLS_NEEDED` → Phase 2 runs

Verify via logs: look for `"Skipping Phase 2: data-only operation handled by Phase 1"` in case 1, and `"Calling LLM for chat"` in cases 2 and 3.

- [ ] **Step 3: Commit (if any adjustments were needed)**

```bash
git add -A
git commit -m "fix: adjustments from smoke testing Phase 2 skip"
```
