package controllers

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/services/fal"
	"github.com/knid/wemob/internal/services/gitrepo"
	"github.com/knid/wemob/internal/templates"
	"github.com/knid/wemob/internal/utils"
	"github.com/knid/wemob/internal/utils/external"
)

// Pre-compiled regexes for SQL keyword detection in executeChatExecuteSQL.
var sqlBlockedKeywordRegexes = func() map[string]*regexp.Regexp {
	keywords := []string{"DELETE", "DROP", "ALTER", "CREATE", "TRUNCATE", "GRANT", "REVOKE"}
	m := make(map[string]*regexp.Regexp, len(keywords))
	for _, kw := range keywords {
		m[kw] = regexp.MustCompile(`\b` + kw + `\b`)
	}
	return m
}()


// buildUnifiedChatTools returns the tools list and executor for the unified
// single-call chat path. Same tools as Phase 1, but used in a single
// StreamWithTools call that also handles code generation.
func (c *Controller) buildUnifiedChatTools(
	ctx context.Context,
	project *models.AppProject,
	user *models.User,
	provider *utils.LLMProvider,
	repo *gitrepo.Repo,
	fileMap map[string]string,
	sendEvent func(string, any) bool,
	userLimits *models.StandartUserLimits,
) ([]utils.Tool, utils.ToolExecutor, *[]string) {
	tools := []utils.Tool{
		chatPreviewWebsiteTool,
		chatManageEnvVarsTool,
		chatInstallPackageTool,
		chatGenerateImageTool,
		chatTakeScreenshotTool,
		chatSearchComponentsTool,
		chatManageAssetsTool,
		chatReadFileTool,
		chatListFilesTool,
		chatDiffFilesTool,
		chatGitLogTool,
	}
	if project.ProjectType == "ecommerce" {
		tools = append(tools, chatManageStoreTool)
	}
	if c.neonConnString(project.ID.String()) != "" {
		tools = append(tools, chatSeedDatabaseTool, chatExecuteSQLTool, chatRunMigrationsTool)
	}
	if project.ProjectType == "mobile_app" {
		tools = append(tools, chatConfigureMobileAppTool)
	}
	if ws := provider.WebSearchTool(); ws != nil {
		tools = append(tools, *ws)
	}

	var collectedContext strings.Builder
	var screenshots []string

	executor := func(name string, arguments string) (string, error) {
		if tracker := utils.TokenTrackerFromContext(ctx); tracker != nil {
			tracker.RecordToolUsed(name)
		}
		switch name {
		case "preview_website":
			return c.executeChatPreviewWebsite(arguments, project, sendEvent, &collectedContext, &screenshots)
		case "manage_store":
			return c.executeChatManageStore(arguments, project, sendEvent, &collectedContext)
		case "seed_database":
			return c.executeChatSeedDatabase(ctx, project, sendEvent, &collectedContext)
		case "execute_sql":
			return c.executeChatExecuteSQL(ctx, arguments, project, &collectedContext)
		case "run_migrations":
			return c.executeChatRunMigrations(ctx, arguments, project, repo, fileMap, sendEvent, &collectedContext)
		case "manage_env_vars":
			return c.executeChatManageEnvVars(arguments, project, repo, fileMap, sendEvent, &collectedContext)
		case "install_package":
			return c.executeChatInstallPackage(arguments, project, repo, fileMap, sendEvent, &collectedContext)
		case "generate_image":
			return c.executeChatGenerateImage(ctx, arguments, project, user, sendEvent, &collectedContext)
		case "take_screenshot":
			return c.executeChatTakeScreenshot(arguments, &collectedContext, &screenshots)
		case "search_components":
			return c.executeChatSearchComponents(arguments, project, repo, fileMap, sendEvent, &collectedContext)
		case "configure_mobile_app":
			return c.executeChatConfigureMobileApp(arguments, project, sendEvent, &collectedContext)
		case "read_file":
			var args struct {
				FilePath string `json:"file_path"`
			}
			if err := json.Unmarshal([]byte(arguments), &args); err != nil {
				return fmt.Sprintf("Error: invalid arguments: %s", err.Error()), nil
			}
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
		case "diff_files":
			var args struct {
				FromCommit string `json:"from_commit"`
				ToCommit   string `json:"to_commit"`
			}
			if err := json.Unmarshal([]byte(arguments), &args); err != nil {
				return "Invalid arguments", nil
			}
			if args.FromCommit == "" {
				diff, err := repo.DiffLastCommit()
				if err != nil {
					return fmt.Sprintf("Error: %s", err.Error()), nil
				}
				if diff == "" {
					return "No changes in the last commit.", nil
				}
				return diff, nil
			}
			diff, err := repo.Diff(args.FromCommit, args.ToCommit)
			if err != nil {
				return fmt.Sprintf("Error: %s", err.Error()), nil
			}
			if diff == "" {
				return "No differences found.", nil
			}
			return diff, nil
		case "git_log":
			var args struct {
				Limit int `json:"limit"`
			}
			if err := json.Unmarshal([]byte(arguments), &args); err != nil {
				args.Limit = 10
			}
			if args.Limit <= 0 {
				args.Limit = 10
			}
			if args.Limit > 50 {
				args.Limit = 50
			}
			commits, err := repo.Log(args.Limit)
			if err != nil {
				return fmt.Sprintf("Error: %s", err.Error()), nil
			}
			if len(commits) == 0 {
				return "No commits found.", nil
			}
			var lines []string
			for _, cm := range commits {
				lines = append(lines, fmt.Sprintf("%s  %s  %s", cm.Hash[:8], cm.Timestamp.Format("2006-01-02 15:04"), cm.Message))
			}
			return strings.Join(lines, "\n"), nil
		case "manage_assets":
			return c.executeChatManageAssets(arguments, project, sendEvent, &collectedContext)
		default:
			return fmt.Sprintf("Unknown tool: %s", name), nil
		}
	}

	return tools, executor, &screenshots
}

// preGenContext holds the results of the pre-generation web search + screenshot phase.

type preGenContext struct {
	Text       string   // Text context (web search results, screenshot metadata)
	Screenshots []string // Base64 data URIs of captured screenshots (for vision)
}

func (p preGenContext) HasContent() bool {
	return p.Text != "" || len(p.Screenshots) > 0
}

func (c *Controller) runPreGenWebSearch(
	ctx context.Context,
	provider *utils.LLMProvider,
	userMessage string,
	imageURLs []string,
	sendEvent func(string, any) bool,
) preGenContext {
	fastModel := provider.FastModel()
	tools := []utils.Tool{chatTakeScreenshotTool}
	if ws := provider.WebSearchTool(); ws != nil {
		tools = append(tools, *ws)
	}

	// If no web search and no screenshot API key, skip entirely
	screenshotAvailable := c.SecretManager.GetSecret("SITESHOT_API_KEY") != ""
	if len(tools) == 1 && !screenshotAvailable {
		return preGenContext{}
	}

	systemPrompt := `You are a research assistant that decides whether web search or screenshots are needed before building a website/app. You are NOT a builder — do NOT generate any code, HTML, CSS, or application content.

Your ONLY job: decide if tools are needed, call them if yes, or respond NO_SEARCH_NEEDED if no.

Call web_search and/or take_screenshot ONLY when the user:
- References a SPECIFIC existing website or domain name (e.g. "shiftdelete.net", "airbnb clone")
- Mentions specific technologies, APIs, or services that need current documentation
- Asks for something that benefits from up-to-date information (pricing, trends, latest versions)
- Wants to clone, replicate, or be inspired by a SPECIFIC real website or product

IMPORTANT: When a user references a real website, ALWAYS call take_screenshot with the website URL. Web search results alone cannot reliably tell you the exact colors, layout, or visual style of a website.

Respond with ONLY "NO_SEARCH_NEEDED" (nothing else) when:
- The user describes a generic app/website type (e.g. "dashboard", "blog", "portfolio", "landing page")
- The prompt is self-contained and doesn't reference any real website, brand, or domain
- The user just describes features they want without referencing external sites

CRITICAL RULES:
- NEVER generate code, HTML, CSS, JavaScript, or any application content
- NEVER start building the requested app — that is handled by a different system
- Your response must be either tool calls OR the exact text "NO_SEARCH_NEEDED"
- Keep any text response under 50 words`

	userMsg := utils.Message{Role: "user", Content: userMessage}
	if len(imageURLs) > 0 && provider.SupportsVision() {
		userMsg.Images = imageURLs
		userMsg.Content += "\n\n[User attached image(s) — if the image shows a website or UI design, take a screenshot is NOT needed since you already have the visual reference.]"
	}

	messages := []utils.Message{
		{Role: "system", Content: systemPrompt},
		userMsg,
	}

	var collectedContext strings.Builder
	var screenshots []string

	executor := func(name string, arguments string) (string, error) {
		switch name {
		case "take_screenshot":
			result, err := c.executePreGenScreenshot(arguments, &collectedContext, &screenshots)
			return result, err
		default:
			return "", fmt.Errorf("unexpected client tool call: %s", name)
		}
	}

	result, err := utils.StreamWithTools(
		ctx, fastModel, messages,
		tools, provider,
		nil, nil, // no response format, no reasoning
		executor,
		nil, // onToolCall
		nil, // onChunk — discard fast model text
		nil, // onReasoning
		utils.ToolCallConfig{
			MaxRounds: 3,
			MaxTokens: 512,
			OnToolCallStart: func(toolName string) {
				sendEvent("tool_start", map[string]string{"tool": toolName})
			},
			OnToolResult: makeToolResultEmitter(sendEvent),
		},
	)

	if err != nil {
		slog.Warn("Pre-gen web search phase failed, continuing without context", "error", err)
		if collectedContext.Len() > 0 {
			return preGenContext{Text: collectedContext.String(), Screenshots: screenshots}
		}
		return preGenContext{}
	}

	// Combine collected context (from take_screenshot) with web search text
	trimmed := strings.TrimSpace(result)
	if collectedContext.Len() == 0 {
		if trimmed == "" || trimmed == "NO_SEARCH_NEEDED" {
			return preGenContext{}
		}
		return preGenContext{Text: "Web search context:\n" + trimmed, Screenshots: screenshots}
	}

	// We have screenshot context — append web search text if any
	if trimmed != "" && trimmed != "NO_SEARCH_NEEDED" {
		collectedContext.WriteString("\nWeb search context:\n" + trimmed)
	}
	return preGenContext{Text: collectedContext.String(), Screenshots: screenshots}
}

// executePreGenScreenshot captures a screenshot and stores the base64 data URI
// both in collectedContext (text) and screenshots slice (for vision).

func (c *Controller) executePreGenScreenshot(
	arguments string,
	collectedContext *strings.Builder,
	screenshots *[]string,
) (string, error) {
	var args struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return fmt.Sprintf("Error: invalid arguments: %s", err.Error()), nil
	}

	targetURL := strings.TrimSpace(args.URL)
	if targetURL == "" {
		return "Error: url is required", nil
	}
	if !strings.HasPrefix(targetURL, "http://") && !strings.HasPrefix(targetURL, "https://") {
		targetURL = "https://" + targetURL
	}

	apiKey := c.SecretManager.GetSecret("SITESHOT_API_KEY")
	if apiKey == "" {
		return "Error: screenshot service not configured", nil
	}

	imgData, err := external.GetDesktopScreenshotOfURL(targetURL, apiKey)
	if err != nil {
		return fmt.Sprintf("Screenshot capture failed: %s", err.Error()), nil
	}
	if len(imgData) == 0 {
		return "Screenshot capture returned empty image", nil
	}

	b64 := base64.StdEncoding.EncodeToString(imgData)
	dataURI := "data:image/png;base64," + b64

	// Store base64 data URI for vision-capable models
	*screenshots = append(*screenshots, dataURI)

	// Store text metadata (without the huge base64 blob) for text-only context
	collectedContext.WriteString(fmt.Sprintf("\n[Screenshot Captured]\nURL: %s\nA desktop screenshot (1280x800) has been captured and will be provided as an image to the code generation model for accurate color/layout matching.\n", targetURL))
	slog.Info("take_screenshot tool called", "url", targetURL, "size_bytes", len(imgData))
	return "Screenshot captured successfully. The image will be provided to the code generation model.", nil
}

// executeChatPreviewWebsite handles the preview_website tool call in the chat tool phase.
// Instead of loading the URL in an iframe, it fetches the HTML structure and takes
// a screenshot so the code-gen model can build a proper app based on the website.

