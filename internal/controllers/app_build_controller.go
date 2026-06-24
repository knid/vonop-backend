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
	"github.com/knid/wemob/internal/observability"
	"github.com/knid/wemob/internal/utils"
	"github.com/knid/wemob/internal/utils/external"
)

func bundleIDForProject(project *models.AppProject, override string) string {
	if override != "" {
		return override
	}
	clean := strings.ReplaceAll(strings.ToLower(project.Slug), "-", "")
	return "com.wemob." + clean
}

// UndeployApp marks all deployments for this project as undeployed and
// enqueues a Fly app teardown so machines, IPs, and registry images stop
// accumulating on canceled accounts.
func (c *Controller) UndeployApp(projectID string) error {
	deployments, err := c.DB.GetAppDeploymentsByProjectID(projectID)
	if err != nil {
		return fmt.Errorf("failed to get deployments: %w", err)
	}
	// Include in-progress states (queued, building, deploying) so a user
	// who undeploys mid-build doesn't leave rows orphaned in their pre-
	// terminal state forever (the partial unique index would then refuse
	// the next deploy).
	for _, d := range deployments {
		switch d.Status {
		case "deployed", "queued", "building", "deploying":
			_, err := c.DB.DB.NewUpdate().
				Model((*models.AppDeployment)(nil)).
				Set("status = ?", "undeployed").
				Set("updated_at = ?", time.Now()).
				Where("id = ?", d.ID).
				Exec(context.Background())
			if err != nil {
				slog.Error("failed to update deployment status", "error", err, "deployment_id", d.ID)
			}
		}
	}

	if runtimeApp, raErr := c.DB.GetRuntimeAppByAppProjectID(projectID); raErr == nil && runtimeApp != nil && runtimeApp.FlyAppName != "" {
		if err := c.EnqueueFlyDelete(runtimeApp.FlyAppName, runtimeApp.ID); err != nil {
			slog.Warn("failed to enqueue Fly app delete on undeploy", "error", err, "fly_app", runtimeApp.FlyAppName, "project_id", projectID)
		}
	}
	return nil
}

// GetAppDeployments returns all deployments for a project
func (c *Controller) GetAppDeployments(projectID string) ([]models.AppDeployment, error) {
	return c.DB.GetAppDeploymentsByProjectID(projectID)
}

