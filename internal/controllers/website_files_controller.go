package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

type fileStructure struct {
	Name        string            `json:"name"`
	ColorScheme map[string]any    `json:"colorScheme"`
	Files       []filePlanEntry   `json:"files"`
}

type filePlanEntry struct {
	Path        string `json:"path"`
	Type        string `json:"type"`
	Description string `json:"description"`
}

// GenerateWebsiteFiles generates a website using the file-based approach.
func (c *Controller) GenerateWebsiteFiles(user *models.User, req requests.WebsiteGenerateRequest) (*responses.WebsiteDetailResponse, error) {
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := utils.ValidateFunc[requests.WebsiteGenerateRequest](req, validate); err != nil {
		return nil, err
	}

	apiKey := c.SecretManager.GetSecret("OPENROUTER_API_KEY")
	model := c.getWebsiteLLMModel()

	// Step 1: Generate file structure via LLM
	templateHint := ""
	if req.TemplateID != nil {
		template, err := c.DB.GetTemplateById(req.TemplateID.String())
		if err == nil && template.PromptHint != "" {
			templateHint = "\n\nTemplate context: " + template.PromptHint
		}
	}

	structureJSON, err := utils.GetJSONFromLLM(
		model,
		constants.WEBSITE_STRUCTURE_PROMPT,
		req.Prompt+templateHint,
		apiKey,
	)
	if err != nil {
		slog.Error("failed to generate website structure", "error", err)
		return nil, fmt.Errorf("failed to generate website structure. try again please")
	}

	var structure fileStructure
	if err := json.Unmarshal([]byte(structureJSON), &structure); err != nil {
		slog.Error("failed to parse website structure", "error", err, "json", structureJSON)
		return nil, fmt.Errorf("failed to parse website structure. try again please")
	}

	if len(structure.Files) == 0 {
		return nil, fmt.Errorf("failed to generate website files. try again please")
	}

	// Step 2: Generate slug
	slug := generateSlug(structure.Name)
	if _, err := c.DB.GetWebsiteBySlug(slug); err == nil {
		suffix, _ := utils.RandomHex(3)
		slug = slug + "-" + suffix
	}

	// Step 3: Create website record
	website := models.Website{
		UserID:      user.ID,
		Name:        structure.Name,
		Slug:        slug,
		Prompt:      req.Prompt,
		ColorScheme: structure.ColorScheme,
		Status:      "draft",
	}

	if req.TemplateID != nil {
		website.TemplateID = *req.TemplateID
	}

	createdWebsite, err := c.DB.CreateWebsite(&website)
	if err != nil {
		return nil, fmt.Errorf("failed to create website: %v", err)
	}

	// Step 4: Generate content for each file
	colorSchemeJSON, _ := json.Marshal(structure.ColorScheme)
	allFilePaths := make([]string, len(structure.Files))
	for i, f := range structure.Files {
		allFilePaths[i] = f.Path
	}
	allFilesContext := strings.Join(allFilePaths, ", ")

	var fileResponses []responses.FileResponse

	for _, filePlan := range structure.Files {
		prompt := c.getFilePrompt(filePlan.Type)
		filePrompt := fmt.Sprintf(
			"Website name: %s\nColor scheme: %s\nAll files: %s\nCurrent file: %s\nDescription: %s\n\nUser's original description: %s",
			structure.Name,
			string(colorSchemeJSON),
			allFilesContext,
			filePlan.Path,
			filePlan.Description,
			req.Prompt,
		)

		content, err := utils.GetTextFromLLM(model, prompt, filePrompt, apiKey)
		if err != nil {
			slog.Error("failed to generate file content", "error", err, "path", filePlan.Path)
			content = c.getFallbackContent(filePlan)
		}

		content = cleanFileOutput(content, filePlan.Type)

		file := models.WebsiteFile{
			WebsiteID: createdWebsite.ID,
			FilePath:  filePlan.Path,
			Content:   content,
			FileType:  filePlan.Type,
			Layer:     "frontend",
		}

		createdFile, err := c.DB.CreateFile(&file)
		if err != nil {
			slog.Error("failed to create file", "error", err, "path", filePlan.Path)
			continue
		}

		fileResponses = append(fileResponses, responses.FileResponse{
			ID:        createdFile.ID,
			WebsiteID: createdFile.WebsiteID,
			FilePath:  createdFile.FilePath,
			Content:   createdFile.Content,
			FileType:  createdFile.FileType,
			Layer:     createdFile.Layer,
			CreatedAt: createdFile.CreatedAt,
			UpdatedAt: createdFile.UpdatedAt,
		})
	}

	// Step 5: Create conversation
	c.DB.CreateConversation(&models.WebsiteConversation{
		WebsiteID: createdWebsite.ID,
		Messages: []models.ConversationMessage{
			{
				Role:      "user",
				Content:   req.Prompt,
				Timestamp: time.Now().Unix(),
			},
			{
				Role:      "assistant",
				Content:   fmt.Sprintf("Created website \"%s\" with %d files.", structure.Name, len(fileResponses)),
				Timestamp: time.Now().Unix(),
			},
		},
	})

	// Step 6: Log generation
	c.DB.CreateAIGenerationLog(&models.AIGenerationLog{
		WebsiteID: createdWebsite.ID,
		UserID:    user.ID,
		Prompt:    req.Prompt,
		Model:     model,
	})

	return &responses.WebsiteDetailResponse{
		WebsiteResponse: responses.WebsiteResponse{
			ID:          createdWebsite.ID,
			Name:        createdWebsite.Name,
			Slug:        createdWebsite.Slug,
			Prompt:      createdWebsite.Prompt,
			ColorScheme: createdWebsite.ColorScheme,
			Status:      createdWebsite.Status,
			CreatedAt:   createdWebsite.CreatedAt,
			UpdatedAt:   createdWebsite.UpdatedAt,
		},
		Files: fileResponses,
	}, nil
}

