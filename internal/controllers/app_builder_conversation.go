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
	"github.com/knid/wemob/internal/utils"
)

// Deprecated: summarizeConversation is no longer used by the unified chat path.
// Conversation history uses simple truncation instead.
// Kept temporarily for reference. Remove after verifying unified path works.
//
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

	summary, err := utils.GetTextFromLLM(ctx, c.fixModel(provider), system, conversationText.String(), provider, 1024)
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

// promptDecision holds the result of classifying a user prompt.
type promptDecision struct {
	IsQuestion        bool   `json:"is_question"`
	NeedsConversation bool   `json:"needs_conversation"`
	Greeting          string `json:"greeting"`
}

// classifyPrompt runs a fast LLM call to classify whether the user's message is
// a question, a vague build request, or a ready-to-build prompt. Also produces
// a greeting message when it's a build request (merged to eliminate a separate LLM call).
func (c *Controller) classifyPrompt(
	ctx context.Context,
	provider *utils.LLMProvider,
	message string,
	imageURLs []string,
	webContext string,
	projectType string,
) promptDecision {
	fastModel := provider.FastModel()

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
		return promptDecision{}
	}

	var decision promptDecision
	if err := json.Unmarshal([]byte(decisionJSON), &decision); err != nil {
		cleaned := utils.ExtractJSON(decisionJSON)
		if err2 := json.Unmarshal([]byte(cleaned), &decision); err2 != nil {
			slog.Warn("failed to parse conversation decision", "error", err2)
			return promptDecision{}
		}
	}
	return decision
}

// runConversationLoop takes a pre-computed classification decision and runs the
// appropriate flow: answer a question, run a multi-turn conversation, or return
// the original prompt. Returns the enriched prompt with conversation transcript appended.
func (c *Controller) runConversationLoop(
	ctx context.Context,
	jobID string,
	provider *utils.LLMProvider,
	decision promptDecision,
	message string,
	imageURLs []string,
	webContext string,
	webSummaryText string,
	projectType string,
	sendEvent func(string, any) bool,
) string {
	// Step 1: If the user asked a general question (not a build request),
	// answer it and signal the caller to stop generation.
	if decision.IsQuestion {
		slog.Info("conversation decision: general question detected, answering instead of building", "message", message)
		c.answerGeneralQuestion(ctx, provider, message, imageURLs, sendEvent)
		return "" // empty string signals caller to abort generation
	}

	// Step 2: If no conversation needed, return original prompt
	if !decision.NeedsConversation {
		return message
	}

	fastModel := provider.FastModel()

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

		// Persist resolution of the awaiting_reply milestone so reconnects don't
		// see a stale awaiting banner after the user has already replied / timed out.
		sendEvent("clarification_received", map[string]any{
			"job_id": jobID,
			"turn":   turn,
		})

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


// streamBuildPlan is deprecated — greeting is now merged into classifyPrompt.
// Kept as a no-op for any remaining callers during transition.

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

