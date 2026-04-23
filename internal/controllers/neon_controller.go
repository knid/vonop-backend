package controllers

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/services/fal"
	"github.com/knid/wemob/internal/services/neon"
	"github.com/knid/wemob/internal/starters"
	"github.com/knid/wemob/internal/utils"
)

// provisionNeonDatabase creates a Neon project, runs the schema SQL, and returns the connection string and project ID.
// If userLimits is non-nil, compute and storage limits are applied to the Neon project.
func (c *Controller) provisionNeonDatabase(ctx context.Context, project *models.AppProject, dataModels []models.DataModel, userLimits *models.StandartUserLimits, sendEvent func(string, any) bool) (string, string, error) {
	if c.NeonService == nil {
		return "", "", fmt.Errorf("Neon service not configured")
	}

	sendEvent("neon_provision", map[string]any{"phase": "creating_project"})

	projectName := fmt.Sprintf("wemob-%s", project.Slug)
	if len(projectName) > 60 {
		projectName = projectName[:60]
	}

	// Build DB limits from the user's plan
	var dbLimits *neon.DBLimits
	if userLimits != nil && (userLimits.DeployDBMaxCU != "" || userLimits.DeployDBStorageMB > 0) {
		maxCU := 0.25
		if userLimits.DeployDBMaxCU != "" {
			if parsed, err := strconv.ParseFloat(userLimits.DeployDBMaxCU, 64); err == nil {
				maxCU = parsed
			}
		}
		dbLimits = &neon.DBLimits{
			MaxCU:     maxCU,
			StorageMB: userLimits.DeployDBStorageMB,
		}
	}

	neonProjectID, connStr, err := c.NeonService.CreateProject(projectName, "", dbLimits)
	if err != nil {
		return "", "", fmt.Errorf("create Neon project: %w", err)
	}

	slog.Info("Neon project created", "neon_project_id", neonProjectID, "conn_str_len", len(connStr), "app_project_id", project.ID)

	// Build and execute schema SQL directly via the connection string.
	schemaSQL := database.BuildCreateTableSQL(dataModels)
	if schemaSQL != "" {
		sendEvent("neon_provision", map[string]any{"phase": "creating_schema"})
		slog.Info("Executing schema SQL on Neon", "neon_project_id", neonProjectID, "sql_length", len(schemaSQL), "sql", schemaSQL)

		neonDB, err := sql.Open("postgres", database.NeonConnStrWithEndpoint(connStr))
		if err != nil {
			slog.Error("Failed to open Neon database driver", "error", err, "neon_project_id", neonProjectID)
			return connStr, neonProjectID, fmt.Errorf("open Neon driver: %w", err)
		}
		defer neonDB.Close()

		// Neon compute needs time to wake up after project creation.
		// Retry ping up to 5 times with increasing delay.
		var pingErr error
		for attempt := 1; attempt <= 5; attempt++ {
			sendEvent("progress", responses.ProgressEvent{
				Phase:   "provisioning_db",
				Message: fmt.Sprintf("Connecting to database (attempt %d/5)...", attempt),
				Current: attempt,
				Total:   5,
			})
			pingCtx, pingCancel := context.WithTimeout(ctx, 10*time.Second)
			pingErr = neonDB.PingContext(pingCtx)
			pingCancel()
			if pingErr == nil {
				slog.Info("Neon database ping succeeded", "attempt", attempt, "neon_project_id", neonProjectID)
				break
			}
			slog.Warn("Neon database not ready, retrying...", "attempt", attempt, "error", pingErr, "neon_project_id", neonProjectID)
			time.Sleep(time.Duration(attempt*2) * time.Second)
		}
		if pingErr != nil {
			slog.Error("Neon database not reachable after retries", "error", pingErr, "neon_project_id", neonProjectID)
			return connStr, neonProjectID, fmt.Errorf("neon database not reachable: %w", pingErr)
		}

		execCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		if _, err := neonDB.ExecContext(execCtx, schemaSQL); err != nil {
			slog.Error("Failed to execute schema SQL on Neon", "error", err, "neon_project_id", neonProjectID, "sql", schemaSQL)
			return connStr, neonProjectID, fmt.Errorf("execute schema SQL: %w", err)
		}
		slog.Info("Neon schema created successfully", "neon_project_id", neonProjectID, "tables", len(dataModels))
	}

	return connStr, neonProjectID, nil
}