// GenerateWebsiteFilesStream generates a website using file-based approach with SSE streaming.
func (c *Controller) GenerateWebsiteFilesStream(ctx context.Context, user *models.User, req requests.WebsiteGenerateRequest, events chan<- responses.SSEEvent) {
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
	if err := utils.ValidateFunc[requests.WebsiteGenerateRequest](req, validate); err != nil {
		sendError("validation", err.Error())
		return
	}

	apiKey := c.SecretManager.GetSecret("OPENROUTER_API_KEY")
	model := c.getWebsiteLLMModel()

	templateHint := ""
	if req.TemplateID != nil {
		template, err := c.DB.GetTemplateById(req.TemplateID.String())
		if err == nil && template.PromptHint != "" {
			templateHint = "\n\nTemplate context: " + template.PromptHint
		}
	}

	if ctx.Err() != nil {
		return
	}

	structureJSON, err := utils.StreamJSONFromLLM(ctx, model, constants.WEBSITE_STRUCTURE_PROMPT, req.Prompt+templateHint, apiKey, func(chunk string) {
		sendEvent("planning_chunk", responses.PlanningChunkEvent{
			Chunk: chunk,
			Phase: "structure",
		})
	})
	if err != nil {
		slog.Error("failed to generate website structure", "error", err)
		sendError("structure", "failed to generate website structure. try again please")
		return
	}

	var structure fileStructure
	if err := json.Unmarshal([]byte(structureJSON), &structure); err != nil {
		slog.Error("failed to parse website structure", "error", err, "json", structureJSON)
		sendError("structure", "failed to parse website structure. try again please")
		return
	}

	if len(structure.Files) == 0 {
		sendError("structure", "failed to generate website files. try again please")
		return
	}

	slug := generateSlug(structure.Name)
	if _, err := c.DB.GetWebsiteBySlug(slug); err == nil {
		suffix, _ := utils.RandomHex(3)
		slug = slug + "-" + suffix
	}

	website := models.Website{
		UserID:      user.ID,
		Name:        structure.Name,
		Slug:        slug,
		Prompt:      req.Prompt,
		ColorScheme: structure.ColorScheme,
		Status:      "draft",
	}

	if req.TemplateID != nil {
		website.TemplateID = *req.TemplateID
	}

	createdWebsite, err := c.DB.CreateWebsite(&website)
	if err != nil {
		sendError("database", fmt.Sprintf("failed to create website: %v", err))
		return
	}

	// Send website created event with file plan
	filePaths := make([]string, len(structure.Files))
	for i, f := range structure.Files {
		filePaths[i] = f.Path
	}

	if !sendEvent("website_created", responses.FileGenerationStartEvent{
		WebsiteResponse: responses.WebsiteResponse{
			ID:          createdWebsite.ID,
			Name:        createdWebsite.Name,
			Slug:        createdWebsite.Slug,
			Prompt:      createdWebsite.Prompt,
			ColorScheme: createdWebsite.ColorScheme,
			Status:      createdWebsite.Status,
			CreatedAt:   createdWebsite.CreatedAt,
			UpdatedAt:   createdWebsite.UpdatedAt,
		},
		TotalFiles: len(structure.Files),
		FilePaths:  filePaths,
	}) {
		return
	}

	// Generate each file with streaming
	colorSchemeJSON, _ := json.Marshal(structure.ColorScheme)
	allFilesContext := strings.Join(filePaths, ", ")

	for _, filePlan := range structure.Files {
		if ctx.Err() != nil {
			return
		}

		prompt := c.getFilePrompt(filePlan.Type)
		filePrompt := fmt.Sprintf(
			"Website name: %s\nColor scheme: %s\nAll files: %s\nCurrent file: %s\nDescription: %s\n\nUser's original description: %s",
			structure.Name,
			string(colorSchemeJSON),
			allFilesContext,
			filePlan.Path,
			filePlan.Description,
			req.Prompt,
		)

		content, err := utils.StreamTextFromLLM(
			ctx,
			model,
			prompt,
			filePrompt,
			apiKey,
			func(chunk string) {
				sendEvent("file_chunk", responses.FileOperationEvent{
					Action:   "create",
					FilePath: filePlan.Path,
					FileType: filePlan.Type,
					Chunk:    chunk,
				})
			},
		)
		if err != nil {
			slog.Error("failed to generate file", "error", err, "path", filePlan.Path)
			content = c.getFallbackContent(filePlan)
		}

		content = cleanFileOutput(content, filePlan.Type)

		file := models.WebsiteFile{
			WebsiteID: createdWebsite.ID,
			FilePath:  filePlan.Path,
			Content:   content,
			FileType:  filePlan.Type,
			Layer:     "frontend",
		}

		createdFile, err := c.DB.CreateFile(&file)
		if err != nil {
			slog.Error("failed to create file", "error", err, "path", filePlan.Path)
			continue
		}

		if !sendEvent("file_complete", responses.FileCompleteEvent{
			Action:   "create",
			FilePath: filePlan.Path,
			FileID:   createdFile.ID,
			FileType: filePlan.Type,
		}) {
			return
		}
	}

	// Create conversation
	c.DB.CreateConversation(&models.WebsiteConversation{
		WebsiteID: createdWebsite.ID,
		Messages: []models.ConversationMessage{
			{Role: "user", Content: req.Prompt, Timestamp: time.Now().Unix()},
			{Role: "assistant", Content: fmt.Sprintf("Created website \"%s\" with %d files.", structure.Name, len(structure.Files)), Timestamp: time.Now().Unix()},
		},
	})

	c.DB.CreateAIGenerationLog(&models.AIGenerationLog{
		WebsiteID: createdWebsite.ID,
		UserID:    user.ID,
		Prompt:    req.Prompt,
		Model:     model,
	})

	sendEvent("complete", responses.GenerationCompleteEvent{
		WebsiteID: createdWebsite.ID,
		Success:   true,
		Message:   "website generation completed",
	})
}

