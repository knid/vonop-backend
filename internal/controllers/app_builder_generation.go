package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/services/fal"
	"github.com/knid/wemob/internal/services/gitrepo"
	"github.com/knid/wemob/internal/services/remoteconfig"
	"github.com/knid/wemob/internal/templates"
	"github.com/knid/wemob/internal/utils"
)

// --- Single-shot initial generation ---

// runPreGenerationSetup handles stateful operations (e-commerce, database, auth)
// BEFORE the single-shot LLM call so the LLM doesn't need tools for setup.
// DB provisioning and schema injection are synchronous (the LLM needs the schema).
// DB seeding and image generation run in the background — the returned waitForSeed
// function blocks until they complete. Callers should defer waitForSeed() after the
// main LLM call so seeding overlaps with code generation.
func (c *Controller) runPreGenerationSetup(
	ctx context.Context,
	project *models.AppProject,
	user *models.User,
	repo *gitrepo.Repo,
	fileMap map[string]string,
	sendEvent func(string, any) bool,
	provider *utils.LLMProvider,
	model string,
	userPrompt string,
) (waitForSeed func(), err error) {
	var seedWG sync.WaitGroup
	waitForSeed = func() { seedWG.Wait() }

	// Look up user's plan limits for DB provisioning
	var userLimits *models.StandartUserLimits
	if fetchedLimits, limitsErr := c.DB.GetStandartUserLimitsByUser(user); limitsErr == nil {
		userLimits = fetchedLimits
	}

	// --- Database setup ---
	if len(project.DataModels) > 0 && c.NeonService != nil {
		dataModels, parseErr := parseDataModels(project.DataModels)
		if parseErr != nil {
			slog.Warn("pre-gen: failed to unmarshal data models", "error", parseErr, "project_id", project.ID)
		}

		if len(dataModels) > 0 {
			// Check if already provisioned
			appIntegrations, _ := c.DB.GetAppIntegrationsByProjectID(project.ID.String())
			alreadyProvisioned := false
			for _, integ := range appIntegrations {
				if integ.Provider == "neon" && integ.Status == "connected" {
					alreadyProvisioned = true
					break
				}
			}

			if !alreadyProvisioned {
				sendEvent("neon_provision", map[string]any{"phase": "start"})
				sendEvent("progress", responses.ProgressEvent{
					Phase:   "provisioning_db",
					Message: "Setting up your database...",
				})

				// Merge auth models if needed
				needsAuth := false
				if settings := project.Settings; settings != nil {
					needsAuth, _ = settings["needs_auth"].(bool)
				}
				if needsAuth {
					var authModels []DataModel
					if err := json.Unmarshal([]byte(constants.AUTH_DEFAULT_DATA_MODELS), &authModels); err == nil {
						for _, authModel := range authModels {
							found := false
							for i, m := range dataModels {
								if m.Name == authModel.Name {
									dataModels[i] = mergeDataModelColumns(authModel, m)
									found = true
									break
								}
							}
							if !found {
								dataModels = append([]DataModel{authModel}, dataModels...)
							}
						}
					}
				}

				// Merge ecommerce models if needed
				if project.ProjectType == "ecommerce" {
					var defaultModels []DataModel
					if err := json.Unmarshal([]byte(constants.ECOMMERCE_DEFAULT_DATA_MODELS), &defaultModels); err == nil {
						existing := make(map[string]bool)
						for _, m := range dataModels {
							existing[m.Name] = true
						}
						for _, dm := range defaultModels {
							if !existing[dm.Name] {
								dataModels = append([]DataModel{dm}, dataModels...)
							}
						}
					}
				}

				connStr, neonProjectID, neonErr := c.provisionNeonDatabase(ctx, project, dataModels, userLimits, sendEvent)
				if connStr != "" && neonProjectID != "" && neonErr == nil {
					if err := c.storeNeonIntegration(project, connStr, neonProjectID); err != nil {
						slog.Error("pre-gen: failed to store neon integration", "error", err, "project_id", project.ID)
					}

					// Inject DB template files (synchronous — LLM needs the schema)
					dbContent := templates.GetTemplate("lib/db.ts")
					c.injectTemplateFile(repo, "lib/db.ts", dbContent, fileMap, sendEvent)

					schemaContent := buildDrizzleSchema(dataModels)
					c.injectTemplateFile(repo, "lib/schema.ts", schemaContent, fileMap, sendEvent)

					// Seed database + generate images in the background.
					// The LLM doesn't need seed data to generate code — only the schema.
					seedWG.Add(1)
					go func() {
						defer seedWG.Done()
						sendEvent("progress", responses.ProgressEvent{
							Phase:   "provisioning_db",
							Message: "Populating database with sample data...",
						})
						if _, seedErr := c.seedNeonDatabase(ctx, project, dataModels, sendEvent); seedErr != nil {
							slog.Error("pre-gen: database seeding failed", "error", seedErr, "project_id", project.ID)
						} else if project.ProjectType != "ecommerce" {
							c.generateAndStoreSeedImages(ctx, project, dataModels, sendEvent, 10)
						}
						sendEvent("progress", responses.ProgressEvent{
							Phase:   "provisioning_db",
							Message: "Database ready!",
						})
						sendEvent("neon_provision", map[string]any{"phase": "ready"})
					}()
				} else {
					slog.Error("pre-gen: database provisioning failed", "error", neonErr, "project_id", project.ID)
				}
			}
		}
	}

	// --- Auth setup ---
	if settings := project.Settings; settings != nil {
		if needsAuth, ok := settings["needs_auth"].(bool); ok && needsAuth {
			// Auth templates import from ./db, so lib/db.ts must exist.
			// If no data_models were planned (e.g. e-commerce uses wemob-store SDK),
			// provision a database. Use all project data models if available, otherwise just auth models.
			if _, hasDB := fileMap["lib/db.ts"]; !hasDB && c.NeonService != nil {
				var authModels []DataModel
				if err := json.Unmarshal([]byte(constants.AUTH_DEFAULT_DATA_MODELS), &authModels); err == nil {
					// If project has data models, use them merged with auth models (not just auth)
					modelsToProvision := authModels
					if len(project.DataModels) > 0 {
						projectModels, _ := parseDataModels(project.DataModels)
						if len(projectModels) > 0 {
							// Merge auth columns into project models
							for _, authModel := range authModels {
								found := false
								for i, m := range projectModels {
									if m.Name == authModel.Name {
										projectModels[i] = mergeDataModelColumns(authModel, m)
										found = true
										break
									}
								}
								if !found {
									projectModels = append([]DataModel{authModel}, projectModels...)
								}
							}
							modelsToProvision = projectModels
						}
					}

					sendEvent("neon_provision", map[string]any{"phase": "start"})
					sendEvent("progress", responses.ProgressEvent{
						Phase:   "provisioning_db",
						Message: "Setting up authentication database...",
					})
					connStr, neonProjectID, neonErr := c.provisionNeonDatabase(ctx, project, modelsToProvision, userLimits, sendEvent)
					if connStr != "" && neonProjectID != "" && neonErr == nil {
						if err := c.storeNeonIntegration(project, connStr, neonProjectID); err != nil {
							slog.Error("pre-gen auth: failed to store neon integration", "error", err, "project_id", project.ID)
						}
						c.injectTemplateFile(repo, "lib/db.ts", templates.GetTemplate("lib/db.ts"), fileMap, sendEvent)
						schemaContent := buildDrizzleSchema(modelsToProvision)
						c.injectTemplateFile(repo, "lib/schema.ts", schemaContent, fileMap, sendEvent)

						// Seed database + generate images in the background
						seedWG.Add(1)
						go func() {
							defer seedWG.Done()
							sendEvent("progress", responses.ProgressEvent{
								Phase:   "provisioning_db",
								Message: "Populating database with sample data...",
							})
							if _, seedErr := c.seedNeonDatabase(ctx, project, modelsToProvision, sendEvent); seedErr != nil {
								slog.Error("pre-gen auth: database seeding failed", "error", seedErr, "project_id", project.ID)
							} else if project.ProjectType != "ecommerce" {
								c.generateAndStoreSeedImages(ctx, project, modelsToProvision, sendEvent, 10)
							}
							sendEvent("progress", responses.ProgressEvent{
								Phase:   "provisioning_db",
								Message: "Database ready!",
							})
							sendEvent("neon_provision", map[string]any{"phase": "ready"})
						}()
					} else {
						slog.Error("pre-gen auth: database provisioning failed", "error", neonErr, "project_id", project.ID)
					}
				}
			}

			// Generate AUTH_SECRET env var if not already set
			existingVars, _ := c.DB.GetAppEnvVarsByProjectID(project.ID.String())
			hasAuthSecret := false
			for _, ev := range existingVars {
				if ev.Key == "AUTH_SECRET" {
					hasAuthSecret = true
					break
				}
			}
			if !hasAuthSecret {
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

			// Update .env.local with auth vars
			envVars := c.CollectAllEnvVars(project)
			if dotEnv := BuildDotEnvLocal(envVars); dotEnv != "" {
				c.injectTemplateFile(repo, ".env.local", dotEnv, fileMap, sendEvent)
			}
		}
	}

	return waitForSeed, nil
}

// streamSingleShotGeneration performs initial app generation in a single LLM call.
// The LLM outputs all files using <file path="...">content</file> blocks which are
// parsed incrementally and saved as they complete.
func (c *Controller) streamSingleShotGeneration(
	ctx context.Context,
	project *models.AppProject,
	user *models.User,
	model string,
	provider *utils.LLMProvider,
	repo *gitrepo.Repo,
	fileMap map[string]string,
	sendEvent func(string, any) bool,
	message string,
	imageURLs []string,
	toolContext string,
	mobileConfig *models.MobileAppConfig,
) (responseText string, ops []appBuilderOperation, err error) {
	// 1. Structure planning — must run BEFORE pre-gen setup so that data_models
	// and needs_auth are available for Neon provisioning and auth template injection.
	// Structure planning is critical for app quality (provides color scheme, typography,
	// file plan, and data models). PlanAppFileStructure retries internally on failure.
	structure, planErr := c.PlanAppFileStructure(ctx, message, project.ProjectType)
	if planErr != nil {
		slog.Error("single-shot: structure planning failed after retries",
			"error", planErr, "project_id", project.ID)
		return "", nil, fmt.Errorf("structure planning failed: %w", planErr)
	}
	// Apply name/slug/theme/file_plan/data_models/needs_auth to project
	if structure.Name != "" {
		project.Name = structure.Name
	}
	if structure.Slug != "" {
		project.Slug = structure.Slug + "-" + uuid.New().String()[:8]
	}
	themeData := models.AppTheme{
		ColorScheme:     structure.ColorScheme,
		DarkColorScheme: structure.DarkColorScheme,
		Typography:      structure.Typography,
	}
	if themeJSON, err := json.Marshal(themeData); err == nil {
		project.Theme = themeJSON
	}
	c.ApplyStructureToProject(project, structure)
	if _, err := c.DB.UpdateAppProject(project); err != nil {
		slog.Warn("single-shot: failed to update project after structure planning",
			"error", err, "project_id", project.ID)
	}

	// 2. Run pre-generation setup (e-commerce store, Neon database, auth templates).
	// Now that structure planning has set data_models and needs_auth on the project,
	// this step can properly provision the database and inject auth files.
	waitForSeed, setupErr := c.runPreGenerationSetup(ctx, project, user, repo, fileMap, sendEvent, provider, model, message)
	if setupErr != nil {
		return "", nil, fmt.Errorf("pre-generation setup failed: %w", setupErr)
	}
	// DB seeding runs in the background — wait for it after the main LLM call
	defer waitForSeed()

	// Inject parameterized templates (layout + globals.css)
	{
		var theme models.AppTheme
		if len(project.Theme) > 0 {
			json.Unmarshal(project.Theme, &theme)
		}
		if project.ProjectType == "ecommerce" {
			if layoutContent := buildEcommerceLayout(project.Name, theme.Typography); layoutContent != "" {
				c.injectTemplateFile(repo, "app/layout.tsx", layoutContent, fileMap, sendEvent)
			}
			if cssContent := buildEcommerceGlobalsCSSTheme(theme.ColorScheme, theme.DarkColorScheme); cssContent != "" {
				c.injectTemplateFile(repo, "app/globals.css", cssContent, fileMap, sendEvent)
			}
		} else {
			// For non-e-commerce apps, inject globals.css with semantic tokens so
			// the UI component templates (button, card, input, badge) work correctly.
			isMobile := project.ProjectType == "mobile_app"
			if cssContent := buildGlobalsCSSTheme(theme.ColorScheme, theme.DarkColorScheme, isMobile); cssContent != "" {
				c.injectTemplateFile(repo, "app/globals.css", cssContent, fileMap, sendEvent)
			}
		}
	}

	// Send project_created event (with planned files when available)
	projectCreatedPayload := map[string]any{
		"id":   project.ID,
		"name": project.Name,
		"slug": project.Slug,
	}
	if structure != nil && len(structure.Files) > 0 {
		plannedPaths := make([]string, 0, len(structure.Files))
		for _, f := range structure.Files {
			plannedPaths = append(plannedPaths, f.Path)
		}
		projectCreatedPayload["planned_files"] = plannedPaths
	}
	sendEvent("project_created", projectCreatedPayload)

	// 3. Generate AI logo in background (don't block code generation)
	logoCh := make(chan string, 1)
	go func() {
		logoCh <- c.generateAppLogo(ctx, project, sendEvent)
	}()

	// 4. Build messages with single-shot system prompt
	projectContext := c.buildProjectContext(project, fileMap, true)

	// Permanent logo URL is deterministic — inject it now so the LLM can use it.
	// The actual image may still be generating, but the URL will work once it's ready.
	permanentLogoURL := fmt.Sprintf("%s/v2/apps/%s/logo", constants.WEMOB_API_BASE_URL, project.ID)
	projectContext += fmt.Sprintf("\n\nApp Logo URL (use this as the site logo/favicon in the header and metadata):\n%s", permanentLogoURL)

	llmMessages := []utils.Message{
		{Role: "system", Content: buildSingleShotSystemPrompt(project, mobileConfig)},
		{Role: "system", Content: projectContext},
	}

	// Inject tool-gathered context (website analysis, web search results) if available
	if toolContext != "" {
		llmMessages = append(llmMessages, utils.Message{Role: "system", Content: toolContext})
	}

	userMsg := utils.Message{Role: "user", Content: message}
	if len(imageURLs) > 0 {
		if provider.SupportsVision() {
			userMsg.Images = imageURLs
		} else {
			userMsg.Content += fmt.Sprintf("\n\n[User attached %d image(s) for visual reference — recreate the design shown]", len(imageURLs))
		}
	}
	llmMessages = append(llmMessages, userMsg)

	// 5. Set up streaming file parser
	plannedTotal := 0
	if structure != nil {
		plannedTotal = len(structure.Files)
	}
	fileCounter := 0

	sendEvent("progress", responses.ProgressEvent{
		Phase:   "generating_files",
		Message: "Starting code generation...",
		Current: 0,
		Total:   plannedTotal,
	})

	var responseBuilder strings.Builder
	parser := &utils.FileStreamParser{
		DiscardIncomplete: true, // Don't save truncated files if LLM hits token limit
		OnFile: func(path, content string) {
			content = utils.StripCodeFences(content)
			_, fileType := guessFileType(path)
			if fileType == "tsx" || fileType == "ts" {
				content, _ = stripForbiddenImports(content, path)
			}
			if err := c.saveGeneratedFile(project, repo, path, content, fileType, fileMap, sendEvent, &ops); err != nil {
				slog.Error("file save failed during generation", "path", path, "error", err)
			}
			fileCounter++
			sendEvent("progress", responses.ProgressEvent{
				Phase:   "generating_files",
				Message: fmt.Sprintf("Generated %s (%d/%d)", path, fileCounter, plannedTotal),
				Current: fileCounter,
				Total:   plannedTotal,
			})
		},
		OnText: func(text string) {
			responseBuilder.WriteString(text)
			cleaned := ansiRegex.ReplaceAllString(text, "")
			if cleaned != "" {
				sendEvent("chat_text", responses.ChatTextEvent{Chunk: cleaned})
			}
		},
	}

	// 6. Single LLM call — no tools, no rounds
	slog.Info("Starting single-shot generation",
		"model", model,
		"project_id", project.ID,
		"message_count", len(llmMessages),
	)

	_, err = utils.StreamTextFromLLMWithMessages(
		ctx, model, llmMessages, provider,
		func(chunk string) { parser.Feed(chunk) },
		func(chunk string) {
			sendEvent("chat_thinking", responses.ChatThinkingEvent{Chunk: chunk})
		},
		utils.StreamTextConfig{
			MaxTokens: 65536,
			OnRetry: func(attempt int, reason string) {
				sendEvent("progress", responses.ProgressEvent{
					Phase:   "generating_files",
					Message: fmt.Sprintf("Retrying code generation (attempt %d)...", attempt+1),
				})
			},
		},
	)
	parser.Flush()

	// 7. Run ALL deterministic auto-fixes (import mismatches, font weights, use client,
	// .ts→.tsx rename, className concat, JSX strings, metadata, layout tags, force-dynamic).
	filesChanged := len(ops)
	c.runDeterministicAutoFixes(project, repo, fileMap, sendEvent, &ops, &filesChanged)

	// Augment package.json with discovered dependencies
	if pkgContent, ok := fileMap["package.json"]; ok {
		augmented := augmentPackageJSON(pkgContent, fileMap)
		if augmented != pkgContent {
			if writeErr := repo.WriteFile("package.json", []byte(augmented)); writeErr != nil {
				slog.Error("failed to persist augmented package.json", "error", writeErr, "project_id", project.ID)
			} else {
				fileMap["package.json"] = augmented
				sendEvent("file_complete", responses.FileCompleteEvent{
					Action: "modify", FilePath: "package.json",
					FileType: "json", Content: augmented,
				})
			}
		}
	}

	// 8. Generate any missing planned files (token limit may have cut generation short)
	if structure != nil && ctx.Err() == nil {
		c.generateMissingPlannedFiles(ctx, structure, project, model, provider, repo, fileMap, sendEvent, &ops, mobileConfig, message)
	}

	// 9. Validation fix pass — run deterministic checks and fix detected errors via LLM
	if ctx.Err() == nil && len(ops) > 0 {
		c.runValidationFixPass(ctx, project, user, provider, repo, fileMap, sendEvent, &ops, mobileConfig)
	}

	// 10. Auto-detect WemobBridge usage and configure mobile app
	c.autoDetectMobileBridgeConfig(project, fileMap, sendEvent)

	// Wait for logo generation to finish (no-op if already done)
	<-logoCh

	return responseBuilder.String(), ops, err
}

// generateAppLogo generates an AI logo for the app via fal.ai, uploads it to S3,
// and returns a presigned URL. Returns "" on any failure (best-effort).
func (c *Controller) generateAppLogo(ctx context.Context, project *models.AppProject, sendEvent func(string, any) bool) string {
	apiKey := c.SecretManager.GetSecret("FAL_API_KEY")
	if apiKey == "" {
		return ""
	}

	falModel := c.SecretManager.GetSecret("FAL_IMAGE_MODEL")
	if falModel == "" {
		falModel = "fal-ai/flux/schnell"
	}

	sendEvent("tool_start", map[string]any{"tool": "generate_logo"})
	sendEvent("progress", responses.ProgressEvent{
		Phase:   "generating_logo",
		Message: "Designing your app logo...",
	})

	prompt := fmt.Sprintf(
		"Minimal, modern app logo icon for '%s'. Clean geometric design, flat style, single iconic symbol, no text, no letters, plain white background, suitable for favicon and app header. Professional, tech-company quality.",
		project.Name,
	)

	cdnURL, err := fal.GenerateImage(ctx, apiKey, falModel, prompt)
	if err != nil {
		slog.Warn("generateAppLogo: fal generation failed", "error", err, "project_id", project.ID)
		return ""
	}

	// Remove background to get a transparent PNG
	transparentURL, err := fal.RemoveBackground(ctx, apiKey, cdnURL)
	if err != nil {
		slog.Warn("generateAppLogo: background removal failed, using original", "error", err, "project_id", project.ID)
	} else {
		cdnURL = transparentURL
	}

	imgData, err := downloadImage(ctx, cdnURL)
	if err != nil {
		slog.Warn("generateAppLogo: failed to download image", "error", err, "project_id", project.ID)
		return ""
	}

	s3Key := fmt.Sprintf("apps/%s/logo.png", project.ID)
	if err := c.AssetsStorage.UploadFileByteWithContentType(ctx, s3Key, imgData, "image/png"); err != nil {
		slog.Warn("generateAppLogo: failed to upload to S3", "error", err, "project_id", project.ID)
		return ""
	}

	// Persist logo S3 key on the project for future use (chat context, re-generation)
	project.LogoS3Key = s3Key
	if _, err := c.DB.DB.NewUpdate().Model(project).Column("logo_s3_key").WherePK().Exec(ctx); err != nil {
		slog.Warn("generateAppLogo: failed to save logo_s3_key", "error", err, "project_id", project.ID)
	}

	sendEvent("progress", responses.ProgressEvent{
		Phase:   "generating_logo",
		Message: "Logo ready!",
	})

	// Return permanent URL — the /v2/apps/{id}/logo endpoint generates fresh
	// pre-signed URLs on each request, so the URL never expires in generated code.
	permanentURL := fmt.Sprintf("%s/v2/apps/%s/logo", constants.WEMOB_API_BASE_URL, project.ID)
	slog.Info("generateAppLogo: logo generated", "project_id", project.ID, "s3_key", s3Key)
	return permanentURL
}

// streamEcommerceGeneration performs initial e-commerce app generation in two phases:
//   Phase 1: Tool-calling with only setup_ecommerce + manage_store (populates the store).
//   Phase 2: Single-shot file generation using <file> blocks (generates all code files).
// This is faster and cheaper than running a single tool loop for everything.
func (c *Controller) streamEcommerceGeneration(
	ctx context.Context,
	project *models.AppProject,
	user *models.User,
	model string,
	provider *utils.LLMProvider,
	repo *gitrepo.Repo,
	fileMap map[string]string,
	sendEvent func(string, any) bool,
	message string,
	imageURLs []string,
	toolContext string,
	mobileConfig *models.MobileAppConfig,
) (responseText string, ops []appBuilderOperation, err error) {
	// 1. Structure planning (same as single-shot — fails hard if retries exhausted)
	structure, planErr := c.PlanAppFileStructure(ctx, message, project.ProjectType)
	if planErr != nil {
		slog.Error("ecommerce-gen: structure planning failed after retries",
			"error", planErr, "project_id", project.ID)
		return "", nil, fmt.Errorf("structure planning failed: %w", planErr)
	}
	if structure.Name != "" {
		project.Name = structure.Name
	}
	if structure.Slug != "" {
		project.Slug = structure.Slug + "-" + uuid.New().String()[:8]
	}
	themeData := models.AppTheme{
		ColorScheme:     structure.ColorScheme,
		DarkColorScheme: structure.DarkColorScheme,
		Typography:      structure.Typography,
	}
	if themeJSON, err := json.Marshal(themeData); err == nil {
		project.Theme = themeJSON
	}
	c.ApplyStructureToProject(project, structure)
	if _, err := c.DB.UpdateAppProject(project); err != nil {
		slog.Warn("ecommerce-gen: failed to update project after structure planning",
			"error", err, "project_id", project.ID)
	}

	// Send project_created event
	projectCreatedPayload := map[string]any{
		"id":   project.ID,
		"name": project.Name,
		"slug": project.Slug,
	}
	if structure != nil && len(structure.Files) > 0 {
		plannedPaths := make([]string, 0, len(structure.Files))
		for _, f := range structure.Files {
			plannedPaths = append(plannedPaths, f.Path)
		}
		projectCreatedPayload["planned_files"] = plannedPaths
	}
	sendEvent("project_created", projectCreatedPayload)

	// === Phase 1: Setup (tool-calling with setup_ecommerce + manage_store only) ===
	// Use fast model — setup is just tool calling to create products, no code generation.
	setupModel := c.fixModel(provider)
	slog.Info("ecommerce-gen: starting Phase 1 (store setup)",
		"model", setupModel, "project_id", project.ID)

	setupTools := filterToolsByName("setup_ecommerce", "manage_store")

	setupCallbacks := &builderToolCallbacks{
		sendEvent:    sendEvent,
		appliedOps:   &ops,
		pendingFiles: make(map[string]string),
	}

	setupExecutor := c.buildBuilderToolExecutor(ctx, project, user, repo, fileMap, setupCallbacks)
	executor := c.compositeToolExecutor(setupExecutor)

	setupMessages := []utils.Message{
		{Role: "system", Content: constants.NEXTJS_ECOMMERCE_SETUP_INSTRUCTIONS},
		{Role: "user", Content: message},
	}

	setupConfig := utils.ToolCallConfig{
		MaxRounds:        c.RemoteConfig.GetInt(remoteconfig.AIGenerationMaxRounds, remoteconfig.Global),
		NudgeAfter:       5,
		StripAfter:       6,
		MaxTokens:        8192,
		MaxContinuations: 1,
		NudgeMessage:     "Finish populating the store and provide a brief summary.",
		OnToolCallStart: func(toolName string) {
			sendEvent("tool_start", map[string]any{"tool": toolName})
		},
	}

	_, setupErr := utils.StreamWithTools(
		ctx, setupModel, setupMessages, setupTools, provider,
		nil, // no forced JSON format
		nil, // no reasoning for setup phase
		executor,
		func(toolName, args string) {
			sendEvent("tool_call", responses.ToolCallEvent{
				Tool:      toolName,
				Arguments: args,
			})
		},
		func(chunk string) {
			chunk = ansiRegex.ReplaceAllString(chunk, "")
			if chunk != "" {
				sendEvent("chat_text", responses.ChatTextEvent{Chunk: chunk})
			}
		},
		func(chunk string) {
			sendEvent("chat_thinking", responses.ChatThinkingEvent{Chunk: chunk})
		},
		setupConfig,
	)

	if setupErr != nil {
		return "", ops, fmt.Errorf("ecommerce setup phase failed: %w", setupErr)
	}

	// Check context cancellation between phases
	if ctx.Err() != nil {
		return "", ops, ctx.Err()
	}

	// === Phase 1.5: Generate AI product images ===
	store, storeErr := c.DB.GetStoreByAppProjectID(project.ID.String())
	if storeErr == nil && store != nil {
		sendEvent("tool_start", map[string]any{"tool": "generate_product_images"})
		sendEvent("progress", responses.ProgressEvent{
			Phase:   "generating_images",
			Message: "Generating product images...",
		})
		c.GenerateProductImages(ctx, store)
	}

	// Check context cancellation between phases
	if ctx.Err() != nil {
		return "", ops, ctx.Err()
	}

	// === Phase 2: File generation (single-shot, no tools) ===
	slog.Info("ecommerce-gen: starting Phase 2 (file generation)",
		"model", model, "project_id", project.ID)

	// Run pre-generation setup (injects e-commerce templates, auth files, etc.)
	waitForSeed, setupErr := c.runPreGenerationSetup(ctx, project, user, repo, fileMap, sendEvent, provider, model, message)
	if setupErr != nil {
		return "", ops, fmt.Errorf("pre-generation setup failed: %w", setupErr)
	}
	defer waitForSeed()

	// Inject parameterized e-commerce templates (layout + globals.css)
	if project.ProjectType == "ecommerce" {
		var theme models.AppTheme
		if len(project.Theme) > 0 {
			json.Unmarshal(project.Theme, &theme)
		}
		if layoutContent := buildEcommerceLayout(project.Name, theme.Typography); layoutContent != "" {
			c.injectTemplateFile(repo, "app/layout.tsx", layoutContent, fileMap, sendEvent)
		}
		if cssContent := buildEcommerceGlobalsCSSTheme(theme.ColorScheme, theme.DarkColorScheme); cssContent != "" {
			c.injectTemplateFile(repo, "app/globals.css", cssContent, fileMap, sendEvent)
		}
	}

	// Generate AI logo for the store
	logoURL := c.generateAppLogo(ctx, project, sendEvent)

	// Rebuild messages with fresh context (now includes store data from Phase 1)
	projectContext := c.buildProjectContext(project, fileMap, true)
	if logoURL != "" {
		projectContext += fmt.Sprintf("\n\nApp Logo URL (use this as the site logo/favicon in the header and metadata):\n%s", logoURL)
	}

	llmMessages := []utils.Message{
		{Role: "system", Content: buildSingleShotSystemPrompt(project, mobileConfig)},
		{Role: "system", Content: projectContext},
	}

	// Inject tool-gathered context (website analysis, web search results) if available
	if toolContext != "" {
		llmMessages = append(llmMessages, utils.Message{Role: "system", Content: toolContext})
	}

	userMsg := utils.Message{Role: "user", Content: message}
	if len(imageURLs) > 0 {
		if provider.SupportsVision() {
			userMsg.Images = imageURLs
		} else {
			userMsg.Content += fmt.Sprintf("\n\n[User attached %d image(s) for visual reference — recreate the design shown]", len(imageURLs))
		}
	}
	llmMessages = append(llmMessages, userMsg)

	// Set up streaming file parser
	ecomPlannedTotal := 0
	if structure != nil {
		ecomPlannedTotal = len(structure.Files)
	}
	ecomFileCounter := 0

	sendEvent("progress", responses.ProgressEvent{
		Phase:   "generating_files",
		Message: "Starting code generation...",
		Current: 0,
		Total:   ecomPlannedTotal,
	})

	var responseBuilder strings.Builder
	parser := &utils.FileStreamParser{
		DiscardIncomplete: true, // Don't save truncated files if LLM hits token limit
		OnFile: func(path, content string) {
			content = utils.StripCodeFences(content)
			_, fileType := guessFileType(path)
			if fileType == "tsx" || fileType == "ts" {
				content, _ = stripForbiddenImports(content, path)
			}
			if err := c.saveGeneratedFile(project, repo, path, content, fileType, fileMap, sendEvent, &ops); err != nil {
				slog.Error("ecom file save failed during generation", "path", path, "error", err)
			}
			ecomFileCounter++
			sendEvent("progress", responses.ProgressEvent{
				Phase:   "generating_files",
				Message: fmt.Sprintf("Generated %s (%d/%d)", path, ecomFileCounter, ecomPlannedTotal),
				Current: ecomFileCounter,
				Total:   ecomPlannedTotal,
			})
		},
		OnText: func(text string) {
			responseBuilder.WriteString(text)
			cleaned := ansiRegex.ReplaceAllString(text, "")
			if cleaned != "" {
				sendEvent("chat_text", responses.ChatTextEvent{Chunk: cleaned})
			}
		},
	}

	// Single LLM call — no tools
	slog.Info("ecommerce-gen: Phase 2 LLM call",
		"model", model,
		"project_id", project.ID,
		"message_count", len(llmMessages),
	)

	_, err = utils.StreamTextFromLLMWithMessages(
		ctx, model, llmMessages, provider,
		func(chunk string) { parser.Feed(chunk) },
		func(chunk string) {
			sendEvent("chat_thinking", responses.ChatThinkingEvent{Chunk: chunk})
		},
		utils.StreamTextConfig{
			MaxTokens: 65536,
			OnRetry: func(attempt int, reason string) {
				sendEvent("progress", responses.ProgressEvent{
					Phase:   "generating_files",
					Message: fmt.Sprintf("Retrying code generation (attempt %d)...", attempt+1),
				})
			},
		},
	)
	parser.Flush()

	// Run ALL deterministic auto-fixes (same as standard single-shot path).
	ecomFilesChanged := len(ops)
	c.runDeterministicAutoFixes(project, repo, fileMap, sendEvent, &ops, &ecomFilesChanged)

	// Augment package.json with discovered dependencies
	if pkgContent, ok := fileMap["package.json"]; ok {
		augmented := augmentPackageJSON(pkgContent, fileMap)
		if augmented != pkgContent {
			if writeErr := repo.WriteFile("package.json", []byte(augmented)); writeErr != nil {
				slog.Error("failed to persist augmented package.json (ecom)", "error", writeErr, "project_id", project.ID)
			} else {
				fileMap["package.json"] = augmented
				sendEvent("file_complete", responses.FileCompleteEvent{
					Action: "modify", FilePath: "package.json",
					FileType: "json", Content: augmented,
				})
			}
		}
	}

	return responseBuilder.String(), ops, err
}