// storeNeonIntegration saves the Neon connection as an AppIntegration (upsert).
func (c *Controller) storeNeonIntegration(project *models.AppProject, connStr, neonProjectID string) error {
	// Check if a Neon integration already exists — update it instead of creating a duplicate
	existing, err := c.DB.GetAppIntegrationByProvider(project.ID.String(), "neon")
	if err == nil && existing.ID != uuid.Nil {
		existing.Config = map[string]any{
			"project_id": neonProjectID,
		}
		existing.Status = "connected"
		existing.EnvVars = map[string]string{
			"DATABASE_URL": connStr,
		}
		existing.UpdatedAt = time.Now()
		_, err := c.DB.UpdateAppIntegration(existing)
		return err
	}

	integration := &models.AppIntegration{
		AppProjectID: project.ID,
		Provider:     "neon",
		Config: map[string]any{
			"project_id": neonProjectID,
		},
		Status: "connected",
		EnvVars: map[string]string{
			"DATABASE_URL": connStr,
		},
	}

	_, err = c.DB.CreateAppIntegration(integration)
	return err
}

// seedNeonDatabase uses an LLM to generate seed SQL and executes it on the app's Neon database.
func (c *Controller) seedNeonDatabase(ctx context.Context, project *models.AppProject, dataModels []models.DataModel, sendEvent func(string, any) bool) (string, error) {
	// Get connection string from the stored integration
	connStr := c.neonConnString(project.ID.String())
	if connStr == "" {
		return "", fmt.Errorf("no Neon connection string found for project")
	}

	// Build schema description for the LLM
	var schemaDesc strings.Builder
	for _, m := range dataModels {
		schemaDesc.WriteString(fmt.Sprintf("Table: %s\n", m.Name))
		for _, col := range m.Columns {
			schemaDesc.WriteString(fmt.Sprintf("  - %s %s", col.Name, col.Type))
			if col.PrimaryKey {
				schemaDesc.WriteString(" PRIMARY KEY")
			}
			if !col.Nullable && !col.PrimaryKey {
				schemaDesc.WriteString(" NOT NULL")
			}
			if col.Default != "" {
				schemaDesc.WriteString(fmt.Sprintf(" DEFAULT %s", col.Default))
			}
			if col.References != "" {
				schemaDesc.WriteString(fmt.Sprintf(" REFERENCES %s", col.References))
			}
			schemaDesc.WriteString("\n")
		}
		schemaDesc.WriteString("\n")
	}

	seedPrompt := fmt.Sprintf(`Generate realistic seed data SQL INSERT statements for a database with the following schema:

%s

The app description: %s

Rules:
1. Generate 3-5 rows of realistic data for each table. Keep it concise — this is demo data for a preview environment.
2. Use gen_random_uuid() for UUID primary keys.
3. CRITICAL: Insert tables in dependency order — parent tables with referenced primary keys MUST be inserted BEFORE child tables that reference them via foreign keys.
4. Use NOW() for timestamp columns.
5. Make the data contextually relevant to the app description.
6. For JSONB columns, use valid JSON strings.
7. Return ONLY valid SQL INSERT statements, no explanations.
8. Use single-line INSERT statements (one per row).
9. Do NOT include CREATE TABLE or DROP statements.
10. Do NOT wrap the output in markdown code fences. Return raw SQL only.
11. CRITICAL: For image/photo columns (cover_image, image_url, avatar_url, thumbnail, photo, banner, logo, icon, etc.), set them to NULL. Images will be generated separately by the platform. Do NOT use placeholder URLs or fake image paths.
12. CRITICAL: Escape all single quotes inside string values by doubling them (e.g., 'Türkiye''nin' not 'Türkiye'nin'). This is standard PostgreSQL escaping and prevents syntax errors.
13. CRITICAL: All text content (names, titles, descriptions, labels, etc.) MUST be in the SAME LANGUAGE as the app description above. If the app description is in English, all seed data text must be in English. If in Turkish, all seed data text must be in Turkish. Match the user's language exactly.`, schemaDesc.String(), utils.SanitizePromptContent(project.Prompt))

	sendEvent("neon_provision", map[string]any{"phase": "seeding_data"})

	provider := c.llmProvider()
	model := provider.FastModel()

	seedSQL, err := utils.GetTextFromLLM(ctx, model, "You are a SQL expert. Generate realistic seed data. Return ONLY valid SQL.", seedPrompt, provider, 8192)
	if err != nil {
		return "", fmt.Errorf("LLM seed generation: %w", err)
	}

	// Clean the SQL (strip markdown fences if present)
	seedSQL = strings.TrimSpace(seedSQL)
	seedSQL = strings.TrimPrefix(seedSQL, "```sql")
	seedSQL = strings.TrimPrefix(seedSQL, "```")
	seedSQL = strings.TrimSuffix(seedSQL, "```")
	seedSQL = strings.TrimSpace(seedSQL)

	if seedSQL == "" {
		return "", fmt.Errorf("LLM returned empty seed SQL")
	}

	// Fix unescaped single quotes inside SQL string literals.
	// LLMs often forget to double-escape quotes in non-English text
	// (e.g., Turkish: Türkiye'nin → should be Türkiye''nin).
	seedSQL = database.FixUnescapedSQLQuotes(seedSQL)

	// Validate that the seed SQL only contains INSERT statements
	if err := database.ValidateSeedSQL(seedSQL); err != nil {
		slog.Warn("LLM generated invalid seed SQL", "error", err, "project_id", project.ID)
		return "", fmt.Errorf("invalid seed SQL: %w", err)
	}

	neonDB, err := sql.Open("postgres", database.NeonConnStrWithEndpoint(connStr))
	if err != nil {
		return "", fmt.Errorf("connect to Neon for seeding: %w", err)
	}
	defer neonDB.Close()

	execCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if _, err := neonDB.ExecContext(execCtx, seedSQL); err != nil {
		slog.Warn("Failed to seed Neon database", "error", err, "project_id", project.ID)
		return "", fmt.Errorf("execute seed SQL: %w", err)
	}
	slog.Info("Neon database seeded", "project_id", project.ID)

	return "Seeded database with sample data.", nil
}

