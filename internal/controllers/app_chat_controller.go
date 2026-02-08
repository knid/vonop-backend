package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

type appChatOperation struct {
	Action  string `json:"action"`
	Path    string `json:"path"`
	Type    string `json:"type"`
	Content string `json:"content"`
}

type appChatResponse struct {
	Operations []appChatOperation `json:"operations"`
	Summary    string             `json:"summary"`
}

// StreamChatAppFileOps processes a chat message and applies file operations with SSE streaming.
// Mirrors the ChatStream pattern from website_chat_controller.go.
func (c *Controller) StreamChatAppFileOps(ctx context.Context, project *models.AppProject, user *models.User, message string, events chan<- responses.SSEEvent) {
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

	apiKey := c.SecretManager.GetSecret("OPENROUTER_API_KEY")
	model := c.SecretManager.GetSecret("NEXTJS_BUILDER_LLM_MODEL")
	if model == "" {
		model = constants.DEFAULT_NEXTJS_LLM_MODEL
	}

	// Load current files
	files, err := c.DB.GetAppGeneratedFilesByProjectID(project.ID.String(), "nextjs")
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
	integrations, _ := c.DB.GetAppIntegrationsByProjectID(project.ID.String())
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
	conv, err := c.DB.GetAppConversationByProjectID(project.ID.String())
	if err != nil {
		conv = &models.AppConversation{
			AppProjectID: project.ID,
			Messages:     []byte("[]"),
		}
		conv, err = c.DB.CreateAppConversation(conv)
		if err != nil {
			sendError("database", "failed to create conversation")
			return
		}
	}

	// Parse existing messages and append user message
	var messages []models.AppConversationMessage
	json.Unmarshal(conv.Messages, &messages)

	messages = append(messages, models.AppConversationMessage{
		Role:      "user",
		Content:   message,
		Timestamp: time.Now().Unix(),
	})
	conv.Messages, _ = json.Marshal(messages)
	c.DB.UpdateAppConversation(conv)

	// Build LLM messages
	llmMessages := []utils.Message{
		{Role: "system", Content: constants.NEXTJS_CHAT_FILE_OPS_PROMPT},
	}

	// Add context as first user message
	var theme map[string]any
	json.Unmarshal(project.Theme, &theme)
	contextMsg := fmt.Sprintf("App: %s\nTheme: %v\n%s\n%s",
		project.Name,
		theme,
		fileTree.String(),
		integrationContext.String(),
	)
	llmMessages = append(llmMessages, utils.Message{Role: "user", Content: contextMsg})
	llmMessages = append(llmMessages, utils.Message{Role: "assistant", Content: "I understand the current project state. What would you like me to do?"})

	// Add conversation history (last 20 messages)
	startIdx := 0
	if len(messages) > 20 {
		startIdx = len(messages) - 20
	}
	for _, msg := range messages[startIdx:] {
		llmMessages = append(llmMessages, utils.Message{Role: msg.Role, Content: msg.Content})
	}

	if ctx.Err() != nil {
		return
	}

	// Create auto-snapshot before making changes
	c.CreateAppSnapshot(project, "Auto-save before chat edit", "auto")

	// Call LLM with streaming (don't stream raw JSON chunks to chat — the frontend
	// shows "Thinking..." while we wait, and we send the summary at the end)
	responseJSON, err := utils.StreamJSONFromLLMWithMessages(
		ctx,
		model,
		llmMessages,
		apiKey,
		nil,
	)
	if err != nil {
		slog.Error("failed to get chat response from LLM", "error", err)
		// Save the error as assistant message so it persists on refresh
		messages = append(messages, models.AppConversationMessage{
			Role:      "assistant",
			Content:   "Sorry, I couldn't process your request. Please try again.",
			Timestamp: time.Now().Unix(),
		})
		conv.Messages, _ = json.Marshal(messages)
		c.DB.UpdateAppConversation(conv)
		sendError("ai", "failed to generate changes. try again please")
		return
	}

	// Clean the response: strip markdown code fences that LLMs often add
	cleaned := strings.TrimSpace(responseJSON)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	var chatResp appChatResponse
	if err := json.Unmarshal([]byte(cleaned), &chatResp); err != nil {
		// Fallback: try to extract JSON object from the response
		extracted := utils.ExtractJSON(responseJSON)
		if err2 := json.Unmarshal([]byte(extracted), &chatResp); err2 != nil {
			slog.Error("failed to parse chat response", "error", err, "extractErr", err2, "json", responseJSON)
			// Last resort: treat the whole response as a summary with no operations
			chatResp = appChatResponse{
				Operations: []appChatOperation{},
				Summary:    "I wasn't able to generate the changes properly. Please try again with a more specific request.",
			}
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
			content := cleanAppFileOutput(op.Content, op.Type)
			if op.Type == "tsx" || op.Type == "ts" {
				content, _ = stripForbiddenImports(content, op.Path)
			}
			file := models.AppGeneratedFile{
				AppProjectID: project.ID,
				Platform:     "nextjs",
				FilePath:     op.Path,
				Content:      content,
				FileType:     op.Type,
				IsGenerated:  true,
			}
			createdFile, err := c.DB.CreateAppGeneratedFile(&file)
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
			existingFile, err := c.DB.GetAppGeneratedFileByPath(project.ID.String(), "nextjs", op.Path)
			if err != nil {
				slog.Error("file not found for modify", "error", err, "path", op.Path)
				continue
			}
			content := cleanAppFileOutput(op.Content, op.Type)
			if op.Type == "tsx" || op.Type == "ts" || existingFile.FileType == "tsx" || existingFile.FileType == "ts" {
				content, _ = stripForbiddenImports(content, op.Path)
			}
			existingFile.Content = content
			existingFile.UpdatedAt = time.Now()
			_, err = c.DB.UpsertAppGeneratedFile(existingFile)
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
			existingFile, err := c.DB.GetAppGeneratedFileByPath(project.ID.String(), "nextjs", op.Path)
			if err != nil {
				continue
			}
			c.DB.DeleteAppGeneratedFile(existingFile)
			if !sendEvent("file_complete", responses.FileCompleteEvent{
				Action:   "delete",
				FilePath: op.Path,
			}) {
				return
			}
			filesChanged++
		}
	}

	// Save assistant response to conversation
	messages = append(messages, models.AppConversationMessage{
		Role:      "assistant",
		Content:   chatResp.Summary,
		Timestamp: time.Now().Unix(),
	})
	conv.Messages, _ = json.Marshal(messages)
	c.DB.UpdateAppConversation(conv)

	// Auto-deploy to Vercel after chat edits
	if c.VercelService != nil && filesChanged > 0 {
		sendEvent("deploying", map[string]string{"status": "starting"})

		deployment, err := c.DeployToVercel(project, user, "production")
		if err != nil {
			slog.Error("Auto-deploy after chat failed", "project_id", project.ID, "error", err)
			sendEvent("deploy_error", map[string]string{"error": err.Error()})
		} else {
			sendEvent("deployed", responses.AppDeploymentResponse{
				ID:           deployment.ID,
				AppProjectID: deployment.AppProjectID,
				Platform:     deployment.Platform,
				Provider:     deployment.Provider,
				Status:       deployment.Status,
				DeployedURL:  deployment.DeployedURL,
				Version:      deployment.Version,
				CreatedAt:    deployment.CreatedAt,
				UpdatedAt:    deployment.UpdatedAt,
			})
		}
	}

	sendEvent("chat_complete", map[string]any{
		"project_id":    project.ID,
		"summary":       chatResp.Summary,
		"files_changed": filesChanged,
	})
}

// GetAppConversation returns the conversation history for a project
func (c *Controller) GetAppConversation(projectID string) (*models.AppConversation, error) {
	return c.DB.GetAppConversationByProjectID(projectID)
}
