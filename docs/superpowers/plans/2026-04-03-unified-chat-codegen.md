# Unified Chat Code Generation — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the multi-phase chat editing pipeline (4-8 LLM calls) with a single unified `StreamWithTools` call that handles tools and code generation together.

**Architecture:** Single `StreamWithTools` call with Sonnet. Model's text output flows through `FileStreamParser` for `<file>`/`<edit>`/`<delete>` blocks. Tools remain available for DB ops, image gen, etc. Model reads files on-demand via `read_file` tool instead of pre-selecting files.

**Tech Stack:** Go, Anthropic Claude Sonnet API, SSE streaming, Bun ORM, Git

**Spec:** `docs/superpowers/specs/2026-04-03-unified-chat-codegen-design.md`

---

### Task 1: Create unified system prompt constant

**Files:**
- Modify: `internal/constants/ai_builder.go`

- [ ] **Step 1: Add the new unified chat prompt constant**

At the bottom of `internal/constants/ai_builder.go` (after line 999), add:

```go
// NEXTJS_BUILDER_UNIFIED_CHAT_INSTRUCTIONS contains the combined tool + codegen
// instructions for the single-call chat path. Replaces the separate Phase 1
// tool prompt and Phase 2 CHAT_INSTRUCTIONS.
const NEXTJS_BUILDER_UNIFIED_CHAT_INSTRUCTIONS = `

### How to Edit an Existing App
The context above shows all project file paths and sizes. Call read_file to see a file's content before editing.

Use **targeted <edit> blocks** to modify existing files — do NOT output the entire file.
Use **<file> blocks** ONLY for creating new files or when rewriting >50% of a file.
Use **<delete path="..."/>** to remove files.

### Output Format
Write a friendly explanation first (2-4 sentences), then output your edit/file blocks, then a warm summary. IMPORTANT: Always respond in the SAME language as the user's message.

**Editing existing files (PREFERRED for all modifications):**
<edit path="app/page.tsx">
<search>
exact lines to find in the current file
` + "</search>" + `
<replace>
replacement content
` + "</replace>" + `
` + "</edit>" + `

**Creating new files or full rewrites:**
<file path="app/new-page.tsx">
complete file content here
` + "</file>" + `

### Rules for <edit> blocks
- The <search> text must EXACTLY match a substring of the current file content — copy it precisely, including whitespace and indentation.
- Include 2-5 surrounding context lines in <search> so the match is unique within the file.
- You can use multiple <edit> blocks for the same file — they are applied in order.
- To delete a section, use an empty ` + "`<replace></replace>`" + `.
- If you need to rewrite most of a file (>50% changed), use <file> instead with complete content.
- If you haven't read the file yet, call read_file FIRST before using <edit>. Never guess file contents.
- If an edit fails, you will receive an error message. Retry with corrected search text or use <file> for full rewrite.

### Tools
You have tools available. Use them when needed:
- Call read_file to see the content of a file before editing it.
- Call execute_sql to diagnose database issues or modify data.
- Call generate_image when the user explicitly asks to create/generate custom images.
- Call preview_website or take_screenshot to analyze URLs.
- Call manage_env_vars, install_package, search_components, manage_assets as needed.
- Call seed_database ONLY when the user says the database is empty or asks for demo data.
- Call run_migrations for schema changes (ALTER TABLE, CREATE TABLE).