func (c *Controller) executeChatPreviewWebsite(
	arguments string,
	project *models.AppProject,
	sendEvent func(string, any) bool,
	collectedContext *strings.Builder,
	screenshots *[]string,
) (string, error) {
	var args struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "Error: invalid arguments", nil
	}
	targetURL := strings.TrimSpace(args.URL)
	if targetURL == "" {
		return "Error: url is required", nil
	}
	if !strings.HasPrefix(targetURL, "http://") && !strings.HasPrefix(targetURL, "https://") {
		targetURL = "https://" + targetURL
	}

	sendEvent("tool_call", responses.ToolCallEvent{
		Tool:      "preview_website",
		Arguments: arguments,
	})

	// Fetch HTML to extract structure for the code-gen model
	html, fetchErr := utils.FetchHTML(
		targetURL,
		c.SecretManager.GetSecret("PROXY_HOST"),
		c.SecretManager.GetSecret("PROXY_AUTH"),
	)

	var siteInfo string
	if fetchErr != nil {
		slog.Warn("Failed to fetch HTML for preview_website", "url", targetURL, "error", fetchErr)
		siteInfo = fmt.Sprintf("Website URL: %s\n(Could not fetch page content — generate based on the URL and common patterns for this type of site.)", targetURL)
	} else {
		title := extractHTMLTitle(html)
		description := extractHTMLMeta(html, "description")
		navURLs := utils.ExtractAllURLs(html, targetURL)
		if len(navURLs) > 20 {
			navURLs = navURLs[:20]
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Website URL: %s\n", targetURL))
		if title != "" {
			sb.WriteString(fmt.Sprintf("Title: %s\n", title))
		}
		if description != "" {
			sb.WriteString(fmt.Sprintf("Description: %s\n", description))
		}
		if len(navURLs) > 0 {
			sb.WriteString(fmt.Sprintf("Navigation links (%d): %s\n", len(navURLs), strings.Join(navURLs, ", ")))
		}
		siteInfo = sb.String()
	}

	// Take a screenshot for visual reference instead of loading in iframe
	apiKey := c.SecretManager.GetSecret("SITESHOT_API_KEY")
	if apiKey != "" {
		imgData, err := external.GetDesktopScreenshotOfURL(targetURL, apiKey)
		if err != nil {
			slog.Warn("Failed to take screenshot for preview_website", "url", targetURL, "error", err)
		} else if len(imgData) > 0 {
			b64 := base64.StdEncoding.EncodeToString(imgData)
			*screenshots = append(*screenshots, "data:image/png;base64,"+b64)
			siteInfo += "A desktop screenshot has been captured and will be provided as an image to the code generation model.\n"
		}
	}

	collectedContext.WriteString(fmt.Sprintf("\n[Website Analysis]\n%s\nGenerate or modify the app to match this website's design and content.\n", siteInfo))
	slog.Info("preview_website tool called via fast model", "url", targetURL, "project_id", project.ID)
	return "Website analyzed and screenshot captured. The code generation model will use this to build the app.", nil
}


// executeChatManageStore handles the manage_store tool call in the chat tool phase.
func (c *Controller) executeChatManageStore(
	arguments string,
	project *models.AppProject,
	sendEvent func(string, any) bool,
	collectedContext *strings.Builder,
) (string, error) {
	var args struct {
		Operations []struct {
			Action string         `json:"action"`
			Data   map[string]any `json:"data"`
		} `json:"operations"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return fmt.Sprintf("Failed to parse arguments: %s", err.Error()), nil
	}

	store, err := c.DB.GetStoreByAppProjectID(project.ID.String())
	if err != nil || store == nil {
		return "Error: No store found for this project. The project must be set up as an e-commerce app first.", nil
	}

	sendEvent("tool_call", responses.ToolCallEvent{
		Tool:      "manage_store",
		Arguments: arguments,
	})

	var results []string
	var errCount int
	for _, op := range args.Operations {
		opData, opErr := c.executeStoreOperation(store, op.Action, op.Data)
		if opErr != nil {
			results = append(results, fmt.Sprintf("FAILED %s: %s", op.Action, opErr.Error()))
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

	collectedContext.WriteString(fmt.Sprintf("\n[Store Update]\n%s\n", summary))
	slog.Info("manage_store tool called via fast model",
		"project_id", project.ID,
		"operations", len(args.Operations),
		"succeeded", len(args.Operations)-errCount,
		"failed", errCount,
	)
	return summary, nil
}


// executeChatSeedDatabase handles the seed_database tool call in the chat tool phase.
func (c *Controller) executeChatSeedDatabase(
	ctx context.Context,
	project *models.AppProject,
	sendEvent func(string, any) bool,
	collectedContext *strings.Builder,
) (string, error) {
	dataModels, err := models.ParseDataModels(project.DataModels)
	if err != nil {
		return fmt.Sprintf("Error: failed to parse data models: %s", err.Error()), nil
	}
	if len(dataModels) == 0 {
		return "Error: No database schema found. The database must be set up first.", nil
	}

	sendEvent("tool_call", responses.ToolCallEvent{
		Tool:      "seed_database",
		Arguments: "{}",
	})

	_, seedErr := c.seedNeonDatabase(ctx, project, dataModels, sendEvent)
	if seedErr != nil {
		slog.Error("seed_database tool failed", "error", seedErr, "project_id", project.ID)
		collectedContext.WriteString(fmt.Sprintf("\n[Database Seed Failed]\nSeeding failed with error: %s\nDo NOT create seed scripts, seed API routes, seed buttons, or migration routes as a workaround. Simply inform the user that seeding failed and suggest they try again.\n", seedErr.Error()))
		return fmt.Sprintf("Database seeding failed: %s", seedErr.Error()), nil
	}

	// Generate AI images for seed data image columns
	if project.ProjectType != "ecommerce" {
		c.generateAndStoreSeedImages(ctx, project, dataModels, sendEvent, 10)
	}

	collectedContext.WriteString("\n[Database Seeded]\nThe database has been populated with realistic demo data and AI-generated images. The app should now display this data when querying the database. Do NOT create seed scripts, seed buttons, or seed API routes — the data is already in the database.\n")
	slog.Info("seed_database tool called via fast model", "project_id", project.ID)
	return "Database seeded successfully with realistic demo data.", nil
}

// sanitizeDBError strips sensitive connection/auth details from database errors
// while preserving useful semantic errors (syntax, missing column/table) that
// help the LLM self-correct its queries.
func sanitizeDBError(projectID string, err error) string {
	msg := err.Error()
	lower := strings.ToLower(msg)
	for _, sensitive := range []string{"dial tcp", "connect:", "tls:", "x509:", "password", "authentication", "ssl"} {
		if strings.Contains(lower, sensitive) {
			slog.Error("execute_sql error (sanitized)", "project_id", projectID, "error", msg)
			return "internal database connection error"
		}
	}
	return msg
}

// executeChatExecuteSQL handles the execute_sql tool call in the chat tool phase.
func (c *Controller) executeChatExecuteSQL(
	ctx context.Context,
	arguments string,
	project *models.AppProject,
	collectedContext *strings.Builder,
) (string, error) {
	var args struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return fmt.Sprintf("Error: invalid arguments: %s", err.Error()), nil
	}

	query := strings.TrimSpace(args.Query)
	if query == "" {
		return "Error: query is required", nil
	}

	// Validate allowed statements: SELECT, INSERT, UPDATE, WITH (CTE)
	upper := strings.ToUpper(query)
	isSelect := strings.HasPrefix(upper, "SELECT") || strings.HasPrefix(upper, "WITH")
	isInsert := strings.HasPrefix(upper, "INSERT")
	isUpdate := strings.HasPrefix(upper, "UPDATE")
	if !isSelect && !isInsert && !isUpdate {
		return "Error: only SELECT, INSERT, and UPDATE queries are allowed. DELETE is not permitted — use UPDATE to set a status flag instead.", nil
	}
	// Reject destructive keywords (word-boundary match to avoid false positives
	// like CREATED_AT matching CREATE, or DELETED matching DELETE)
	for kw, re := range sqlBlockedKeywordRegexes {
		if re.MatchString(upper) {
			return fmt.Sprintf("Error: query contains disallowed keyword: %s. Use UPDATE with a status/flag column instead of DELETE.", kw), nil
		}
	}
	// Block multi-statement queries (semicolons) to prevent statement stacking attacks.
	if strings.Contains(query, ";") {
		return "Error: only single-statement queries are allowed. Remove semicolons and send one query at a time.", nil
	}

	neonDB, err := c.openNeonDB(project.ID.String())
	if err != nil {
		slog.Error("execute_sql: failed to connect to database", "error", err, "project_id", project.ID)
		return "Error: failed to connect to database. Please try again.", nil
	}
	defer neonDB.Close()

	queryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// For write queries (INSERT/UPDATE), execute and return affected rows
	if isInsert || isUpdate {
		// If INSERT has RETURNING, use QueryContext to capture returned rows
		if isInsert && strings.Contains(upper, "RETURNING") {
			rows, err := neonDB.QueryContext(queryCtx, query)
			if err != nil {
				return fmt.Sprintf("Query error: %s", sanitizeDBError(project.ID.String(), err)), nil
			}
			defer rows.Close()
			cols, _ := rows.Columns()
			var resultBuf strings.Builder
			resultBuf.WriteString(strings.Join(cols, " | "))
			resultBuf.WriteString("\n")
			rowCount := 0
			for rows.Next() && rowCount < 50 {
				values := make([]any, len(cols))
				valuePtrs := make([]any, len(cols))
				for i := range values {
					valuePtrs[i] = &values[i]
				}
				if err := rows.Scan(valuePtrs...); err != nil {
					continue
				}
				var rowVals []string
				for _, v := range values {
					if v == nil {
						rowVals = append(rowVals, "NULL")
					} else if b, ok := v.([]byte); ok {
						rowVals = append(rowVals, string(b))
					} else {
						rowVals = append(rowVals, fmt.Sprintf("%v", v))
					}
				}
				resultBuf.WriteString(strings.Join(rowVals, " | "))
				resultBuf.WriteString("\n")
				rowCount++
			}
			summary := fmt.Sprintf("OK — %d row(s) inserted:\n%s", rowCount, resultBuf.String())
			collectedContext.WriteString(fmt.Sprintf("\n[Database Write Result]\nQuery: %s\n%s\n", query, summary))
			slog.Info("execute_sql insert+returning via fast model", "project_id", project.ID, "rows", rowCount)
			return summary, nil
		}

		result, err := neonDB.ExecContext(queryCtx, query)
		if err != nil {
			return fmt.Sprintf("Query error: %s", sanitizeDBError(project.ID.String(), err)), nil
		}
		affected, _ := result.RowsAffected()
		summary := fmt.Sprintf("OK — %d row(s) affected.", affected)
		collectedContext.WriteString(fmt.Sprintf("\n[Database Write Result]\nQuery: %s\n%s\n", query, summary))
		slog.Info("execute_sql write via fast model", "project_id", project.ID, "affected", affected)
		return summary, nil
	}

	// For SELECT queries, use read-only transaction
	tx, err := neonDB.BeginTx(queryCtx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return fmt.Sprintf("Error: failed to begin read-only transaction: %s", err.Error()), nil
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(queryCtx, query)
	if err != nil {
		return fmt.Sprintf("Query error: %s", sanitizeDBError(project.ID.String(), err)), nil
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return fmt.Sprintf("Error reading columns: %s", err.Error()), nil
	}

	var result strings.Builder
	result.WriteString(strings.Join(cols, " | "))
	result.WriteString("\n")

	rowCount := 0
	for rows.Next() && rowCount < 50 {
		values := make([]any, len(cols))
		valuePtrs := make([]any, len(cols))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			continue
		}
		var rowVals []string
		for _, v := range values {
			if v == nil {
				rowVals = append(rowVals, "NULL")
			} else if b, ok := v.([]byte); ok {
				rowVals = append(rowVals, string(b))
			} else {
				rowVals = append(rowVals, fmt.Sprintf("%v", v))
			}
		}
		result.WriteString(strings.Join(rowVals, " | "))
		result.WriteString("\n")
		rowCount++
	}

	var summary string
	if rowCount == 0 {
		summary = fmt.Sprintf("Query returned 0 rows. Columns: %s", strings.Join(cols, ", "))
	} else {
		summary = fmt.Sprintf("%d row(s):\n%s", rowCount, result.String())
	}

	collectedContext.WriteString(fmt.Sprintf("\n[Database Query Result]\nQuery: %s\n%s\n", query, summary))
	slog.Info("execute_sql tool called via fast model", "project_id", project.ID, "rows", rowCount)
	return summary, nil
}

// executeChatRunMigrations handles schema changes on the app's Neon database.
func (c *Controller) executeChatRunMigrations(
	ctx context.Context,
	arguments string,
	project *models.AppProject,
	repo *gitrepo.Repo,
	fileMap map[string]string,
	sendEvent func(string, any) bool,
	collectedContext *strings.Builder,
) (string, error) {
	var args struct {
		SQL           string             `json:"sql"`
		UpdatedModels []models.DataModel `json:"updated_models"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return fmt.Sprintf("Error: invalid arguments: %s", err.Error()), nil
	}
	if strings.TrimSpace(args.SQL) == "" {
		return "Error: sql is required", nil
	}
	if len(args.UpdatedModels) == 0 {
		return "Error: updated_models is required", nil
	}

	// Validate SQL — only allow DDL statements (ALTER, CREATE, DROP) per statement
	for _, stmt := range strings.Split(args.SQL, ";") {
		trimmed := strings.TrimSpace(stmt)
		if trimmed == "" {
			continue
		}
		stripped := database.StripLeadingSQLComments(trimmed)
		if stripped == "" {
			continue
		}
		upperStmt := strings.ToUpper(stripped)
		if !strings.HasPrefix(upperStmt, "ALTER") && !strings.HasPrefix(upperStmt, "CREATE") && !strings.HasPrefix(upperStmt, "DROP") {
			return fmt.Sprintf("Error: only DDL statements (ALTER TABLE, CREATE TABLE, DROP TABLE) are allowed, found: %.60s", trimmed), nil
		}
	}

	sendEvent("tool_call", responses.ToolCallEvent{Tool: "run_migrations", Arguments: arguments})

	neonDB, err := c.openNeonDB(project.ID.String())
	if err != nil {
		return fmt.Sprintf("Error: failed to connect to database: %s", err.Error()), nil
	}
	defer neonDB.Close()

	queryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if _, err := neonDB.ExecContext(queryCtx, args.SQL); err != nil {
		return fmt.Sprintf("Migration failed: %s", err.Error()), nil
	}

	// Update stored data models
	dmJSON, err := models.MarshalDataModels(args.UpdatedModels)
	if err != nil {
		return fmt.Sprintf("Migration applied but failed to update models: %s", err.Error()), nil
	}
	project.DataModels = dmJSON
	if _, err := c.DB.UpdateAppProject(project); err != nil {
		slog.Error("failed to save updated data models", "error", err, "project_id", project.ID)
	}

	// Regenerate lib/schema.ts
	schemaContent := database.BuildDrizzleSchema(args.UpdatedModels)
	c.injectTemplateFile(repo, "lib/schema.ts", schemaContent, fileMap, sendEvent)

	collectedContext.WriteString("\n[Database Migrated]\nSchema changes applied successfully. lib/schema.ts has been regenerated. Update any code that uses the affected tables.\n")
	slog.Info("run_migrations tool called", "project_id", project.ID)
	return "Migration applied successfully. Schema updated.", nil
}

