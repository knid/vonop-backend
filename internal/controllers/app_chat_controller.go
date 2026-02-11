package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
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
	Content string `json:"-"`
}

// UnmarshalJSON handles content being either a string or an inline JSON object.
// LLMs sometimes return JSON file content as an object instead of a string.
func (o *appChatOperation) UnmarshalJSON(data []byte) error {
	type Alias appChatOperation
	aux := &struct {
		Content json.RawMessage `json:"content"`
		*Alias
	}{Alias: (*Alias)(o)}

	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	if len(aux.Content) == 0 {
		return nil
	}

	// Try string first
	var s string
	if err := json.Unmarshal(aux.Content, &s); err == nil {
		o.Content = s
		return nil
	}

	// Content is a JSON object/array — re-marshal it to a formatted string
	o.Content = string(aux.Content)
	// Pretty-print for readability
	var pretty json.RawMessage
	if json.Unmarshal(aux.Content, &pretty) == nil {
		if formatted, err := json.MarshalIndent(pretty, "", "  "); err == nil {
			o.Content = string(formatted)
		}
	}
	return nil
}

type appChatResponse struct {
	Operations []appChatOperation `json:"operations"`
	Summary    string             `json:"summary"`
}

// appChatTools defines the tools available to the chat LLM for on-demand file access.
var appChatTools = []utils.Tool{
	{
		Type: "function",
		Function: utils.ToolFunction{
			Name:        "read_file",
			Description: "Read the full content of a project file by its path. Use this to see a file's current implementation before modifying it.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file_path": map[string]any{
						"type":        "string",
						"description": "Relative file path, e.g. 'app/page.tsx' or 'components/header.tsx'",
					},
				},
				"required": []string{"file_path"},
			},
		},
	},
	{
		Type: "function",
		Function: utils.ToolFunction{
			Name:        "list_files",
			Description: "List all files in the project with their types and sizes. Use this to understand the project structure.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{},
			},
		},
	},
	{
		Type: "function",
		Function: utils.ToolFunction{
			Name:        "search_files",
			Description: "Search for files containing a specific text pattern. Returns matching file paths and the matching lines.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Text to search for in file contents (case-insensitive)",
					},
				},
				"required": []string{"query"},
			},
		},
	},
}

