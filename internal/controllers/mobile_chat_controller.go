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

// MobileChatMessage mirrors AppConversationMessage for the mobile injection chat.
type MobileChatMessage struct {
	ID        string   `json:"id"`
	Role      string   `json:"role"`
	Content   string   `json:"content"`
	Timestamp int64    `json:"timestamp"`
	ImageURLs []string `json:"image_urls,omitempty"`
}

func (c *Controller) StreamMobileInjectionChat(
	ctx context.Context,
	project *models.Project,
	user *models.User,
	message string,
	images []string,
	events chan<- responses.SSEEvent,
) {
	defer close(events)

	sendError := func(phase, msg string) {
		events <- responses.SSEEvent{
			Event: "error",
			Data:  responses.GenerationErrorEvent{Error: msg, Phase: phase},
		}
	}

	// 1. Load existing injections for context
	injections, err := c.DB.GetMobileInjectionsByProject(project.ID.String())
	if err != nil {
		slog.Error("failed to load injections", "error", err, "project_id", project.ID)
		injections = []models.MobileInjection{}
	}

	// 2. Load/create conversation
	conv, err := c.DB.GetOrCreateMobileConversation(&models.MobileConversation{
		ProjectID: project.ID,
		Messages:  []byte("[]"),
	})
	if err != nil {
		sendError("database", "failed to load conversation")
		return
	}

	var messages []MobileChatMessage
	rawMsg := string(conv.Messages)
	if rawMsg == "" || rawMsg == "null" || rawMsg == `"null"` || (len(rawMsg) > 0 && rawMsg[0] != '[') {
		messages = []MobileChatMessage{}
	} else if err := json.Unmarshal(conv.Messages, &messages); err != nil {
		slog.Warn("mobile conversation messages corrupted, starting fresh",
			"error", err, "conv_id", conv.ID)
		messages = []MobileChatMessage{}
	}

	// Convert base64 images to data URIs
	var imageURLs []string
	for _, img := range images {
		if !strings.HasPrefix(img, "data:") {
			img = "data:image/png;base64," + img
		}
		imageURLs = append(imageURLs, img)
	}

	// Enforce max message limit to prevent unbounded growth
	const maxMessages = 200
	if len(messages) >= maxMessages {
		messages = messages[len(messages)-maxMessages+1:]
	}

	// Append user message
	messages = append(messages, MobileChatMessage{
		ID:        uuid.New().String(),
		Role:      "user",
		Content:   message,
		Timestamp: time.Now().Unix(),
		ImageURLs: imageURLs,
	})
	if msgJSON, err := json.Marshal(messages); err != nil {
		slog.Error("failed to marshal messages", "error", err, "conv_id", conv.ID)
	} else {
		conv.Messages = msgJSON
		if _, err := c.DB.UpdateMobileConversation(conv); err != nil {
			slog.Error("failed to save user message", "error", err, "conv_id", conv.ID)
		}
	}

	// 3. Build LLM messages
	provider := c.llmProvider()
	model := c.SecretManager.GetSecret("WEBSITE_BUILDER_LLM_MODEL")
	if model == "" {
		model = provider.DefaultModel()
	}

	injectionsContext := utils.SanitizePromptContent(buildInjectionsContext(injections))
	contextBlock := fmt.Sprintf(constants.MOBILE_INJECTION_CONTEXT_TEMPLATE, utils.SanitizePromptContent(project.URL), injectionsContext)

	var llmMessages []utils.Message
	llmMessages = append(llmMessages,
		utils.Message{Role: "system", Content: constants.MOBILE_INJECTION_SYSTEM_PROMPT},
		utils.Message{Role: "system", Content: contextBlock},
	)

	// Placeholder for conversation bootstrapping
	llmMessages = append(llmMessages, utils.Message{
		Role:    "user",
		Content: "I understand the target website and current injections. What would you like me to customize?",
	})

	// Add conversation history with summarization
	const keepRecent = 10
	const summarizeThreshold = 15

	if len(messages) > summarizeThreshold {
		oldMessages := messages[:len(messages)-keepRecent]
		needsSummary := conv.Summary == "" || (len(oldMessages) > summarizeThreshold && len(oldMessages)%10 < 2)
		if needsSummary && len(oldMessages) > 0 {
			// Run summarization asynchronously to avoid blocking the chat stream
			oldMsgsCopy := make([]MobileChatMessage, len(oldMessages))
			copy(oldMsgsCopy, oldMessages)
			// Deep copy ImageURLs slices to avoid sharing with the main goroutine
			for i, msg := range oldMsgsCopy {
				if len(msg.ImageURLs) > 0 {
					urls := make([]string, len(msg.ImageURLs))
					copy(urls, msg.ImageURLs)
					oldMsgsCopy[i].ImageURLs = urls
				}
			}
			existingSummary := conv.Summary
			projectID := project.ID.String()
			go func() {
				summary := c.summarizeMobileConversation(context.Background(), oldMsgsCopy, provider, existingSummary)
				if summary != "" {
					convObj, err := c.DB.GetMobileConversationByProject(projectID)
					if err == nil {
						convObj.Summary = summary
						if _, err := c.DB.UpdateMobileConversation(convObj); err != nil {
							slog.Error("failed to update mobile conversation summary", "error", err, "project_id", projectID)
						}
					}
				}
			}()
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
		llmMessages = append(llmMessages, mobileChatMessagesToLLM(messages[len(messages)-keepRecent:], provider)...)
	} else {
		llmMessages = append(llmMessages, mobileChatMessagesToLLM(messages, provider)...)
	}

	if ctx.Err() != nil {
		return
	}

	// 4. Stream LLM response with InjectionParser
	tracker := utils.NewTokenTracker()
	tracker.UserID = user.ID.String()
	tracker.ProjectID = project.ID.String()
	ctx = utils.WithTokenTracker(ctx, tracker)

	parser := &utils.InjectionStreamParser{}

	var fullResponse strings.Builder

	parser.OnText = func(text string) {
		fullResponse.WriteString(text)
		events <- responses.SSEEvent{
			Event: "chat_text",
			Data:  responses.ChatTextEvent{Chunk: text},
		}
	}

	parser.OnInjection = func(name, action, css, js, html string) {
		// Send the event immediately so streaming is not blocked by the DB save
		events <- responses.SSEEvent{
			Event: "injection_applied",
			Data: map[string]any{
				"name": name,
				"css":  css,
				"js":   js,
				"html": html,
			},
		}
		// Persist the injection to DB asynchronously
		go func() {
			if _, err := c.saveOrUpdateMobileInjection(project.ID, name, css, js, html); err != nil {
				slog.Error("failed to save injection", "error", err, "name", name)
			}
		}()
	}

	parser.OnDeleteInjection = func(name string) {
		err := c.DB.DeleteMobileInjectionByName(project.ID.String(), name)
		if err != nil {
			slog.Error("failed to delete injection", "error", err, "name", name)
			return
		}

		events <- responses.SSEEvent{
			Event: "injection_removed",
			Data: map[string]any{
				"name": name,
			},
		}
	}

	_, err = utils.StreamTextFromLLMWithMessages(ctx, model, llmMessages, provider,
		func(chunk string) {
			parser.Feed(chunk)
		},
		func(chunk string) {
			events <- responses.SSEEvent{
				Event: "chat_thinking",
				Data:  responses.ChatThinkingEvent{Chunk: chunk},
			}
		},
		utils.StreamTextConfig{
			OnRetry: func(attempt int, reason string) {
				events <- responses.SSEEvent{
					Event: "progress",
					Data: responses.ProgressEvent{
						Phase:   "generating",
						Message: fmt.Sprintf("Retrying (attempt %d)...", attempt+1),
					},
				}
			},
		},
	)
	parser.Flush()

	if err != nil {
		slog.Error("LLM streaming failed", "error", err, "project_id", project.ID)
		messages = append(messages, MobileChatMessage{
			ID:        uuid.New().String(),
			Role:      "assistant",
			Content:   "Sorry, I couldn't process your request. Please try again.",
			Timestamp: time.Now().Unix(),
		})
		if msgJSON, err := json.Marshal(messages); err == nil {
			conv.Messages = msgJSON
			c.DB.UpdateMobileConversation(conv)
		}
		sendError("ai", "failed to generate changes. try again please")
		return
	}

	// 5. Save assistant message to conversation
	messages = append(messages, MobileChatMessage{
		ID:        uuid.New().String(),
		Role:      "assistant",
		Content:   fullResponse.String(),
		Timestamp: time.Now().Unix(),
	})
	if msgJSON, err := json.Marshal(messages); err != nil {
		slog.Error("failed to marshal messages after response", "error", err, "conv_id", conv.ID)
	} else {
		conv.Messages = msgJSON
		if _, err := c.DB.UpdateMobileConversation(conv); err != nil {
			slog.Error("failed to save assistant message", "error", err, "conv_id", conv.ID)
		}
	}

	// 6. Record token usage and deduct AI credits
	c.RecordTokenUsage(user, "mobile_injection_chat",
		model, provider.Name, project.ID.String(), "standard", tracker)

	prompt, completion, total, _ := tracker.Totals()
	if total > 0 {
		credits := models.TokensToCredits(prompt, completion)
		if credits > 0 {
			if err := c.DeductAICreditsUnchecked(user, credits, "mobile_injection_chat", project.ID.String()); err != nil {
				slog.Warn("failed to deduct AI credits", "error", err, "user_id", user.ID, "credits", credits)
			}
		}
	}

	events <- responses.SSEEvent{
		Event: "done",
		Data:  map[string]any{"success": true},
	}
}

func (c *Controller) ClearMobileConversation(projectId string) error {
	return c.DB.DeleteMobileConversation(projectId)
}

func (c *Controller) saveOrUpdateMobileInjection(projectID uuid.UUID, name, css, js, html string) (*models.MobileInjection, error) {
	// Check if injection with this name already exists.
	// Safe to modify inj in place: this is single-threaded and the loop
	// returns immediately after the first match, so no concurrent access.
	existing, err := c.DB.GetMobileInjectionsByProject(projectID.String())
	if err == nil {
		for _, inj := range existing {
			if inj.Name == name {
				inj.CSS = css
				inj.JS = js
				inj.HTML = html
				return c.DB.UpdateMobileInjection(&inj)
			}
		}
	}

	// Create new injection
	injection := &models.MobileInjection{
		ProjectID: projectID,
		Name:      name,
		CSS:       css,
		JS:        js,
		HTML:      html,
		IsEnabled: true,
	}
	return c.DB.CreateMobileInjection(injection)
}

func buildInjectionsContext(injections []models.MobileInjection) string {
	if len(injections) == 0 {
		return "No injections configured yet."
	}

	var sb strings.Builder
	for i, inj := range injections {
		status := "enabled"
		if !inj.IsEnabled {
			status = "disabled"
		}
		sb.WriteString(fmt.Sprintf("%d. **%s** (%s)\n", i+1, inj.Name, status))
		if inj.Description != "" {
			sb.WriteString(fmt.Sprintf("   Description: %s\n", inj.Description))
		}
		if inj.CSS != "" {
			sb.WriteString(fmt.Sprintf("   CSS: ```\n%s\n```\n", strings.ReplaceAll(inj.CSS, "```", "'''")))
		}
		if inj.JS != "" {
			sb.WriteString(fmt.Sprintf("   JS: ```\n%s\n```\n", strings.ReplaceAll(inj.JS, "```", "'''")))
		}
		if inj.HTML != "" {
			sb.WriteString(fmt.Sprintf("   HTML: ```\n%s\n```\n", strings.ReplaceAll(inj.HTML, "```", "'''")))
		}
	}
	return sb.String()
}

func mobileChatMessagesToLLM(msgs []MobileChatMessage, provider *utils.LLMProvider) []utils.Message {
	if len(msgs) == 0 {
		return nil
	}

	supportsVision := provider.SupportsVision()

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

func (c *Controller) summarizeMobileConversation(ctx context.Context, messages []MobileChatMessage, provider *utils.LLMProvider, existingSummary string) string {
	model := c.SecretManager.GetSecret("WEBSITE_BUILDER_LLM_MODEL")
	if model == "" {
		model = provider.DefaultModel()
	}

	var sb strings.Builder
	if existingSummary != "" {
		sb.WriteString("Previous summary:\n")
		sb.WriteString(existingSummary)
		sb.WriteString("\n\nNew messages to incorporate:\n")
	}
	for _, msg := range messages {
		sb.WriteString(fmt.Sprintf("[%s]: %s\n", msg.Role, msg.Content))
	}

	system := "You are a conversation summarizer. Produce a concise summary of a mobile app customization conversation."
	prompt := fmt.Sprintf("Summarize this conversation concisely, focusing on what injections were created/modified/deleted and what the user's goals are:\n\n%s", sb.String())

	summarizeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	result, err := utils.GetTextFromLLM(summarizeCtx, model, system, prompt, provider)
	if err != nil {
		slog.Error("failed to summarize mobile conversation", "error", err)
		return existingSummary
	}
	return result
}