// File CRUD operations

func (c *Controller) GetWebsiteFiles(user *models.User, websiteId string) ([]responses.FileResponse, error) {
	_, err := c.verifyWebsiteOwnership(user, websiteId)
	if err != nil {
		return nil, err
	}

	files, err := c.DB.GetFilesByWebsiteId(websiteId)
	if err != nil {
		return nil, err
	}

	result := make([]responses.FileResponse, len(files))
	for i, f := range files {
		result[i] = responses.FileResponse{
			ID:        f.ID,
			WebsiteID: f.WebsiteID,
			FilePath:  f.FilePath,
			Content:   f.Content,
			FileType:  f.FileType,
			Layer:     f.Layer,
			CreatedAt: f.CreatedAt,
			UpdatedAt: f.UpdatedAt,
		}
	}
	return result, nil
}

func (c *Controller) GetWebsiteFile(user *models.User, websiteId string, fileId string) (*responses.FileResponse, error) {
	_, err := c.verifyWebsiteOwnership(user, websiteId)
	if err != nil {
		return nil, err
	}

	file, err := c.DB.GetFileById(fileId)
	if err != nil {
		return nil, errors.New("file not found")
	}

	if file.WebsiteID.String() != websiteId {
		return nil, errors.New("file does not belong to this website")
	}

	return &responses.FileResponse{
		ID:        file.ID,
		WebsiteID: file.WebsiteID,
		FilePath:  file.FilePath,
		Content:   file.Content,
		FileType:  file.FileType,
		Layer:     file.Layer,
		CreatedAt: file.CreatedAt,
		UpdatedAt: file.UpdatedAt,
	}, nil
}

