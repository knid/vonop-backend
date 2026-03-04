package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/services/integrations"
	"github.com/knid/wemob/internal/utils"
	"github.com/posthog/posthog-go"
)

// ansiRegex strips ANSI escape sequences that some LLM providers emit in streamed text.
var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// normalizeContent trims and collapses whitespace for content comparison.
func normalizeContent(s string) string {
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}

// appBuilderOperation tracks a file mutation applied during a build session.
type appBuilderOperation struct {
	Action string `json:"action"` // "create", "modify", "delete"
	Path   string `json:"path"`
	Type   string `json:"type"` // file type: "tsx", "ts", "css", etc.
}

// builderToolCallbacks holds callbacks for file-mutating tool operations so the
// executor can emit SSE events and track changes as they happen.
type builderToolCallbacks struct {
	sendEvent      func(eventName string, data any) bool
	appliedOps     *[]appBuilderOperation
	mu             sync.Mutex
	fileMu         sync.RWMutex           // guards fileMap reads/writes
	pendingFiles   map[string]string       // files changed since last preview sync
	pendingDeletes []string               // files deleted since last preview sync
}

// StreamBuildApp is the unified entry point for both initial app generation and chat editing.
// When isInitialGeneration=true, it creates a new app from a prompt.
// When isInitialGeneration=false, it processes a chat edit on an existing app.
// jobID is used to coordinate clarification questions; pass "" to skip clarification.
func (c *Controller) StreamBuildApp(
	ctx context.Context,
	project *models.AppProject,
	user *models.User,
	message string,
	images []string,
	reasoningEffort string,
	isInitialGeneration bool,
	jobID string,
	events chan<- responses.SSEEvent,
) {
	var closeEvents sync.Once
	defer func() { closeEvents.Do(func() { close(events) }) }()

	sendEvent := func(eventName string, data any) bool {
		select {
		case <-ctx.Done():
			return false
		case events <- responses.SSEEvent{Event: eventName, Data: data}:
			return true
		}
	}

	sendError := func(phase string, errMsg string) {
		sendEvent("error", responses.GenerationErrorEvent{
			Error: errMsg,
			Phase: phase,
		})
	}

	// Rollback state — captured by panic handler and error paths
	var rollbackSnapshotID string
	var rollbackOps *[]appBuilderOperation

	defer func() {
		if r := recover(); r != nil {
			slog.Error("StreamBuildApp panicked", "error", r, "project_id", project.ID)
			// Auto-rollback on panic
			if rollbackSnapshotID != "" && rollbackOps != nil && len(*rollbackOps) > 0 {
				slog.Info("Auto-rollback: restoring after panic", "project_id", project.ID)
				c.RestoreAppSnapshot(project, rollbackSnapshotID)
			}
			sendEvent("error", responses.GenerationErrorEvent{
				Error: fmt.Sprintf("internal error: %v", r),
				Phase: "generation",
			})
		}
	}()

	// --- Provider & model selection ---
	provider := c.llmProvider()

	var model string
	if isInitialGeneration {
		model = c.SecretManager.GetSecret("WEBSITE_BUILDER_LLM_MODEL")
		if model == "" {
			model = provider.DefaultModel() // defaults to claude-sonnet-4
		}
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

	// --- Token tracking ---
	genStart := time.Now()
	tracker := utils.NewTokenTracker()
	tracker.TrackContent = true
	ctx = utils.WithTokenTracker(ctx, tracker)

	actionLabel := "app_chat"
	if isInitialGeneration {
		actionLabel = "generate_app"
	}

	defer func() {
		closeEvents.Do(func() { close(events) })
		c.RecordTokenUsage(user.ID, actionLabel, model, provider.Name, project.ID.String(), project.ProjectType, tracker)
		prompt, completion, total, callCount := tracker.Totals()
		if total > 0 {
			credits := models.TokensToCredits(prompt, completion)
			if err := c.DeductAICreditsUnchecked(user, credits, actionLabel, project.ID.String()); err != nil {
				slog.Warn("failed to deduct AI credits", "error", err,
					"user_id", user.ID, "credits", credits, "tokens", total)
			}
			genDuration := time.Since(genStart).Seconds()
			slog.Info("=== BUILD SESSION SUMMARY ===",
				"project_id", project.ID,
				"is_generation", isInitialGeneration,
				"duration_s", fmt.Sprintf("%.1f", genDuration),
				"llm_calls", callCount,
				"prompt_tokens", prompt,
				"completion_tokens", completion,
				"total_tokens", total,
				"credits_used", credits,
				"model", model,
				"provider", provider.Name,
			)
			c.TrackUserEvent(user, actionLabel+"_completed", posthog.NewProperties().
				Set("app_id", project.ID.String()).
				Set("duration_seconds", genDuration).
				Set("total_tokens", total).
				Set("prompt_tokens", prompt).
				Set("completion_tokens", completion).
				Set("llm_call_count", callCount).
				Set("credits_used", credits).
				Set("model", model).
				Set("has_images", len(images) > 0).
				Set("is_generation", isInitialGeneration))
		}
	}()

	// --- Keepalive goroutine ---
	keepaliveDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-keepaliveDone:
				return
			case <-ticker.C:
				sendEvent("keepalive", map[string]any{"ts": time.Now().Unix()})
			}
		}
	}()
	defer close(keepaliveDone)

	// --- Build fileMap ---
	fileMap := make(map[string]string)

	if isInitialGeneration {
		// Clean slate: delete existing files
		c.DB.DeleteAppGeneratedFilesByProjectPlatform(project.ID.String(), "nextjs")

		// Inject template files (package.json, tsconfig, postcss, next.config)
		for _, tp := range constants.NEXTJS_TEMPLATE_FILE_PATHS {
			// For globals.css, don't inject a pre-built theme — let the LLM write it
			if tp == "app/globals.css" {
				continue
			}
			content := getTemplateContent(tp, nil, nil, project.ProjectType, nil)
			if content == "" {
				continue
			}
			c.injectTemplateFile(project, tp, content, fileMap, sendEvent)
		}

		// Inject UI component templates (lib/utils, components/ui/*)
		for _, tp := range constants.NEXTJS_UI_TEMPLATE_FILE_PATHS {
			content := getUITemplateContent(tp)
			if content != "" {
				c.injectTemplateFile(project, tp, content, fileMap, sendEvent)
			}
		}

		// Inject ecommerce templates if applicable
		if project.ProjectType == "ecommerce" {
			for _, tp := range constants.NEXTJS_ECOMMERCE_TEMPLATE_FILE_PATHS {
				content := getEcommerceTemplateContent(tp)
				if content != "" {
					c.injectTemplateFile(project, tp, content, fileMap, sendEvent)
				}
			}
		}

		// Generate .env.local so env vars are available
		envVars := c.CollectAllEnvVars(project)
		if dotEnv := BuildDotEnvLocal(envVars); dotEnv != "" {
			c.injectTemplateFile(project, ".env.local", dotEnv, fileMap, sendEvent)
		}

		// project_created event is sent by streamSingleShotGeneration
		// (with planned_files when structure planning succeeds)
	} else {
		// Load existing files for chat editing
		files, err := c.DB.GetAppGeneratedFilesByProjectID(project.ID.String(), "nextjs")
		if err != nil {
			sendError("database", "failed to load project files")
			return
		}
		for _, f := range files {
			fileMap[f.FilePath] = f.Content
		}
	}

	// --- Build conversation ---
	conv, err := c.DB.GetOrCreateAppConversation(&models.AppConversation{
		AppProjectID: project.ID,
		Messages:     []byte("[]"),
	})
	if err != nil {
		sendError("database", "failed to create conversation")
		return
	}

	var messages []models.AppConversationMessage
	rawMsg := string(conv.Messages)
	if rawMsg == "" || rawMsg == "null" || rawMsg == `"null"` || (len(rawMsg) > 0 && rawMsg[0] != '[') {
		// nil, JSON null, double-encoded "null", or non-array JSONB — treat as empty
		if len(rawMsg) > 0 && rawMsg[0] != '[' && rawMsg != "null" && rawMsg != `"null"` {
			slog.Warn("conversation messages corrupted (not a JSON array), starting fresh",
				"conv_id", conv.ID, "raw_length", len(conv.Messages), "first_byte", rawMsg[0])
		}
		messages = []models.AppConversationMessage{}
	} else if err := json.Unmarshal(conv.Messages, &messages); err != nil {
		slog.Warn("conversation messages corrupted, starting fresh",
			"error", err, "conv_id", conv.ID, "raw_length", len(conv.Messages))
		messages = []models.AppConversationMessage{}
	}

	// Convert base64 images to data URIs
	var imageURLs []string
	for _, img := range images {
		if img == "" {
			continue
		}
		if !strings.HasPrefix(img, "data:") {
			img = "data:image/png;base64," + img
		}
		imageURLs = append(imageURLs, img)
	}

	// Append user message
	messages = append(messages, models.AppConversationMessage{
		ID:        uuid.New().String(),
		Role:      "user",
		Content:   message,
		Timestamp: time.Now().Unix(),
		ImageURLs: imageURLs,
	})
	if msgJSON, err := json.Marshal(messages); err != nil {
		slog.Error("failed to marshal messages after user message", "error", err, "conv_id", conv.ID)
	} else {
		conv.Messages = msgJSON
		if _, err := c.DB.UpdateAppConversation(conv); err != nil {
			slog.Error("failed to save user message", "error", err, "conv_id", conv.ID)
		}
	}

	// --- Build LLM messages ---
	llmMessages := []utils.Message{
		{Role: "system", Content: buildSystemPrompt(project)},
	}

	// System Block 2: Project context
	projectContext := c.buildProjectContext(project, fileMap, isInitialGeneration)
	llmMessages = append(llmMessages, utils.Message{Role: "system", Content: projectContext})

	// Add conversation history
	if isInitialGeneration {
		// For initial generation, just add the user prompt
		llmMessages = append(llmMessages, utils.Message{Role: "user", Content: message})
		if len(imageURLs) > 0 {
			// Append image context
			lastIdx := len(llmMessages) - 1
			llmMessages[lastIdx].Content += fmt.Sprintf("\n\n[User attached %d image(s) for visual reference — recreate the design shown]", len(imageURLs))
		}
	} else {
		// For chat: inject preview build errors if available
		if c.PreviewService != nil {
			if logs, err := c.PreviewService.GetLogs(project.ID.String()); err == nil && logs != "" {
				if buildErrors := parseNextJSErrors(logs); len(buildErrors) > 0 {
					projectContext += fmt.Sprintf("\n\n⚠ Preview build errors detected:\n%s\n\nFix these errors if the user's request touches affected files, or if they ask about build issues.",
						strings.Join(buildErrors, "\n"))
					// Re-set the system block with updated context
					llmMessages[1] = utils.Message{Role: "system", Content: projectContext}
				}
			}
		}

		// Add context + conversation history with summarization
		llmMessages = append(llmMessages, utils.Message{Role: "user", Content: "I understand the current project state. What would you like me to do?"})

		const keepRecent = 10
		const summarizeThreshold = 15

		if len(messages) > summarizeThreshold {
			oldMessages := messages[:len(messages)-keepRecent]
			// Re-summarize when no summary exists or when old messages have grown
			// significantly since last summary (every 10 new messages beyond threshold)
			needsSummary := conv.Summary == "" || (len(oldMessages) > summarizeThreshold && len(oldMessages)%10 < 2)
			if needsSummary && len(oldMessages) > 0 {
				conv.Summary = c.summarizeConversation(ctx, oldMessages, provider, conv.Summary)
				if msgJSON, err := json.Marshal(messages); err != nil {
					slog.Error("failed to marshal messages after summarization", "error", err, "conv_id", conv.ID)
				} else {
					conv.Messages = msgJSON
					if _, err := c.DB.UpdateAppConversation(conv); err != nil {
						slog.Error("failed to save conversation after summarization", "error", err, "conv_id", conv.ID)
					}
				}
			}
			if conv.Summary != "" {
				llmMessages = append(llmMessages, utils.Message{
					Role:    "user",
					Content: fmt.Sprintf("[Earlier conversation summary]\n%s", conv.Summary),
				})
				llmMessages = append(llmMessages, utils.Message{
					Role:    "assistant",
					Content: "I understand the earlier context. Let me continue from where we left off.",
				})
			}
			for _, msg := range messages[len(messages)-keepRecent:] {
				content := msg.Content
				if msg.Role == "user" && len(msg.ImageURLs) > 0 {
					content += fmt.Sprintf("\n\n[User attached %d image(s)]", len(msg.ImageURLs))
				}
				llmMessages = append(llmMessages, utils.Message{Role: msg.Role, Content: content})
			}
		} else {
			for _, msg := range messages {
				content := msg.Content
				if msg.Role == "user" && len(msg.ImageURLs) > 0 {
					content += fmt.Sprintf("\n\n[User attached %d image(s)]", len(msg.ImageURLs))
				}
				llmMessages = append(llmMessages, utils.Message{Role: msg.Role, Content: content})
			}
		}
	}

	if ctx.Err() != nil {
		return
	}

	// --- Create auto-snapshot before changes ---
	preSnapshot, snapErr := c.CreateAppSnapshot(project, "Auto-save before changes", "auto")
	if snapErr != nil {
		slog.Warn("failed to create pre-change snapshot, rollback will not be available",
			"error", snapErr, "project_id", project.ID)
	}
	var preSnapshotID string
	if preSnapshot != nil {
		preSnapshotID = preSnapshot.ID.String()
		if err := c.DB.DeleteOldAutoSnapshots(project.ID.String(), 20); err != nil {
			slog.Warn("failed to delete old auto snapshots", "error", err, "project_id", project.ID)
		}
	}

	// --- Clarification questions (initial generation only) ---
	if isInitialGeneration && jobID != "" {
		questions, err := c.GenerateClarificationQuestions(ctx, message, project.ProjectType)
		if err == nil && len(questions.Questions) >= 2 {
			result, answered := c.waitForClarification(ctx, jobID, questions, sendEvent)
			if answered {
				sendEvent("clarification_received", map[string]any{
					"message": "Answers received, starting generation...",
				})
			}
			message = buildEnrichedPrompt(message, result)
		}
	}

	// --- Generation: branch between single-shot (initial) and tool-calling (chat) ---
	var toolAppliedOps []appBuilderOperation
	rollbackSnapshotID = preSnapshotID
	rollbackOps = &toolAppliedOps

	var responseText string
	var thinkingBuf strings.Builder
	var accToolCalls []models.AppChatToolCall
	thinkingStart := time.Time{}

	if isInitialGeneration && project.ProjectType == "ecommerce" {
		// === E-commerce two-phase path: setup tools → single-shot file gen ===
		ecomText, ecomOps, ecomErr := c.streamEcommerceGeneration(
			ctx, project, user, model, provider, fileMap, sendEvent, message, imageURLs,
		)
		if ecomErr != nil {
			slog.Error("E-commerce generation failed", "error", ecomErr, "project_id", project.ID)

			// Auto-rollback on failure
			if preSnapshotID != "" && len(ecomOps) > 0 {
				slog.Info("Auto-rollback: restoring pre-snapshot after failed e-commerce generation",
					"project_id", project.ID,
					"snapshot_id", preSnapshotID,
					"partial_ops", len(ecomOps),
				)
				if _, rollbackErr := c.RestoreAppSnapshot(project, preSnapshotID); rollbackErr != nil {
					slog.Error("Auto-rollback failed", "error", rollbackErr, "project_id", project.ID)
				} else {
					sendEvent("rollback", map[string]any{
						"snapshot_id": preSnapshotID,
						"reason":      "generation_failed",
						"rolled_back": len(ecomOps),
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
		responseText = ecomText
		toolAppliedOps = ecomOps
	} else if isInitialGeneration {
		// === Single-shot path: one LLM call, no tool loop (non-ecommerce) ===
		singleShotText, singleShotOps, singleShotErr := c.streamSingleShotGeneration(
			ctx, project, user, model, provider, fileMap, sendEvent, message, imageURLs,
		)
		if singleShotErr != nil {
			slog.Error("Single-shot generation failed", "error", singleShotErr, "project_id", project.ID)

			// Auto-rollback on failure
			if preSnapshotID != "" && len(singleShotOps) > 0 {
				slog.Info("Auto-rollback: restoring pre-snapshot after failed single-shot generation",
					"project_id", project.ID,
					"snapshot_id", preSnapshotID,
					"partial_ops", len(singleShotOps),
				)
				if _, rollbackErr := c.RestoreAppSnapshot(project, preSnapshotID); rollbackErr != nil {
					slog.Error("Auto-rollback failed", "error", rollbackErr, "project_id", project.ID)
				} else {
					sendEvent("rollback", map[string]any{
						"snapshot_id": preSnapshotID,
						"reason":      "generation_failed",
						"rolled_back": len(singleShotOps),
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
		responseText = singleShotText
		toolAppliedOps = singleShotOps
	} else {
		// === Tool-calling path: chat edits ===

		toolCallbacks := &builderToolCallbacks{
			sendEvent:    sendEvent,
			appliedOps:   &toolAppliedOps,
			pendingFiles: make(map[string]string),
		}
		// Ensure any pending files are flushed to preview even on error/cancellation
		defer c.flushPendingPreview(project, toolCallbacks)

		// Reasoning config
		reasoning := c.reasoningConfig()
		if reasoningEffort != "" && reasoningEffort != "none" {
			reasoning = &utils.ReasoningConfig{Effort: reasoningEffort}
		} else if reasoningEffort == "none" {
			reasoning = nil
		}

		// Build tools list
		localExecutor := c.buildBuilderToolExecutor(ctx, project, user, fileMap, toolCallbacks)
		executor := c.compositeToolExecutor(localExecutor)
		tools := append([]utils.Tool{}, appBuilderTools...)
		tools = append(tools, c.mcpTools()...)
		if wsTool := provider.WebSearchTool(); wsTool != nil {
			tools = append(tools, *wsTool)
		}

		// Tool call config — trim file content from history to prevent token bloat.
		trimArgs := func(toolName, args string) string {
			switch toolName {
			case "write_file", "write_files":
				var single struct {
					FilePath string `json:"file_path"`
					Content  string `json:"content"`
				}
				if err := json.Unmarshal([]byte(args), &single); err == nil && single.FilePath != "" {
					return fmt.Sprintf(`{"file_path":%q,"content_written":"%d chars saved to disk"}`, single.FilePath, len(single.Content))
				}
				var batch struct {
					Files []struct {
						FilePath string `json:"file_path"`
						Content  string `json:"content"`
					} `json:"files"`
				}
				if err := json.Unmarshal([]byte(args), &batch); err == nil && len(batch.Files) > 0 {
					summaries := make([]string, len(batch.Files))
					for i, f := range batch.Files {
						summaries[i] = fmt.Sprintf(`{"file_path":%q,"content_written":"%d chars saved to disk"}`, f.FilePath, len(f.Content))
					}
					return fmt.Sprintf(`{"files":[%s]}`, strings.Join(summaries, ","))
				}
				if len(args) > 200 {
					return fmt.Sprintf(`{"error":"malformed write_files arguments truncated","original_length":%d}`, len(args))
				}
				return args
			case "edit_file":
				var edit struct {
					FilePath  string `json:"file_path"`
					OldString string `json:"old_string"`
					NewString string `json:"new_string"`
				}
				if err := json.Unmarshal([]byte(args), &edit); err == nil {
					return fmt.Sprintf(`{"file_path":%q,"edit_applied":"replaced %d chars with %d chars"}`,
						edit.FilePath, len(edit.OldString), len(edit.NewString))
				}
				return args
			default:
				return args
			}
		}
		trimResults := func(toolName, result string) string {
			switch toolName {
			case "read_file":
				lines := strings.Count(result, "\n") + 1
				if len(result) > 500 {
					return fmt.Sprintf("[file read: %d chars, %d lines]", len(result), lines)
				}
				return result
			default:
				return result
			}
		}

		writtenFiles := make(map[string]bool)
		toolConfig := utils.ToolCallConfig{
			MaxRounds:    25,
			NudgeAfter:   10,
			StripAfter:   18,
			MaxTokens:    16384,
			NudgeMessage: "You have made many tool calls. Focus on completing the user's request. Do not re-write files that are already written — the 'content_written' summaries confirm they were saved successfully.",
			TrimArgs:     trimArgs,
			TrimResults:  trimResults,
			InterceptToolCall: func(toolName, toolArgs string) (string, bool) {
				switch toolName {
				case "write_file":
					var args struct {
						FilePath string `json:"file_path"`
					}
					if err := json.Unmarshal([]byte(toolArgs), &args); err != nil || args.FilePath == "" {
						return "", false
					}
					if writtenFiles[args.FilePath] {
						return fmt.Sprintf("ALREADY WRITTEN: %s was already written in this session with complete code. Do NOT re-write unless the user specifically requests changes. Continue with other work.", args.FilePath), true
					}
					writtenFiles[args.FilePath] = true
				case "write_files":
					var batch struct {
						Files []struct {
							FilePath string `json:"file_path"`
							Content  string `json:"content"`
						} `json:"files"`
					}
					if err := json.Unmarshal([]byte(toolArgs), &batch); err == nil && len(batch.Files) > 0 {
						type batchFile struct {
							FilePath string `json:"file_path"`
							Content  string `json:"content"`
						}
						var newFiles []batchFile
						var skippedPaths []string

						for _, f := range batch.Files {
							if writtenFiles[f.FilePath] {
								skippedPaths = append(skippedPaths, f.FilePath)
								continue
							}
							newFiles = append(newFiles, batchFile{FilePath: f.FilePath, Content: f.Content})
							writtenFiles[f.FilePath] = true
						}

						if len(skippedPaths) > 0 {
							if len(newFiles) == 0 {
								return fmt.Sprintf("ALREADY WRITTEN: [%s] were already written in this session with complete code. Do NOT re-write unless the user specifically requests changes. Continue with other work.", strings.Join(skippedPaths, ", ")), true
							}
							modifiedBatch, _ := json.Marshal(map[string]any{"files": newFiles})
							slog.Info("Stripped already-written files from write_files batch (chat)",
								"skipped", skippedPaths, "remaining", len(newFiles))
							return string(modifiedBatch), false
						}
					}
				}
				return "", false
			},
			AfterToolRound: func(allIntercepted bool) string {
				c.flushPendingPreview(project, toolCallbacks)
				if allIntercepted {
					return ""
				}
				if len(toolAppliedOps) == 0 {
					return ""
				}
				var written []string
				for _, op := range toolAppliedOps {
					if op.Action == "create" || op.Action == "modify" {
						written = append(written, op.Path)
					}
				}
				if len(written) == 0 {
					return ""
				}
				return fmt.Sprintf("[SYSTEM: %d files written to disk. The 'content_written' summaries in tool args above confirm successful writes — do NOT re-write these files unless the user asks for changes to them.]\nWritten: %s",
					len(written), strings.Join(written, ", "))
			},
			OnToolCallStart: func(toolName string) {
				sendEvent("tool_start", map[string]any{"tool": toolName})
			},
		}

		slog.Info("Calling LLM for build",
			"model", model,
			"project_id", project.ID,
			"is_generation", isInitialGeneration,
			"message_count", len(llmMessages),
			"tool_count", len(tools),
		)

		// --- Call LLM with tools ---
		var err error
		responseText, err = utils.StreamWithTools(
			ctx,
			model,
			llmMessages,
			tools,
			provider,
			nil, // no forced JSON format
			reasoning,
			executor,
			func(toolName, args string) {
				accToolCalls = append(accToolCalls, models.AppChatToolCall{Tool: toolName, Arguments: args})
				sendEvent("tool_call", responses.ToolCallEvent{
					Tool:      toolName,
					Arguments: args,
				})
			},
			func(chunk string) {
				chunk = ansiRegex.ReplaceAllString(chunk, "")
				if chunk != "" {
					sendEvent("chat_text", responses.ChatTextEvent{Chunk: chunk})
				}
			},
			func(chunk string) {
				if thinkingStart.IsZero() {
					thinkingStart = time.Now()
				}
				thinkingBuf.WriteString(chunk)
				sendEvent("chat_thinking", responses.ChatThinkingEvent{Chunk: chunk})
			},
			toolConfig,
		)
		if err != nil {
			slog.Error("LLM build call failed", "error", err, "project_id", project.ID)

			// Auto-rollback: if files were partially written and we have a pre-snapshot, restore it
			if preSnapshotID != "" && len(toolAppliedOps) > 0 {
				slog.Info("Auto-rollback: restoring pre-snapshot after failed generation",
					"project_id", project.ID,
					"snapshot_id", preSnapshotID,
					"partial_ops", len(toolAppliedOps),
				)
				if _, rollbackErr := c.RestoreAppSnapshot(project, preSnapshotID); rollbackErr != nil {
					slog.Error("Auto-rollback failed", "error", rollbackErr, "project_id", project.ID)
				} else {
					sendEvent("rollback", map[string]any{
						"snapshot_id": preSnapshotID,
						"reason":      "generation_failed",
						"rolled_back": len(toolAppliedOps),
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
	}

	// Record conversation tracing
	tracker.TraceInput = message
	tracker.TraceOutput = responseText

	// --- Post-processing: deterministic auto-fixes ---
	filesChanged := len(toolAppliedOps)
	var appliedOps []appBuilderOperation
	appliedOps = append(appliedOps, toolAppliedOps...)

	if filesChanged > 0 {
		c.runDeterministicAutoFixes(project, fileMap, sendEvent, &appliedOps, &filesChanged)

		// Augment package.json with discovered dependencies
		if pkgContent, ok := fileMap["package.json"]; ok {
			augmented := augmentPackageJSON(pkgContent, fileMap)
			if augmented != pkgContent {
				dbFile, err := c.DB.GetAppGeneratedFileByPath(project.ID.String(), "nextjs", "package.json")
				if err == nil && dbFile != nil {
					dbFile.Content = augmented
					c.DB.UpdateAppGeneratedFile(dbFile)
					fileMap["package.json"] = augmented
					slog.Info("Augmented package.json with discovered deps", "project_id", project.ID)
				}
			}
		}

		// Sync to preview container (synchronous — validation depends on this)
		var deletedPaths []string
		if c.PreviewService != nil {
			for _, op := range appliedOps {
				if op.Action == "delete" {
					deletedPaths = append(deletedPaths, op.Path)
				}
			}
			c.SyncPreviewAll(project, deletedPaths...)
		}

		// Build validation: check logs, fix errors
		// Create a tool executor for validation fix passes (used by both paths)
		validationExecutor := c.buildBuilderToolExecutor(ctx, project, user, fileMap, nil)
		c.validateWithPreviewLogs(ctx, project, fileMap, provider,
			validationExecutor, sendEvent, &appliedOps, &filesChanged, deletedPaths)
	}

	// --- Detect missing env vars ---
	var missingVars []RequiredEnvVar
	if filesChanged > 0 {
		codeKeys := detectEnvVarsFromCode(fileMap)
		var storedRequired []RequiredEnvVar
		if raw, ok := project.Settings["required_env_vars"]; ok {
			if b, err := json.Marshal(raw); err != nil {
				slog.Warn("failed to marshal required_env_vars setting", "error", err, "project_id", project.ID)
			} else if err := json.Unmarshal(b, &storedRequired); err != nil {
				slog.Warn("failed to unmarshal required_env_vars setting", "error", err, "project_id", project.ID)
			}
		}
		missingVars = c.computeMissingEnvVars(project, storedRequired, codeKeys)
	}

	// --- Save assistant message to conversation ---
	assistantContent := strings.TrimSpace(responseText)

	var fileOps []models.AppChatFileOp
	for _, op := range appliedOps {
		fileOps = append(fileOps, models.AppChatFileOp{
			Action:   op.Action,
			FilePath: op.Path,
			FileType: op.Type,
		})
	}

	var thinkingDurationMs int
	if !thinkingStart.IsZero() {
		thinkingDurationMs = int(time.Since(thinkingStart).Milliseconds())
	}
	assistantMsg := models.AppConversationMessage{
		ID:                 uuid.New().String(),
		Role:               "assistant",
		Content:            assistantContent,
		Timestamp:          time.Now().Unix(),
		FileOperations:     fileOps,
		SnapshotID:         preSnapshotID,
		ThinkingText:       thinkingBuf.String(),
		ThinkingDurationMs: thinkingDurationMs,
		ToolCalls:          accToolCalls,
	}
	messages = append(messages, assistantMsg)
	if msgJSON, err := json.Marshal(messages); err != nil {
		slog.Error("failed to marshal messages after assistant message", "error", err, "conv_id", conv.ID)
	} else {
		conv.Messages = msgJSON
		if _, err := c.DB.UpdateAppConversation(conv); err != nil {
			slog.Error("failed to save assistant message", "error", err, "conv_id", conv.ID)
		}
	}

	// --- Update project status for initial generation ---
	if isInitialGeneration && filesChanged > 0 {
		project.Status = "ready"
		if updated, err := c.DB.UpdateAppProject(project); err != nil {
			slog.Error("failed to update project status to ready", "error", err, "project_id", project.ID)
		} else {
			project = updated
		}
	}

	// --- Send done event ---
	if isInitialGeneration {
		sendEvent("done", map[string]any{
			"app_project_id":  project.ID.String(),
			"files_generated": filesChanged,
			"missing_env_vars": missingVars,
		})
	} else {
		sendEvent("chat_complete", map[string]any{
			"project_id":      project.ID,
			"summary":         strings.TrimSpace(responseText),
			"files_changed":   filesChanged,
			"file_operations": fileOps,
			"snapshot_id":     preSnapshotID,
			"missing_env_vars": missingVars,
		})
	}
}

// --- Helper: resolve model for fix/utility passes ---

// fixModel returns the model to use for validation fix passes and conversation
// summarization. Uses WEBSITE_BUILDER_FIX_MODEL secret if set, otherwise falls
// back to the provider's fast (cheap) model.
func (c *Controller) fixModel(provider *utils.LLMProvider) string {
	if m := c.SecretManager.GetSecret("WEBSITE_BUILDER_FIX_MODEL"); m != "" {
		return m
	}
	return provider.FastModel()
}

// --- Helper: flush pending files to preview container ---

func (c *Controller) flushPendingPreview(project *models.AppProject, cbs *builderToolCallbacks) {
	if c.PreviewService == nil || cbs == nil {
		return
	}

	cbs.mu.Lock()
	pending := cbs.pendingFiles
	deletes := cbs.pendingDeletes
	cbs.pendingFiles = make(map[string]string)
	cbs.pendingDeletes = nil
	cbs.mu.Unlock()

	if len(pending) > 0 || len(deletes) > 0 {
		go func() {
			if err := c.PreviewService.UpdateFiles(project.ID.String(), pending, deletes); err != nil {
				slog.Warn("incremental preview sync failed", "error", err, "project_id", project.ID)
			}
		}()
	}
}

// --- Helper: inject a template file into DB and fileMap ---

func (c *Controller) injectTemplateFile(project *models.AppProject, tp string, content string, fileMap map[string]string, sendEvent func(string, any) bool) {
	fileType := "json"
	switch {
	case strings.HasSuffix(tp, ".ts"):
		fileType = "ts"
	case strings.HasSuffix(tp, ".tsx"):
		fileType = "tsx"
	case strings.HasSuffix(tp, ".mjs"):
		fileType = "mjs"
	case strings.HasSuffix(tp, ".css"):
		fileType = "css"
	case tp == ".env.local":
		fileType = "env"
	}

	file := models.AppGeneratedFile{
		AppProjectID: project.ID,
		Platform:     "nextjs",
		FilePath:     tp,
		Content:      content,
		FileType:     fileType,
		IsGenerated:  false, // template-managed
	}
	createdFile, err := c.DB.UpsertAppGeneratedFile(&file)
	if err != nil {
		slog.Error("failed to save template file", "error", err, "path", tp)
		return
	}
	fileMap[tp] = content
	sendEvent("file_complete", responses.FileCompleteEvent{
		Action:   "create",
		FilePath: tp,
		FileID:   createdFile.ID,
		FileType: fileType,
	})
}

// --- Helper: save a generated file to DB, fileMap, and SSE ---

// saveGeneratedFile persists a generated file to the database, updates fileMap,
// emits a file_complete SSE event, and tracks the operation. Used by both the
// single-shot parser and the tool executor.
func (c *Controller) saveGeneratedFile(
	project *models.AppProject,
	path string,
	content string,
	fileType string,
	fileMap map[string]string,
	sendEvent func(string, any) bool,
	ops *[]appBuilderOperation,
) {
	// Guard: never let the LLM overwrite template-managed files
	if isProtectedTemplateFile(path) {
		slog.Warn("saveGeneratedFile: skipping protected template file", "path", path)
		return
	}

	// Guard: e-commerce projects must not have API routes (except auth) or drizzle files.
	// All store data comes from the SDK, not custom API routes.
	if project.ProjectType == "ecommerce" {
		if strings.HasPrefix(path, "app/api/") && !strings.HasPrefix(path, "app/api/auth/") {
			slog.Warn("saveGeneratedFile: blocking API route in e-commerce project", "path", path)
			return
		}
		if path == "lib/db.ts" || path == "lib/schema.ts" {
			slog.Warn("saveGeneratedFile: blocking drizzle file in e-commerce project", "path", path)
			return
		}
	}

	existingFile, err := c.DB.GetAppGeneratedFileByPath(project.ID.String(), "nextjs", path)
	action := "create"
	var fileID uuid.UUID
	if err == nil && existingFile != nil {
		if normalizeContent(content) == normalizeContent(existingFile.Content) {
			// No changes — skip
			return
		}
		existingFile.Content = content
		existingFile.UpdatedAt = time.Now()
		if _, err := c.DB.UpdateAppGeneratedFile(existingFile); err != nil {
			slog.Error("saveGeneratedFile: failed to update", "path", path, "error", err)
			return
		}
		action = "modify"
		fileID = existingFile.ID
	} else {
		newFile := models.AppGeneratedFile{
			AppProjectID: project.ID,
			Platform:     "nextjs",
			FilePath:     path,
			Content:      content,
			FileType:     fileType,
			IsGenerated:  true,
		}
		createdFile, err := c.DB.CreateAppGeneratedFile(&newFile)
		if err != nil {
			slog.Error("saveGeneratedFile: failed to create", "path", path, "error", err)
			return
		}
		fileID = createdFile.ID
	}
	fileMap[path] = content
	sendEvent("file_complete", responses.FileCompleteEvent{
		Action: action, FilePath: path,
		FileID: fileID, FileType: fileType, Content: content,
	})
	*ops = append(*ops, appBuilderOperation{
		Action: action, Path: path, Type: fileType,
	})
}

// --- Helper: build modular system prompt ---

// buildSystemPrompt assembles the system prompt for the tool-calling path,
// only including e-commerce and auth rules when the project actually uses those features.
func buildSystemPrompt(project *models.AppProject) string {
	var prompt string
	if project.ProjectType == "ecommerce" {
		prompt = constants.NEXTJS_ECOMMERCE_COMMON_RULES + constants.NEXTJS_BUILDER_TOOL_INSTRUCTIONS
	} else {
		prompt = constants.NEXTJS_BUILDER_SYSTEM_PROMPT + constants.NEXTJS_BUILDER_DATABASE_RULES
	}

	if settings := project.Settings; settings != nil {
		if needsAuth, ok := settings["needs_auth"].(bool); ok && needsAuth {
			prompt += constants.NEXTJS_BUILDER_AUTH_RULES
		}
	}

	return prompt
}

// buildSingleShotSystemPrompt assembles the system prompt for single-shot initial
// generation using <file> block output format instead of tool calls.
func buildSingleShotSystemPrompt(project *models.AppProject) string {
	var prompt string
	if project.ProjectType == "ecommerce" {
		prompt = constants.NEXTJS_ECOMMERCE_COMMON_RULES + constants.NEXTJS_BUILDER_SINGLESHOT_INSTRUCTIONS
	} else {
		prompt = constants.NEXTJS_BUILDER_COMMON_RULES + constants.NEXTJS_BUILDER_SINGLESHOT_INSTRUCTIONS + constants.NEXTJS_BUILDER_DATABASE_RULES
	}

	if settings := project.Settings; settings != nil {
		if needsAuth, ok := settings["needs_auth"].(bool); ok && needsAuth {
			prompt += constants.NEXTJS_BUILDER_AUTH_RULES
		}
	}

	return prompt
}

// --- Helper: extract file summary for project context ---

// exportPattern matches common TypeScript/JavaScript export patterns.
var exportPattern = regexp.MustCompile(`(?m)^export\s+(?:default\s+)?(?:function|const|class|type|interface|enum)\s+(\w+)`)

// extractFileSummary produces a brief structural summary of a file's exports.
// Returns something like "exports: Button, buttonVariants" or "default: HeroSection".
func extractFileSummary(content string) string {
	var names []string
	hasDefault := false

	matches := exportPattern.FindAllStringSubmatch(content, 20)
	for _, m := range matches {
		if len(m) > 1 {
			names = append(names, m[1])
		}
	}
	if strings.Contains(content, "export default") {
		hasDefault = true
	}
	if strings.HasPrefix(strings.TrimSpace(content), `"use client"`) || strings.HasPrefix(strings.TrimSpace(content), `'use client'`) {
		if len(names) > 0 {
			return fmt.Sprintf("\"use client\"; exports: %s", strings.Join(names, ", "))
		}
		if hasDefault {
			return "\"use client\"; default export"
		}
		return "\"use client\""
	}
	if len(names) > 0 {
		return fmt.Sprintf("exports: %s", strings.Join(names, ", "))
	}
	if hasDefault {
		return "default export"
	}
	return ""
}

// --- Helper: build project context (system block 2) ---

func (c *Controller) buildProjectContext(project *models.AppProject, fileMap map[string]string, isInitialGeneration bool) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("App: %s (slug: %s)\n", project.Name, project.Slug))
	sb.WriteString(fmt.Sprintf("Project type: %s\n", project.ProjectType))

	// Theme
	var theme map[string]any
	if len(project.Theme) > 0 && project.Theme[0] == '{' {
		if err := json.Unmarshal(project.Theme, &theme); err != nil {
			slog.Warn("failed to unmarshal project theme", "error", err, "project_id", project.ID)
		}
	} else if len(project.Theme) > 0 {
		// Theme is not a JSON object (possibly a string or corrupted) — try to unwrap
		var raw string
		if json.Unmarshal(project.Theme, &raw) == nil && len(raw) > 0 && raw[0] == '{' {
			// Double-encoded JSON string — unwrap and parse
			if err := json.Unmarshal([]byte(raw), &theme); err != nil {
				slog.Warn("failed to unmarshal double-encoded project theme", "error", err, "project_id", project.ID)
			}
		}
	}
	if len(theme) > 0 {
		sb.WriteString(fmt.Sprintf("Theme: %v\n", theme))
	}

	// File tree with structural summaries
	if len(fileMap) > 0 {
		sb.WriteString("\nCurrent project files:\n")
		paths := make([]string, 0, len(fileMap))
		for p := range fileMap {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		brokenCount := 0
		for _, fp := range paths {
			content := fileMap[fp]
			if strings.Contains(content, fallbackSentinel) || strings.Contains(content, "Generation failed") {
				sb.WriteString(fmt.Sprintf("- %s (%d chars) [BROKEN]\n", fp, len(content)))
				brokenCount++
			} else if len(content) <= 500 {
				// Inline small files so the LLM doesn't need read_file
				sb.WriteString(fmt.Sprintf("- %s (%d chars) [inline]:\n```\n%s\n```\n", fp, len(content), content))
			} else {
				// Extract exports summary for larger files
				summary := extractFileSummary(content)
				if summary != "" {
					sb.WriteString(fmt.Sprintf("- %s (%d chars) — %s\n", fp, len(content), summary))
				} else {
					sb.WriteString(fmt.Sprintf("- %s (%d chars)\n", fp, len(content)))
				}
			}
		}
		if brokenCount > 0 {
			sb.WriteString(fmt.Sprintf("\nWARNING: %d file(s) marked [BROKEN] need full regeneration.\n", brokenCount))
		}
		sb.WriteString("\nUse read_file to view any file's content before modifying it.")
	} else if isInitialGeneration {
		sb.WriteString("\nNo files yet — you are building this app from scratch.\n")
		sb.WriteString("User prompt: " + project.Prompt + "\n")
	}

	// Inject file plan from structure phase if available (for initial gen regardless of fileMap state,
	// since template files may already be injected before the LLM runs).
	if isInitialGeneration {
		if raw, ok := project.Settings["file_plan"]; ok {
			b, marshalErr := json.Marshal(raw)
			if marshalErr != nil {
				slog.Warn("failed to marshal file_plan setting", "error", marshalErr, "project_id", project.ID)
			}
			var filePlan []struct {
				Path        string `json:"path"`
				Type        string `json:"type"`
				Description string `json:"description"`
			}
			if marshalErr == nil && json.Unmarshal(b, &filePlan) == nil && len(filePlan) > 0 {
				sb.WriteString("\nPlanned file structure (create these files):\n")
				for _, f := range filePlan {
					sb.WriteString(fmt.Sprintf("- %s (%s): %s\n", f.Path, f.Type, f.Description))
				}
				sb.WriteString("\nFollow this plan. You may add extra files if needed, but create all planned files.\n")
			}
		}
	}

	// Database schema context
	if len(project.DataModels) > 0 {
		var dataModels []DataModel
		json.Unmarshal(project.DataModels, &dataModels)
		if len(dataModels) > 0 {
			sb.WriteString("\nDatabase schema (Drizzle ORM via lib/schema.ts):\n")
			for _, dm := range dataModels {
				sb.WriteString(fmt.Sprintf("  Table: %s (", dm.Name))
				var cols []string
				for _, col := range dm.Columns {
					cols = append(cols, fmt.Sprintf("%s:%s", col.Name, col.Type))
				}
				sb.WriteString(strings.Join(cols, ", "))
				sb.WriteString(")\n")
			}
		}
	}

	// Integration context
	appIntegrations, _ := c.DB.GetAppIntegrationsByProjectID(project.ID.String())
	hasPaymentGateway := false
	if len(appIntegrations) > 0 {
		sb.WriteString("\nConnected integrations:\n")
		for _, integ := range appIntegrations {
			config, err := integrations.GetProviderConfig(integ.Provider)
			if err != nil {
				sb.WriteString(fmt.Sprintf("- %s (status: %s)\n", integ.Provider, integ.Status))
				continue
			}
			sb.WriteString(fmt.Sprintf("- %s (category: %s, status: %s)\n", integ.Provider, config.Category, integ.Status))
			var envNames []string
			for _, envName := range config.EnvVarMapping {
				envNames = append(envNames, envName)
			}
			if len(envNames) > 0 {
				sb.WriteString(fmt.Sprintf("  Env vars: %s\n", strings.Join(envNames, ", ")))
			}
			if config.Category == "payment" {
				hasPaymentGateway = true
			}
		}
	}

	// E-commerce store context
	if project.ProjectType == "ecommerce" {
		store, err := c.DB.GetStoreByAppProjectID(project.ID.String())
		if err == nil && store != nil {
			sb.WriteString(fmt.Sprintf("\nE-commerce store: %s (store_id: %s)\n", store.Status, store.ID.String()))
			sb.WriteString(fmt.Sprintf("Storefront API: %s/v2/storefront/%s\n", constants.WEMOB_API_BASE_URL, store.ID.String()))
			sb.WriteString(fmt.Sprintf("Currency: %s, Locale: %s, Tax rate: %.2f%%\n", store.Currency, store.Locale, store.TaxRate))

			products, _ := c.DB.GetProductsByStoreID(store.ID.String())
			sb.WriteString(fmt.Sprintf("Products: %d\n", len(products)))

			categories, _ := c.DB.GetCategoriesByStoreID(store.ID.String())
			if len(categories) > 0 {
				var catNames []string
				for _, cat := range categories {
					catNames = append(catNames, cat.Name)
				}
				sb.WriteString(fmt.Sprintf("Categories: %s\n", strings.Join(catNames, ", ")))
			}

			var paymentProviders []string
			for _, integ := range appIntegrations {
				if integ.Status == "connected" {
					for _, cfg := range constants.IntegrationProviders {
						if cfg.Provider == integ.Provider && cfg.Category == "payment" {
							paymentProviders = append(paymentProviders, cfg.Name)
						}
					}
				}
			}
			if len(paymentProviders) > 0 {
				sb.WriteString(fmt.Sprintf("Payment providers: %s\n", strings.Join(paymentProviders, ", ")))
			}

			sb.WriteString("SDK: lib/wemob-store.ts provides CartProvider, useCart, useProducts, etc. Wrap layout.tsx with <CartProvider>.\n")
		}
		if !hasPaymentGateway {
			sb.WriteString("\nWARNING: No payment gateway connected. Tell user to connect Stripe or Iyzico.\n")
		}
	}

	// Auth context
	if settings := project.Settings; settings != nil {
		if needsAuth, ok := settings["needs_auth"].(bool); ok && needsAuth {
			sb.WriteString("\nAuthentication: enabled\n")
			sb.WriteString("- Server: import { register, login, logout, getSession, requireAuth, getApiSession } from '@/lib/wemob-auth'\n")
			sb.WriteString("- Client: import { AuthProvider, useAuth } from '@/lib/wemob-auth-client'\n")
			sb.WriteString("- Wrap layout with <AuthProvider>. Create app/api/auth/[...action]/route.ts.\n")
		}
	}

	// Custom env vars
	customEnvVars, _ := c.DB.GetAppEnvVarsByProjectID(project.ID.String())
	if len(customEnvVars) > 0 {
		sb.WriteString("\nCustom environment variables:\n")
		for _, ev := range customEnvVars {
			key := ev.Key
			if ev.IsPublic && !strings.HasPrefix(key, "NEXT_PUBLIC_") {
				key = "NEXT_PUBLIC_" + key
			}
			sb.WriteString(fmt.Sprintf("- %s\n", key))
		}
	}

	return sb.String()
}

// --- Helper: run deterministic auto-fixes ---

func (c *Controller) runDeterministicAutoFixes(
	project *models.AppProject,
	fileMap map[string]string,
	sendEvent func(string, any) bool,
	appliedOps *[]appBuilderOperation,
	filesChanged *int,
) {
	// Auto-fix .ts files containing JSX → rename to .tsx
	tsxRenamed, tsxChangedPaths := autoFixTSFilesWithJSX(fileMap)
	if tsxRenamed > 0 {
		slog.Info("Auto-fix: renamed .ts→.tsx", "project_id", project.ID, "count", tsxRenamed)
		for i := 0; i+1 < len(tsxChangedPaths); i += 2 {
			oldPath := tsxChangedPaths[i]
			newPath := tsxChangedPaths[i+1]
			_, fileType := guessFileType(newPath)

			// Preserve file ID by updating in-place instead of delete+create
			if oldFile, err := c.DB.GetAppGeneratedFileByPath(project.ID.String(), "nextjs", oldPath); err == nil {
				oldFile.FilePath = newPath
				oldFile.FileType = fileType
				oldFile.Content = fileMap[newPath]
				oldFile.UpdatedAt = time.Now()
				if _, err := c.DB.UpdateAppGeneratedFile(oldFile); err != nil {
					slog.Error("failed to rename file in DB", "error", err, "old", oldPath, "new", newPath)
				} else {
					sendEvent("file_complete", responses.FileCompleteEvent{
						Action: "modify", FilePath: newPath, FileID: oldFile.ID,
						FileType: fileType, Content: fileMap[newPath],
					})
				}
			} else {
				// Fallback: old file not found, create new
				newFile := models.AppGeneratedFile{
					AppProjectID: project.ID,
					Platform:     "nextjs",
					FilePath:     newPath,
					Content:      fileMap[newPath],
					FileType:     fileType,
					IsGenerated:  true,
				}
				if created, err := c.DB.CreateAppGeneratedFile(&newFile); err == nil {
					sendEvent("file_complete", responses.FileCompleteEvent{
						Action: "create", FilePath: newPath, FileID: created.ID,
						FileType: fileType, Content: fileMap[newPath],
					})
				}
			}
			*filesChanged++
		}
	}

	// Auto-fix metadata in client component
	autoFixedMeta, changedPathsMeta := autoFixMetadataInClientComponent(fileMap)

	// Auto-fix import/export mismatches
	autoFixed, changedPaths := autoFixImportMismatches(fileMap)
	// Auto-fix missing "use client" directives
	autoFixed2, changedPaths2 := autoFixMissingUseClient(fileMap)
	autoFixed += autoFixed2
	changedPaths = append(changedPaths, changedPaths2...)
	autoFixed += autoFixedMeta
	changedPaths = append(changedPaths, changedPathsMeta...)

	// Auto-fix root layout tags
	if fixed, fixedPath := autoFixRootLayoutTags(fileMap); fixed && fixedPath != "" {
		changedPaths = append(changedPaths, fixedPath)
		autoFixed++
	}

	if autoFixed > 0 {
		slog.Info("Deterministic auto-fix applied", "project_id", project.ID, "files_fixed", autoFixed)
		for _, fixedPath := range changedPaths {
			dbFile, err := c.DB.GetAppGeneratedFileByPath(project.ID.String(), "nextjs", fixedPath)
			if err == nil && dbFile != nil {
				dbFile.Content = fileMap[fixedPath]
				dbFile.UpdatedAt = time.Now()
				c.DB.UpdateAppGeneratedFile(dbFile)
				sendEvent("file_complete", responses.FileCompleteEvent{
					Action: "modify", FilePath: fixedPath,
					FileID: dbFile.ID, FileType: dbFile.FileType,
					Content: fileMap[fixedPath],
				})
				*appliedOps = append(*appliedOps, appBuilderOperation{
					Action: "modify", Path: fixedPath, Type: dbFile.FileType,
				})
				*filesChanged++
			} else {
				// New file created by auto-fix (e.g. components/layout-client.tsx)
				_, fileType := guessFileType(fixedPath)
				newFile := models.AppGeneratedFile{
					AppProjectID: project.ID,
					Platform:     "nextjs",
					FilePath:     fixedPath,
					Content:      fileMap[fixedPath],
					FileType:     fileType,
					IsGenerated:  true,
				}
				if created, createErr := c.DB.CreateAppGeneratedFile(&newFile); createErr == nil {
					sendEvent("file_complete", responses.FileCompleteEvent{
						Action: "create", FilePath: fixedPath,
						FileID: created.ID, FileType: fileType,
						Content: fileMap[fixedPath],
					})
					*appliedOps = append(*appliedOps, appBuilderOperation{
						Action: "create", Path: fixedPath, Type: fileType,
					})
					*filesChanged++
				}
			}
		}
	}
}

// --- Build validation loop ---

// nextjsErrorPatterns matches common Next.js build/dev error patterns.
var nextjsErrorPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)Module not found:.*`),
	regexp.MustCompile(`(?i)Type error:.*`),
	regexp.MustCompile(`(?i)SyntaxError:.*`),
	regexp.MustCompile(`(?i)ReferenceError:.*`),
	regexp.MustCompile(`(?i)Error:\s+.+`),
	regexp.MustCompile(`\./[^\s]+:\d+:\d+.*`),
}

// parseNextJSErrors extracts unique error lines from Next.js dev server logs.
func parseNextJSErrors(logs string) []string {
	seen := make(map[string]bool)
	var errors []string

	for _, line := range strings.Split(logs, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		for _, pat := range nextjsErrorPatterns {
			if match := pat.FindString(line); match != "" {
				if !seen[match] {
					seen[match] = true
					errors = append(errors, match)
				}
				break
			}
		}
		if len(errors) >= 10 {
			break
		}
	}
	return errors
}

// validateWithPreviewLogs syncs files to the preview container, waits for Next.js
// to recompile, checks dev server logs for errors, and if found, calls the LLM
// for a focused fix pass. Repeats up to 3 times.
func (c *Controller) validateWithPreviewLogs(
	ctx context.Context,
	project *models.AppProject,
	fileMap map[string]string,
	provider *utils.LLMProvider,
	executor utils.ToolExecutor,
	sendEvent func(string, any) bool,
	appliedOps *[]appBuilderOperation,
	filesChanged *int,
	deletedPaths []string,
) {
	if c.PreviewService == nil {
		return
	}

	const maxValidationRounds = 3
	const pollInterval = 2 * time.Second
	const maxPollWait = 20 * time.Second

	for round := 0; round < maxValidationRounds; round++ {
		if ctx.Err() != nil {
			return
		}

		// 1. Sync all files to preview (include deletes so removed files are cleaned up)
		c.SyncPreviewAll(project, deletedPaths...)

		// 2. Poll logs until compilation markers appear (or timeout)
		var errors []string
		pollDeadline := time.Now().Add(maxPollWait)
		foundMarker := false
		for time.Now().Before(pollDeadline) {
			select {
			case <-ctx.Done():
				return
			case <-time.After(pollInterval):
			}

			logs, err := c.PreviewService.GetLogs(project.ID.String())
			if err != nil || logs == "" {
				continue
			}

			// Check for compilation success markers
			if strings.Contains(logs, "Compiled") || strings.Contains(logs, "compiled client and server") {
				foundMarker = true
			}

			// Check for error markers
			if strings.Contains(logs, "Error:") || strings.Contains(logs, "error TS") || strings.Contains(logs, "Module not found") {
				foundMarker = true
			}

			if foundMarker {
				// One more poll to let logs settle
				select {
				case <-ctx.Done():
					return
				case <-time.After(pollInterval):
				}
				logs, _ = c.PreviewService.GetLogs(project.ID.String())
				if logs != "" {
					errors = parseNextJSErrors(logs)
				}
				break
			}
		}

		// If no marker found by deadline, parse whatever logs are available
		if !foundMarker {
			logs, err := c.PreviewService.GetLogs(project.ID.String())
			if err != nil || logs == "" {
				return
			}
			errors = parseNextJSErrors(logs)
		}

		// 3. No errors — done
		if len(errors) == 0 {
			return // no errors — done
		}

		slog.Info("Build validation found errors",
			"project_id", project.ID,
			"round", round+1,
			"error_count", len(errors),
		)

		// 5. Fire validation SSE event
		sendEvent("validation", responses.ValidationEvent{
			Iteration: round + 1,
			Errors:    errors,
			Fixing:    true,
		})

		// 6. Build fix messages
		fixMessages := []utils.Message{
			{Role: "system", Content: buildSystemPrompt(project)},
			{Role: "system", Content: c.buildProjectContext(project, fileMap, false)},
			{Role: "user", Content: fmt.Sprintf(
				"The preview build has these errors. Fix them:\n\n%s\n\nUse read_file to check the current content of broken files, then fix with edit_file or write_file.",
				strings.Join(errors, "\n"),
			)},
		}

		// 7. Call LLM with limited rounds
		fixConfig := utils.ToolCallConfig{
			MaxRounds:  5,
			NudgeAfter: 4,
			StripAfter: 5,
			MaxTokens:  16384,
		}

		fixModel := c.fixModel(provider)
		_, fixErr := utils.StreamWithTools(ctx, fixModel, fixMessages, appBuilderTools, provider,
			nil, nil, executor,
			func(toolName, args string) { /* silent tool calls */ },
			func(chunk string) { sendEvent("chat_text", responses.ChatTextEvent{Chunk: chunk}) },
			func(chunk string) { /* ignore thinking */ },
			fixConfig,
		)
		if fixErr != nil {
			slog.Error("validation fix pass failed", "error", fixErr, "project_id", project.ID, "round", round+1)
			sendEvent("validation", responses.ValidationEvent{
				Iteration: round + 1,
				Errors:    errors,
				Fixing:    false,
			})
			break
		}

		// Run deterministic auto-fixes again after LLM fixes
		c.runDeterministicAutoFixes(project, fileMap, sendEvent, appliedOps, filesChanged)
	}
}

// --- Tool definitions ---

// filterToolsByName extracts tools from appBuilderTools matching the given names.
func filterToolsByName(names ...string) []utils.Tool {
	wanted := make(map[string]bool, len(names))
	for _, n := range names {
		wanted[n] = true
	}
	var out []utils.Tool
	for _, t := range appBuilderTools {
		if wanted[t.Function.Name] {
			out = append(out, t)
		}
	}
	return out
}

// appBuilderTools defines the tools available to the unified builder LLM.
var appBuilderTools = []utils.Tool{
	{
		Type: "function",
		Function: utils.ToolFunction{
			Name:        "read_file",
			Description: "Read the full content of a project file by its path. Use this to see a file's current implementation before modifying it.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file_path": map[string]any{
						"type":        "string",
						"description": "Relative file path, e.g. 'app/page.tsx' or 'components/header.tsx'",
					},
				},
				"required": []string{"file_path"},
			},
		},
	},
	{
		Type: "function",
		Function: utils.ToolFunction{
			Name:        "list_files",
			Description: "List all files in the project with their types and sizes. Use this to understand the project structure.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{},
			},
		},
	},
	{
		Type: "function",
		Function: utils.ToolFunction{
			Name:        "search_files",
			Description: "Search for files containing a specific text pattern. Returns matching file paths and the matching lines.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Text to search for in file contents (case-insensitive)",
					},
				},
				"required": []string{"query"},
			},
		},
	},
	{
		Type: "function",
		Function: utils.ToolFunction{
			Name:        "write_file",
			Description: "Create a new file or overwrite an existing file with the given content. Use this to create new files or when you need to replace the entire content of a file. For small targeted changes to existing files, prefer edit_file instead.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file_path": map[string]any{
						"type":        "string",
						"description": "Relative file path, e.g. 'app/page.tsx' or 'components/header.tsx'",
					},
					"content": map[string]any{
						"type":        "string",
						"description": "The complete file content to write",
					},
				},
				"required": []string{"file_path", "content"},
			},
		},
	},
	{
		Type: "function",
		Function: utils.ToolFunction{
			Name:        "write_files",
			Description: "Create or overwrite multiple files at once. Use this instead of calling write_file repeatedly when you need to create several files. Much more efficient for initial app generation.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"files": map[string]any{
						"type":        "array",
						"description": "Array of files to write",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"file_path": map[string]any{
									"type":        "string",
									"description": "Relative file path, e.g. 'app/page.tsx'",
								},
								"content": map[string]any{
									"type":        "string",
									"description": "The complete file content to write",
								},
							},
							"required": []string{"file_path", "content"},
						},
					},
				},
				"required": []string{"files"},
			},
		},
	},
	{
		Type: "function",
		Function: utils.ToolFunction{
			Name:        "edit_file",
			Description: "Apply a targeted edit to an existing file by replacing a specific string with new content. You MUST read_file first to see the exact content to replace. The old_string must match exactly (including whitespace and indentation). For replacing the entire file, use write_file instead.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file_path": map[string]any{
						"type":        "string",
						"description": "Relative file path of the file to edit",
					},
					"old_string": map[string]any{
						"type":        "string",
						"description": "The exact string to find and replace. Must be unique within the file.",
					},
					"new_string": map[string]any{
						"type":        "string",
						"description": "The replacement string",
					},
				},
				"required": []string{"file_path", "old_string", "new_string"},
			},
		},
	},
	{
		Type: "function",
		Function: utils.ToolFunction{
			Name:        "delete_file",
			Description: "Delete a file from the project. Use this to remove files that are no longer needed.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file_path": map[string]any{
						"type":        "string",
						"description": "Relative file path of the file to delete",
					},
				},
				"required": []string{"file_path"},
			},
		},
	},
	{
		Type: "function",
		Function: utils.ToolFunction{
			Name:        "setup_ecommerce",
			Description: "Set up e-commerce for this project. Creates a store and configures the project for e-commerce. Call this when the user wants to sell products, create a shop, or add e-commerce functionality.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"store_name": map[string]any{
						"type":        "string",
						"description": "Name for the store, e.g. 'My Shoe Store'",
					},
				},
				"required": []string{"store_name"},
			},
		},
	},
	{
		Type: "function",
		Function: utils.ToolFunction{
			Name:        "manage_store",
			Description: "Create or manage products, categories, and discounts in the e-commerce store. Use this after setup_ecommerce to populate the store with products. Supports batch operations.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"operations": map[string]any{
						"type":        "array",
						"description": "Array of store operations to execute",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"action": map[string]any{
									"type":        "string",
									"enum":        []string{"create_product", "update_product", "delete_product", "bulk_create_products", "create_category", "update_category", "delete_category", "create_discount", "update_discount", "delete_discount", "update_store_settings", "list_products", "list_categories"},
									"description": "The operation to perform",
								},
								"data": map[string]any{
									"type":        "object",
									"description": "Operation data varies by action.",
								},
							},
							"required": []string{"action", "data"},
						},
					},
				},
				"required": []string{"operations"},
			},
		},
	},
	{
		Type: "function",
		Function: utils.ToolFunction{
			Name:        "setup_database",
			Description: "Provision a PostgreSQL database with schema tables. Call this when the app needs persistent data storage. Returns connection details and generates lib/db.ts and lib/schema.ts. Also provisions auth tables if the app needs authentication.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"tables": map[string]any{
						"type":        "array",
						"description": "Array of table definitions",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"name": map[string]any{
									"type":        "string",
									"description": "Table name, e.g. 'posts'",
								},
								"columns": map[string]any{
									"type": "array",
									"items": map[string]any{
										"type": "object",
										"properties": map[string]any{
											"name":        map[string]any{"type": "string"},
											"type":        map[string]any{"type": "string", "description": "text, integer, boolean, timestamp, uuid, jsonb"},
											"primary_key": map[string]any{"type": "boolean"},
											"nullable":    map[string]any{"type": "boolean"},
											"default":     map[string]any{"type": "string"},
											"references":  map[string]any{"type": "string", "description": "e.g. 'categories.id'"},
										},
										"required": []string{"name", "type"},
									},
								},
							},
							"required": []string{"name", "columns"},
						},
					},
					"needs_auth": map[string]any{
						"type":        "boolean",
						"description": "Set to true if the app needs user authentication. This adds a users table and auth infrastructure.",
					},
				},
				"required": []string{"tables"},
			},
		},
	},
}

// --- Tool executor ---

// buildBuilderToolExecutor returns a ToolExecutor for the unified builder.
func (c *Controller) buildBuilderToolExecutor(ctx context.Context, project *models.AppProject, user *models.User, fileMap map[string]string, cbs *builderToolCallbacks) utils.ToolExecutor {
	return func(name string, arguments string) (string, error) {
		// Early exit if context is cancelled — prevents partial writes after timeout
		if ctx.Err() != nil {
			return "", ctx.Err()
		}

		switch name {
		case "read_file":
			var args struct {
				FilePath string `json:"file_path"`
			}
			if err := json.Unmarshal([]byte(arguments), &args); err != nil {
				return fmt.Sprintf("Error: invalid arguments: %s", err.Error()), nil
			}
			cbs.fileMu.RLock()
			content, ok := fileMap[args.FilePath]
			cbs.fileMu.RUnlock()
			if !ok {
				return fmt.Sprintf("Error: file '%s' not found in project", args.FilePath), nil
			}
			return content, nil

		case "list_files":
			cbs.fileMu.RLock()
			var lines []string
			for path, content := range fileMap {
				lines = append(lines, fmt.Sprintf("%s (%d chars)", path, len(content)))
			}
			cbs.fileMu.RUnlock()
			sort.Strings(lines)
			return strings.Join(lines, "\n"), nil

		case "search_files":
			var args struct {
				Query string `json:"query"`
			}
			if err := json.Unmarshal([]byte(arguments), &args); err != nil {
				return fmt.Sprintf("Error: invalid arguments: %s", err.Error()), nil
			}
			query := strings.ToLower(args.Query)
			cbs.fileMu.RLock()
			var results []string
			for path, content := range fileMap {
				lines := strings.Split(content, "\n")
				for i, line := range lines {
					if strings.Contains(strings.ToLower(line), query) {
						results = append(results, fmt.Sprintf("%s:%d: %s", path, i+1, strings.TrimSpace(line)))
					}
				}
			}
			cbs.fileMu.RUnlock()
			if len(results) == 0 {
				return "No matches found.", nil
			}
			if len(results) > 50 {
				results = results[:50]
			}
			return strings.Join(results, "\n"), nil

		case "write_file":
			var args struct {
				FilePath string `json:"file_path"`
				Content  string `json:"content"`
			}
			if err := json.Unmarshal([]byte(arguments), &args); err != nil {
				return fmt.Sprintf("Error: invalid arguments: %s", err.Error()), nil
			}
			if args.FilePath == "" {
				return "Error: file_path is required", nil
			}
			// Block writes to template-managed files
			if isProtectedTemplateFile(args.FilePath) {
				return fmt.Sprintf("Error: %s is template-managed and cannot be written directly. Dependencies are auto-detected from imports.", args.FilePath), nil
			}

			content := utils.StripCodeFences(args.Content)
			_, fileType := guessFileType(args.FilePath)
			if fileType == "tsx" || fileType == "ts" {
				content, _ = stripForbiddenImports(content, args.FilePath)
			}

			existingFile, err := c.DB.GetAppGeneratedFileByPath(project.ID.String(), "nextjs", args.FilePath)
			action := "create"
			var fileID uuid.UUID
			if err == nil && existingFile != nil {
				if normalizeContent(content) == normalizeContent(existingFile.Content) {
					return fmt.Sprintf("File %s is already up to date (no changes needed).", args.FilePath), nil
				}
				existingFile.Content = content
				existingFile.UpdatedAt = time.Now()
				_, err = c.DB.UpdateAppGeneratedFile(existingFile)
				if err != nil {
					return fmt.Sprintf("Error: failed to update file: %s", err.Error()), nil
				}
				action = "modify"
				fileID = existingFile.ID
			} else {
				newFile := models.AppGeneratedFile{
					AppProjectID: project.ID,
					Platform:     "nextjs",
					FilePath:     args.FilePath,
					Content:      content,
					FileType:     fileType,
					IsGenerated:  true,
				}
				createdFile, err := c.DB.CreateAppGeneratedFile(&newFile)
				if err != nil {
					return fmt.Sprintf("Error: failed to create file: %s", err.Error()), nil
				}
				fileID = createdFile.ID
			}
			if cbs != nil {
				cbs.mu.Lock()
				cbs.fileMu.Lock()
				fileMap[args.FilePath] = content
				cbs.fileMu.Unlock()
				cbs.sendEvent("file_complete", responses.FileCompleteEvent{
					Action: action, FilePath: args.FilePath,
					FileID: fileID, FileType: fileType, Content: content,
				})
				*cbs.appliedOps = append(*cbs.appliedOps, appBuilderOperation{
					Action: action, Path: args.FilePath, Type: fileType,
				})
				cbs.pendingFiles[args.FilePath] = content
				cbs.mu.Unlock()
			} else {
				fileMap[args.FilePath] = content
			}
			return fmt.Sprintf("Successfully wrote %s (%d chars, action: %s). File is saved — do not re-write.", args.FilePath, len(content), action), nil

		case "write_files":
			var args struct {
				Files []struct {
					FilePath string `json:"file_path"`
					Content  string `json:"content"`
				} `json:"files"`
			}
			if err := json.Unmarshal([]byte(arguments), &args); err != nil {
				preview := arguments
				if len(preview) > 200 {
					preview = preview[:200]
				}
				slog.Warn("write_files: malformed arguments", "error", err, "args_preview", preview)

				// Fallback: LLM may have sent write_file format (file_path + content at top level)
				var single struct {
					FilePath string `json:"file_path"`
					Content  string `json:"content"`
				}
				if sErr := json.Unmarshal([]byte(arguments), &single); sErr == nil && single.FilePath != "" && single.Content != "" {
					slog.Info("write_files: recovered malformed args as single file", "path", single.FilePath)
					args.Files = append(args.Files, struct {
						FilePath string `json:"file_path"`
						Content  string `json:"content"`
					}{FilePath: single.FilePath, Content: single.Content})
				} else {
					return `Error: invalid arguments for write_files. The "files" field must be a JSON array. Expected: {"files": [{"file_path": "app/page.tsx", "content": "..."}]}. Parse error: ` + err.Error(), nil
				}
			}
			if len(args.Files) == 0 {
				return `Error: files array is empty. Expected: {"files": [{"file_path": "app/page.tsx", "content": "..."}]}`, nil
			}

			var results []string
			for _, f := range args.Files {
				if f.FilePath == "" {
					results = append(results, "SKIP: empty file_path")
					continue
				}
				// Block writes to template-managed files
				if isProtectedTemplateFile(f.FilePath) {
					results = append(results, fmt.Sprintf("SKIP %s: template-managed, dependencies are auto-detected from imports", f.FilePath))
					continue
				}
				content := utils.StripCodeFences(f.Content)
				_, fileType := guessFileType(f.FilePath)
				if fileType == "tsx" || fileType == "ts" {
					content, _ = stripForbiddenImports(content, f.FilePath)
				}

				existingFile, err := c.DB.GetAppGeneratedFileByPath(project.ID.String(), "nextjs", f.FilePath)
				action := "create"
				var fileID uuid.UUID
				if err == nil && existingFile != nil {
					if normalizeContent(content) == normalizeContent(existingFile.Content) {
						results = append(results, fmt.Sprintf("OK %s (no changes)", f.FilePath))
						continue
					}
					existingFile.Content = content
					existingFile.UpdatedAt = time.Now()
					_, err = c.DB.UpdateAppGeneratedFile(existingFile)
					if err != nil {
						results = append(results, fmt.Sprintf("ERROR %s: %s", f.FilePath, err.Error()))
						continue
					}
					action = "modify"
					fileID = existingFile.ID
				} else {
					newFile := models.AppGeneratedFile{
						AppProjectID: project.ID,
						Platform:     "nextjs",
						FilePath:     f.FilePath,
						Content:      content,
						FileType:     fileType,
						IsGenerated:  true,
					}
					createdFile, err := c.DB.CreateAppGeneratedFile(&newFile)
					if err != nil {
						results = append(results, fmt.Sprintf("ERROR %s: %s", f.FilePath, err.Error()))
						continue
					}
					fileID = createdFile.ID
				}
				if cbs != nil {
					cbs.mu.Lock()
					cbs.fileMu.Lock()
					fileMap[f.FilePath] = content
					cbs.fileMu.Unlock()
					cbs.sendEvent("file_complete", responses.FileCompleteEvent{
						Action: action, FilePath: f.FilePath,
						FileID: fileID, FileType: fileType, Content: content,
					})
					*cbs.appliedOps = append(*cbs.appliedOps, appBuilderOperation{
						Action: action, Path: f.FilePath, Type: fileType,
					})
					cbs.pendingFiles[f.FilePath] = content
					cbs.mu.Unlock()
				} else {
					fileMap[f.FilePath] = content
				}
				results = append(results, fmt.Sprintf("OK %s (%d chars, %s)", f.FilePath, len(content), action))
			}
			return fmt.Sprintf("Successfully wrote %d files (all saved to project):\n%s", len(args.Files), strings.Join(results, "\n")), nil

		case "edit_file":
			var args struct {
				FilePath  string `json:"file_path"`
				OldString string `json:"old_string"`
				NewString string `json:"new_string"`
			}
			if err := json.Unmarshal([]byte(arguments), &args); err != nil {
				return fmt.Sprintf("Error: invalid arguments: %s", err.Error()), nil
			}
			if args.FilePath == "" || args.OldString == "" {
				return "Error: file_path and old_string are required", nil
			}
			// Block edits to template-managed files
			if isProtectedTemplateFile(args.FilePath) {
				return fmt.Sprintf("Error: %s is template-managed and cannot be edited directly. Dependencies are auto-detected from imports.", args.FilePath), nil
			}

			cbs.fileMu.RLock()
			content, ok := fileMap[args.FilePath]
			cbs.fileMu.RUnlock()
			if !ok {
				return fmt.Sprintf("Error: file '%s' not found. Use write_file to create it.", args.FilePath), nil
			}

			count := strings.Count(content, args.OldString)
			if count == 0 {
				return fmt.Sprintf("Error: old_string not found in %s. Use read_file to see current content.", args.FilePath), nil
			}
			if count > 1 {
				return fmt.Sprintf("Error: old_string found %d times in %s. Provide a larger unique string.", count, args.FilePath), nil
			}

			newContent := strings.Replace(content, args.OldString, args.NewString, 1)
			_, fileType := guessFileType(args.FilePath)
			if fileType == "tsx" || fileType == "ts" {
				newContent, _ = stripForbiddenImports(newContent, args.FilePath)
			}

			existingFile, err := c.DB.GetAppGeneratedFileByPath(project.ID.String(), "nextjs", args.FilePath)
			if err != nil {
				return fmt.Sprintf("Error: failed to load file: %s", err.Error()), nil
			}
			existingFile.Content = newContent
			existingFile.UpdatedAt = time.Now()
			_, err = c.DB.UpdateAppGeneratedFile(existingFile)
			if err != nil {
				return fmt.Sprintf("Error: failed to save: %s", err.Error()), nil
			}
			if cbs != nil {
				cbs.mu.Lock()
				cbs.fileMu.Lock()
				fileMap[args.FilePath] = newContent
				cbs.fileMu.Unlock()
				cbs.sendEvent("file_complete", responses.FileCompleteEvent{
					Action: "modify", FilePath: args.FilePath,
					FileID: existingFile.ID, FileType: existingFile.FileType,
					Content: newContent,
				})
				*cbs.appliedOps = append(*cbs.appliedOps, appBuilderOperation{
					Action: "modify", Path: args.FilePath, Type: existingFile.FileType,
				})
				cbs.pendingFiles[args.FilePath] = newContent
				cbs.mu.Unlock()
			} else {
				fileMap[args.FilePath] = newContent
			}
			return fmt.Sprintf("Successfully edited %s", args.FilePath), nil

		case "delete_file":
			var args struct {
				FilePath string `json:"file_path"`
			}
			if err := json.Unmarshal([]byte(arguments), &args); err != nil {
				return fmt.Sprintf("Error: invalid arguments: %s", err.Error()), nil
			}
			// Block deletion of template-managed files
			if isProtectedTemplateFile(args.FilePath) {
				return fmt.Sprintf("Error: %s is template-managed and cannot be deleted.", args.FilePath), nil
			}
			existingFile, err := c.DB.GetAppGeneratedFileByPath(project.ID.String(), "nextjs", args.FilePath)
			if err != nil {
				return fmt.Sprintf("Error: file '%s' not found", args.FilePath), nil
			}
			if err := c.DB.DeleteAppGeneratedFile(existingFile); err != nil {
				return fmt.Sprintf("Error: failed to delete file '%s': %s", args.FilePath, err.Error()), nil
			}

			if cbs != nil {
				cbs.mu.Lock()
				cbs.fileMu.Lock()
				delete(fileMap, args.FilePath)
				cbs.fileMu.Unlock()
				cbs.sendEvent("file_complete", responses.FileCompleteEvent{
					Action: "delete", FilePath: args.FilePath,
				})
				*cbs.appliedOps = append(*cbs.appliedOps, appBuilderOperation{
					Action: "delete", Path: args.FilePath,
				})
				delete(cbs.pendingFiles, args.FilePath)
				cbs.pendingDeletes = append(cbs.pendingDeletes, args.FilePath)
				cbs.mu.Unlock()
			} else {
				delete(fileMap, args.FilePath)
			}
			return fmt.Sprintf("Successfully deleted %s", args.FilePath), nil

		case "setup_ecommerce":
			var args struct {
				StoreName string `json:"store_name"`
			}
			if err := json.Unmarshal([]byte(arguments), &args); err != nil {
				return fmt.Sprintf("Error: invalid arguments: %s", err.Error()), nil
			}
			if args.StoreName == "" {
				args.StoreName = project.Name + " Store"
			}

			// Inject e-commerce template files
			injectEcommerceTemplates := func() {
				for _, tp := range constants.NEXTJS_ECOMMERCE_TEMPLATE_FILE_PATHS {
					if _, exists := fileMap[tp]; exists {
						continue
					}
					content := getEcommerceTemplateContent(tp)
					if content == "" {
						continue
					}
					file := &models.AppGeneratedFile{
						AppProjectID: project.ID,
						Platform:     "nextjs",
						FilePath:     tp,
						Content:      content,
						FileType:     "ts",
						IsGenerated:  false,
					}
					if _, err := c.DB.UpsertAppGeneratedFile(file); err != nil {
						slog.Error("failed to upsert ecommerce template", "path", tp, "error", err)
					}
					fileMap[tp] = content
				}
			}

			existingStore, err := c.DB.GetStoreByAppProjectID(project.ID.String())
			if err == nil && existingStore != nil {
				injectEcommerceTemplates()
				envVars := c.CollectAllEnvVars(project)
				if dotEnv := BuildDotEnvLocal(envVars); dotEnv != "" {
					c.injectTemplateFileForTool(project, ".env.local", dotEnv, fileMap, cbs)
				}
				return fmt.Sprintf("E-commerce store already exists.\nStore ID: %s\nStore name: %s\nStorefront API: %s/v2/storefront/%s\nUse NEXT_PUBLIC_STORE_ID and NEXT_PUBLIC_WEMOB_API_URL env vars.\nlib/wemob-store.ts is available — import from \"@/lib/wemob-store\".\nNEXT STEP: Call manage_store to add products.",
					existingStore.ID.String(), existingStore.StoreName, constants.WEMOB_API_BASE_URL, existingStore.ID.String()), nil
			}

			if project.ProjectType != "ecommerce" {
				project.ProjectType = "ecommerce"
				c.DB.UpdateAppProject(project)
			}

			store, err := c.CreateStore(project, user, args.StoreName)
			if err != nil {
				return fmt.Sprintf("Failed to create store: %s", err.Error()), nil
			}

			// Emit visible event so the user sees store creation feedback
			if cbs != nil {
				cbs.sendEvent("store_created", map[string]any{
					"store_id":   store.ID.String(),
					"store_name": store.StoreName,
				})
			}

			injectEcommerceTemplates()
			envVars := c.CollectAllEnvVars(project)
			if dotEnv := BuildDotEnvLocal(envVars); dotEnv != "" {
				c.injectTemplateFileForTool(project, ".env.local", dotEnv, fileMap, cbs)
			}
			return fmt.Sprintf("E-commerce store created!\nStore ID: %s\nStore name: %s\nStorefront API: %s/v2/storefront/%s\nEnv vars: NEXT_PUBLIC_STORE_ID=\"%s\", NEXT_PUBLIC_WEMOB_API_URL=\"%s\"\nlib/wemob-store.ts is available.\nNEXT STEP: Call manage_store to add products.",
				store.ID.String(), store.StoreName, constants.WEMOB_API_BASE_URL, store.ID.String(), store.ID.String(), constants.WEMOB_API_BASE_URL), nil

		case "manage_store":
			var args struct {
				Operations []struct {
					Action string         `json:"action"`
					Data   map[string]any `json:"data"`
				} `json:"operations"`
			}
			if err := json.Unmarshal([]byte(arguments), &args); err != nil {
				return fmt.Sprintf("Failed to parse arguments: %s", err.Error()), nil
			}

			store, err := c.DB.GetStoreByAppProjectID(project.ID.String())
			if err != nil || store == nil {
				return "Error: No store found. Call setup_ecommerce first.", nil
			}

			var results []string
			var errCount int
			for _, op := range args.Operations {
				opData, err := c.executeStoreOperation(store, op.Action, op.Data)
				if err != nil {
					results = append(results, fmt.Sprintf("FAILED %s: %s", op.Action, err.Error()))
					errCount++
				} else {
					name, _ := op.Data["name"].(string)
					code, _ := op.Data["code"].(string)
					label := name
					if label == "" {
						label = code
					}
					if opData != "" {
						results = append(results, fmt.Sprintf("OK %s: %s\n%s", op.Action, label, opData))
					} else {
						results = append(results, fmt.Sprintf("OK %s: %s", op.Action, label))
					}
				}
			}

			return fmt.Sprintf("Executed %d operations (%d succeeded, %d failed):\n%s",
				len(args.Operations), len(args.Operations)-errCount, errCount, strings.Join(results, "\n")), nil

		case "setup_database":
			var args struct {
				Tables    []DataModel `json:"tables"`
				NeedsAuth bool        `json:"needs_auth"`
			}
			if err := json.Unmarshal([]byte(arguments), &args); err != nil {
				return fmt.Sprintf("Error: invalid arguments: %s", err.Error()), nil
			}

			dataModels := args.Tables

			// Merge default ecommerce models if this is an ecommerce project
			if project.ProjectType == "ecommerce" {
				var defaultModels []DataModel
				if err := json.Unmarshal([]byte(constants.ECOMMERCE_DEFAULT_DATA_MODELS), &defaultModels); err == nil {
					existing := make(map[string]bool)
					for _, m := range dataModels {
						existing[m.Name] = true
					}
					for _, dm := range defaultModels {
						if !existing[dm.Name] {
							dataModels = append([]DataModel{dm}, dataModels...)
						}
					}
				}
			}

			// Merge auth models if needed
			if args.NeedsAuth {
				var authModels []DataModel
				if err := json.Unmarshal([]byte(constants.AUTH_DEFAULT_DATA_MODELS), &authModels); err == nil {
					for _, authModel := range authModels {
						found := false
						for i, m := range dataModels {
							if m.Name == authModel.Name {
								dataModels[i] = mergeDataModelColumns(authModel, m)
								found = true
								break
							}
						}
						if !found {
							dataModels = append([]DataModel{authModel}, dataModels...)
						}
					}
				}
				if project.Settings == nil {
					project.Settings = make(map[string]any)
				}
				project.Settings["needs_auth"] = true
			}

			// Store data models in project
			dmJSON, err := json.Marshal(dataModels)
			if err != nil {
				return fmt.Sprintf("Error: failed to marshal data models: %s", err.Error()), nil
			}
			project.DataModels = dmJSON
			if updated, err := c.DB.UpdateAppProject(project); err != nil {
				slog.Error("failed to save data models to project", "error", err, "project_id", project.ID)
			} else {
				project = updated
			}

			// Provision Neon database
			if c.NeonService == nil {
				return "Error: Database provisioning service not available", nil
			}

			if cbs != nil {
				cbs.sendEvent("neon_provision", map[string]any{"phase": "start"})
			}

			connStr, neonProjectID, neonErr := c.provisionNeonDatabase(ctx, project, dataModels, func(eventName string, data any) bool {
				if cbs != nil {
					return cbs.sendEvent(eventName, data)
				}
				return true
			})

			if connStr == "" || neonProjectID == "" || neonErr != nil {
				errMsg := "unknown error"
				if neonErr != nil {
					errMsg = neonErr.Error()
				}
				return fmt.Sprintf("Error: Database provisioning failed: %s", errMsg), nil
			}

			if err := c.storeNeonIntegration(project, connStr, neonProjectID); err != nil {
				return fmt.Sprintf("Error: Failed to store database integration: %s", err.Error()), nil
			}

			// Inject DB template files
			dbContent := constants.NEXTJS_TEMPLATE_DB_TS
			c.injectTemplateFileForTool(project, "lib/db.ts", dbContent, fileMap, cbs)

			schemaContent := buildDrizzleSchema(dataModels)
			c.injectTemplateFileForTool(project, "lib/schema.ts", schemaContent, fileMap, cbs)

			// Inject auth template files if needed
			if args.NeedsAuth {
				authSecret, err := utils.RandomHex(32)
				if err == nil {
					envVar := &models.AppEnvVar{
						AppProjectID: project.ID,
						Key:          "AUTH_SECRET",
						Value:        authSecret,
						IsPublic:     false,
					}
					c.DB.CreateAppEnvVar(envVar)
				}

				for _, tp := range constants.NEXTJS_AUTH_TEMPLATE_FILE_PATHS {
					content := getAuthTemplateContent(tp)
					if content != "" {
						c.injectTemplateFileForTool(project, tp, content, fileMap, cbs)
					}
				}
			}

			// Update .env.local with new vars
			envVars := c.CollectAllEnvVars(project)
			if dotEnv := BuildDotEnvLocal(envVars); dotEnv != "" {
				c.injectTemplateFileForTool(project, ".env.local", dotEnv, fileMap, cbs)
			}

			// Seed the database
			seedResult, seedErr := c.seedNeonDatabase(ctx, project, dataModels, func(eventName string, data any) bool {
				if cbs != nil {
					return cbs.sendEvent(eventName, data)
				}
				return true
			})
			if seedErr != nil {
				slog.Warn("database seeding failed, tables will be empty", "error", seedErr, "project_id", project.ID)
			}
			_ = seedResult

			if cbs != nil {
				cbs.sendEvent("neon_provision", map[string]any{"phase": "ready"})
			}

			var result strings.Builder
			result.WriteString("Database provisioned successfully!\n")
			result.WriteString(fmt.Sprintf("Tables created: %d\n", len(dataModels)))
			for _, dm := range dataModels {
				result.WriteString(fmt.Sprintf("  - %s (%d columns)\n", dm.Name, len(dm.Columns)))
			}
			result.WriteString("lib/db.ts and lib/schema.ts have been generated.\n")
			result.WriteString("Use: import { db } from '@/lib/db' and import { tableName } from '@/lib/schema'\n")
			result.WriteString("Use Drizzle ORM for queries: db.select().from(tableName)\n")
			if args.NeedsAuth {
				result.WriteString("\nAuth infrastructure provisioned:\n")
				result.WriteString("- lib/wemob-auth.ts (server-side auth)\n")
				result.WriteString("- lib/wemob-auth-client.tsx (client-side auth)\n")
				result.WriteString("- users table with id, email, password_hash, name, role, etc.\n")
			}
			return result.String(), nil

		default:
			return fmt.Sprintf("Unknown tool: %s", name), nil
		}
	}
}

// injectTemplateFileForTool is like injectTemplateFile but uses builderToolCallbacks.
func (c *Controller) injectTemplateFileForTool(project *models.AppProject, tp string, content string, fileMap map[string]string, cbs *builderToolCallbacks) {
	fileType := "ts"
	switch {
	case strings.HasSuffix(tp, ".tsx"):
		fileType = "tsx"
	case strings.HasSuffix(tp, ".css"):
		fileType = "css"
	case strings.HasSuffix(tp, ".json"):
		fileType = "json"
	case strings.HasSuffix(tp, ".mjs"):
		fileType = "mjs"
	case tp == ".env.local":
		fileType = "env"
	}

	file := &models.AppGeneratedFile{
		AppProjectID: project.ID,
		Platform:     "nextjs",
		FilePath:     tp,
		Content:      content,
		FileType:     fileType,
		IsGenerated:  false,
	}
	createdFile, err := c.DB.UpsertAppGeneratedFile(file)
	if err != nil {
		slog.Error("failed to save template file via tool", "path", tp, "error", err)
		return
	}
	if cbs != nil {
		cbs.mu.Lock()
		cbs.fileMu.Lock()
		fileMap[tp] = content
		cbs.fileMu.Unlock()
		cbs.sendEvent("file_complete", responses.FileCompleteEvent{
			Action: "create", FilePath: tp,
			FileID: createdFile.ID, FileType: fileType,
			Content: content,
		})
		*cbs.appliedOps = append(*cbs.appliedOps, appBuilderOperation{
			Action: "create", Path: tp, Type: fileType,
		})
		cbs.pendingFiles[tp] = content
		cbs.mu.Unlock()
	} else {
		fileMap[tp] = content
	}
}

// compositeToolExecutor wraps a local ToolExecutor and routes MCP-prefixed tool calls
// to the MCP service, while passing everything else to the local executor.
func (c *Controller) compositeToolExecutor(local utils.ToolExecutor) utils.ToolExecutor {
	return func(name string, arguments string) (string, error) {
		mcp := c.MCPService.Load()
		if mcp != nil && mcp.IsMCPTool(name) {
			return mcp.ToolExecutor()(name, arguments)
		}
		result, err := local(name, arguments)
		if err != nil {
			return result, err
		}
		if strings.HasPrefix(result, "Unknown tool:") && mcp != nil {
			if mcpResult, mcpErr := mcp.ToolExecutor()(name, arguments); mcpErr == nil && !strings.HasPrefix(mcpResult, "Unknown MCP tool:") {
				return mcpResult, nil
			}
		}
		return result, nil
	}
}

// mcpTools returns MCP tools if the service is available, or nil.
func (c *Controller) mcpTools() []utils.Tool {
	mcp := c.MCPService.Load()
	if mcp != nil && mcp.HasTools() {
		return mcp.Tools()
	}
	return nil
}

// --- Single-shot initial generation ---

// runPreGenerationSetup handles stateful operations (e-commerce, database, auth)
// BEFORE the single-shot LLM call so the LLM doesn't need tools for setup.
func (c *Controller) runPreGenerationSetup(
	ctx context.Context,
	project *models.AppProject,
	user *models.User,
	fileMap map[string]string,
	sendEvent func(string, any) bool,
	provider *utils.LLMProvider,
	model string,
	userPrompt string,
) error {
	// --- Database setup ---
	if len(project.DataModels) > 0 && c.NeonService != nil {
		var dataModels []DataModel
		if err := json.Unmarshal(project.DataModels, &dataModels); err != nil {
			slog.Warn("pre-gen: failed to unmarshal data models", "error", err, "project_id", project.ID)
		}

		if len(dataModels) > 0 {
			// Check if already provisioned
			appIntegrations, _ := c.DB.GetAppIntegrationsByProjectID(project.ID.String())
			alreadyProvisioned := false
			for _, integ := range appIntegrations {
				if integ.Provider == "neon" && integ.Status == "connected" {
					alreadyProvisioned = true
					break
				}
			}

			if !alreadyProvisioned {
				sendEvent("neon_provision", map[string]any{"phase": "start"})

				// Merge auth models if needed
				needsAuth := false
				if settings := project.Settings; settings != nil {
					needsAuth, _ = settings["needs_auth"].(bool)
				}
				if needsAuth {
					var authModels []DataModel
					if err := json.Unmarshal([]byte(constants.AUTH_DEFAULT_DATA_MODELS), &authModels); err == nil {
						for _, authModel := range authModels {
							found := false
							for i, m := range dataModels {
								if m.Name == authModel.Name {
									dataModels[i] = mergeDataModelColumns(authModel, m)
									found = true
									break
								}
							}
							if !found {
								dataModels = append([]DataModel{authModel}, dataModels...)
							}
						}
					}
				}

				// Merge ecommerce models if needed
				if project.ProjectType == "ecommerce" {
					var defaultModels []DataModel
					if err := json.Unmarshal([]byte(constants.ECOMMERCE_DEFAULT_DATA_MODELS), &defaultModels); err == nil {
						existing := make(map[string]bool)
						for _, m := range dataModels {
							existing[m.Name] = true
						}
						for _, dm := range defaultModels {
							if !existing[dm.Name] {
								dataModels = append([]DataModel{dm}, dataModels...)
							}
						}
					}
				}

				connStr, neonProjectID, neonErr := c.provisionNeonDatabase(ctx, project, dataModels, sendEvent)
				if connStr != "" && neonProjectID != "" && neonErr == nil {
					if err := c.storeNeonIntegration(project, connStr, neonProjectID); err != nil {
						slog.Error("pre-gen: failed to store neon integration", "error", err, "project_id", project.ID)
					}

					// Inject DB template files
					dbContent := constants.NEXTJS_TEMPLATE_DB_TS
					c.injectTemplateFile(project, "lib/db.ts", dbContent, fileMap, sendEvent)

					schemaContent := buildDrizzleSchema(dataModels)
					c.injectTemplateFile(project, "lib/schema.ts", schemaContent, fileMap, sendEvent)

					// Seed database
					if _, seedErr := c.seedNeonDatabase(ctx, project, dataModels, sendEvent); seedErr != nil {
						slog.Warn("pre-gen: database seeding failed", "error", seedErr, "project_id", project.ID)
					}
				} else {
					slog.Error("pre-gen: database provisioning failed", "error", neonErr, "project_id", project.ID)
				}

				sendEvent("neon_provision", map[string]any{"phase": "ready"})
			}
		}
	}

	// --- Auth setup ---
	if settings := project.Settings; settings != nil {
		if needsAuth, ok := settings["needs_auth"].(bool); ok && needsAuth {
			// Check if auth templates are already injected
			if _, exists := fileMap["lib/wemob-auth.ts"]; !exists {
				authSecret, err := utils.RandomHex(32)
				if err == nil {
					envVar := &models.AppEnvVar{
						AppProjectID: project.ID,
						Key:          "AUTH_SECRET",
						Value:        authSecret,
						IsPublic:     false,
					}
					c.DB.CreateAppEnvVar(envVar)
				}

				for _, tp := range constants.NEXTJS_AUTH_TEMPLATE_FILE_PATHS {
					content := getAuthTemplateContent(tp)
					if content != "" {
						c.injectTemplateFile(project, tp, content, fileMap, sendEvent)
					}
				}
			}

			// Update .env.local with auth vars
			envVars := c.CollectAllEnvVars(project)
			if dotEnv := BuildDotEnvLocal(envVars); dotEnv != "" {
				c.injectTemplateFile(project, ".env.local", dotEnv, fileMap, sendEvent)
			}
		}
	}

	return nil
}

// streamSingleShotGeneration performs initial app generation in a single LLM call.
// The LLM outputs all files using <file path="...">content</file> blocks which are
// parsed incrementally and saved as they complete.
func (c *Controller) streamSingleShotGeneration(
	ctx context.Context,
	project *models.AppProject,
	user *models.User,
	model string,
	provider *utils.LLMProvider,
	fileMap map[string]string,
	sendEvent func(string, any) bool,
	message string,
	imageURLs []string,
) (responseText string, ops []appBuilderOperation, err error) {
	// 1. Structure planning — must run BEFORE pre-gen setup so that data_models
	// and needs_auth are available for Neon provisioning and auth template injection.
	structure, planErr := c.PlanAppFileStructure(ctx, message, project.ProjectType)
	if planErr != nil {
		slog.Warn("single-shot: structure planning failed, proceeding without plan",
			"error", planErr, "project_id", project.ID)
	} else {
		// Apply name/slug/theme/file_plan/data_models/needs_auth to project
		if structure.Name != "" {
			project.Name = structure.Name
		}
		if structure.Slug != "" {
			project.Slug = structure.Slug + "-" + uuid.New().String()[:8]
		}
		themeData := models.AppTheme{
			ColorScheme:     structure.ColorScheme,
			DarkColorScheme: structure.DarkColorScheme,
			Typography:      structure.Typography,
		}
		if themeJSON, err := json.Marshal(themeData); err == nil {
			project.Theme = themeJSON
		}
		c.ApplyStructureToProject(project, structure)
		if _, err := c.DB.UpdateAppProject(project); err != nil {
			slog.Warn("single-shot: failed to update project after structure planning",
				"error", err, "project_id", project.ID)
		}
	}

	// 2. Run pre-generation setup (e-commerce store, Neon database, auth templates).
	// Now that structure planning has set data_models and needs_auth on the project,
	// this step can properly provision the database and inject auth files.
	if setupErr := c.runPreGenerationSetup(ctx, project, user, fileMap, sendEvent, provider, model, message); setupErr != nil {
		return "", nil, fmt.Errorf("pre-generation setup failed: %w", setupErr)
	}

	// Inject parameterized e-commerce templates (layout + globals.css)
	if project.ProjectType == "ecommerce" {
		var theme models.AppTheme
		if len(project.Theme) > 0 {
			json.Unmarshal(project.Theme, &theme)
		}
		if layoutContent := buildEcommerceLayout(project.Name, theme.Typography); layoutContent != "" {
			c.injectTemplateFile(project, "app/layout.tsx", layoutContent, fileMap, sendEvent)
		}
		if cssContent := buildEcommerceGlobalsCSSTheme(theme.ColorScheme, theme.DarkColorScheme); cssContent != "" {
			c.injectTemplateFile(project, "app/globals.css", cssContent, fileMap, sendEvent)
		}
	}

	// Send project_created event (with planned files when available)
	projectCreatedPayload := map[string]any{
		"id":   project.ID,
		"name": project.Name,
		"slug": project.Slug,
	}
	if structure != nil && len(structure.Files) > 0 {
		plannedPaths := make([]string, 0, len(structure.Files))
		for _, f := range structure.Files {
			plannedPaths = append(plannedPaths, f.Path)
		}
		projectCreatedPayload["planned_files"] = plannedPaths
	}
	sendEvent("project_created", projectCreatedPayload)

	// 3. Build messages with single-shot system prompt
	llmMessages := []utils.Message{
		{Role: "system", Content: buildSingleShotSystemPrompt(project)},
		{Role: "system", Content: c.buildProjectContext(project, fileMap, true)},
	}

	userContent := message
	if len(imageURLs) > 0 {
		userContent += fmt.Sprintf("\n\n[User attached %d image(s) for visual reference — recreate the design shown]", len(imageURLs))
	}
	llmMessages = append(llmMessages, utils.Message{Role: "user", Content: userContent})

	// 4. Set up streaming file parser
	var responseBuilder strings.Builder
	parser := &utils.FileStreamParser{
		OnFile: func(path, content string) {
			content = utils.StripCodeFences(content)
			_, fileType := guessFileType(path)
			if fileType == "tsx" || fileType == "ts" {
				content, _ = stripForbiddenImports(content, path)
			}
			c.saveGeneratedFile(project, path, content, fileType, fileMap, sendEvent, &ops)
		},
		OnText: func(text string) {
			responseBuilder.WriteString(text)
			cleaned := ansiRegex.ReplaceAllString(text, "")
			if cleaned != "" {
				sendEvent("chat_text", responses.ChatTextEvent{Chunk: cleaned})
			}
		},
	}

	// 5. Single LLM call — no tools, no rounds
	slog.Info("Starting single-shot generation",
		"model", model,
		"project_id", project.ID,
		"message_count", len(llmMessages),
	)

	_, err = utils.StreamTextFromLLMWithMessages(
		ctx, model, llmMessages, provider,
		func(chunk string) { parser.Feed(chunk) },
		func(chunk string) {
			sendEvent("chat_thinking", responses.ChatThinkingEvent{Chunk: chunk})
		},
		utils.StreamTextConfig{MaxTokens: 65536},
	)
	parser.Flush()

	// 6. Run deterministic auto-fixes before syncing (e.g. remove "use client" from
	// files that export metadata — Next.js will 500 otherwise).
	preFixSnapshot := make(map[string]string, len(fileMap))
	for k, v := range fileMap {
		preFixSnapshot[k] = v
	}
	autoFixMetadataInClientComponent(fileMap)
	autoFixMissingUseClient(fileMap)
	autoFixRootLayoutTags(fileMap)
	// Persist any auto-fixed or newly created content back to the DB
	for path, content := range fileMap {
		prev, existed := preFixSnapshot[path]
		if !existed {
			// New file created by auto-fix (e.g. components/layout-client.tsx)
			_, fileType := guessFileType(path)
			c.saveGeneratedFile(project, path, content, fileType, fileMap, sendEvent, &ops)
		} else if content != prev {
			// Existing file was modified by auto-fix
			if dbFile, err := c.DB.GetAppGeneratedFileByPath(project.ID.String(), "nextjs", path); err == nil && dbFile != nil {
				dbFile.Content = content
				c.DB.UpdateAppGeneratedFile(dbFile)
			}
		}
	}

	// 7. Sync all files to preview
	if c.PreviewService != nil {
		c.SyncPreviewAll(project)
	}

	return responseBuilder.String(), ops, err
}

// streamEcommerceGeneration performs initial e-commerce app generation in two phases:
//   Phase 1: Tool-calling with only setup_ecommerce + manage_store (populates the store).
//   Phase 2: Single-shot file generation using <file> blocks (generates all code files).
// This is faster and cheaper than running a single tool loop for everything.
func (c *Controller) streamEcommerceGeneration(
	ctx context.Context,
	project *models.AppProject,
	user *models.User,
	model string,
	provider *utils.LLMProvider,
	fileMap map[string]string,
	sendEvent func(string, any) bool,
	message string,
	imageURLs []string,
) (responseText string, ops []appBuilderOperation, err error) {
	// 1. Structure planning (same as single-shot)
	structure, planErr := c.PlanAppFileStructure(ctx, message, project.ProjectType)
	if planErr != nil {
		slog.Warn("ecommerce-gen: structure planning failed, proceeding without plan",
			"error", planErr, "project_id", project.ID)
	} else {
		if structure.Name != "" {
			project.Name = structure.Name
		}
		if structure.Slug != "" {
			project.Slug = structure.Slug + "-" + uuid.New().String()[:8]
		}
		themeData := models.AppTheme{
			ColorScheme:     structure.ColorScheme,
			DarkColorScheme: structure.DarkColorScheme,
			Typography:      structure.Typography,
		}
		if themeJSON, err := json.Marshal(themeData); err == nil {
			project.Theme = themeJSON
		}
		c.ApplyStructureToProject(project, structure)
		if _, err := c.DB.UpdateAppProject(project); err != nil {
			slog.Warn("ecommerce-gen: failed to update project after structure planning",
				"error", err, "project_id", project.ID)
		}
	}

	// Send project_created event
	projectCreatedPayload := map[string]any{
		"id":   project.ID,
		"name": project.Name,
		"slug": project.Slug,
	}
	if structure != nil && len(structure.Files) > 0 {
		plannedPaths := make([]string, 0, len(structure.Files))
		for _, f := range structure.Files {
			plannedPaths = append(plannedPaths, f.Path)
		}
		projectCreatedPayload["planned_files"] = plannedPaths
	}
	sendEvent("project_created", projectCreatedPayload)

	// === Phase 1: Setup (tool-calling with setup_ecommerce + manage_store only) ===
	// Use fast model — setup is just tool calling to create products, no code generation.
	setupModel := c.fixModel(provider)
	slog.Info("ecommerce-gen: starting Phase 1 (store setup)",
		"model", setupModel, "project_id", project.ID)

	setupTools := filterToolsByName("setup_ecommerce", "manage_store")

	setupCallbacks := &builderToolCallbacks{
		sendEvent:    sendEvent,
		appliedOps:   &ops,
		pendingFiles: make(map[string]string),
	}

	setupExecutor := c.buildBuilderToolExecutor(ctx, project, user, fileMap, setupCallbacks)
	executor := c.compositeToolExecutor(setupExecutor)

	setupMessages := []utils.Message{
		{Role: "system", Content: constants.NEXTJS_ECOMMERCE_SETUP_INSTRUCTIONS},
		{Role: "user", Content: message},
	}

	setupConfig := utils.ToolCallConfig{
		MaxRounds:  8,
		NudgeAfter: 5,
		StripAfter: 6,
		MaxTokens:  4096,
		NudgeMessage: "Finish populating the store and provide a brief summary.",
		TrimArgs: func(toolName, args string) string {
			// manage_store args contain full product data (images, descriptions) —
			// compress in history to avoid token bloat across rounds.
			if toolName == "manage_store" && len(args) > 300 {
				var parsed struct {
					Operations []struct {
						Action string `json:"action"`
					} `json:"operations"`
				}
				if err := json.Unmarshal([]byte(args), &parsed); err == nil {
					actions := make([]string, len(parsed.Operations))
					for i, op := range parsed.Operations {
						actions[i] = op.Action
					}
					return fmt.Sprintf(`{"operations_summary":"%d ops: %s"}`, len(parsed.Operations), strings.Join(actions, ", "))
				}
			}
			return args
		},
		OnToolCallStart: func(toolName string) {
			sendEvent("tool_start", map[string]any{"tool": toolName})
		},
	}

	_, setupErr := utils.StreamWithTools(
		ctx, setupModel, setupMessages, setupTools, provider,
		nil, // no forced JSON format
		nil, // no reasoning for setup phase
		executor,
		func(toolName, args string) {
			sendEvent("tool_call", responses.ToolCallEvent{
				Tool:      toolName,
				Arguments: args,
			})
		},
		func(chunk string) {
			chunk = ansiRegex.ReplaceAllString(chunk, "")
			if chunk != "" {
				sendEvent("chat_text", responses.ChatTextEvent{Chunk: chunk})
			}
		},
		func(chunk string) {
			sendEvent("chat_thinking", responses.ChatThinkingEvent{Chunk: chunk})
		},
		setupConfig,
	)

	// Flush any pending preview files from Phase 1
	c.flushPendingPreview(project, setupCallbacks)

	if setupErr != nil {
		return "", ops, fmt.Errorf("ecommerce setup phase failed: %w", setupErr)
	}

	// Check context cancellation between phases
	if ctx.Err() != nil {
		return "", ops, ctx.Err()
	}

	// === Phase 1.5: Generate AI product images ===
	store, storeErr := c.DB.GetStoreByAppProjectID(project.ID.String())
	if storeErr == nil && store != nil {
		sendEvent("tool_start", map[string]any{"tool": "generate_product_images"})
		c.GenerateProductImages(ctx, store)
	}

	// Check context cancellation between phases
	if ctx.Err() != nil {
		return "", ops, ctx.Err()
	}

	// === Phase 2: File generation (single-shot, no tools) ===
	slog.Info("ecommerce-gen: starting Phase 2 (file generation)",
		"model", model, "project_id", project.ID)

	// Run pre-generation setup (injects e-commerce templates, auth files, etc.)
	if setupErr := c.runPreGenerationSetup(ctx, project, user, fileMap, sendEvent, provider, model, message); setupErr != nil {
		return "", ops, fmt.Errorf("pre-generation setup failed: %w", setupErr)
	}

	// Inject parameterized e-commerce templates (layout + globals.css)
	if project.ProjectType == "ecommerce" {
		var theme models.AppTheme
		if len(project.Theme) > 0 {
			json.Unmarshal(project.Theme, &theme)
		}
		if layoutContent := buildEcommerceLayout(project.Name, theme.Typography); layoutContent != "" {
			c.injectTemplateFile(project, "app/layout.tsx", layoutContent, fileMap, sendEvent)
		}
		if cssContent := buildEcommerceGlobalsCSSTheme(theme.ColorScheme, theme.DarkColorScheme); cssContent != "" {
			c.injectTemplateFile(project, "app/globals.css", cssContent, fileMap, sendEvent)
		}
	}

	// Rebuild messages with fresh context (now includes store data from Phase 1)
	llmMessages := []utils.Message{
		{Role: "system", Content: buildSingleShotSystemPrompt(project)},
		{Role: "system", Content: c.buildProjectContext(project, fileMap, true)},
	}

	userContent := message
	if len(imageURLs) > 0 {
		userContent += fmt.Sprintf("\n\n[User attached %d image(s) for visual reference — recreate the design shown]", len(imageURLs))
	}
	llmMessages = append(llmMessages, utils.Message{Role: "user", Content: userContent})

	// Set up streaming file parser
	var responseBuilder strings.Builder
	parser := &utils.FileStreamParser{
		OnFile: func(path, content string) {
			content = utils.StripCodeFences(content)
			_, fileType := guessFileType(path)
			if fileType == "tsx" || fileType == "ts" {
				content, _ = stripForbiddenImports(content, path)
			}
			c.saveGeneratedFile(project, path, content, fileType, fileMap, sendEvent, &ops)
		},
		OnText: func(text string) {
			responseBuilder.WriteString(text)
			cleaned := ansiRegex.ReplaceAllString(text, "")
			if cleaned != "" {
				sendEvent("chat_text", responses.ChatTextEvent{Chunk: cleaned})
			}
		},
	}

	// Single LLM call — no tools
	slog.Info("ecommerce-gen: Phase 2 LLM call",
		"model", model,
		"project_id", project.ID,
		"message_count", len(llmMessages),
	)

	_, err = utils.StreamTextFromLLMWithMessages(
		ctx, model, llmMessages, provider,
		func(chunk string) { parser.Feed(chunk) },
		func(chunk string) {
			sendEvent("chat_thinking", responses.ChatThinkingEvent{Chunk: chunk})
		},
		utils.StreamTextConfig{MaxTokens: 65536},
	)
	parser.Flush()

	// Run deterministic auto-fixes before syncing (e.g. remove "use client" from
	// files that export metadata — Next.js will 500 otherwise).
	preFixSnapshot := make(map[string]string, len(fileMap))
	for k, v := range fileMap {
		preFixSnapshot[k] = v
	}
	autoFixMetadataInClientComponent(fileMap)
	autoFixMissingUseClient(fileMap)
	autoFixRootLayoutTags(fileMap)
	// Persist any auto-fixed or newly created content back to the DB
	for path, content := range fileMap {
		prev, existed := preFixSnapshot[path]
		if !existed {
			_, fileType := guessFileType(path)
			c.saveGeneratedFile(project, path, content, fileType, fileMap, sendEvent, &ops)
		} else if content != prev {
			if dbFile, err := c.DB.GetAppGeneratedFileByPath(project.ID.String(), "nextjs", path); err == nil && dbFile != nil {
				dbFile.Content = content
				c.DB.UpdateAppGeneratedFile(dbFile)
			}
		}
	}

	// Sync all files to preview
	if c.PreviewService != nil {
		c.SyncPreviewAll(project)
	}

	return responseBuilder.String(), ops, err
}

// summarizeConversation generates a concise summary of older conversation messages.
// If the LLM call fails and existingSummary is non-empty, it is preserved instead of
// generating a lossy fallback.
func (c *Controller) summarizeConversation(ctx context.Context, messages []models.AppConversationMessage, provider *utils.LLMProvider, existingSummary string) string {
	var conversationText strings.Builder
	for _, msg := range messages {
		role := msg.Role
		content := msg.Content
		if len(content) > 500 {
			content = content[:500] + "..."
		}
		conversationText.WriteString(fmt.Sprintf("[%s]: %s\n", role, content))
		if len(msg.FileOperations) > 0 {
			var ops []string
			for _, op := range msg.FileOperations {
				ops = append(ops, fmt.Sprintf("%s %s", op.Action, op.FilePath))
			}
			conversationText.WriteString(fmt.Sprintf("  Files: %s\n", strings.Join(ops, ", ")))
		}
	}

	system := `Summarize this conversation between a user and an AI assistant that is building/editing a Next.js application. Focus on:
1. What the user asked for (features, changes, fixes)
2. What files were created, modified, or deleted
3. Key design decisions made
4. Any issues encountered and how they were resolved

Be concise (3-6 sentences). Include specific file paths and component names.`

	summary, err := utils.GetTextFromLLM(ctx, c.fixModel(provider), system, conversationText.String(), provider)
	if err != nil {
		slog.Warn("failed to summarize conversation", "error", err)
		// Preserve existing summary if available instead of generating a lossy fallback
		if existingSummary != "" {
			return existingSummary
		}
		var ops []string
		for _, msg := range messages {
			for _, op := range msg.FileOperations {
				ops = append(ops, fmt.Sprintf("%s %s", op.Action, op.FilePath))
			}
		}
		if len(ops) > 0 {
			return fmt.Sprintf("Previous conversation (%d messages). File operations: %s", len(messages), strings.Join(ops, ", "))
		}
		return fmt.Sprintf("Previous conversation with %d messages.", len(messages))
	}
	return strings.TrimSpace(summary)
}

// GetAppConversation returns the conversation history for a project.
func (c *Controller) GetAppConversation(projectID string) (*models.AppConversation, error) {
	return c.DB.GetAppConversationByProjectID(projectID)
}

// ClearAppConversation deletes the conversation for a project so the user can start fresh.
func (c *Controller) ClearAppConversation(projectID string) error {
	return c.DB.DeleteAppConversation(projectID)
}

// waitForClarification sends clarification questions to the client and blocks
// until the user answers, skips, or a 5-minute timeout elapses.
// Returns the result and whether the user actually answered (vs skip/timeout).
func (c *Controller) waitForClarification(
	ctx context.Context,
	jobID string,
	questions *ClarificationResponse,
	sendEvent func(string, any) bool,
) (ClarificationResult, bool) {
	ch := make(chan ClarificationResult, 1)
	c.PendingClarifications.Store(jobID, ch)

	// Update job status to awaiting_input
	if err := c.DB.UpdateGenerationJobStatus(jobID, "awaiting_input", "waiting for clarification answers"); err != nil {
		slog.Warn("failed to update job status to awaiting_input", "error", err, "job_id", jobID)
	}

	// Send clarification_needed event to client
	sendEvent("clarification_needed", map[string]any{
		"questions": questions.Questions,
	})

	// Block until answer, timeout, or cancellation
	timer := time.NewTimer(5 * time.Minute)
	defer timer.Stop()

	select {
	case result, ok := <-ch:
		c.PendingClarifications.Delete(jobID)
		if !ok || result.Skip {
			// Channel closed (cancel) or user skipped
			c.DB.UpdateGenerationJobStatus(jobID, "running", "")
			return ClarificationResult{Skip: true}, false
		}
		c.DB.UpdateGenerationJobStatus(jobID, "running", "")
		return result, true
	case <-timer.C:
		c.PendingClarifications.Delete(jobID)
		slog.Info("clarification timed out, proceeding with original prompt", "job_id", jobID)
		c.DB.UpdateGenerationJobStatus(jobID, "running", "")
		return ClarificationResult{Skip: true}, false
	case <-ctx.Done():
		c.PendingClarifications.Delete(jobID)
		return ClarificationResult{Skip: true}, false
	}
}

// buildEnrichedPrompt appends user clarification answers to the original prompt.
func buildEnrichedPrompt(original string, result ClarificationResult) string {
	if result.Skip || len(result.Answers) == 0 {
		return original
	}

	var sb strings.Builder
	sb.WriteString(original)
	sb.WriteString("\n\n--- User clarifications ---\n")
	for _, a := range result.Answers {
		sb.WriteString(fmt.Sprintf("Q (%s): %s\n", a.ID, a.Answer))
	}
	return sb.String()
}
