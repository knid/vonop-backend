package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
	"github.com/posthog/posthog-go"
)

// platformSchemaCache caches the result of ExtractSchemaFromPlatformDB with a short TTL.
var platformSchemaCache struct {
	sync.Mutex
	data      json.RawMessage
	expiresAt time.Time
}

const platformSchemaCacheTTL = 60 * time.Second

// schemaRelevantPatterns are file path patterns that likely contain database schema info.
var schemaRelevantPatterns = []string{
	"schema", "supabase", "drizzle", "prisma", "types", "model", "db",
	"migration", "database", "table", "entity",
}

// ExtractSchemaFromGeneratedFiles reads generated code files and uses LLM to extract table definitions.
func (c *Controller) ExtractSchemaFromGeneratedFiles(ctx context.Context, projectID string) (json.RawMessage, error) {
	repo, err := c.GitRepoService.Open(projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to load project files: %w", err)
	}
	fileMap, err := repo.ReadAllFiles()
	if err != nil {
		return nil, fmt.Errorf("failed to load project files: %w", err)
	}

	// Filter for schema-relevant files
	var relevantFiles []string
	for path, content := range fileMap {
		lowerPath := strings.ToLower(path)
		isRelevant := false
		for _, pattern := range schemaRelevantPatterns {
			if strings.Contains(lowerPath, pattern) {
				isRelevant = true
				break
			}
		}
		// Also include files under lib/
		if strings.HasPrefix(lowerPath, "lib/") {
			isRelevant = true
		}
		if isRelevant && content != "" {
			relevantFiles = append(relevantFiles, fmt.Sprintf("--- %s ---\n%s", path, content))
		}
	}

	if len(relevantFiles) == 0 {
		return json.RawMessage("[]"), nil
	}

	provider := c.llmProvider()
	model := c.SecretManager.GetSecret("WEBSITE_BUILDER_LLM_MODEL")
	if model == "" {
		model = provider.DefaultModel()
	}

	message := "Extract the database schema from these source code files:\n\n" + strings.Join(relevantFiles, "\n\n")

	result, err := utils.GetJSONFromLLM(ctx, model, constants.DASHBOARD_SCHEMA_EXTRACTION_PROMPT, message, provider)
	if err != nil {
		return nil, fmt.Errorf("schema extraction failed: %w", err)
	}

	// Validate it's valid JSON
	result = utils.ExtractJSON(result)
	var schema json.RawMessage
	if err := json.Unmarshal([]byte(result), &schema); err != nil {
		return json.RawMessage("[]"), nil
	}

	return schema, nil
}