func (c *Controller) CreateWebsiteFile(user *models.User, websiteId string, req requests.FileCreateRequest) (*responses.FileResponse, error) {
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := utils.ValidateFunc[requests.FileCreateRequest](req, validate); err != nil {
		return nil, err
	}

	_, err := c.verifyWebsiteOwnership(user, websiteId)
	if err != nil {
		return nil, err
	}

	websiteUUID, err := uuid.Parse(websiteId)
	if err != nil {
		return nil, errors.New("invalid website id")
	}

	// Check if file already exists at this path
	if _, err := c.DB.GetFileByWebsiteIdAndPath(websiteId, req.FilePath); err == nil {
		return nil, errors.New("a file already exists at this path")
	}

	file := models.WebsiteFile{
		WebsiteID: websiteUUID,
		FilePath:  req.FilePath,
		Content:   req.Content,
		FileType:  req.FileType,
		Layer:     "frontend",
	}

	createdFile, err := c.DB.CreateFile(&file)
	if err != nil {
		return nil, err
	}

	return &responses.FileResponse{
		ID:        createdFile.ID,
		WebsiteID: createdFile.WebsiteID,
		FilePath:  createdFile.FilePath,
		Content:   createdFile.Content,
		FileType:  createdFile.FileType,
		Layer:     createdFile.Layer,
		CreatedAt: createdFile.CreatedAt,
		UpdatedAt: createdFile.UpdatedAt,
	}, nil
}

