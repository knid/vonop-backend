package controllers

import (
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

// GenerateWebsiteFromScreenshot creates a website from a screenshot using vision LLM.
func (c *Controller) GenerateWebsiteFromScreenshot(user *models.User, req requests.ScreenshotToCodeRequest) (*responses.WebsiteDetailResponse, error) {
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := utils.ValidateFunc[requests.ScreenshotToCodeRequest](req, validate); err != nil {
		return nil, err
	}

	apiKey := c.SecretManager.GetSecret("OPENROUTER_API_KEY")
	model := c.getWebsiteLLMModel()

	// Step 1: Analyze screenshot with vision LLM
	structureJSON, err := utils.GetJSONFromLLMWithVision(
		model,
		constants.WEBSITE_SCREENSHOT_TO_CODE_PROMPT,
		"Analyze this screenshot and generate a website structure.",
		req.ImageBase64,
		apiKey,
	)
	if err != nil {
		slog.Error("failed to analyze screenshot", "error", err)
		return nil, fmt.Errorf("failed to analyze screenshot. try again please")
	}

	var structure fileStructure
	if err := json.Unmarshal([]byte(structureJSON), &structure); err != nil {
		slog.Error("failed to parse screenshot analysis", "error", err, "json", structureJSON)
		return nil, fmt.Errorf("failed to parse screenshot analysis. try again please")
	}

	if len(structure.Files) == 0 {
		return nil, fmt.Errorf("failed to generate website files from screenshot. try again please")
	}

	// Step 2: Generate slug
	slug := generateSlug(structure.Name)
	if _, err := c.DB.GetWebsiteBySlug(slug); err == nil {
		suffix, _ := utils.RandomHex(3)
		slug = slug + "-" + suffix
	}

	// Step 3: Create website
	website := models.Website{
		UserID:      user.ID,
		Name:        structure.Name,
		Slug:        slug,
		Prompt:      "Generated from screenshot",
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

	// Step 4: Generate file contents
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
			"Website name: %s\nColor scheme: %s\nAll files: %s\nCurrent file: %s\nDescription: %s\n\nGenerated from a screenshot analysis.",
			structure.Name,
			string(colorSchemeJSON),
			allFilesContext,
			filePlan.Path,
			filePlan.Description,
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
				Content:   "Generated from screenshot",
				Timestamp: time.Now().Unix(),
			},
			{
				Role:      "assistant",
				Content:   fmt.Sprintf("Created website \"%s\" from screenshot with %d files.", structure.Name, len(fileResponses)),
				Timestamp: time.Now().Unix(),
			},
		},
	})

	// Step 6: Log generation
	c.DB.CreateAIGenerationLog(&models.AIGenerationLog{
		WebsiteID: createdWebsite.ID,
		UserID:    user.ID,
		Prompt:    "screenshot-to-code",
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