// executeChatManageEnvVars handles adding, updating, and deleting environment variables.
func (c *Controller) executeChatManageEnvVars(
	arguments string,
	project *models.AppProject,
	repo *gitrepo.Repo,
	fileMap map[string]string,
	sendEvent func(string, any) bool,
	collectedContext *strings.Builder,
) (string, error) {
	var args struct {
		Operations []struct {
			Action   string `json:"action"`
			Key      string `json:"key"`
			Value    string `json:"value"`
			IsPublic bool   `json:"is_public"`
		} `json:"operations"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return fmt.Sprintf("Error: invalid arguments: %s", err.Error()), nil
	}

	sendEvent("tool_call", responses.ToolCallEvent{Tool: "manage_env_vars", Arguments: arguments})

	var results []string
	for _, op := range args.Operations {
		switch op.Action {
		case "set":
			existing, err := c.DB.GetAppEnvVarByKey(project.ID.String(), op.Key)
			if err == nil && existing.ID != uuid.Nil {
				existing.Value = op.Value
				existing.IsPublic = op.IsPublic
				existing.UpdatedAt = time.Now()
				if _, err := c.DB.UpdateAppEnvVar(existing); err != nil {
					results = append(results, fmt.Sprintf("FAILED set %s: %s", op.Key, err.Error()))
					continue
				}
			} else {
				envVar := &models.AppEnvVar{
					AppProjectID: project.ID,
					Key:          op.Key,
					Value:        op.Value,
					IsPublic:     op.IsPublic,
				}
				if _, err := c.DB.CreateAppEnvVar(envVar); err != nil {
					results = append(results, fmt.Sprintf("FAILED set %s: %s", op.Key, err.Error()))
					continue
				}
			}
			results = append(results, fmt.Sprintf("OK set %s", op.Key))

		case "delete":
			existing, err := c.DB.GetAppEnvVarByKey(project.ID.String(), op.Key)
			if err != nil || existing.ID == uuid.Nil {
				results = append(results, fmt.Sprintf("SKIPPED delete %s: not found", op.Key))
				continue
			}
			if err := c.DB.DeleteAppEnvVar(existing); err != nil {
				results = append(results, fmt.Sprintf("FAILED delete %s: %s", op.Key, err.Error()))
				continue
			}
			results = append(results, fmt.Sprintf("OK delete %s", op.Key))
		}
	}

	// Regenerate .env.local
	envVars := c.CollectAllEnvVars(project)
	if dotEnv := BuildDotEnvLocal(envVars); dotEnv != "" {
		c.injectTemplateFile(repo, ".env.local", dotEnv, fileMap, sendEvent)
	}

	summary := strings.Join(results, "\n")
	collectedContext.WriteString(fmt.Sprintf("\n[Environment Variables Updated]\n%s\n.env.local has been regenerated.\n", summary))
	slog.Info("manage_env_vars tool called", "project_id", project.ID, "operations", len(args.Operations))
	return summary, nil
}

// executeChatInstallPackage adds npm packages to package.json.
func (c *Controller) executeChatInstallPackage(
	arguments string,
	project *models.AppProject,
	repo *gitrepo.Repo,
	fileMap map[string]string,
	sendEvent func(string, any) bool,
	collectedContext *strings.Builder,
) (string, error) {
	var args struct {
		Packages []string `json:"packages"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return fmt.Sprintf("Error: invalid arguments: %s", err.Error()), nil
	}
	if len(args.Packages) == 0 {
		return "Error: packages list is empty", nil
	}

	sendEvent("tool_call", responses.ToolCallEvent{Tool: "install_package", Arguments: arguments})

	pkgContent, ok := fileMap["package.json"]
	if !ok {
		return "Error: package.json not found in project", nil
	}

	var pkgJSON map[string]any
	if err := json.Unmarshal([]byte(pkgContent), &pkgJSON); err != nil {
		return fmt.Sprintf("Error: failed to parse package.json: %s", err.Error()), nil
	}

	deps, ok := pkgJSON["dependencies"].(map[string]any)
	if !ok {
		deps = make(map[string]any)
		pkgJSON["dependencies"] = deps
	}

	var added []string
	var skipped []string
	for _, pkg := range args.Packages {
		pkg = strings.TrimSpace(pkg)
		if pkg == "" {
			continue
		}
		if _, exists := deps[pkg]; exists {
			skipped = append(skipped, pkg)
			continue
		}
		version := packageVersionLookup(pkg)
		deps[pkg] = version
		added = append(added, fmt.Sprintf("%s@%s", pkg, version))
	}

	if len(added) == 0 {
		return fmt.Sprintf("All packages already installed: %s", strings.Join(skipped, ", ")), nil
	}

	updatedJSON, err := json.MarshalIndent(pkgJSON, "", "  ")
	if err != nil {
		return fmt.Sprintf("Error: failed to serialize package.json: %s", err.Error()), nil
	}

	if writeErr := repo.WriteFile("package.json", updatedJSON); writeErr != nil {
		slog.Error("failed to update package.json", "error", writeErr, "project_id", project.ID)
	} else {
		fileMap["package.json"] = string(updatedJSON)
		sendEvent("file_complete", responses.FileCompleteEvent{
			Action: "modify", FilePath: "package.json",
			FileType: "json", Content: string(updatedJSON),
		})
	}

	sendEvent("deps_ready", map[string]any{"package_json_changed": true})

	summary := fmt.Sprintf("Installed: %s", strings.Join(added, ", "))
	if len(skipped) > 0 {
		summary += fmt.Sprintf("\nAlready installed: %s", strings.Join(skipped, ", "))
	}
	collectedContext.WriteString(fmt.Sprintf("\n[Packages Installed]\n%s\n", summary))
	slog.Info("install_package tool called", "project_id", project.ID, "added", len(added))
	return summary, nil
}

// packageVersionLookup returns a version string for common npm packages.
func packageVersionLookup(pkg string) string {
	versions := map[string]string{
		"zustand": "^4.5.0", "axios": "^1.7.0", "date-fns": "^3.6.0",
		"zod": "^3.23.0", "react-hook-form": "^7.51.0", "@hookform/resolvers": "^3.3.0",
		"recharts": "^2.12.0", "react-icons": "^5.0.0", "swiper": "^11.0.0",
		"@tanstack/react-query": "^5.0.0", "@tanstack/react-table": "^8.0.0",
		"sonner": "^1.0.0", "next-themes": "^0.3.0", "vaul": "^0.9.0",
		"cmdk": "^0.2.0", "swr": "^2.2.0", "stripe": "^14.0.0",
		"@stripe/stripe-js": "^2.0.0", "@stripe/react-stripe-js": "^2.0.0",
		"resend": "^3.0.0", "react-markdown": "^9.0.0", "sharp": "^0.33.0",
		"drizzle-orm": "^0.39.0", "@neondatabase/serverless": "^0.10.0",
		"jose": "^5.9.0", "bcryptjs": "^2.4.3", "uuid": "^9.0.0",
		"lodash": "^4.17.0", "framer-motion": "^11.0.0",
		"class-variance-authority": "^0.7.0", "tailwind-merge": "^2.2.0",
	}
	if strings.HasPrefix(pkg, "@radix-ui/react-") {
		return "^1.1.0"
	}
	if v, ok := versions[pkg]; ok {
		return v
	}
	return "latest"
}

