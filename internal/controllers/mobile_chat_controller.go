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

	injectionsContext := buildInjectionsContext(injections)
	contextBlock := fmt.Sprintf(constants.MOBILE_INJECTION_CONTEXT_TEMPLATE, project.URL, injectionsContext)

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
			conv.Summary = c.summarizeMobileConversation(ctx, oldMessages, provider, conv.Summary)
			if msgJSON, err := json.Marshal(messages); err == nil {
				conv.Messages = msgJSON
				c.DB.UpdateMobileConversation(conv)
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
		llmMessages = append(llmMessages, mobileChatMessagesToLLM(messages[len(messages)-keepRecent:], provider)...)
	} else {
		llmMessages = append(llmMessages, mobileChatMessagesToLLM(messages, provider)...)
	}

	if ctx.Err() != nil {
		return
	}

	// 4. Stream LLM response with InjectionParser
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
		// Save/update injection in DB
		injection, err := c.saveOrUpdateMobileInjection(project.ID, name, css, js, html)
		if err != nil {
			slog.Error("failed to save injection", "error", err, "name", name)
			return
		}

		events <- responses.SSEEvent{
			Event: "injection_applied",
			Data: map[string]any{
				"id":   injection.ID,
				"name": name,
				"css":  css,
				"js":   js,
				"html": html,
			},
		}
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

	// 6. Deduct AI credits
	c.DeductAICreditsUnchecked(user, 1, "mobile_injection_chat", project.ID.String())

	events <- responses.SSEEvent{
		Event: "done",
		Data:  map[string]any{"success": true},
	}
}

func (c *Controller) ClearMobileConversation(projectId string) error {
	return c.DB.DeleteMobileConversation(projectId)
}

func (c *Controller) saveOrUpdateMobileInjection(projectID uuid.UUID, name, css, js, html string) (*models.MobileInjection, error) {
	// Check if injection with this name already exists
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
			sb.WriteString(fmt.Sprintf("   CSS: ```%s```\n", inj.CSS))
		}
		if inj.JS != "" {
			sb.WriteString(fmt.Sprintf("   JS: ```%s```\n", inj.JS))
		}
		if inj.HTML != "" {
			sb.WriteString(fmt.Sprintf("   HTML: ```%s```\n", inj.HTML))
		}
	}
	return sb.String()
}

func mobileChatMessagesToLLM(msgs []MobileChatMessage, provider *utils.LLMProvider) []utils.Message {
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

	result, err := utils.GetTextFromLLM(ctx, model, system, prompt, provider)
	if err != nil {
		slog.Error("failed to summarize mobile conversation", "error", err)
		return existingSummary
	}
	return result
}
