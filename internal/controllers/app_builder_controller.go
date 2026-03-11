package controllers

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/services/fal"
	"github.com/knid/wemob/internal/services/integrations"
	"github.com/knid/wemob/internal/utils"
	"github.com/knid/wemob/internal/utils/external"
	"github.com/posthog/posthog-go"
)

// ansiRegex strips ANSI escape sequences that some LLM providers emit in streamed text.
var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// isErrorReport detects whether a user message is a structured error report
// (auto-sent by the frontend when a runtime/build error occurs). These messages
// follow a Markdown pattern with ## Error Type, ## Error Message, ## Code Frame.
func isErrorReport(msg string) bool {
	return (strings.Contains(msg, "## Error Type") || strings.Contains(msg, "## Error Message")) &&
		(strings.Contains(msg, "## Code Frame") || strings.Contains(msg, "at ") || strings.Contains(msg, "Module not found"))
}

// normalizeContent trims whitespace and normalizes line endings for content
// comparison. We preserve internal formatting (indentation, multi-line strings)
// to avoid false "unchanged" results when only whitespace differs.
func normalizeContent(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return s
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
	fileMap        map[string]string       // the shared file map — all access must go through fileMu
	pendingFiles   map[string]string       // files changed since last preview sync
	pendingDeletes []string               // files deleted since last preview sync
}

// setFile safely writes a key-value pair to the shared fileMap.
func (b *builderToolCallbacks) setFile(path, content string) {
	b.fileMu.Lock()
	b.fileMap[path] = content
	b.fileMu.Unlock()
}

// deleteFile safely removes a path from the shared fileMap.
func (b *builderToolCallbacks) deleteFile(path string) {
	b.fileMu.Lock()
	delete(b.fileMap, path)
	b.fileMu.Unlock()
}

// getFile safely reads a file from the shared fileMap.
func (b *builderToolCallbacks) getFile(path string) (string, bool) {
	b.fileMu.RLock()
	content, ok := b.fileMap[path]
	b.fileMu.RUnlock()
	return content, ok
}

// fileCount returns the number of files in the shared fileMap.
func (b *builderToolCallbacks) fileCount() int {
	b.fileMu.RLock()
	n := len(b.fileMap)
	b.fileMu.RUnlock()
	return n
}

