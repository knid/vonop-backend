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
	"github.com/knid/wemob/internal/tasks"
	"github.com/knid/wemob/internal/utils"
	"github.com/knid/wemob/internal/utils/external"

	"github.com/hibiken/asynq"
)

// DeployToVercel deploys the app to Vercel using files from DB (code-first approach)
func (c *Controller) DeployToVercel(project *models.AppProject, user *models.User, target string) (*models.AppDeployment, error) {
	if c.VercelService == nil {
		return nil, fmt.Errorf("vercel service not configured")
	}

	if target == "" {
		target = "production"
	}

	// Read files from git repo
	repo, err := c.GitRepoService.Open(project.ID.String())
	if err != nil {
		return nil, fmt.Errorf("no generated files found for deployment")
	}
	repoFiles, err := repo.ReadAllFiles()
	if err != nil || len(repoFiles) == 0 {
		return nil, fmt.Errorf("no generated files found for deployment")
	}

	// Get or create Vercel project
	vercelProject, err := c.DB.GetVercelProjectByAppProjectID(project.ID.String())
	if err != nil {
		// Create new Vercel project
		vp, err := c.VercelService.CreateProject("wemob-" + project.Slug)
		if err != nil {
			slog.Error("Failed to create Vercel project", "error", err)
			return nil, fmt.Errorf("failed to create Vercel project")
		}

		// Disable deployment protection so URLs are publicly accessible
		c.VercelService.DisableDeploymentProtection(vp.ID)

		vercelProject = &models.VercelProject{
			AppProjectID:    project.ID,
			VercelProjectID: vp.ID,
			Framework:       "nextjs",
		}
		vercelProject, err = c.DB.CreateVercelProject(vercelProject)
		if err != nil {
			slog.Error("Failed to persist Vercel project to DB", "error", err)
			return nil, fmt.Errorf("failed to save Vercel project")
		}
	}

	// Collect all env vars (integrations + custom + auto-injected store vars)
	envVars := c.CollectAllEnvVars(project)

	// Set env vars on Vercel project
	if len(envVars) > 0 {
		if err := c.VercelService.SetEnvVars(vercelProject.VercelProjectID, envVars); err != nil {
			slog.Warn("Failed to set some env vars", "error", err)
		}
	}

	// Ensure deployment protection is disabled (idempotent)
	c.VercelService.DisableDeploymentProtection(vercelProject.VercelProjectID)

	// Build file map from repo files
	fileMap := repoFiles

	// Inject .env.local so Next.js can inline NEXT_PUBLIC_* vars at build time
	if dotEnv := BuildDotEnvLocal(envVars); dotEnv != "" {
		fileMap[".env.local"] = dotEnv
	}

	// Inject web analytics tracking script into the root layout
	injectAnalyticsScript(fileMap, project.ID.String())

	deployResp, err := c.VercelService.DeployFiles(vercelProject.VercelProjectID, fileMap, target)
	if err != nil {
		slog.Error("Failed to deploy to Vercel", "error", err)
		return nil, fmt.Errorf("deployment failed")
	}

	// Auto-assign {slug}.wemob.app subdomain (best-effort)
	subdomain := project.Slug + "." + constants.WEMOB_APP_BASE_DOMAIN
	if err := c.VercelService.AddDomain(vercelProject.VercelProjectID, subdomain); err != nil {
		slog.Warn("Failed to assign wemob.app subdomain", "subdomain", subdomain, "error", err)
	}

	// Use branded URL as the deployed URL; raw Vercel URL stays in ProviderData as fallback
	deployedURL := "https://" + subdomain

	// Track deployment
	deployment := &models.AppDeployment{
		AppProjectID: project.ID,
		UserID:       user.ID,
		Platform:     "web",
		Provider:     "vercel",
		Status:       "building",
		ProviderData: map[string]any{
			"deployment_id": deployResp.ID,
			"url":           deployResp.URL,
		},
		DeployedURL: deployedURL,
		Version:     1,
	}

	// Record commit hash from the repo
	if commitHash, headErr := repo.Head(); headErr == nil {
		deployment.CommitHash = commitHash
		// Tag the deployment
		repo.Tag(fmt.Sprintf("deploy-%d", time.Now().Unix()))
	}

	// Get latest version
	latestDeploy, err := c.DB.GetLatestAppDeployment(project.ID.String(), "web")
	if err == nil {
		deployment.Version = latestDeploy.Version + 1
	}

	deployment, err = c.DB.CreateAppDeployment(deployment)
	if err != nil {
		slog.Error("Failed to save deployment record", "error", err)
	}

	// Update Vercel project record
	vercelProject.ProductionURL = deployedURL
	vercelProject.CustomDomain = subdomain
	c.DB.UpdateVercelProject(vercelProject)

	// Update project status
	c.UpdateAppProjectStatus(project, "deployed")

	// For mobile_app projects, ensure a v1 Project exists so the app appears
	// in the v1 listing (analytics, notifications, builds).
	if project.ProjectType == "mobile_app" {
		if err := c.ensureV1ProjectForMobileApp(project, user, deployedURL); err != nil {
			slog.Error("Failed to create v1 Project for mobile app", "error", err, "app_project_id", project.ID)
		}
	}

	return deployment, nil
}

