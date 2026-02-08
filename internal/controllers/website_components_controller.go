package controllers

import (
	"context"
	"encoding/json"
	"errors"
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

// GetComponents returns all active components, optionally filtered by category.
func (c *Controller) GetComponents(category string) ([]responses.ComponentListResponse, error) {
	var components []models.WebsiteComponent
	var err error

	if category != "" {
		components, err = c.DB.GetComponentsByCategory(category)
	} else {
		components, err = c.DB.GetActiveComponents()
	}
	if err != nil {
		return nil, err
	}

	result := make([]responses.ComponentListResponse, len(components))
	for i, comp := range components {
		result[i] = responses.ComponentListResponse{
			ID:           comp.ID,
			Name:         comp.Name,
			Category:     comp.Category,
			PreviewImage: comp.PreviewImage,
			Tags:         comp.Tags,
		}
	}
	return result, nil
}

// GetComponent returns a single component with its HTML template.
func (c *Controller) GetComponent(componentId string) (*responses.ComponentResponse, error) {
	comp, err := c.DB.GetComponentById(componentId)
	if err != nil {
		return nil, errors.New("component not found")
	}

	return &responses.ComponentResponse{
		ID:           comp.ID,
		Name:         comp.Name,
		Category:     comp.Category,
		HTMLTemplate: comp.HTMLTemplate,
		PreviewImage: comp.PreviewImage,
		Tags:         comp.Tags,
	}, nil
}

// InsertComponent inserts a component into a website file, adapted to the site's color scheme.
func (c *Controller) InsertComponent(user *models.User, websiteId string, req requests.InsertComponentRequest) (*responses.FileResponse, error) {
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := utils.ValidateFunc[requests.InsertComponentRequest](req, validate); err != nil {
		return nil, err
	}

	website, err := c.verifyWebsiteOwnership(user, websiteId)
	if err != nil {
		return nil, err
	}

	component, err := c.DB.GetComponentById(req.ComponentID)
	if err != nil {
		return nil, errors.New("component not found")
	}

	file, err := c.DB.GetFileById(req.TargetFileID)
	if err != nil {
		return nil, errors.New("target file not found")
	}

	if file.WebsiteID.String() != websiteId {
		return nil, errors.New("file does not belong to this website")
	}

	// Create snapshot before editing
	c.CreateSnapshotBeforeEdit(website.ID, user.ID, "Auto-save before component insert")

	apiKey := c.SecretManager.GetSecret("OPENROUTER_API_KEY")
	model := c.getWebsiteLLMModel()

	colorSchemeJSON, _ := json.Marshal(website.ColorScheme)

	prompt := fmt.Sprintf(
		"Current file content:\n```\n%s\n```\n\nComponent HTML to insert:\n```\n%s\n```\n\nWebsite color scheme: %s\nInsertion position: %s\n\nAdapt the component HTML to match the website's color scheme and style. Then insert it at the specified position in the file. Return the COMPLETE updated file content.",
		file.Content,
		component.HTMLTemplate,
		string(colorSchemeJSON),
		req.Position,
	)

	newContent, err := utils.GetTextFromLLM(
		model,
		constants.WEBSITE_EDIT_PAGE_PROMPT,
		prompt,
		apiKey,
	)
	if err != nil {
		slog.Error("failed to insert component", "error", err)
		return nil, fmt.Errorf("failed to insert component. try again please")
	}

	newContent = cleanFileOutput(newContent, file.FileType)

	file.Content = newContent
	file.UpdatedAt = time.Now()
	updatedFile, err := c.DB.UpdateFile(file)
	if err != nil {
		return nil, err
	}

	return &responses.FileResponse{
		ID:        updatedFile.ID,
		WebsiteID: updatedFile.WebsiteID,
		FilePath:  updatedFile.FilePath,
		Content:   updatedFile.Content,
		FileType:  updatedFile.FileType,
		Layer:     updatedFile.Layer,
		CreatedAt: updatedFile.CreatedAt,
		UpdatedAt: updatedFile.UpdatedAt,
	}, nil
}

// InsertComponentStream inserts a component into a website file with SSE streaming.
func (c *Controller) InsertComponentStream(ctx context.Context, user *models.User, websiteId string, req requests.InsertComponentRequest, events chan<- responses.SSEEvent) {
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
	if err := utils.ValidateFunc[requests.InsertComponentRequest](req, validate); err != nil {
		sendError("validation", err.Error())
		return
	}

	website, err := c.verifyWebsiteOwnership(user, websiteId)
	if err != nil {
		sendError("validation", err.Error())
		return
	}

	component, err := c.DB.GetComponentById(req.ComponentID)
	if err != nil {
		sendError("validation", "component not found")
		return
	}

	file, err := c.DB.GetFileById(req.TargetFileID)
	if err != nil {
		sendError("validation", "target file not found")
		return
	}

	if file.WebsiteID.String() != websiteId {
		sendError("validation", "file does not belong to this website")
		return
	}

	// Create snapshot before editing
	c.CreateSnapshotBeforeEdit(website.ID, user.ID, "Auto-save before component insert")

	apiKey := c.SecretManager.GetSecret("OPENROUTER_API_KEY")
	model := c.getWebsiteLLMModel()

	colorSchemeJSON, _ := json.Marshal(website.ColorScheme)

	prompt := fmt.Sprintf(
		"Current file content:\n```\n%s\n```\n\nComponent HTML to insert:\n```\n%s\n```\n\nWebsite color scheme: %s\nInsertion position: %s\n\nAdapt the component HTML to match the website's color scheme and style. Then insert it at the specified position in the file. Return the COMPLETE updated file content.",
		file.Content,
		component.HTMLTemplate,
		string(colorSchemeJSON),
		req.Position,
	)

	if ctx.Err() != nil {
		return
	}

	sendEvent("file_chunk", responses.FileOperationEvent{
		Action:   "modify",
		FilePath: file.FilePath,
		FileType: file.FileType,
		Chunk:    "",
	})

	newContent, err := utils.StreamTextFromLLM(
		ctx,
		model,
		constants.WEBSITE_EDIT_PAGE_PROMPT,
		prompt,
		apiKey,
		func(chunk string) {
			sendEvent("file_chunk", responses.FileOperationEvent{
				Action:   "modify",
				FilePath: file.FilePath,
				FileType: file.FileType,
				Chunk:    chunk,
			})
		},
	)
	if err != nil {
		slog.Error("failed to insert component", "error", err)
		sendError("generation", "failed to insert component. try again please")
		return
	}

	newContent = cleanFileOutput(newContent, file.FileType)

	file.Content = newContent
	file.UpdatedAt = time.Now()
	updatedFile, err := c.DB.UpdateFile(file)
	if err != nil {
		sendError("database", err.Error())
		return
	}

	sendEvent("file_complete", responses.FileCompleteEvent{
		Action:   "modify",
		FilePath: updatedFile.FilePath,
		FileID:   updatedFile.ID,
		FileType: updatedFile.FileType,
	})

	sendEvent("complete", responses.GenerationCompleteEvent{
		WebsiteID: updatedFile.WebsiteID,
		Success:   true,
		Message:   "component inserted",
	})
}
