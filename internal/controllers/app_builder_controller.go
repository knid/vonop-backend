package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/services/gitrepo"
	"github.com/knid/wemob/internal/services/remoteconfig"
	"github.com/knid/wemob/internal/templates"
	"github.com/knid/wemob/internal/utils"
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
// NOTE: This is intentionally a shallow copy (string values are immutable in Go)
// which is sufficient for rollback safety — callers get an isolated key set
// while the string values themselves cannot be mutated in place.
func (b *builderToolCallbacks) snapshotFileMap() map[string]string {
	b.fileMu.RLock()
	m := make(map[string]string, len(b.fileMap))
	for k, v := range b.fileMap {
		m[k] = v
	}
	b.fileMu.RUnlock()
	return m
}

// safeTruncate truncates s to at most maxBytes while preserving valid UTF-8.
func safeTruncate(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	// Walk back from the byte boundary to find a valid rune start
	for maxBytes > 0 && maxBytes < len(s) && s[maxBytes]&0xC0 == 0x80 {
		maxBytes--
	}
	return s[:maxBytes]
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
		defer eventsClosed.Unlock()
		if !eventsOpen {
			return false
		}
		if ctx.Err() != nil {
			return false
		}
		// Use a blocking send with a timeout instead of a non-blocking send.
		// A non-blocking send permanently drops all future events (including
		// chat_complete/done) if the channel buffer is momentarily full,
		// leaving the frontend hanging indefinitely.
		select {
		case events <- responses.SSEEvent{Event: eventName, Data: data}:
			return true
		case <-time.After(10 * time.Second):
			slog.Warn("sendEvent timed out waiting for channel space, dropping event",
				"event", eventName, "project_id", project.ID)
			return false
		case <-ctx.Done():
			return false
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
			// Auto-rollback on panic via git revert
			if rollbackSnapshotID != "" && rollbackOps != nil && len(*rollbackOps) > 0 {
				slog.Info("Auto-rollback: restoring after panic", "project_id", project.ID)
				if panicRepo, panicErr := c.GitRepoService.Open(project.ID.String()); panicErr == nil {
					panicRepo.RevertAndCommit(rollbackSnapshotID, "auto: rollback after panic")
				}
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
		// No Opus switching — always Sonnet, reasoning effort handled via ReasoningConfig
	}

	// --- User plan limits (used for DB provisioning) ---
	var userLimits *models.StandartUserLimits
	if fetchedLimits, limitsErr := c.DB.GetStandartUserLimitsByUser(user); limitsErr == nil {
		userLimits = fetchedLimits
	}

	// --- Token tracking ---
	genStart := time.Now()
	tracker := utils.NewTokenTracker()
	tracker.TrackContent = true
	tracker.UserID = user.ID.String()
	tracker.ProjectID = project.ID.String()
	ctx = utils.WithTokenTracker(ctx, tracker)

	actionLabel := "app_chat"
	if isInitialGeneration {
		actionLabel = "generate_app"
	}
	var generationSucceeded bool // set to true only when generation completes without error
	var lastGenErr error         // captures the error from whichever path failed (chat/singleshot/ecommerce)

	defer func() {
		closeEvents.Do(func() { close(events) })
		c.RecordTokenUsage(user, actionLabel, model, provider.Name, project.ID.String(), project.ProjectType, tracker)
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
				"succeeded", generationSucceeded,
				"duration_s", fmt.Sprintf("%.1f", genDuration),
				"llm_calls", callCount,
				"prompt_tokens", prompt,
				"completion_tokens", completion,
				"total_tokens", total,
				"credits_used", credits,
				"model", model,
				"provider", provider.Name,
			)
			if generationSucceeded {
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
		}
		if !generationSucceeded {
			failedProps := posthog.NewProperties().
				Set("app_id", project.ID.String()).
				Set("duration_seconds", time.Since(genStart).Seconds()).
				Set("total_tokens", total).
				Set("prompt_tokens", prompt).
				Set("completion_tokens", completion).
				Set("llm_call_count", callCount).
				Set("model", model).
				Set("is_generation", isInitialGeneration)
			if lastGenErr != nil {
				failedProps.Set("error", lastGenErr.Error())
			}
			c.TrackUserEvent(user, actionLabel+"_failed", failedProps)
		}
	}()

	// --- Keepalive goroutine ---
	keepaliveDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		maxLifetime := time.NewTimer(30 * time.Minute)
		defer maxLifetime.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-keepaliveDone:
				return
			case <-maxLifetime.C:
				slog.Warn("keepalive goroutine reached max lifetime, exiting")
				return
			case <-ticker.C:
				sendEvent("keepalive", map[string]any{"ts": time.Now().Unix()})
			}
		}
	}()
	defer close(keepaliveDone)

	// --- Load mobile app config (for bridge SDK injection) ---
	var mobileConfig *models.MobileAppConfig
	if project.ProjectType == "mobile_app" {
		cfg, err := c.DB.GetMobileAppConfig(project.ID.String())
		if err == nil {
			mobileConfig = cfg
		}
	}

	// --- Open git repo ---
	repo, err := c.GitRepoService.OpenOrInit(project.ID.String())
	if err != nil {
		slog.Error("failed to open git repo", "error", err, "project_id", project.ID)
		sendError("repo", "failed to open project repository")
		return
	}

	// --- Build fileMap ---
	fileMap := make(map[string]string)

	if isInitialGeneration {
		// Detect language from user prompt for localized templates
		lang := constants.DetectLanguage(project.Prompt)

		// Inject template files (package.json, tsconfig, postcss, next.config)
		for _, tp := range templates.BaseFilePaths {
			// For globals.css, don't inject a pre-built theme — let the LLM write it
			if tp == "app/globals.css" {
				continue
			}
			content := getTemplateContentWithLang(tp, nil, nil, project.ProjectType, nil, lang)
			if content == "" {
				continue
			}
			c.injectTemplateFile(repo, tp, content, fileMap, sendEvent)
		}

		// Inject UI component templates (lib/utils, components/ui/*)
		for _, tp := range templates.UIFilePaths {
			content := getUITemplateContent(tp)
			if content != "" {
				c.injectTemplateFile(repo, tp, content, fileMap, sendEvent)
			}
		}

		// Inject ecommerce templates if applicable
		if project.ProjectType == "ecommerce" {
			needsAuth := false
			if settings := project.Settings; settings != nil {
				needsAuth, _ = settings["needs_auth"].(bool)
			}
			for _, tp := range templates.EcommerceFilePaths {
				var content string
				if tp == "components/ecommerce/providers.tsx" {
					content = buildEcommerceProviders(needsAuth)
				} else {
					content = getEcommerceTemplateContent(tp, lang)
				}
				if content != "" {
					c.injectTemplateFile(repo, tp, content, fileMap, sendEvent)
				}
			}
		}

		// Generate .env.local so env vars are available
		envVars := c.CollectAllEnvVars(project)
		if dotEnv := BuildDotEnvLocal(envVars); dotEnv != "" {
			c.injectTemplateFile(repo, ".env.local", dotEnv, fileMap, sendEvent)
		}

		// project_created event is sent by streamSingleShotGeneration
		// (with planned_files when structure planning succeeds)
	} else {
		// Load existing files from git repo for chat editing
		repoFiles, err := repo.ReadAllFiles()
		if err != nil {
			sendError("repo", "failed to load project files")
			return
		}
		for k, v := range repoFiles {
			fileMap[k] = v
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

	// Convert base64 images to data URIs for LLM, upload to S3 for storage
	var imageURLs []string    // data URIs for LLM
	var storedImageURLs []string // S3 URLs for conversation storage
	for i, img := range images {
		if img == "" {
			continue
		}
		if !strings.HasPrefix(img, "data:") {
			img = "data:image/png;base64," + img
		}
		imageURLs = append(imageURLs, img)
		// Upload to S3 so we don't store multi-MB base64 in the conversation
		if imgData, err := utils.DecodeBase64Image(img); err == nil && len(imgData) < 5*1024*1024 {
			s3Key := fmt.Sprintf("apps/%s/chat-images/%s-%d.png", project.ID, uuid.New().String()[:8], i)
			if publicURL, err := c.AssetsStorage.UploadFileBytePublic(ctx, s3Key, imgData, "image/png"); err == nil {
				storedImageURLs = append(storedImageURLs, publicURL)
				continue
			}
		}
		// Fallback: store a placeholder instead of the full base64
		storedImageURLs = append(storedImageURLs, "[image]")
	}

	// Append user message (store S3 URLs, not base64)
	messages = append(messages, models.AppConversationMessage{
		ID:        uuid.New().String(),
		Role:      "user",
		Content:   message,
		Timestamp: time.Now().Unix(),
		ImageURLs: storedImageURLs,
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
			{Role: "system", Content: buildSystemPrompt(project, mobileConfig)},
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
		// Chat path: unified single-call with tools.
		// Build system prompt, file list context, conversation history, and tools,
		// then make a single StreamWithTools call that can read files on-demand.

		// 1. Build unified system prompt
		chatSystemPrompt := buildUnifiedChatSystemPrompt(project, mobileConfig)

		// 2. Build file list context (paths + sizes only; model reads on-demand)
		fileListContext := c.buildFileListContext(project, fileMap)

		// 3. Add DB schema to system prompt if project has database
		if c.neonConnString(project.ID.String()) != "" {
			dataModels, err := parseDataModels(project.DataModels)
			if err != nil {
				slog.Warn("failed to parse data models for chat system prompt", "error", err)
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
				chatSystemPrompt += "All tables have an auto-generated UUID 'id' column and auto-generated created_at/updated_at timestamps — do NOT provide id, created_at, or updated_at in INSERT statements.\n"
			}
		}

		// 4. Build conversation history
		keepRecent := c.RemoteConfig.GetInt(remoteconfig.AIChatKeepRecent, remoteconfig.Global)
		chatHistory := buildSimpleChatHistory(conv, messages, provider, imageURLs, keepRecent)

		llmMessages = []utils.Message{
			{Role: "system", Content: chatSystemPrompt},
			{Role: "system", Content: fileListContext},
		}
		llmMessages = append(llmMessages, chatHistory...)
	}

	if ctx.Err() != nil {
		return
	}

	// --- Create auto-snapshot (git commit) before changes ---
	// For chat edits, we require a commit so we can rollback on failure.
	// For initial generation there's nothing to rollback to, so a failure is OK.
	preCommitHash, snapErr := c.CreateAppSnapshot(project, "Auto-save before changes", "auto")
	if snapErr != nil && !isInitialGeneration {
		slog.Error("failed to create pre-change snapshot, aborting chat edit",
			"error", snapErr, "project_id", project.ID)
		sendError("snapshot", "failed to create safety snapshot. please try again")
		return
	} else if snapErr != nil {
		slog.Warn("failed to create pre-change snapshot for initial gen, rollback will not be available",
			"error", snapErr, "project_id", project.ID)
	}
	preSnapshotID := preCommitHash

	// --- Pre-generation: web search + classification in parallel ---
	var preGenCtx preGenContext
	var classification promptDecision
	if isInitialGeneration {
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			preGenCtx = c.runPreGenWebSearch(ctx, provider, message, imageURLs, sendEvent)
			if preGenCtx.HasContent() {
				slog.Info("Initial generation: web search returned context",
					"project_id", project.ID, "context_length", len(preGenCtx.Text),
					"screenshots", len(preGenCtx.Screenshots))
			}
		}()
		go func() {
			defer wg.Done()
			classification = c.classifyPrompt(ctx, provider, message, imageURLs, "", project.ProjectType)
		}()
		wg.Wait()
	}
	// preGenToolContext is the text portion, used by downstream functions that take string
	preGenToolContext := preGenCtx.Text

	if ctx.Err() != nil {
		return
	}

	// --- Conversational pre-build phase ---
	if isInitialGeneration && jobID != "" {
		// Phase A: Stream a summary of web search results to the user
		var webSummaryText string
		if preGenCtx.HasContent() {
			webSummaryText = c.streamWebSearchSummary(ctx, provider, preGenCtx, message, sendEvent)
		}

		if ctx.Err() != nil {
			return
		}

		// Phase B: Stream the greeting from classification (merged — no separate LLM call)
		if greeting := strings.TrimSpace(classification.Greeting); greeting != "" && !classification.IsQuestion {
			sendEvent("conversation_chunk", map[string]any{"chunk": greeting})
			sendEvent("conversation_done", map[string]any{"type": "build_plan", "text": greeting})
		}

		// Phase C: Natural conversation loop (if prompt is vague or is a question)
		message = c.runConversationLoop(ctx, jobID, provider, classification, message, imageURLs, preGenToolContext, webSummaryText, project.ProjectType, sendEvent)

		if ctx.Err() != nil {
			return
		}

		// If runConversationLoop returned empty, the user asked a general question
		// (not a build request). Stop generation — the question was already answered.
		if message == "" {
			sendEvent("done", map[string]any{"success": true, "files_changed": 0})
			return
		}
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
			ctx, project, user, model, provider, repo, fileMap, sendEvent, message, ecomImages, preGenToolContext, mobileConfig,
		)
		if ecomErr != nil {
			lastGenErr = ecomErr
			slog.Error("E-commerce generation failed", "error", ecomErr, "project_id", project.ID)

			// Auto-rollback on failure
			if preSnapshotID != "" && len(ecomOps) > 0 {
				slog.Info("Auto-rollback: restoring pre-snapshot after failed e-commerce generation",
					"project_id", project.ID,
					"snapshot_id", preSnapshotID,
					"partial_ops", len(ecomOps),
				)
				if _, rollbackErr := repo.RevertAndCommit(preSnapshotID, "auto: rollback after failure"); rollbackErr != nil {
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
			ctx, project, user, model, provider, repo, fileMap, sendEvent, message, genImages, preGenToolContext, mobileConfig,
		)
		if singleShotErr != nil {
			lastGenErr = singleShotErr
			slog.Error("Single-shot generation failed", "error", singleShotErr, "project_id", project.ID)

			// Auto-rollback on failure
			if preSnapshotID != "" && len(singleShotOps) > 0 {
				slog.Info("Auto-rollback: restoring pre-snapshot after failed single-shot generation",
					"project_id", project.ID,
					"snapshot_id", preSnapshotID,
					"partial_ops", len(singleShotOps),
				)
				if _, rollbackErr := repo.RevertAndCommit(preSnapshotID, "auto: rollback after failure"); rollbackErr != nil {
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
		// === Unified tool-calling path: chat edits ===
		// Single StreamWithTools call that can read files, preview websites,
		// manage env vars, etc. while also streaming <file>/<edit>/<delete> blocks.

		var responseBuilder strings.Builder
		var deletedPaths []string
		chatFileCounter := 0

		// Track failed edits for retry
		type failedEditRecord struct {
			Path    string
			Search  string
			Replace string
		}
		var failedEdits []failedEditRecord

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
				deletedPaths = append(deletedPaths, path)
			},
			OnEdit: func(path, search, replace string) {
				original, ok := fileMap[path]
				if !ok {
					slog.Warn("edit target file not found in fileMap", "path", path)
					failedEdits = append(failedEdits, failedEditRecord{Path: path, Search: search, Replace: replace})
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
					failedEdits = append(failedEdits, failedEditRecord{Path: path, Search: search, Replace: replace})
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

		// Reasoning config — always Sonnet, no Opus switch
		reasoning := c.reasoningConfig()
		if reasoningEffort != "" && reasoningEffort != "none" {
			reasoning = &utils.ReasoningConfig{Effort: reasoningEffort}
		} else if reasoningEffort == "none" {
			reasoning = nil
		}

		// Build tools and executor
		// NOTE: screenshots from preview_website/take_screenshot are captured but cannot
		// be attached as vision content mid-conversation (ToolExecutor returns string only).
		// The text analysis from these tools is still returned to the model via tool results.
		tools, executor, _ := c.buildUnifiedChatTools(ctx, project, provider, repo, fileMap, sendEvent, userLimits)

		slog.Info("Calling LLM for chat (unified tool path)",
			"model", model,
			"project_id", project.ID,
			"message_count", len(llmMessages),
			"tool_count", len(tools),
		)

		lastReportedOpCount := 0

		// Single unified StreamWithTools call
		_, chatErr := utils.StreamWithTools(
			ctx, model, llmMessages, tools, provider,
			nil, // no response format
			reasoning,
			executor,
			func(toolName string, args string) {
				sendEvent("tool_start", map[string]string{"tool": toolName})
				accToolCalls = append(accToolCalls, models.AppChatToolCall{
					Tool:      toolName,
					Arguments: args,
				})
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
				NudgeAfter:       3,
				StripAfter:       5,
				MaxTokens:        65536,
				MaxContinuations: 3,
				NudgeMessage:     "You have gathered enough context. Output your code changes now using <file> and <edit> blocks.",
				OnRoundStart: func(round int) {
					parser.Flush()
				},
				TrimResults: func(toolName, result string) string {
					// Keep read_file results mostly intact — the LLM needs exact content
					// for <edit> search text. Only trim very large files.
					if toolName == "read_file" && len(result) > 6000 {
						preview := safeTruncate(result, 3000)
						return fmt.Sprintf("%s\n...[trimmed, %d chars total]", preview, len(result))
					}
					if toolName == "list_files" && len(result) > 500 {
						lines := strings.Split(result, "\n")
						if len(lines) > 10 {
							return strings.Join(lines[:10], "\n") + fmt.Sprintf("\n...[%d more files]", len(lines)-10)
						}
					}
					if len(result) > 2000 {
						return safeTruncate(result, 500) + fmt.Sprintf("\n...[trimmed, %d chars total]", len(result))
					}
					return result
				},
				TrimArgs: func(toolName, args string) string {
					if len(args) > 1000 {
						var parsed map[string]any
						if json.Unmarshal([]byte(args), &parsed) == nil {
							for k, v := range parsed {
								if s, ok := v.(string); ok && len(s) > 200 {
									parsed[k] = safeTruncate(s, 200) + "...[trimmed]"
								}
							}
							if trimmed, err := json.Marshal(parsed); err == nil {
								return string(trimmed)
							}
						}
					}
					return args
				},
				AfterToolRound: func(allIntercepted bool) string {
					if allIntercepted {
						return ""
					}
					if len(toolAppliedOps) == 0 || len(toolAppliedOps) == lastReportedOpCount {
						return ""
					}
					// Only report files changed since last report
					var paths []string
					seen := make(map[string]bool)
					for _, op := range toolAppliedOps[lastReportedOpCount:] {
						if !seen[op.Path] {
							paths = append(paths, fmt.Sprintf("%s %s", op.Action, op.Path))
							seen[op.Path] = true
						}
					}
					lastReportedOpCount = len(toolAppliedOps)
					if len(paths) == 0 {
						return ""
					}
					return fmt.Sprintf("Files changed this round: %s. Do NOT re-read or re-edit files you already changed — move on to the next file or output your final response.", strings.Join(paths, ", "))
				},
				InterceptToolCall: func(toolName, toolArgs string) (string, bool) {
					// Fast-path read_file from in-memory fileMap (always current).
					// Safe: all fileMap writes (saveGeneratedFile) and reads (here) execute
					// on the same goroutine — StreamWithTools processes tools sequentially.
					if toolName == "read_file" {
						var args struct {
							FilePath string `json:"file_path"`
						}
						if json.Unmarshal([]byte(toolArgs), &args) == nil && args.FilePath != "" {
							if content, ok := fileMap[args.FilePath]; ok {
								return content, true
							}
						}
					}
					return "", false
				},
			},
		)
		parser.Flush()
		responseText = responseBuilder.String()

		// Retry once if there were failed edits
		if len(failedEdits) > 0 && chatErr == nil {
			slog.Info("Retrying failed edits",
				"project_id", project.ID,
				"failed_count", len(failedEdits),
			)
			var retryMsg strings.Builder
			retryMsg.WriteString("Some edits failed to apply because the search text was not found in the current file. Please re-read the affected files and retry these edits:\n\n")
			for _, fe := range failedEdits {
				preview := fe.Search
				if len(preview) > 200 {
					preview = preview[:200] + "..."
				}
				retryMsg.WriteString(fmt.Sprintf("- File: %s\n  Search text (not found): %s\n", fe.Path, preview))
			}

			failedEdits = nil // reset for retry
			retryMessages := make([]utils.Message, len(llmMessages))
			copy(retryMessages, llmMessages)
			retryMessages = append(retryMessages,
				utils.Message{Role: "assistant", Content: responseText},
				utils.Message{Role: "system", Content: retryMsg.String()},
			)

			_, retryErr := utils.StreamWithTools(
				ctx, model, retryMessages, tools, provider,
				nil,
				reasoning,
				executor,
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
					NudgeAfter:       2,
					StripAfter:       3,
					MaxTokens:        65536,
					MaxContinuations: 1,
					NudgeMessage:     "Output your corrected <edit> blocks now. Use the file contents you just read.",
					OnRoundStart: func(round int) {
						parser.Flush()
					},
					InterceptToolCall: func(toolName, toolArgs string) (string, bool) {
						if toolName == "read_file" {
							var args struct {
								FilePath string `json:"file_path"`
							}
							if json.Unmarshal([]byte(toolArgs), &args) == nil && args.FilePath != "" {
								if content, ok := fileMap[args.FilePath]; ok {
									return content, true
								}
							}
						}
						return "", false
					},
				},
			)
			parser.Flush()
			if retryErr != nil && !errors.Is(retryErr, utils.ErrResponseTruncated) {
				slog.Warn("Failed edit retry call failed", "error", retryErr, "project_id", project.ID)
			}
			// Capture retry output into responseText
			responseText = responseBuilder.String()
		}

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
		c.runDeterministicAutoFixes(project, repo, fileMap, sendEvent, &appliedOps, &filesChanged)

		// Augment package.json with discovered dependencies
		pkgChanged := false
		if pkgContent, ok := fileMap["package.json"]; ok {
			augmented := augmentPackageJSON(pkgContent, fileMap)
			if augmented != pkgContent {
				pkgChanged = true
				if writeErr := repo.WriteFile("package.json", []byte(augmented)); writeErr != nil {
					slog.Error("failed to persist augmented package.json", "error", writeErr, "project_id", project.ID)
				} else {
					fileMap["package.json"] = augmented
					slog.Info("Augmented package.json with discovered deps", "project_id", project.ID)
					sendEvent("file_complete", responses.FileCompleteEvent{
						Action: "modify", FilePath: "package.json",
						FileType: "json", Content: augmented,
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

	// --- Auto-detect mobile bridge usage after chat edits ---
	if filesChanged > 0 {
		c.autoDetectMobileBridgeConfig(project, fileMap, sendEvent)
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

	// Extract and persist project memory asynchronously (design prefs, decisions, etc.)
	if !isInitialGeneration && filesChanged > 0 {
		go c.extractAndUpdateProjectMemory(context.Background(), project.ID.String(), provider, message, assistantContent, fileOps)
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
	// Set the flag before commit so credits are deducted for successful generation
	// even if the git commit has issues. If commit fails, revert the flag.
	generationSucceeded = true

	// --- Commit changes to git ---
	if filesChanged > 0 {
		// Write theme to repo before commit
		if err := repo.WriteTheme(project.Theme); err != nil {
			slog.Warn("failed to write theme to repo", "error", err, "project_id", project.ID)
		}
		commitMsg := "init: initial generation"
		if !isInitialGeneration {
			commitMsg = fmt.Sprintf("chat: %s", message)
			if len(commitMsg) > 100 {
				commitMsg = commitMsg[:100]
			}
		}
		if hash, commitErr := repo.Commit(commitMsg); commitErr != nil {
			slog.Error("failed to commit generation changes", "error", commitErr, "project_id", project.ID)
			// Commit failed — revert flag so credits are not deducted for a broken state
			generationSucceeded = false
		} else {
			slog.Info("Committed generation changes", "project_id", project.ID, "hash", hash[:8], "files_changed", filesChanged)
		}
	}

	// --- Invalidate conversation summary after successful generation ---
	// The codebase has changed, so the old summary may reference stale state.
	if generationSucceeded && conv.Summary != "" {
		conv.Summary = ""
		if _, err := c.DB.UpdateAppConversation(conv); err != nil {
			slog.Error("failed to clear conversation summary after generation", "error", err, "conv_id", conv.ID)
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

// --- Helper: inject a template file into git repo and fileMap ---

func (c *Controller) injectTemplateFile(repo *gitrepo.Repo, tp string, content string, fileMap map[string]string, sendEvent func(string, any) bool) {
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

	if err := repo.WriteFile(tp, []byte(content)); err != nil {
		slog.Error("failed to save template file", "error", err, "path", tp)
		return
	}
	fileMap[tp] = content
	sendEvent("file_complete", responses.FileCompleteEvent{
		Action:   "create",
		FilePath: tp,
		FileType: fileType,
		Content:  content,
	})
}

// --- Helper: save a generated file to DB, fileMap, and SSE ---

// saveGeneratedFile persists a generated file to the git repo, updates fileMap,
// emits a file_complete SSE event, and tracks the operation. Used by both the

func (c *Controller) saveGeneratedFile(
	project *models.AppProject,
	repo *gitrepo.Repo,
	path string,
	content string,
	fileType string,
	fileMap map[string]string,
	sendEvent func(string, any) bool,
	ops *[]appBuilderOperation,
	allowProtected ...bool,
) error {
	// Strip parser boundary tags that may have leaked into generated content.
	// Only strip </search> and </replace> which are unique to our parser and
	// extremely unlikely in legitimate code. Avoid stripping </file> and </edit>
	// which could appear in XML/SVG/HTML content.
	for _, tag := range []string{"</search>", "</replace>"} {
		content = strings.ReplaceAll(content, tag, "")
	}

	// Sanitize file path to prevent directory traversal from LLM output.
	cleanPath, valid := utils.SanitizeFilePath(path)
	if !valid {
		slog.Warn("saveGeneratedFile: rejecting suspicious file path", "path", path)
		return nil
	}
	path = cleanPath

	// Guard: never let the LLM overwrite template-managed files during initial generation.
	// Chat edits pass allowProtected=true so the AI can fix errors in template files.
	// Still emit file_complete with the repo content so the sandbox stays in sync.
	canOverride := len(allowProtected) > 0 && allowProtected[0]
	if !canOverride && isProtectedTemplateFile(path) {
		slog.Warn("saveGeneratedFile: skipping protected template file", "path", path)
		if existingContent, readErr := repo.ReadFile(path); readErr == nil {
			sendEvent("file_complete", responses.FileCompleteEvent{
				Action: "modify", FilePath: path,
				FileType: fileType, Content: string(existingContent),
			})
		}
		return nil
	}

	// Guard: block seed routes — the platform handles seeding, not user-facing API routes.
	if strings.HasPrefix(path, "app/api/seed") || strings.HasPrefix(path, "app/api/migrate") {
		slog.Warn("saveGeneratedFile: blocking seed/migrate route", "path", path)
		return nil
	}

	// Guard: e-commerce projects must not have API routes (except auth) or drizzle files.
	// All store data comes from the SDK, not custom API routes.
	if project.ProjectType == "ecommerce" {
		if strings.HasPrefix(path, "app/api/") && !strings.HasPrefix(path, "app/api/auth/") {
			slog.Warn("saveGeneratedFile: blocking API route in e-commerce project", "path", path)
			return nil
		}
		if path == "lib/db.ts" || path == "lib/schema.ts" {
			slog.Warn("saveGeneratedFile: blocking drizzle file in e-commerce project", "path", path)
			return nil
		}
	}

	action := "create"
	if repo.FileExists(path) {
		// Check if content is unchanged
		if existingContent, readErr := repo.ReadFile(path); readErr == nil {
			if normalizeContent(content) == normalizeContent(string(existingContent)) {
				// Content unchanged — still emit file_complete so the sandbox
				// gets re-synced (a previous write may have failed silently).
				sendEvent("file_complete", responses.FileCompleteEvent{
					Action: "modify", FilePath: path,
					FileType: fileType, Content: content,
				})
				return nil
			}
		}
		action = "modify"
	}

	if err := repo.WriteFile(path, []byte(content)); err != nil {
		slog.Error("saveGeneratedFile: failed to write", "path", path, "error", err)
		return fmt.Errorf("saveGeneratedFile: write %s: %w", path, err)
	}

	fileMap[path] = content
	sendEvent("file_complete", responses.FileCompleteEvent{
		Action: action, FilePath: path,
		FileType: fileType, Content: content,
	})
	*ops = append(*ops, appBuilderOperation{
		Action: action, Path: path, Type: fileType,
	})
	return nil
}

// deleteGeneratedFile removes a file from the git repo and fileMap, emitting events.

func (c *Controller) deleteGeneratedFile(
	project *models.AppProject,
	repo *gitrepo.Repo,
	path string,
	fileMap map[string]string,
	sendEvent func(string, any) bool,
	ops *[]appBuilderOperation,
) error {
	cleanPath, valid := utils.SanitizeFilePath(path)
	if !valid {
		slog.Warn("deleteGeneratedFile: rejecting suspicious file path", "path", path)
		return nil
	}
	path = cleanPath

	if !repo.FileExists(path) {
		slog.Warn("deleteGeneratedFile: file not found", "path", path)
		return nil
	}

	_, fileType := guessFileType(path)
	if err := repo.DeleteFile(path); err != nil {
		slog.Error("deleteGeneratedFile: failed to delete", "path", path, "error", err)
		return fmt.Errorf("deleteGeneratedFile: delete %s: %w", path, err)
	}
	delete(fileMap, path)
	sendEvent("file_complete", responses.FileCompleteEvent{
		Action: "delete", FilePath: path,
		FileType: fileType,
	})
	*ops = append(*ops, appBuilderOperation{
		Action: "delete", Path: path, Type: fileType,
	})
	return nil
}