// BuildAppMobile creates a native mobile build for an AI-generated app by wrapping
// its Vercel deployment URL using the old URL-wrapper build system.
func (c *Controller) BuildAppMobile(appProject *models.AppProject, user *models.User, platform string, appName string, icon string, splashColor string) (*models.Build, *models.AppBuildTask, error) {
	// Enforce platform-specific build permissions
	limits, err := c.DB.GetStandartUserLimitsByUser(user)
	if err != nil {
		slog.Warn("BuildAppMobile: failed to get user limits", "error", err, "user_id", user.ID)
	} else {
		if platform == "android" && !limits.CanBuildToAndroid {
			return nil, nil, fmt.Errorf("your subscription does not allow Android builds")
		}
		if platform == "ios" && !limits.CanBuildToIOS {
			return nil, nil, fmt.Errorf("your subscription does not allow iOS builds")
		}
	}

	// Get the Vercel deployment URL — the app must be deployed to web first
	deployment, err := c.DB.GetLatestAppDeployment(appProject.ID.String(), "web")
	if err != nil || deployment.DeployedURL == "" {
		return nil, nil, fmt.Errorf("app must be deployed to web first")
	}

	wrappedURL := deployment.DeployedURL

	// Find or create an old-style Project for this URL
	project, err := c.DB.GetProjectByUserAndURL(user.ID.String(), wrappedURL)
	if err != nil {
		// Auto-generate package name
		pkHex, hexErr := utils.RandomHex(5)
		if hexErr != nil {
			return nil, nil, fmt.Errorf("failed to generate package name: %w", hexErr)
		}
		pkName := "com.wemob.app" + pkHex[0:5]

		// Create new project linked to this AI app's URL
		project = &models.Project{
			UserID:          user.ID,
			URL:             wrappedURL,
			PackageName:     pkName,
			IconPath:        icon,
			OneSignalAppID:  "wemob-app-" + appProject.ID.String(),
			OneSignalAppToken: "wemob-app-token-" + appProject.ID.String(),
			BuildForAndroid: platform == "android",
			BuildForIOS:     platform == "ios",
		}
		project, err = c.DB.CreateProject(project)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create build project: %w", err)
		}
	} else {
		// Update platform flags and metadata on existing project
		if platform == "android" {
			project.BuildForAndroid = true
		} else {
			project.BuildForIOS = true
		}
		// Keep existing package name
		if icon != "" {
			project.IconPath = icon
		}

		// Replace fake OneSignal IDs with real ones if needed
		if strings.HasPrefix(project.OneSignalAppID, "wemob-app-") {
			if err := c.replaceWithRealOneSignal(project); err != nil {
				slog.Warn("Failed to replace fake OneSignal IDs, continuing with existing", "error", err, "project_id", project.ID)
			}
		}

		c.DB.UpdateProject(project)
	}

	// Check for pending builds
	pendingTasks, err := c.DB.GetPendingAppBuildTasks(project)
	if err == nil && len(pendingTasks) > 0 {
		return nil, nil, fmt.Errorf("a build is already in progress, please wait")
	}

	// Get last build number
	var lastBuildNumber int
	lastBuild, err := c.DB.GetLastProjectBuild(project)
	if err == nil {
		lastBuildNumber = lastBuild.BuildNumber
	}

	// Create build record
	build := &models.Build{
		ProjectID:         project.ID,
		Active:            false,
		Title:             appName,
		Icon:              icon,
		SplashScreenColor: splashColor,
		NavBarStyle:       "default",
		BuildNumber:       lastBuildNumber + 1,
		VersionString:     utils.CreateVersionString(lastBuildNumber + 1),
	}

	build, err = c.DB.CreateProjectBuild(build)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create build record")
	}

	// Create task and enqueue
	var appBuildTask *models.AppBuildTask

	// Fetch mobile app config — try v1 Project ID first, then AppProject ID
	mobileConfig, _ := c.DB.GetMobileAppConfig(project.ID.String())
	if mobileConfig == nil {
		mobileConfig, _ = c.DB.GetMobileAppConfig(appProject.ID.String())
	}

	// Sync config to v1 Project ID so the Flutter app can fetch it at runtime
	if mobileConfig != nil && mobileConfig.ProjectID != project.ID {
		configCopy := *mobileConfig
		configCopy.ProjectID = project.ID
		c.DB.UpsertMobileAppConfig(&configCopy)
	}

	if platform == "android" {
		appBuildTask, err = c.DB.CreateAppBuildTask(*build, "Android", "Queued", false)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create build task: %w", err)
		}

		payload, _ := json.Marshal(&tasks.AndroidBuildPayload{
			ProjectID:         project.ID.String(),
			Version:           build.BuildNumber,
			BuildID:           build.ID.String(),
			TaskID:            appBuildTask.ID.String(),
			Title:             appName,
			URL:               wrappedURL,
			IconURL:           icon,
			SplashScreenColor: splashColor,
			PackageName:       project.PackageName,
			Secret:            appBuildTask.ID.String(),
			Permissions: func() []string {
				if mobileConfig != nil {
					return mobileConfig.Permissions
				}
				return nil
			}(),
			URLScheme: func() string {
				if mobileConfig != nil {
					return mobileConfig.URLScheme
				}
				return ""
			}(),
			UniversalLinkDomain: func() string {
				if mobileConfig != nil {
					return mobileConfig.UniversalLinkDomain
				}
				return ""
			}(),
		})
		task := asynq.NewTask(tasks.TypeBuildAndroid, payload)
		if _, err := c.TaskService.Enqueue(task, asynq.MaxRetry(2), asynq.Timeout(time.Minute*10)); err != nil {
			return nil, nil, fmt.Errorf("failed to enqueue Android build task: %w", err)
		}
	} else {
		appBuildTask, err = c.DB.CreateAppBuildTask(*build, "IOS", "Queued", false)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create build task: %w", err)
		}

		payload, _ := json.Marshal(&tasks.BuildUnsignedIpaPayload{
			ProjectID:         project.ID.String(),
			Version:           build.BuildNumber,
			BuildID:           build.ID.String(),
			TaskID:            appBuildTask.ID.String(),
			Title:             appName,
			URL:               wrappedURL,
			IconURL:           icon,
			SplashScreenColor: splashColor,
			PackageName:       project.PackageName,
			Secret:            appBuildTask.ID.String(),
			Permissions: func() []string {
				if mobileConfig != nil {
					return mobileConfig.Permissions
				}
				return nil
			}(),
			URLScheme: func() string {
				if mobileConfig != nil {
					return mobileConfig.URLScheme
				}
				return ""
			}(),
			UniversalLinkDomain: func() string {
				if mobileConfig != nil {
					return mobileConfig.UniversalLinkDomain
				}
				return ""
			}(),
		})
		task := asynq.NewTask(tasks.TypeBuildUnsignedIpa, payload)
		if _, err := c.TaskService.Enqueue(task, asynq.MaxRetry(2), asynq.Timeout(time.Minute*10)); err != nil {
			return nil, nil, fmt.Errorf("failed to enqueue iOS build task: %w", err)
		}
	}

	slog.Info("BuildAppMobile: build enqueued",
		"app_project_id", appProject.ID,
		"platform", platform,
		"build_id", build.ID,
		"url", wrappedURL,
	)

	return build, appBuildTask, nil
}

