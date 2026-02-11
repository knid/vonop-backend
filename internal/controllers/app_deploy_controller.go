package controllers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/preview"
	"github.com/knid/wemob/internal/tasks"
	"github.com/knid/wemob/internal/utils"

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
		vercelProject, _ = c.DB.CreateVercelProject(vercelProject)
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

	deployResp, err := c.VercelService.DeployFiles(vercelProject.VercelProjectID, fileMap, target)
	if err != nil {
		slog.Error("Failed to deploy to Vercel", "error", err)
		return nil, fmt.Errorf("deployment failed")
	}

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
		DeployedURL: "https://" + deployResp.URL,
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
	vercelProject.ProductionURL = "https://" + deployResp.URL
	c.DB.UpdateVercelProject(vercelProject)

	// Update project status
	c.UpdateAppProjectStatus(project, "deployed")

	return deployment, nil
}

// BuildFlutter enqueues a Flutter build task
func (c *Controller) BuildFlutter(project *models.AppProject, user *models.User, platform string, packageName string, appName string) (*models.FlutterBuild, error) {
	// Create build record
	build := &models.FlutterBuild{
		AppProjectID: project.ID,
		Platform:     platform,
		Status:       "pending",
		Version:      1,
		Config: map[string]any{
			"package_name": packageName,
			"app_name":     appName,
		},
	}

	latestBuilds, _ := c.DB.GetFlutterBuildsByProjectID(project.ID.String())
	for _, b := range latestBuilds {
		if b.Platform == platform && b.Version >= build.Version {
			build.Version = b.Version + 1
		}
	}

	build, err := c.DB.CreateFlutterBuild(build)
	if err != nil {
		return nil, fmt.Errorf("failed to create build record")
	}

	// Determine task type
	taskType := tasks.TypeBuildFlutterAndroid
	if platform == "ios" {
		taskType = tasks.TypeBuildFlutterIOS
	}

	// Enqueue build task
	payload, _ := json.Marshal(tasks.FlutterBuildPayload{
		AppProjectID: project.ID.String(),
		Platform:     platform,
		BuildID:      build.ID.String(),
		PackageName:  packageName,
		AppName:      appName,
		Version:      build.Version,
		Secret:       c.SecretManager.GetSecret("WORKER_SECRET"),
	})

	task := asynq.NewTask(taskType, payload)
	info, err := c.TaskService.Enqueue(task)
	if err != nil {
		build.Status = "failed"
		build.BuildLog = "Failed to enqueue build task: " + err.Error()
		c.DB.UpdateFlutterBuild(build)
		return build, fmt.Errorf("failed to enqueue build task")
	}

	build.TaskID = info.ID
	build.Status = "building"
	c.DB.UpdateFlutterBuild(build)

	// Create deployment record
	provider := "flutter_android"
	if platform == "ios" {
		provider = "flutter_ios"
	}

	c.DB.CreateAppDeployment(&models.AppDeployment{
		AppProjectID: project.ID,
		UserID:       user.ID,
		Platform:     platform,
		Provider:     provider,
		Status:       "building",
		Version:      build.Version,
		ASTSnapshot:  project.AST,
	})

	return build, nil
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
	apiKey := c.SecretManager.GetSecret("OPENROUTER_API_KEY")
	model := c.SecretManager.GetSecret("NEXTJS_BUILDER_LLM_MODEL")
	if model == "" {
		model = constants.DEFAULT_NEXTJS_LLM_MODEL
	}

	var fileContext strings.Builder
	fileContext.WriteString("Project files:\n")
	for _, f := range files {
		fileContext.WriteString(fmt.Sprintf("\n--- %s (%s) ---\n%s\n", f.FilePath, f.FileType, f.Content))
	}

	responseJSON, err := utils.GetJSONFromLLM(model, constants.NEXTJS_VALIDATE_PROMPT, fileContext.String(), apiKey)
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

// StartPreview creates a Docker-based preview for the project
func (c *Controller) StartPreview(project *models.AppProject) (*preview.PreviewStatus, error) {
	if c.PreviewService == nil {
		return nil, fmt.Errorf("preview service not configured")
	}

	// Read all files from DB
	dbFiles, err := c.DB.GetAppGeneratedFilesByProjectID(project.ID.String(), "nextjs")
	if err != nil || len(dbFiles) == 0 {
		return nil, fmt.Errorf("no generated files found for preview")
	}

	fileMap := make(map[string]string)
	for _, f := range dbFiles {
		fileMap[f.FilePath] = f.Content
	}

	// Collect all env vars (integrations + custom + auto-injected store vars)
	envVars := c.CollectAllEnvVars(project)

	status, err := c.PreviewService.CreatePreview(project.ID.String(), fileMap, envVars)
	if err != nil {
		slog.Error("Failed to start preview", "error", err, "project_id", project.ID)
		return nil, fmt.Errorf("failed to start preview")
	}

	return status, nil
}

// SyncPreviewFiles updates files in a running preview container
func (c *Controller) SyncPreviewFiles(projectID string) error {
	if c.PreviewService == nil {
		return nil
	}

	dbFiles, err := c.DB.GetAppGeneratedFilesByProjectID(projectID, "nextjs")
	if err != nil || len(dbFiles) == 0 {
		return fmt.Errorf("no files to sync")
	}

	fileMap := make(map[string]string)
	for _, f := range dbFiles {
		fileMap[f.FilePath] = f.Content
	}

	return c.PreviewService.UpdateFiles(projectID, fileMap)
}

// StopPreview stops and removes a preview container
func (c *Controller) StopPreview(projectID string) error {
	if c.PreviewService == nil {
		return fmt.Errorf("preview service not configured")
	}
	return c.PreviewService.DeletePreview(projectID)
}

// GetPreviewLogs returns container logs for the preview
func (c *Controller) GetPreviewLogs(projectID string) (string, error) {
	if c.PreviewService == nil {
		return "", fmt.Errorf("preview service not configured")
	}
	return c.PreviewService.GetLogs(projectID)
}

// GetPreviewStatus returns the current preview status
func (c *Controller) GetPreviewStatus(projectID string) (*preview.PreviewStatus, error) {
	if c.PreviewService == nil {
		return nil, fmt.Errorf("preview service not configured")
	}
	return c.PreviewService.GetStatus(projectID)
}
