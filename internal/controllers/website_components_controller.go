package controllers

import (
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