// GetAppDeployments returns all deployments for a project
func (c *Controller) GetAppDeployments(projectID string) ([]models.AppDeployment, error) {
	return c.DB.GetAppDeploymentsByProjectID(projectID)
}

// GetLatestDeploymentStatus returns the latest deployment status for a project.
// If the status is still "building", it checks the Vercel API for the real status and updates the DB.
func (c *Controller) GetLatestDeploymentStatus(projectID string) (*models.AppDeployment, error) {
	deployment, err := c.DB.GetLatestAppDeployment(projectID, "web")
	if err != nil {
		return nil, err
	}

	// If still building, check Vercel for actual status
	if (deployment.Status == "building" || deployment.Status == "pending") && c.VercelService != nil {
		deploymentID, _ := deployment.ProviderData["deployment_id"].(string)
		if deploymentID != "" {
			vercelDep, err := c.VercelService.GetDeployment(deploymentID)
			if err != nil {
				slog.Warn("Failed to check Vercel deployment status", "error", err, "deployment_id", deploymentID)
				return deployment, nil
			}

			// Map Vercel readyState to our status
			// Vercel states: QUEUED, BUILDING, INITIALIZING, READY, ERROR, CANCELED
			var newStatus string
			var errorMessage string
			switch vercelDep.State {
			case "READY":
				newStatus = "deployed"
			case "ERROR":
				newStatus = "failed"
				// Fetch actual build log from Vercel
				buildLog, logErr := c.VercelService.GetDeploymentBuildLog(deploymentID)
				if logErr != nil {
					slog.Warn("Failed to fetch build log", "error", logErr, "deployment_id", deploymentID)
					errorMessage = "Build failed on Vercel"
				} else if buildLog != "" {
					errorMessage = buildLog
				} else {
					errorMessage = "Build failed on Vercel"
				}
			case "CANCELED":
				newStatus = "failed"
				errorMessage = "Build was canceled"
			default:
				// Still building (QUEUED, BUILDING, INITIALIZING)
				return deployment, nil
			}

			// Update DB with real status
			deployment.Status = newStatus
			if errorMessage != "" {
				deployment.ErrorMessage = errorMessage
			}
			if _, err := c.DB.UpdateAppDeployment(deployment); err != nil {
				slog.Error("Failed to update deployment status", "error", err)
			}

		}
	}

	return deployment, nil
}

