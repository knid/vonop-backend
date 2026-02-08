package controllers

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/tasks"

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
		vercelProject = &models.VercelProject{
			AppProjectID:    project.ID,
			VercelProjectID: vp.ID,
			Framework:       "nextjs",
		}
		vercelProject, _ = c.DB.CreateVercelProject(vercelProject)
	}

	// Collect env vars from integrations
	integrations, _ := c.DB.GetAppIntegrationsByProjectID(project.ID.String())
	envVars := make(map[string]string)
	for _, integ := range integrations {
		for k, v := range integ.EnvVars {
			envVars[k] = v
		}
	}

	// Set env vars on Vercel project
	if len(envVars) > 0 {
		if err := c.VercelService.SetEnvVars(vercelProject.VercelProjectID, envVars); err != nil {
			slog.Warn("Failed to set some env vars", "error", err)
		}
	}

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
				errorMessage = "Build failed on Vercel"
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