// GetLatestDeploymentStatus returns the latest web AppDeployment for a
// project.
func (c *Controller) GetLatestDeploymentStatus(projectID string) (*models.AppDeployment, error) {
	return c.DB.GetLatestAppDeployment(projectID, "web")
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

// ValidateAppFiles runs LLM-based validation on project files.
// Runtime sandbox verification catches build errors, making a static
// pre-flight layer redundant.
//
// `user` is optional and only used for PostHog attribution. Pass nil to skip
// the trace.
func (c *Controller) ValidateAppFiles(ctx context.Context, projectID string, user *models.User) (*AppValidationResult, error) {
	repo, err := c.GitRepoService.Open(projectID)
	if err != nil {
		return nil, fmt.Errorf("no files found to validate")
	}
	fileMap, err := repo.ReadAllFiles()
	if err != nil || len(fileMap) == 0 {
		return nil, fmt.Errorf("no files found to validate")
	}

	result := AppValidationResult{Valid: true}

	// Run LLM-based validation
	provider := c.llmProviderForUser(user)
	model := c.builderModel(provider)

	var fileContext strings.Builder
	fileContext.WriteString("Project files:\n")
	const maxValidationBytes = 100_000
	totalBytes := 0
	filesWritten := 0
	for path, content := range fileMap {
		size := len(path) + len(content) + 30 // overhead for "--- path (type) ---\n"
		if totalBytes+size > maxValidationBytes {
			remaining := len(fileMap) - filesWritten
			if remaining > 0 {
				fileContext.WriteString(fmt.Sprintf("\n... (%d more files omitted to stay within token budget)\n", remaining))
			}
			break
		}
		totalBytes += size
		filesWritten++
		fileContext.WriteString(fmt.Sprintf("\n--- %s (%s) ---\n%s\n", path, InferFileType(path), content))
	}

	tracker := utils.NewTokenTracker()
	if user != nil {
		tracker.UserID = user.ID.String()
		tracker.ProjectID = projectID
		if orgID, orgErr := c.ResolveOrgForProjectID(user, projectID); orgErr == nil {
			tracker.OrganizationID = orgID.String()
		}
	}
	llmCtx := utils.WithTokenTracker(ctx, tracker)
	systemPrompt := c.compileLangfuseTextPrompt(llmCtx, observability.PromptValidation, constants.NEXTJS_VALIDATE_PROMPT, nil)
	tracker.Prompt = observability.PromptLink{Name: systemPrompt.Name, Version: systemPrompt.Version}

	responseJSON, err := utils.GetJSONFromLLM(llmCtx, model, systemPrompt.Text, fileContext.String(), provider)
	if user != nil {
		c.RecordTokenUsage(user, "validate_app_files", model, provider.Name, projectID, "validate", tracker)
	}
	if err != nil {
		slog.Error("validation LLM call failed", "error", err)
		return &result, nil
	}

	var llmResult AppValidationResult
	if err := json.Unmarshal([]byte(responseJSON), &llmResult); err != nil {
		cleaned := utils.ExtractJSON(responseJSON)
		if err2 := json.Unmarshal([]byte(cleaned), &llmResult); err2 != nil {
			slog.Warn("failed to parse validation response", "error", err2)
			return &result, nil
		}
	}

	if !llmResult.Valid {
		result.Valid = false
		result.Errors = append(result.Errors, llmResult.Errors...)
	}

	return &result, nil
}

// GetPreviewBundle returns all project files and env vars for the sandbox
// preview. The shape varies by project_type:
//
//   - vite_react: returns the frontend/ subtree with the prefix stripped so
//     the existing sandbox can run npm scripts from its root. backend/ files
//     are returned separately for split-aware clients.
//   - frontend_runtime=none: returns only the backend/ subtree with the prefix
//     stripped so internal backend-only previews run against the API app.
//   - mobile_app_native: returns only the mobile/ subtree (with the prefix
//     stripped) so the sandbox runs `npx expo start --web` against the
//     Expo project directly. Next.js-specific patches (custom error pages,
//     analytics script) are skipped because the sandbox runs Metro/Expo,
//     not Next.js.
//   - everything else: returns the full file map with Next.js patches.
//
// project_type is included so sandbox setup can branch by dev-server command
// and readiness port without an extra backend round-trip.
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

	if project.UsesSplitWebRuntime() {
		frontendFiles := stripPreviewSubtree(fileMap, "frontend/")
		if len(frontendFiles) == 0 {
			return nil, fmt.Errorf("no frontend files found")
		}
		return map[string]interface{}{
			"files":            frontendFiles,
			"backend_files":    stripPreviewSubtree(fileMap, "backend/"),
			"env_vars":         envVars,
			"project_type":     project.ProjectType,
			"frontend_runtime": project.FrontendRuntime,
			"frontend_root":    "frontend",
			"backend_enabled":  project.BackendEnabled,
			"backend_runtime":  project.BackendRuntime,
		}, nil
	}

	if project.FrontendRuntime == models.FrontendRuntimeNone {
		backendFiles := stripPreviewSubtree(fileMap, "backend/")
		if len(backendFiles) == 0 {
			return nil, fmt.Errorf("no backend files found")
		}
		return map[string]interface{}{
			"files":            backendFiles,
			"env_vars":         envVars,
			"project_type":     project.ProjectType,
			"frontend_runtime": project.FrontendRuntime,
			"backend_enabled":  project.BackendEnabled,
			"backend_runtime":  project.BackendRuntime,
			"backend_root":     "backend",
		}, nil
	}

	if project.ProjectType == models.ProjectTypeMobileAppNative {
		mobileFiles := stripPreviewSubtree(fileMap, "mobile/")
		return map[string]interface{}{
			"files":        mobileFiles,
			"env_vars":     envVars,
			"project_type": project.ProjectType,
		}, nil
	}

	// Defensive: ensure the preview bundle always ships custom error pages so
	// users never see Next.js's default dev error UI in the iframe. Only writes
	// files that are missing — user-customized versions are preserved.
	injectCustomErrorPages(fileMap, constants.DetectLanguage(project.Prompt))

	return map[string]interface{}{
		"files":        fileMap,
		"env_vars":     envVars,
		"project_type": project.ProjectType,
	}, nil
}

