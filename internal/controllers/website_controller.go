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
	"github.com/knid/wemob/internal/utils/external"
)

type websiteStructure struct {
	Name        string         `json:"name"`
	ColorScheme map[string]any `json:"colorScheme"`
	Pages       []pageStructure `json:"pages"`
}

type pageStructure struct {
	Title    string   `json:"title"`
	Path     string   `json:"path"`
	Sections []string `json:"sections"`
}

func (c *Controller) verifyWebsiteOwnership(user *models.User, websiteId string) (*models.Website, error) {
	website, err := c.DB.GetWebsiteById(websiteId)
	if err != nil {
		return nil, errors.New("website not found")
	}

	if website.UserID.String() != user.ID.String() {
		return nil, errors.New("you do not have access this resources")
	}

	return website, nil
}

// VerifyWebsiteOwnershipPublic is an exported version of verifyWebsiteOwnership for use in views.
func (c *Controller) VerifyWebsiteOwnershipPublic(user *models.User, websiteId string) (*models.Website, error) {
	return c.verifyWebsiteOwnership(user, websiteId)
}

func (c *Controller) getWebsiteLLMModel() string {
	model := c.SecretManager.GetSecret("WEBSITE_BUILDER_LLM_MODEL")
	if model == "" {
		return constants.DEFAULT_WEBSITE_LLM_MODEL
	}
	return model
}

func (c *Controller) GenerateWebsite(user *models.User, req requests.WebsiteGenerateRequest) (*responses.WebsiteDetailResponse, error) {
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := utils.ValidateFunc[requests.WebsiteGenerateRequest](req, validate); err != nil {
		return nil, err
	}

	apiKey := c.SecretManager.GetSecret("OPENROUTER_API_KEY")
	model := c.getWebsiteLLMModel()

	// Step 1: Generate website structure via LLM
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

	var structure websiteStructure
	if err := json.Unmarshal([]byte(structureJSON), &structure); err != nil {
		slog.Error("failed to parse website structure", "error", err, "json", structureJSON)
		return nil, fmt.Errorf("failed to parse website structure. try again please")
	}

	if len(structure.Pages) == 0 {
		return nil, fmt.Errorf("failed to generate website pages. try again please")
	}

	// Step 2: Generate slug
	slug := generateSlug(structure.Name)

	// Check slug uniqueness, append random suffix if needed
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

	// Step 4: Generate HTML for each page
	colorSchemeJSON, _ := json.Marshal(structure.ColorScheme)
	pageNames := make([]string, len(structure.Pages))
	for i, p := range structure.Pages {
		pageNames[i] = fmt.Sprintf("%s (%s)", p.Title, p.Path)
	}
	allPagesContext := strings.Join(pageNames, ", ")

	var pageResponses []responses.PageResponse

	for i, pageStruct := range structure.Pages {
		sectionsJSON, _ := json.Marshal(pageStruct.Sections)
		pagePrompt := fmt.Sprintf(
			"Website name: %s\nColor scheme: %s\nAll pages: %s\nCurrent page: %s (path: %s)\nSections for this page: %s\n\nUser's original description: %s",
			structure.Name,
			string(colorSchemeJSON),
			allPagesContext,
			pageStruct.Title,
			pageStruct.Path,
			string(sectionsJSON),
			req.Prompt,
		)

		htmlContent, err := utils.GetTextFromLLM(
			model,
			constants.WEBSITE_PAGE_HTML_PROMPT,
			pagePrompt,
			apiKey,
		)
		if err != nil {
			slog.Error("failed to generate page HTML", "error", err, "page", pageStruct.Title)
			htmlContent = fmt.Sprintf("<!DOCTYPE html><html><head><title>%s</title></head><body><h1>%s</h1><p>Page generation failed. Please try editing this page.</p></body></html>", pageStruct.Title, pageStruct.Title)
		}

		htmlContent = cleanHTMLOutput(htmlContent)

		page := models.WebsitePage{
			WebsiteID:   createdWebsite.ID,
			Path:        pageStruct.Path,
			Title:       pageStruct.Title,
			HTMLContent: htmlContent,
			SortOrder:   i,
		}

		createdPage, err := c.DB.CreatePage(&page)
		if err != nil {
			slog.Error("failed to create page", "error", err, "page", pageStruct.Title)
			continue
		}

		pageResponses = append(pageResponses, responses.PageResponse{
			ID:          createdPage.ID,
			WebsiteID:   createdPage.WebsiteID,
			Path:        createdPage.Path,
			Title:       createdPage.Title,
			HTMLContent: createdPage.HTMLContent,
			CSSContent:  createdPage.CSSContent,
			JSContent:   createdPage.JSContent,
			SortOrder:   createdPage.SortOrder,
			CreatedAt:   createdPage.CreatedAt,
			UpdatedAt:   createdPage.UpdatedAt,
		})
	}

	// Step 5: Log generation
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
		Pages: pageResponses,
	}, nil
}