// snapshotFileMap returns a shallow copy of fileMap for read-only use.
func (b *builderToolCallbacks) snapshotFileMap() map[string]string {
	b.fileMu.RLock()
	m := make(map[string]string, len(b.fileMap))
	for k, v := range b.fileMap {
		m[k] = v
	}
	b.fileMu.RUnlock()
	return m
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

	var eventsClosed sync.Mutex
	eventsOpen := true
	sendEvent := func(eventName string, data any) bool {
		eventsClosed.Lock()
		if !eventsOpen {
			eventsClosed.Unlock()
			return false
		}
		eventsClosed.Unlock()
		defer func() {
			if r := recover(); r != nil {
				eventsClosed.Lock()
				eventsOpen = false
				eventsClosed.Unlock()
			}
		}()
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
			slog.Error("StreamBuildApp panicked", "error", r, "project_id", project.ID,
				"stack", string(debug.Stack()))
			// Auto-rollback on panic
			if rollbackSnapshotID != "" && rollbackOps != nil && len(*rollbackOps) > 0 {
				slog.Info("Auto-rollback: restoring after panic", "project_id", project.ID)
				c.RestoreAppSnapshot(project, rollbackSnapshotID)
			}
			// Best-effort error notification — sendEvent is panic-safe
			sendEvent("error", responses.GenerationErrorEvent{
				Error: "internal error, please try again",
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
	var generationSucceeded bool // set to true only when generation completes without error

	defer func() {
		closeEvents.Do(func() { close(events) })
		c.RecordTokenUsage(user.ID, actionLabel, model, provider.Name, project.ID.String(), project.ProjectType, tracker)
		prompt, completion, total, callCount := tracker.Totals()
		if total > 0 && generationSucceeded {
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
		if err := c.DB.DeleteAppGeneratedFilesByProjectPlatform(project.ID.String(), "nextjs"); err != nil {
			slog.Error("failed to delete existing files before generation", "error", err, "project_id", project.ID)
			sendError("database", "failed to clean up existing files")
			return
		}

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
	var llmMessages []utils.Message

	// Add conversation history
	if isInitialGeneration {
		llmMessages = []utils.Message{
			{Role: "system", Content: buildSystemPrompt(project)},
			{Role: "system", Content: c.buildProjectContext(project, fileMap, isInitialGeneration)},
		}
		userMsg := utils.Message{Role: "user", Content: message}
		if len(imageURLs) > 0 {
			if provider.SupportsVision() {
				userMsg.Images = imageURLs
			} else {
				userMsg.Content += fmt.Sprintf("\n\n[User attached %d image(s) for visual reference — recreate the design shown]", len(imageURLs))
			}
		}
		llmMessages = append(llmMessages, userMsg)
	} else {
		// Chat path: select relevant files, then build context
		selectedFiles := c.selectContextFiles(ctx, provider, fileMap, message, messages)
		projectContext := c.buildProjectContextForChat(project, fileMap, selectedFiles)

		llmMessages = []utils.Message{
			{Role: "system", Content: buildChatSystemPrompt(project)},
			{Role: "system", Content: projectContext},
		}

		// Add context + conversation history with summarization
		llmMessages = append(llmMessages, utils.Message{Role: "user", Content: "I understand the current project state. What would you like me to do?"})

		const keepRecent = 10
		const summarizeThreshold = 15

		if len(messages) > summarizeThreshold {
			oldMessages := messages[:len(messages)-keepRecent]
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
			llmMessages = append(llmMessages, chatMessagesToLLM(messages[len(messages)-keepRecent:], provider)...)
		} else {
			llmMessages = append(llmMessages, chatMessagesToLLM(messages, provider)...)
		}
	}

	if ctx.Err() != nil {
		return
	}

	// --- Create auto-snapshot before changes ---
	// For chat edits, we require a snapshot so we can rollback on failure.
	// For initial generation there's nothing to rollback to, so a failure is OK.
	preSnapshot, snapErr := c.CreateAppSnapshot(project, "Auto-save before changes", "auto")
	if snapErr != nil && !isInitialGeneration {
		slog.Error("failed to create pre-change snapshot, aborting chat edit",
			"error", snapErr, "project_id", project.ID)
		sendError("snapshot", "failed to create safety snapshot. please try again")
		return
	} else if snapErr != nil {
		slog.Warn("failed to create pre-change snapshot for initial gen, rollback will not be available",
			"error", snapErr, "project_id", project.ID)
	}
	var preSnapshotID string
	if preSnapshot != nil {
		preSnapshotID = preSnapshot.ID.String()
		if err := c.DB.DeleteOldAutoSnapshots(project.ID.String(), 20); err != nil {
			slog.Warn("failed to delete old auto snapshots", "error", err, "project_id", project.ID)
		}
	}

	// --- Pre-generation web search phase ---
	// For initial generation, run a web search pass to gather context about
	// referenced websites, technologies, or anything needing up-to-date info
	// BEFORE clarification questions so that results inform the entire flow.
	var preGenCtx preGenContext
	if isInitialGeneration {
		preGenCtx = c.runPreGenWebSearch(ctx, provider, message, sendEvent)
		if preGenCtx.HasContent() {
			slog.Info("Initial generation: web search returned context",
				"project_id", project.ID, "context_length", len(preGenCtx.Text),
				"screenshots", len(preGenCtx.Screenshots))
		}
	}
	// preGenToolContext is the text portion, used by downstream functions that take string
	preGenToolContext := preGenCtx.Text

	if ctx.Err() != nil {
		return
	}

	// --- Conversational pre-build phase (replaces old clarification questions) ---
	if isInitialGeneration && jobID != "" {
		// Phase A: Stream a summary of web search results to the user
		var webSummaryText string
		if preGenCtx.HasContent() {
			webSummaryText = c.streamWebSearchSummary(ctx, provider, preGenCtx, message, sendEvent)
		}

		if ctx.Err() != nil {
			return
		}

		// Phase B: Natural conversation loop (if prompt is vague)
		message = c.runConversationLoop(ctx, jobID, provider, message, preGenToolContext, webSummaryText, project.ProjectType, sendEvent)

		if ctx.Err() != nil {
			return
		}

		// Phase C: Stream build plan to the user
		c.streamBuildPlan(ctx, provider, message, preGenToolContext, project.ProjectType, sendEvent)
	}

	if ctx.Err() != nil {
		return
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
		ecomImages := imageURLs
		if len(preGenCtx.Screenshots) > 0 {
			ecomImages = append(append([]string{}, imageURLs...), preGenCtx.Screenshots...)
		}
		ecomText, ecomOps, ecomErr := c.streamEcommerceGeneration(
			ctx, project, user, model, provider, fileMap, sendEvent, message, ecomImages, preGenToolContext,
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
						"rolled_back":     len(ecomOps),
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
		responseText = ecomText
		toolAppliedOps = ecomOps
	} else if isInitialGeneration {
		// === Single-shot path: one LLM call, no tool loop (non-ecommerce) ===
		// Merge pre-gen screenshots with user-provided images for vision
		genImages := imageURLs
		if len(preGenCtx.Screenshots) > 0 {
			genImages = append(append([]string{}, imageURLs...), preGenCtx.Screenshots...)
		}
		singleShotText, singleShotOps, singleShotErr := c.streamSingleShotGeneration(
			ctx, project, user, model, provider, fileMap, sendEvent, message, genImages, preGenToolContext,
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
						"rolled_back":     len(singleShotOps),
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
		responseText = singleShotText
		toolAppliedOps = singleShotOps
	} else {
		// === File-block path: chat edits ===
		// Two phases:
		//   Phase 1 (fast model): Tool-calling with Haiku to detect URLs, preview
		//     websites, and gather context the code-gen model needs.
		//   Phase 2 (main model): Single-shot StreamTextFromLLMWithMessages that
		//     outputs <file> blocks, parsed by FileStreamParser.

		var responseBuilder strings.Builder
		var deletedPaths []string
		chatFileCounter := 0
		parser := &utils.FileStreamParser{
			DiscardIncomplete: true,
			OnFile: func(path, content string) {
				content = utils.StripCodeFences(content)
				_, fileType := guessFileType(path)
				if fileType == "tsx" || fileType == "ts" {
					content, _ = stripForbiddenImports(content, path)
				}
				c.saveGeneratedFile(project, path, content, fileType, fileMap, sendEvent, &toolAppliedOps, true)
				chatFileCounter++
				sendEvent("progress", responses.ProgressEvent{
					Phase:   "generating_files",
					Message: fmt.Sprintf("Updated %s (%d files)", path, chatFileCounter),
					Current: chatFileCounter,
				})
			},
			OnDelete: func(path string) {
				c.deleteGeneratedFile(project, path, fileMap, sendEvent, &toolAppliedOps)
				deletedPaths = append(deletedPaths, path)
			},
			OnText: func(text string) {
				responseBuilder.WriteString(text)
				cleaned := ansiRegex.ReplaceAllString(text, "")
				if cleaned != "" {
					sendEvent("chat_text", responses.ChatTextEvent{Chunk: cleaned})
				}
			},
		}

		// Reasoning config
		reasoning := c.reasoningConfig()
		if reasoningEffort != "" && reasoningEffort != "none" {
			reasoning = &utils.ReasoningConfig{Effort: reasoningEffort}
		} else if reasoningEffort == "none" {
			reasoning = nil
		}

		// --- Phase 1: fast model tool-calling (Haiku) ---
		// Runs a cheap model with tools to detect URLs and gather extra context
		// before the main code-gen call. If no tools are called the phase is a
		// near-instant no-op.
		toolContext := c.runChatToolPhase(ctx, project, provider, message, fileMap, sendEvent)
		if toolContext != "" {
			// Inject tool-gathered context as a system message before the user message
			insertIdx := len(llmMessages) - 1 // before last user message
			if insertIdx < 0 {
				insertIdx = 0
			}
			contextMsg := utils.Message{
				Role:    "system",
				Content: toolContext,
			}
			llmMessages = append(llmMessages[:insertIdx], append([]utils.Message{contextMsg}, llmMessages[insertIdx:]...)...)
		}

		// --- Phase 2: main model single-shot code generation ---
		slog.Info("Calling LLM for chat (file-block path)",
			"model", model,
			"project_id", project.ID,
			"message_count", len(llmMessages),
			"has_tool_context", toolContext != "",
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
				MaxTokens: 16384,
				Reasoning: reasoning,
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

		if chatErr != nil {
			slog.Error("LLM chat call failed", "error", chatErr, "project_id", project.ID)

			// Auto-rollback on failure
			if preSnapshotID != "" && len(toolAppliedOps) > 0 {
				slog.Info("Auto-rollback: restoring pre-snapshot after failed chat",
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
	}

	// Record conversation tracing
	tracker.TraceInput = message
	tracker.TraceOutput = responseText

	// --- Post-processing: deterministic auto-fixes ---
	filesChanged := len(toolAppliedOps)
	var appliedOps []appBuilderOperation
	appliedOps = append(appliedOps, toolAppliedOps...)

	if filesChanged > 0 {
		sendEvent("progress", responses.ProgressEvent{
			Phase:   "auto_fix",
			Message: "Running quality checks...",
		})
		c.runDeterministicAutoFixes(project, fileMap, sendEvent, &appliedOps, &filesChanged)

		// Augment package.json with discovered dependencies
		pkgChanged := false
		if pkgContent, ok := fileMap["package.json"]; ok {
			augmented := augmentPackageJSON(pkgContent, fileMap)
			if augmented != pkgContent {
				pkgChanged = true
				dbFile, err := c.DB.GetAppGeneratedFileByPath(project.ID.String(), "nextjs", "package.json")
				if err == nil && dbFile != nil {
					dbFile.Content = augmented
					if _, updateErr := c.DB.UpdateAppGeneratedFile(dbFile); updateErr != nil {
						slog.Error("failed to persist augmented package.json", "error", updateErr, "project_id", project.ID)
					}
					fileMap["package.json"] = augmented
					slog.Info("Augmented package.json with discovered deps", "project_id", project.ID)
					sendEvent("file_complete", responses.FileCompleteEvent{
						Action: "modify", FilePath: "package.json",
						FileID: dbFile.ID, FileType: "json", Content: augmented,
					})
				}
			}
		}

		// Signal that dependencies are finalized
		sendEvent("deps_ready", map[string]any{
			"package_json_changed": pkgChanged,
		})
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

	// --- Generation completed successfully ---
	generationSucceeded = true

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
		Content:  content,
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
	allowProtected ...bool,
) {
	// Sanitize file path to prevent directory traversal from LLM output.
	cleanPath, valid := utils.SanitizeFilePath(path)
	if !valid {
		slog.Warn("saveGeneratedFile: rejecting suspicious file path", "path", path)
		return
	}
	path = cleanPath

	// Guard: never let the LLM overwrite template-managed files during initial generation.
	// Chat edits pass allowProtected=true so the AI can fix errors in template files.
	// Still emit file_complete with the DB content so the sandbox stays in sync.
	canOverride := len(allowProtected) > 0 && allowProtected[0]
	if !canOverride && isProtectedTemplateFile(path) {
		slog.Warn("saveGeneratedFile: skipping protected template file", "path", path)
		if existingFile, err := c.DB.GetAppGeneratedFileByPath(project.ID.String(), "nextjs", path); err == nil && existingFile != nil {
			sendEvent("file_complete", responses.FileCompleteEvent{
				Action: "modify", FilePath: path,
				FileID: existingFile.ID, FileType: fileType, Content: existingFile.Content,
			})
		}
		return
	}

	// Guard: block seed routes — the platform handles seeding, not user-facing API routes.
	if strings.HasPrefix(path, "app/api/seed") || strings.HasPrefix(path, "app/api/migrate") {
		slog.Warn("saveGeneratedFile: blocking seed/migrate route", "path", path)
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
			// Content unchanged in DB — still emit file_complete so the sandbox
			// gets re-synced (a previous write may have failed silently).
			sendEvent("file_complete", responses.FileCompleteEvent{
				Action: "modify", FilePath: path,
				FileID: existingFile.ID, FileType: fileType, Content: content,
			})
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

// deleteGeneratedFile removes a file from the project DB and fileMap, emitting events.
func (c *Controller) deleteGeneratedFile(
	project *models.AppProject,
	path string,
	fileMap map[string]string,
	sendEvent func(string, any) bool,
	ops *[]appBuilderOperation,
) {
	cleanPath, valid := utils.SanitizeFilePath(path)
	if !valid {
		slog.Warn("deleteGeneratedFile: rejecting suspicious file path", "path", path)
		return
	}
	path = cleanPath

	existingFile, err := c.DB.GetAppGeneratedFileByPath(project.ID.String(), "nextjs", path)
	if err != nil || existingFile == nil {
		slog.Warn("deleteGeneratedFile: file not found", "path", path)
		return
	}
	if err := c.DB.DeleteAppGeneratedFile(existingFile); err != nil {
		slog.Error("deleteGeneratedFile: failed to delete", "path", path, "error", err)
		return
	}
	delete(fileMap, path)
	sendEvent("file_complete", responses.FileCompleteEvent{
		Action: "delete", FilePath: path,
		FileID: existingFile.ID, FileType: existingFile.FileType,
	})
	*ops = append(*ops, appBuilderOperation{
		Action: "delete", Path: path, Type: existingFile.FileType,
	})
}

// --- Helper: fast-model tool-calling phase ---

// runChatToolPhase runs a quick tool-calling pass with the fast model (Haiku)
// to detect URLs and store-management requests before the main code-gen model.
// Returns additional context to inject into the code-gen messages, or "" if
// no tools were called.
func (c *Controller) runChatToolPhase(
	ctx context.Context,
	project *models.AppProject,
	provider *utils.LLMProvider,
	userMessage string,
	fileMap map[string]string,
	sendEvent func(string, any) bool,
) string {
	fastModel := provider.FastModel()

	// Build tools list — always include these general tools.
	tools := []utils.Tool{
		chatPreviewWebsiteTool,
		chatManageEnvVarsTool,
		chatInstallPackageTool,
		chatGenerateImageTool,
		chatTakeScreenshotTool,
		chatSearchComponentsTool,
	}
	isEcommerce := project.ProjectType == "ecommerce"
	if isEcommerce {
		tools = append(tools, chatManageStoreTool)
	}
	// Add database tools if the project has a provisioned database
	hasDB := c.neonConnString(project.ID.String()) != ""
	if hasDB {
		tools = append(tools, chatSeedDatabaseTool, chatExecuteSQLTool, chatRunMigrationsTool)
	}
	if ws := provider.WebSearchTool(); ws != nil {
		tools = append(tools, *ws)
	}

	systemPrompt := `You are a pre-processing assistant. Analyze the user's message and decide if any tools should be called BEFORE the main code-generation model runs.
- If the user provides a URL or asks to convert/clone/replicate a website, call preview_website (and optionally take_screenshot to capture a visual reference).
- If the user asks about current technologies, APIs, documentation, pricing, latest versions, or anything that benefits from up-to-date web information, use web_search to find accurate information.
- If the user asks to set, add, or remove environment variables / API keys / secrets, call manage_env_vars.
- If the user asks to install or add a specific npm package/library, call install_package.
- If the user asks for a custom image, visual asset, hero background, or illustration, call generate_image to create it. If the app has a database with a relevant image column (e.g. cover_image, thumbnail), generate the image and UPDATE the database row with the URL. If there is no database or no suitable column, generate the image and pass the URL to the code-gen model to use directly in code.
- If the user references a website and you want to see what it looks like visually, call take_screenshot with the URL.
- If the user asks for a specific UI component (dialog, tabs, accordion, toast, dropdown, etc.) that may not be in the project yet, call search_components to find and inject it.`
	if hasDB {
		// Include database schema so the LLM knows exact table/column names and types
		dataModels, _ := parseDataModels(project.DataModels)
		if len(dataModels) > 0 {
			systemPrompt += "\n\n### Database Schema\n"
			for _, dm := range dataModels {
				systemPrompt += fmt.Sprintf("Table: %s\n  Columns: ", dm.Name)
				for i, col := range dm.Columns {
					if i > 0 {
						systemPrompt += ", "
					}
					systemPrompt += fmt.Sprintf("%s (%s)", col.Name, col.Type)
				}
				systemPrompt += "\n"
			}
			systemPrompt += "All tables have an auto-generated UUID 'id' column and auto-generated created_at/updated_at timestamps — do NOT provide id, created_at, or updated_at in INSERT statements.\n"
		} else {
			systemPrompt += "\n\n### Database Schema\nSchema metadata is unavailable. Before writing data, first discover the schema by running: SELECT table_name FROM information_schema.tables WHERE table_schema = 'public' AND table_type = 'BASE TABLE', then for each table: SELECT column_name, data_type, is_nullable, column_default FROM information_schema.columns WHERE table_name = '<table>' ORDER BY ordinal_position. All tables have auto-generated UUID 'id' and timestamp created_at/updated_at columns — do NOT provide them in INSERT statements.\n"
		}
		systemPrompt += `
- If the user says the database is empty, has no data, asks for demo/sample/test data, or complains about missing content, call seed_database to populate it with realistic data.
- If you need to check whether the database has data, call execute_sql with a SELECT/COUNT query (e.g. SELECT COUNT(*) FROM table_name).
- If the user asks to add, insert, or create specific data rows, call execute_sql with an INSERT query. You can insert multiple rows in a single INSERT using VALUES (...), (...), (...) syntax.
- If the user asks to update, modify, or change existing data, call execute_sql with an UPDATE query.
- DELETE is NOT allowed. If the user wants to remove data, use UPDATE to set a status flag (e.g. UPDATE articles SET is_active = false WHERE ...).
- If the user asks to generate/create images for database records (e.g. "add images to articles", "generate cover images"), use this workflow: first call execute_sql to SELECT the rows that need images, then for each row call generate_image with a descriptive prompt, then call execute_sql with an UPDATE to set the image URL on that row. Chain these tool calls sequentially.
- If the user asks to add/remove/rename database columns or create new tables, call run_migrations with the ALTER TABLE / CREATE TABLE SQL and the complete updated data model.
- For timestamps, use NOW(). For UUIDs, use gen_random_uuid(). Do NOT manually provide id values.`
	}
	if isEcommerce {
		systemPrompt += `
- If the user asks to add/create/update/delete products, categories, or discounts, call manage_store with the appropriate operations.
  - Products need: name (required), price (in cents, e.g. 2999 for $29.99), description, images (URLs), category (slug), tags, status (active/draft/archived), sku, inventory.
  - Categories need: name (required), description, image (URL).
  - Discounts need: code (required), type (percentage/fixed), value (percentage 0-100 or cents), min_order_cents, max_uses, starts_at/ends_at (RFC3339).
  - For updates, include id or slug or name to identify the item, plus the fields to change.
  - For deletes, include id or slug or name.
  - Use bulk_create_products for multiple products at once (pass "products" array in data).
  - Use list_products or list_categories to see current store data.`
	}
	systemPrompt += `
- If the user's message is a normal chat request (code changes, questions, fixes) that doesn't need any of the above tools, respond with just: NO_TOOLS_NEEDED
- You can call multiple tools if needed (e.g. generate_image + install_package).
- IMPORTANT: Before calling any tool, ALWAYS write a brief message (1-2 sentences, in the same language as the user's message) explaining what you're about to do. For example: "Let me check what's currently in the database...", "I'll generate a custom image for this...", "Looking at the website to understand the design...". This text is streamed to the user in real time so they know what's happening. Never call a tool without explaining first.`

	messages := []utils.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userMessage},
	}

	var collectedContext strings.Builder

	executor := func(name string, arguments string) (string, error) {
		switch name {
		case "preview_website":
			return c.executeChatPreviewWebsite(arguments, project, sendEvent, &collectedContext)
		case "manage_store":
			return c.executeChatManageStore(arguments, project, sendEvent, &collectedContext)
		case "seed_database":
			return c.executeChatSeedDatabase(ctx, project, sendEvent, &collectedContext)
		case "execute_sql":
			return c.executeChatExecuteSQL(ctx, arguments, project, &collectedContext)
		case "run_migrations":
			return c.executeChatRunMigrations(ctx, arguments, project, fileMap, sendEvent, &collectedContext)
		case "manage_env_vars":
			return c.executeChatManageEnvVars(arguments, project, fileMap, sendEvent, &collectedContext)
		case "install_package":
			return c.executeChatInstallPackage(arguments, project, fileMap, sendEvent, &collectedContext)
		case "generate_image":
			return c.executeChatGenerateImage(ctx, arguments, project, sendEvent, &collectedContext)
		case "take_screenshot":
			return c.executeChatTakeScreenshot(arguments, &collectedContext)
		case "search_components":
			return c.executeChatSearchComponents(arguments, project, fileMap, sendEvent, &collectedContext)
		default:
			return fmt.Sprintf("Unknown tool: %s", name), nil
		}
	}

	result, err := utils.StreamWithTools(
		ctx, fastModel, messages,
		tools, provider,
		nil, nil, // no response format, no reasoning
		executor,
		nil, // onToolCall — handled in executors
		func() func(string) {
			// Buffer initial chunks to suppress "NO_TOOLS_NEEDED" responses.
			// Once we see a real sentence we flush the buffer and stream directly.
			var buf strings.Builder
			flushed := false
			return func(chunk string) {
				cleaned := ansiRegex.ReplaceAllString(chunk, "")
				if cleaned == "" {
					return
				}
				if flushed {
					sendEvent("chat_text", responses.ChatTextEvent{Chunk: cleaned})
					return
				}
				buf.WriteString(cleaned)
				s := buf.String()
				if strings.Contains(s, "NO_TOOLS_NEEDED") {
					// Discard — no tools will be called
					return
				}
				// Flush once we have enough text to know it's a real message
				if len(s) > 20 {
					flushed = true
					sendEvent("chat_text", responses.ChatTextEvent{Chunk: s})
				}
			}
		}(),
		nil, // onReasoning — not needed
		utils.ToolCallConfig{
			MaxRounds:  30,
			MaxTokens:  4096,
			StripAfter: 28, // Strip tools near the end so model is forced to respond
			OnToolCallStart: func(toolName string) {
				sendEvent("tool_start", map[string]string{"tool": toolName})
			},
		},
	)

	if err != nil {
		slog.Warn("Chat tool phase hit limit, preserving any collected context", "error", err, "context_len", collectedContext.Len())
		// Don't discard collected context from successful tool calls (e.g. seed_database)
		// just because the model kept calling tools past MaxRounds.
		if collectedContext.Len() > 0 {
			return collectedContext.String()
		}
		return ""
	}

	if collectedContext.Len() == 0 {
		trimmed := strings.TrimSpace(result)
		if trimmed == "" || trimmed == "NO_TOOLS_NEEDED" {
			slog.Info("Chat tool phase: no tools called", "result_preview", truncate(result, 80))
			return ""
		}
		// Server-side tool (e.g. web_search) produced a useful response
		return "Web search context:\n" + trimmed
	}

	return collectedContext.String()
}

// preGenContext holds the results of the pre-generation web search + screenshot phase.
type preGenContext struct {
	Text       string   // Text context (web search results, screenshot metadata)
	Screenshots []string // Base64 data URIs of captured screenshots (for vision)
}

func (p preGenContext) HasContent() bool {
	return p.Text != "" || len(p.Screenshots) > 0
}

// runPreGenWebSearch runs a web-search + screenshot tool phase before initial generation.
// Unlike runChatToolPhase (which also offers preview_website and manage_store),
// this only exposes web_search and take_screenshot so the fast model can research
// referenced websites visually and textually.
// Returns text context + screenshot images for downstream vision-capable models.
func (c *Controller) runPreGenWebSearch(
	ctx context.Context,
	provider *utils.LLMProvider,
	userMessage string,
	sendEvent func(string, any) bool,
) preGenContext {
	fastModel := provider.FastModel()
	tools := []utils.Tool{chatTakeScreenshotTool}
	if ws := provider.WebSearchTool(); ws != nil {
		tools = append(tools, *ws)
	}

	// If no web search and no screenshot API key, skip entirely
	screenshotAvailable := c.SecretManager.GetSecret("SITESHOT_API_KEY") != ""
	if len(tools) == 1 && !screenshotAvailable {
		return preGenContext{}
	}

	systemPrompt := `You are a research assistant. Analyze the user's prompt and decide if web search or screenshots would help gather context for building their website/app.

Search and/or take screenshots when the user:
- References an existing website or domain name (e.g. "shiftdelete.net", "airbnb clone") — search to understand what that site is, AND take a screenshot to see its actual visual design (colors, layout, typography)
- Mentions specific technologies, APIs, or services that may need current documentation
- Asks for something that benefits from up-to-date information (pricing, trends, latest versions)
- Wants to clone, replicate, or be inspired by a real website or product

IMPORTANT: When a user references a real website, ALWAYS call take_screenshot with the website URL. Web search results alone cannot reliably tell you the exact colors, layout, or visual style of a website. The screenshot is essential for accurate color matching and design replication.

Do NOT search/screenshot when:
- The user describes a generic app/website from scratch without referencing real sites
- The prompt is self-contained and doesn't need external context

When researching a website, do both:
1. Use web_search to understand what the site is, its features, and content
2. Use take_screenshot to capture the actual visual design (colors, layout, typography)

Be thorough — the results will be used to generate an accurate replica/revision.
If no search or screenshot is needed, respond with just: NO_SEARCH_NEEDED`

	messages := []utils.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userMessage},
	}

	var collectedContext strings.Builder
	var screenshots []string

	executor := func(name string, arguments string) (string, error) {
		switch name {
		case "take_screenshot":
			result, err := c.executePreGenScreenshot(arguments, &collectedContext, &screenshots)
			return result, err
		default:
			return "", fmt.Errorf("unexpected client tool call: %s", name)
		}
	}

	result, err := utils.StreamWithTools(
		ctx, fastModel, messages,
		tools, provider,
		nil, nil, // no response format, no reasoning
		executor,
		nil, // onToolCall
		nil, // onChunk — discard fast model text
		nil, // onReasoning
		utils.ToolCallConfig{
			MaxRounds: 5,
			MaxTokens: 2048,
			OnToolCallStart: func(toolName string) {
				sendEvent("tool_start", map[string]string{"tool": toolName})
			},
		},
	)

	if err != nil {
		slog.Warn("Pre-gen web search phase failed, continuing without context", "error", err)
		if collectedContext.Len() > 0 {
			return preGenContext{Text: collectedContext.String(), Screenshots: screenshots}
		}
		return preGenContext{}
	}

	// Combine collected context (from take_screenshot) with web search text
	trimmed := strings.TrimSpace(result)
	if collectedContext.Len() == 0 {
		if trimmed == "" || trimmed == "NO_SEARCH_NEEDED" {
			return preGenContext{}
		}
		return preGenContext{Text: "Web search context:\n" + trimmed, Screenshots: screenshots}
	}

	// We have screenshot context — append web search text if any
	if trimmed != "" && trimmed != "NO_SEARCH_NEEDED" {
		collectedContext.WriteString("\nWeb search context:\n" + trimmed)
	}
	return preGenContext{Text: collectedContext.String(), Screenshots: screenshots}
}

// executePreGenScreenshot captures a screenshot and stores the base64 data URI
// both in collectedContext (text) and screenshots slice (for vision).
func (c *Controller) executePreGenScreenshot(
	arguments string,
	collectedContext *strings.Builder,
	screenshots *[]string,
) (string, error) {
	var args struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return fmt.Sprintf("Error: invalid arguments: %s", err.Error()), nil
	}

	targetURL := strings.TrimSpace(args.URL)
	if targetURL == "" {
		return "Error: url is required", nil
	}
	if !strings.HasPrefix(targetURL, "http://") && !strings.HasPrefix(targetURL, "https://") {
		targetURL = "https://" + targetURL
	}

	apiKey := c.SecretManager.GetSecret("SITESHOT_API_KEY")
	if apiKey == "" {
		return "Error: screenshot service not configured", nil
	}

	imgData, err := external.GetDesktopScreenshotOfURL(targetURL, apiKey)
	if err != nil {
		return fmt.Sprintf("Screenshot capture failed: %s", err.Error()), nil
	}
	if len(imgData) == 0 {
		return "Screenshot capture returned empty image", nil
	}

	b64 := base64.StdEncoding.EncodeToString(imgData)
	dataURI := "data:image/png;base64," + b64

	// Store base64 data URI for vision-capable models
	*screenshots = append(*screenshots, dataURI)

	// Store text metadata (without the huge base64 blob) for text-only context
	collectedContext.WriteString(fmt.Sprintf("\n[Screenshot Captured]\nURL: %s\nA desktop screenshot (1280x800) has been captured and will be provided as an image to the code generation model for accurate color/layout matching.\n", targetURL))
	slog.Info("take_screenshot tool called", "url", targetURL, "size_bytes", len(imgData))
	return "Screenshot captured successfully. The image will be provided to the code generation model.", nil
}

// executeChatPreviewWebsite handles the preview_website tool call in the chat tool phase.
func (c *Controller) executeChatPreviewWebsite(
	arguments string,
	project *models.AppProject,
	sendEvent func(string, any) bool,
	collectedContext *strings.Builder,
) (string, error) {
	var args struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "Error: invalid arguments", nil
	}
	targetURL := strings.TrimSpace(args.URL)
	if targetURL == "" {
		return "Error: url is required", nil
	}
	if !strings.HasPrefix(targetURL, "http://") && !strings.HasPrefix(targetURL, "https://") {
		targetURL = "https://" + targetURL
	}
	proxyURL := fmt.Sprintf("https://proxy.wemob.io/?url=%s", targetURL)

	sendEvent("website_preview", responses.WebsitePreviewEvent{
		URL:      targetURL,
		ProxyURL: proxyURL,
	})
	sendEvent("tool_call", responses.ToolCallEvent{
		Tool:      "preview_website",
		Arguments: arguments,
	})

	// Fetch HTML to extract structure for the code-gen model
	html, fetchErr := utils.FetchHTML(
		targetURL,
		c.SecretManager.GetSecret("PROXY_HOST"),
		c.SecretManager.GetSecret("PROXY_AUTH"),
	)

	var siteInfo string
	if fetchErr != nil {
		slog.Warn("Failed to fetch HTML for preview_website", "url", targetURL, "error", fetchErr)
		siteInfo = fmt.Sprintf("Website URL: %s\n(Could not fetch page content — generate based on the URL and common patterns for this type of site.)", targetURL)
	} else {
		title := extractHTMLTitle(html)
		description := extractHTMLMeta(html, "description")
		navURLs := utils.ExtractAllURLs(html, targetURL)
		if len(navURLs) > 20 {
			navURLs = navURLs[:20]
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Website URL: %s\n", targetURL))
		if title != "" {
			sb.WriteString(fmt.Sprintf("Title: %s\n", title))
		}
		if description != "" {
			sb.WriteString(fmt.Sprintf("Description: %s\n", description))
		}
		if len(navURLs) > 0 {
			sb.WriteString(fmt.Sprintf("Navigation links (%d): %s\n", len(navURLs), strings.Join(navURLs, ", ")))
		}
		siteInfo = sb.String()
	}

	collectedContext.WriteString(fmt.Sprintf("\n[Website Analysis]\n%s\nThe website is now visible in the user's preview panel. Generate or modify the app to match this website's design and content.\n", siteInfo))
	slog.Info("preview_website tool called via fast model", "url", targetURL, "project_id", project.ID)
	return "Website preview loaded.", nil
}

// executeChatManageStore handles the manage_store tool call in the chat tool phase.
func (c *Controller) executeChatManageStore(
	arguments string,
	project *models.AppProject,
	sendEvent func(string, any) bool,
	collectedContext *strings.Builder,
) (string, error) {
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
		return "Error: No store found for this project. The project must be set up as an e-commerce app first.", nil
	}

	sendEvent("tool_call", responses.ToolCallEvent{
		Tool:      "manage_store",
		Arguments: arguments,
	})

	var results []string
	var errCount int
	for _, op := range args.Operations {
		opData, opErr := c.executeStoreOperation(store, op.Action, op.Data)
		if opErr != nil {
			results = append(results, fmt.Sprintf("FAILED %s: %s", op.Action, opErr.Error()))
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

	summary := fmt.Sprintf("Executed %d operations (%d succeeded, %d failed):\n%s",
		len(args.Operations), len(args.Operations)-errCount, errCount, strings.Join(results, "\n"))

	collectedContext.WriteString(fmt.Sprintf("\n[Store Update]\n%s\n", summary))
	slog.Info("manage_store tool called via fast model",
		"project_id", project.ID,
		"operations", len(args.Operations),
		"succeeded", len(args.Operations)-errCount,
		"failed", errCount,
	)
	return summary, nil
}

// executeChatSeedDatabase handles the seed_database tool call in the chat tool phase.
func (c *Controller) executeChatSeedDatabase(
	ctx context.Context,
	project *models.AppProject,
	sendEvent func(string, any) bool,
	collectedContext *strings.Builder,
) (string, error) {
	dataModels, err := parseDataModels(project.DataModels)
	if err != nil {
		return fmt.Sprintf("Error: failed to parse data models: %s", err.Error()), nil
	}
	if len(dataModels) == 0 {
		return "Error: No database schema found. The database must be set up first.", nil
	}

	sendEvent("tool_call", responses.ToolCallEvent{
		Tool:      "seed_database",
		Arguments: "{}",
	})

	_, seedErr := c.seedNeonDatabase(ctx, project, dataModels, sendEvent)
	if seedErr != nil {
		slog.Error("seed_database tool failed", "error", seedErr, "project_id", project.ID)
		collectedContext.WriteString(fmt.Sprintf("\n[Database Seed Failed]\nSeeding failed with error: %s\nDo NOT create seed scripts, seed API routes, seed buttons, or migration routes as a workaround. Simply inform the user that seeding failed and suggest they try again.\n", seedErr.Error()))
		return fmt.Sprintf("Database seeding failed: %s", seedErr.Error()), nil
	}

	// Generate AI images for seed data image columns
	if project.ProjectType != "ecommerce" {
		c.generateAndStoreSeedImages(ctx, project, dataModels, sendEvent, 10)
	}

	collectedContext.WriteString("\n[Database Seeded]\nThe database has been populated with realistic demo data and AI-generated images. The app should now display this data when querying the database. Do NOT create seed scripts, seed buttons, or seed API routes — the data is already in the database.\n")
	slog.Info("seed_database tool called via fast model", "project_id", project.ID)
	return "Database seeded successfully with realistic demo data.", nil
}

// executeChatExecuteSQL handles the execute_sql tool call in the chat tool phase.
func (c *Controller) executeChatExecuteSQL(
	ctx context.Context,
	arguments string,
	project *models.AppProject,
	collectedContext *strings.Builder,
) (string, error) {
	var args struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return fmt.Sprintf("Error: invalid arguments: %s", err.Error()), nil
	}

	query := strings.TrimSpace(args.Query)
	if query == "" {
		return "Error: query is required", nil
	}

	// Validate allowed statements: SELECT, INSERT, UPDATE, WITH (CTE)
	upper := strings.ToUpper(query)
	isSelect := strings.HasPrefix(upper, "SELECT") || strings.HasPrefix(upper, "WITH")
	isInsert := strings.HasPrefix(upper, "INSERT")
	isUpdate := strings.HasPrefix(upper, "UPDATE")
	if !isSelect && !isInsert && !isUpdate {
		return "Error: only SELECT, INSERT, and UPDATE queries are allowed. DELETE is not permitted — use UPDATE to set a status flag instead.", nil
	}
	// Reject destructive keywords (word-boundary match to avoid false positives
	// like CREATED_AT matching CREATE, or DELETED matching DELETE)
	for _, kw := range []string{"DELETE", "DROP", "ALTER", "CREATE", "TRUNCATE", "GRANT", "REVOKE"} {
		pattern := `\b` + kw + `\b`
		if matched, _ := regexp.MatchString(pattern, upper); matched {
			return fmt.Sprintf("Error: query contains disallowed keyword: %s. Use UPDATE with a status/flag column instead of DELETE.", kw), nil
		}
	}

	neonDB, err := c.openNeonDB(project.ID.String())
	if err != nil {
		return fmt.Sprintf("Error: failed to connect to database: %s", err.Error()), nil
	}
	defer neonDB.Close()

	queryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// For write queries (INSERT/UPDATE), execute and return affected rows
	if isInsert || isUpdate {
		// If INSERT has RETURNING, use QueryContext to capture returned rows
		if isInsert && strings.Contains(upper, "RETURNING") {
			rows, err := neonDB.QueryContext(queryCtx, query)
			if err != nil {
				return fmt.Sprintf("Query error: %s", err.Error()), nil
			}
			defer rows.Close()
			cols, _ := rows.Columns()
			var resultBuf strings.Builder
			resultBuf.WriteString(strings.Join(cols, " | "))
			resultBuf.WriteString("\n")
			rowCount := 0
			for rows.Next() && rowCount < 50 {
				values := make([]any, len(cols))
				valuePtrs := make([]any, len(cols))
				for i := range values {
					valuePtrs[i] = &values[i]
				}
				if err := rows.Scan(valuePtrs...); err != nil {
					continue
				}
				var rowVals []string
				for _, v := range values {
					if v == nil {
						rowVals = append(rowVals, "NULL")
					} else if b, ok := v.([]byte); ok {
						rowVals = append(rowVals, string(b))
					} else {
						rowVals = append(rowVals, fmt.Sprintf("%v", v))
					}
				}
				resultBuf.WriteString(strings.Join(rowVals, " | "))
				resultBuf.WriteString("\n")
				rowCount++
			}
			summary := fmt.Sprintf("OK — %d row(s) inserted:\n%s", rowCount, resultBuf.String())
			collectedContext.WriteString(fmt.Sprintf("\n[Database Write Result]\nQuery: %s\n%s\n", query, summary))
			slog.Info("execute_sql insert+returning via fast model", "project_id", project.ID, "rows", rowCount)
			return summary, nil
		}

		result, err := neonDB.ExecContext(queryCtx, query)
		if err != nil {
			return fmt.Sprintf("Query error: %s", err.Error()), nil
		}
		affected, _ := result.RowsAffected()
		summary := fmt.Sprintf("OK — %d row(s) affected.", affected)
		collectedContext.WriteString(fmt.Sprintf("\n[Database Write Result]\nQuery: %s\n%s\n", query, summary))
		slog.Info("execute_sql write via fast model", "project_id", project.ID, "affected", affected)
		return summary, nil
	}

	// For SELECT queries, use read-only transaction
	tx, err := neonDB.BeginTx(queryCtx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return fmt.Sprintf("Error: failed to begin read-only transaction: %s", err.Error()), nil
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(queryCtx, query)
	if err != nil {
		return fmt.Sprintf("Query error: %s", err.Error()), nil
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return fmt.Sprintf("Error reading columns: %s", err.Error()), nil
	}

	var result strings.Builder
	result.WriteString(strings.Join(cols, " | "))
	result.WriteString("\n")

	rowCount := 0
	for rows.Next() && rowCount < 50 {
		values := make([]any, len(cols))
		valuePtrs := make([]any, len(cols))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			continue
		}
		var rowVals []string
		for _, v := range values {
			if v == nil {
				rowVals = append(rowVals, "NULL")
			} else if b, ok := v.([]byte); ok {
				rowVals = append(rowVals, string(b))
			} else {
				rowVals = append(rowVals, fmt.Sprintf("%v", v))
			}
		}
		result.WriteString(strings.Join(rowVals, " | "))
		result.WriteString("\n")
		rowCount++
	}

	var summary string
	if rowCount == 0 {
		summary = fmt.Sprintf("Query returned 0 rows. Columns: %s", strings.Join(cols, ", "))
	} else {
		summary = fmt.Sprintf("%d row(s):\n%s", rowCount, result.String())
	}

	collectedContext.WriteString(fmt.Sprintf("\n[Database Query Result]\nQuery: %s\n%s\n", query, summary))
	slog.Info("execute_sql tool called via fast model", "project_id", project.ID, "rows", rowCount)
	return summary, nil
}

// executeChatRunMigrations handles schema changes on the app's Neon database.
func (c *Controller) executeChatRunMigrations(
	ctx context.Context,
	arguments string,
	project *models.AppProject,
	fileMap map[string]string,
	sendEvent func(string, any) bool,
	collectedContext *strings.Builder,
) (string, error) {
	var args struct {
		SQL           string      `json:"sql"`
		UpdatedModels []DataModel `json:"updated_models"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return fmt.Sprintf("Error: invalid arguments: %s", err.Error()), nil
	}
	if strings.TrimSpace(args.SQL) == "" {
		return "Error: sql is required", nil
	}
	if len(args.UpdatedModels) == 0 {
		return "Error: updated_models is required", nil
	}

	// Validate SQL — only allow DDL statements
	upper := strings.ToUpper(strings.TrimSpace(args.SQL))
	for _, kw := range []string{"INSERT", "UPDATE", "DELETE", "SELECT", "TRUNCATE", "GRANT", "REVOKE"} {
		if strings.HasPrefix(upper, kw) {
			return fmt.Sprintf("Error: only DDL statements (ALTER TABLE, CREATE TABLE) are allowed, not %s", kw), nil
		}
	}

	sendEvent("tool_call", responses.ToolCallEvent{Tool: "run_migrations", Arguments: arguments})

	neonDB, err := c.openNeonDB(project.ID.String())
	if err != nil {
		return fmt.Sprintf("Error: failed to connect to database: %s", err.Error()), nil
	}
	defer neonDB.Close()

	queryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if _, err := neonDB.ExecContext(queryCtx, args.SQL); err != nil {
		return fmt.Sprintf("Migration failed: %s", err.Error()), nil
	}

	// Update stored data models
	dmJSON, err := json.Marshal(args.UpdatedModels)
	if err != nil {
		return fmt.Sprintf("Migration applied but failed to update models: %s", err.Error()), nil
	}
	project.DataModels = dmJSON
	if _, err := c.DB.UpdateAppProject(project); err != nil {
		slog.Error("failed to save updated data models", "error", err, "project_id", project.ID)
	}

	// Regenerate lib/schema.ts
	schemaContent := buildDrizzleSchema(args.UpdatedModels)
	c.injectTemplateFile(project, "lib/schema.ts", schemaContent, fileMap, sendEvent)

	collectedContext.WriteString("\n[Database Migrated]\nSchema changes applied successfully. lib/schema.ts has been regenerated. Update any code that uses the affected tables.\n")
	slog.Info("run_migrations tool called", "project_id", project.ID)
	return "Migration applied successfully. Schema updated.", nil
}

// executeChatManageEnvVars handles adding, updating, and deleting environment variables.
func (c *Controller) executeChatManageEnvVars(
	arguments string,
	project *models.AppProject,
	fileMap map[string]string,
	sendEvent func(string, any) bool,
	collectedContext *strings.Builder,
) (string, error) {
	var args struct {
		Operations []struct {
			Action   string `json:"action"`
			Key      string `json:"key"`
			Value    string `json:"value"`
			IsPublic bool   `json:"is_public"`
		} `json:"operations"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return fmt.Sprintf("Error: invalid arguments: %s", err.Error()), nil
	}

	sendEvent("tool_call", responses.ToolCallEvent{Tool: "manage_env_vars", Arguments: arguments})

	var results []string
	for _, op := range args.Operations {
		switch op.Action {
		case "set":
			existing, err := c.DB.GetAppEnvVarByKey(project.ID.String(), op.Key)
			if err == nil && existing.ID != uuid.Nil {
				existing.Value = op.Value
				existing.IsPublic = op.IsPublic
				existing.UpdatedAt = time.Now()
				if _, err := c.DB.UpdateAppEnvVar(existing); err != nil {
					results = append(results, fmt.Sprintf("FAILED set %s: %s", op.Key, err.Error()))
					continue
				}
			} else {
				envVar := &models.AppEnvVar{
					AppProjectID: project.ID,
					Key:          op.Key,
					Value:        op.Value,
					IsPublic:     op.IsPublic,
				}
				if _, err := c.DB.CreateAppEnvVar(envVar); err != nil {
					results = append(results, fmt.Sprintf("FAILED set %s: %s", op.Key, err.Error()))
					continue
				}
			}
			results = append(results, fmt.Sprintf("OK set %s", op.Key))

		case "delete":
			existing, err := c.DB.GetAppEnvVarByKey(project.ID.String(), op.Key)
			if err != nil || existing.ID == uuid.Nil {
				results = append(results, fmt.Sprintf("SKIPPED delete %s: not found", op.Key))
				continue
			}
			if err := c.DB.DeleteAppEnvVar(existing); err != nil {
				results = append(results, fmt.Sprintf("FAILED delete %s: %s", op.Key, err.Error()))
				continue
			}
			results = append(results, fmt.Sprintf("OK delete %s", op.Key))
		}
	}

	// Regenerate .env.local
	envVars := c.CollectAllEnvVars(project)
	if dotEnv := BuildDotEnvLocal(envVars); dotEnv != "" {
		c.injectTemplateFile(project, ".env.local", dotEnv, fileMap, sendEvent)
	}

	summary := strings.Join(results, "\n")
	collectedContext.WriteString(fmt.Sprintf("\n[Environment Variables Updated]\n%s\n.env.local has been regenerated.\n", summary))
	slog.Info("manage_env_vars tool called", "project_id", project.ID, "operations", len(args.Operations))
	return summary, nil
}

// executeChatInstallPackage adds npm packages to package.json.
func (c *Controller) executeChatInstallPackage(
	arguments string,
	project *models.AppProject,
	fileMap map[string]string,
	sendEvent func(string, any) bool,
	collectedContext *strings.Builder,
) (string, error) {
	var args struct {
		Packages []string `json:"packages"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return fmt.Sprintf("Error: invalid arguments: %s", err.Error()), nil
	}
	if len(args.Packages) == 0 {
		return "Error: packages list is empty", nil
	}

	sendEvent("tool_call", responses.ToolCallEvent{Tool: "install_package", Arguments: arguments})

	pkgContent, ok := fileMap["package.json"]
	if !ok {
		return "Error: package.json not found in project", nil
	}

	var pkgJSON map[string]any
	if err := json.Unmarshal([]byte(pkgContent), &pkgJSON); err != nil {
		return fmt.Sprintf("Error: failed to parse package.json: %s", err.Error()), nil
	}

	deps, ok := pkgJSON["dependencies"].(map[string]any)
	if !ok {
		deps = make(map[string]any)
		pkgJSON["dependencies"] = deps
	}

	var added []string
	var skipped []string
	for _, pkg := range args.Packages {
		pkg = strings.TrimSpace(pkg)
		if pkg == "" {
			continue
		}
		if _, exists := deps[pkg]; exists {
			skipped = append(skipped, pkg)
			continue
		}
		version := packageVersionLookup(pkg)
		deps[pkg] = version
		added = append(added, fmt.Sprintf("%s@%s", pkg, version))
	}

	if len(added) == 0 {
		return fmt.Sprintf("All packages already installed: %s", strings.Join(skipped, ", ")), nil
	}

	updatedJSON, err := json.MarshalIndent(pkgJSON, "", "  ")
	if err != nil {
		return fmt.Sprintf("Error: failed to serialize package.json: %s", err.Error()), nil
	}

	dbFile, err := c.DB.GetAppGeneratedFileByPath(project.ID.String(), "nextjs", "package.json")
	if err == nil && dbFile != nil {
		dbFile.Content = string(updatedJSON)
		if _, err := c.DB.UpdateAppGeneratedFile(dbFile); err != nil {
			slog.Error("failed to update package.json", "error", err, "project_id", project.ID)
		}
		fileMap["package.json"] = string(updatedJSON)
		sendEvent("file_complete", responses.FileCompleteEvent{
			Action: "modify", FilePath: "package.json",
			FileID: dbFile.ID, FileType: "json", Content: string(updatedJSON),
		})
	}

	sendEvent("deps_ready", map[string]any{"package_json_changed": true})

	summary := fmt.Sprintf("Installed: %s", strings.Join(added, ", "))
	if len(skipped) > 0 {
		summary += fmt.Sprintf("\nAlready installed: %s", strings.Join(skipped, ", "))
	}
	collectedContext.WriteString(fmt.Sprintf("\n[Packages Installed]\n%s\n", summary))
	slog.Info("install_package tool called", "project_id", project.ID, "added", len(added))
	return summary, nil
}

// packageVersionLookup returns a version string for common npm packages.
func packageVersionLookup(pkg string) string {
	versions := map[string]string{
		"zustand": "^4.5.0", "axios": "^1.7.0", "date-fns": "^3.6.0",
		"zod": "^3.23.0", "react-hook-form": "^7.51.0", "@hookform/resolvers": "^3.3.0",
		"recharts": "^2.12.0", "react-icons": "^5.0.0", "swiper": "^11.0.0",
		"@tanstack/react-query": "^5.0.0", "@tanstack/react-table": "^8.0.0",
		"sonner": "^1.0.0", "next-themes": "^0.3.0", "vaul": "^0.9.0",
		"cmdk": "^0.2.0", "swr": "^2.2.0", "stripe": "^14.0.0",
		"@stripe/stripe-js": "^2.0.0", "@stripe/react-stripe-js": "^2.0.0",
		"resend": "^3.0.0", "react-markdown": "^9.0.0", "sharp": "^0.33.0",
		"drizzle-orm": "^0.39.0", "@neondatabase/serverless": "^0.10.0",
		"jose": "^5.9.0", "bcryptjs": "^2.4.3", "uuid": "^9.0.0",
		"lodash": "^4.17.0", "framer-motion": "^11.0.0",
		"class-variance-authority": "^0.7.0", "tailwind-merge": "^2.2.0",
	}
	if strings.HasPrefix(pkg, "@radix-ui/react-") {
		return "^1.1.0"
	}
	if v, ok := versions[pkg]; ok {
		return v
	}
	return "latest"
}

// executeChatGenerateImage generates an AI image via fal.ai and uploads to S3.
func (c *Controller) executeChatGenerateImage(
	ctx context.Context,
	arguments string,
	project *models.AppProject,
	sendEvent func(string, any) bool,
	collectedContext *strings.Builder,
) (string, error) {
	var args struct {
		Prompt   string `json:"prompt"`
		Filename string `json:"filename"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return fmt.Sprintf("Error: invalid arguments: %s", err.Error()), nil
	}

	apiKey := c.SecretManager.GetSecret("FAL_API_KEY")
	if apiKey == "" {
		return "Error: image generation service not configured (FAL_API_KEY missing)", nil
	}

	falModel := c.SecretManager.GetSecret("FAL_IMAGE_MODEL")
	if falModel == "" {
		falModel = "fal-ai/flux/schnell"
	}

	sendEvent("tool_call", responses.ToolCallEvent{Tool: "generate_image", Arguments: arguments})
	sendEvent("progress", responses.ProgressEvent{
		Phase:   "generating_image",
		Message: "Generating image...",
	})

	cdnURL, err := fal.GenerateImage(ctx, apiKey, falModel, args.Prompt)
	if err != nil {
		return fmt.Sprintf("Image generation failed: %s", err.Error()), nil
	}

	imgData, err := downloadImage(ctx, cdnURL)
	if err != nil {
		collectedContext.WriteString(fmt.Sprintf("\n[Generated Image]\nImage URL (temporary CDN): %s\nUse this URL in code via next/image.\n", cdnURL))
		return fmt.Sprintf("Image generated (CDN URL): %s", cdnURL), nil
	}

	filename := strings.TrimSpace(args.Filename)
	if filename == "" {
		filename = "generated-image.jpg"
	}
	s3Key := fmt.Sprintf("apps/%s/images/%s", project.ID, filename)
	imageURL, err := c.AssetsStorage.UploadFileBytePublic(ctx, s3Key, imgData, "image/jpeg")
	if err != nil {
		collectedContext.WriteString(fmt.Sprintf("\n[Generated Image]\nImage URL (temporary CDN): %s\n", cdnURL))
		return fmt.Sprintf("Image generated but S3 upload failed. CDN URL: %s", cdnURL), nil
	}
	collectedContext.WriteString(fmt.Sprintf("\n[Generated Image]\nImage URL: %s\nUse this URL in code via next/image or as a background. The image shows: %s\n", imageURL, args.Prompt))
	slog.Info("generate_image tool called", "project_id", project.ID, "s3_key", s3Key)
	return fmt.Sprintf("Image generated and uploaded: %s", imageURL), nil
}

// executeChatTakeScreenshot captures a screenshot of a URL for visual context.
func (c *Controller) executeChatTakeScreenshot(
	arguments string,
	collectedContext *strings.Builder,
) (string, error) {
	var args struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return fmt.Sprintf("Error: invalid arguments: %s", err.Error()), nil
	}

	targetURL := strings.TrimSpace(args.URL)
	if targetURL == "" {
		return "Error: url is required", nil
	}
	if !strings.HasPrefix(targetURL, "http://") && !strings.HasPrefix(targetURL, "https://") {
		targetURL = "https://" + targetURL
	}

	apiKey := c.SecretManager.GetSecret("SITESHOT_API_KEY")
	if apiKey == "" {
		return "Error: screenshot service not configured (SITESHOT_API_KEY missing)", nil
	}

	imgData, err := external.GetDesktopScreenshotOfURL(targetURL, apiKey)
	if err != nil {
		return fmt.Sprintf("Screenshot capture failed: %s", err.Error()), nil
	}
	if len(imgData) == 0 {
		return "Screenshot capture returned empty image", nil
	}

	b64 := base64.StdEncoding.EncodeToString(imgData)
	collectedContext.WriteString(fmt.Sprintf("\n[Screenshot Captured]\nURL: %s\nScreenshot captured (1280x800 desktop). The code-gen model can use this as visual reference.\n![screenshot](data:image/png;base64,%s)\n", targetURL, b64))
	slog.Info("take_screenshot tool called", "url", targetURL, "size_bytes", len(imgData))
	return "Screenshot captured successfully.", nil
}

// executeChatSearchComponents finds and injects pre-built UI components.
func (c *Controller) executeChatSearchComponents(
	arguments string,
	project *models.AppProject,
	fileMap map[string]string,
	sendEvent func(string, any) bool,
	collectedContext *strings.Builder,
) (string, error) {
	var args struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return fmt.Sprintf("Error: invalid arguments: %s", err.Error()), nil
	}

	query := strings.ToLower(strings.TrimSpace(args.Query))
	if query == "" {
		return "Error: query is required", nil
	}

	sendEvent("tool_call", responses.ToolCallEvent{Tool: "search_components", Arguments: arguments})

	type componentInfo struct {
		Name     string
		Path     string
		Keywords []string
		HasTemplate bool
	}
	components := []componentInfo{
		// Pre-built templates (already have hardcoded content)
		{Name: "Button", Path: "components/ui/button.tsx", Keywords: []string{"button", "btn"}, HasTemplate: true},
		{Name: "Card", Path: "components/ui/card.tsx", Keywords: []string{"card", "panel"}, HasTemplate: true},
		{Name: "Input", Path: "components/ui/input.tsx", Keywords: []string{"input", "text field"}, HasTemplate: true},
		{Name: "Badge", Path: "components/ui/badge.tsx", Keywords: []string{"badge", "tag", "chip"}, HasTemplate: true},
		{Name: "Skeleton", Path: "components/ui/skeleton.tsx", Keywords: []string{"skeleton", "loading"}, HasTemplate: true},
		// Generatable by code-gen model (no template, but known shadcn/ui components)
		{Name: "Dialog", Path: "components/ui/dialog.tsx", Keywords: []string{"dialog", "modal", "popup"}},
		{Name: "Sheet", Path: "components/ui/sheet.tsx", Keywords: []string{"sheet", "drawer", "side panel"}},
		{Name: "Tabs", Path: "components/ui/tabs.tsx", Keywords: []string{"tabs", "tab"}},
		{Name: "Accordion", Path: "components/ui/accordion.tsx", Keywords: []string{"accordion", "collapse", "faq"}},
		{Name: "Alert Dialog", Path: "components/ui/alert-dialog.tsx", Keywords: []string{"alert", "confirm"}},
		{Name: "Dropdown Menu", Path: "components/ui/dropdown-menu.tsx", Keywords: []string{"dropdown", "menu"}},
		{Name: "Select", Path: "components/ui/select.tsx", Keywords: []string{"select", "picker", "combobox"}},
		{Name: "Toast", Path: "components/ui/toast.tsx", Keywords: []string{"toast", "notification", "snackbar"}},
		{Name: "Tooltip", Path: "components/ui/tooltip.tsx", Keywords: []string{"tooltip", "hint"}},
		{Name: "Popover", Path: "components/ui/popover.tsx", Keywords: []string{"popover", "floating"}},
		{Name: "Checkbox", Path: "components/ui/checkbox.tsx", Keywords: []string{"checkbox", "check"}},
		{Name: "Radio Group", Path: "components/ui/radio-group.tsx", Keywords: []string{"radio"}},
		{Name: "Switch", Path: "components/ui/switch.tsx", Keywords: []string{"switch", "toggle"}},
		{Name: "Slider", Path: "components/ui/slider.tsx", Keywords: []string{"slider", "range"}},
		{Name: "Progress", Path: "components/ui/progress.tsx", Keywords: []string{"progress", "loading bar"}},
		{Name: "Separator", Path: "components/ui/separator.tsx", Keywords: []string{"separator", "divider"}},
		{Name: "Avatar", Path: "components/ui/avatar.tsx", Keywords: []string{"avatar", "profile picture"}},
		{Name: "Table", Path: "components/ui/table.tsx", Keywords: []string{"table", "data table", "grid"}},
		{Name: "Textarea", Path: "components/ui/textarea.tsx", Keywords: []string{"textarea", "multiline"}},
		{Name: "Label", Path: "components/ui/label.tsx", Keywords: []string{"label", "form label"}},
		{Name: "Calendar", Path: "components/ui/calendar.tsx", Keywords: []string{"calendar", "date picker", "date"}},
		{Name: "Carousel", Path: "components/ui/carousel.tsx", Keywords: []string{"carousel", "slideshow", "gallery"}},
		{Name: "Navigation Menu", Path: "components/ui/navigation-menu.tsx", Keywords: []string{"navigation", "nav menu"}},
		{Name: "Scroll Area", Path: "components/ui/scroll-area.tsx", Keywords: []string{"scroll", "overflow"}},
	}

	var matched []componentInfo
	for _, comp := range components {
		for _, kw := range comp.Keywords {
			if strings.Contains(query, kw) || strings.Contains(kw, query) {
				matched = append(matched, comp)
				break
			}
		}
	}

	if len(matched) == 0 {
		return fmt.Sprintf("No components found matching '%s'. The code-gen model can create custom components.", query), nil
	}

	var injected, alreadyInstalled, toGenerate []string
	for _, comp := range matched {
		if _, exists := fileMap[comp.Path]; exists {
			alreadyInstalled = append(alreadyInstalled, comp.Name)
			continue
		}
		if comp.HasTemplate {
			content := getUITemplateContent(comp.Path)
			if content != "" {
				c.injectTemplateFile(project, comp.Path, content, fileMap, sendEvent)
				injected = append(injected, comp.Name)
				continue
			}
		}
		toGenerate = append(toGenerate, comp.Name)
	}

	var result strings.Builder
	if len(injected) > 0 {
		result.WriteString(fmt.Sprintf("Injected: %s\n", strings.Join(injected, ", ")))
	}
	if len(alreadyInstalled) > 0 {
		result.WriteString(fmt.Sprintf("Already in project: %s\n", strings.Join(alreadyInstalled, ", ")))
	}
	if len(toGenerate) > 0 {
		result.WriteString(fmt.Sprintf("Needs code-gen model to create: %s (using @radix-ui primitives + shadcn/ui patterns)\n", strings.Join(toGenerate, ", ")))
	}

	collectedContext.WriteString(fmt.Sprintf("\n[Component Search: %s]\n%s\n", query, result.String()))
	slog.Info("search_components tool called", "project_id", project.ID, "query", query, "matched", len(matched))
	return result.String(), nil
}

// extractHTMLTitle extracts the <title> content from HTML.
func extractHTMLTitle(htmlContent string) string {
	lower := strings.ToLower(htmlContent)
	start := strings.Index(lower, "<title")
	if start < 0 {
		return ""
	}
	// Skip past the opening tag
	closeTag := strings.Index(lower[start:], ">")
	if closeTag < 0 {
		return ""
	}
	contentStart := start + closeTag + 1
	end := strings.Index(lower[contentStart:], "</title>")
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(htmlContent[contentStart : contentStart+end])
}

// extractHTMLMeta extracts a <meta name="..." content="..."> value from HTML.
func extractHTMLMeta(htmlContent string, name string) string {
	lower := strings.ToLower(htmlContent)
	search := fmt.Sprintf(`name="%s"`, name)
	idx := strings.Index(lower, search)
	if idx < 0 {
		search = fmt.Sprintf(`name='%s'`, name)
		idx = strings.Index(lower, search)
	}
	if idx < 0 {
		return ""
	}
	// Find the enclosing <meta ...> tag
	tagStart := strings.LastIndex(lower[:idx], "<meta")
	if tagStart < 0 {
		return ""
	}
	tagEnd := strings.Index(lower[tagStart:], ">")
	if tagEnd < 0 {
		return ""
	}
	tag := htmlContent[tagStart : tagStart+tagEnd+1]
	// Extract content="..."
	contentIdx := strings.Index(strings.ToLower(tag), `content="`)
	if contentIdx < 0 {
		contentIdx = strings.Index(strings.ToLower(tag), `content='`)
	}
	if contentIdx < 0 {
		return ""
	}
	quote := tag[contentIdx+8] // " or '
	valStart := contentIdx + 9
	valEnd := strings.Index(tag[valStart:], string(quote))
	if valEnd < 0 {
		return ""
	}
	return tag[valStart : valStart+valEnd]
}

// truncate returns the first n characters of s, or s if shorter.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
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

// buildChatSystemPrompt assembles the system prompt for the simplified file-block
// chat path. Uses CHAT_INSTRUCTIONS instead of TOOL_INSTRUCTIONS — no tool loop.
func buildChatSystemPrompt(project *models.AppProject) string {
	var prompt string
	if project.ProjectType == "ecommerce" {
		prompt = constants.NEXTJS_ECOMMERCE_COMMON_RULES + constants.NEXTJS_BUILDER_CHAT_INSTRUCTIONS
	} else {
		prompt = constants.NEXTJS_BUILDER_CHAT_SYSTEM_PROMPT + constants.NEXTJS_BUILDER_DATABASE_RULES
	}

	if settings := project.Settings; settings != nil {
		if needsAuth, ok := settings["needs_auth"].(bool); ok && needsAuth {
			prompt += constants.NEXTJS_BUILDER_AUTH_RULES
		}
	}

	if project.ProjectType == "mobile_app" {
		prompt += constants.NEXTJS_BUILDER_MOBILE_RULES
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

	if project.ProjectType == "mobile_app" {
		prompt += constants.NEXTJS_BUILDER_MOBILE_RULES
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

// selectContextFiles uses a fast LLM call to pick the most relevant files for
// the user's current message. Returns a set of file paths to inline. On error
// or when there are few files, returns all paths (safe fallback).
func (c *Controller) selectContextFiles(ctx context.Context, provider *utils.LLMProvider, fileMap map[string]string, userMessage string, recentMessages []models.AppConversationMessage) map[string]bool {
	const maxAutoInline = 15 // if project has ≤ this many files, inline all

	if len(fileMap) <= maxAutoInline {
		all := make(map[string]bool, len(fileMap))
		for p := range fileMap {
			all[p] = true
		}
		return all
	}

	// Force-include files explicitly mentioned in error messages or user text.
	// This ensures build errors like "./project/app/products/page.tsx (167:11)"
	// always get the actual file content in the LLM context.
	forcedFiles := extractFilePathsFromMessage(userMessage, fileMap)

	// Build file list with sizes
	paths := make([]string, 0, len(fileMap))
	for p := range fileMap {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var fileList strings.Builder
	for _, p := range paths {
		fileList.WriteString(fmt.Sprintf("- %s (%d chars)\n", p, len(fileMap[p])))
	}

	// Include last 3 assistant messages for context
	var recentContext string
	start := len(recentMessages) - 6
	if start < 0 {
		start = 0
	}
	for _, msg := range recentMessages[start:] {
		if msg.Role == "assistant" && len(msg.Content) > 0 {
			summary := msg.Content
			if len(summary) > 200 {
				summary = summary[:200] + "..."
			}
			recentContext += fmt.Sprintf("Assistant: %s\n", summary)
		}
	}

	prompt := fmt.Sprintf(`You are a software engineer reviewing a project's files to determine which ones are relevant to the user's current request.

Available files:
%s
%sUser's message:
%s

Select the files that are most relevant to this task. You can select up to 10 files. Always include:
- Files directly mentioned in the message or error traces
- Files that import/depend on mentioned files
- Layout/page files if UI changes are requested
- The main layout and page files (app/layout.tsx, app/page.tsx) when in doubt

Respond with ONLY a list of file paths, one per line. No explanation, no markdown, no numbering.`, fileList.String(), recentContext, userMessage)

	fastModel := provider.FastModel()
	result, err := utils.GetTextFromLLM(ctx, fastModel, "", prompt, provider)
	if err != nil {
		slog.Warn("selectContextFiles: LLM call failed, falling back to all files", "error", err)
		all := make(map[string]bool, len(fileMap))
		for p := range fileMap {
			all[p] = true
		}
		return all
	}

	selected := make(map[string]bool)
	for _, line := range strings.Split(result, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "- ")
		line = strings.TrimPrefix(line, "* ")
		if _, exists := fileMap[line]; exists {
			selected[line] = true
		}
	}

	// Fallback: if selection is empty or too small, inline all
	if len(selected) < 2 {
		slog.Warn("selectContextFiles: too few files selected, falling back to all", "selected", len(selected))
		all := make(map[string]bool, len(fileMap))
		for p := range fileMap {
			all[p] = true
		}
		return all
	}

	// Always include files extracted from error messages / user text
	for _, fp := range forcedFiles {
		if !selected[fp] {
			selected[fp] = true
			slog.Info("selectContextFiles: force-included file from message", "path", fp)
		}
	}

	slog.Info("selectContextFiles: selected files for context",
		"total", len(fileMap), "selected", len(selected))
	return selected
}

// extractFilePathsFromMessage scans a user message for file paths that exist in
// the project. Handles patterns like "./project/app/page.tsx (167:11)" from Next.js
// build errors, bare paths like "app/page.tsx", and common error trace formats.
var filePathInMessageRe = regexp.MustCompile(`(?:\.?/project/)?([a-zA-Z0-9_\-./]+\.(?:tsx?|jsx?|css|json|mjs))`)

func extractFilePathsFromMessage(message string, fileMap map[string]string) []string {
	matches := filePathInMessageRe.FindAllStringSubmatch(message, -1)
	seen := make(map[string]bool)
	var paths []string
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		candidate := m[1]
		// Strip leading "./" if present
		candidate = strings.TrimPrefix(candidate, "./")
		if _, exists := fileMap[candidate]; exists && !seen[candidate] {
			seen[candidate] = true
			paths = append(paths, candidate)
		}
	}
	return paths
}

// buildProjectContextForChat builds project context with selected file contents
// inlined and remaining files listed as paths only.
// chatMessagesToLLM converts stored conversation messages to LLM messages,
// attaching actual image data only to the last user message (token optimization).
// Older user messages with images get a text placeholder instead.
func chatMessagesToLLM(msgs []models.AppConversationMessage, provider *utils.LLMProvider) []utils.Message {
	supportsVision := provider.SupportsVision()

	// Find the index of the last user message that has images
	lastImgIdx := -1
	if supportsVision {
		for i := len(msgs) - 1; i >= 0; i-- {
			if msgs[i].Role == "user" && len(msgs[i].ImageURLs) > 0 {
				lastImgIdx = i
				break
			}
		}
	}

	out := make([]utils.Message, 0, len(msgs))
	for i, msg := range msgs {
		m := utils.Message{Role: msg.Role, Content: msg.Content}
		if msg.Role == "user" && len(msg.ImageURLs) > 0 {
			if i == lastImgIdx {
				m.Images = msg.ImageURLs
			} else {
				m.Content += fmt.Sprintf("\n\n[User previously attached %d image(s)]", len(msg.ImageURLs))
			}
		}
		out = append(out, m)
	}
	return out
}

func (c *Controller) getProjectLogoURL(project *models.AppProject) string {
	if project.LogoS3Key == "" {
		return ""
	}
	presigned, err := c.AssetsStorage.GetObject(context.Background(), project.LogoS3Key, 7*24*3600)
	if err != nil {
		return ""
	}
	return presigned.URL
}

func (c *Controller) buildProjectContextForChat(project *models.AppProject, fileMap map[string]string, selectedFiles map[string]bool) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("App: %s (slug: %s)\n", project.Name, project.Slug))
	sb.WriteString(fmt.Sprintf("Project type: %s\n", project.ProjectType))

	// Include logo URL if available
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

	if len(fileMap) > 0 {
		paths := make([]string, 0, len(fileMap))
		for p := range fileMap {
			paths = append(paths, p)
		}
		sort.Strings(paths)

		// List all files as paths (so LLM knows project structure)
		sb.WriteString("\nAll project files:\n")
		for _, fp := range paths {
			sb.WriteString(fmt.Sprintf("- %s (%d chars)\n", fp, len(fileMap[fp])))
		}

		// Inline selected files
		sb.WriteString("\nFile contents (relevant to current task):\n")
		for _, fp := range paths {
			if !selectedFiles[fp] {
				continue
			}
			content := fileMap[fp]
			if strings.Contains(content, fallbackSentinel) || strings.Contains(content, "Generation failed") {
				sb.WriteString(fmt.Sprintf("\n--- %s [BROKEN — needs regeneration] ---\n", fp))
			} else {
				sb.WriteString(fmt.Sprintf("\n--- %s ---\n%s\n", fp, content))
			}
		}
	}

	return sb.String()
}

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
		sb.WriteString("User prompt: " + utils.SanitizePromptContent(project.Prompt) + "\n")
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
		dataModels, _ := parseDataModels(project.DataModels)
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
					// Emit delete for old path so sandbox removes the stale .ts file
					sendEvent("file_complete", responses.FileCompleteEvent{
						Action: "delete", FilePath: oldPath,
						FileID: oldFile.ID, FileType: "ts",
					})
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

	// Auto-fix font weight for non-variable Google Fonts
	autoFixedFonts, changedPathsFonts := autoFixFontWeights(fileMap)

	// Auto-fix import/export mismatches
	autoFixed, changedPaths := autoFixImportMismatches(fileMap)
	autoFixed += autoFixedFonts
	changedPaths = append(changedPaths, changedPathsFonts...)
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

	// Auto-fix className string concatenation → template literals
	autoFixedConcat, changedPathsConcat := autoFixClassNameConcat(fileMap)
	autoFixed += autoFixedConcat
	changedPaths = append(changedPaths, changedPathsConcat...)

	// Auto-fix unterminated JSX string expressions {"text → {"text"}
	autoFixedJSXStr, changedPathsJSXStr := autoFixUnterminatedJSXStrings(fileMap)
	autoFixed += autoFixedJSXStr
	changedPaths = append(changedPaths, changedPathsJSXStr...)

	// Auto-fix missing force-dynamic on DB-backed pages
	autoFixedDynamic, changedPathsDynamic := autoFixMissingForceDynamic(fileMap)
	autoFixed += autoFixedDynamic
	changedPaths = append(changedPaths, changedPathsDynamic...)

	if autoFixed > 0 {
		// Collect fix types for the announcement
		var fixTypes []string
		if tsxRenamed > 0 {
			fixTypes = append(fixTypes, "ts_to_tsx")
		}
		if autoFixedMeta > 0 {
			fixTypes = append(fixTypes, "metadata_client_component")
		}
		if autoFixedFonts > 0 {
			fixTypes = append(fixTypes, "font_weights")
		}
		if autoFixed2 > 0 {
			fixTypes = append(fixTypes, "use_client")
		}
		if autoFixedConcat > 0 {
			fixTypes = append(fixTypes, "classname_concat")
		}
		if autoFixedJSXStr > 0 {
			fixTypes = append(fixTypes, "jsx_strings")
		}

		// Announce auto-fixes to the frontend
		sendEvent("progress", responses.ProgressEvent{
			Phase:   "auto_fix",
			Message: fmt.Sprintf("Polishing %d file(s)...", autoFixed+tsxRenamed),
		})

		slog.Info("Deterministic auto-fix applied", "project_id", project.ID, "files_fixed", autoFixed)
		for _, fixedPath := range changedPaths {
			dbFile, err := c.DB.GetAppGeneratedFileByPath(project.ID.String(), "nextjs", fixedPath)
			if err == nil && dbFile != nil {
				dbFile.Content = fileMap[fixedPath]
				dbFile.UpdatedAt = time.Now()
				if _, updateErr := c.DB.UpdateAppGeneratedFile(dbFile); updateErr != nil {
					slog.Error("runDeterministicAutoFixes: failed to persist auto-fix", "path", fixedPath, "error", updateErr)
					continue
				}
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

		// Emit auto-fix summary event
		sendEvent("auto_fix", responses.AutoFixEvent{
			FilesFixed: autoFixed + tsxRenamed,
			FixTypes:   fixTypes,
			Paths:      changedPaths,
		})
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
			Description: "Apply a single small edit to an existing file by replacing a specific string. ONLY use for tiny, surgical changes (one import, one value). You MUST read_file first. The old_string must match exactly. NEVER call edit_file more than once on the same file — use write_file to rewrite it instead. If old_string is not found, use write_file to rewrite the whole file.",
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

// chatManageStoreTool lets the fast model manage products, categories, and
// discounts in the user's e-commerce store during the chat tool phase.
var chatManageStoreTool = utils.Tool{
	Type: "function",
	Function: utils.ToolFunction{
		Name:        "manage_store",
		Description: "Create, update, delete, or list products, categories, and discounts in the e-commerce store. Use when the user asks to manage their store inventory.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"operations": map[string]any{
					"type":        "array",
					"description": "Array of store operations to execute in order",
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
								"description": "Operation payload. Products: name, price_in_cents, description, images, category (slug), tags, status, sku, inventory. Categories: name, description, image. Discounts: code, type (percentage/fixed), value, min_order_cents, max_uses, starts_at, ends_at. For updates/deletes: include id or slug or name to identify the item.",
							},
						},
						"required": []string{"action", "data"},
					},
				},
			},
			"required": []string{"operations"},
		},
	},
}

// chatPreviewWebsiteTool lets the LLM load a website URL in the preview panel
// so the user sees it while the LLM generates or modifies the app.
var chatPreviewWebsiteTool = utils.Tool{
	Type: "function",
	Function: utils.ToolFunction{
		Name:        "preview_website",
		Description: "Load a website URL in the mobile preview panel so the user can see it. Use this when the user provides a URL or asks to convert/clone a website into a mobile app. After calling this tool, generate the app code based on the website.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url": map[string]any{
					"type":        "string",
					"description": "The website URL to preview (e.g. https://example.com). Add https:// if the user didn't include it.",
				},
			},
			"required": []string{"url"},
		},
	},
}

// chatSeedDatabaseTool lets the fast model seed or reseed the app's Neon database
// with realistic demo data when the user reports missing data.
var chatSeedDatabaseTool = utils.Tool{
	Type: "function",
	Function: utils.ToolFunction{
		Name:        "seed_database",
		Description: "Seed the app's database with realistic demo data. Call this when the user says the database is empty, has no data, or asks for demo/sample/test data to be added.",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	},
}

// chatExecuteSQLTool lets the fast model run SELECT, INSERT, and UPDATE queries
// on the app's Neon database. DELETE is not allowed — use UPDATE with a soft-delete
// flag or status column instead.
var chatExecuteSQLTool = utils.Tool{
	Type: "function",
	Function: utils.ToolFunction{
		Name:        "execute_sql",
		Description: "Run a SQL query on the app's PostgreSQL database. Supports SELECT, INSERT, UPDATE. DELETE is NOT allowed. Use gen_random_uuid() for UUIDs, NOW() for timestamps. Do NOT provide 'id' values — they are auto-generated. You can insert multiple rows: INSERT INTO t (col) VALUES (...), (...), (...). Use RETURNING to get inserted row data back.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "A single SQL statement. Allowed: SELECT, INSERT, UPDATE, WITH. Not allowed: DELETE, DROP, ALTER, CREATE, TRUNCATE. Use the exact table and column names from the database schema.",
				},
			},
			"required": []string{"query"},
		},
	},
}

