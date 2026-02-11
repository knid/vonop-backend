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

// MigrateWebsiteToApp converts a v1 Website into a v2 AppProject.
// Uses the code-first approach: analyzes existing website content and generates
// a file structure plan, then creates files directly.
func (c *Controller) MigrateWebsiteToApp(user *models.User, websiteID string) (*models.AppProject, error) {
	// 1. Get the website and verify ownership
	website, err := c.DB.GetWebsiteById(websiteID)
	if err != nil {
		return nil, fmt.Errorf("website not found")
	}
	if website.UserID != user.ID {
		return nil, fmt.Errorf("unauthorized")
	}

	// 2. Collect all website content (prefer files, fall back to pages)
	var contentSummary strings.Builder
	contentSummary.WriteString(fmt.Sprintf("Website Name: %s\n", website.Name))
	contentSummary.WriteString(fmt.Sprintf("Original Prompt: %s\n\n", website.Prompt))

	files, err := c.DB.GetFilesByWebsiteId(websiteID)
	if err == nil && len(files) > 0 {
		for _, f := range files {
			contentSummary.WriteString(fmt.Sprintf("--- File: %s (type: %s) ---\n", f.FilePath, f.FileType))
			content := f.Content
			if len(content) > 8000 {
				content = content[:8000] + "\n... (truncated)"
			}
			contentSummary.WriteString(content)
			contentSummary.WriteString("\n\n")
		}
	} else {
		pages, err := c.DB.GetPagesByWebsiteId(websiteID)
		if err == nil {
			for _, p := range pages {
				contentSummary.WriteString(fmt.Sprintf("--- Page: %s (path: %s) ---\n", p.Title, p.Path))
				html := p.HTMLContent
				if len(html) > 8000 {
					html = html[:8000] + "\n... (truncated)"
				}
				contentSummary.WriteString(html)
				contentSummary.WriteString("\n\n")
			}
		}
	}

	// 3. Use LLM to plan a Next.js file structure from the existing website content
	migrationPrompt := fmt.Sprintf(`Migrate the following website to a Next.js 14 App Router application.
Preserve the design, layout, and functionality as closely as possible.
Extract the color scheme from the existing styles.

%s`, contentSummary.String())

	structure, err := c.PlanAppFileStructure(context.Background(), migrationPrompt, "app")
	if err != nil {
		slog.Error("LLM migration error", "error", err, "websiteId", websiteID)
		return nil, fmt.Errorf("failed to analyze website for migration")
	}

	// 4. Create new AppProject
	websiteUUID, _ := uuid.Parse(websiteID)

	name := structure.Name
	if name == "" {
		name = website.Name
	}
	slug := structure.Slug
	if slug == "" {
		slug = website.Slug + "-app"
	}
	slug = slug + "-" + uuid.New().String()[:8]

	themeData := models.AppTheme{
		ColorScheme:     structure.ColorScheme,
		DarkColorScheme: structure.DarkColorScheme,
		Typography:      structure.Typography,
	}
	themeJSON, _ := json.Marshal(themeData)

	project := &models.AppProject{
		UserID:          user.ID,
		Name:            name,
		Slug:            slug,
		Prompt:          website.Prompt,
		ProjectType:     "app",
		AST:             []byte("[]"),
		Theme:           themeJSON,
		DataModels:      []byte("[]"),
		Settings:        map[string]any{"migrated_from": websiteID},
		Status:          "generating",
		LegacyWebsiteID: websiteUUID,
	}

	project, err = c.DB.CreateAppProject(project)
	if err != nil {
		slog.Error("Failed to create migrated app project", "error", err)
		return nil, fmt.Errorf("failed to create app project")
	}

	// 5. Generate Next.js files from the plan (code-first)
	provider := c.llmProvider()
	model := c.SecretManager.GetSecret("NEXTJS_BUILDER_LLM_MODEL")
	if model == "" {
		model = constants.DEFAULT_NEXTJS_LLM_MODEL
	}

	colorSchemeJSON, _ := json.Marshal(structure.ColorScheme)
	filePaths := make([]string, len(structure.Files))
	for i, f := range structure.Files {
		filePaths[i] = f.Path
	}
	allFilesContext := strings.Join(filePaths, ", ")

	for _, filePlan := range structure.Files {
		prompt := c.getAppFilePrompt(filePlan.Type)
		filePrompt := fmt.Sprintf(
			"App name: %s\nColor scheme: %s\nTypography: %v\nAll files: %s\nCurrent file: %s\nDescription: %s\n\nUser's original description: %s",
			structure.Name,
			string(colorSchemeJSON),
			structure.Typography,
			allFilesContext,
			filePlan.Path,
			filePlan.Description,
			website.Prompt,
		)

		content, err := utils.GetTextFromLLM(model, prompt, filePrompt, provider)
		if err != nil {
			slog.Error("failed to generate migration file", "error", err, "path", filePlan.Path)
			content = c.getAppFileFallback(filePlan)
		}

		content = cleanAppFileOutput(content, filePlan.Type)

		file := &models.AppGeneratedFile{
			AppProjectID: project.ID,
			Platform:     "nextjs",
			FilePath:     filePlan.Path,
			Content:      content,
			FileType:     filePlan.Type,
			IsGenerated:  true,
		}
		c.DB.CreateAppGeneratedFile(file)
	}

	// Create conversation
	assistantMsg := fmt.Sprintf("Migrated website \"%s\" to app with %d files.", website.Name, len(structure.Files))
	convMessages := []models.AppConversationMessage{
		{Role: "user", Content: "Migrate website to app", Timestamp: time.Now().Unix()},
		{Role: "assistant", Content: assistantMsg, Timestamp: time.Now().Unix()},
	}
	convJSON, _ := json.Marshal(convMessages)
	c.DB.CreateAppConversation(&models.AppConversation{
		AppProjectID: project.ID,
		Messages:     convJSON,
	})

	project.Status = "ready"
	c.DB.UpdateAppProject(project)

	return project, nil
}

