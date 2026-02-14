package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/services/integrations"
	"github.com/knid/wemob/internal/utils"
	"github.com/posthog/posthog-go"
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
	Operations  []appChatOperation `json:"operations"`
	Summary     string             `json:"summary"`
	Suggestions []string           `json:"suggestions,omitempty"`
}

// ansiRegex strips ANSI escape sequences that some LLM providers emit in streamed text.
var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

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
	{
		Type: "function",
		Function: utils.ToolFunction{
			Name:        "setup_ecommerce",
			Description: "Set up e-commerce for this project. Creates a store and configures the project for e-commerce. Call this when the user wants to sell products, create a shop, or add e-commerce functionality.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"store_name": map[string]any{
						"type":        "string",
						"description": "Name for the store, e.g. 'My Shoe Store'",
					},
				},
				"required": []string{"store_name"},
			},
		},
	},
	{
		Type: "function",
		Function: utils.ToolFunction{
			Name:        "manage_store",
			Description: "Create or manage products, categories, variants, and discounts in the e-commerce store. Use this after setup_ecommerce to populate the store with products. Supports batch operations.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"operations": map[string]any{
						"type":        "array",
						"description": "Array of store operations to execute",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"action": map[string]any{
									"type":        "string",
									"enum":        []string{"create_product", "update_product", "delete_product", "create_variant", "create_variant_with_options", "bulk_create_products", "create_category", "update_category", "delete_category", "create_discount", "update_discount", "delete_discount", "update_store_settings", "list_products", "list_categories"},
									"description": "The operation to perform",
								},
								"data": map[string]any{
									"type":        "object",
									"description": "Operation data varies by action. create_product: {name, description, price (cents), images, tags, status}. update_product: {id, name, description, status}. delete_product: {id}. create_variant: {product_id or product_slug, name, sku, price_in_cents, inventory, is_default}. create_variant_with_options: {product_id or product_slug, name, price_in_cents, sku, inventory, options: {size, color, ...}, is_default}. bulk_create_products: {products: [{name, description, price, images, tags}, ...]}. create_category: {name, slug, description}. update_category: {id, name, description, sort_order}. delete_category: {id}. create_discount: {code, type (percentage/fixed), value, min_order_cents, max_uses}. update_discount: {id, code, is_active}. delete_discount: {id}. update_store_settings: {currency, tax_rate, notify_email}. list_products: {}. list_categories: {}.",
								},
							},
							"required": []string{"action", "data"},
						},
					},
				},
				"required": []string{"operations"},
			},
		},
	},
}