type AppValidationError struct {
	File  string `json:"file"`
	Line  int    `json:"line,omitempty"`
	Error string `json:"error"`
	Fix   string `json:"fix,omitempty"`
}

type AppValidationResult struct {
	Valid  bool                 `json:"valid"`
	Errors []AppValidationError `json:"errors"`
}

// ValidateAppFiles runs deterministic checks + optional LLM validation on project files.
func (c *Controller) ValidateAppFiles(projectID string) (*AppValidationResult, error) {
	repo, err := c.GitRepoService.Open(projectID)
	if err != nil {
		return nil, fmt.Errorf("no files found to validate")
	}
	fileMap, err := repo.ReadAllFiles()
	if err != nil || len(fileMap) == 0 {
		return nil, fmt.Errorf("no files found to validate")
	}

	// Run deterministic checks first
	detErrors := runDeterministicChecks(fileMap)

	var result AppValidationResult
	if len(detErrors) > 0 {
		result.Valid = false
		for _, e := range detErrors {
			result.Errors = append(result.Errors, AppValidationError{
				File:  e.File,
				Error: e.Error,
			})
		}
	}

	// Also run LLM-based validation for deeper checks
	provider := c.llmProvider()
	model := c.SecretManager.GetSecret("WEBSITE_BUILDER_LLM_MODEL")
	if model == "" {
		model = provider.DefaultModel()
	}

	var fileContext strings.Builder
	fileContext.WriteString("Project files:\n")
	for path, content := range fileMap {
		fileContext.WriteString(fmt.Sprintf("\n--- %s (%s) ---\n%s\n", path, InferFileType(path), content))
	}

	responseJSON, err := utils.GetJSONFromLLM(context.Background(), model, constants.NEXTJS_VALIDATE_PROMPT, fileContext.String(), provider)
	if err != nil {
		slog.Error("validation LLM call failed", "error", err)
		// Still return deterministic results
		if len(result.Errors) == 0 {
			result.Valid = true
		}
		return &result, nil
	}

	var llmResult AppValidationResult
	if err := json.Unmarshal([]byte(responseJSON), &llmResult); err != nil {
		cleaned := utils.ExtractJSON(responseJSON)
		if err2 := json.Unmarshal([]byte(cleaned), &llmResult); err2 != nil {
			slog.Warn("failed to parse validation response", "error", err2)
			if len(result.Errors) == 0 {
				result.Valid = true
			}
			return &result, nil
		}
	}

	// Merge LLM errors with deterministic errors
	if !llmResult.Valid {
		result.Valid = false
		result.Errors = append(result.Errors, llmResult.Errors...)
	} else if len(result.Errors) == 0 {
		result.Valid = true
	}

	return &result, nil
}