func (c *Controller) GenerateWebsiteStream(ctx context.Context, user *models.User, req requests.WebsiteGenerateRequest, events chan<- responses.SSEEvent) {
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

	// Step 1: Generate website structure via LLM
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

	structureJSON, err := utils.GetJSONFromLLM(
		model,
		constants.WEBSITE_STRUCTURE_PROMPT,
		req.Prompt+templateHint,
		apiKey,
	)
	if err != nil {
		slog.Error("failed to generate website structure", "error", err)
		sendError("structure", "failed to generate website structure. try again please")
		return
	}

	var structure websiteStructure
	if err := json.Unmarshal([]byte(structureJSON), &structure); err != nil {
		slog.Error("failed to parse website structure", "error", err, "json", structureJSON)
		sendError("structure", "failed to parse website structure. try again please")
		return
	}

	if len(structure.Pages) == 0 {
		sendError("structure", "failed to generate website pages. try again please")
		return
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
		sendError("database", fmt.Sprintf("failed to create website: %v", err))
		return
	}

	// Send website_created event
	pageTitles := make([]string, len(structure.Pages))
	for i, p := range structure.Pages {
		pageTitles[i] = p.Title
	}

	if !sendEvent("website_created", responses.WebsiteCreatedEvent{
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
		TotalPages: len(structure.Pages),
		PageTitles: pageTitles,
	}) {
		return
	}

	// Step 4: Generate HTML for each page
	colorSchemeJSON, _ := json.Marshal(structure.ColorScheme)
	pageNames := make([]string, len(structure.Pages))
	for i, p := range structure.Pages {
		pageNames[i] = fmt.Sprintf("%s (%s)", p.Title, p.Path)
	}
	allPagesContext := strings.Join(pageNames, ", ")

	for i, pageStruct := range structure.Pages {
		if ctx.Err() != nil {
			return
		}

		sectionsJSON, _ := json.Marshal(pageStruct.Sections)
		pagePrompt := fmt.Sprintf(
			"Website name: %s\nColor scheme: %s\nAll pages: %s\nCurrent page: %s (path: %s)\nSections for this page: %s\n\nUser's original description: %s",
			structure.Name,
			string(colorSchemeJSON),
			allPagesContext,
			pageStruct.Title,
			pageStruct.Path,
			string(sectionsJSON),
			req.Prompt,
		)

		htmlContent, err := utils.StreamTextFromLLM(
			ctx,
			model,
			constants.WEBSITE_PAGE_HTML_PROMPT,
			pagePrompt,
			apiKey,
			func(chunk string) {
				sendEvent("code_chunk", responses.CodeChunkEvent{
					PageIndex:  i,
					PageTitle:  pageStruct.Title,
					Chunk:      chunk,
					TotalPages: len(structure.Pages),
				})
			},
		)
		if err != nil {
			slog.Error("failed to generate page HTML", "error", err, "page", pageStruct.Title)
			htmlContent = fmt.Sprintf("<!DOCTYPE html><html><head><title>%s</title></head><body><h1>%s</h1><p>Page generation failed. Please try editing this page.</p></body></html>", pageStruct.Title, pageStruct.Title)
		}

		htmlContent = cleanHTMLOutput(htmlContent)

		page := models.WebsitePage{
			WebsiteID:   createdWebsite.ID,
			Path:        pageStruct.Path,
			Title:       pageStruct.Title,
			HTMLContent: htmlContent,
			SortOrder:   i,
		}

		createdPage, err := c.DB.CreatePage(&page)
		if err != nil {
			slog.Error("failed to create page", "error", err, "page", pageStruct.Title)
			continue
		}

		if !sendEvent("page_generated", responses.PageGeneratedEvent{
			PageResponse: responses.PageResponse{
				ID:          createdPage.ID,
				WebsiteID:   createdPage.WebsiteID,
				Path:        createdPage.Path,
				Title:       createdPage.Title,
				HTMLContent: createdPage.HTMLContent,
				CSSContent:  createdPage.CSSContent,
				JSContent:   createdPage.JSContent,
				SortOrder:   createdPage.SortOrder,
				CreatedAt:   createdPage.CreatedAt,
				UpdatedAt:   createdPage.UpdatedAt,
			},
			PageIndex:  i,
			TotalPages: len(structure.Pages),
		}) {
			return
		}
	}

	// Step 5: Log generation
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

func (c *Controller) GetWebsites(user *models.User) ([]responses.WebsiteResponse, error) {
	websites, err := c.DB.GetWebsitesByUserId(user.ID.String())
	if err != nil {
		return nil, err
	}

	websiteResponses := make([]responses.WebsiteResponse, len(websites))
	for i, w := range websites {
		websiteResponses[i] = responses.WebsiteResponse{
			ID:              w.ID,
			Name:            w.Name,
			Slug:            w.Slug,
			Prompt:          w.Prompt,
			ColorScheme:     w.ColorScheme,
			DarkColorScheme: w.DarkColorScheme,
			Status:          w.Status,
			PublishedURL:    w.PublishedURL,
			ProjectID:       w.ProjectID,
			CreatedAt:       w.CreatedAt,
			UpdatedAt:       w.UpdatedAt,
		}
	}
	return websiteResponses, nil
}

func (c *Controller) GetWebsite(user *models.User, websiteId string) (*responses.WebsiteDetailResponse, error) {
	website, err := c.verifyWebsiteOwnership(user, websiteId)
	if err != nil {
		return nil, err
	}

	pages, err := c.DB.GetPagesByWebsiteId(website.ID.String())
	if err != nil {
		return nil, err
	}

	pageResponses := make([]responses.PageResponse, len(pages))
	for i, p := range pages {
		pageResponses[i] = responses.PageResponse{
			ID:          p.ID,
			WebsiteID:   p.WebsiteID,
			Path:        p.Path,
			Title:       p.Title,
			HTMLContent: p.HTMLContent,
			CSSContent:  p.CSSContent,
			JSContent:   p.JSContent,
			SortOrder:   p.SortOrder,
			CreatedAt:   p.CreatedAt,
			UpdatedAt:   p.UpdatedAt,
		}
	}

	// Also load files (new file-based model)
	files, _ := c.DB.GetFilesByWebsiteId(website.ID.String())
	fileResponses := make([]responses.FileResponse, len(files))
	for i, f := range files {
		fileResponses[i] = responses.FileResponse{
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

	return &responses.WebsiteDetailResponse{
		WebsiteResponse: responses.WebsiteResponse{
			ID:              website.ID,
			Name:            website.Name,
			Slug:            website.Slug,
			Prompt:          website.Prompt,
			ColorScheme:     website.ColorScheme,
			DarkColorScheme: website.DarkColorScheme,
			Status:          website.Status,
			PublishedURL:    website.PublishedURL,
			ProjectID:       website.ProjectID,
			CreatedAt:       website.CreatedAt,
			UpdatedAt:       website.UpdatedAt,
		},
		Pages: pageResponses,
		Files: fileResponses,
	}, nil
}

func (c *Controller) UpdateWebsiteInfo(user *models.User, websiteId string, req requests.WebsiteUpdateRequest) (*responses.WebsiteResponse, error) {
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := utils.ValidateFunc[requests.WebsiteUpdateRequest](req, validate); err != nil {
		return nil, err
	}

	website, err := c.verifyWebsiteOwnership(user, websiteId)
	if err != nil {
		return nil, err
	}

	// Check slug uniqueness if changed
	if req.Slug != website.Slug {
		newSlug := generateSlug(req.Slug)
		existing, err := c.DB.GetWebsiteBySlug(newSlug)
		if err == nil && existing.ID.String() != website.ID.String() {
			return nil, errors.New("slug already taken")
		}
		website.Slug = newSlug
	}

	website.Name = req.Name
	website.UpdatedAt = time.Now()

	updatedWebsite, err := c.DB.UpdateWebsite(website)
	if err != nil {
		return nil, err
	}

	return &responses.WebsiteResponse{
		ID:              updatedWebsite.ID,
		Name:            updatedWebsite.Name,
		Slug:            updatedWebsite.Slug,
		Prompt:          updatedWebsite.Prompt,
		ColorScheme:     updatedWebsite.ColorScheme,
		DarkColorScheme: updatedWebsite.DarkColorScheme,
		Status:          updatedWebsite.Status,
		PublishedURL:    updatedWebsite.PublishedURL,
		ProjectID:       updatedWebsite.ProjectID,
		CreatedAt:       updatedWebsite.CreatedAt,
		UpdatedAt:       updatedWebsite.UpdatedAt,
	}, nil
}

func (c *Controller) UpdateWebsiteTheme(user *models.User, websiteId string, req requests.UpdateThemeRequest) (*responses.WebsiteResponse, error) {
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := utils.ValidateFunc[requests.UpdateThemeRequest](req, validate); err != nil {
		return nil, err
	}

	website, err := c.verifyWebsiteOwnership(user, websiteId)
	if err != nil {
		return nil, err
	}

	website.ColorScheme = req.ColorScheme
	if req.DarkColorScheme != nil {
		website.DarkColorScheme = req.DarkColorScheme
	}
	website.UpdatedAt = time.Now()

	updatedWebsite, err := c.DB.UpdateWebsite(website)
	if err != nil {
		return nil, err
	}

	return &responses.WebsiteResponse{
		ID:              updatedWebsite.ID,
		Name:            updatedWebsite.Name,
		Slug:            updatedWebsite.Slug,
		Prompt:          updatedWebsite.Prompt,
		ColorScheme:     updatedWebsite.ColorScheme,
		DarkColorScheme: updatedWebsite.DarkColorScheme,
		Status:          updatedWebsite.Status,
		PublishedURL:    updatedWebsite.PublishedURL,
		ProjectID:       updatedWebsite.ProjectID,
		CreatedAt:       updatedWebsite.CreatedAt,
		UpdatedAt:       updatedWebsite.UpdatedAt,
	}, nil
}

func (c *Controller) DeleteWebsiteById(user *models.User, websiteId string) error {
	website, err := c.verifyWebsiteOwnership(user, websiteId)
	if err != nil {
		return err
	}

	return c.DB.DeleteWebsite(website)
}

// Page CRUD

func (c *Controller) GetWebsitePages(user *models.User, websiteId string) ([]responses.PageResponse, error) {
	_, err := c.verifyWebsiteOwnership(user, websiteId)
	if err != nil {
		return nil, err
	}

	pages, err := c.DB.GetPagesByWebsiteId(websiteId)
	if err != nil {
		return nil, err
	}

	pageResponses := make([]responses.PageResponse, len(pages))
	for i, p := range pages {
		pageResponses[i] = responses.PageResponse{
			ID:          p.ID,
			WebsiteID:   p.WebsiteID,
			Path:        p.Path,
			Title:       p.Title,
			HTMLContent: p.HTMLContent,
			CSSContent:  p.CSSContent,
			JSContent:   p.JSContent,
			SortOrder:   p.SortOrder,
			CreatedAt:   p.CreatedAt,
			UpdatedAt:   p.UpdatedAt,
		}
	}
	return pageResponses, nil
}

func (c *Controller) CreateWebsitePage(user *models.User, websiteId string, req requests.PageCreateRequest) (*responses.PageResponse, error) {
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := utils.ValidateFunc[requests.PageCreateRequest](req, validate); err != nil {
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

	// Get existing pages to determine sort order
	pages, _ := c.DB.GetPagesByWebsiteId(websiteId)
	sortOrder := len(pages)

	page := models.WebsitePage{
		WebsiteID:   websiteUUID,
		Path:        req.Path,
		Title:       req.Title,
		HTMLContent: req.HTMLContent,
		SortOrder:   sortOrder,
	}

	createdPage, err := c.DB.CreatePage(&page)
	if err != nil {
		return nil, err
	}

	return &responses.PageResponse{
		ID:          createdPage.ID,
		WebsiteID:   createdPage.WebsiteID,
		Path:        createdPage.Path,
		Title:       createdPage.Title,
		HTMLContent: createdPage.HTMLContent,
		SortOrder:   createdPage.SortOrder,
		CreatedAt:   createdPage.CreatedAt,
		UpdatedAt:   createdPage.UpdatedAt,
	}, nil
}

func (c *Controller) UpdateWebsitePage(user *models.User, websiteId string, pageId string, req requests.PageUpdateRequest) (*responses.PageResponse, error) {
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := utils.ValidateFunc[requests.PageUpdateRequest](req, validate); err != nil {
		return nil, err
	}

	_, err := c.verifyWebsiteOwnership(user, websiteId)
	if err != nil {
		return nil, err
	}

	page, err := c.DB.GetPageById(pageId)
	if err != nil {
		return nil, errors.New("page not found")
	}

	if page.WebsiteID.String() != websiteId {
		return nil, errors.New("page does not belong to this website")
	}

	page.Title = req.Title
	page.HTMLContent = req.HTMLContent
	page.UpdatedAt = time.Now()

	updatedPage, err := c.DB.UpdatePage(page)
	if err != nil {
		return nil, err
	}

	return &responses.PageResponse{
		ID:          updatedPage.ID,
		WebsiteID:   updatedPage.WebsiteID,
		Path:        updatedPage.Path,
		Title:       updatedPage.Title,
		HTMLContent: updatedPage.HTMLContent,
		CSSContent:  updatedPage.CSSContent,
		JSContent:   updatedPage.JSContent,
		SortOrder:   updatedPage.SortOrder,
		CreatedAt:   updatedPage.CreatedAt,
		UpdatedAt:   updatedPage.UpdatedAt,
	}, nil
}

func (c *Controller) DeleteWebsitePage(user *models.User, websiteId string, pageId string) error {
	_, err := c.verifyWebsiteOwnership(user, websiteId)
	if err != nil {
		return err
	}

	page, err := c.DB.GetPageById(pageId)
	if err != nil {
		return errors.New("page not found")
	}

	if page.WebsiteID.String() != websiteId {
		return errors.New("page does not belong to this website")
	}

	return c.DB.DeletePage(page)
}

// Publishing

func (c *Controller) PublishWebsite(user *models.User, websiteId string) (*responses.PublishResponse, error) {
	website, err := c.verifyWebsiteOwnership(user, websiteId)
	if err != nil {
		return nil, err
	}

	pages, err := c.DB.GetPagesByWebsiteId(websiteId)
	if err != nil {
		return nil, err
	}

	if len(pages) == 0 {
		return nil, errors.New("website has no pages to publish")
	}

	// Upload each page to S3
	for _, page := range pages {
		s3Path := fmt.Sprintf("sites/%s%s", website.Slug, pagePath(page.Path))
		err := c.AssetsStorage.UploadFileByteWithContentType(
			context.Background(),
			s3Path,
			[]byte(page.HTMLContent),
			"text/html",
		)
		if err != nil {
			slog.Error("failed to upload page to S3", "error", err, "path", s3Path)
			return nil, fmt.Errorf("failed to publish page: %s", page.Title)
		}
	}

	// Update website status
	publishedURL := fmt.Sprintf("https://sites.wemob.io/%s", website.Slug)
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

// AI Edit

func (c *Controller) AIEditPage(user *models.User, websiteId string, pageId string, req requests.AIEditPageRequest) (*responses.PageResponse, error) {
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := utils.ValidateFunc[requests.AIEditPageRequest](req, validate); err != nil {
		return nil, err
	}

	website, err := c.verifyWebsiteOwnership(user, websiteId)
	if err != nil {
		return nil, err
	}

	page, err := c.DB.GetPageById(pageId)
	if err != nil {
		return nil, errors.New("page not found")
	}

	if page.WebsiteID.String() != websiteId {
		return nil, errors.New("page does not belong to this website")
	}

	apiKey := c.SecretManager.GetSecret("OPENROUTER_API_KEY")
	model := c.getWebsiteLLMModel()

	editPrompt := fmt.Sprintf(
		"Current HTML:\n```html\n%s\n```\n\nEdit instruction: %s",
		page.HTMLContent,
		req.Prompt,
	)

	newHTML, err := utils.GetTextFromLLM(
		model,
		constants.WEBSITE_EDIT_PAGE_PROMPT,
		editPrompt,
		apiKey,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to edit page. try again please")
	}

	newHTML = cleanHTMLOutput(newHTML)

	page.HTMLContent = newHTML
	page.UpdatedAt = time.Now()

	updatedPage, err := c.DB.UpdatePage(page)
	if err != nil {
		return nil, err
	}

	// Log generation
	c.DB.CreateAIGenerationLog(&models.AIGenerationLog{
		WebsiteID: website.ID,
		UserID:    user.ID,
		Prompt:    req.Prompt,
		Model:     model,
	})

	return &responses.PageResponse{
		ID:          updatedPage.ID,
		WebsiteID:   updatedPage.WebsiteID,
		Path:        updatedPage.Path,
		Title:       updatedPage.Title,
		HTMLContent: updatedPage.HTMLContent,
		CSSContent:  updatedPage.CSSContent,
		JSContent:   updatedPage.JSContent,
		SortOrder:   updatedPage.SortOrder,
		CreatedAt:   updatedPage.CreatedAt,
		UpdatedAt:   updatedPage.UpdatedAt,
	}, nil
}

func (c *Controller) AIAddPage(user *models.User, websiteId string, req requests.AIAddPageRequest) (*responses.PageResponse, error) {
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := utils.ValidateFunc[requests.AIAddPageRequest](req, validate); err != nil {
		return nil, err
	}

	website, err := c.verifyWebsiteOwnership(user, websiteId)
	if err != nil {
		return nil, err
	}

	pages, err := c.DB.GetPagesByWebsiteId(websiteId)
	if err != nil {
		return nil, err
	}

	// Build context from existing pages
	colorSchemeJSON, _ := json.Marshal(website.ColorScheme)
	pageNames := make([]string, len(pages))
	for i, p := range pages {
		pageNames[i] = fmt.Sprintf("%s (%s)", p.Title, p.Path)
	}
	existingPages := strings.Join(pageNames, ", ")

	apiKey := c.SecretManager.GetSecret("OPENROUTER_API_KEY")
	model := c.getWebsiteLLMModel()

	addPrompt := fmt.Sprintf(
		"Website name: %s\nColor scheme: %s\nExisting pages: %s\n\nNew page request: %s",
		website.Name,
		string(colorSchemeJSON),
		existingPages,
		req.Prompt,
	)

	htmlContent, err := utils.GetTextFromLLM(
		model,
		constants.WEBSITE_ADD_PAGE_PROMPT,
		addPrompt,
		apiKey,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to generate new page. try again please")
	}

	htmlContent = cleanHTMLOutput(htmlContent)

	// Determine path and title from prompt (simple heuristic)
	pageTitle := req.Prompt
	if len(pageTitle) > 50 {
		pageTitle = pageTitle[:50]
	}
	pagePath := "/" + generateSlug(pageTitle)

	page := models.WebsitePage{
		WebsiteID:   website.ID,
		Path:        pagePath,
		Title:       pageTitle,
		HTMLContent: htmlContent,
		SortOrder:   len(pages),
	}

	createdPage, err := c.DB.CreatePage(&page)
	if err != nil {
		return nil, err
	}

	// Log generation
	c.DB.CreateAIGenerationLog(&models.AIGenerationLog{
		WebsiteID: website.ID,
		UserID:    user.ID,
		Prompt:    req.Prompt,
		Model:     model,
	})

	return &responses.PageResponse{
		ID:          createdPage.ID,
		WebsiteID:   createdPage.WebsiteID,
		Path:        createdPage.Path,
		Title:       createdPage.Title,
		HTMLContent: createdPage.HTMLContent,
		SortOrder:   createdPage.SortOrder,
		CreatedAt:   createdPage.CreatedAt,
		UpdatedAt:   createdPage.UpdatedAt,
	}, nil
}

// Convert to Mobile

func (c *Controller) ConvertWebsiteToMobile(user *models.User, websiteId string, req requests.ConvertToMobileRequest) (*responses.ConvertToMobileResponse, error) {
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := utils.ValidateFunc[requests.ConvertToMobileRequest](req, validate); err != nil {
		return nil, err
	}

	website, err := c.verifyWebsiteOwnership(user, websiteId)
	if err != nil {
		return nil, err
	}

	if website.Status != "published" {
		return nil, errors.New("website must be published before converting to mobile")
	}

	if website.PublishedURL == "" {
		return nil, errors.New("website does not have a published URL")
	}

	// Check if already converted
	if website.ProjectID != uuid.Nil {
		return nil, errors.New("website has already been converted to a mobile app")
	}

	// Create package name
	pkName, err := utils.RandomHex(5)
	if err != nil {
		return nil, err
	}
	pkName = "com.wemob.site" + pkName[0:5]

	project := models.Project{
		UserID:          user.ID,
		URL:             website.PublishedURL,
		BuildForAndroid: req.BuildForAndroid,
		BuildForIOS:     req.BuildForIOS,
		PackageName:     pkName,
	}

	// Create OneSignal app
	onesignalAppID, err := external.CreateOneSignalApplication(
		project,
		c.SecretManager.GetSecret("ONESIGNAL_ORG_ID"),
		c.SecretManager.GetSecret("ONESIGNAL_ORG_API_KEY"),
		c.SecretManager.GetSecret("FCM_V1_SERVICE_ACCOUNT_JSON_B64"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create notification service: %v", err)
	}

	project.OneSignalAppID = onesignalAppID

	time.Sleep(2 * time.Second)

	onesignalAppToken, err := external.CreateOneSignalAppToken(
		project,
		c.SecretManager.GetSecret("ONESIGNAL_ORG_API_KEY"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create notification token: %v", err)
	}
	project.OneSignalAppToken = onesignalAppToken

	createdProject, err := c.DB.CreateProject(&project)
	if err != nil {
		return nil, fmt.Errorf("failed to create mobile project: %v", err)
	}

	// Link website to project
	website.ProjectID = createdProject.ID
	website.UpdatedAt = time.Now()
	c.DB.UpdateWebsite(website)

	return &responses.ConvertToMobileResponse{
		SuccessResponse: responses.SuccessResponse{Success: true},
		ProjectID:       createdProject.ID,
	}, nil
}

// Templates

func (c *Controller) GetWebsiteTemplates() ([]responses.TemplateResponse, error) {
	templates, err := c.DB.GetActiveTemplates()
	if err != nil {
		return nil, err
	}

	templateResponses := make([]responses.TemplateResponse, len(templates))
	for i, t := range templates {
		templateResponses[i] = responses.TemplateResponse{
			ID:         t.ID,
			Name:       t.Name,
			Category:   t.Category,
			PreviewURL: t.PreviewURL,
		}
	}
	return templateResponses, nil
}

// Asset upload

func (c *Controller) UploadWebsiteAsset(user *models.User, websiteId string, fileName string, contentType string, data []byte) (*models.WebsiteAsset, error) {
	website, err := c.verifyWebsiteOwnership(user, websiteId)
	if err != nil {
		return nil, err
	}

	assetType := "image"
	if strings.HasPrefix(contentType, "font/") || strings.HasPrefix(contentType, "application/font") {
		assetType = "font"
	} else if strings.HasPrefix(contentType, "video/") {
		assetType = "video"
	}

	s3Path := fmt.Sprintf("sites/%s/assets/%s/%s", website.Slug, assetType, fileName)
	err = c.AssetsStorage.UploadFileByteWithContentType(
		context.Background(),
		s3Path,
		data,
		contentType,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to upload asset: %v", err)
	}

	asset := models.WebsiteAsset{
		WebsiteID:    website.ID,
		Type:         assetType,
		S3Path:       s3Path,
		OriginalName: fileName,
		ContentType:  contentType,
		Size:         int64(len(data)),
	}

	createdAsset, err := c.DB.CreateAsset(&asset)
	if err != nil {
		return nil, err
	}

	return createdAsset, nil
}

// Helpers

func generateSlug(name string) string {
	slug := strings.ToLower(name)
	slug = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == ' ' {
			return r
		}
		return -1
	}, slug)
	slug = strings.ReplaceAll(slug, " ", "-")
	// Remove consecutive hyphens
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}
	slug = strings.Trim(slug, "-")
	if len(slug) > 60 {
		slug = slug[:60]
	}
	return slug
}

func pagePath(path string) string {
	if path == "/" {
		return "/index.html"
	}
	path = strings.TrimSuffix(path, "/")
	return path + "/index.html"
}

func cleanHTMLOutput(html string) string {
	html = strings.TrimSpace(html)
	html = strings.TrimPrefix(html, "```html")
	html = strings.TrimPrefix(html, "```")
	html = strings.TrimSuffix(html, "```")
	return strings.TrimSpace(html)
}
