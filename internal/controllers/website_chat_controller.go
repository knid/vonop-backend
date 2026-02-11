package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

type chatOperation struct {
	Action  string `json:"action"`
	Path    string `json:"path"`
	Type    string `json:"type"`
	Content string `json:"content"`
}

type chatResponse struct {
	Operations []chatOperation `json:"operations"`
	Summary    string          `json:"summary"`
}

func (c *Controller) GetConversation(user *models.User, websiteId string) (*responses.ConversationResponse, error) {
	_, err := c.verifyWebsiteOwnership(user, websiteId)
	if err != nil {
		return nil, err
	}

	conversation, err := c.DB.GetConversationByWebsiteId(websiteId)
	if err != nil {
		// No conversation yet — return empty
		return &responses.ConversationResponse{
			Messages: []responses.ConversationMessageDTO{},
		}, nil
	}

	messages := make([]responses.ConversationMessageDTO, len(conversation.Messages))
	for i, m := range conversation.Messages {
		messages[i] = responses.ConversationMessageDTO{
			Role:      m.Role,
			Content:   m.Content,
			Timestamp: m.Timestamp,
		}
	}

	return &responses.ConversationResponse{
		ID:        conversation.ID,
		WebsiteID: conversation.WebsiteID,
		Messages:  messages,
		CreatedAt: conversation.CreatedAt,
		UpdatedAt: conversation.UpdatedAt,
	}, nil
}