// GetPreviewBundle returns all project files and env vars for client-side WebContainer preview.
func (c *Controller) GetPreviewBundle(project *models.AppProject) (map[string]interface{}, error) {
	repo, err := c.GitRepoService.Open(project.ID.String())
	if err != nil {
		return nil, fmt.Errorf("no generated files found")
	}
	fileMap, err := repo.ReadAllFiles()
	if err != nil || len(fileMap) == 0 {
		return nil, fmt.Errorf("no generated files found")
	}

	envVars := c.CollectAllEnvVars(project)

	return map[string]interface{}{
		"files":    fileMap,
		"env_vars": envVars,
	}, nil
}

// injectAnalyticsScript adds the web analytics tracking script into the root
// layout file. For JSX files (.tsx/.jsx) it uses the JSX-safe
// dangerouslySetInnerHTML pattern so the parser doesn't choke on raw JS.
// The __PROJECT_ID__ placeholder is replaced with the actual project UUID.
// Note: the script body is a static, server-controlled string (not user input)
// so there is no XSS risk from using dangerouslySetInnerHTML here.
func injectAnalyticsScript(fileMap map[string]string, projectID string) {
	jsBody := strings.Replace(constants.WEB_ANALYTICS_SCRIPT_BODY, "__PROJECT_ID__", projectID, 1)

	// Escape backticks so the JS body is safe inside a JSX template literal
	escapedBody := strings.ReplaceAll(jsBody, "`", "\\`")

	// JSX-compatible: uses dangerouslySetInnerHTML so the JSX parser
	// doesn't try to interpret the JS as JSX children.
	jsxScript := `<script data-wemob-analytics dangerouslySetInnerHTML={{ __html: ` + "`" + escapedBody + "`" + ` }} />`

	layoutPaths := []string{
		"app/layout.tsx",
		"app/layout.jsx",
		"src/app/layout.tsx",
		"src/app/layout.jsx",
	}

	for _, path := range layoutPaths {
		content, ok := fileMap[path]
		if !ok {
			continue
		}

		// Prefer injecting before </head>
		if idx := strings.Index(content, "</head>"); idx != -1 {
			fileMap[path] = content[:idx] + jsxScript + "\n" + content[idx:]
			return
		}
		// Fallback: inject before </body>
		if idx := strings.Index(content, "</body>"); idx != -1 {
			fileMap[path] = content[:idx] + jsxScript + "\n" + content[idx:]
			return
		}
	}

	slog.Warn("analytics script injection: no suitable layout found", "project_id", projectID)
}