// chatRunMigrationsTool lets the fast model alter existing database tables.
var chatRunMigrationsTool = utils.Tool{
	Type: "function",
	Function: utils.ToolFunction{
		Name:        "run_migrations",
		Description: "Alter existing database tables — add columns, remove columns, rename columns, or create new tables. Call this when the user wants to change the database schema (e.g. 'add a rating field to articles', 'create a comments table'). This also regenerates lib/schema.ts to match.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"sql": map[string]any{
					"type":        "string",
					"description": "SQL DDL statements to execute. Use ALTER TABLE to add/drop/rename columns, CREATE TABLE for new tables. Example: ALTER TABLE \"articles\" ADD COLUMN \"rating\" INTEGER DEFAULT 0;",
				},
				"updated_models": map[string]any{
					"type":        "array",
					"description": "The full updated data model definitions (all tables, not just changed ones). Same format as setup_database tables.",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"name": map[string]any{"type": "string"},
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
										"references":  map[string]any{"type": "string"},
									},
									"required": []string{"name", "type"},
								},
							},
						},
						"required": []string{"name", "columns"},
					},
				},
			},
			"required": []string{"sql", "updated_models"},
		},
	},
}

// chatManageEnvVarsTool lets the fast model add, update, or delete environment variables.
var chatManageEnvVarsTool = utils.Tool{
	Type: "function",
	Function: utils.ToolFunction{
		Name:        "manage_env_vars",
		Description: "Add, update, or delete environment variables for the app. Call this when the user wants to set API keys, secrets, or configuration values. After changes, .env.local is automatically regenerated.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"operations": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"action": map[string]any{
								"type": "string",
								"enum": []string{"set", "delete"},
							},
							"key": map[string]any{
								"type":        "string",
								"description": "Environment variable name (e.g. STRIPE_SECRET_KEY, NEXT_PUBLIC_API_URL)",
							},
							"value": map[string]any{
								"type":        "string",
								"description": "Value to set (required for 'set' action)",
							},
							"is_public": map[string]any{
								"type":        "boolean",
								"description": "If true, prefixed with NEXT_PUBLIC_ and accessible in browser. Default false.",
							},
						},
						"required": []string{"action", "key"},
					},
				},
			},
			"required": []string{"operations"},
		},
	},
}