func stripPreviewSubtree(fileMap map[string]string, prefix string) map[string]string {
	files := make(map[string]string)
	for path, content := range fileMap {
		if strings.HasPrefix(path, prefix) {
			files[strings.TrimPrefix(path, prefix)] = content
		}
	}
	return files
}

// injectCustomErrorPages writes app/error.tsx, app/not-found.tsx, and
// app/global-error.tsx into the file map if they're missing. Supports both the
// "app/" and "src/app/" directory layouts — whichever one the project uses is
// detected from the existing layout file location.
func injectCustomErrorPages(fileMap map[string]string, lang string) {
	// Detect app-directory layout from existing layout file
	prefix := "app/"
	if _, ok := fileMap["src/app/layout.tsx"]; ok {
		prefix = "src/app/"
	} else if _, ok := fileMap["src/app/layout.jsx"]; ok {
		prefix = "src/app/"
	}

	writeIfMissing := func(path, content string) {
		if _, exists := fileMap[path]; !exists {
			fileMap[path] = content
		}
	}

	writeIfMissing(prefix+"error.tsx", constants.GetErrorTemplate(lang))
	writeIfMissing(prefix+"not-found.tsx", constants.GetNotFoundTemplate(lang))
	writeIfMissing(prefix+"global-error.tsx", constants.GetGlobalErrorTemplate(lang))
}

// injectWemobBadge injects a "Created by Wemob" badge script into the root layout.
// Uses a hardcoded constant (not user input) so there is no XSS risk from
// using dangerouslySetInnerHTML here.
func injectWemobBadge(fileMap map[string]string) {
	escapedBody := strings.ReplaceAll(constants.WEMOB_BADGE_SCRIPT, "`", "\\`")
	jsxScript := `<script data-wemob-badge-loader dangerouslySetInnerHTML={{ __html: ` + "`" + escapedBody + "`" + ` }}></script>`

	layoutPaths := []string{
		"app/layout.tsx",
		"app/layout.jsx",
		"src/app/layout.tsx",
		"src/app/layout.jsx",
		"backend/app/layout.tsx",
		"backend/app/layout.jsx",
		"backend/src/app/layout.tsx",
		"backend/src/app/layout.jsx",
	}

	for _, path := range layoutPaths {
		content, ok := fileMap[path]
		if !ok {
			continue
		}
		if idx := strings.Index(content, "</body>"); idx != -1 {
			fileMap[path] = content[:idx] + jsxScript + "\n" + content[idx:]
			return
		}
	}
}