// CaptureAppScreenshot takes a desktop screenshot of the preview URL and uploads it to S3.
// Intended to be called in a goroutine (fire-and-forget).
func (c *Controller) CaptureAppScreenshot(project *models.AppProject, previewURL string) {
	// Wait for the preview to fully render before capturing
	time.Sleep(10 * time.Second)

	apiKey := c.SecretManager.GetSecret("SITESHOT_API_KEY")
	if apiKey == "" {
		slog.Warn("CaptureAppScreenshot: SITESHOT_API_KEY not configured")
		return
	}

	imgData, err := external.GetDesktopScreenshotOfURL(previewURL, apiKey)
	if err != nil {
		slog.Warn("CaptureAppScreenshot: failed to capture screenshot", "error", err, "project_id", project.ID)
		return
	}

	if len(imgData) == 0 {
		slog.Warn("CaptureAppScreenshot: empty screenshot data", "project_id", project.ID)
		return
	}

	s3Key := fmt.Sprintf("app-screenshots/%s.png", project.ID.String())
	if err := c.AssetsStorage.UploadFileByteWithContentType(context.Background(), s3Key, imgData, "image/png"); err != nil {
		slog.Warn("CaptureAppScreenshot: failed to upload to S3", "error", err, "project_id", project.ID)
		return
	}

	project.ScreenshotS3Key = s3Key
	if _, err := c.DB.UpdateAppProject(project); err != nil {
		slog.Warn("CaptureAppScreenshot: failed to update project", "error", err, "project_id", project.ID)
	}

	slog.Info("CaptureAppScreenshot: screenshot captured", "project_id", project.ID, "s3_key", s3Key)
}

// ensureV1ProjectForMobileApp creates a v1 Project (with real OneSignal IDs)
// and an initial Build record for the given mobile_app AppProject, unless one
// already exists for the same user+URL.
func (c *Controller) ensureV1ProjectForMobileApp(appProject *models.AppProject, user *models.User, deployedURL string) error {
	// Check if v1 Project already exists for this URL
	if existing, err := c.DB.GetProjectByUserAndURL(user.ID.String(), deployedURL); err == nil && existing.ID.String() != "" {
		return nil // already exists
	}

	pkHex, err := utils.RandomHex(5)
	if err != nil {
		return fmt.Errorf("failed to generate package name: %w", err)
	}
	pkName := "com.wemob.app" + pkHex[0:5]

	project := models.Project{
		UserID:      user.ID,
		URL:         deployedURL,
		PackageName: pkName,
		IconPath:    appProject.LogoS3Key,
	}

	// Create real OneSignal application
	onesignalAppID, err := external.CreateOneSignalApplication(project,
		c.SecretManager.GetSecret("ONESIGNAL_ORG_ID"),
		c.SecretManager.GetSecret("ONESIGNAL_ORG_API_KEY"),
		c.SecretManager.GetSecret("FCM_V1_SERVICE_ACCOUNT_JSON_B64"))
	if err != nil {
		return fmt.Errorf("OneSignal app creation failed: %w", err)
	}
	project.OneSignalAppID = onesignalAppID

	time.Sleep(2 * time.Second)

	onesignalAppToken, err := external.CreateOneSignalAppToken(project, c.SecretManager.GetSecret("ONESIGNAL_ORG_API_KEY"))
	if err != nil {
		return fmt.Errorf("OneSignal token creation failed: %w", err)
	}
	project.OneSignalAppToken = onesignalAppToken

	created, err := c.DB.CreateProject(&project)
	if err != nil {
		return fmt.Errorf("failed to create v1 project: %w", err)
	}

	// Create initial Build record so GetProject (v1 listing) can find a lastBuild
	build := &models.Build{
		ProjectID:     created.ID,
		Active:        false,
		Title:         appProject.Name,
		Icon:          appProject.LogoS3Key,
		NavBarStyle:   "default",
		BuildNumber:   1,
		VersionString: utils.CreateVersionString(1),
	}
	if _, err := c.DB.CreateProjectBuild(build); err != nil {
		return fmt.Errorf("failed to create initial build: %w", err)
	}

	slog.Info("Created v1 Project for mobile app",
		"app_project_id", appProject.ID,
		"project_id", created.ID,
		"url", deployedURL,
	)

	return nil
}