Before calling any tool, write a brief message (1-2 sentences, in the user's language) explaining what you're doing.

### Rules
- CRITICAL SCOPE RULE: Make ONLY the changes the user explicitly requested. Do NOT remove, add, or rearrange page sections or components that weren't mentioned. Preserve the current page structure unless the user asks to change it.
- ONLY output edits/files that you are changing. Do NOT re-output unchanged files.
- Skip template-managed files: lib/utils.ts, components/ui/* (button, card, input, badge, skeleton).
- Do NOT wrap content in code fences inside <file> or <edit> blocks.
- CRITICAL: NEVER include the literal strings ` + "`</file>`" + `, ` + "`</edit>`" + `, ` + "`</search>`" + `, or ` + "`</replace>`" + ` inside block content.

### Fixing Errors
When the user reports a runtime error, build error, or TypeError:
- **Diagnose first**: Read the error message carefully. Call read_file on the erroring file.
- **Fix only what's broken**: Output only the file(s) that need changes.
- **Do NOT change UI/design**: Error fixes must be scoped to the bug.

### Website Conversion
When the context includes a [Website Analysis] section, the user wants to convert that website into a mobile app. Use the analysis to create proper mobile screens.

### Store Updates
When the context includes a [Store Update] section, products/categories/discounts were just created or modified. If code changes are needed, output them. If purely data, just confirm.`

// NEXTJS_BUILDER_UNIFIED_CHAT_PROMPT is the composed prompt for the unified single-call chat path.
const NEXTJS_BUILDER_UNIFIED_CHAT_PROMPT = NEXTJS_BUILDER_COMMON_RULES + NEXTJS_BUILDER_UNIFIED_CHAT_INSTRUCTIONS
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./...`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add internal/constants/ai_builder.go
git commit -m "feat: add unified chat system prompt constant"
```

---

### Task 2: Add buildUnifiedChatSystemPrompt function

**Files:**
- Modify: `internal/controllers/app_builder_context.go`

- [ ] **Step 1: Add the new prompt builder function**

After the existing `buildChatSystemPrompt` function (around line 366), add:

```go
// buildUnifiedChatSystemPrompt assembles the system prompt for the unified
// single-call chat path that combines tool usage and code generation.
func buildUnifiedChatSystemPrompt(project *models.AppProject, mobileConfig *models.MobileAppConfig) string {
	var prompt string
	if project.ProjectType == "ecommerce" {
		prompt = constants.NEXTJS_ECOMMERCE_COMMON_RULES + constants.NEXTJS_BUILDER_UNIFIED_CHAT_INSTRUCTIONS
	} else {
		prompt = constants.NEXTJS_BUILDER_UNIFIED_CHAT_PROMPT
		if projectHasDataModels(project) {
			prompt += constants.NEXTJS_BUILDER_DATABASE_RULES
		}
	}

	if settings := project.Settings; settings != nil {
		if needsAuth, ok := settings["needs_auth"].(bool); ok && needsAuth {
			prompt += constants.NEXTJS_BUILDER_AUTH_RULES
		}
	}

	if project.ProjectType == "mobile_app" {
		prompt += constants.NEXTJS_BUILDER_MOBILE_RULES
		prompt += constants.BuildBridgeSDKRules(constants.ALL_BRIDGE_METHODS)
	}

	return prompt
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./...`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add internal/controllers/app_builder_context.go
git commit -m "feat: add buildUnifiedChatSystemPrompt function"
```

---

### Task 3: Simplify buildProjectContextForChat to file-list only

**Files:**
- Modify: `internal/controllers/app_builder_context.go`

- [ ] **Step 1: Add new buildFileListContext function**

Add this function after `buildUnifiedChatSystemPrompt`:

```go
// buildFileListContext builds the project context with file paths and sizes only
// (no file contents). The model reads files on-demand via the read_file tool.
func (c *Controller) buildFileListContext(project *models.AppProject, fileMap map[string]string) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("App: %s (slug: %s)\n", project.Name, project.Slug))
	sb.WriteString(fmt.Sprintf("Project type: %s\n", project.ProjectType))

	// Inject persisted project memory
	if memCtx := c.buildProjectMemoryContext(project.ID.String()); memCtx != "" {
		sb.WriteString(memCtx)
	}

	// Logo URL
	if logoURL := c.getProjectLogoURL(project); logoURL != "" {
		sb.WriteString(fmt.Sprintf("\nApp Logo URL (use this as the site logo in the header/navbar via next/image and as the favicon in metadata — do not use text for the brand name, always use this logo image):\n%s\n", logoURL))
	}

	// Theme
	var theme map[string]any
	if len(project.Theme) > 0 && project.Theme[0] == '{' {
		if err := json.Unmarshal(project.Theme, &theme); err != nil {
			slog.Warn("failed to unmarshal project theme", "error", err, "project_id", project.ID)
		}
	} else if len(project.Theme) > 0 {
		var raw string
		if json.Unmarshal(project.Theme, &raw) == nil && len(raw) > 0 && raw[0] == '{' {
			if err := json.Unmarshal([]byte(raw), &theme); err != nil {
				slog.Warn("failed to unmarshal double-encoded project theme", "error", err, "project_id", project.ID)
			}
		}
	}
	if len(theme) > 0 {
		sb.WriteString(fmt.Sprintf("Theme: %v\n", theme))
	}

	// Installed UI components
	if components := detectInstalledUIComponents(fileMap); len(components) > 0 {
		sb.WriteString("\n### Installed UI Components (USE THESE — do not rebuild from scratch)\n")
		sb.WriteString("Import from @/components/ui/<name>. Available:\n")
		for _, comp := range components {
			sb.WriteString(fmt.Sprintf("- %s\n", comp))
		}
	}

	// File list (paths + sizes only)
	if len(fileMap) > 0 {
		paths := make([]string, 0, len(fileMap))
		for p := range fileMap {
			paths = append(paths, p)
		}
		sort.Strings(paths)

		sb.WriteString("\nAll project files (call read_file to see content):\n")
		for _, fp := range paths {
			if isSecretFile(fp) {
				continue
			}
			sb.WriteString(fmt.Sprintf("- %s (%d chars)\n", fp, len(fileMap[fp])))
		}
	}

	// User-uploaded assets
	chatAssets, _ := c.DB.GetAppAssetsByProjectID(project.ID.String())
	if len(chatAssets) > 0 {
		sb.WriteString("\nUser-uploaded assets (use these URLs directly in code):\n")
		for _, a := range chatAssets {
			sb.WriteString(fmt.Sprintf("- %s (%s) → %s\n", a.FileName, a.ContentType, a.PublicURL))
		}
	} else {
		sb.WriteString("\nNo user-uploaded assets. If the user's request requires custom files (3D models, fonts, images, videos, etc.), tell them to upload the files first using the Assets button (paperclip icon) in the chat toolbar.\n")
	}

	// Available imports
	sb.WriteString(buildAvailableImportsSection(fileMap))

	return sb.String()
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./...`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add internal/controllers/app_builder_context.go
git commit -m "feat: add buildFileListContext for unified chat path"
```

---

### Task 4: Add simple conversation history builder

**Files:**
- Modify: `internal/controllers/app_builder_context.go`

- [ ] **Step 1: Add buildSimpleChatHistory function**

Add after `buildFileListContext`:

```go
// buildSimpleChatHistory builds conversation history for the unified chat path.
// Keeps last keepRecent messages with full content, truncates older messages.
// No LLM summarization — just simple truncation.
func buildSimpleChatHistory(
	conv *models.AppConversation,
	messages []models.AppConversationMessage,
	provider *utils.LLMProvider,
	imageURLs []string,
	keepRecent int,
) []utils.Message {
	var history []utils.Message

	// Include existing summary if available (backwards compatible)
	if conv.Summary != "" {
		history = append(history, utils.Message{
			Role:    "user",
			Content: fmt.Sprintf("[Earlier conversation summary]\n%s", utils.SanitizePromptContent(conv.Summary)),
		})
		history = append(history, utils.Message{
			Role:    "assistant",
			Content: "I understand the earlier context. Let me continue from where we left off.",
		})
	}

	if len(messages) <= keepRecent {
		// All messages fit — include them all with full content
		history = append(history, chatMessagesToLLM(messages, provider, imageURLs)...)
		return history
	}

	// Truncate older messages, keep recent ones full
	olderMessages := messages[:len(messages)-keepRecent]
	recentMessages := messages[len(messages)-keepRecent:]

	// Older messages: truncated content + file operations summary
	for _, msg := range olderMessages {
		content := msg.Content
		if len(content) > 200 {
			content = content[:200] + "..."
		}
		if len(msg.FileOperations) > 0 {
			var ops []string
			for _, op := range msg.FileOperations {
				ops = append(ops, fmt.Sprintf("%s %s", op.Action, op.FilePath))
			}
			content += fmt.Sprintf("\n[Files: %s]", strings.Join(ops, ", "))
		}
		history = append(history, utils.Message{Role: msg.Role, Content: content})
	}

	// Recent messages: full content
	history = append(history, chatMessagesToLLM(recentMessages, provider, imageURLs)...)

	// Prune heavy fields from older messages in the DB to prevent blob growth
	for i := range olderMessages {
		messages[i].ThinkingText = ""
		messages[i].ToolCalls = nil
	}

	return history
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./...`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add internal/controllers/app_builder_context.go
git commit -m "feat: add buildSimpleChatHistory without LLM summarization"
```

---

### Task 5: Add tag leak prevention to saveGeneratedFile

**Files:**
- Modify: `internal/controllers/app_builder_controller.go`

- [ ] **Step 1: Add tag sanitization**

In `saveGeneratedFile` (line 1186), find the function body start and add sanitization after the `path` and `content` parameters are used but before the file is written. Find this line:

```go
func (c *Controller) saveGeneratedFile(
	project *models.AppProject,
	repo *gitrepo.Repo,
	path string,
```

Read the full function to find the right insertion point. Add these lines at the start of the function body (after parameter declarations):

```go
	// Strip parser boundary tags that may have leaked into generated content.
	// Prevents syntax errors from literal </search>, </edit> etc. in code.
	for _, tag := range []string{"</search>", "</replace>", "</edit>", "</file>"} {
		content = strings.ReplaceAll(content, tag, "")
	}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./...`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add internal/controllers/app_builder_controller.go
git commit -m "fix: strip leaked parser boundary tags from generated file content"
```

---

### Task 6: Add tool executor with read_file support for unified path

**Files:**
- Modify: `internal/controllers/app_builder_tools.go`

- [ ] **Step 1: Add unified tool executor function**

Add a new function after `runChatToolPhase` (which we'll remove later but leave for now). This function builds the tool list and executor for the unified path, reusing all existing `executeChat*` functions:

```go
// buildUnifiedChatTools returns the tools list and executor for the unified
// single-call chat path. Same tools as Phase 1, but the executor also handles
// file mutations via the shared fileMap (since there's no separate Phase 2).
func (c *Controller) buildUnifiedChatTools(
	ctx context.Context,
	project *models.AppProject,
	provider *utils.LLMProvider,
	repo *gitrepo.Repo,
	fileMap map[string]string,
	sendEvent func(string, any) bool,
	userLimits *models.StandartUserLimits,
) ([]utils.Tool, utils.ToolExecutor) {
	tools := []utils.Tool{
		chatPreviewWebsiteTool,
		chatManageEnvVarsTool,
		chatInstallPackageTool,
		chatGenerateImageTool,
		chatTakeScreenshotTool,
		chatSearchComponentsTool,
		chatManageAssetsTool,
		chatReadFileTool,
		chatListFilesTool,
		chatDiffFilesTool,
		chatGitLogTool,
	}
	if project.ProjectType == "ecommerce" {
		tools = append(tools, chatManageStoreTool)
	}
	if c.neonConnString(project.ID.String()) != "" {
		tools = append(tools, chatSeedDatabaseTool, chatExecuteSQLTool, chatRunMigrationsTool)
	}
	if project.ProjectType == "mobile_app" {
		tools = append(tools, chatConfigureMobileAppTool)
	}
	if ws := provider.WebSearchTool(); ws != nil {
		tools = append(tools, *ws)
	}

	// Shared context collector for tool results that should be visible to the model
	var collectedContext strings.Builder
	var screenshots []string

	executor := func(name string, arguments string) (string, error) {
		// Track tool usage on the token tracker
		if tracker := utils.TokenTrackerFromContext(ctx); tracker != nil {
			tracker.RecordToolUsed(name)
		}
		switch name {
		case "preview_website":
			return c.executeChatPreviewWebsite(arguments, project, sendEvent, &collectedContext, &screenshots)
		case "manage_store":
			return c.executeChatManageStore(arguments, project, sendEvent, &collectedContext)
		case "seed_database":
			return c.executeChatSeedDatabase(ctx, project, sendEvent, &collectedContext)
		case "execute_sql":
			return c.executeChatExecuteSQL(ctx, arguments, project, &collectedContext)
		case "run_migrations":
			return c.executeChatRunMigrations(ctx, arguments, project, repo, fileMap, sendEvent, &collectedContext)
		case "manage_env_vars":
			return c.executeChatManageEnvVars(arguments, project, repo, fileMap, sendEvent, &collectedContext)
		case "install_package":
			return c.executeChatInstallPackage(arguments, project, repo, fileMap, sendEvent, &collectedContext)
		case "generate_image":
			return c.executeChatGenerateImage(ctx, arguments, project, sendEvent, &collectedContext)
		case "take_screenshot":
			return c.executeChatTakeScreenshot(arguments, &collectedContext, &screenshots)
		case "search_components":
			return c.executeChatSearchComponents(arguments, project, repo, fileMap, sendEvent, &collectedContext)
		case "configure_mobile_app":
			return c.executeChatConfigureMobileApp(ctx, arguments, project, sendEvent, &collectedContext)
		case "read_file":
			return c.executeChatReadFile(arguments, fileMap)
		case "list_files":
			return c.executeChatListFiles(arguments, fileMap)
		case "diff_files":
			var args struct {
				FromCommit string `json:"from_commit"`
				ToCommit   string `json:"to_commit"`
			}
			if err := json.Unmarshal([]byte(arguments), &args); err != nil {
				return "Invalid arguments", nil
			}
			if args.FromCommit == "" {
				diff, err := repo.DiffLastCommit()
				if err != nil {
					return fmt.Sprintf("Error: %s", err.Error()), nil
				}
				if diff == "" {
					return "No changes in the last commit.", nil
				}
				return diff, nil
			}
			diff, err := repo.Diff(args.FromCommit, args.ToCommit)
			if err != nil {
				return fmt.Sprintf("Error: %s", err.Error()), nil
			}
			if diff == "" {
				return "No differences found.", nil
			}
			return diff, nil
		case "git_log":
			var args struct {
				Limit int `json:"limit"`
			}
			if err := json.Unmarshal([]byte(arguments), &args); err != nil {
				args.Limit = 10
			}
			if args.Limit <= 0 {
				args.Limit = 10
			}
			if args.Limit > 50 {
				args.Limit = 50
			}
			commits, err := repo.Log(args.Limit)
			if err != nil {
				return fmt.Sprintf("Error: %s", err.Error()), nil
			}
			if len(commits) == 0 {
				return "No commits found.", nil
			}
			var lines []string
			for _, cm := range commits {
				lines = append(lines, fmt.Sprintf("%s  %s  %s", cm.Hash[:8], cm.Timestamp.Format("2006-01-02 15:04"), cm.Message))
			}
			return strings.Join(lines, "\n"), nil
		case "manage_assets":
			return c.executeChatManageAssets(arguments, project, sendEvent, &collectedContext)
		default:
			return fmt.Sprintf("Unknown tool: %s", name), nil
		}
	}

	return tools, executor
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./...`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add internal/controllers/app_builder_tools.go
git commit -m "feat: add buildUnifiedChatTools for single-call chat path"
```

---

### Task 7: Replace chat path in StreamBuildApp with unified call

This is the core change. Replace the Phase 1 → Phase 2 pipeline (lines ~460-967 in the `else` branch of `StreamBuildApp`) with the unified single-call approach.

**Files:**
- Modify: `internal/controllers/app_builder_controller.go`

- [ ] **Step 1: Replace the chat path**

Find the `} else {` block starting at line 478 (the chat path after `if isInitialGeneration`). Replace the entire chat path from the conversation history building through the end of Phase 2 codegen (up to the `}` closing the `else` block before `// Record conversation tracing`).

Replace the block from:
```go
	} else {
		// Chat path: start file selection in background while we prepare conversation history.
```

Through to (but NOT including):
```go
	// Record conversation tracing
	tracker.TraceInput = message
```

With:
```go
	} else {
		// === Unified chat path: single StreamWithTools call ===

		// Build system prompt
		chatSystemPrompt := buildUnifiedChatSystemPrompt(project, mobileConfig)

		// Build project context (file list only, no content)
		projectContext := c.buildFileListContext(project, fileMap)

		// Add DB schema to system prompt if applicable
		if hasDB := c.neonConnString(project.ID.String()) != ""; hasDB {
			dataModels, err := parseDataModels(project.DataModels)
			if err != nil {
				slog.Warn("failed to parse data models", "error", err)
			}
			if len(dataModels) > 0 {
				chatSystemPrompt += "\n\n### Database Schema\n"
				for _, dm := range dataModels {
					chatSystemPrompt += fmt.Sprintf("Table: %s\n  Columns: ", dm.Name)
					for i, col := range dm.Columns {
						if i > 0 {
							chatSystemPrompt += ", "
						}
						chatSystemPrompt += fmt.Sprintf("%s (%s)", col.Name, col.Type)
					}
					chatSystemPrompt += "\n"
				}
				chatSystemPrompt += "All tables have auto-generated UUID 'id' and timestamp created_at/updated_at columns — do NOT provide them in INSERT statements.\n"
			}
		}

		// Build conversation history (simple truncation, no LLM summarization)
		keepRecent := c.RemoteConfig.GetInt(remoteconfig.AIChatKeepRecent, remoteconfig.Global)
		chatHistory := buildSimpleChatHistory(conv, messages, provider, imageURLs, keepRecent)

		llmMessages = []utils.Message{
			{Role: "system", Content: chatSystemPrompt},
			{Role: "system", Content: projectContext},
		}
		llmMessages = append(llmMessages, chatHistory...)

		// --- Unified StreamWithTools call ---
		var responseBuilder strings.Builder
		chatFileCounter := 0

		// Track failed edits for retry
		type failedEdit struct {
			Path          string
			SearchPreview string
		}
		var failedEdits []failedEdit
		var failedEditsMu sync.Mutex

		parser := &utils.FileStreamParser{
			DiscardIncomplete: true,
			OnFile: func(path, content string) {
				content = utils.StripCodeFences(content)
				_, fileType := guessFileType(path)
				if fileType == "tsx" || fileType == "ts" {
					content, _ = stripForbiddenImports(content, path)
				}
				if err := c.saveGeneratedFile(project, repo, path, content, fileType, fileMap, sendEvent, &toolAppliedOps, true); err != nil {
					slog.Error("chat file save failed", "path", path, "error", err)
				}
				chatFileCounter++
				sendEvent("progress", responses.ProgressEvent{
					Phase:   "generating_files",
					Message: fmt.Sprintf("Updated %s (%d files)", path, chatFileCounter),
					Current: chatFileCounter,
				})
			},
			OnDelete: func(path string) {
				if err := c.deleteGeneratedFile(project, repo, path, fileMap, sendEvent, &toolAppliedOps); err != nil {
					slog.Error("chat file delete failed", "path", path, "error", err)
				}
			},
			OnEdit: func(path, search, replace string) {
				original, ok := fileMap[path]
				if !ok {
					slog.Warn("edit target file not found in fileMap", "path", path)
					failedEditsMu.Lock()
					failedEdits = append(failedEdits, failedEdit{Path: path, SearchPreview: "file not in fileMap"})
					failedEditsMu.Unlock()
					return
				}
				result, applied := utils.ApplyEdit(original, search, replace)
				if !applied {
					preview := search
					if len(preview) > 100 {
						preview = preview[:100] + "..."
					}
					slog.Warn("applyEdit: search text not found", "path", path, "search_preview", preview)
					failedEditsMu.Lock()
					failedEdits = append(failedEdits, failedEdit{Path: path, SearchPreview: preview})
					failedEditsMu.Unlock()
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
			OnText: func(text string) {
				responseBuilder.WriteString(text)
				cleaned := ansiRegex.ReplaceAllString(text, "")
				if cleaned != "" {
					sendEvent("chat_text", responses.ChatTextEvent{Chunk: cleaned})
				}
			},
		}

		// Reasoning config — always Sonnet, no Opus switching
		reasoning := c.reasoningConfig()
		if reasoningEffort != "" && reasoningEffort != "none" {
			reasoning = &utils.ReasoningConfig{Effort: reasoningEffort}
		} else if reasoningEffort == "none" {
			reasoning = nil
		}

		// Build tools and executor
		tools, executor := c.buildUnifiedChatTools(ctx, project, provider, repo, fileMap, sendEvent, userLimits)

		slog.Info("Calling LLM for unified chat",
			"model", model,
			"project_id", project.ID,
			"message_count", len(llmMessages),
			"tool_count", len(tools),
		)

		_, chatErr := utils.StreamWithTools(
			ctx, model, llmMessages,
			tools, provider,
			nil,       // no response format
			reasoning,
			executor,
			func(toolName string, args string) {
				sendEvent("tool_start", map[string]string{"tool": toolName})
			},
			func(chunk string) { parser.Feed(chunk) },
			func(chunk string) {
				if thinkingStart.IsZero() {
					thinkingStart = time.Now()
				}
				thinkingBuf.WriteString(chunk)
				sendEvent("chat_thinking", responses.ChatThinkingEvent{Chunk: chunk})
			},
			utils.ToolCallConfig{
				MaxRounds:        10,
				NudgeAfter:       5,
				StripAfter:       7,
				MaxTokens:        65536,
				MaxContinuations: 3,
				NudgeMessage:     "You have gathered enough context. Output your code changes now using <file> and <edit> blocks.",
				OnRoundStart: func(round int) {
					if round > 0 {
						parser.Flush()
					}
				},
			},
		)
		parser.Flush()
		responseText = responseBuilder.String()

		// Generate minimal summary if only file blocks were output
		if strings.TrimSpace(responseText) == "" && len(toolAppliedOps) > 0 {
			var changedPaths []string
			for _, op := range toolAppliedOps {
				changedPaths = append(changedPaths, op.Path)
			}
			responseText = fmt.Sprintf("Updated %d file(s): %s", len(changedPaths), strings.Join(changedPaths, ", "))
		}

		// Edit failure retry: if edits failed, give the model one chance to fix them
		if len(failedEdits) > 0 && chatErr == nil {
			var errMsg strings.Builder
			for _, f := range failedEdits {
				errMsg.WriteString(fmt.Sprintf("Edit failed for %q: search text not found (preview: %q). Fix the <search> text to match exactly, or use <file> for full rewrite.\n", f.Path, f.SearchPreview))
			}
			slog.Info("Edit failures detected, retrying",
				"project_id", project.ID,
				"failed_count", len(failedEdits),
			)

			// Reset for retry
			failedEdits = nil
			retryMessages := append(llmMessages, utils.Message{
				Role:    "assistant",
				Content: responseText,
			}, utils.Message{
				Role:    "system",
				Content: errMsg.String(),
			})

			_, retryErr := utils.StreamWithTools(
				ctx, model, retryMessages,
				tools, provider,
				nil, reasoning, executor,
				func(toolName string, args string) {
					sendEvent("tool_start", map[string]string{"tool": toolName})
				},
				func(chunk string) { parser.Feed(chunk) },
				func(chunk string) {
					thinkingBuf.WriteString(chunk)
					sendEvent("chat_thinking", responses.ChatThinkingEvent{Chunk: chunk})
				},
				utils.ToolCallConfig{
					MaxRounds:        3,
					MaxTokens:        65536,
					MaxContinuations: 1,
				},
			)
			parser.Flush()
			if retryErr != nil {
				slog.Warn("Edit retry failed", "error", retryErr, "project_id", project.ID)
			}
		}

		if chatErr != nil && !errors.Is(chatErr, utils.ErrResponseTruncated) {
			lastGenErr = chatErr
			slog.Error("Unified chat call failed", "error", chatErr, "project_id", project.ID)

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
						"snapshot_id":     preSnapshotID,
						"reason":          "generation_failed",
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
			slog.Warn("Unified chat truncated",
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

- [ ] **Step 2: Remove unused imports if any**

Check that the removed code doesn't leave orphaned imports. The `sync` import is still needed. The `selectedFiles` variable declaration and `fileSelWG` should be removed.

- [ ] **Step 3: Verify it compiles**

Run: `go build ./...`
Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add internal/controllers/app_builder_controller.go
git commit -m "feat: replace Phase 1 + Phase 2 pipeline with unified StreamWithTools call"
```

---

### Task 8: Remove model switching for reasoning effort

**Files:**
- Modify: `internal/controllers/app_builder_controller.go`

- [ ] **Step 1: Simplify model selection for chat path**

Find the model selection block (around lines 193-209):

```go
	} else {
		model = c.SecretManager.GetSecret("WEBSITE_BUILDER_CHAT_MODEL")
		if model == "" {
			model = c.SecretManager.GetSecret("WEBSITE_BUILDER_LLM_MODEL")
		}
		if model == "" {
			model = provider.DefaultModel()
		}
		// Override to Opus if user requests high reasoning
		if reasoningEffort == "high" || reasoningEffort == "xhigh" {
			if provider.IsAnthropic() {
				model = "claude-opus-4-6"
			} else {
				model = "anthropic/claude-opus-4-6"
			}
		}
	}
```

Replace with:

```go
	} else {
		model = c.SecretManager.GetSecret("WEBSITE_BUILDER_CHAT_MODEL")
		if model == "" {
			model = c.SecretManager.GetSecret("WEBSITE_BUILDER_LLM_MODEL")
		}
		if model == "" {
			model = provider.DefaultModel()
		}
		// No Opus switching — always Sonnet, reasoning effort handled via ReasoningConfig
	}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./...`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add internal/controllers/app_builder_controller.go
git commit -m "feat: remove Opus model switching, always use Sonnet with ReasoningConfig"
```

---

### Task 9: Clean up dead code

**Files:**
- Modify: `internal/controllers/app_builder_tools.go`
- Modify: `internal/controllers/app_builder_context.go`
- Modify: `internal/controllers/app_builder_conversation.go`

- [ ] **Step 1: Mark runChatToolPhase as deprecated**

In `internal/controllers/app_builder_tools.go`, add a deprecation comment above `runChatToolPhase`:

```go
// Deprecated: runChatToolPhase is no longer used by the unified chat path.
// Kept temporarily for reference. Remove after verifying unified path works.
```

- [ ] **Step 2: Mark selectContextFiles as deprecated**

In `internal/controllers/app_builder_context.go`, add above `selectContextFiles`:

```go
// Deprecated: selectContextFiles is no longer used by the unified chat path.
// The model reads files on-demand via the read_file tool.
// Kept temporarily for reference. Remove after verifying unified path works.
```

- [ ] **Step 3: Mark summarizeConversation as deprecated**

In `internal/controllers/app_builder_conversation.go`, add above `summarizeConversation`:

```go
// Deprecated: summarizeConversation is no longer used by the unified chat path.
// Conversation history uses simple truncation instead.
// Kept temporarily for reference. Remove after verifying unified path works.
```

- [ ] **Step 4: Verify it compiles**

Run: `go build ./...`
Expected: no errors. If there are unused variable warnings from the old chat path being gone, fix them.

- [ ] **Step 5: Commit**

```bash
git add internal/controllers/app_builder_tools.go internal/controllers/app_builder_context.go internal/controllers/app_builder_conversation.go
git commit -m "chore: deprecate Phase 1/Phase 2 functions replaced by unified chat path"
```

---

### Task 10: Manual integration test

No automated test suite exists in this project. Verify the change works end-to-end.

**Files:** None (testing only)

- [ ] **Step 1: Build the project**

Run: `make build`
Expected: successful build with no errors

- [ ] **Step 2: Test a simple chat edit**

Start the dev server and send a chat message via the API (or use the frontend):
- Simple text change: "Change the heading to Welcome"
- Verify: model calls `read_file`, reads the file, outputs `<edit>` block, file updates

- [ ] **Step 3: Test a tool-using chat edit**

Send a message that requires tools:
- Database query: "Show me what's in the users table"
- Verify: model calls `execute_sql`, returns results, responds naturally

- [ ] **Step 4: Test a greeting (no code needed)**

Send: "hello" or "selam"
- Verify: model responds with a friendly greeting, no file changes, no tool calls

- [ ] **Step 5: Test edit failure retry**

Send a message that references a file the model hasn't read:
- Verify: if an `<edit>` fails, the retry mechanism kicks in and the model corrects itself

- [ ] **Step 6: Commit final verification**

```bash
git add -A
git commit -m "feat: unified chat codegen - all tasks complete"
```