// ExtractSchemaFromPlatformDB introspects the platform's own PostgreSQL database
// and returns table/column definitions from information_schema.
// Results are cached for 60 seconds to avoid repeated heavy information_schema queries.
func (c *Controller) ExtractSchemaFromPlatformDB() (json.RawMessage, error) {
	platformSchemaCache.Lock()
	if platformSchemaCache.data != nil && time.Now().Before(platformSchemaCache.expiresAt) {
		cached := platformSchemaCache.data
		platformSchemaCache.Unlock()
		return cached, nil
	}
	platformSchemaCache.Unlock()

	db := c.DB.DB.DB

	rows, err := db.Query(`
		SELECT t.table_name, c.column_name, c.data_type, c.is_nullable,
			CASE WHEN kcu.column_name IS NOT NULL THEN 'YES' ELSE 'NO' END AS is_primary_key
		FROM information_schema.tables t
		JOIN information_schema.columns c ON c.table_name = t.table_name AND c.table_schema = t.table_schema
		LEFT JOIN information_schema.table_constraints tc
			ON tc.table_name = t.table_name AND tc.table_schema = t.table_schema AND tc.constraint_type = 'PRIMARY KEY'
		LEFT JOIN information_schema.key_column_usage kcu
			ON kcu.constraint_name = tc.constraint_name AND kcu.table_schema = tc.table_schema AND kcu.column_name = c.column_name
		WHERE t.table_schema = 'public' AND t.table_type = 'BASE TABLE'
		ORDER BY t.table_name, c.ordinal_position
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query information_schema: %w", err)
	}
	defer rows.Close()

	type column struct {
		Name         string `json:"name"`
		Type         string `json:"type"`
		Nullable     bool   `json:"nullable"`
		IsPrimaryKey bool   `json:"is_primary_key"`
	}
	type table struct {
		Name    string   `json:"name"`
		Columns []column `json:"columns"`
	}

	tableMap := map[string]*table{}
	var tableOrder []string

	for rows.Next() {
		var tableName, colName, dataType, isNullable, isPK string
		if err := rows.Scan(&tableName, &colName, &dataType, &isNullable, &isPK); err != nil {
			continue
		}
		t, ok := tableMap[tableName]
		if !ok {
			t = &table{Name: tableName}
			tableMap[tableName] = t
			tableOrder = append(tableOrder, tableName)
		}
		t.Columns = append(t.Columns, column{
			Name:         colName,
			Type:         dataType,
			Nullable:     isNullable == "YES",
			IsPrimaryKey: isPK == "YES",
		})
	}

	var tables []table
	for _, name := range tableOrder {
		tables = append(tables, *tableMap[name])
	}

	schema, err := json.Marshal(tables)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal schema: %w", err)
	}

	platformSchemaCache.Lock()
	platformSchemaCache.data = schema
	platformSchemaCache.expiresAt = time.Now().Add(platformSchemaCacheTTL)
	platformSchemaCache.Unlock()

	return schema, nil
}

// CreateDashboard creates a new dashboard for a project with AI-generated panels.
func (c *Controller) CreateDashboard(ctx context.Context, project *models.AppProject, user *models.User, prompt string, events chan<- responses.SSEEvent) {
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
		model = provider.DefaultModel()
	}

	// Token tracking
	tracker := utils.NewTokenTracker()
	tracker.TrackContent = true
	tracker.UserID = user.ID.String()
	tracker.ProjectID = project.ID.String()
	ctx = utils.WithTokenTracker(ctx, tracker)
	defer func() {
		c.RecordTokenUsage(user, "dashboard_create", model, provider.Name, project.ID.String(), project.ProjectType, tracker)
		prompt, completion, total, _ := tracker.Totals()
		if total > 0 {
			credits := models.TokensToCredits(prompt, completion)
			if err := c.DeductAICreditsUnchecked(user, credits, "dashboard_create", project.ID.String()); err != nil {
				slog.Warn("failed to deduct AI credits for dashboard", "error", err, "user_id", user.ID, "credits", credits)
			}
		}
	}()

	// Find database integration (optional — falls back to platform DB)
	dbProvider := "platform"
	integrations, err := c.DB.GetAppIntegrationsByProjectID(project.ID.String())
	if err == nil {
		for _, integ := range integrations {
			if integ.Provider == "supabase" || integ.Provider == "neon" {
				dbProvider = integ.Provider
				break
			}
		}
	}

	sendEvent("status", map[string]string{"phase": "extracting_schema"})

	// Extract schema
	var schema json.RawMessage
	if dbProvider == "platform" {
		schema, err = c.ExtractSchemaFromPlatformDB()
	} else {
		schema, err = c.ExtractSchemaFromGeneratedFiles(ctx, project.ID.String())
	}
	if err != nil {
		slog.Error("schema extraction failed", "error", err, "project_id", project.ID)
		sendError("schema", "failed to extract database schema")
		return
	}

	// Check if schema is empty
	var tables []any
	json.Unmarshal(schema, &tables)
	if len(tables) == 0 {
		sendError("schema", "no database tables found")
		return
	}

	sendEvent("status", map[string]string{"phase": "generating_panels"})

	// Generate panels with LLM
	panelMsg := fmt.Sprintf("Database provider: %s\nApp name: %s\nApp description: %s\n\nDatabase schema:\n%s",
		dbProvider, utils.SanitizePromptContent(project.Name), utils.SanitizePromptContent(project.Prompt), string(schema))
	if prompt != "" {
		panelMsg += fmt.Sprintf("\n\nUser request: %s", utils.SanitizePromptContent(prompt))
	}

	panelResult, err := utils.GetJSONFromLLM(ctx, model, constants.DASHBOARD_GENERATION_PROMPT, panelMsg, provider)
	if err != nil {
		slog.Error("panel generation failed", "error", err, "project_id", project.ID)
		sendError("generation", "failed to generate dashboard panels")
		return
	}

	panelResult = utils.ExtractJSON(panelResult)
	var generated struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Panels      []struct {
			Title     string         `json:"title"`
			PanelType string         `json:"panel_type"`
			Query     json.RawMessage `json:"query"`
			Config    map[string]any `json:"config"`
			SortOrder int            `json:"sort_order"`
			GridWidth int            `json:"grid_width"`
		} `json:"panels"`
	}
	if err := json.Unmarshal([]byte(panelResult), &generated); err != nil {
		slog.Error("failed to parse generated panels", "error", err, "result", panelResult[:min(500, len(panelResult))])
		sendError("generation", "failed to parse generated dashboard")
		return
	}

	// Delete existing dashboard if any
	existing, err := c.DB.GetAppDashboardByProjectID(project.ID.String())
	if err == nil && existing.ID != uuid.Nil {
		c.DB.DeleteAppDashboardPanelsByDashboardID(existing.ID.String())
		c.DB.DeleteAppDashboard(existing)
	}

	// Create dashboard
	name := generated.Name
	if name == "" {
		name = project.Name + " Dashboard"
	}
	dashboard := &models.AppDashboard{
		AppProjectID: project.ID,
		UserID:       user.ID,
		Name:         name,
		Description:  generated.Description,
		Schema:       schema,
		Layout:       []byte("[]"),
		Settings:     map[string]any{},
		Provider:     dbProvider,
		Status:       "ready",
	}
	dashboard, err = c.DB.CreateAppDashboard(dashboard)
	if err != nil {
		slog.Error("failed to create dashboard", "error", err)
		sendError("database", "failed to save dashboard")
		return
	}

	// Create panels
	var panels []models.AppDashboardPanel
	for _, p := range generated.Panels {
		gridWidth := p.GridWidth
		if gridWidth < 1 || gridWidth > 12 {
			gridWidth = 6
		}
		panels = append(panels, models.AppDashboardPanel{
			DashboardID: dashboard.ID,
			Title:       p.Title,
			PanelType:   p.PanelType,
			Query:       p.Query,
			Config:      p.Config,
			SortOrder:   p.SortOrder,
			GridWidth:   gridWidth,
		})
	}

	if err := c.DB.BulkCreateAppDashboardPanels(panels); err != nil {
		slog.Error("failed to create dashboard panels", "error", err)
		sendError("database", "failed to save dashboard panels")
		return
	}

	// Fetch created panels for response
	createdPanels, _ := c.DB.GetAppDashboardPanelsByDashboardID(dashboard.ID.String())

	c.TrackUserEvent(user, "dashboard_created", posthog.NewProperties().
		Set("app_id", project.ID.String()).
		Set("provider", dbProvider).
		Set("panel_count", len(createdPanels)))

	sendEvent("dashboard_complete", buildDashboardResponse(dashboard, createdPanels))
}

// GetDashboard returns the dashboard for a project.
func (c *Controller) GetDashboard(projectID string) (*models.AppDashboard, []models.AppDashboardPanel, error) {
	dashboard, err := c.DB.GetAppDashboardByProjectID(projectID)
	if err != nil {
		return nil, nil, fmt.Errorf("dashboard not found")
	}

	// Auto-upgrade schema with PK info for platform provider (cheap DB introspection)
	if dashboard.Provider == "platform" && len(dashboard.Schema) > 0 {
		c.ensureSchemaPKInfo(dashboard)
	}

	panels, err := c.DB.GetAppDashboardPanelsByDashboardID(dashboard.ID.String())
	if err != nil {
		return dashboard, nil, nil
	}

	return dashboard, panels, nil
}

// ensureSchemaPKInfo checks if the cached schema has PK info and re-extracts if missing.
func (c *Controller) ensureSchemaPKInfo(dashboard *models.AppDashboard) {
	var tables []struct {
		Columns []struct {
			IsPrimaryKey *bool `json:"is_primary_key"`
		} `json:"columns"`
	}
	if err := json.Unmarshal(dashboard.Schema, &tables); err != nil {
		return
	}
	// Check if any column has is_primary_key field
	for _, t := range tables {
		for _, col := range t.Columns {
			if col.IsPrimaryKey != nil {
				return // already has PK info
			}
		}
	}
	// Re-extract schema with PK info
	schema, err := c.ExtractSchemaFromPlatformDB()
	if err != nil {
		return
	}
	dashboard.Schema = schema
	dashboard.UpdatedAt = time.Now()
	c.DB.UpdateAppDashboard(dashboard)
}

// UpdateDashboard updates the dashboard metadata.
func (c *Controller) UpdateDashboard(projectID string, name, description string, settings map[string]any) (*models.AppDashboard, error) {
	dashboard, err := c.DB.GetAppDashboardByProjectID(projectID)
	if err != nil {
		return nil, fmt.Errorf("dashboard not found")
	}

	if name != "" {
		dashboard.Name = name
	}
	if description != "" {
		dashboard.Description = description
	}
	if settings != nil {
		dashboard.Settings = settings
	}
	dashboard.UpdatedAt = time.Now()

	return c.DB.UpdateAppDashboard(dashboard)
}

// DeleteDashboard removes the dashboard and all its panels.
func (c *Controller) DeleteDashboard(projectID string) error {
	dashboard, err := c.DB.GetAppDashboardByProjectID(projectID)
	if err != nil {
		return fmt.Errorf("dashboard not found")
	}

	c.DB.DeleteAppDashboardPanelsByDashboardID(dashboard.ID.String())
	return c.DB.DeleteAppDashboard(dashboard)
}

// UpdateDashboardPanel updates a single panel.
func (c *Controller) UpdateDashboardPanel(panelID string, dashboardID uuid.UUID, title, panelType string, config map[string]any, sortOrder *int, gridWidth *int) (*models.AppDashboardPanel, error) {
	panel, err := c.DB.GetAppDashboardPanelByID(panelID)
	if err != nil {
		return nil, fmt.Errorf("panel not found")
	}
	if panel.DashboardID != dashboardID {
		return nil, fmt.Errorf("panel does not belong to this dashboard")
	}

	if title != "" {
		panel.Title = title
	}
	if panelType != "" {
		panel.PanelType = panelType
	}
	if config != nil {
		panel.Config = config
	}
	if sortOrder != nil {
		panel.SortOrder = *sortOrder
	}
	if gridWidth != nil && *gridWidth >= 1 && *gridWidth <= 12 {
		panel.GridWidth = *gridWidth
	}
	panel.UpdatedAt = time.Now()

	return c.DB.UpdateAppDashboardPanel(panel)
}

// DeleteDashboardPanel removes a single panel.
func (c *Controller) DeleteDashboardPanel(panelID string, dashboardID uuid.UUID) error {
	panel, err := c.DB.GetAppDashboardPanelByID(panelID)
	if err != nil {
		return fmt.Errorf("panel not found")
	}
	if panel.DashboardID != dashboardID {
		return fmt.Errorf("panel does not belong to this dashboard")
	}
	return c.DB.DeleteAppDashboardPanel(panel)
}

// StreamDashboardChat handles chat-based dashboard refinement with tool calling.
func (c *Controller) StreamDashboardChat(ctx context.Context, dashboard *models.AppDashboard, user *models.User, message string, events chan<- responses.SSEEvent) {
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
		model = provider.DefaultModel()
	}

	// Token tracking
	tracker := utils.NewTokenTracker()
	tracker.TrackContent = true
	tracker.UserID = user.ID.String()
	tracker.ProjectID = dashboard.AppProjectID.String()
	ctx = utils.WithTokenTracker(ctx, tracker)
	defer func() {
		c.RecordTokenUsage(user, "dashboard_chat", model, provider.Name, dashboard.AppProjectID.String(), "dashboard", tracker)
		prompt, completion, total, _ := tracker.Totals()
		if total > 0 {
			credits := models.TokensToCredits(prompt, completion)
			if err := c.DeductAICreditsUnchecked(user, credits, "dashboard_chat", dashboard.AppProjectID.String()); err != nil {
				slog.Warn("failed to deduct AI credits for dashboard chat", "error", err)
			}
		}
	}()

	// Load current panels
	panels, _ := c.DB.GetAppDashboardPanelsByDashboardID(dashboard.ID.String())

	// Build context for LLM
	var panelsList strings.Builder
	for _, p := range panels {
		panelsList.WriteString(fmt.Sprintf("- [%s] %s (type: %s, width: %d)\n", p.ID.String(), p.Title, p.PanelType, p.GridWidth))
	}

	// Dashboard chat tools
	dashboardTools := []utils.Tool{
		{
			Type: "function",
			Function: utils.ToolFunction{
				Name:        "list_panels",
				Description: "List all current dashboard panels with their configurations",
				Parameters: map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
			},
		},
		{
			Type: "function",
			Function: utils.ToolFunction{
				Name:        "add_panel",
				Description: "Add a new panel to the dashboard",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"title":      map[string]any{"type": "string", "description": "Panel title"},
						"panel_type": map[string]any{"type": "string", "enum": []string{"stat_card", "bar_chart", "line_chart", "pie_chart", "table", "area_chart"}},
						"query":      map[string]any{"type": "object", "description": "Query configuration with table/select/filters/sql fields"},
						"config":     map[string]any{"type": "object", "description": "Visualization config (label, color, x_axis, y_axis, etc.)"},
						"grid_width": map[string]any{"type": "integer", "description": "Grid width 1-12", "minimum": 1, "maximum": 12},
					},
					"required": []string{"title", "panel_type", "query"},
				},
			},
		},
		{
			Type: "function",
			Function: utils.ToolFunction{
				Name:        "update_panel",
				Description: "Update an existing panel's configuration",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"panel_id":   map[string]any{"type": "string", "description": "Panel ID to update"},
						"title":      map[string]any{"type": "string", "description": "New title"},
						"panel_type": map[string]any{"type": "string", "enum": []string{"stat_card", "bar_chart", "line_chart", "pie_chart", "table", "area_chart"}},
						"query":      map[string]any{"type": "object", "description": "New query configuration"},
						"config":     map[string]any{"type": "object", "description": "New visualization config"},
						"grid_width": map[string]any{"type": "integer", "description": "New grid width 1-12"},
					},
					"required": []string{"panel_id"},
				},
			},
		},
		{
			Type: "function",
			Function: utils.ToolFunction{
				Name:        "remove_panel",
				Description: "Remove a panel from the dashboard",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"panel_id": map[string]any{"type": "string", "description": "Panel ID to remove"},
					},
					"required": []string{"panel_id"},
				},
			},
		},
	}

	// Build tool executor
	executor := func(name string, arguments string) (string, error) {
		switch name {
		case "list_panels":
			currentPanels, _ := c.DB.GetAppDashboardPanelsByDashboardID(dashboard.ID.String())
			var lines []string
			for _, p := range currentPanels {
				queryStr := string(p.Query)
				lines = append(lines, fmt.Sprintf("ID: %s\nTitle: %s\nType: %s\nWidth: %d\nQuery: %s\n", p.ID, p.Title, p.PanelType, p.GridWidth, queryStr))
			}
			if len(lines) == 0 {
				return "No panels configured.", nil
			}
			return strings.Join(lines, "\n---\n"), nil

		case "add_panel":
			var args struct {
				Title     string          `json:"title"`
				PanelType string          `json:"panel_type"`
				Query     json.RawMessage `json:"query"`
				Config    map[string]any  `json:"config"`
				GridWidth int             `json:"grid_width"`
			}
			json.Unmarshal([]byte(arguments), &args)
			if args.GridWidth < 1 || args.GridWidth > 12 {
				args.GridWidth = 6
			}
			currentPanels, _ := c.DB.GetAppDashboardPanelsByDashboardID(dashboard.ID.String())
			newPanel := &models.AppDashboardPanel{
				DashboardID: dashboard.ID,
				Title:       args.Title,
				PanelType:   args.PanelType,
				Query:       args.Query,
				Config:      args.Config,
				SortOrder:   len(currentPanels),
				GridWidth:   args.GridWidth,
			}
			created, err := c.DB.CreateAppDashboardPanel(newPanel)
			if err != nil {
				return fmt.Sprintf("Failed to add panel: %s", err.Error()), nil
			}
			return fmt.Sprintf("Panel added: %s (ID: %s)", created.Title, created.ID), nil

		case "update_panel":
			var args struct {
				PanelID   string          `json:"panel_id"`
				Title     string          `json:"title"`
				PanelType string          `json:"panel_type"`
				Query     json.RawMessage `json:"query"`
				Config    map[string]any  `json:"config"`
				GridWidth int             `json:"grid_width"`
			}
			json.Unmarshal([]byte(arguments), &args)
			panel, err := c.DB.GetAppDashboardPanelByID(args.PanelID)
			if err != nil {
				return fmt.Sprintf("Panel not found: %s", args.PanelID), nil
			}
			if panel.DashboardID != dashboard.ID {
				return "Panel does not belong to this dashboard", nil
			}
			if args.Title != "" {
				panel.Title = args.Title
			}
			if args.PanelType != "" {
				panel.PanelType = args.PanelType
			}
			if args.Query != nil {
				panel.Query = args.Query
			}
			if args.Config != nil {
				panel.Config = args.Config
			}
			if args.GridWidth >= 1 && args.GridWidth <= 12 {
				panel.GridWidth = args.GridWidth
			}
			panel.UpdatedAt = time.Now()
			c.DB.UpdateAppDashboardPanel(panel)
			return fmt.Sprintf("Panel updated: %s", panel.Title), nil

		case "remove_panel":
			var args struct {
				PanelID string `json:"panel_id"`
			}
			json.Unmarshal([]byte(arguments), &args)
			panel, err := c.DB.GetAppDashboardPanelByID(args.PanelID)
			if err != nil {
				return fmt.Sprintf("Panel not found: %s", args.PanelID), nil
			}
			if panel.DashboardID != dashboard.ID {
				return "Panel does not belong to this dashboard", nil
			}
			c.DB.DeleteAppDashboardPanel(panel)
			return fmt.Sprintf("Panel removed: %s", panel.Title), nil

		default:
			return fmt.Sprintf("Unknown tool: %s", name), nil
		}
	}

	// Build LLM messages
	llmMessages := []utils.Message{
		{Role: "system", Content: constants.DASHBOARD_CHAT_PROMPT},
		{Role: "user", Content: fmt.Sprintf("Dashboard: %s\nProvider: %s\nSchema: %s\n\nCurrent panels:\n%s",
			dashboard.Name, dashboard.Provider, string(dashboard.Schema), panelsList.String())},
		{Role: "assistant", Content: "I understand the dashboard configuration. How would you like to modify it?"},
		{Role: "user", Content: message},
	}

	responseText, err := utils.StreamWithTools(
		ctx, model, llmMessages, dashboardTools, provider,
		nil, c.reasoningConfig(), executor,
		func(toolName, args string) {
			sendEvent("tool_call", responses.ToolCallEvent{Tool: toolName, Arguments: args})
		},
		func(chunk string) {
			sendEvent("chat_text", responses.ChatTextEvent{Chunk: chunk})
		},
		func(chunk string) {
			sendEvent("chat_thinking", responses.ChatThinkingEvent{Chunk: chunk})
		},
	)
	if err != nil {
		slog.Error("dashboard chat LLM error", "error", err)
		sendError("ai", "failed to process chat message")
		return
	}

	// Reload panels after modifications
	updatedPanels, _ := c.DB.GetAppDashboardPanelsByDashboardID(dashboard.ID.String())

	sendEvent("chat_complete", map[string]any{
		"summary": responseText,
		"panels":  buildPanelResponses(updatedPanels),
	})
}

// RefreshDashboardSchema re-extracts the schema from generated files and updates the dashboard.
func (c *Controller) RefreshDashboardSchema(ctx context.Context, projectID string, user *models.User) (json.RawMessage, error) {
	provider := c.llmProvider()
	model := c.SecretManager.GetSecret("WEBSITE_BUILDER_LLM_MODEL")
	if model == "" {
		model = provider.DefaultModel()
	}

	tracker := utils.NewTokenTracker()
	tracker.TrackContent = true
	tracker.UserID = user.ID.String()
	tracker.ProjectID = projectID
	ctx = utils.WithTokenTracker(ctx, tracker)
	defer func() {
		c.RecordTokenUsage(user, "dashboard_schema_refresh", model, provider.Name, projectID, "dashboard", tracker)
		prompt, completion, total, _ := tracker.Totals()
		if total > 0 {
			credits := models.TokensToCredits(prompt, completion)
			if err := c.DeductAICreditsUnchecked(user, credits, "dashboard_schema_refresh", projectID); err != nil {
				slog.Warn("failed to deduct AI credits for schema refresh", "error", err, "user_id", user.ID, "credits", credits)
			}
		}
	}()

	// Check if dashboard uses platform provider
	dashboard, dashErr := c.DB.GetAppDashboardByProjectID(projectID)

	var schema json.RawMessage
	var schemaErr error
	if dashErr == nil && dashboard.Provider == "platform" {
		schema, schemaErr = c.ExtractSchemaFromPlatformDB()
	} else {
		schema, schemaErr = c.ExtractSchemaFromGeneratedFiles(ctx, projectID)
	}
	if schemaErr != nil {
		return nil, schemaErr
	}

	// Update dashboard schema if exists
	if dashErr == nil {
		dashboard.Schema = schema
		dashboard.UpdatedAt = time.Now()
		c.DB.UpdateAppDashboard(dashboard)
	}

	return schema, nil
}

// buildDashboardResponse constructs the response for a dashboard.
func buildDashboardResponse(d *models.AppDashboard, panels []models.AppDashboardPanel) responses.DashboardResponse {
	resp := responses.DashboardResponse{
		ID:          d.ID,
		Name:        d.Name,
		Description: d.Description,
		Provider:    d.Provider,
		Status:      d.Status,
		Settings:    d.Settings,
		CreatedAt:   d.CreatedAt.Unix(),
		UpdatedAt:   d.UpdatedAt.Unix(),
	}
	if len(d.Schema) > 0 {
		var schema any
		json.Unmarshal(d.Schema, &schema)
		resp.Schema = schema
	}
	resp.Panels = buildPanelResponses(panels)
	return resp
}

func buildPanelResponses(panels []models.AppDashboardPanel) []responses.DashboardPanelResponse {
	var result []responses.DashboardPanelResponse
	for _, p := range panels {
		var query any
		json.Unmarshal(p.Query, &query)
		result = append(result, responses.DashboardPanelResponse{
			ID:        p.ID,
			Title:     p.Title,
			PanelType: p.PanelType,
			Query:     query,
			Config:    p.Config,
			SortOrder: p.SortOrder,
			GridWidth: p.GridWidth,
		})
	}
	return result
}