// BackfillV1ProjectsForMobileApps finds mobile_app AppProjects that have been
// deployed to web but don't have a corresponding v1 Project, and creates one
// for each. Intended to be called once at startup in a goroutine.
func (c *Controller) BackfillV1ProjectsForMobileApps() {
	// Find mobile_app AppProjects with a web deployment but no v1 Project for that URL.
	// We join app_deployments to get the deployed URL and left-join projects to check absence.
	type backfillRow struct {
		AppProjectID string `bun:"app_project_id"`
		UserID       string `bun:"user_id"`
		DeployedURL  string `bun:"deployed_url"`
		Name         string `bun:"name"`
		LogoS3Key    string `bun:"logo_s3_key"`
	}

	var rows []backfillRow
	err := c.DB.DB.NewRaw(`
		SELECT DISTINCT ON (ap.id)
			ap.id AS app_project_id,
			ap.user_id,
			ad.deployed_url,
			ap.name,
			ap.logo_s3_key
		FROM app_projects ap
		JOIN app_deployments ad ON ad.app_project_id = ap.id AND ad.platform = 'web' AND ad.deployed_url != ''
		LEFT JOIN projects p ON p.user_id = ap.user_id AND p.url = ad.deployed_url AND p.deleted_at IS NULL
		WHERE ap.project_type = 'mobile_app'
			AND ap.deleted_at IS NULL
			AND p.id IS NULL
		ORDER BY ap.id, ad.created_at DESC
	`).Scan(context.Background(), &rows)
	if err != nil {
		slog.Error("BackfillV1ProjectsForMobileApps: query failed", "error", err)
		return
	}

	if len(rows) == 0 {
		slog.Info("BackfillV1ProjectsForMobileApps: no mobile apps need backfill")
		return
	}

	slog.Info("BackfillV1ProjectsForMobileApps: starting", "count", len(rows))

	for _, row := range rows {
		appProject := &models.AppProject{
			Name:      row.Name,
			LogoS3Key: row.LogoS3Key,
		}
		appProject.ID = uuid.MustParse(row.AppProjectID)

		user := &models.User{}
		user.ID = uuid.MustParse(row.UserID)

		if err := c.ensureV1ProjectForMobileApp(appProject, user, row.DeployedURL); err != nil {
			slog.Error("BackfillV1ProjectsForMobileApps: failed for app",
				"app_project_id", row.AppProjectID,
				"error", err,
			)
			continue
		}
	}

	slog.Info("BackfillV1ProjectsForMobileApps: done", "processed", len(rows))
}

// replaceWithRealOneSignal replaces fake placeholder OneSignal IDs on a v1
// Project with real ones from the OneSignal API. Mutates the project in place
// (caller is responsible for persisting via UpdateProject).
func (c *Controller) replaceWithRealOneSignal(project *models.Project) error {
	onesignalAppID, err := external.CreateOneSignalApplication(*project,
		c.SecretManager.GetSecret("ONESIGNAL_ORG_ID"),
		c.SecretManager.GetSecret("ONESIGNAL_ORG_API_KEY"),
		c.SecretManager.GetSecret("FCM_V1_SERVICE_ACCOUNT_JSON_B64"))
	if err != nil {
		return fmt.Errorf("OneSignal app creation failed: %w", err)
	}
	project.OneSignalAppID = onesignalAppID

	time.Sleep(2 * time.Second)

	onesignalAppToken, err := external.CreateOneSignalAppToken(*project, c.SecretManager.GetSecret("ONESIGNAL_ORG_API_KEY"))
	if err != nil {
		return fmt.Errorf("OneSignal token creation failed: %w", err)
	}
	project.OneSignalAppToken = onesignalAppToken

	slog.Info("Replaced fake OneSignal IDs with real ones", "project_id", project.ID)
	return nil
}