// executeChatGenerateImage generates an AI image via fal.ai and uploads to S3.
// Deducts 1 AI credit per successful generation.
func (c *Controller) executeChatGenerateImage(
	ctx context.Context,
	arguments string,
	project *models.AppProject,
	user *models.User,
	sendEvent func(string, any) bool,
	collectedContext *strings.Builder,
) (string, error) {
	var args struct {
		Prompt   string `json:"prompt"`
		Filename string `json:"filename"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return fmt.Sprintf("Error: invalid arguments: %s", err.Error()), nil
	}

	apiKey := c.SecretManager.GetSecret("FAL_API_KEY")
	if apiKey == "" {
		return "Error: image generation service not configured (FAL_API_KEY missing)", nil
	}

	falModel := c.SecretManager.GetSecret("FAL_IMAGE_MODEL")
	if falModel == "" {
		falModel = "fal-ai/flux/schnell"
	}

	sendEvent("tool_call", responses.ToolCallEvent{Tool: "generate_image", Arguments: arguments})
	sendEvent("progress", responses.ProgressEvent{
		Phase:   "generating_image",
		Message: "Generating image...",
	})

	cdnURL, err := fal.GenerateImage(ctx, apiKey, falModel, args.Prompt, "")
	if err != nil {
		return fmt.Sprintf("Image generation failed: %s", err.Error()), nil
	}

	// Deduct 1 credit for image generation
	if user != nil {
		if err := c.DeductAICreditsUnchecked(user, 1, "generate_image", project.ID.String()); err != nil {
			slog.Warn("failed to deduct credit for image generation", "error", err, "user_id", user.ID, "project_id", project.ID)
		}
	}

	imgData, err := downloadImage(ctx, cdnURL)
	if err != nil {
		collectedContext.WriteString(fmt.Sprintf("\n[Generated Image]\nImage URL (temporary CDN): %s\nUse this URL in code via next/image.\n", cdnURL))
		return fmt.Sprintf("Image generated (CDN URL): %s", cdnURL), nil
	}

	filename := strings.TrimSpace(args.Filename)
	if filename == "" {
		filename = "generated-image.jpg"
	}
	s3Key := fmt.Sprintf("apps/%s/images/%s", project.ID, filename)
	// Use a detached context for S3 upload so it doesn't fail when the parent
	// tool-execution context is near its deadline (the image is already generated).
	uploadCtx, uploadCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer uploadCancel()
	var imageURL string
	var uploadErr error
	for attempt := 0; attempt < 3; attempt++ {
		imageURL, uploadErr = c.AssetsStorage.UploadFileBytePublic(uploadCtx, s3Key, imgData, "image/jpeg")
		if uploadErr == nil {
			break
		}
		slog.Warn("S3 upload failed for generated image, retrying",
			"project_id", project.ID, "s3_key", s3Key,
			"attempt", attempt+1, "error", uploadErr)
		time.Sleep(time.Duration(attempt+1) * time.Second)
	}
	if uploadErr != nil {
		collectedContext.WriteString(fmt.Sprintf("\n[Generated Image]\nImage URL (temporary CDN): %s\n", cdnURL))
		return fmt.Sprintf("Image generated but S3 upload failed. CDN URL: %s", cdnURL), nil
	}
	collectedContext.WriteString(fmt.Sprintf("\n[Generated Image]\nImage URL: %s\nUse this URL in code via next/image or as a background. The image shows: %s\n", imageURL, args.Prompt))
	slog.Info("generate_image tool called", "project_id", project.ID, "s3_key", s3Key)
	return fmt.Sprintf("Image generated and uploaded: %s", imageURL), nil
}

// executeChatTakeScreenshot captures a screenshot of a URL for visual context.
// The base64 data URI is stored in the screenshots slice for proper vision
// handling, NOT embedded as text in collectedContext (which would blow past
// the token limit).
func (c *Controller) executeChatTakeScreenshot(
	arguments string,
	collectedContext *strings.Builder,
	screenshots *[]string,
) (string, error) {
	var args struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return fmt.Sprintf("Error: invalid arguments: %s", err.Error()), nil
	}

	targetURL := strings.TrimSpace(args.URL)
	if targetURL == "" {
		return "Error: url is required", nil
	}
	if !strings.HasPrefix(targetURL, "http://") && !strings.HasPrefix(targetURL, "https://") {
		targetURL = "https://" + targetURL
	}

	apiKey := c.SecretManager.GetSecret("SITESHOT_API_KEY")
	if apiKey == "" {
		return "Error: screenshot service not configured (SITESHOT_API_KEY missing)", nil
	}

	imgData, err := external.GetDesktopScreenshotOfURL(targetURL, apiKey)
	if err != nil {
		return fmt.Sprintf("Screenshot capture failed: %s", err.Error()), nil
	}
	if len(imgData) == 0 {
		return "Screenshot capture returned empty image", nil
	}

	b64 := base64.StdEncoding.EncodeToString(imgData)
	*screenshots = append(*screenshots, "data:image/png;base64,"+b64)

	collectedContext.WriteString(fmt.Sprintf("\n[Screenshot Captured]\nURL: %s\nA desktop screenshot (1280x800) has been captured.\n", targetURL))
	slog.Info("take_screenshot tool called", "url", targetURL, "size_bytes", len(imgData))
	return fmt.Sprintf("Screenshot captured successfully for %s. Use the website analysis text to understand the design — exact colors, layout, and structure are described in the analysis.", targetURL), nil
}

// executeChatConfigureMobileApp handles the configure_mobile_app tool during
// the chat Phase 1 (fast model tool phase). Validates bridge methods, derives
// permissions, and persists the config to the database.
func (c *Controller) executeChatConfigureMobileApp(
	arguments string,
	project *models.AppProject,
	sendEvent func(string, any) bool,
	collectedContext *strings.Builder,
) (string, error) {
	var args struct {
		BridgeMethods []string `json:"bridge_methods"`
		Permissions   []string `json:"permissions"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return fmt.Sprintf("Error: invalid arguments: %s", err.Error()), nil
	}

	sendEvent("tool_call", responses.ToolCallEvent{Tool: "configure_mobile_app", Arguments: arguments})

	validMethods := map[string]bool{
		"getDeviceInfo": true, "share": true, "haptic": true,
		"openExternal": true, "getConnectionStatus": true,
		"storage.get": true, "storage.set": true, "storage.remove": true,
		"camera.takePhoto": true, "camera.pickImage": true, "camera.pickVideo": true,
		"contacts.getAll": true, "contacts.pick": true,
		"calendar.addEvent": true, "files.pick": true,
	}
	var filteredMethods []string
	for _, m := range args.BridgeMethods {
		if validMethods[m] {
			filteredMethods = append(filteredMethods, m)
		}
	}

	methodToPermission := map[string]string{
		"camera.takePhoto": "camera", "camera.pickImage": "camera", "camera.pickVideo": "camera",
		"contacts.getAll": "contacts", "contacts.pick": "contacts",
		"calendar.addEvent": "calendar",
		"files.pick":        "storage",
	}
	permSet := map[string]bool{"notifications": true}
	for _, p := range args.Permissions {
		permSet[p] = true
	}
	for _, m := range filteredMethods {
		if p, ok := methodToPermission[m]; ok {
			permSet[p] = true
		}
	}
	var permissions []string
	for p := range permSet {
		permissions = append(permissions, p)
	}

	mobileConfig, configErr := c.DB.GetMobileAppConfig(project.ID.String())
	if configErr != nil {
		mobileConfig = &models.MobileAppConfig{
			ProjectID:            project.ID,
			PushEnabled:          true,
			BridgeEnabled:        true,
			AnalyticsEnabled:     true,
			PullToRefreshEnabled: true,
			ExternalBrowserLinks: true,
		}
	}
	mobileConfig.BridgeEnabled = true
	mobileConfig.PushEnabled = true
	mobileConfig.BridgeMethods = filteredMethods
	mobileConfig.Permissions = permissions

	if _, upsertErr := c.DB.UpsertMobileAppConfig(mobileConfig); upsertErr != nil {
		return fmt.Sprintf("Error: failed to save config: %s", upsertErr.Error()), nil
	}

	sendEvent("mobile_config_updated", map[string]any{
		"bridge_methods": filteredMethods,
		"permissions":    permissions,
	})

	result := fmt.Sprintf("Mobile app configured. Bridge methods: %v. Permissions: %v.", filteredMethods, permissions)
	collectedContext.WriteString("\n[Mobile config updated: bridge_methods=" + strings.Join(filteredMethods, ",") + " permissions=" + strings.Join(permissions, ",") + "]\n")
	return result, nil
}

// executeChatSearchComponents finds and injects pre-built UI components.
func (c *Controller) executeChatSearchComponents(
	arguments string,
	project *models.AppProject,
	repo *gitrepo.Repo,
	fileMap map[string]string,
	sendEvent func(string, any) bool,
	collectedContext *strings.Builder,
) (string, error) {
	var args struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return fmt.Sprintf("Error: invalid arguments: %s", err.Error()), nil
	}

	query := strings.ToLower(strings.TrimSpace(args.Query))
	if query == "" {
		return "Error: query is required", nil
	}

	sendEvent("tool_call", responses.ToolCallEvent{Tool: "search_components", Arguments: arguments})

	type componentInfo struct {
		Name     string
		Path     string
		Keywords []string
		HasTemplate bool
	}
	components := []componentInfo{
		// Pre-built templates (already have hardcoded content)
		{Name: "Button", Path: "components/ui/button.tsx", Keywords: []string{"button", "btn"}, HasTemplate: true},
		{Name: "Card", Path: "components/ui/card.tsx", Keywords: []string{"card", "panel"}, HasTemplate: true},
		{Name: "Input", Path: "components/ui/input.tsx", Keywords: []string{"input", "text field"}, HasTemplate: true},
		{Name: "Badge", Path: "components/ui/badge.tsx", Keywords: []string{"badge", "tag", "chip"}, HasTemplate: true},
		{Name: "Skeleton", Path: "components/ui/skeleton.tsx", Keywords: []string{"skeleton", "loading"}, HasTemplate: true},
		// Generatable by code-gen model (no template, but known shadcn/ui components)
		{Name: "Dialog", Path: "components/ui/dialog.tsx", Keywords: []string{"dialog", "modal", "popup"}},
		{Name: "Sheet", Path: "components/ui/sheet.tsx", Keywords: []string{"sheet", "drawer", "side panel"}},
		{Name: "Tabs", Path: "components/ui/tabs.tsx", Keywords: []string{"tabs", "tab"}},
		{Name: "Accordion", Path: "components/ui/accordion.tsx", Keywords: []string{"accordion", "collapse", "faq"}},
		{Name: "Alert Dialog", Path: "components/ui/alert-dialog.tsx", Keywords: []string{"alert", "confirm"}},
		{Name: "Dropdown Menu", Path: "components/ui/dropdown-menu.tsx", Keywords: []string{"dropdown", "menu"}},
		{Name: "Select", Path: "components/ui/select.tsx", Keywords: []string{"select", "picker", "combobox"}},
		{Name: "Toast", Path: "components/ui/toast.tsx", Keywords: []string{"toast", "notification", "snackbar"}},
		{Name: "Tooltip", Path: "components/ui/tooltip.tsx", Keywords: []string{"tooltip", "hint"}},
		{Name: "Popover", Path: "components/ui/popover.tsx", Keywords: []string{"popover", "floating"}},
		{Name: "Checkbox", Path: "components/ui/checkbox.tsx", Keywords: []string{"checkbox", "check"}},
		{Name: "Radio Group", Path: "components/ui/radio-group.tsx", Keywords: []string{"radio"}},
		{Name: "Switch", Path: "components/ui/switch.tsx", Keywords: []string{"switch", "toggle"}},
		{Name: "Slider", Path: "components/ui/slider.tsx", Keywords: []string{"slider", "range"}},
		{Name: "Progress", Path: "components/ui/progress.tsx", Keywords: []string{"progress", "loading bar"}},
		{Name: "Separator", Path: "components/ui/separator.tsx", Keywords: []string{"separator", "divider"}},
		{Name: "Avatar", Path: "components/ui/avatar.tsx", Keywords: []string{"avatar", "profile picture"}},
		{Name: "Table", Path: "components/ui/table.tsx", Keywords: []string{"table", "data table", "grid"}},
		{Name: "Textarea", Path: "components/ui/textarea.tsx", Keywords: []string{"textarea", "multiline"}},
		{Name: "Label", Path: "components/ui/label.tsx", Keywords: []string{"label", "form label"}},
		{Name: "Calendar", Path: "components/ui/calendar.tsx", Keywords: []string{"calendar", "date picker", "date"}},
		{Name: "Carousel", Path: "components/ui/carousel.tsx", Keywords: []string{"carousel", "slideshow", "gallery"}},
		{Name: "Navigation Menu", Path: "components/ui/navigation-menu.tsx", Keywords: []string{"navigation", "nav menu"}},
		{Name: "Scroll Area", Path: "components/ui/scroll-area.tsx", Keywords: []string{"scroll", "overflow"}},
	}

	var matched []componentInfo
	for _, comp := range components {
		for _, kw := range comp.Keywords {
			if strings.Contains(query, kw) || strings.Contains(kw, query) {
				matched = append(matched, comp)
				break
			}
		}
	}

	if len(matched) == 0 {
		return fmt.Sprintf("No components found matching '%s'. The code-gen model can create custom components.", query), nil
	}

	var injected, alreadyInstalled, toGenerate []string
	for _, comp := range matched {
		if _, exists := fileMap[comp.Path]; exists {
			alreadyInstalled = append(alreadyInstalled, comp.Name)
			continue
		}
		if comp.HasTemplate {
			content := templates.GetUIContent(comp.Path)
			if content != "" {
				c.injectTemplateFile(repo, comp.Path, content, fileMap, sendEvent)
				injected = append(injected, comp.Name)
				continue
			}
		}
		toGenerate = append(toGenerate, comp.Name)
	}

	var result strings.Builder
	if len(injected) > 0 {
		result.WriteString(fmt.Sprintf("Injected: %s\n", strings.Join(injected, ", ")))
	}
	if len(alreadyInstalled) > 0 {
		result.WriteString(fmt.Sprintf("Already in project: %s\n", strings.Join(alreadyInstalled, ", ")))
	}
	if len(toGenerate) > 0 {
		result.WriteString(fmt.Sprintf("Needs code-gen model to create: %s (using @radix-ui primitives + shadcn/ui patterns)\n", strings.Join(toGenerate, ", ")))
	}

	collectedContext.WriteString(fmt.Sprintf("\n[Component Search: %s]\n%s\n", query, result.String()))
	slog.Info("search_components tool called", "project_id", project.ID, "query", query, "matched", len(matched))
	return result.String(), nil
}

func filterToolsByName(names ...string) []utils.Tool {
	wanted := make(map[string]bool, len(names))
	for _, n := range names {
		wanted[n] = true
	}
	var out []utils.Tool
	for _, t := range appBuilderTools {
		if wanted[t.Function.Name] {
			out = append(out, t)
		}
	}
	return out
}