// chatInstallPackageTool lets the fast model add npm packages to package.json.
var chatInstallPackageTool = utils.Tool{
	Type: "function",
	Function: utils.ToolFunction{
		Name:        "install_package",
		Description: "Add npm packages to the project's package.json. Call this when the user asks to install a library or when you detect a needed dependency. Packages are added to the dependencies section.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"packages": map[string]any{
					"type":        "array",
					"description": "List of package names to install (e.g. ['zustand', '@tanstack/react-query', 'stripe'])",
					"items":       map[string]any{"type": "string"},
				},
			},
			"required": []string{"packages"},
		},
	},
}

// chatGenerateImageTool lets the fast model generate AI images via fal.ai.
var chatGenerateImageTool = utils.Tool{
	Type: "function",
	Function: utils.ToolFunction{
		Name:        "generate_image",
		Description: "Generate an AI image from a text prompt. The image is uploaded to S3 and a URL is returned. Use this when the user asks for a custom image, hero visual, logo concept, background, or any visual asset for their app.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"prompt": map[string]any{
					"type":        "string",
					"description": "Detailed description of the image to generate. Be specific about style, colors, composition, and subject matter.",
				},
				"filename": map[string]any{
					"type":        "string",
					"description": "Filename for the image (e.g. 'hero-bg.jpg', 'logo.jpg'). Used in the S3 path.",
				},
			},
			"required": []string{"prompt", "filename"},
		},
	},
}