// imageColumnNames are column names that typically hold image URLs.
var imageColumnNames = map[string]bool{
	"cover_image": true, "image": true, "image_url": true,
	"avatar_url": true, "thumbnail": true, "thumbnail_url": true,
	"photo": true, "photo_url": true, "banner": true, "banner_url": true,
	"logo": true, "logo_url": true, "icon": true, "icon_url": true,
}

// generateAndStoreSeedImages scans the data models for image columns, queries
// the seeded database for rows with NULL image values, generates AI images
// via fal.ai, uploads them to S3, and UPDATEs the rows with presigned URLs.
// Best-effort: failures are logged but don't block generation.
func (c *Controller) generateAndStoreSeedImages(
	ctx context.Context,
	project *models.AppProject,
	dataModels []models.DataModel,
	sendEvent func(string, any) bool,
	maxImages int,
) {
	apiKey := c.SecretManager.GetSecret("FAL_API_KEY")
	if apiKey == "" {
		slog.Debug("generateAndStoreSeedImages: FAL_API_KEY not set, skipping")
		return
	}

	falModel := c.SecretManager.GetSecret("FAL_IMAGE_MODEL")
	if falModel == "" {
		falModel = "fal-ai/flux/schnell"
	}

	// Find tables with image columns
	type imageCol struct {
		table  string
		column string
	}
	var imageCols []imageCol
	for _, m := range dataModels {
		for _, col := range m.Columns {
			if imageColumnNames[col.Name] && col.Type == "text" {
				imageCols = append(imageCols, imageCol{table: m.Name, column: col.Name})
			}
		}
	}

	if len(imageCols) == 0 {
		return
	}

	connStr := c.neonConnString(project.ID.String())
	if connStr == "" {
		return
	}

	neonDB, err := sql.Open("postgres", database.NeonConnStrWithEndpoint(connStr))
	if err != nil {
		slog.Warn("generateAndStoreSeedImages: failed to connect", "error", err)
		return
	}
	defer neonDB.Close()

	sendEvent("tool_start", map[string]any{"tool": "generate_seed_images"})
	sendEvent("progress", responses.ProgressEvent{
		Phase:   "generating_images",
		Message: "Preparing seed images for your database...",
	})

	// Collect rows that need images
	type imageTask struct {
		table  string
		column string
		rowID  string
		prompt string
	}
	var tasks []imageTask

	// Get a descriptive name for the row to build a prompt
	for _, ic := range imageCols {
		// Find the "name" or "title" column for building image prompts
		var titleCol string
		for _, m := range dataModels {
			if m.Name == ic.table {
				for _, col := range m.Columns {
					if col.Name == "title" || col.Name == "name" || col.Name == "product_name" {
						titleCol = col.Name
						break
					}
				}
				break
			}
		}

		// Query for rows with NULL image column, up to maxImages
		query := fmt.Sprintf(
			"SELECT %s%s FROM %s WHERE %s IS NULL LIMIT %d",
			quoteIdentifier("id"),
			func() string {
				if titleCol != "" {
					return ", " + quoteIdentifier(titleCol)
				}
				return ""
			}(),
			quoteIdentifier(ic.table), quoteIdentifier(ic.column), maxImages-len(tasks),
		)

		rows, err := neonDB.QueryContext(ctx, query)
		if err != nil {
			slog.Warn("generateAndStoreSeedImages: query failed", "table", ic.table, "error", err)
			continue
		}

		for rows.Next() {
			var rowID string
			var title sql.NullString

			if titleCol != "" {
				if err := rows.Scan(&rowID, &title); err != nil {
					continue
				}
			} else {
				if err := rows.Scan(&rowID); err != nil {
					continue
				}
			}

			prompt := fmt.Sprintf("Professional, high-quality photo for %s", ic.table)
			if title.Valid && title.String != "" {
				prompt = fmt.Sprintf("%s — professional, high-quality photo, clean composition, editorial quality, suitable for a modern web application.", title.String)
			}

			tasks = append(tasks, imageTask{
				table:  ic.table,
				column: ic.column,
				rowID:  rowID,
				prompt: prompt,
			})

			if len(tasks) >= maxImages {
				break
			}
		}
		rows.Close()

		if len(tasks) >= maxImages {
			break
		}
	}

	if len(tasks) == 0 {
		return
	}

	slog.Info("generateAndStoreSeedImages: generating images",
		"count", len(tasks), "project_id", project.ID)

	// Generate images sequentially to avoid fal.ai rate limits
	totalImages := len(tasks)
	updated := 0

	for i, task := range tasks {
		sendEvent("progress", responses.ProgressEvent{
			Phase:   "generating_images",
			Message: fmt.Sprintf("Generating image %d of %d", i+1, totalImages),
			Current: i + 1,
			Total:   totalImages,
		})

		// Retry up to 3 times with backoff
		var cdnURL string
		var genErr error
		for attempt := 0; attempt < 3; attempt++ {
			cdnURL, genErr = fal.GenerateImage(ctx, apiKey, falModel, task.prompt, "")
			if genErr == nil {
				break
			}
			slog.Warn("generateAndStoreSeedImages: fal generation failed, retrying",
				"table", task.table, "attempt", attempt+1, "error", genErr)
			time.Sleep(time.Duration(attempt+1) * 2 * time.Second)
		}
		if genErr != nil {
			slog.Warn("generateAndStoreSeedImages: fal generation failed after retries",
				"table", task.table, "error", genErr)
			continue
		}

		imgData, err := downloadImage(ctx, cdnURL)
		if err != nil {
			slog.Warn("generateAndStoreSeedImages: download failed",
				"table", task.table, "error", err)
			continue
		}

		assetPath := fmt.Sprintf("images/seed-%s-%d.jpg", task.table, i+1)
		s3Key := fmt.Sprintf("apps/%s/%s", project.ID, assetPath)
		uploadCtx, uploadCancel := context.WithTimeout(context.Background(), 30*time.Second)
		var uploadErr error
		for attempt := 0; attempt < 3; attempt++ {
			uploadErr = c.AssetsStorage.UploadFileByteWithContentType(uploadCtx, s3Key, imgData, "image/jpeg")
			if uploadErr == nil {
				break
			}
			slog.Warn("generateAndStoreSeedImages: S3 upload failed, retrying",
				"table", task.table, "attempt", attempt+1, "error", uploadErr)
			time.Sleep(time.Duration(attempt+1) * time.Second)
		}
		uploadCancel()
		if uploadErr != nil {
			continue
		}

		// Point the app at the /v2/apps/{id}/file/* proxy instead of the raw
		// S3 URL — the assets bucket is private, so direct URLs 403. The proxy
		// generates a fresh presigned URL on each request and returns a 302
		// redirect, so the stored value never expires.
		proxyURL := fmt.Sprintf("%s/v2/apps/%s/file/%s", constants.WEMOB_API_BASE_URL, project.ID, assetPath)

		// Update the database row immediately
		updateQuery := fmt.Sprintf(
			"UPDATE %s SET %s = $1 WHERE %s = $2",
			quoteIdentifier(task.table), quoteIdentifier(task.column), quoteIdentifier("id"),
		)
		if _, err := neonDB.ExecContext(ctx, updateQuery, proxyURL, task.rowID); err != nil {
			slog.Warn("generateAndStoreSeedImages: update failed",
				"table", task.table, "row_id", task.rowID, "error", err)
			continue
		}
		updated++
	}

	slog.Info("generateAndStoreSeedImages: complete",
		"project_id", project.ID,
		"total", totalImages,
		"updated", updated)
}

