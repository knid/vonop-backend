package controllers

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

// AIEditElementStream applies a targeted edit to a specific element in a file using AI.
func (c *Controller) AIEditElementStream(ctx context.Context, user *models.User, websiteId string, req requests.EditElementRequest, events chan<- responses.SSEEvent) {
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
	if err := utils.ValidateFunc[requests.EditElementRequest](req, validate); err != nil {
		sendError("validation", err.Error())
		return
	}

	website, err := c.verifyWebsiteOwnership(user, websiteId)
	if err != nil {
		sendError("auth", err.Error())
		return
	}

	file, err := c.DB.GetFileById(req.FileID)
	if err != nil {
		sendError("database", "file not found")
		return
	}

	if file.WebsiteID.String() != websiteId {
		sendError("auth", "file does not belong to this website")
		return
	}

	// Create snapshot before editing
	c.CreateSnapshotBeforeEdit(website.ID, user.ID, "Auto-save before element edit")

	provider := c.llmProvider()
	model := c.getWebsiteLLMModel()

	editPrompt := fmt.Sprintf(
		"Current file content:\n```\n%s\n```\n\nTarget element CSS selector: %s\nEdit instruction: %s",
		file.Content,
		req.ElementSelector,
		req.Instruction,
	)

	llmMessages := []utils.Message{
		{Role: "system", Content: constants.WEBSITE_EDIT_ELEMENT_PROMPT},
		{Role: "user", Content: editPrompt},
	}

	if ctx.Err() != nil {
		return
	}

	newContent, err := utils.StreamJSONFromLLMWithMessages(
		ctx,
		model,
		llmMessages,
		provider,
		nil,
		func(chunk string) {
			sendEvent("chat_thinking", responses.ChatThinkingEvent{Chunk: chunk})
		},
	)
	if err != nil {
		slog.Error("failed to edit element", "error", err)
		sendError("ai", "failed to edit element. try again please")
		return
	}

	file.Content = utils.StripCodeFences(newContent)
	file.UpdatedAt = time.Now()
	_, err = c.DB.UpdateFile(file)
	if err != nil {
		sendError("database", "failed to save changes")
		return
	}

	// Log generation
	c.DB.CreateAIGenerationLog(&models.AIGenerationLog{
		WebsiteID: website.ID,
		UserID:    user.ID,
		Prompt:    req.Instruction,
		Model:     model,
	})

	sendEvent("file_complete", responses.FileCompleteEvent{
		Action:   "modify",
		FilePath: file.FilePath,
		FileID:   file.ID,
		FileType: file.FileType,
	})

	sendEvent("chat_complete", responses.ChatCompleteEvent{
		WebsiteID:    website.ID,
		Summary:      fmt.Sprintf("Edited element '%s' in %s", req.ElementSelector, file.FilePath),
		FilesChanged: 1,
	})
}