// appBuilderTools defines the tools available to the unified builder LLM.
var appBuilderTools = []utils.Tool{
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
			Name:        "write_file",
			Description: "Create a new file or overwrite an existing file with the given content. Use this to create new files or when you need to replace the entire content of a file. For small targeted changes to existing files, prefer edit_file instead.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file_path": map[string]any{
						"type":        "string",
						"description": "Relative file path, e.g. 'app/page.tsx' or 'components/header.tsx'",
					},
					"content": map[string]any{
						"type":        "string",
						"description": "The complete file content to write",
					},
				},
				"required": []string{"file_path", "content"},
			},
		},
	},
	{
		Type: "function",
		Function: utils.ToolFunction{
			Name:        "write_files",
			Description: "Create or overwrite multiple files at once. Use this instead of calling write_file repeatedly when you need to create several files. Much more efficient for initial app generation.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"files": map[string]any{
						"type":        "array",
						"description": "Array of files to write",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"file_path": map[string]any{
									"type":        "string",
									"description": "Relative file path, e.g. 'app/page.tsx'",
								},
								"content": map[string]any{
									"type":        "string",
									"description": "The complete file content to write",
								},
							},
							"required": []string{"file_path", "content"},
						},
					},
				},
				"required": []string{"files"},
			},
		},
	},
	{
		Type: "function",
		Function: utils.ToolFunction{
			Name:        "edit_file",
			Description: "Apply a single small edit to an existing file by replacing a specific string. ONLY use for tiny, surgical changes (one import, one value). You MUST read_file first. The old_string must match exactly. NEVER call edit_file more than once on the same file — use write_file to rewrite it instead. If old_string is not found, use write_file to rewrite the whole file.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file_path": map[string]any{
						"type":        "string",
						"description": "Relative file path of the file to edit",
					},
					"old_string": map[string]any{
						"type":        "string",
						"description": "The exact string to find and replace. Must be unique within the file.",
					},
					"new_string": map[string]any{
						"type":        "string",
						"description": "The replacement string",
					},
				},
				"required": []string{"file_path", "old_string", "new_string"},
			},
		},
	},
	{
		Type: "function",
		Function: utils.ToolFunction{
			Name:        "delete_file",
			Description: "Delete a file from the project. Use this to remove files that are no longer needed.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file_path": map[string]any{
						"type":        "string",
						"description": "Relative file path of the file to delete",
					},
				},
				"required": []string{"file_path"},
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
			Description: "Create or manage products, categories, and discounts in the e-commerce store. Use this after setup_ecommerce to populate the store with products. Supports batch operations.",
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
									"enum":        []string{"create_product", "update_product", "delete_product", "bulk_create_products", "create_category", "update_category", "delete_category", "create_discount", "update_discount", "delete_discount", "update_store_settings", "list_products", "list_categories"},
									"description": "The operation to perform",
								},
								"data": map[string]any{
									"type":        "object",
									"description": "Operation data. For bulk_create_products: MUST contain a \"products\" array, e.g. {\"products\": [{\"name\": \"...\", \"price_in_cents\": 999, \"description\": \"...\", \"category_name\": \"...\", \"inventory\": 50}]}. For create_category: {\"name\": \"...\", \"description\": \"...\"}. For create_discount: {\"code\": \"...\", \"type\": \"percentage\", \"value\": 10}.",
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
	{
		Type: "function",
		Function: utils.ToolFunction{
			Name:        "setup_database",
			Description: "Provision a PostgreSQL database with schema tables. Call this when the app needs persistent data storage. Returns connection details and generates lib/db.ts and lib/schema.ts. Also provisions auth tables if the app needs authentication.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"tables": map[string]any{
						"type":        "array",
						"description": "Array of table definitions",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"name": map[string]any{
									"type":        "string",
									"description": "Table name, e.g. 'posts'",
								},
								"columns": map[string]any{
									"type": "array",
									"items": map[string]any{
										"type": "object",
										"properties": map[string]any{
											"name":        map[string]any{"type": "string"},
											"type":        map[string]any{"type": "string", "description": "text, integer, boolean, timestamp, uuid, jsonb"},
											"primary_key": map[string]any{"type": "boolean"},
											"nullable":    map[string]any{"type": "boolean"},
											"default":     map[string]any{"type": "string"},
											"references":  map[string]any{"type": "string", "description": "e.g. 'categories.id'"},
										},
										"required": []string{"name", "type"},
									},
								},
							},
							"required": []string{"name", "columns"},
						},
					},
					"needs_auth": map[string]any{
						"type":        "boolean",
						"description": "Set to true if the app needs user authentication. This adds a users table and auth infrastructure.",
					},
				},
				"required": []string{"tables"},
			},
		},
	},
	{
		Type: "function",
		Function: utils.ToolFunction{
			Name:        "configure_mobile_app",
			Description: "Enable native bridge methods and set device permissions for the mobile app. You MUST call this BEFORE writing any code that uses WemobBridge methods.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"bridge_methods": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Bridge methods to enable. Valid: getDeviceInfo, share, haptic, openExternal, getConnectionStatus, storage.get, storage.set, storage.remove, camera.takePhoto, camera.pickImage, camera.pickVideo, contacts.getAll, contacts.pick, calendar.addEvent, files.pick",
					},
					"permissions": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Device permissions to request. Valid: camera, location, contacts, storage, microphone, photos, notifications, calendar, biometrics. 'notifications' is always included automatically.",
					},
				},
				"required": []string{"bridge_methods", "permissions"},
			},
		},
	},
}

// chatManageStoreTool lets the fast model manage products, categories, and
// discounts in the user's e-commerce store during the chat tool phase.
var chatManageStoreTool = utils.Tool{
	Type: "function",
	Function: utils.ToolFunction{
		Name:        "manage_store",
		Description: "Create, update, delete, or list products, categories, and discounts in the e-commerce store. Use when the user asks to manage their store inventory. IMPORTANT: Product names, descriptions, and category names MUST be in the same language as the user's message.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"operations": map[string]any{
					"type":        "array",
					"description": "Array of store operations to execute in order",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"action": map[string]any{
								"type":        "string",
								"enum":        []string{"create_product", "update_product", "delete_product", "bulk_create_products", "create_category", "update_category", "delete_category", "create_discount", "update_discount", "delete_discount", "update_store_settings", "list_products", "list_categories"},
								"description": "The operation to perform",
							},
							"data": map[string]any{
								"type":        "object",
								"description": "Operation payload. Products: name, price_in_cents, description, images, category (slug), tags, status, sku, inventory. Categories: name, description, image. Discounts: code, type (percentage/fixed), value, min_order_cents, max_uses, starts_at, ends_at. For updates/deletes: include id or slug or name to identify the item.",
							},
						},
						"required": []string{"action", "data"},
					},
				},
			},
			"required": []string{"operations"},
		},
	},
}

// chatPreviewWebsiteTool lets the LLM analyze a website's structure and capture
// a screenshot for the code generation model to build the app from.
var chatPreviewWebsiteTool = utils.Tool{
	Type: "function",
	Function: utils.ToolFunction{
		Name:        "preview_website",
		Description: "Analyze a website URL by fetching its HTML structure and taking a screenshot. Use this when the user provides a URL or asks to convert/clone a website into an app. The screenshot and site metadata will be provided to the code generation model as reference.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url": map[string]any{
					"type":        "string",
					"description": "The website URL to preview (e.g. https://example.com). Add https:// if the user didn't include it.",
				},
			},
			"required": []string{"url"},
		},
	},
}

// chatSeedDatabaseTool lets the fast model seed or reseed the app's Neon database
// with realistic demo data when the user reports missing data.
var chatSeedDatabaseTool = utils.Tool{
	Type: "function",
	Function: utils.ToolFunction{
		Name:        "seed_database",
		Description: "Seed the app's database with realistic demo data in the user's language. Call this when the user says the database is empty, has no data, or asks for demo/sample/test data to be added.",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	},
}

// chatExecuteSQLTool lets the fast model run SELECT, INSERT, and UPDATE queries
// on the app's Neon database. DELETE is not allowed — use UPDATE with a soft-delete
// flag or status column instead.
var chatExecuteSQLTool = utils.Tool{
	Type: "function",
	Function: utils.ToolFunction{
		Name:        "execute_sql",
		Description: "Run a SQL query on the app's PostgreSQL database. Supports SELECT, INSERT, UPDATE. DELETE is NOT allowed. Use gen_random_uuid() for UUIDs, NOW() for timestamps. Do NOT provide 'id' values — they are auto-generated. You can insert multiple rows: INSERT INTO t (col) VALUES (...), (...), (...). Use RETURNING to get inserted row data back. IMPORTANT: All text content in INSERT/UPDATE statements must be in the same language as the user's message.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "A single SQL statement. Allowed: SELECT, INSERT, UPDATE, WITH. Not allowed: DELETE, DROP, ALTER, CREATE, TRUNCATE. Use the exact table and column names from the database schema.",
				},
			},
			"required": []string{"query"},
		},
	},
}

// chatRunMigrationsTool lets the fast model alter existing database tables.
var chatRunMigrationsTool = utils.Tool{
	Type: "function",
	Function: utils.ToolFunction{
		Name:        "run_migrations",
		Description: "Alter existing database tables — add columns, remove columns, rename columns, create new tables, or drop tables entirely. Call this when the user wants to change the database schema (e.g. 'add a rating field to articles', 'create a comments table', 'remove the products system'). This also regenerates lib/schema.ts to match.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"sql": map[string]any{
					"type":        "string",
					"description": "SQL DDL statements to execute. Use ALTER TABLE to add/drop/rename columns, CREATE TABLE for new tables, DROP TABLE to remove tables. Examples: ALTER TABLE \"articles\" ADD COLUMN \"rating\" INTEGER DEFAULT 0; DROP TABLE IF EXISTS \"products\";",
				},
				"updated_models": map[string]any{
					"type":        "array",
					"description": "The full updated data model definitions (all tables, not just changed ones). Same format as setup_database tables.",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"name": map[string]any{"type": "string"},
							"columns": map[string]any{
								"type": "array",
								"items": map[string]any{
									"type": "object",
									"properties": map[string]any{
										"name":        map[string]any{"type": "string"},
										"type":        map[string]any{"type": "string", "description": "text, integer, boolean, timestamp, uuid, jsonb"},
										"primary_key": map[string]any{"type": "boolean"},
										"nullable":    map[string]any{"type": "boolean"},
										"default":     map[string]any{"type": "string"},
										"references":  map[string]any{"type": "string"},
									},
									"required": []string{"name", "type"},
								},
							},
						},
						"required": []string{"name", "columns"},
					},
				},
			},
			"required": []string{"sql", "updated_models"},
		},
	},
}

// chatManageEnvVarsTool lets the fast model add, update, or delete environment variables.
var chatManageEnvVarsTool = utils.Tool{
	Type: "function",
	Function: utils.ToolFunction{
		Name:        "manage_env_vars",
		Description: "Add, update, or delete environment variables for the app. Call this when the user wants to set API keys, secrets, or configuration values. After changes, .env.local is automatically regenerated.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"operations": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"action": map[string]any{
								"type": "string",
								"enum": []string{"set", "delete"},
							},
							"key": map[string]any{
								"type":        "string",
								"description": "Environment variable name (e.g. STRIPE_SECRET_KEY, NEXT_PUBLIC_API_URL)",
							},
							"value": map[string]any{
								"type":        "string",
								"description": "Value to set (required for 'set' action)",
							},
							"is_public": map[string]any{
								"type":        "boolean",
								"description": "If true, prefixed with NEXT_PUBLIC_ and accessible in browser. Default false.",
							},
						},
						"required": []string{"action", "key"},
					},
				},
			},
			"required": []string{"operations"},
		},
	},
}

// chatInstallPackageTool lets the fast model add npm packages to package.json.
var chatInstallPackageTool = utils.Tool{
	Type: "function",
	Function: utils.ToolFunction{
		Name:        "install_package",
		Description: "Add npm packages to the project's package.json. Call this when the user asks to install a library or when you detect a needed dependency. Packages are added to the dependencies section.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"packages": map[string]any{
					"type":        "array",
					"description": "List of package names to install (e.g. ['zustand', '@tanstack/react-query', 'stripe'])",
					"items":       map[string]any{"type": "string"},
				},
			},
			"required": []string{"packages"},
		},
	},
}

// chatGenerateImageTool lets the fast model generate AI images via fal.ai.
var chatGenerateImageTool = utils.Tool{
	Type: "function",
	Function: utils.ToolFunction{
		Name:        "generate_image",
		Description: "Generate an AI image from a text prompt. The image is uploaded to S3 and a URL is returned. Use this when the user asks for a custom image, hero visual, logo concept, background, or any visual asset for their app. IMPORTANT: Write the image prompt in the SAME LANGUAGE as the user's message. If the user writes in Turkish, describe the image in Turkish. If in English, use English.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"prompt": map[string]any{
					"type":        "string",
					"description": "Detailed description of the image to generate. Be specific about style, colors, composition, and subject matter.",
				},
				"filename": map[string]any{
					"type":        "string",
					"description": "Filename for the image (e.g. 'hero-bg.jpg', 'logo.jpg'). Used in the S3 path.",
				},
			},
			"required": []string{"prompt", "filename"},
		},
	},
}

// chatTakeScreenshotTool lets the fast model capture a screenshot of a URL.
var chatTakeScreenshotTool = utils.Tool{
	Type: "function",
	Function: utils.ToolFunction{
		Name:        "take_screenshot",
		Description: "Capture a desktop screenshot of a URL. Use this when the user provides a reference website URL and you need to see what it looks like, or to capture the current deployed app for visual comparison. Returns a base64 image that can be used for vision analysis.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url": map[string]any{
					"type":        "string",
					"description": "The URL to screenshot (e.g. 'https://example.com'). Add https:// if not included.",
				},
			},
			"required": []string{"url"},
		},
	},
}

// chatSearchComponentsTool lets the fast model search for available UI components.
var chatSearchComponentsTool = utils.Tool{
	Type: "function",
	Function: utils.ToolFunction{
		Name:        "search_components",
		Description: "Search for available pre-built UI components that can be injected into the project. Use this when the user asks for a specific component (dialog, accordion, tabs, etc.) or when you need a shadcn/ui component that isn't already in the project.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Search term for the component (e.g. 'dialog', 'tabs', 'accordion', 'dropdown menu', 'toast')",
				},
			},
			"required": []string{"query"},
		},
	},
}

// chatConfigureMobileAppTool lets the fast model configure mobile bridge methods
// and permissions during the chat tool phase.
var chatConfigureMobileAppTool = utils.Tool{
	Type: "function",
	Function: utils.ToolFunction{
		Name:        "configure_mobile_app",
		Description: "Enable native bridge methods and set device permissions for the mobile app. Call this when the user explicitly asks to enable or change mobile-specific features like camera, contacts, or haptic feedback.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"bridge_methods": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Bridge methods to enable. Valid: getDeviceInfo, share, haptic, openExternal, getConnectionStatus, storage.get, storage.set, storage.remove, camera.takePhoto, camera.pickImage, camera.pickVideo, contacts.getAll, contacts.pick, calendar.addEvent, files.pick",
				},
				"permissions": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Device permissions to request. Valid: camera, location, contacts, storage, microphone, photos, notifications, calendar, biometrics. 'notifications' is always included automatically.",
				},
			},
			"required": []string{"bridge_methods", "permissions"},
		},
	},
}