// provisionDatabaseForAgent is the agent-callable entry point for creating a
// project's wemob db on demand. It is invoked from the ProvisionDatabaseTool
// in internal/agent via the RunDependencies.ProvisionDatabase callback wired
// in AgentDeps. The chain:
//
//  1. Reject if the project already has a Neon integration (defense in depth;
//     the tool layer also checks).
//  2. Resolve the user's plan limits so Neon compute/storage is sized.
//  3. provisionNeonDatabase: create the Neon project + run CREATE TABLE.
//  4. storeNeonIntegration: persist the connection string as an AppIntegration.
//  5. Persist the supplied schema on project.DataModels so future runs and
//     the dashboard see the same shape.
//  6. Flip project.Settings["has_database"] = true so subsequent runs pick up
//     the DatabaseAddendum from BuildSystemMessages.
//  7. Best-effort seed images for any image columns (matches the template
//     copy flow so users see a fully populated DB on the first run).
//
// Errors are returned verbatim; the tool surfaces them to the model.
func (c *Controller) provisionDatabaseForAgent(ctx context.Context, project *models.AppProject, user *models.User, tables []models.DataModel) error {
	if c.NeonService == nil {
		return fmt.Errorf("wemob db is not configured on this environment")
	}
	if c.isNeonBacked(project.ID.String()) {
		return fmt.Errorf("project already has a wemob db connected")
	}
	if len(tables) == 0 {
		return fmt.Errorf("at least one table is required")
	}

	var userLimits *models.StandartUserLimits
	if user != nil {
		if limits, err := c.GetUserLimits(user); err == nil {
			userLimits = limits
		} else {
			slog.Warn("provisionDatabaseForAgent: failed to load user limits", "error", err, "user_id", user.ID)
		}
	}

	noopEvent := func(string, any) bool { return false }
	connStr, neonProjectID, err := c.provisionNeonDatabase(ctx, project, tables, userLimits, noopEvent)
	if err != nil {
		return fmt.Errorf("provision wemob db: %w", err)
	}

	if err := c.storeNeonIntegration(project, connStr, neonProjectID); err != nil {
		return fmt.Errorf("store wemob db integration: %w", err)
	}

	if err := c.seedStarterSchema(ctx, connStr, project); err != nil {
		// Mark the integration as errored so the agent can re-run provisioning;
		// otherwise isNeonBacked would keep rejecting retries while has_database
		// is still false.
		c.markNeonIntegrationErrored(project, neonProjectID)
		return fmt.Errorf("seed starter schema: %w", err)
	}

	// Persist the schema on the project so future runs and the dashboard
	// share a single source of truth for the table layout.
	if dataModelsJSON, err := models.MarshalDataModels(tables); err == nil && dataModelsJSON != nil {
		project.DataModels = dataModelsJSON
	} else if err != nil {
		slog.Warn("provisionDatabaseForAgent: failed to marshal data models", "error", err, "project_id", project.ID)
	}

	if project.Settings == nil {
		project.Settings = make(map[string]any)
	}
	project.Settings["has_database"] = true

	if _, err := c.DB.UpdateAppProject(project); err != nil {
		return fmt.Errorf("persist wemob db on project: %w", err)
	}

	// Seed images best-effort, matching the template-copy flow. Failures are
	// logged but not surfaced — the database is already usable without them.
	go func() {
		seedCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		c.generateAndStoreSeedImages(seedCtx, project, tables, noopEvent, 12)
	}()

	return nil
}