// buildChatToolExecutor returns a ToolExecutor that can read/list/search project files
// and perform actions like setting up e-commerce.
func (c *Controller) buildChatToolExecutor(project *models.AppProject, user *models.User, fileMap map[string]string) utils.ToolExecutor {
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

		case "setup_ecommerce":
			var args struct {
				StoreName string `json:"store_name"`
			}
			json.Unmarshal([]byte(arguments), &args)
			if args.StoreName == "" {
				args.StoreName = project.Name + " Store"
			}

			// Inject e-commerce template files into fileMap and DB if not already present
			injectEcommerceTemplates := func() {
				for _, tp := range constants.NEXTJS_ECOMMERCE_TEMPLATE_FILE_PATHS {
					if _, exists := fileMap[tp]; exists {
						continue
					}
					content := getEcommerceTemplateContent(tp)
					if content == "" {
						continue
					}
					file := &models.AppGeneratedFile{
						AppProjectID: project.ID,
						Platform:     "nextjs",
						FilePath:     tp,
						Content:      content,
						FileType:     "ts",
						IsGenerated:  false, // template-managed
					}
					if _, err := c.DB.UpsertAppGeneratedFile(file); err != nil {
						slog.Error("failed to upsert ecommerce template file", "path", tp, "error", err)
					}
					fileMap[tp] = content
				}
			}

			// Check if store already exists
			existingStore, err := c.DB.GetStoreByAppProjectID(project.ID.String())
			if err == nil && existingStore != nil {
				injectEcommerceTemplates()
				return fmt.Sprintf("E-commerce store already exists.\nStore ID: %s\nStore name: %s\nStorefront API: %s/v2/storefront/%s\nUse NEXT_PUBLIC_STORE_ID and NEXT_PUBLIC_WEMOB_API_URL env vars in your code.\nlib/wemob-store.ts is available with typed API client functions — import from \"@/lib/wemob-store\".\nNEXT STEP: Call the manage_store tool to add products, categories, and variants to the store.",
					existingStore.ID.String(), existingStore.StoreName, constants.WEMOB_API_BASE_URL, existingStore.ID.String()), nil
			}

			// Set project type to ecommerce
			if project.ProjectType != "ecommerce" {
				project.ProjectType = "ecommerce"
				c.DB.UpdateAppProject(project)
			}

			// Create store
			store, err := c.CreateStore(project, user, args.StoreName)
			if err != nil {
				return fmt.Sprintf("Failed to create store: %s", err.Error()), nil
			}

			injectEcommerceTemplates()
			return fmt.Sprintf("E-commerce store created successfully!\nStore ID: %s\nStore name: %s\nStorefront API: %s/v2/storefront/%s\nUse these env vars in generated code:\n- NEXT_PUBLIC_STORE_ID = \"%s\"\n- NEXT_PUBLIC_WEMOB_API_URL = \"%s\"\nlib/wemob-store.ts is available with typed API client functions — import from \"@/lib/wemob-store\".\nNEXT STEP: Call the manage_store tool to add products, categories, and variants to the store.",
				store.ID.String(), store.StoreName, constants.WEMOB_API_BASE_URL, store.ID.String(), store.ID.String(), constants.WEMOB_API_BASE_URL), nil

		case "manage_store":
			var args struct {
				Operations []struct {
					Action string         `json:"action"`
					Data   map[string]any `json:"data"`
				} `json:"operations"`
			}
			if err := json.Unmarshal([]byte(arguments), &args); err != nil {
				return fmt.Sprintf("Failed to parse manage_store arguments: %s", err.Error()), nil
			}

			// Get store for this project
			store, err := c.DB.GetStoreByAppProjectID(project.ID.String())
			if err != nil || store == nil {
				return "Error: No e-commerce store found. Call setup_ecommerce first to create a store.", nil
			}

			var results []string
			var errCount int
			for _, op := range args.Operations {
				opData, err := c.executeStoreOperation(store, op.Action, op.Data)
				if err != nil {
					slog.Warn("manage_store operation failed", "action", op.Action, "error", err)
					results = append(results, fmt.Sprintf("FAILED %s: %s", op.Action, err.Error()))
					errCount++
				} else {
					name, _ := op.Data["name"].(string)
					code, _ := op.Data["code"].(string)
					label := name
					if label == "" {
						label = code
					}
					if opData != "" {
						results = append(results, fmt.Sprintf("OK %s: %s\n%s", op.Action, label, opData))
					} else {
						results = append(results, fmt.Sprintf("OK %s: %s", op.Action, label))
					}
				}
			}

			summary := fmt.Sprintf("Executed %d operations (%d succeeded, %d failed):\n%s",
				len(args.Operations), len(args.Operations)-errCount, errCount, strings.Join(results, "\n"))
			return summary, nil

		default:
			return fmt.Sprintf("Unknown tool: %s", name), nil
		}
	}
}

// compositeToolExecutor wraps a local ToolExecutor and routes MCP-prefixed tool calls
// to the MCP service, while passing everything else to the local executor.
func (c *Controller) compositeToolExecutor(local utils.ToolExecutor) utils.ToolExecutor {
	return func(name string, arguments string) (string, error) {
		// Route to MCP if the service recognises this tool name
		if c.MCPService != nil && c.MCPService.IsMCPTool(name) {
			return c.MCPService.ToolExecutor()(name, arguments)
		}
		// Try local executor
		result, err := local(name, arguments)
		if err != nil {
			return result, err
		}
		// If local returns "Unknown tool:" and MCP is available, try MCP as fallback
		if strings.HasPrefix(result, "Unknown tool:") && c.MCPService != nil {
			if mcpResult, mcpErr := c.MCPService.ToolExecutor()(name, arguments); mcpErr == nil && !strings.HasPrefix(mcpResult, "Unknown MCP tool:") {
				return mcpResult, nil
			}
		}
		return result, nil
	}
}