// chatReadFileTool lets the fast model read project file contents during the chat
// tool phase so users can ask to see their code without hallucinated responses.
var chatReadFileTool = utils.Tool{
	Type: "function",
	Function: utils.ToolFunction{
		Name:        "read_file",
		Description: "Read the full content of a project file by its path. Use this when the user asks to see code, view a file, or show what changed. Call list_files first if you don't know the exact path.",
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
}

// chatListFilesTool lets the fast model list all project files during the chat
// tool phase so it can answer questions about project structure.
var chatListFilesTool = utils.Tool{
	Type: "function",
	Function: utils.ToolFunction{
		Name:        "list_files",
		Description: "List all files in the project with their types and sizes. Use this to understand the project structure or when the user asks what files exist.",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	},
}

// chatDiffFilesTool lets the fast model show what changed between commits so
// users can ask "what did you change?" and get real git diffs instead of guesses.
var chatDiffFilesTool = utils.Tool{
	Type: "function",
	Function: utils.ToolFunction{
		Name:        "diff_files",
		Description: "Show what changed in the project's code. With no arguments, shows the diff from the last commit (most recent changes). Optionally provide a commit hash to diff from a specific point. Use git_log first to find commit hashes if needed.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"from_commit": map[string]any{
					"type":        "string",
					"description": "Optional commit hash to diff from. If omitted, shows the last commit's changes.",
				},
				"to_commit": map[string]any{
					"type":        "string",
					"description": "Optional commit hash to diff to. If omitted, diffs to HEAD.",
				},
			},
		},
	},
}

// chatGitLogTool lets the fast model show recent commit history.
var chatGitLogTool = utils.Tool{
	Type: "function",
	Function: utils.ToolFunction{
		Name:        "git_log",
		Description: "Show recent commit history for the project. Returns the last N commits with their hashes, messages, and timestamps.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"limit": map[string]any{
					"type":        "integer",
					"description": "Number of commits to return (default 10, max 50).",
				},
			},
		},
	},
}

// chatManageAssetsTool lets the fast model list, rename, or delete user-uploaded assets.
var chatManageAssetsTool = utils.Tool{
	Type: "function",
	Function: utils.ToolFunction{
		Name:        "manage_assets",
		Description: "List, rename, or delete user-uploaded assets (3D models, fonts, images, videos, etc.). Use 'list' to show all assets. Use 'rename' or 'delete' with the asset id. Cannot upload files — the user must upload via the Assets button in the chat toolbar.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action": map[string]any{
					"type":        "string",
					"enum":        []string{"list", "rename", "delete"},
					"description": "Action to perform on assets.",
				},
				"asset_id": map[string]any{
					"type":        "string",
					"description": "Asset ID (required for rename and delete).",
				},
				"new_name": map[string]any{
					"type":        "string",
					"description": "New file name (required for rename).",
				},
			},
			"required": []string{"action"},
		},
	},
}