func (c *Controller) ChatStream(ctx context.Context, user *models.User, websiteId string, req requests.ChatMessageRequest, events chan<- responses.SSEEvent) {
	defer close(events)

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

	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := utils.ValidateFunc[requests.ChatMessageRequest](req, validate); err != nil {
		sendError("validation", err.Error())
		return
	}

	website, err := c.verifyWebsiteOwnership(user, websiteId)
	if err != nil {
		sendError("auth", err.Error())
		return
	}

	provider := c.llmProvider()
	model := c.getWebsiteLLMModel()

	// Get current files
	files, err := c.DB.GetFilesByWebsiteId(websiteId)
	if err != nil {
		sendError("database", "failed to load project files")
		return
	}

	// Build file tree context
	var fileTree strings.Builder
	fileTree.WriteString("Current project files:\n")
	for _, f := range files {
		fileTree.WriteString(fmt.Sprintf("\n--- %s (%s) ---\n%s\n", f.FilePath, f.FileType, f.Content))
	}

	// Get connected integrations
	integrations, _ := c.DB.GetIntegrationsByWebsiteId(websiteId)
	var integrationContext strings.Builder
	if len(integrations) > 0 {
		integrationContext.WriteString("\nConnected integrations:\n")
		for _, integ := range integrations {
			integrationContext.WriteString(fmt.Sprintf("- %s (status: %s)\n", integ.Provider, integ.Status))
		}
	} else {
		integrationContext.WriteString("\nNo integrations connected.\n")
	}

	// Get or create conversation
	conversation, err := c.DB.GetConversationByWebsiteId(websiteId)
	if err != nil {
		conversation = &models.WebsiteConversation{
			WebsiteID: website.ID,
			Messages:  []models.ConversationMessage{},
		}
		conversation, err = c.DB.CreateConversation(conversation)
		if err != nil {
			sendError("database", "failed to create conversation")
			return
		}
	}

	// Append user message and persist immediately so it's not lost on LLM failure
	conversation.Messages = append(conversation.Messages, models.ConversationMessage{
		Role:      "user",
		Content:   req.Message,
		Timestamp: time.Now().Unix(),
	})
	conversation.UpdatedAt = time.Now()
	if _, err := c.DB.UpdateConversation(conversation); err != nil {
		slog.Error("failed to persist user message", "error", err)
		sendError("database", "failed to save message")
		return
	}

	// Build LLM messages
	llmMessages := []utils.Message{
		{Role: "system", Content: constants.WEBSITE_CHAT_SYSTEM_PROMPT},
	}

	// Add context as first user message
	contextMsg := fmt.Sprintf("Website: %s\nColor scheme: %v\n%s\n%s",
		website.Name,
		website.ColorScheme,
		fileTree.String(),
		integrationContext.String(),
	)
	llmMessages = append(llmMessages, utils.Message{Role: "user", Content: contextMsg})
	llmMessages = append(llmMessages, utils.Message{Role: "assistant", Content: "I understand the current project state. What would you like me to do?"})

	// Add conversation history (last 20 messages to keep context manageable)
	startIdx := 0
	if len(conversation.Messages) > 20 {
		startIdx = len(conversation.Messages) - 20
	}
	for _, msg := range conversation.Messages[startIdx:] {
		llmMessages = append(llmMessages, utils.Message{Role: msg.Role, Content: msg.Content})
	}

	if ctx.Err() != nil {
		return
	}

	// Create auto-snapshot before making changes
	c.CreateSnapshotBeforeEdit(website.ID, user.ID, "Auto-save before chat edit")

	// Call LLM with streaming
	responseJSON, err := utils.StreamJSONFromLLMWithMessages(
		ctx,
		model,
		llmMessages,
		provider,
		func(chunk string) {
			sendEvent("chat_thinking", responses.ChatThinkingEvent{Chunk: chunk})
		},
	)
	if err != nil {
		slog.Error("failed to get chat response from LLM", "error", err)
		sendError("ai", "failed to generate changes. try again please")
		return
	}

	var chatResp chatResponse
	if err := json.Unmarshal([]byte(responseJSON), &chatResp); err != nil {
		// Fallback: try to extract JSON object from the response
		extracted := utils.ExtractJSON(responseJSON)
		if err2 := json.Unmarshal([]byte(extracted), &chatResp); err2 != nil {
			slog.Error("failed to parse chat response", "error", err, "extractErr", err2, "json", responseJSON)
			sendError("ai", "failed to parse AI response. try again please")
			return
		}
	}

	// Apply operations
	filesChanged := 0
	for _, op := range chatResp.Operations {
		if ctx.Err() != nil {
			return
		}

		switch op.Action {
		case "create":
			content := cleanCodeOutput(op.Content)
			file := models.WebsiteFile{
				WebsiteID: website.ID,
				FilePath:  op.Path,
				Content:   content,
				FileType:  op.Type,
				Layer:     "frontend",
			}
			createdFile, err := c.DB.CreateFile(&file)
			if err != nil {
				slog.Error("failed to create file", "error", err, "path", op.Path)
				continue
			}
			if !sendEvent("file_complete", responses.FileCompleteEvent{
				Action:   "create",
				FilePath: op.Path,
				FileID:   createdFile.ID,
				FileType: op.Type,
			}) {
				return
			}
			filesChanged++

		case "modify":
			existingFile, err := c.DB.GetFileByWebsiteIdAndPath(websiteId, op.Path)
			if err != nil {
				slog.Error("file not found for modify", "error", err, "path", op.Path)
				continue
			}
			content := cleanCodeOutput(op.Content)
			existingFile.Content = content
			existingFile.UpdatedAt = time.Now()
			_, err = c.DB.UpdateFile(existingFile)
			if err != nil {
				slog.Error("failed to update file", "error", err, "path", op.Path)
				continue
			}
			if !sendEvent("file_complete", responses.FileCompleteEvent{
				Action:   "modify",
				FilePath: op.Path,
				FileID:   existingFile.ID,
				FileType: existingFile.FileType,
			}) {
				return
			}
			filesChanged++

		case "delete":
			existingFile, err := c.DB.GetFileByWebsiteIdAndPath(websiteId, op.Path)
			if err != nil {
				continue
			}
			c.DB.DeleteFile(existingFile)
			if !sendEvent("file_complete", responses.FileCompleteEvent{
				Action:   "delete",
				FilePath: op.Path,
			}) {
				return
			}
			filesChanged++
		}
	}

	// Append assistant response to conversation
	conversation.Messages = append(conversation.Messages, models.ConversationMessage{
		Role:      "assistant",
		Content:   chatResp.Summary,
		Timestamp: time.Now().Unix(),
	})
	conversation.UpdatedAt = time.Now()
	if _, err := c.DB.UpdateConversation(conversation); err != nil {
		slog.Error("failed to save assistant response to conversation", "error", err)
	}

	// Log generation
	c.DB.CreateAIGenerationLog(&models.AIGenerationLog{
		WebsiteID: website.ID,
		UserID:    user.ID,
		Prompt:    req.Message,
		Model:     model,
	})

	sendEvent("chat_complete", responses.ChatCompleteEvent{
		WebsiteID:    website.ID,
		Summary:      chatResp.Summary,
		FilesChanged: filesChanged,
	})
}