// getNeonIntegration returns the Neon integration for a project, if any.
// Only returns integrations with status "connected".
func (c *Controller) getNeonIntegration(projectID string) (*models.AppIntegration, error) {
	integrations, err := c.DB.GetAppIntegrationsByProjectID(projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to get neon integration: %w", err)
	}
	for i, integ := range integrations {
		if integ.Provider == "neon" && integ.Status == "connected" {
			return &integrations[i], nil
		}
	}
	return nil, nil
}

// seedStarterSchema runs the canonical ecommerce starter SQL against a freshly
// provisioned Neon DB. Called only for projects where ProjectType == "ecommerce".
// After this runs, wemob never modifies the tenant's schema again — the AI owns
// it.
func (c *Controller) seedStarterSchema(ctx context.Context, connStr string, project *models.AppProject) error {
	if project.ProjectType != "ecommerce" {
		return nil
	}

	db, err := sql.Open("postgres", database.NeonConnStrWithEndpoint(connStr))
	if err != nil {
		return fmt.Errorf("open neon for seed: %w", err)
	}
	defer db.Close()

	execCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if _, err := db.ExecContext(execCtx, starters.EcommerceSchemaSQL); err != nil {
		return fmt.Errorf("apply ecommerce starter schema: %w", err)
	}
	slog.Info("ecommerce starter schema seeded", "project_id", project.ID)
	return nil
}

// markNeonIntegrationErrored flips the Neon integration row's status to "error"
// so isNeonBacked / getNeonIntegration no longer treat it as live. Used to
// unblock retries after seedStarterSchema fails — we keep the Neon project
// itself around (app-deletion flow owns cleanup) but stop advertising it as
// connected. Errors from this rollback are logged only; the caller surfaces
// the original seed failure.
func (c *Controller) markNeonIntegrationErrored(project *models.AppProject, neonProjectID string) {
	integ, err := c.DB.GetAppIntegrationByProvider(project.ID.String(), "neon")
	if err != nil {
		slog.Warn("mark neon errored: integration lookup failed",
			"error", err, "project_id", project.ID, "neon_project_id", neonProjectID)
		return
	}
	if integ == nil || integ.ID == uuid.Nil {
		slog.Warn("mark neon errored: no integration found",
			"project_id", project.ID, "neon_project_id", neonProjectID)
		return
	}
	integ.Status = "error"
	integ.UpdatedAt = time.Now()
	if _, err := c.DB.UpdateAppIntegration(integ); err != nil {
		slog.Warn("mark neon errored: update failed",
			"error", err, "project_id", project.ID, "neon_project_id", neonProjectID)
	}
}