// executeChatManageAssets handles listing, renaming, and deleting user-uploaded assets.
func (c *Controller) executeChatManageAssets(
	arguments string,
	project *models.AppProject,
	sendEvent func(string, any) bool,
	collectedContext *strings.Builder,
) (string, error) {
	var args struct {
		Action  string `json:"action"`
		AssetID string `json:"asset_id"`
		NewName string `json:"new_name"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return fmt.Sprintf("Error: invalid arguments: %s", err.Error()), nil
	}

	sendEvent("tool_call", responses.ToolCallEvent{Tool: "manage_assets", Arguments: arguments})

	switch args.Action {
	case "list":
		assets, err := c.DB.GetAppAssetsByProjectID(project.ID.String())
		if err != nil {
			return fmt.Sprintf("Error listing assets: %s", err.Error()), nil
		}
		if len(assets) == 0 {
			return "No assets uploaded. The user can upload files using the Assets button (paperclip icon) in the chat toolbar.", nil
		}
		var lines []string
		for _, a := range assets {
			lines = append(lines, fmt.Sprintf("- id:%s name:%s type:%s url:%s", a.ID.String(), a.FileName, a.ContentType, a.PublicURL))
		}
		result := fmt.Sprintf("%d asset(s):\n%s", len(assets), strings.Join(lines, "\n"))
		collectedContext.WriteString(fmt.Sprintf("\n[Assets Listed]\n%s\n", result))
		return result, nil

	case "rename":
		if args.AssetID == "" || args.NewName == "" {
			return "Error: asset_id and new_name are required for rename", nil
		}
		asset, err := c.DB.GetAppAssetByID(args.AssetID)
		if err != nil {
			return fmt.Sprintf("Error: asset not found: %s", args.AssetID), nil
		}
		if asset.AppProjectID.String() != project.ID.String() {
			return "Error: asset does not belong to this project", nil
		}
		asset.FileName = args.NewName
		asset.UpdatedAt = time.Now()
		if _, err := c.DB.UpdateAppAsset(asset); err != nil {
			return fmt.Sprintf("Error renaming asset: %s", err.Error()), nil
		}
		result := fmt.Sprintf("Renamed asset to: %s (url: %s)", args.NewName, asset.PublicURL)
		collectedContext.WriteString(fmt.Sprintf("\n[Asset Renamed] %s\n", result))
		return result, nil

	case "delete":
		if args.AssetID == "" {
			return "Error: asset_id is required for delete", nil
		}
		asset, err := c.DB.GetAppAssetByID(args.AssetID)
		if err != nil {
			return fmt.Sprintf("Error: asset not found: %s", args.AssetID), nil
		}
		if asset.AppProjectID.String() != project.ID.String() {
			return "Error: asset does not belong to this project", nil
		}
		_ = c.AssetsStorage.DeleteObjectDirect(context.Background(), asset.S3Key)
		if err := c.DB.DeleteAppAsset(asset); err != nil {
			return fmt.Sprintf("Error deleting asset: %s", err.Error()), nil
		}
		result := fmt.Sprintf("Deleted asset: %s", asset.FileName)
		collectedContext.WriteString(fmt.Sprintf("\n[Asset Deleted] %s\n", result))
		return result, nil

	default:
		return fmt.Sprintf("Error: unknown action '%s'. Use list, rename, or delete.", args.Action), nil
	}
}

// --- Tool executor ---

// buildBuilderToolExecutor returns a ToolExecutor for the unified builder.
func (c *Controller) buildBuilderToolExecutor(ctx context.Context, project *models.AppProject, user *models.User, repo *gitrepo.Repo, fileMap map[string]string, cbs *builderToolCallbacks) utils.ToolExecutor {
	// Look up user's plan limits for DB provisioning
	var userLimits *models.StandartUserLimits
	if fetchedLimits, limitsErr := c.GetUserLimits(user); limitsErr == nil {
		userLimits = fetchedLimits
	}

	return func(name string, arguments string) (string, error) {
		// Early exit if context is cancelled — prevents partial writes after timeout
		if ctx.Err() != nil {
			return "", ctx.Err()
		}

		// Validate tool name
		if name == "" {
			return "Error: tool name is empty", nil
		}

		// Normalize empty/nil arguments to empty JSON object for tools that expect structured input.
		// read_file, list_files, etc. parse arguments with json.Unmarshal which would fail on "".
		if arguments == "" || arguments == "null" {
			arguments = "{}"
		}

		switch name {
		case "read_file":
			var args struct {
				FilePath string `json:"file_path"`
			}
			if err := json.Unmarshal([]byte(arguments), &args); err != nil {
				return fmt.Sprintf("Error: invalid arguments: %s", err.Error()), nil
			}
			cbs.fileMu.RLock()
			content, ok := fileMap[args.FilePath]
			cbs.fileMu.RUnlock()
			if !ok {
				return fmt.Sprintf("Error: file '%s' not found in project", args.FilePath), nil
			}
			return content, nil

		case "list_files":
			cbs.fileMu.RLock()
			var lines []string
			for path, content := range fileMap {
				lines = append(lines, fmt.Sprintf("%s (%d chars)", path, len(content)))
			}
			cbs.fileMu.RUnlock()
			sort.Strings(lines)
			return strings.Join(lines, "\n"), nil

		case "search_files":
			var args struct {
				Query string `json:"query"`
			}
			if err := json.Unmarshal([]byte(arguments), &args); err != nil {
				return fmt.Sprintf("Error: invalid arguments: %s", err.Error()), nil
			}
			query := strings.ToLower(args.Query)
			cbs.fileMu.RLock()
			var results []string
			for path, content := range fileMap {
				lines := strings.Split(content, "\n")
				for i, line := range lines {
					if strings.Contains(strings.ToLower(line), query) {
						results = append(results, fmt.Sprintf("%s:%d: %s", path, i+1, strings.TrimSpace(line)))
					}
				}
			}
			cbs.fileMu.RUnlock()
			if len(results) == 0 {
				return "No matches found.", nil
			}
			if len(results) > 50 {
				results = results[:50]
			}
			return strings.Join(results, "\n"), nil

		case "write_file":
			var args struct {
				FilePath string `json:"file_path"`
				Content  string `json:"content"`
			}
			if err := json.Unmarshal([]byte(arguments), &args); err != nil {
				return fmt.Sprintf("Error: invalid arguments: %s", err.Error()), nil
			}
			if args.FilePath == "" {
				return "Error: file_path is required", nil
			}
			// Block writes to template-managed files
			if templates.IsTemplateFile(args.FilePath) {
				return fmt.Sprintf("Error: %s is template-managed and cannot be written directly. Dependencies are auto-detected from imports.", args.FilePath), nil
			}

			content := utils.StripCodeFences(args.Content)
			_, fileType := guessFileType(args.FilePath)
			if fileType == "tsx" || fileType == "ts" {
				content, _ = stripForbiddenImports(content, args.FilePath)
			}

			action := "create"
			if repo.FileExists(args.FilePath) {
				if existingContent, readErr := repo.ReadFile(args.FilePath); readErr == nil {
					if normalizeContent(content) == normalizeContent(string(existingContent)) {
						return fmt.Sprintf("File %s is already up to date (no changes needed).", args.FilePath), nil
					}
				}
				action = "modify"
			}
			if writeErr := repo.WriteFile(args.FilePath, []byte(content)); writeErr != nil {
				return fmt.Sprintf("Error: failed to write file: %s", writeErr.Error()), nil
			}
			if cbs != nil {
				cbs.mu.Lock()
				cbs.fileMu.Lock()
				fileMap[args.FilePath] = content
				cbs.fileMu.Unlock()
				cbs.sendEvent("file_complete", responses.FileCompleteEvent{
					Action: action, FilePath: args.FilePath,
					FileType: fileType, Content: content,
				})
				*cbs.appliedOps = append(*cbs.appliedOps, appBuilderOperation{
					Action: action, Path: args.FilePath, Type: fileType,
				})
				cbs.pendingFiles[args.FilePath] = content
				cbs.mu.Unlock()
			} else {
				fileMap[args.FilePath] = content
			}
			return fmt.Sprintf("Successfully wrote %s (%d chars, action: %s). File is saved — do not re-write.", args.FilePath, len(content), action), nil

		case "write_files":
			var args struct {
				Files []struct {
					FilePath string `json:"file_path"`
					Content  string `json:"content"`
				} `json:"files"`
			}
			if err := json.Unmarshal([]byte(arguments), &args); err != nil {
				preview := arguments
				if len(preview) > 200 {
					preview = preview[:200]
				}
				slog.Warn("write_files: malformed arguments", "error", err, "args_preview", preview)

				// Fallback: LLM may have sent write_file format (file_path + content at top level)
				var single struct {
					FilePath string `json:"file_path"`
					Content  string `json:"content"`
				}
				if sErr := json.Unmarshal([]byte(arguments), &single); sErr == nil && single.FilePath != "" && single.Content != "" {
					slog.Info("write_files: recovered malformed args as single file", "path", single.FilePath)
					args.Files = append(args.Files, struct {
						FilePath string `json:"file_path"`
						Content  string `json:"content"`
					}{FilePath: single.FilePath, Content: single.Content})
				} else {
					return `Error: invalid arguments for write_files. The "files" field must be a JSON array. Expected: {"files": [{"file_path": "app/page.tsx", "content": "..."}]}. Parse error: ` + err.Error(), nil
				}
			}
			if len(args.Files) == 0 {
				return `Error: files array is empty. Expected: {"files": [{"file_path": "app/page.tsx", "content": "..."}]}`, nil
			}

			var results []string
			for _, f := range args.Files {
				if f.FilePath == "" {
					results = append(results, "SKIP: empty file_path")
					continue
				}
				// Block writes to template-managed files
				if templates.IsTemplateFile(f.FilePath) {
					results = append(results, fmt.Sprintf("SKIP %s: template-managed, dependencies are auto-detected from imports", f.FilePath))
					continue
				}
				content := utils.StripCodeFences(f.Content)
				_, fileType := guessFileType(f.FilePath)
				if fileType == "tsx" || fileType == "ts" {
					content, _ = stripForbiddenImports(content, f.FilePath)
				}

				action := "create"
				if repo.FileExists(f.FilePath) {
					if existingContent, readErr := repo.ReadFile(f.FilePath); readErr == nil {
						if normalizeContent(content) == normalizeContent(string(existingContent)) {
							results = append(results, fmt.Sprintf("OK %s (no changes)", f.FilePath))
							continue
						}
					}
					action = "modify"
				}
				if writeErr := repo.WriteFile(f.FilePath, []byte(content)); writeErr != nil {
					results = append(results, fmt.Sprintf("ERROR %s: %s", f.FilePath, writeErr.Error()))
					continue
				}
				if cbs != nil {
					cbs.mu.Lock()
					cbs.fileMu.Lock()
					fileMap[f.FilePath] = content
					cbs.fileMu.Unlock()
					cbs.sendEvent("file_complete", responses.FileCompleteEvent{
						Action: action, FilePath: f.FilePath,
						FileType: fileType, Content: content,
					})
					*cbs.appliedOps = append(*cbs.appliedOps, appBuilderOperation{
						Action: action, Path: f.FilePath, Type: fileType,
					})
					cbs.pendingFiles[f.FilePath] = content
					cbs.mu.Unlock()
				} else {
					fileMap[f.FilePath] = content
				}
				results = append(results, fmt.Sprintf("OK %s (%d chars, %s)", f.FilePath, len(content), action))
			}
			return fmt.Sprintf("Successfully wrote %d files (all saved to project):\n%s", len(args.Files), strings.Join(results, "\n")), nil

		case "edit_file":
			var args struct {
				FilePath  string `json:"file_path"`
				OldString string `json:"old_string"`
				NewString string `json:"new_string"`
			}
			if err := json.Unmarshal([]byte(arguments), &args); err != nil {
				return fmt.Sprintf("Error: invalid arguments: %s", err.Error()), nil
			}
			if args.FilePath == "" || args.OldString == "" {
				return "Error: file_path and old_string are required", nil
			}
			// Block edits to template-managed files
			if templates.IsTemplateFile(args.FilePath) {
				return fmt.Sprintf("Error: %s is template-managed and cannot be edited directly. Dependencies are auto-detected from imports.", args.FilePath), nil
			}

			cbs.fileMu.RLock()
			content, ok := fileMap[args.FilePath]
			cbs.fileMu.RUnlock()
			if !ok {
				return fmt.Sprintf("Error: file '%s' not found. Use write_file to create it.", args.FilePath), nil
			}

			count := strings.Count(content, args.OldString)
			if count == 0 {
				return fmt.Sprintf("Error: old_string not found in %s. Do NOT retry edit_file — use write_file to rewrite the entire file instead.", args.FilePath), nil
			}
			if count > 1 {
				return fmt.Sprintf("Error: old_string found %d times in %s. Use write_file to rewrite the entire file instead.", count, args.FilePath), nil
			}

			newContent := strings.Replace(content, args.OldString, args.NewString, 1)
			_, fileType := guessFileType(args.FilePath)
			if fileType == "tsx" || fileType == "ts" {
				newContent, _ = stripForbiddenImports(newContent, args.FilePath)
			}

			if writeErr := repo.WriteFile(args.FilePath, []byte(newContent)); writeErr != nil {
				return fmt.Sprintf("Error: failed to save: %s", writeErr.Error()), nil
			}
			if cbs != nil {
				cbs.mu.Lock()
				cbs.fileMu.Lock()
				fileMap[args.FilePath] = newContent
				cbs.fileMu.Unlock()
				cbs.sendEvent("file_complete", responses.FileCompleteEvent{
					Action: "modify", FilePath: args.FilePath,
					FileType: fileType,
					Content: newContent,
				})
				*cbs.appliedOps = append(*cbs.appliedOps, appBuilderOperation{
					Action: "modify", Path: args.FilePath, Type: fileType,
				})
				cbs.pendingFiles[args.FilePath] = newContent
				cbs.mu.Unlock()
			} else {
				fileMap[args.FilePath] = newContent
			}
			return fmt.Sprintf("Successfully edited %s", args.FilePath), nil

		case "delete_file":
			var args struct {
				FilePath string `json:"file_path"`
			}
			if err := json.Unmarshal([]byte(arguments), &args); err != nil {
				return fmt.Sprintf("Error: invalid arguments: %s", err.Error()), nil
			}
			// Block deletion of template-managed files
			if templates.IsTemplateFile(args.FilePath) {
				return fmt.Sprintf("Error: %s is template-managed and cannot be deleted.", args.FilePath), nil
			}
			if !repo.FileExists(args.FilePath) {
				return fmt.Sprintf("Error: file '%s' not found", args.FilePath), nil
			}
			if delErr := repo.DeleteFile(args.FilePath); delErr != nil {
				return fmt.Sprintf("Error: failed to delete file '%s': %s", args.FilePath, delErr.Error()), nil
			}

			if cbs != nil {
				cbs.mu.Lock()
				cbs.fileMu.Lock()
				delete(fileMap, args.FilePath)
				cbs.fileMu.Unlock()
				cbs.sendEvent("file_complete", responses.FileCompleteEvent{
					Action: "delete", FilePath: args.FilePath,
				})
				*cbs.appliedOps = append(*cbs.appliedOps, appBuilderOperation{
					Action: "delete", Path: args.FilePath,
				})
				delete(cbs.pendingFiles, args.FilePath)
				cbs.pendingDeletes = append(cbs.pendingDeletes, args.FilePath)
				cbs.mu.Unlock()
			} else {
				delete(fileMap, args.FilePath)
			}
			return fmt.Sprintf("Successfully deleted %s", args.FilePath), nil

		case "setup_ecommerce":
			var args struct {
				StoreName string `json:"store_name"`
			}
			if err := json.Unmarshal([]byte(arguments), &args); err != nil {
				return fmt.Sprintf("Error: invalid arguments: %s", err.Error()), nil
			}
			if args.StoreName == "" {
				args.StoreName = project.Name + " Store"
			}

			// Inject e-commerce template files
			needsAuth := false
			if settings := project.Settings; settings != nil {
				needsAuth, _ = settings["needs_auth"].(bool)
			}
			ecommLang := constants.DetectLanguage(project.Prompt)
			injectEcommerceTemplates := func() {
				for _, tp := range templates.EcommerceFilePaths {
					if _, exists := fileMap[tp]; exists {
						continue
					}
					var content string
					if tp == "components/ecommerce/providers.tsx" {
						content = templates.BuildEcommerceProviders(needsAuth)
					} else {
						content = templates.GetEcommerceContent(tp, ecommLang)
					}
					if content == "" {
						continue
					}
					if writeErr := repo.WriteFile(tp, []byte(content)); writeErr != nil {
						slog.Error("failed to write ecommerce template", "path", tp, "error", writeErr)
					}
					fileMap[tp] = content
				}
			}

			existingStore, err := c.DB.GetStoreByAppProjectID(project.ID.String())
			if err == nil && existingStore != nil {
				injectEcommerceTemplates()
				envVars := c.CollectAllEnvVars(project)
				if dotEnv := BuildDotEnvLocal(envVars); dotEnv != "" {
					c.injectTemplateFileForTool(repo, ".env.local", dotEnv, fileMap, cbs)
				}
				return fmt.Sprintf("E-commerce store already exists.\nStore ID: %s\nStore name: %s\nStorefront API: %s/v2/storefront/%s\nUse NEXT_PUBLIC_STORE_ID and NEXT_PUBLIC_WEMOB_API_URL env vars.\nlib/wemob-store.ts is available — import from \"@/lib/wemob-store\".\nNEXT STEP: Call manage_store to add products.",
					existingStore.ID.String(), existingStore.StoreName, constants.WEMOB_API_BASE_URL, existingStore.ID.String()), nil
			}

			if project.ProjectType != "ecommerce" {
				project.ProjectType = "ecommerce"
				c.DB.UpdateAppProject(project)
			}

			store, err := c.CreateStore(project, user, args.StoreName)
			if err != nil {
				return fmt.Sprintf("Failed to create store: %s", err.Error()), nil
			}

			// Emit visible event so the user sees store creation feedback
			if cbs != nil {
				cbs.sendEvent("store_created", map[string]any{
					"store_id":   store.ID.String(),
					"store_name": store.StoreName,
				})
			}

			injectEcommerceTemplates()
			envVars := c.CollectAllEnvVars(project)
			if dotEnv := BuildDotEnvLocal(envVars); dotEnv != "" {
				c.injectTemplateFileForTool(repo, ".env.local", dotEnv, fileMap, cbs)
			}
			return fmt.Sprintf("E-commerce store created!\nStore ID: %s\nStore name: %s\nStorefront API: %s/v2/storefront/%s\nEnv vars: NEXT_PUBLIC_STORE_ID=\"%s\", NEXT_PUBLIC_WEMOB_API_URL=\"%s\"\nlib/wemob-store.ts is available.\nNEXT STEP: Call manage_store to add products.",
				store.ID.String(), store.StoreName, constants.WEMOB_API_BASE_URL, store.ID.String(), store.ID.String(), constants.WEMOB_API_BASE_URL), nil

		case "manage_store":
			var args struct {
				Operations []struct {
					Action string         `json:"action"`
					Data   map[string]any `json:"data"`
				} `json:"operations"`
			}
			if err := json.Unmarshal([]byte(arguments), &args); err != nil {
				return fmt.Sprintf("Failed to parse arguments: %s", err.Error()), nil
			}

			store, err := c.DB.GetStoreByAppProjectID(project.ID.String())
			if err != nil || store == nil {
				return "Error: No store found. Call setup_ecommerce first.", nil
			}

			var results []string
			var errCount int
			for _, op := range args.Operations {
				opData, err := c.executeStoreOperation(store, op.Action, op.Data)
				if err != nil {
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

			return fmt.Sprintf("Executed %d operations (%d succeeded, %d failed):\n%s",
				len(args.Operations), len(args.Operations)-errCount, errCount, strings.Join(results, "\n")), nil

		case "setup_database":
			var args struct {
				Tables    []models.DataModel `json:"tables"`
				NeedsAuth bool               `json:"needs_auth"`
			}
			if err := json.Unmarshal([]byte(arguments), &args); err != nil {
				return fmt.Sprintf("Error: invalid arguments: %s", err.Error()), nil
			}

			dataModels := args.Tables

			// Merge default ecommerce models if this is an ecommerce project
			if project.ProjectType == "ecommerce" {
				var defaultModels []models.DataModel
				if err := json.Unmarshal([]byte(constants.ECOMMERCE_DEFAULT_DATA_MODELS), &defaultModels); err == nil {
					existing := make(map[string]bool)
					for _, m := range dataModels {
						existing[m.Name] = true
					}
					for _, dm := range defaultModels {
						if !existing[dm.Name] {
							dataModels = append([]models.DataModel{dm}, dataModels...)
						}
					}
				}
			}

			// Merge auth models if needed
			if args.NeedsAuth {
				var authModels []models.DataModel
				if err := json.Unmarshal([]byte(constants.AUTH_DEFAULT_DATA_MODELS), &authModels); err == nil {
					for _, authModel := range authModels {
						found := false
						for i, m := range dataModels {
							if m.Name == authModel.Name {
								dataModels[i] = models.MergeDataModelColumns(authModel, m)
								found = true
								break
							}
						}
						if !found {
							dataModels = append([]models.DataModel{authModel}, dataModels...)
						}
					}
				}
				if project.Settings == nil {
					project.Settings = make(map[string]any)
				}
				project.Settings["needs_auth"] = true
			}

			// Store data models in project
			dmJSON, err := models.MarshalDataModels(dataModels)
			if err != nil {
				return fmt.Sprintf("Error: failed to marshal data models: %s", err.Error()), nil
			}
			project.DataModels = dmJSON
			if updated, err := c.DB.UpdateAppProject(project); err != nil {
				slog.Error("failed to save data models to project", "error", err, "project_id", project.ID)
			} else {
				project = updated
			}

			// Provision Neon database
			if c.NeonService == nil {
				return "Error: Database provisioning service not available", nil
			}

			if cbs != nil {
				cbs.sendEvent("neon_provision", map[string]any{"phase": "start"})
			}

			connStr, neonProjectID, neonErr := c.provisionNeonDatabase(ctx, project, dataModels, userLimits, func(eventName string, data any) bool {
				if cbs != nil {
					return cbs.sendEvent(eventName, data)
				}
				return true
			})

			if connStr == "" || neonProjectID == "" || neonErr != nil {
				errMsg := "unknown error"
				if neonErr != nil {
					errMsg = neonErr.Error()
				}
				return fmt.Sprintf("Error: Database provisioning failed: %s", errMsg), nil
			}

			if err := c.storeNeonIntegration(project, connStr, neonProjectID); err != nil {
				return fmt.Sprintf("Error: Failed to store database integration: %s", err.Error()), nil
			}

			// Inject DB template files
			dbContent := templates.GetTemplate("lib/db.ts")
			c.injectTemplateFileForTool(repo, "lib/db.ts", dbContent, fileMap, cbs)

			schemaContent := database.BuildDrizzleSchema(dataModels)
			c.injectTemplateFileForTool(repo, "lib/schema.ts", schemaContent, fileMap, cbs)

			// Generate AUTH_SECRET env var if auth is needed
			if args.NeedsAuth {
				authSecret, err := utils.RandomHex(32)
				if err == nil {
					envVar := &models.AppEnvVar{
						AppProjectID: project.ID,
						Key:          "AUTH_SECRET",
						Value:        authSecret,
						IsPublic:     false,
					}
					c.DB.CreateAppEnvVar(envVar)
				}
			}

			// Update .env.local with new vars
			envVars := c.CollectAllEnvVars(project)
			if dotEnv := BuildDotEnvLocal(envVars); dotEnv != "" {
				c.injectTemplateFileForTool(repo, ".env.local", dotEnv, fileMap, cbs)
			}

			// Seed the database
			seedResult, seedErr := c.seedNeonDatabase(ctx, project, dataModels, func(eventName string, data any) bool {
				if cbs != nil {
					return cbs.sendEvent(eventName, data)
				}
				return true
			})
			if seedErr != nil {
				slog.Error("database seeding failed, tables will be empty", "error", seedErr, "project_id", project.ID)
			} else if project.ProjectType != "ecommerce" {
				// Generate AI images for seed data image columns and store in S3/DB
				c.generateAndStoreSeedImages(ctx, project, dataModels, func(eventName string, data any) bool {
					if cbs != nil {
						return cbs.sendEvent(eventName, data)
					}
					return true
				}, 10)
			}
			_ = seedResult

			if cbs != nil {
				cbs.sendEvent("neon_provision", map[string]any{"phase": "ready"})
			}

			var result strings.Builder
			result.WriteString("Database provisioned successfully!\n")
			result.WriteString(fmt.Sprintf("Tables created: %d\n", len(dataModels)))
			for _, dm := range dataModels {
				result.WriteString(fmt.Sprintf("  - %s (%d columns)\n", dm.Name, len(dm.Columns)))
			}
			if seedErr == nil {
				result.WriteString("\nREALISTIC DEMO DATA HAS BEEN INSERTED into all tables. The database is NOT empty — it contains sample rows. Do NOT create seed buttons, seed routes, or tell the user to load demo data. The app should display this data directly from the database.\n")
			} else {
				result.WriteString("\nWARNING: Demo data seeding failed. Generate code that shows graceful empty states when no data exists. Do NOT create seed routes or seed buttons — the platform will handle data seeding separately.\n")
			}
			result.WriteString("lib/db.ts and lib/schema.ts have been generated.\n")
			result.WriteString("Use: import { db } from '@/lib/db' and import { tableName } from '@/lib/schema'\n")
			result.WriteString("Use Drizzle ORM for queries: db.select().from(tableName)\n")
			if args.NeedsAuth {
				result.WriteString("\nAuth enabled:\n")
				result.WriteString("- AUTH_SECRET env var generated\n")
				result.WriteString("- users table with id, email, password_hash, name, role, etc.\n")
				result.WriteString("- You MUST generate: lib/wemob-auth.ts (server-side), lib/wemob-auth-client.tsx (client-side), app/api/auth/[...action]/route.ts\n")
			}
			return result.String(), nil

		case "configure_mobile_app":
			var args struct {
				BridgeMethods []string `json:"bridge_methods"`
				Permissions   []string `json:"permissions"`
			}
			if err := json.Unmarshal([]byte(arguments), &args); err != nil {
				return fmt.Sprintf("Error: invalid arguments: %s", err.Error()), nil
			}

			// Validate bridge methods
			validMethods := map[string]bool{
				"getDeviceInfo": true, "share": true, "haptic": true,
				"openExternal": true, "getConnectionStatus": true,
				"storage.get": true, "storage.set": true, "storage.remove": true,
				"camera.takePhoto": true, "camera.pickImage": true, "camera.pickVideo": true,
				"contacts.getAll": true, "contacts.pick": true,
				"calendar.addEvent": true, "files.pick": true,
			}
			var filteredMethods []string
			for _, m := range args.BridgeMethods {
				if validMethods[m] {
					filteredMethods = append(filteredMethods, m)
				}
			}

			// Auto-derive permissions from bridge methods
			methodToPermission := map[string]string{
				"camera.takePhoto": "camera", "camera.pickImage": "camera", "camera.pickVideo": "camera",
				"contacts.getAll": "contacts", "contacts.pick": "contacts",
				"calendar.addEvent": "calendar",
				"files.pick":        "storage",
			}
			permSet := map[string]bool{"notifications": true} // always include notifications
			for _, p := range args.Permissions {
				permSet[p] = true
			}
			for _, m := range filteredMethods {
				if p, ok := methodToPermission[m]; ok {
					permSet[p] = true
				}
			}
			var permissions []string
			for p := range permSet {
				permissions = append(permissions, p)
			}

			// Get or create config
			mobileConfig, configErr := c.DB.GetMobileAppConfig(project.ID.String())
			if configErr != nil {
				mobileConfig = &models.MobileAppConfig{
					ProjectID:            project.ID,
					PushEnabled:          true,
					BridgeEnabled:        true,
					AnalyticsEnabled:     true,
					PullToRefreshEnabled: true,
					ExternalBrowserLinks: true,
				}
			}
			mobileConfig.BridgeEnabled = true
			mobileConfig.PushEnabled = true
			mobileConfig.BridgeMethods = filteredMethods
			mobileConfig.Permissions = permissions

			if _, upsertErr := c.DB.UpsertMobileAppConfig(mobileConfig); upsertErr != nil {
				return fmt.Sprintf("Error: failed to save config: %s", upsertErr.Error()), nil
			}

			// Notify dashboard in real time
			if cbs != nil {
				cbs.sendEvent("mobile_config_updated", map[string]any{
					"bridge_methods": filteredMethods,
					"permissions":    permissions,
				})
			}

			return fmt.Sprintf("Mobile app configured. Bridge methods: %v. Permissions: %v.", filteredMethods, permissions), nil

		default:
			return fmt.Sprintf("Unknown tool: %s", name), nil
		}
	}
}

// injectTemplateFileForTool is like injectTemplateFile but uses builderToolCallbacks.
func (c *Controller) injectTemplateFileForTool(repo *gitrepo.Repo, tp string, content string, fileMap map[string]string, cbs *builderToolCallbacks) {
	fileType := "ts"
	switch {
	case strings.HasSuffix(tp, ".tsx"):
		fileType = "tsx"
	case strings.HasSuffix(tp, ".css"):
		fileType = "css"
	case strings.HasSuffix(tp, ".json"):
		fileType = "json"
	case strings.HasSuffix(tp, ".mjs"):
		fileType = "mjs"
	case tp == ".env.local":
		fileType = "env"
	}

	if writeErr := repo.WriteFile(tp, []byte(content)); writeErr != nil {
		slog.Error("failed to save template file via tool", "path", tp, "error", writeErr)
		return
	}
	if cbs != nil {
		cbs.mu.Lock()
		cbs.fileMu.Lock()
		fileMap[tp] = content
		cbs.fileMu.Unlock()
		cbs.sendEvent("file_complete", responses.FileCompleteEvent{
			Action: "create", FilePath: tp,
			FileType: fileType,
			Content: content,
		})
		*cbs.appliedOps = append(*cbs.appliedOps, appBuilderOperation{
			Action: "create", Path: tp, Type: fileType,
		})
		cbs.pendingFiles[tp] = content
		cbs.mu.Unlock()
	} else {
		fileMap[tp] = content
	}
}

// autoDetectMobileBridgeConfig scans generated files for WemobBridge method usage
// and automatically configures the mobile app config (bridge_methods + permissions).
// This replaces the configure_mobile_app tool call for paths that don't support tools
// (single-shot generation and chat Phase 2 file-block streaming).
func (c *Controller) autoDetectMobileBridgeConfig(project *models.AppProject, fileMap map[string]string, sendEvent func(string, any) bool) {
	if project.ProjectType != "mobile_app" {
		return
	}

	// Scan all files for WemobBridge method usage
	methodPatterns := map[string]*regexp.Regexp{
		"getDeviceInfo":       regexp.MustCompile(`WemobBridge\.getDeviceInfo`),
		"share":               regexp.MustCompile(`WemobBridge\.share`),
		"haptic":              regexp.MustCompile(`WemobBridge\.haptic`),
		"openExternal":        regexp.MustCompile(`WemobBridge\.openExternal`),
		"getConnectionStatus": regexp.MustCompile(`WemobBridge\.getConnectionStatus`),
		"storage.get":         regexp.MustCompile(`WemobBridge\.storage\.get`),
		"storage.set":         regexp.MustCompile(`WemobBridge\.storage\.set`),
		"storage.remove":      regexp.MustCompile(`WemobBridge\.storage\.remove`),
		"camera.takePhoto":    regexp.MustCompile(`WemobBridge\.camera\.takePhoto`),
		"camera.pickImage":    regexp.MustCompile(`WemobBridge\.camera\.pickImage`),
		"camera.pickVideo":    regexp.MustCompile(`WemobBridge\.camera\.pickVideo`),
		"contacts.getAll":     regexp.MustCompile(`WemobBridge\.contacts\.getAll`),
		"contacts.pick":       regexp.MustCompile(`WemobBridge\.contacts\.pick`),
		"calendar.addEvent":   regexp.MustCompile(`WemobBridge\.calendar\.addEvent`),
		"files.pick":          regexp.MustCompile(`WemobBridge\.files\.pick`),
	}

	detectedMethods := make(map[string]bool)
	for _, content := range fileMap {
		for method, pattern := range methodPatterns {
			if pattern.MatchString(content) {
				detectedMethods[method] = true
			}
		}
	}

	if len(detectedMethods) == 0 {
		return
	}

	var filteredMethods []string
	for m := range detectedMethods {
		filteredMethods = append(filteredMethods, m)
	}
	sort.Strings(filteredMethods)

	// Auto-derive permissions from detected bridge methods
	methodToPermission := map[string]string{
		"camera.takePhoto": "camera", "camera.pickImage": "camera", "camera.pickVideo": "camera",
		"contacts.getAll": "contacts", "contacts.pick": "contacts",
		"calendar.addEvent": "calendar",
		"files.pick":        "storage",
	}
	permSet := map[string]bool{"notifications": true}
	for _, m := range filteredMethods {
		if p, ok := methodToPermission[m]; ok {
			permSet[p] = true
		}
	}
	var permissions []string
	for p := range permSet {
		permissions = append(permissions, p)
	}
	sort.Strings(permissions)

	// Get or create config
	mobileConfig, configErr := c.DB.GetMobileAppConfig(project.ID.String())
	if configErr != nil {
		mobileConfig = &models.MobileAppConfig{
			ProjectID:            project.ID,
			PushEnabled:          true,
			BridgeEnabled:        true,
			AnalyticsEnabled:     true,
			PullToRefreshEnabled: true,
			ExternalBrowserLinks: true,
		}
	}
	mobileConfig.BridgeEnabled = true
	mobileConfig.PushEnabled = true
	mobileConfig.BridgeMethods = filteredMethods
	mobileConfig.Permissions = permissions

	if _, upsertErr := c.DB.UpsertMobileAppConfig(mobileConfig); upsertErr != nil {
		slog.Error("auto-detect: failed to save mobile config", "error", upsertErr, "project_id", project.ID)
		return
	}

	slog.Info("auto-detect: mobile bridge config updated",
		"project_id", project.ID,
		"bridge_methods", filteredMethods,
		"permissions", permissions,
	)

	sendEvent("mobile_config_updated", map[string]any{
		"bridge_methods": filteredMethods,
		"permissions":    permissions,
	})
}

// compositeToolExecutor wraps a local ToolExecutor and routes MCP-prefixed tool calls
// to the MCP service, while passing everything else to the local executor.
func (c *Controller) compositeToolExecutor(local utils.ToolExecutor) utils.ToolExecutor {
	return func(name string, arguments string) (string, error) {
		mcp := c.MCPService.Load()
		if mcp != nil && mcp.IsMCPTool(name) {
			return mcp.ToolExecutor()(name, arguments)
		}
		result, err := local(name, arguments)
		if err != nil {
			return result, err
		}
		if strings.HasPrefix(result, "Unknown tool:") && mcp != nil {
			if mcpResult, mcpErr := mcp.ToolExecutor()(name, arguments); mcpErr == nil && !strings.HasPrefix(mcpResult, "Unknown MCP tool:") {
				return mcpResult, nil
			}
		}
		return result, nil
	}
}

// mcpTools returns MCP tools if the service is available, or nil.
func (c *Controller) mcpTools() []utils.Tool {
	mcp := c.MCPService.Load()
	if mcp != nil && mcp.HasTools() {
		return mcp.Tools()
	}
	return nil
}
