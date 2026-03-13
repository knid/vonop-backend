package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

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

	// Read files directly from DB — no AST codegen
	dbFiles, err := c.DB.GetAppGeneratedFilesByProjectID(project.ID.String(), "nextjs")
	if err != nil || len(dbFiles) == 0 {
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

	// Build file map from DB files
	fileMap := make(map[string]string)
	for _, f := range dbFiles {
		fileMap[f.FilePath] = f.Content
	}

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
		ASTSnapshot: project.AST,
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

	return deployment, nil
}

// BuildAppMobile creates a native mobile build for an AI-generated app by wrapping
// its Vercel deployment URL using the old URL-wrapper build system.
func (c *Controller) BuildAppMobile(appProject *models.AppProject, user *models.User, platform string, packageName string, appName string, icon string, splashColor string) (*models.Build, *models.AppBuildTask, error) {
	// Get the Vercel deployment URL — the app must be deployed to web first
	deployment, err := c.DB.GetLatestAppDeployment(appProject.ID.String(), "web")
	if err != nil || deployment.DeployedURL == "" {
		return nil, nil, fmt.Errorf("app must be deployed to web first")
	}

	wrappedURL := deployment.DeployedURL

	// Find or create an old-style Project for this URL
	project, err := c.DB.GetProjectByUserAndURL(user.ID.String(), wrappedURL)
	if err != nil {
		// Create new project linked to this AI app's URL
		project = &models.Project{
			UserID:          user.ID,
			URL:             wrappedURL,
			PackageName:     packageName,
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
		project.PackageName = packageName
		if icon != "" {
			project.IconPath = icon
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

	// Fetch mobile app config for permissions and deep linking
	mobileConfig, _ := c.DB.GetMobileAppConfig(project.ID.String())

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
			PackageName:       packageName,
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
			PackageName:       packageName,
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
	files, err := c.DB.GetAppGeneratedFilesByProjectID(projectID, "nextjs")
	if err != nil || len(files) == 0 {
		return nil, fmt.Errorf("no files found to validate")
	}

	// Build file map for deterministic checks
	fileMap := make(map[string]string)
	for _, f := range files {
		fileMap[f.FilePath] = f.Content
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
	for _, f := range files {
		fileContext.WriteString(fmt.Sprintf("\n--- %s (%s) ---\n%s\n", f.FilePath, f.FileType, f.Content))
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
	dbFiles, err := c.DB.GetAppGeneratedFilesByProjectID(project.ID.String(), "nextjs")
	if err != nil || len(dbFiles) == 0 {
		return nil, fmt.Errorf("no generated files found")
	}

	fileMap := make(map[string]string)
	for _, f := range dbFiles {
		fileMap[f.FilePath] = f.Content
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