// mcpTools returns MCP tools if the service is available, or nil.
func (c *Controller) mcpTools() []utils.Tool {
	if c.MCPService != nil && c.MCPService.HasTools() {
		return c.MCPService.Tools()
	}
	return nil
}

// StreamChatAppFileOps processes a chat message and applies file operations with SSE streaming.
func (c *Controller) StreamChatAppFileOps(ctx context.Context, project *models.AppProject, user *models.User, message string, images []string, events chan<- responses.SSEEvent) {
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

	provider := c.llmProvider()
	model := c.SecretManager.GetSecret("WEBSITE_BUILDER_LLM_MODEL")
	if model == "" {
		model = constants.DEFAULT_NEXTJS_LLM_MODEL
	}

	// Token usage tracking
	tracker := utils.NewTokenTracker()
	ctx = utils.WithTokenTracker(ctx, tracker)
	defer func() {
		c.RecordTokenUsage(user.ID, "app_chat", model, provider.Name, project.ID.String(), tracker)
		// Token-based credit deduction
		prompt, completion, total, callCount := tracker.Totals()
		if total > 0 {
			credits := models.TokensToCredits(total)
			if err := c.DeductAICreditsUnchecked(user, credits, "app_chat", project.ID.String()); err != nil {
				slog.Warn("failed to deduct AI credits from tokens", "error", err,
					"user_id", user.ID, "credits", credits, "tokens", total)
			}
			c.TrackUserEvent(user, "app_chat_completed", posthog.NewProperties().
				Set("app_id", project.ID.String()).
				Set("total_tokens", total).
				Set("prompt_tokens", prompt).
				Set("completion_tokens", completion).
				Set("credits_used", credits).
				Set("llm_call_count", callCount).
				Set("has_images", len(images) > 0))
		}
	}()

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
	brokenCount := 0
	for _, fp := range filePaths {
		content := currentFiles[fp]
		if strings.Contains(content, "Generation failed") {
			fileTree.WriteString(fmt.Sprintf("- %s (%d chars) [BROKEN: contains placeholder, needs full implementation]\n", fp, len(content)))
			brokenCount++
		} else {
			fileTree.WriteString(fmt.Sprintf("- %s (%d chars)\n", fp, len(content)))
		}
	}
	if brokenCount > 0 {
		fileTree.WriteString(fmt.Sprintf("\nWARNING: %d file(s) marked [BROKEN] contain placeholder content from failed generation. These need to be fully regenerated with proper implementations.\n", brokenCount))
	}
	fileTree.WriteString("\nUse the read_file tool to view any file's content before modifying it.")

	// Build rich integration context for AI
	appIntegrations, _ := c.DB.GetAppIntegrationsByProjectID(project.ID.String())
	var integrationContext strings.Builder
	hasPaymentGateway := false
	if len(appIntegrations) > 0 {
		integrationContext.WriteString("\nConnected integrations:\n")
		for _, integ := range appIntegrations {
			config, err := integrations.GetProviderConfig(integ.Provider)
			if err != nil {
				integrationContext.WriteString(fmt.Sprintf("- %s (status: %s)\n", integ.Provider, integ.Status))
				continue
			}
			integrationContext.WriteString(fmt.Sprintf("- %s (category: %s, status: %s)\n", integ.Provider, config.Category, integ.Status))
			// Show env var names (not values) so AI knows what to reference
			var envNames []string
			for _, envName := range config.EnvVarMapping {
				envNames = append(envNames, envName)
			}
			if len(envNames) > 0 {
				integrationContext.WriteString(fmt.Sprintf("  Env vars: %s\n", strings.Join(envNames, ", ")))
			}
			if config.Category == "payment" {
				hasPaymentGateway = true
			}
		}
	} else {
		integrationContext.WriteString("\nNo integrations connected.\n")
	}

	// E-commerce store context
	if project.ProjectType == "ecommerce" {
		store, err := c.DB.GetStoreByAppProjectID(project.ID.String())
		if err == nil && store != nil {
			integrationContext.WriteString(fmt.Sprintf("\nE-commerce store: %s (store_id: %s)\n", store.Status, store.ID.String()))
			integrationContext.WriteString(fmt.Sprintf("Storefront API: %s/v2/storefront/%s\n", constants.WEMOB_API_BASE_URL, store.ID.String()))
			integrationContext.WriteString(fmt.Sprintf("Currency: %s, Locale: %s, Tax rate: %.2f%%\n", store.Currency, store.Locale, store.TaxRate))

			// Product count
			products, _ := c.DB.GetProductsByStoreID(store.ID.String())
			integrationContext.WriteString(fmt.Sprintf("Products: %d\n", len(products)))

			// Category list
			categories, _ := c.DB.GetCategoriesByStoreID(store.ID.String())
			if len(categories) > 0 {
				var catNames []string
				for _, cat := range categories {
					catNames = append(catNames, cat.Name)
				}
				integrationContext.WriteString(fmt.Sprintf("Categories: %s\n", strings.Join(catNames, ", ")))
			}

			// Payment providers
			var paymentProviders []string
			for _, integ := range appIntegrations {
				if integ.Status == "connected" {
					for _, cfg := range constants.IntegrationProviders {
						if cfg.Provider == integ.Provider && cfg.Category == "payment" {
							paymentProviders = append(paymentProviders, cfg.Name)
						}
					}
				}
			}
			if len(paymentProviders) > 0 {
				integrationContext.WriteString(fmt.Sprintf("Payment providers: %s\n", strings.Join(paymentProviders, ", ")))
			}

			// SDK hint
			integrationContext.WriteString("SDK: lib/wemob-store.ts provides CartProvider, useCart, useProducts, useProduct, useCategories, useStoreConfig hooks. Wrap layout.tsx with <CartProvider>.\n")
		}
		if !hasPaymentGateway {
			integrationContext.WriteString("\nWARNING: No payment gateway connected. Tell the user to connect Stripe or Iyzico via the dashboard to enable checkout.\n")
		}
	}

	// Custom env vars (key names only)
	customEnvVars, _ := c.DB.GetAppEnvVarsByProjectID(project.ID.String())
	if len(customEnvVars) > 0 {
		integrationContext.WriteString("\nCustom environment variables:\n")
		for _, ev := range customEnvVars {
			key := ev.Key
			if ev.IsPublic && !strings.HasPrefix(key, "NEXT_PUBLIC_") {
				key = "NEXT_PUBLIC_" + key
			}
			integrationContext.WriteString(fmt.Sprintf("- %s\n", key))
		}
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

	// Convert base64 images to data URIs for storage and LLM context
	var imageURLs []string
	for _, img := range images {
		if img == "" {
			continue
		}
		// Store as data URI (works for frontend <img> rendering and LLM references)
		if !strings.HasPrefix(img, "data:") {
			img = "data:image/png;base64," + img
		}
		imageURLs = append(imageURLs, img)
	}

	messages = append(messages, models.AppConversationMessage{
		ID:        uuid.New().String(),
		Role:      "user",
		Content:   message,
		Timestamp: time.Now().Unix(),
		ImageURLs: imageURLs,
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
		content := msg.Content
		// Annotate user messages that had image attachments
		if msg.Role == "user" && len(msg.ImageURLs) > 0 {
			content += fmt.Sprintf("\n\n[User attached %d image(s) for visual reference]", len(msg.ImageURLs))
		}
		llmMessages = append(llmMessages, utils.Message{Role: msg.Role, Content: content})
	}

	if ctx.Err() != nil {
		return
	}

	// Create auto-snapshot before making changes
	preSnapshot, _ := c.CreateAppSnapshot(project, "Auto-save before chat edit", "auto")
	var preSnapshotID string
	if preSnapshot != nil {
		preSnapshotID = preSnapshot.ID.String()
	}


	slog.Info("Calling LLM for chat", "model", model, "project_id", project.ID, "message_count", len(llmMessages))

	// Call LLM with tool calling — LLM reads files on demand instead of all files in context
	localExecutor := c.buildChatToolExecutor(project, user, currentFiles)
	executor := c.compositeToolExecutor(localExecutor)
	chatTools := append([]utils.Tool{}, appChatTools...)
	chatTools = append(chatTools, c.mcpTools()...)
	if wsTool := provider.WebSearchTool(); wsTool != nil {
		chatTools = append(chatTools, *wsTool)
	}
	responseJSON, err := utils.StreamWithTools(
		ctx,
		model,
		llmMessages,
		chatTools,
		provider,
		nil, // no json_object format — response is explanation text + fenced JSON
		c.reasoningConfig(),
		executor,
		func(toolName, args string) {
			sendEvent("tool_call", responses.ToolCallEvent{
				Tool:      toolName,
				Arguments: args,
			})
		},
		func() func(chunk string) {
			var buf strings.Builder
			jsonStarted := false
			return func(chunk string) {
				// Strip ANSI escape codes that some LLM providers emit
				chunk = ansiRegex.ReplaceAllString(chunk, "")
				if chunk == "" {
					return
				}
				if jsonStarted {
					return // don't stream the JSON block to the frontend
				}
				buf.WriteString(chunk)
				s := buf.String()
				if strings.Contains(s, "```") {
					// JSON block starting — send only the part before it
					idx := strings.Index(s, "```")
					remaining := s[idx:]
					_ = remaining
					jsonStarted = true
					// Send the pre-fence text that hasn't been sent yet
					preFence := chunk
					if ci := strings.Index(chunk, "```"); ci >= 0 {
						preFence = chunk[:ci]
					}
					if preFence != "" {
						sendEvent("chat_text", responses.ChatTextEvent{Chunk: preFence})
					}
					return
				}
				sendEvent("chat_text", responses.ChatTextEvent{Chunk: chunk})
			}
		}(),
		func(chunk string) {
			sendEvent("chat_thinking", responses.ChatThinkingEvent{Chunk: chunk})
		},
	)
	if err != nil {
		slog.Error("failed to get chat response from LLM", "error", err, "response_so_far", len(responseJSON))
		// Save the error as assistant message so it persists on refresh
		messages = append(messages, models.AppConversationMessage{
			ID:        uuid.New().String(),
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

	// Extract explanation text (everything before the JSON block) and the JSON itself
	explanationText := ""
	jsonPart := responseJSON

	// Try to split on fenced JSON block
	if idx := strings.Index(responseJSON, "```json"); idx >= 0 {
		explanationText = strings.TrimSpace(responseJSON[:idx])
		rest := responseJSON[idx+7:] // skip ```json
		if endIdx := strings.Index(rest, "```"); endIdx >= 0 {
			jsonPart = strings.TrimSpace(rest[:endIdx])
		} else {
			jsonPart = strings.TrimSpace(rest)
		}
	} else if idx := strings.Index(responseJSON, "```"); idx >= 0 {
		explanationText = strings.TrimSpace(responseJSON[:idx])
		rest := responseJSON[idx+3:]
		if endIdx := strings.Index(rest, "```"); endIdx >= 0 {
			jsonPart = strings.TrimSpace(rest[:endIdx])
		} else {
			jsonPart = strings.TrimSpace(rest)
		}
	}

	var chatResp appChatResponse
	if err := json.Unmarshal([]byte(jsonPart), &chatResp); err != nil {
		// Fallback: try direct parse of the full response
		if err2 := json.Unmarshal([]byte(responseJSON), &chatResp); err2 != nil {
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
					"direct_err", err2,
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
			content := utils.StripCodeFences(op.Content)
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
				Content:  content,
			}) {
				return
			}
			filesChanged++

		case "modify":
			existingFile, err := c.DB.GetAppGeneratedFileByPath(project.ID.String(), "nextjs", op.Path)
			if err != nil {
				// File not found — fall back to creating it instead of silently skipping
				slog.Warn("file not found for modify, creating instead", "path", op.Path)
				content := utils.StripCodeFences(op.Content)
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
					Content:  content,
				}) {
					return
				}
				filesChanged++
				continue
			}
			content := utils.StripCodeFences(op.Content)
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
				Content:  content,
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

		// Auto-sync to preview container if running (including env vars)
		if c.PreviewService != nil {
			go c.SyncPreviewAll(project)
		}
	}

	// Emit file_complete events for e-commerce template files injected by setup_ecommerce.
	// These were added to currentFiles and DB by the tool executor but the frontend
	// doesn't know about them unless we send explicit events.
	if project.ProjectType == "ecommerce" {
		for _, tp := range constants.NEXTJS_ECOMMERCE_TEMPLATE_FILE_PATHS {
			content, exists := currentFiles[tp]
			if !exists {
				continue
			}
			// Skip if the LLM already emitted a file operation for this path
			alreadyEmitted := false
			for _, op := range chatResp.Operations {
				if op.Path == tp {
					alreadyEmitted = true
					break
				}
			}
			if alreadyEmitted {
				continue
			}
			dbFile, err := c.DB.GetAppGeneratedFileByPath(project.ID.String(), "nextjs", tp)
			if err == nil && dbFile != nil {
				sendEvent("file_complete", responses.FileCompleteEvent{
					Action:   "create",
					FilePath: tp,
					FileID:   dbFile.ID,
					FileType: dbFile.FileType,
					Content:  content,
				})
				filesChanged++
			}
		}

		// Sync preview even if no LLM file ops, since setup_ecommerce injected templates and env vars changed
		if filesChanged > 0 && c.PreviewService != nil {
			go c.SyncPreviewAll(project)
		}
	}

	// Save assistant response to conversation (include operations so the LLM
	// remembers what files were changed in previous turns)
	// Prefer explanation text (streamed in real-time) over the JSON summary
	assistantContent := chatResp.Summary
	if explanationText != "" {
		assistantContent = explanationText
	}
	// Append file list context so the LLM remembers what changed in history
	if len(chatResp.Operations) > 0 {
		var opLines []string
		for _, op := range chatResp.Operations {
			opLines = append(opLines, fmt.Sprintf("- %s %s", op.Action, op.Path))
		}
		assistantContent += "\n\nFiles changed:\n" + strings.Join(opLines, "\n")
	}

	// Build file operations for the message
	var fileOps []models.AppChatFileOp
	for _, op := range chatResp.Operations {
		fileOps = append(fileOps, models.AppChatFileOp{
			Action:   op.Action,
			FilePath: op.Path,
			FileType: op.Type,
		})
	}

	assistantMsg := models.AppConversationMessage{
		ID:             uuid.New().String(),
		Role:           "assistant",
		Content:        assistantContent,
		Timestamp:      time.Now().Unix(),
		FileOperations: fileOps,
		SnapshotID:     preSnapshotID,
		Suggestions:    chatResp.Suggestions,
	}
	messages = append(messages, assistantMsg)
	conv.Messages, _ = json.Marshal(messages)
	if _, err := c.DB.UpdateAppConversation(conv); err != nil {
		slog.Error("failed to save assistant message to conversation", "error", err, "conv_id", conv.ID)
	}

	// Send explanation text (or summary fallback) as the display content
	displayContent := chatResp.Summary
	if explanationText != "" {
		displayContent = explanationText
	}
	sendEvent("chat_complete", map[string]any{
		"project_id":      project.ID,
		"summary":         displayContent,
		"files_changed":   filesChanged,
		"file_operations": fileOps,
		"snapshot_id":     preSnapshotID,
		"suggestions":     chatResp.Suggestions,
	})
}

// GetAppConversation returns the conversation history for a project
func (c *Controller) GetAppConversation(projectID string) (*models.AppConversation, error) {
	return c.DB.GetAppConversationByProjectID(projectID)
}

// ClearAppConversation deletes the conversation for a project so the user can start fresh
func (c *Controller) ClearAppConversation(projectID string) error {
	return c.DB.DeleteAppConversation(projectID)
}