// injectWemobAnalytics injects a lightweight JS tracker into the root layout of
// deployed apps. The script captures pageviews, pageleaves (with duration),
// referrer, UTM params, screen size, and language, and POSTs them to the
// analytics collect endpoint. projectID is the AppProject UUID.
func injectWemobAnalytics(fileMap map[string]string, projectID string) {
	script := strings.ReplaceAll(constants.WEMOB_ANALYTICS_SCRIPT, "__WEMOB_PROJECT_ID__", projectID)
	escapedBody := strings.ReplaceAll(script, "`", "\\`")
	prop := "dangerouslySetInnerHTML"
	jsxScript := `<script data-wemob-analytics ` + prop + `={{ __html: ` + "`" + escapedBody + "`" + ` }}></script>`

	layoutPaths := []string{
		"app/layout.tsx",
		"app/layout.jsx",
		"src/app/layout.tsx",
		"src/app/layout.jsx",
		"backend/app/layout.tsx",
		"backend/app/layout.jsx",
		"backend/src/app/layout.tsx",
		"backend/src/app/layout.jsx",
	}

	for _, path := range layoutPaths {
		content, ok := fileMap[path]
		if !ok {
			continue
		}
		if idx := strings.Index(content, "</body>"); idx != -1 {
			fileMap[path] = content[:idx] + jsxScript + "\n" + content[idx:]
			return
		}
	}
}

func injectViteStaticScripts(fileMap map[string]string, projectID string, includeBadge bool) {
	const indexPath = "frontend/index.html"
	content, ok := fileMap[indexPath]
	if !ok {
		return
	}

	var scripts []string
	if includeBadge && !strings.Contains(content, "data-wemob-badge") {
		scripts = append(scripts, inlineScriptTag("wemob-badge-loader", constants.WEMOB_BADGE_SCRIPT))
	}
	if !strings.Contains(content, "data-wemob-analytics") {
		script := strings.ReplaceAll(constants.WEMOB_ANALYTICS_SCRIPT, "__WEMOB_PROJECT_ID__", projectID)
		scripts = append(scripts, inlineScriptTag("wemob-analytics", script))
	}
	if len(scripts) == 0 {
		return
	}

	injection := strings.Join(scripts, "\n") + "\n"
	lower := strings.ToLower(content)
	if idx := strings.LastIndex(lower, "</body>"); idx >= 0 {
		fileMap[indexPath] = content[:idx] + injection + content[idx:]
		return
	}
	fileMap[indexPath] = content + "\n" + injection
}

func inlineScriptTag(name, body string) string {
	body = strings.ReplaceAll(body, "</script", "<\\/script")
	return `<script data-` + name + `>` + body + `</script>`
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

// BackendDeployURL returns the URL where the project's backend is (or will
// be) deployed. Used by scaffolding and deploy helpers so docs and runtime
// configuration stay in sync with reality.
//
// Resolution order:
//  1. RuntimeApp.CustomDomain (set on first deploy; equals {slug}.wemob.app
//     by default but extensible for future user-bring-your-own-domain support)
//  2. Canonical {slug}.{WEMOB_APP_BASE_DOMAIN} fallback (used for first-build
//     before any deploy lands the runtime app row)
//
// Always returns an https:// scheme. Returns "" only when project is nil.
func (c *Controller) BackendDeployURL(project *models.AppProject) string {
	if project == nil {
		return ""
	}
	if c.DB != nil {
		if ra, err := c.DB.GetRuntimeAppByAppProjectID(project.ID.String()); err == nil && ra != nil && ra.CustomDomain != "" {
			return "https://" + ra.CustomDomain
		}
	}
	return "https://" + project.Slug + "." + constants.WEMOB_APP_BASE_DOMAIN
}

// WebPreviewURL returns the URL where the project's Expo Web preview is
// served. P1.3 locked architecture: same Fly app as the Next.js backend,
// with the web export bundled into the Next.js public/_web/ directory at
// deploy time (see web_deploy_controller.go for the bake step — a follow-up
// in P1.3.B). The studio's MobilePreview pane points an iframe at this URL.
//
// Builds atop BackendDeployURL so a custom domain on RuntimeApp transparently
// flows through. Returns "" only when project is nil — the dashboard treats
// "" as "render the empty state".
func (c *Controller) WebPreviewURL(project *models.AppProject) string {
	base := c.BackendDeployURL(project)
	if base == "" {
		return ""
	}
	return base + "/_web/"
}