// MigrateWebsiteToAppStream converts a v1 Website into a v2 AppProject with SSE streaming.
func (c *Controller) MigrateWebsiteToAppStream(ctx context.Context, user *models.User, websiteID string, events chan<- responses.SSEEvent) {
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

	// 1. Get the website and verify ownership
	website, err := c.DB.GetWebsiteById(websiteID)
	if err != nil {
		sendError("validation", "website not found")
		return
	}
	if website.UserID != user.ID {
		sendError("validation", "unauthorized")
		return
	}

	// 2. Collect all website content
	var contentSummary strings.Builder
	contentSummary.WriteString(fmt.Sprintf("Website Name: %s\n", website.Name))
	contentSummary.WriteString(fmt.Sprintf("Original Prompt: %s\n\n", website.Prompt))

	files, err := c.DB.GetFilesByWebsiteId(websiteID)
	if err == nil && len(files) > 0 {
		for _, f := range files {
			contentSummary.WriteString(fmt.Sprintf("--- File: %s (type: %s) ---\n", f.FilePath, f.FileType))
			content := f.Content
			if len(content) > 8000 {
				content = content[:8000] + "\n... (truncated)"
			}
			contentSummary.WriteString(content)
			contentSummary.WriteString("\n\n")
		}
	} else {
		pages, err := c.DB.GetPagesByWebsiteId(websiteID)
		if err == nil {
			for _, p := range pages {
				contentSummary.WriteString(fmt.Sprintf("--- Page: %s (path: %s) ---\n", p.Title, p.Path))
				html := p.HTMLContent
				if len(html) > 8000 {
					html = html[:8000] + "\n... (truncated)"
				}
				contentSummary.WriteString(html)
				contentSummary.WriteString("\n\n")
			}
		}
	}

	if ctx.Err() != nil {
		return
	}

	// 3. Plan structure (streamed)
	migrationPrompt := fmt.Sprintf(`Migrate the following website to a Next.js 14 App Router application.
Preserve the design, layout, and functionality as closely as possible.
Extract the color scheme from the existing styles.

%s`, contentSummary.String())

	structure, err := c.PlanAppFileStructureStream(ctx, migrationPrompt, "app", func(chunk string) {
		sendEvent("planning_chunk", responses.PlanningChunkEvent{
			Chunk: chunk,
			Phase: "structure",
		})
	})
	if err != nil {
		slog.Error("LLM migration error", "error", err, "websiteId", websiteID)
		sendError("structure", "failed to analyze website for migration")
		return
	}

	// 4. Create new AppProject
	websiteUUID, _ := uuid.Parse(websiteID)

	name := structure.Name
	if name == "" {
		name = website.Name
	}
	slug := structure.Slug
	if slug == "" {
		slug = website.Slug + "-app"
	}
	slug = slug + "-" + uuid.New().String()[:8]

	themeData := models.AppTheme{
		ColorScheme:     structure.ColorScheme,
		DarkColorScheme: structure.DarkColorScheme,
		Typography:      structure.Typography,
	}
	themeJSON, _ := json.Marshal(themeData)

	project := &models.AppProject{
		UserID:          user.ID,
		Name:            name,
		Slug:            slug,
		Prompt:          website.Prompt,
		ProjectType:     "app",
		AST:             []byte("[]"),
		Theme:           themeJSON,
		DataModels:      []byte("[]"),
		Settings:        map[string]any{"migrated_from": websiteID},
		Status:          "generating",
		LegacyWebsiteID: websiteUUID,
	}

	project, err = c.DB.CreateAppProject(project)
	if err != nil {
		slog.Error("Failed to create migrated app project", "error", err)
		sendError("database", "failed to create app project")
		return
	}

	// Send project_created event
	filePaths := make([]string, len(structure.Files))
	for i, f := range structure.Files {
		filePaths[i] = f.Path
	}

	if !sendEvent("project_created", map[string]any{
		"id":         project.ID,
		"name":       project.Name,
		"slug":       project.Slug,
		"files":      filePaths,
		"totalFiles": len(structure.Files),
	}) {
		return
	}

	// 5. Generate files (streamed)
	provider := c.llmProvider()
	model := c.SecretManager.GetSecret("NEXTJS_BUILDER_LLM_MODEL")
	if model == "" {
		model = constants.DEFAULT_NEXTJS_LLM_MODEL
	}

	colorSchemeJSON, _ := json.Marshal(structure.ColorScheme)
	allFilesContext := strings.Join(filePaths, ", ")

	for _, filePlan := range structure.Files {
		if ctx.Err() != nil {
			return
		}

		prompt := c.getAppFilePrompt(filePlan.Type)
		filePrompt := fmt.Sprintf(
			"App name: %s\nColor scheme: %s\nTypography: %v\nAll files: %s\nCurrent file: %s\nDescription: %s\n\nUser's original description: %s",
			structure.Name,
			string(colorSchemeJSON),
			structure.Typography,
			allFilesContext,
			filePlan.Path,
			filePlan.Description,
			website.Prompt,
		)

		content, err := utils.StreamTextFromLLM(
			ctx,
			model,
			prompt,
			filePrompt,
			provider,
			c.reasoningConfig(),
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
			slog.Error("failed to generate migration file", "error", err, "path", filePlan.Path)
			content = c.getAppFileFallback(filePlan)
		}

		content = cleanAppFileOutput(content, filePlan.Type)

		file := &models.AppGeneratedFile{
			AppProjectID: project.ID,
			Platform:     "nextjs",
			FilePath:     filePlan.Path,
			Content:      content,
			FileType:     filePlan.Type,
			IsGenerated:  true,
		}

		createdFile, err := c.DB.CreateAppGeneratedFile(file)
		if err != nil {
			slog.Error("failed to save migration file", "error", err, "path", filePlan.Path)
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
	assistantMsgStream := fmt.Sprintf("Migrated website \"%s\" to app with %d files.", website.Name, len(structure.Files))
	streamConvMessages := []models.AppConversationMessage{
		{Role: "user", Content: "Migrate website to app", Timestamp: time.Now().Unix()},
		{Role: "assistant", Content: assistantMsgStream, Timestamp: time.Now().Unix()},
	}
	streamConvJSON, _ := json.Marshal(streamConvMessages)
	c.DB.CreateAppConversation(&models.AppConversation{
		AppProjectID: project.ID,
		Messages:     streamConvJSON,
	})

	project.Status = "ready"
	c.DB.UpdateAppProject(project)

	sendEvent("done", map[string]any{
		"project_id": project.ID,
		"success":    true,
		"message":    "migration completed",
	})
}