func (c *Controller) UpdateWebsiteFile(user *models.User, websiteId string, fileId string, req requests.FileUpdateRequest) (*responses.FileResponse, error) {
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := utils.ValidateFunc[requests.FileUpdateRequest](req, validate); err != nil {
		return nil, err
	}

	_, err := c.verifyWebsiteOwnership(user, websiteId)
	if err != nil {
		return nil, err
	}

	file, err := c.DB.GetFileById(fileId)
	if err != nil {
		return nil, errors.New("file not found")
	}

	if file.WebsiteID.String() != websiteId {
		return nil, errors.New("file does not belong to this website")
	}

	file.Content = req.Content
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

func (c *Controller) DeleteWebsiteFile(user *models.User, websiteId string, fileId string) error {
	_, err := c.verifyWebsiteOwnership(user, websiteId)
	if err != nil {
		return err
	}

	file, err := c.DB.GetFileById(fileId)
	if err != nil {
		return errors.New("file not found")
	}

	if file.WebsiteID.String() != websiteId {
		return errors.New("file does not belong to this website")
	}

	return c.DB.DeleteFile(file)
}

// PublishWebsiteFiles publishes a file-based website to S3.
func (c *Controller) PublishWebsiteFiles(user *models.User, websiteId string) (*responses.PublishResponse, error) {
	website, err := c.verifyWebsiteOwnership(user, websiteId)
	if err != nil {
		return nil, err
	}

	files, err := c.DB.GetFilesByWebsiteId(websiteId)
	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		return nil, errors.New("website has no files to publish")
	}

	// Build integration credentials map for placeholder replacement
	integrations, _ := c.DB.GetIntegrationsByWebsiteId(websiteId)
	integrationCreds := make(map[string]map[string]any)
	for _, integ := range integrations {
		if integ.Status == "connected" {
			integrationCreds[integ.Provider] = integ.Credentials
		}
	}

	// Load SEO settings
	seo, _ := c.DB.GetSEOByWebsiteId(websiteId)

	contentTypeMap := map[string]string{
		"html": "text/html",
		"css":  "text/css",
		"js":   "application/javascript",
		"json": "application/json",
	}

	publishedURL := fmt.Sprintf("https://sites.wemob.io/%s", website.Slug)
	var publishedFilePaths []string

	for _, file := range files {
		content := file.Content

		// Replace integration placeholders with real values
		if len(integrationCreds) > 0 {
			content = utils.ReplaceIntegrationPlaceholders(content, integrationCreds)
		}

		// Inject SEO meta tags into HTML files
		if file.FileType == "html" && seo != nil {
			content = InjectSEOMetaTags(content, seo)
		}

		s3Path := fmt.Sprintf("sites/%s%s", website.Slug, file.FilePath)
		ct := contentTypeMap[file.FileType]
		if ct == "" {
			ct = "text/plain"
		}

		err := c.AssetsStorage.UploadFileByteWithContentType(
			context.Background(),
			s3Path,
			[]byte(content),
			ct,
		)
		if err != nil {
			slog.Error("failed to upload file to S3", "error", err, "path", s3Path)
			return nil, fmt.Errorf("failed to publish file: %s", file.FilePath)
		}

		publishedFilePaths = append(publishedFilePaths, file.FilePath)
	}

	// Generate and upload sitemap.xml
	sitemapURL := publishedURL + "/sitemap.xml"
	sitemapContent := GenerateSitemap(publishedURL, publishedFilePaths)
	c.AssetsStorage.UploadFileByteWithContentType(
		context.Background(),
		fmt.Sprintf("sites/%s/sitemap.xml", website.Slug),
		[]byte(sitemapContent),
		"application/xml",
	)

	// Generate and upload robots.txt
	robotsContent := GenerateRobotsTxt(sitemapURL)
	c.AssetsStorage.UploadFileByteWithContentType(
		context.Background(),
		fmt.Sprintf("sites/%s/robots.txt", website.Slug),
		[]byte(robotsContent),
		"text/plain",
	)

	website.Status = "published"
	website.PublishedURL = publishedURL
	website.UpdatedAt = time.Now()

	_, err = c.DB.UpdateWebsite(website)
	if err != nil {
		return nil, err
	}

	return &responses.PublishResponse{
		SuccessResponse: responses.SuccessResponse{Success: true},
		PublishedURL:    publishedURL,
	}, nil
}

// Helpers

func (c *Controller) getFilePrompt(fileType string) string {
	switch fileType {
	case "css":
		return constants.WEBSITE_FILE_CSS_PROMPT
	case "js":
		return constants.WEBSITE_FILE_JS_PROMPT
	default:
		return constants.WEBSITE_FILE_HTML_PROMPT
	}
}

func (c *Controller) getFallbackContent(filePlan filePlanEntry) string {
	switch filePlan.Type {
	case "html":
		return fmt.Sprintf("<!DOCTYPE html><html><head><title>%s</title><script src=\"https://cdn.tailwindcss.com\"></script></head><body><h1>%s</h1><p>Page generation failed. Please try editing this file.</p></body></html>", filePlan.Description, filePlan.Description)
	case "css":
		return "/* Styles generation failed. Please edit manually. */"
	case "js":
		return "// JavaScript generation failed. Please edit manually."
	default:
		return ""
	}
}

func cleanFileOutput(content string, fileType string) string {
	content = strings.TrimSpace(content)
	switch fileType {
	case "html":
		content = strings.TrimPrefix(content, "```html")
		content = strings.TrimPrefix(content, "```")
		content = strings.TrimSuffix(content, "```")
	case "css":
		content = strings.TrimPrefix(content, "```css")
		content = strings.TrimPrefix(content, "```")
		content = strings.TrimSuffix(content, "```")
	case "js":
		content = strings.TrimPrefix(content, "```javascript")
		content = strings.TrimPrefix(content, "```js")
		content = strings.TrimPrefix(content, "```")
		content = strings.TrimSuffix(content, "```")
	case "json":
		content = strings.TrimPrefix(content, "```json")
		content = strings.TrimPrefix(content, "```")
		content = strings.TrimSuffix(content, "```")
	}
	return strings.TrimSpace(content)
}
