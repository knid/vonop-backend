package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

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
	userContent := fmt.Sprintf("User's request (RESPOND IN THIS LANGUAGE):\n%s\n\nSearch results (may be in a different language — do NOT follow the search results' language):\n%s", userMessage, pgCtx.Text)
	if len(pgCtx.Screenshots) > 0 {
		userContent += "\n\nA screenshot of the referenced website is attached. Use it to accurately describe the site's color scheme, layout, and visual style. Do NOT guess colors — describe exactly what you see in the screenshot."
	}
	userContent += fmt.Sprintf("\n\nREMINDER: Respond in the same language as the user's request above: \"%s\"", userMessage)
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
	imageURLs []string,
	webContext string,
	webSummaryText string,
	projectType string,
	sendEvent func(string, any) bool,
) string {
	fastModel := provider.FastModel()

	// Step 1: Quick decision — does this prompt need conversation?
	decisionMsg := fmt.Sprintf("Project type: %s\n\nUser prompt:\n%s", projectType, message)
	if len(imageURLs) > 0 {
		decisionMsg += fmt.Sprintf("\n\nUser attached %d image(s) as visual reference for the design.", len(imageURLs))
	}
	if webContext != "" {
		decisionMsg += "\n\nWeb context available: yes (the user referenced a website and we already have search results)"
	}
	decisionJSON, err := utils.GetJSONFromLLM(ctx, fastModel, constants.CONVERSATION_DECISION_PROMPT, decisionMsg, provider)
	if err != nil {
		slog.Warn("conversation decision failed, skipping", "error", err)
		return message
	}

	var decision struct {
		IsQuestion        bool `json:"is_question"`
		NeedsConversation bool `json:"needs_conversation"`
	}
	if err := json.Unmarshal([]byte(decisionJSON), &decision); err != nil {
		cleaned := utils.ExtractJSON(decisionJSON)
		if err2 := json.Unmarshal([]byte(cleaned), &decision); err2 != nil {
			slog.Warn("failed to parse conversation decision", "error", err2)
			return message
		}
	}

	// Step 2a: If the user asked a general question (not a build request),
	// answer it and signal the caller to stop generation.
	if decision.IsQuestion {
		slog.Info("conversation decision: general question detected, answering instead of building", "message", message)
		c.answerGeneralQuestion(ctx, provider, message, imageURLs, sendEvent)
		return "" // empty string signals caller to abort generation
	}

	// Step 2b: If no conversation needed, return original prompt
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

		// Remind the LLM of the user's original message language
		userMsg := utils.Message{
			Role:    "user",
			Content: contextPrefix + fmt.Sprintf("\nIMPORTANT: The user's original message was: \"%s\" — respond in the SAME language as this message.\nAsk your next question(s) to clarify what to build.", message),
		}
		// Attach images on the first turn so the fast model can see user-provided screenshots
		if turn == 0 && len(imageURLs) > 0 && provider.SupportsVision() {
			userMsg.Images = imageURLs
		}
		llmMessages = append(llmMessages, userMsg)

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

// answerGeneralQuestion streams a helpful answer when the user asks a general

func (c *Controller) answerGeneralQuestion(
	ctx context.Context,
	provider *utils.LLMProvider,
	message string,
	imageURLs []string,
	sendEvent func(string, any) bool,
) {
	fastModel := provider.FastModel()
	systemPrompt := `You are Wemob AI, a friendly app builder assistant. The user asked a general question instead of requesting an app to be built.

Answer their question helpfully and concisely. Mention your key capabilities:
- Building full-stack applications from a text description or screenshot
- E-commerce stores with product management, cart, and checkout
- Portfolio sites, blogs, dashboards, landing pages, and more
- Real-time preview and iterative editing via chat
- Database integration, authentication, custom domains, SEO

Rules:
- CRITICAL LANGUAGE RULE: You MUST respond in the SAME LANGUAGE as the user's message. If the user wrote in English, respond in English. If in Turkish, respond in Turkish. Match their language exactly.
- Be warm, conversational, and enthusiastic
- Keep it to 3-5 sentences max
- End by encouraging them to describe what they'd like to build
- No markdown headers, no bullet points — just natural text
- NEVER mention specific technical frameworks like Next.js, React, or TypeScript to the user`

	userMsg := utils.Message{Role: "user", Content: message}
	if len(imageURLs) > 0 && provider.SupportsVision() {
		userMsg.Images = imageURLs
	}

	messages := []utils.Message{
		{Role: "system", Content: systemPrompt},
		userMsg,
	}

	_, err := utils.StreamTextFromLLMWithMessages(
		ctx, fastModel, messages, provider,
		func(chunk string) {
			sendEvent("conversation_chunk", map[string]any{"chunk": chunk})
		},
		nil, // no reasoning
		utils.StreamTextConfig{MaxTokens: 512},
	)
	if err != nil {
		slog.Warn("failed to answer general question", "error", err)
	}
	sendEvent("conversation_done", map[string]any{"type": "general_answer"})
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
	sb.WriteString("\n\n--- Pre-build conversation (use for context, but generate all app content in the SAME language as the original prompt above) ---\n")
	for _, t := range history[1:] { // skip the first entry which is the original prompt
		if t.Role == "assistant" {
			sb.WriteString(fmt.Sprintf("AI: %s\n", t.Content))
		} else {
			sb.WriteString(fmt.Sprintf("User: %s\n", t.Content))
		}
	}
	return sb.String()
}


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

// --- Project memory extraction ---

// projectMemoryData holds the structured memory extracted from a conversation turn.
type projectMemoryData struct {
	DesignPreferences  []string `json:"design_preferences"`
	RejectedApproaches []string `json:"rejected_approaches"`
	TechnicalDecisions []string `json:"technical_decisions"`
	UserPreferences    []string `json:"user_preferences"`
}

// extractAndUpdateProjectMemory runs a fast LLM call to extract structured memory
// from the latest user+assistant exchange, then merges it into the persisted
// project memory. Runs asynchronously — callers should go this function.
func (c *Controller) extractAndUpdateProjectMemory(
	ctx context.Context,
	projectID string,
	provider *utils.LLMProvider,
	userMessage string,
	assistantResponse string,
	fileOps []models.AppChatFileOp,
) {
	// Only extract if there's meaningful content
	if len(userMessage) < 10 || len(assistantResponse) < 10 {
		return
	}

	fastModel := provider.FastModel()

	// Build a concise summary of what happened
	var opsDesc string
	if len(fileOps) > 0 {
		var ops []string
		for _, op := range fileOps {
			ops = append(ops, fmt.Sprintf("%s %s", op.Action, op.FilePath))
		}
		opsDesc = fmt.Sprintf("\nFiles changed: %s", strings.Join(ops, ", "))
	}

	prompt := fmt.Sprintf(`Extract any lasting insights from this conversation turn that would help future AI sessions with this project.

User said: %s

Assistant responded: %s%s

Return a JSON object with these arrays (each containing short, specific strings). Only include entries that are NEW learnings — not obvious facts. Return empty arrays if nothing notable:
{
  "design_preferences": ["user's stated design likes/dislikes, e.g. 'prefers dark theme', 'no hero background images'"],
  "rejected_approaches": ["approaches tried and rejected, e.g. 'sidebar nav rejected, user wanted top nav'"],
  "technical_decisions": ["tech choices made, e.g. 'using Stripe for payments', 'PostgreSQL with Drizzle ORM'"],
  "user_preferences": ["communication/workflow prefs, e.g. 'prefers Turkish UI text', 'wants minimal explanations'"]
}`, truncate(userMessage, 500), truncate(assistantResponse, 500), opsDesc)

	resultJSON, err := utils.GetJSONFromLLM(ctx, fastModel, "", prompt, provider)
	if err != nil {
		slog.Debug("extractProjectMemory: LLM call failed", "error", err, "project_id", projectID)
		return
	}

	var extracted projectMemoryData
	if err := json.Unmarshal([]byte(resultJSON), &extracted); err != nil {
		cleaned := utils.ExtractJSON(resultJSON)
		if err2 := json.Unmarshal([]byte(cleaned), &extracted); err2 != nil {
			return
		}
	}

	// Check if there's anything to save
	if len(extracted.DesignPreferences) == 0 && len(extracted.RejectedApproaches) == 0 &&
		len(extracted.TechnicalDecisions) == 0 && len(extracted.UserPreferences) == 0 {
		return
	}

	// Load existing memory and merge
	mem, err := c.DB.GetOrCreateAppProjectMemory(uuid.Must(uuid.Parse(projectID)))
	if err != nil {
		slog.Warn("extractProjectMemory: failed to load memory", "error", err, "project_id", projectID)
		return
	}

	mergeMemoryField(&mem.DesignPreferences, extracted.DesignPreferences, 20)
	mergeMemoryField(&mem.RejectedApproaches, extracted.RejectedApproaches, 15)
	mergeMemoryField(&mem.TechnicalDecisions, extracted.TechnicalDecisions, 20)
	mergeMemoryField(&mem.UserPreferences, extracted.UserPreferences, 10)

	if err := c.DB.UpdateAppProjectMemory(mem); err != nil {
		slog.Warn("extractProjectMemory: failed to save memory", "error", err, "project_id", projectID)
	} else {
		slog.Info("extractProjectMemory: updated project memory", "project_id", projectID)
	}
}

// mergeMemoryField appends new entries to a JSONB []string field, deduplicating
// and capping at maxEntries (FIFO — oldest entries dropped).
func mergeMemoryField(field *[]byte, newEntries []string, maxEntries int) {
	if len(newEntries) == 0 {
		return
	}

	var existing []string
	if len(*field) > 0 {
		json.Unmarshal(*field, &existing)
	}

	// Deduplicate by checking lowercase containment
	seen := make(map[string]bool, len(existing))
	for _, e := range existing {
		seen[strings.ToLower(e)] = true
	}
	for _, entry := range newEntries {
		entry = strings.TrimSpace(entry)
		if entry == "" || seen[strings.ToLower(entry)] {
			continue
		}
		existing = append(existing, entry)
		seen[strings.ToLower(entry)] = true
	}

	// Cap at maxEntries — keep the most recent
	if len(existing) > maxEntries {
		existing = existing[len(existing)-maxEntries:]
	}

	if data, err := json.Marshal(existing); err == nil {
		*field = data
	}
}

// buildProjectMemoryContext returns a formatted string of the project's persisted
// memory for injection into the system prompt. Returns "" if no memory exists.
func (c *Controller) buildProjectMemoryContext(projectID string) string {
	mem, err := c.DB.GetAppProjectMemory(projectID)
	if err != nil {
		return ""
	}

	var sections []string

	appendSection := func(label string, data []byte) {
		var items []string
		if json.Unmarshal(data, &items) != nil || len(items) == 0 {
			return
		}
		var sb strings.Builder
		sb.WriteString(label + ":\n")
		for _, item := range items {
			sb.WriteString("- " + item + "\n")
		}
		sections = append(sections, sb.String())
	}

	appendSection("Design preferences (from past conversations)", mem.DesignPreferences)
	appendSection("Approaches tried and rejected", mem.RejectedApproaches)
	appendSection("Technical decisions made", mem.TechnicalDecisions)
	appendSection("User preferences", mem.UserPreferences)

	if len(sections) == 0 {
		return ""
	}

	return "\n### Project Memory (from past sessions)\n" + strings.Join(sections, "\n")
}

// mobileOptimizeFile is a lightweight struct for files selected for mobile optimization.
type mobileOptimizeFile struct {
	Path    string
	Content string
}

// selectMobileOptimizeFilesFromMap filters project files to those likely containing
// UI/layout code that needs mobile conversion. API routes, lib utilities,
// type definitions, and config files are excluded.
func selectMobileOptimizeFilesFromMap(fileMap map[string]string) []mobileOptimizeFile {
	var selected []mobileOptimizeFile
	for path, content := range fileMap {
		// Exclude non-UI files
		if strings.HasPrefix(path, "app/api/") ||
			strings.HasPrefix(path, "lib/db") ||
			strings.HasPrefix(path, "lib/schema") ||
			strings.HasPrefix(path, "types/") ||
			strings.HasSuffix(path, ".config.ts") ||
			strings.HasSuffix(path, ".config.mjs") ||
			strings.HasSuffix(path, ".config.js") ||
			strings.HasSuffix(path, ".json") ||
			strings.HasSuffix(path, ".css") ||
			path == "middleware.ts" ||
			path == "tailwind.config.ts" ||
			path == "next.config.mjs" ||
			path == "postcss.config.mjs" ||
			path == "tsconfig.json" {
			continue
		}

		// Include pages, layouts, and components
		isUI := strings.HasSuffix(path, "/page.tsx") ||
			strings.HasSuffix(path, "/layout.tsx") ||
			strings.Contains(path, "components/")

		// Include files with desktop patterns
		hasDesktop := strings.Contains(content, "sm:") ||
			strings.Contains(content, "md:") ||
			strings.Contains(content, "lg:") ||
			strings.Contains(content, "xl:") ||
			strings.Contains(content, "<nav") ||
			strings.Contains(content, "sidebar") ||
			strings.Contains(content, "Sidebar") ||
			strings.Contains(content, "grid-cols-")

		if isUI || hasDesktop {
			selected = append(selected, mobileOptimizeFile{Path: path, Content: content})
		}
	}
	return selected
}

// StreamMobileOptimize reads the project's cloned web files, sends them to the
// LLM with mobile conversion instructions, and streams back modified files.
// It follows the same SSE pattern as StreamBuildApp (file-block streaming).
func (c *Controller) StreamMobileOptimize(
	ctx context.Context,
	project *models.AppProject,
	user *models.User,
	events chan<- responses.SSEEvent,
) {
	defer close(events)

	sendEvent := func(eventName string, data any) bool {
		select {
		case <-ctx.Done():
			return false
		case events <- responses.SSEEvent{Event: eventName, Data: data}:
			return true
		}
	}
	sendError := func(phase, msg string) {
		events <- responses.SSEEvent{
			Event: "error",
			Data:  responses.GenerationErrorEvent{Error: msg, Phase: phase},
		}
	}

	// 1. Set status to optimizing
	project.Status = "optimizing"
	if _, err := c.DB.UpdateAppProject(project); err != nil {
		slog.Error("failed to set project status to optimizing", "error", err, "project_id", project.ID)
	}

	resetStatus := func() {
		project.Status = "ready"
		if _, err := c.DB.UpdateAppProject(project); err != nil {
			slog.Error("failed to reset project status", "error", err, "project_id", project.ID)
		}
	}

	// 2. Open repo and load all files
	repo, err := c.GitRepoService.Open(project.ID.String())
	if err != nil {
		slog.Error("mobile optimize: failed to open repo", "error", err, "project_id", project.ID)
		resetStatus()
		sendError("load", "failed to load project files")
		return
	}
	fileMap, err := repo.ReadAllFiles()
	if err != nil || len(fileMap) == 0 {
		slog.Error("mobile optimize: failed to load files", "error", err, "project_id", project.ID)
		resetStatus()
		sendError("load", "failed to load project files")
		return
	}

	// 3. Create auto-snapshot before changes
	preCommitHash, snapErr := c.CreateAppSnapshot(project, "Auto-save before mobile optimization", "auto")
	if snapErr != nil {
		slog.Error("mobile optimize: failed to create snapshot", "error", snapErr, "project_id", project.ID)
		resetStatus()
		sendError("snapshot", "failed to create safety snapshot. please try again")
		return
	}
	preSnapshotID := preCommitHash

	// 4. Filter to UI files
	uiFiles := selectMobileOptimizeFilesFromMap(fileMap)
	if len(uiFiles) == 0 {
		slog.Info("mobile optimize: no UI files to optimize", "project_id", project.ID)
		resetStatus()
		sendEvent("done", map[string]any{"success": true, "files_changed": 0})
		return
	}

	// Token tracking for billing
	tracker := utils.NewTokenTracker()
	tracker.UserID = user.ID.String()
	tracker.ProjectID = project.ID.String()
	ctx = utils.WithTokenTracker(ctx, tracker)

	// 5. Build file listing for system prompt (all files for context)
	var fileListing strings.Builder
	fileListing.WriteString("\n\n## Project Files\n")
	for path := range fileMap {
		fileListing.WriteString(fmt.Sprintf("- %s (%s)\n", path, InferFileType(path)))
	}

	// Build user message with selected file contents
	var userContent strings.Builder
	userContent.WriteString("Convert the following files to mobile-only layout. Only output files that need changes.\n\n")
	for _, uf := range uiFiles {
		userContent.WriteString(fmt.Sprintf("<file path=\"%s\">\n%s\n</file>\n\n", uf.Path, uf.Content))
	}

	provider := c.llmProvider()
	model := c.SecretManager.GetSecret("WEBSITE_BUILDER_LLM_MODEL")
	if model == "" {
		model = provider.DefaultModel()
	}

	llmMessages := []utils.Message{
		{Role: "system", Content: constants.NEXTJS_MOBILE_OPTIMIZE_PROMPT + fileListing.String()},
		{Role: "user", Content: userContent.String()},
	}

	// 6. Stream LLM response with FileStreamParser
	var ops []appBuilderOperation
	fileCounter := 0
	var responseBuilder strings.Builder

	parser := &utils.FileStreamParser{
		DiscardIncomplete: true,
		OnFile: func(path, content string) {
			content = utils.StripCodeFences(content)
			_, fileType := guessFileType(path)
			if fileType == "tsx" || fileType == "ts" {
				content, _ = stripForbiddenImports(content, path)
			}
			if err := c.saveGeneratedFile(project, repo, path, content, fileType, fileMap, sendEvent, &ops, true); err != nil {
				slog.Error("optimization file save failed", "path", path, "error", err)
			}
			fileCounter++
			sendEvent("progress", responses.ProgressEvent{
				Phase:   "optimizing",
				Message: fmt.Sprintf("Optimized %s (%d files)", path, fileCounter),
				Current: fileCounter,
			})
		},
		OnDelete: func(path string) {
			if err := c.deleteGeneratedFile(project, repo, path, fileMap, sendEvent, &ops); err != nil {
				slog.Error("optimization file delete failed", "path", path, "error", err)
			}
		},
		OnText: func(text string) {
			responseBuilder.WriteString(text)
			cleaned := ansiRegex.ReplaceAllString(text, "")
			if cleaned != "" {
				sendEvent("chat_text", responses.ChatTextEvent{Chunk: cleaned})
			}
		},
	}

	slog.Info("Starting mobile optimization LLM call",
		"model", model,
		"project_id", project.ID,
		"ui_files", len(uiFiles),
		"total_files", len(fileMap),
	)

	_, llmErr := utils.StreamTextFromLLMWithMessages(
		ctx, model, llmMessages, provider,
		func(chunk string) { parser.Feed(chunk) },
		func(chunk string) {
			sendEvent("chat_thinking", responses.ChatThinkingEvent{Chunk: chunk})
		},
		utils.StreamTextConfig{MaxTokens: 16384},
	)
	parser.Flush()

	if llmErr != nil {
		slog.Error("mobile optimize LLM call failed", "error", llmErr, "project_id", project.ID)

		// Auto-rollback on failure
		if len(ops) > 0 {
			slog.Info("mobile optimize: rolling back after failure",
				"project_id", project.ID,
				"snapshot_id", preSnapshotID,
				"partial_ops", len(ops),
			)
			if _, rollbackErr := repo.RevertAndCommit(preSnapshotID, "auto: rollback after mobile optimization failure"); rollbackErr != nil {
				slog.Error("mobile optimize: rollback failed", "error", rollbackErr, "project_id", project.ID)
			} else {
				sendEvent("rollback", map[string]any{
					"snapshot_id":     preSnapshotID,
					"reason":          "optimization_failed",
					"rolled_back":     len(ops),
					"resync_required": true,
				})
			}
		}

		resetStatus()
		sendError("ai", "mobile optimization failed. your files have been restored")
		return
	}

	// 7. Set status back to ready and send done
	resetStatus()

	// 8. Record token usage and deduct credits
	c.RecordTokenUsage(user, "mobile_optimize",
		model, provider.Name, project.ID.String(), "standard", tracker)

	prompt, completion, total, _ := tracker.Totals()
	if total > 0 {
		credits := models.TokensToCredits(prompt, completion)
		if err := c.DeductAICreditsUnchecked(user, credits, "mobile_optimize", project.ID.String()); err != nil {
			slog.Warn("failed to deduct AI credits", "error", err, "user_id", user.ID, "credits", credits)
		}
	}

	sendEvent("done", map[string]any{
		"success":       true,
		"files_changed": len(ops),
		"snapshot_id":   preSnapshotID,
	})
}