// buildChatToolExecutor returns a ToolExecutor that can read/list/search project files.
// fileMap is the in-memory map of path→content for the current project.
func buildChatToolExecutor(fileMap map[string]string) utils.ToolExecutor {
	return func(name string, arguments string) (string, error) {
		switch name {
		case "read_file":
			var args struct {
				FilePath string `json:"file_path"`
			}
			json.Unmarshal([]byte(arguments), &args)
			content, ok := fileMap[args.FilePath]
			if !ok {
				return fmt.Sprintf("Error: file '%s' not found in project", args.FilePath), nil
			}
			return content, nil

		case "list_files":
			var lines []string
			for path, content := range fileMap {
				lines = append(lines, fmt.Sprintf("%s (%d chars)", path, len(content)))
			}
			sort.Strings(lines)
			return strings.Join(lines, "\n"), nil

		case "search_files":
			var args struct {
				Query string `json:"query"`
			}
			json.Unmarshal([]byte(arguments), &args)
			query := strings.ToLower(args.Query)
			var results []string
			for path, content := range fileMap {
				lines := strings.Split(content, "\n")
				for i, line := range lines {
					if strings.Contains(strings.ToLower(line), query) {
						results = append(results, fmt.Sprintf("%s:%d: %s", path, i+1, strings.TrimSpace(line)))
					}
				}
			}
			if len(results) == 0 {
				return "No matches found.", nil
			}
			if len(results) > 50 {
				results = results[:50]
			}
			return strings.Join(results, "\n"), nil

		default:
			return fmt.Sprintf("Unknown tool: %s", name), nil
		}
	}
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

	// Build in-memory file map for tool executor
	currentFiles := make(map[string]string)
	for _, f := range files {
		currentFiles[f.FilePath] = f.Content
	}

	// Build lightweight file tree (paths + sizes only) — LLM will use tools to read files
	var fileTree strings.Builder
	fileTree.WriteString("Current project files:\n")
	filePaths := make([]string, 0, len(files))
	for _, f := range files {
		filePaths = append(filePaths, f.FilePath)
	}
	sort.Strings(filePaths)
	for _, fp := range filePaths {
		content := currentFiles[fp]
		fileTree.WriteString(fmt.Sprintf("- %s (%d chars)\n", fp, len(content)))
	}
	fileTree.WriteString("\nUse the read_file tool to view any file's content before modifying it.")

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
	if _, err := c.DB.UpdateAppConversation(conv); err != nil {
		slog.Error("failed to save user message to conversation", "error", err, "conv_id", conv.ID)
	}

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

	// Send thinking event so the frontend shows progress
	sendEvent("chat_thinking", responses.ChatThinkingEvent{Chunk: ""})

	slog.Info("Calling LLM for chat", "model", model, "project_id", project.ID, "message_count", len(llmMessages))

	// Call LLM with tool calling — LLM reads files on demand instead of all files in context
	executor := buildChatToolExecutor(currentFiles)
	responseJSON, err := utils.StreamWithTools(
		ctx,
		model,
		llmMessages,
		appChatTools,
		apiKey,
		&utils.ResponseFormat{Type: "json_object"},
		executor,
		func(toolName, args string) {
			sendEvent("tool_call", responses.ToolCallEvent{
				Tool:      toolName,
				Arguments: args,
			})
		},
		nil,
	)
	if err != nil {
		slog.Error("failed to get chat response from LLM", "error", err, "response_so_far", len(responseJSON))
		// Save the error as assistant message so it persists on refresh
		messages = append(messages, models.AppConversationMessage{
			Role:      "assistant",
			Content:   "Sorry, I couldn't process your request. Please try again.",
			Timestamp: time.Now().Unix(),
		})
		conv.Messages, _ = json.Marshal(messages)
		if _, err := c.DB.UpdateAppConversation(conv); err != nil {
			slog.Error("failed to save error message to conversation", "error", err, "conv_id", conv.ID)
		}
		sendError("ai", "failed to generate changes. try again please")
		return
	}

	slog.Info("Chat LLM response received", "response_length", len(responseJSON), "project_id", project.ID)

	// StreamJSONFromLLMWithMessages already calls CleanLLMOutput, so responseJSON
	// should be clean JSON. Try direct parse first.
	var chatResp appChatResponse
	if err := json.Unmarshal([]byte(responseJSON), &chatResp); err != nil {
		// Fallback: strip any remaining markdown fences
		cleaned := strings.TrimSpace(responseJSON)
		cleaned = strings.TrimPrefix(cleaned, "```json")
		cleaned = strings.TrimPrefix(cleaned, "```")
		cleaned = strings.TrimSuffix(cleaned, "```")
		cleaned = strings.TrimSpace(cleaned)

		if err2 := json.Unmarshal([]byte(cleaned), &chatResp); err2 != nil {
			// Last fallback: extract JSON object from response
			extracted := utils.ExtractJSON(responseJSON)
			if err3 := json.Unmarshal([]byte(extracted), &chatResp); err3 != nil {
				// Log details for debugging
				preview := responseJSON
				if len(preview) > 500 {
					preview = preview[:250] + "\n...[truncated]...\n" + preview[len(preview)-250:]
				}
				slog.Error("failed to parse chat response — all attempts failed",
					"parse_err", err,
					"clean_err", err2,
					"extract_err", err3,
					"response_length", len(responseJSON),
					"response_preview", preview,
				)
				chatResp = appChatResponse{
					Operations: []appChatOperation{},
					Summary:    "I wasn't able to generate the changes properly. Please try again with a more specific request.",
				}
			}
		}
	}

	slog.Info("Chat response parsed",
		"operations_count", len(chatResp.Operations),
		"summary_length", len(chatResp.Summary),
		"project_id", project.ID,
	)

	// Apply operations (filter out template files)
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
			currentFiles[op.Path] = content
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
				// File not found — fall back to creating it instead of silently skipping
				slog.Warn("file not found for modify, creating instead", "path", op.Path)
				content := cleanAppFileOutput(op.Content, op.Type)
				fileType := op.Type
				if fileType == "" {
					_, fileType = guessFileType(op.Path)
				}
				if fileType == "tsx" || fileType == "ts" {
					content, _ = stripForbiddenImports(content, op.Path)
				}
				newFile := models.AppGeneratedFile{
					AppProjectID: project.ID,
					Platform:     "nextjs",
					FilePath:     op.Path,
					Content:      content,
					FileType:     fileType,
					IsGenerated:  true,
				}
				createdFile, createErr := c.DB.CreateAppGeneratedFile(&newFile)
				if createErr != nil {
					slog.Error("failed to create file (modify fallback)", "error", createErr, "path", op.Path)
					continue
				}
				currentFiles[op.Path] = content
				if !sendEvent("file_complete", responses.FileCompleteEvent{
					Action:   "create",
					FilePath: op.Path,
					FileID:   createdFile.ID,
					FileType: fileType,
				}) {
					return
				}
				filesChanged++
				continue
			}
			content := cleanAppFileOutput(op.Content, op.Type)
			if op.Type == "tsx" || op.Type == "ts" || existingFile.FileType == "tsx" || existingFile.FileType == "ts" {
				content, _ = stripForbiddenImports(content, op.Path)
			}
			existingFile.Content = content
			existingFile.UpdatedAt = time.Now()
			_, err = c.DB.UpdateAppGeneratedFile(existingFile)
			if err != nil {
				slog.Error("failed to update file", "error", err, "path", op.Path)
				continue
			}
			currentFiles[op.Path] = content
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
			delete(currentFiles, op.Path)
			if !sendEvent("file_complete", responses.FileCompleteEvent{
				Action:   "delete",
				FilePath: op.Path,
			}) {
				return
			}
			filesChanged++
		}
	}

	// Run deterministic validation + LLM fix on modified files
	if filesChanged > 0 {
		fixedCount := c.validateAndFixFiles(ctx, project, currentFiles, nil, sendEvent)
		if fixedCount > 0 {
			slog.Info("Chat validation fixed files", "project_id", project.ID, "fixed_count", fixedCount)
		}

		// Augment package.json with any new third-party imports added by the chat
		if pkgContent, ok := currentFiles["package.json"]; ok {
			augmented := augmentPackageJSON(pkgContent, currentFiles)
			if augmented != pkgContent {
				dbFile, err := c.DB.GetAppGeneratedFileByPath(project.ID.String(), "nextjs", "package.json")
				if err == nil && dbFile != nil {
					dbFile.Content = augmented
					c.DB.UpdateAppGeneratedFile(dbFile)
					currentFiles["package.json"] = augmented
					slog.Info("Chat augmented package.json with discovered deps", "project_id", project.ID)
				}
			}
		}

		// Auto-sync to preview container if running
		if c.PreviewService != nil {
			go c.SyncPreviewFiles(project.ID.String())
		}
	}

	// Save assistant response to conversation
	messages = append(messages, models.AppConversationMessage{
		Role:      "assistant",
		Content:   chatResp.Summary,
		Timestamp: time.Now().Unix(),
	})
	conv.Messages, _ = json.Marshal(messages)
	if _, err := c.DB.UpdateAppConversation(conv); err != nil {
		slog.Error("failed to save assistant message to conversation", "error", err, "conv_id", conv.ID)
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