// chatTakeScreenshotTool lets the fast model capture a screenshot of a URL.
var chatTakeScreenshotTool = utils.Tool{
	Type: "function",
	Function: utils.ToolFunction{
		Name:        "take_screenshot",
		Description: "Capture a desktop screenshot of a URL. Use this when the user provides a reference website URL and you need to see what it looks like, or to capture the current deployed app for visual comparison. Returns a base64 image that can be used for vision analysis.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url": map[string]any{
					"type":        "string",
					"description": "The URL to screenshot (e.g. 'https://example.com'). Add https:// if not included.",
				},
			},
			"required": []string{"url"},
		},
	},
}

// chatSearchComponentsTool lets the fast model search for available UI components.
var chatSearchComponentsTool = utils.Tool{
	Type: "function",
	Function: utils.ToolFunction{
		Name:        "search_components",
		Description: "Search for available pre-built UI components that can be injected into the project. Use this when the user asks for a specific component (dialog, accordion, tabs, etc.) or when you need a shadcn/ui component that isn't already in the project.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Search term for the component (e.g. 'dialog', 'tabs', 'accordion', 'dropdown menu', 'toast')",
				},
			},
			"required": []string{"query"},
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
				return fmt.Sprintf("Error: old_string not found in %s. Do NOT retry edit_file — use write_file to rewrite the entire file instead.", args.FilePath), nil
			}
			if count > 1 {
				return fmt.Sprintf("Error: old_string found %d times in %s. Use write_file to rewrite the entire file instead.", count, args.FilePath), nil
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
				slog.Error("database seeding failed, tables will be empty", "error", seedErr, "project_id", project.ID)
			} else if project.ProjectType != "ecommerce" {
				// Generate AI images for seed data image columns and store in S3/DB
				c.generateAndStoreSeedImages(ctx, project, dataModels, func(eventName string, data any) bool {
					if cbs != nil {
						return cbs.sendEvent(eventName, data)
					}
					return true
				}, 10)
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
			if seedErr == nil {
				result.WriteString("\nREALISTIC DEMO DATA HAS BEEN INSERTED into all tables. The database is NOT empty — it contains sample rows. Do NOT create seed buttons, seed routes, or tell the user to load demo data. The app should display this data directly from the database.\n")
			} else {
				result.WriteString("\nWARNING: Demo data seeding failed. Generate code that shows graceful empty states when no data exists. Do NOT create seed routes or seed buttons — the platform will handle data seeding separately.\n")
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
		dataModels, parseErr := parseDataModels(project.DataModels)
		if parseErr != nil {
			slog.Warn("pre-gen: failed to unmarshal data models", "error", parseErr, "project_id", project.ID)
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
				sendEvent("progress", responses.ProgressEvent{
					Phase:   "provisioning_db",
					Message: "Setting up your database...",
				})

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
					sendEvent("progress", responses.ProgressEvent{
						Phase:   "provisioning_db",
						Message: "Populating database with sample data...",
					})
					if _, seedErr := c.seedNeonDatabase(ctx, project, dataModels, sendEvent); seedErr != nil {
						slog.Error("pre-gen: database seeding failed", "error", seedErr, "project_id", project.ID)
					} else if project.ProjectType != "ecommerce" {
						// Generate AI images for seed data image columns and store in S3/DB
						c.generateAndStoreSeedImages(ctx, project, dataModels, sendEvent, 10)
					}
				} else {
					slog.Error("pre-gen: database provisioning failed", "error", neonErr, "project_id", project.ID)
				}

				sendEvent("progress", responses.ProgressEvent{
					Phase:   "provisioning_db",
					Message: "Database ready!",
				})
				sendEvent("neon_provision", map[string]any{"phase": "ready"})
			}
		}
	}

	// --- Auth setup ---
	if settings := project.Settings; settings != nil {
		if needsAuth, ok := settings["needs_auth"].(bool); ok && needsAuth {
			// Auth templates import from ./db, so lib/db.ts must exist.
			// If no data_models were planned (e.g. e-commerce uses wemob-store SDK),
			// provision a database. Use all project data models if available, otherwise just auth models.
			if _, hasDB := fileMap["lib/db.ts"]; !hasDB && c.NeonService != nil {
				var authModels []DataModel
				if err := json.Unmarshal([]byte(constants.AUTH_DEFAULT_DATA_MODELS), &authModels); err == nil {
					// If project has data models, use them merged with auth models (not just auth)
					modelsToProvision := authModels
					if len(project.DataModels) > 0 {
						projectModels, _ := parseDataModels(project.DataModels)
						if len(projectModels) > 0 {
							// Merge auth columns into project models
							for _, authModel := range authModels {
								found := false
								for i, m := range projectModels {
									if m.Name == authModel.Name {
										projectModels[i] = mergeDataModelColumns(authModel, m)
										found = true
										break
									}
								}
								if !found {
									projectModels = append([]DataModel{authModel}, projectModels...)
								}
							}
							modelsToProvision = projectModels
						}
					}

					sendEvent("neon_provision", map[string]any{"phase": "start"})
					sendEvent("progress", responses.ProgressEvent{
						Phase:   "provisioning_db",
						Message: "Setting up authentication database...",
					})
					connStr, neonProjectID, neonErr := c.provisionNeonDatabase(ctx, project, modelsToProvision, sendEvent)
					if connStr != "" && neonProjectID != "" && neonErr == nil {
						if err := c.storeNeonIntegration(project, connStr, neonProjectID); err != nil {
							slog.Error("pre-gen auth: failed to store neon integration", "error", err, "project_id", project.ID)
						}
						c.injectTemplateFile(project, "lib/db.ts", constants.NEXTJS_TEMPLATE_DB_TS, fileMap, sendEvent)
						schemaContent := buildDrizzleSchema(modelsToProvision)
						c.injectTemplateFile(project, "lib/schema.ts", schemaContent, fileMap, sendEvent)
						sendEvent("progress", responses.ProgressEvent{
							Phase:   "provisioning_db",
							Message: "Populating database with sample data...",
						})
						if _, seedErr := c.seedNeonDatabase(ctx, project, modelsToProvision, sendEvent); seedErr != nil {
							slog.Error("pre-gen auth: database seeding failed", "error", seedErr, "project_id", project.ID)
						} else if project.ProjectType != "ecommerce" {
							c.generateAndStoreSeedImages(ctx, project, modelsToProvision, sendEvent, 10)
						}
					} else {
						slog.Error("pre-gen auth: database provisioning failed", "error", neonErr, "project_id", project.ID)
					}
					sendEvent("progress", responses.ProgressEvent{
						Phase:   "provisioning_db",
						Message: "Database ready!",
					})
					sendEvent("neon_provision", map[string]any{"phase": "ready"})
				}
			}

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
	toolContext string,
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

	// Inject parameterized templates (layout + globals.css)
	{
		var theme models.AppTheme
		if len(project.Theme) > 0 {
			json.Unmarshal(project.Theme, &theme)
		}
		if project.ProjectType == "ecommerce" {
			if layoutContent := buildEcommerceLayout(project.Name, theme.Typography); layoutContent != "" {
				c.injectTemplateFile(project, "app/layout.tsx", layoutContent, fileMap, sendEvent)
			}
			if cssContent := buildEcommerceGlobalsCSSTheme(theme.ColorScheme, theme.DarkColorScheme); cssContent != "" {
				c.injectTemplateFile(project, "app/globals.css", cssContent, fileMap, sendEvent)
			}
		} else {
			// For non-e-commerce apps, inject globals.css with semantic tokens so
			// the UI component templates (button, card, input, badge) work correctly.
			isMobile := project.ProjectType == "mobile_app"
			if cssContent := buildGlobalsCSSTheme(theme.ColorScheme, theme.DarkColorScheme, isMobile); cssContent != "" {
				c.injectTemplateFile(project, "app/globals.css", cssContent, fileMap, sendEvent)
			}
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

	// 3. Generate AI logo (runs in parallel with nothing — just do it inline, fast)
	logoURL := c.generateAppLogo(ctx, project, sendEvent)

	// 4. Build messages with single-shot system prompt
	projectContext := c.buildProjectContext(project, fileMap, true)
	if logoURL != "" {
		projectContext += fmt.Sprintf("\n\nApp Logo URL (use this as the site logo/favicon in the header and metadata):\n%s", logoURL)
	}

	llmMessages := []utils.Message{
		{Role: "system", Content: buildSingleShotSystemPrompt(project)},
		{Role: "system", Content: projectContext},
	}

	// Inject tool-gathered context (website analysis, web search results) if available
	if toolContext != "" {
		llmMessages = append(llmMessages, utils.Message{Role: "system", Content: toolContext})
	}

	userMsg := utils.Message{Role: "user", Content: message}
	if len(imageURLs) > 0 {
		if provider.SupportsVision() {
			userMsg.Images = imageURLs
		} else {
			userMsg.Content += fmt.Sprintf("\n\n[User attached %d image(s) for visual reference — recreate the design shown]", len(imageURLs))
		}
	}
	llmMessages = append(llmMessages, userMsg)

	// 5. Set up streaming file parser
	plannedTotal := 0
	if structure != nil {
		plannedTotal = len(structure.Files)
	}
	fileCounter := 0

	sendEvent("progress", responses.ProgressEvent{
		Phase:   "generating_files",
		Message: "Starting code generation...",
		Current: 0,
		Total:   plannedTotal,
	})

	var responseBuilder strings.Builder
	parser := &utils.FileStreamParser{
		OnFile: func(path, content string) {
			content = utils.StripCodeFences(content)
			_, fileType := guessFileType(path)
			if fileType == "tsx" || fileType == "ts" {
				content, _ = stripForbiddenImports(content, path)
			}
			c.saveGeneratedFile(project, path, content, fileType, fileMap, sendEvent, &ops)
			fileCounter++
			sendEvent("progress", responses.ProgressEvent{
				Phase:   "generating_files",
				Message: fmt.Sprintf("Generated %s (%d/%d)", path, fileCounter, plannedTotal),
				Current: fileCounter,
				Total:   plannedTotal,
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

	// 6. Single LLM call — no tools, no rounds
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

	// 7. Run deterministic auto-fixes before syncing (e.g. remove "use client" from
	// files that export metadata — Next.js will 500 otherwise).
	preFixSnapshot := make(map[string]string, len(fileMap))
	for k, v := range fileMap {
		preFixSnapshot[k] = v
	}
	autoFixMetadataInClientComponent(fileMap)
	autoFixMissingUseClient(fileMap)
	autoFixRootLayoutTags(fileMap)
	autoFixUnterminatedJSXStrings(fileMap)
	autoFixMissingForceDynamic(fileMap)
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
				if _, updateErr := c.DB.UpdateAppGeneratedFile(dbFile); updateErr != nil {
					slog.Error("failed to persist auto-fixed file", "path", path, "error", updateErr)
				}
			}
		}
	}

	return responseBuilder.String(), ops, err
}

// generateAppLogo generates an AI logo for the app via fal.ai, uploads it to S3,
// and returns a presigned URL. Returns "" on any failure (best-effort).
func (c *Controller) generateAppLogo(ctx context.Context, project *models.AppProject, sendEvent func(string, any) bool) string {
	apiKey := c.SecretManager.GetSecret("FAL_API_KEY")
	if apiKey == "" {
		return ""
	}

	falModel := c.SecretManager.GetSecret("FAL_IMAGE_MODEL")
	if falModel == "" {
		falModel = "fal-ai/flux/schnell"
	}

	sendEvent("tool_start", map[string]any{"tool": "generate_logo"})
	sendEvent("progress", responses.ProgressEvent{
		Phase:   "generating_logo",
		Message: "Designing your app logo...",
	})

	prompt := fmt.Sprintf(
		"Minimal, modern app logo icon for '%s'. Clean geometric design, flat style, single iconic symbol, no text, no letters, solid background, suitable for favicon and app header. Professional, tech-company quality.",
		project.Name,
	)

	cdnURL, err := fal.GenerateImage(ctx, apiKey, falModel, prompt)
	if err != nil {
		slog.Warn("generateAppLogo: fal generation failed", "error", err, "project_id", project.ID)
		return ""
	}

	imgData, err := downloadImage(ctx, cdnURL)
	if err != nil {
		slog.Warn("generateAppLogo: failed to download image", "error", err, "project_id", project.ID)
		return ""
	}

	s3Key := fmt.Sprintf("apps/%s/logo.png", project.ID)
	if err := c.AssetsStorage.UploadFileByteWithContentType(ctx, s3Key, imgData, "image/png"); err != nil {
		slog.Warn("generateAppLogo: failed to upload to S3", "error", err, "project_id", project.ID)
		return ""
	}

	// Persist logo S3 key on the project for future use (chat context, re-generation)
	project.LogoS3Key = s3Key
	if _, err := c.DB.DB.NewUpdate().Model(project).Column("logo_s3_key").WherePK().Exec(ctx); err != nil {
		slog.Warn("generateAppLogo: failed to save logo_s3_key", "error", err, "project_id", project.ID)
	}

	presigned, err := c.AssetsStorage.GetObject(ctx, s3Key, 7*24*3600)
	if err != nil {
		slog.Warn("generateAppLogo: failed to presign", "error", err, "project_id", project.ID)
		return ""
	}

	sendEvent("progress", responses.ProgressEvent{
		Phase:   "generating_logo",
		Message: "Logo ready!",
	})

	slog.Info("generateAppLogo: logo generated", "project_id", project.ID, "s3_key", s3Key)
	return presigned.URL
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
	toolContext string,
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
		sendEvent("progress", responses.ProgressEvent{
			Phase:   "generating_images",
			Message: "Generating product images...",
		})
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

	// Generate AI logo for the store
	logoURL := c.generateAppLogo(ctx, project, sendEvent)

	// Rebuild messages with fresh context (now includes store data from Phase 1)
	projectContext := c.buildProjectContext(project, fileMap, true)
	if logoURL != "" {
		projectContext += fmt.Sprintf("\n\nApp Logo URL (use this as the site logo/favicon in the header and metadata):\n%s", logoURL)
	}

	llmMessages := []utils.Message{
		{Role: "system", Content: buildSingleShotSystemPrompt(project)},
		{Role: "system", Content: projectContext},
	}

	// Inject tool-gathered context (website analysis, web search results) if available
	if toolContext != "" {
		llmMessages = append(llmMessages, utils.Message{Role: "system", Content: toolContext})
	}

	userMsg := utils.Message{Role: "user", Content: message}
	if len(imageURLs) > 0 {
		if provider.SupportsVision() {
			userMsg.Images = imageURLs
		} else {
			userMsg.Content += fmt.Sprintf("\n\n[User attached %d image(s) for visual reference — recreate the design shown]", len(imageURLs))
		}
	}
	llmMessages = append(llmMessages, userMsg)

	// Set up streaming file parser
	ecomPlannedTotal := 0
	if structure != nil {
		ecomPlannedTotal = len(structure.Files)
	}
	ecomFileCounter := 0

	sendEvent("progress", responses.ProgressEvent{
		Phase:   "generating_files",
		Message: "Starting code generation...",
		Current: 0,
		Total:   ecomPlannedTotal,
	})

	var responseBuilder strings.Builder
	parser := &utils.FileStreamParser{
		OnFile: func(path, content string) {
			content = utils.StripCodeFences(content)
			_, fileType := guessFileType(path)
			if fileType == "tsx" || fileType == "ts" {
				content, _ = stripForbiddenImports(content, path)
			}
			c.saveGeneratedFile(project, path, content, fileType, fileMap, sendEvent, &ops)
			ecomFileCounter++
			sendEvent("progress", responses.ProgressEvent{
				Phase:   "generating_files",
				Message: fmt.Sprintf("Generated %s (%d/%d)", path, ecomFileCounter, ecomPlannedTotal),
				Current: ecomFileCounter,
				Total:   ecomPlannedTotal,
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
	autoFixUnterminatedJSXStrings(fileMap)
	autoFixMissingForceDynamic(fileMap)
	// Persist any auto-fixed or newly created content back to the DB
	for path, content := range fileMap {
		prev, existed := preFixSnapshot[path]
		if !existed {
			_, fileType := guessFileType(path)
			c.saveGeneratedFile(project, path, content, fileType, fileMap, sendEvent, &ops)
		} else if content != prev {
			if dbFile, err := c.DB.GetAppGeneratedFileByPath(project.ID.String(), "nextjs", path); err == nil && dbFile != nil {
				dbFile.Content = content
				if _, updateErr := c.DB.UpdateAppGeneratedFile(dbFile); updateErr != nil {
					slog.Error("failed to persist auto-fixed file (ecom)", "path", path, "error", updateErr)
				}
			}
		}
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

// streamWebSearchSummary streams a natural language summary of web search findings to the user.
// Includes screenshots as vision content so the model can accurately describe colors/layout.
func (c *Controller) streamWebSearchSummary(
	ctx context.Context,
	provider *utils.LLMProvider,
	pgCtx preGenContext,
	userMessage string,
	sendEvent func(string, any) bool,
) string {
	fastModel := provider.FastModel()
	userContent := fmt.Sprintf("User's request:\n%s\n\nSearch results:\n%s", userMessage, pgCtx.Text)
	if len(pgCtx.Screenshots) > 0 {
		userContent += "\n\nA screenshot of the referenced website is attached. Use it to accurately describe the site's color scheme, layout, and visual style. Do NOT guess colors — describe exactly what you see in the screenshot."
	}
	messages := []utils.Message{
		{Role: "system", Content: constants.CONVERSATION_WEB_SUMMARY_PROMPT},
		{Role: "user", Content: userContent, Images: pgCtx.Screenshots},
	}

	fullText, err := utils.StreamTextFromLLMWithMessages(
		ctx, fastModel, messages, provider,
		func(chunk string) {
			sendEvent("conversation_chunk", map[string]any{"chunk": chunk})
		},
		nil, // no reasoning
		utils.StreamTextConfig{MaxTokens: 512},
	)
	if err != nil {
		slog.Warn("failed to stream web search summary", "error", err)
		return ""
	}
	sendEvent("conversation_done", map[string]any{"type": "web_summary", "text": fullText})
	return fullText
}

// runConversationLoop checks if the prompt needs clarification and runs a natural
// multi-turn conversation (max 3 turns) to gather requirements. Returns the
// enriched prompt with conversation transcript appended.
func (c *Controller) runConversationLoop(
	ctx context.Context,
	jobID string,
	provider *utils.LLMProvider,
	message string,
	webContext string,
	webSummaryText string,
	projectType string,
	sendEvent func(string, any) bool,
) string {
	fastModel := provider.FastModel()

	// Step 1: Quick decision — does this prompt need conversation?
	decisionMsg := fmt.Sprintf("Project type: %s\n\nUser prompt:\n%s", projectType, message)
	if webContext != "" {
		decisionMsg += "\n\nWeb context available: yes (the user referenced a website and we already have search results)"
	}
	decisionJSON, err := utils.GetJSONFromLLM(ctx, fastModel, constants.CONVERSATION_DECISION_PROMPT, decisionMsg, provider)
	if err != nil {
		slog.Warn("conversation decision failed, skipping", "error", err)
		return message
	}

	var decision struct {
		NeedsConversation bool `json:"needs_conversation"`
	}
	if err := json.Unmarshal([]byte(decisionJSON), &decision); err != nil {
		cleaned := utils.ExtractJSON(decisionJSON)
		if err2 := json.Unmarshal([]byte(cleaned), &decision); err2 != nil {
			slog.Warn("failed to parse conversation decision", "error", err2)
			return message
		}
	}

	// Step 2: If no conversation needed, return original prompt
	if !decision.NeedsConversation {
		return message
	}

	// Step 3: Conversation loop (max 3 turns)
	conversationHistory := []conversationTurn{
		{Role: "user", Content: message},
	}
	// If we already streamed a web summary, include it so the LLM doesn't repeat itself
	if webSummaryText != "" {
		conversationHistory = append(conversationHistory, conversationTurn{Role: "assistant", Content: webSummaryText})
	}

	for turn := 0; turn < 3; turn++ {
		if ctx.Err() != nil {
			break
		}

		// Build messages for the LLM
		llmMessages := []utils.Message{
			{Role: "system", Content: constants.CONVERSATION_CHAT_PROMPT},
		}

		// Add context about the project
		contextPrefix := fmt.Sprintf("Project type: %s\n", projectType)
		if webContext != "" {
			contextPrefix += "Web search context is available and will be used during generation.\n"
		}
		contextPrefix += "\nConversation so far:\n"

		for _, t := range conversationHistory {
			contextPrefix += fmt.Sprintf("[%s]: %s\n", t.Role, t.Content)
		}

		llmMessages = append(llmMessages, utils.Message{
			Role:    "user",
			Content: contextPrefix + "\nAsk your next question(s) to clarify what to build.",
		})

		// Stream the AI's question to the user
		var aiMessage strings.Builder
		_, err := utils.StreamTextFromLLMWithMessages(
			ctx, fastModel, llmMessages, provider,
			func(chunk string) {
				aiMessage.WriteString(chunk)
				sendEvent("conversation_chunk", map[string]any{"chunk": chunk})
			},
			nil,
			utils.StreamTextConfig{MaxTokens: 512},
		)
		if err != nil {
			slog.Warn("conversation turn failed", "error", err, "turn", turn)
			break
		}

		aiText := aiMessage.String()
		sendEvent("conversation_done", map[string]any{"type": "conversation", "text": aiText})
		conversationHistory = append(conversationHistory, conversationTurn{Role: "assistant", Content: aiText})

		// Wait for user reply
		ch := make(chan ClarificationResult, 1)
		c.PendingClarifications.Store(jobID, ch)

		if err := c.DB.UpdateGenerationJobStatus(jobID, "awaiting_input", "waiting for user reply"); err != nil {
			slog.Warn("failed to update job status to awaiting_input", "error", err, "job_id", jobID)
		}

		sendEvent("awaiting_reply", map[string]any{
			"job_id":          jobID,
			"timeout_seconds": 120,
		})

		timer := time.NewTimer(2 * time.Minute)
		var userReply string
		gotReply := false

		select {
		case result, ok := <-ch:
			timer.Stop()
			c.PendingClarifications.Delete(jobID)
			if ok && !result.Skip && len(result.Answers) > 0 {
				userReply = result.Answers[0].Answer
				gotReply = true
			}
		case <-timer.C:
			c.PendingClarifications.Delete(jobID)
			slog.Info("conversation reply timed out, proceeding with what we have", "job_id", jobID, "turn", turn)
		case <-ctx.Done():
			timer.Stop()
			c.PendingClarifications.Delete(jobID)
			return message
		}

		c.DB.UpdateGenerationJobStatus(jobID, "running", "")

		if !gotReply {
			break
		}

		conversationHistory = append(conversationHistory, conversationTurn{Role: "user", Content: userReply})

		// Check if we have enough info to proceed (after at least 1 exchange)
		if turn >= 0 {
			readyCheck := fmt.Sprintf("Based on this conversation, do we have enough context to build? Conversation:\n")
			for _, t := range conversationHistory {
				readyCheck += fmt.Sprintf("[%s]: %s\n", t.Role, t.Content)
			}
			readyJSON, err := utils.GetJSONFromLLM(ctx, fastModel, `Decide if there is enough information to start building based on the conversation. Return ONLY: {"ready": true} or {"ready": false}`, readyCheck, provider)
			if err == nil {
				var ready struct {
					Ready bool `json:"ready"`
				}
				if json.Unmarshal([]byte(readyJSON), &ready) == nil && ready.Ready {
					break
				}
				cleaned := utils.ExtractJSON(readyJSON)
				if json.Unmarshal([]byte(cleaned), &ready) == nil && ready.Ready {
					break
				}
			}
		}
	}

	// Step 4: Build enriched prompt with conversation transcript
	return buildConversationEnrichedPrompt(message, conversationHistory)
}

// conversationTurn represents a single turn in a pre-build conversation.
type conversationTurn struct {
	Role    string
	Content string
}

// buildConversationEnrichedPrompt appends a conversation transcript to the original prompt.
func buildConversationEnrichedPrompt(original string, history []conversationTurn) string {
	if len(history) <= 1 {
		return original
	}

	var sb strings.Builder
	sb.WriteString(original)
	sb.WriteString("\n\n--- Pre-build conversation ---\n")
	for _, t := range history[1:] { // skip the first entry which is the original prompt
		if t.Role == "assistant" {
			sb.WriteString(fmt.Sprintf("AI: %s\n", t.Content))
		} else {
			sb.WriteString(fmt.Sprintf("User: %s\n", t.Content))
		}
	}
	return sb.String()
}

// streamBuildPlan streams a brief "here's what I'll create" message to the user.
func (c *Controller) streamBuildPlan(
	ctx context.Context,
	provider *utils.LLMProvider,
	enrichedMessage string,
	webContext string,
	projectType string,
	sendEvent func(string, any) bool,
) {
	fastModel := provider.FastModel()

	userContent := fmt.Sprintf("Project type: %s\n\nUser request (with any conversation context):\n%s", projectType, enrichedMessage)
	if webContext != "" {
		userContent += "\n\nWeb search context:\n" + webContext
	}

	messages := []utils.Message{
		{Role: "system", Content: constants.CONVERSATION_BUILD_PLAN_PROMPT},
		{Role: "user", Content: userContent},
	}

	fullText, err := utils.StreamTextFromLLMWithMessages(
		ctx, fastModel, messages, provider,
		func(chunk string) {
			sendEvent("conversation_chunk", map[string]any{"chunk": chunk})
		},
		nil,
		utils.StreamTextConfig{MaxTokens: 512},
	)
	if err != nil {
		slog.Warn("failed to stream build plan", "error", err)
		return
	}
	sendEvent("conversation_done", map[string]any{"type": "build_plan", "text": fullText})
}

// Deprecated: waitForClarification sends clarification questions to the client and blocks
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
		"questions":       questions.Questions,
		"job_id":          jobID,
		"timeout_seconds": 300,
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
