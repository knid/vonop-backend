package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

type appFileStructure struct {
	Name            string             `json:"name"`
	Slug            string             `json:"slug"`
	ColorScheme     map[string]any     `json:"colorScheme"`
	DarkColorScheme map[string]any     `json:"darkColorScheme,omitempty"`
	Typography      map[string]any     `json:"typography,omitempty"`
	Files           []appFilePlanEntry `json:"files"`
}

type appFilePlanEntry struct {
	Path        string `json:"path"`
	Type        string `json:"type"`
	Description string `json:"description"`
}

// PlanAppFileStructure calls LLM to generate a file plan for the app
func (c *Controller) PlanAppFileStructure(ctx context.Context, prompt string, projectType string) (*appFileStructure, error) {
	apiKey := c.SecretManager.GetSecret("OPENROUTER_API_KEY")
	model := c.SecretManager.GetSecret("NEXTJS_BUILDER_LLM_MODEL")
	if model == "" {
		model = constants.DEFAULT_NEXTJS_LLM_MODEL
	}

	systemPrompt := constants.NEXTJS_STRUCTURE_PROMPT
	if projectType == "ecommerce" {
		systemPrompt = constants.NEXTJS_ECOMMERCE_STRUCTURE_PROMPT
	}

	structureJSON, err := utils.GetJSONFromLLM(model, systemPrompt, prompt, apiKey)
	if err != nil {
		slog.Error("failed to generate app structure", "error", err)
		return nil, fmt.Errorf("failed to generate app structure. try again please")
	}

	var structure appFileStructure
	if err := json.Unmarshal([]byte(structureJSON), &structure); err != nil {
		slog.Error("failed to parse app structure", "error", err, "json", structureJSON)
		return nil, fmt.Errorf("failed to parse app structure. try again please")
	}

	if len(structure.Files) == 0 {
		return nil, fmt.Errorf("failed to generate app files. try again please")
	}

	return &structure, nil
}

// PlanAppFileStructureStream is the streaming variant of PlanAppFileStructure.
// It sends planning chunks via onChunk as the LLM generates the structure.
func (c *Controller) PlanAppFileStructureStream(ctx context.Context, prompt string, projectType string, onChunk func(chunk string)) (*appFileStructure, error) {
	apiKey := c.SecretManager.GetSecret("OPENROUTER_API_KEY")
	model := c.SecretManager.GetSecret("NEXTJS_BUILDER_LLM_MODEL")
	if model == "" {
		model = constants.DEFAULT_NEXTJS_LLM_MODEL
	}

	systemPrompt := constants.NEXTJS_STRUCTURE_PROMPT
	if projectType == "ecommerce" {
		systemPrompt = constants.NEXTJS_ECOMMERCE_STRUCTURE_PROMPT
	}

	structureJSON, err := utils.StreamJSONFromLLM(ctx, model, systemPrompt, prompt, apiKey, onChunk)
	if err != nil {
		slog.Error("failed to generate app structure", "error", err)
		return nil, fmt.Errorf("failed to generate app structure. try again please")
	}

	var structure appFileStructure
	if err := json.Unmarshal([]byte(structureJSON), &structure); err != nil {
		slog.Error("failed to parse app structure", "error", err, "json", structureJSON)
		return nil, fmt.Errorf("failed to parse app structure. try again please")
	}

	if len(structure.Files) == 0 {
		return nil, fmt.Errorf("failed to generate app files. try again please")
	}

	return &structure, nil
}

// PlanAppFileStructureFromScreenshot uses vision LLM to plan files from a screenshot
func (c *Controller) PlanAppFileStructureFromScreenshot(imageBase64 string) (*appFileStructure, error) {
	apiKey := c.SecretManager.GetSecret("OPENROUTER_API_KEY")
	model := c.SecretManager.GetSecret("NEXTJS_BUILDER_LLM_MODEL")
	if model == "" {
		model = constants.DEFAULT_NEXTJS_LLM_MODEL
	}

	response, err := utils.GetJSONFromLLMWithVision(model, constants.NEXTJS_SCREENSHOT_TO_FILES_PROMPT, "Analyze this screenshot and generate the file structure plan.", imageBase64, apiKey)
	if err != nil {
		return nil, fmt.Errorf("failed to analyze screenshot")
	}

	var structure appFileStructure
	if err := json.Unmarshal([]byte(response), &structure); err != nil {
		return nil, fmt.Errorf("failed to parse generated structure")
	}

	return &structure, nil
}

// getAppFilePrompt returns the appropriate LLM prompt for a file type
func (c *Controller) getAppFilePrompt(fileType string) string {
	switch fileType {
	case "css":
		return constants.NEXTJS_FILE_CSS_PROMPT
	case "json":
		return constants.NEXTJS_FILE_JSON_PROMPT
	case "ts":
		return constants.NEXTJS_FILE_CONFIG_PROMPT
	default: // tsx, ts components
		return constants.NEXTJS_FILE_TSX_PROMPT
	}
}

// getAppFileFallback returns fallback content if LLM generation fails for a file
func (c *Controller) getAppFileFallback(filePlan appFilePlanEntry) string {
	switch filePlan.Type {
	case "tsx":
		return fmt.Sprintf("export default function Page() {\n  return <div><h1>%s</h1><p>Generation failed. Please edit this file.</p></div>\n}", filePlan.Description)
	case "css":
		return "@tailwind base;\n@tailwind components;\n@tailwind utilities;\n\n/* Generation failed. Please edit manually. */"
	case "json":
		return `{"name":"app","version":"0.1.0","private":true,"scripts":{"dev":"next dev","build":"next build","start":"next start"},"dependencies":{"next":"^14.0.0","react":"^18.0.0","react-dom":"^18.0.0"},"devDependencies":{"typescript":"^5.0.0","@types/react":"^18.0.0","@types/node":"^20.0.0","tailwindcss":"^3.4.0","postcss":"^8.0.0","autoprefixer":"^10.0.0"}}`
	case "ts":
		return "// Config generation failed. Please edit manually."
	default:
		return ""
	}
}

// StreamGenerateAppFiles generates app files one by one with SSE streaming.
// Mirrors the GenerateWebsiteFilesStream pattern.
func (c *Controller) StreamGenerateAppFiles(ctx context.Context, project *models.AppProject, user *models.User, prompt string, projectType string, events chan<- responses.SSEEvent) {
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

	apiKey := c.SecretManager.GetSecret("OPENROUTER_API_KEY")
	model := c.SecretManager.GetSecret("NEXTJS_BUILDER_LLM_MODEL")
	if model == "" {
		model = constants.DEFAULT_NEXTJS_LLM_MODEL
	}

	// Step 1: Plan file structure (streamed)
	structure, err := c.PlanAppFileStructureStream(ctx, prompt, projectType, func(chunk string) {
		sendEvent("planning_chunk", responses.PlanningChunkEvent{
			Chunk: chunk,
			Phase: "structure",
		})
	})
	if err != nil {
		sendError("structure", err.Error())
		return
	}

	// Step 2: Update project with name/slug/theme from structure
	if structure.Name != "" {
		project.Name = structure.Name
	}
	if structure.Slug != "" {
		project.Slug = structure.Slug + "-" + uuid.New().String()[:8]
	}

	// Store theme
	themeData := models.AppTheme{
		ColorScheme:     structure.ColorScheme,
		DarkColorScheme: structure.DarkColorScheme,
		Typography:      structure.Typography,
	}
	themeJSON, _ := json.Marshal(themeData)
	project.Theme = themeJSON
	project, _ = c.DB.UpdateAppProject(project)

	// Step 3: Send project_created event with file plan
	filePaths := make([]string, len(structure.Files))
	for i, f := range structure.Files {
		filePaths[i] = f.Path
	}

	if !sendEvent("project_created", map[string]any{
		"id":         project.ID,
		"name":       project.Name,
		"slug":       project.Slug,
		"files":      filePaths,
		"totalFiles": len(structure.Files),
	}) {
		return
	}

	// Step 4: Create conversation early with user message (so it persists even if generation fails)
	convMessages := []models.AppConversationMessage{
		{Role: "user", Content: project.Prompt, Timestamp: time.Now().Unix()},
	}
	convJSON, _ := json.Marshal(convMessages)
	conv, convErr := c.DB.CreateAppConversation(&models.AppConversation{
		AppProjectID: project.ID,
		Messages:     convJSON,
	})
	if convErr != nil {
		slog.Error("failed to create conversation", "error", convErr, "project_id", project.ID)
	}

	// Step 5: Delete existing generated files for this project
	c.DB.DeleteAppGeneratedFilesByProjectPlatform(project.ID.String(), "nextjs")

	// Step 6: Generate each file with streaming
	colorSchemeJSON, _ := json.Marshal(structure.ColorScheme)
	allFilesContext := strings.Join(filePaths, ", ")

	for _, filePlan := range structure.Files {
		if ctx.Err() != nil {
			return
		}

		prompt := c.getAppFilePrompt(filePlan.Type)
		filePrompt := fmt.Sprintf(
			"App name: %s\nColor scheme: %s\nTypography: %v\nAll files: %s\nCurrent file: %s\nDescription: %s\n\nUser's original description: %s",
			structure.Name,
			string(colorSchemeJSON),
			structure.Typography,
			allFilesContext,
			filePlan.Path,
			filePlan.Description,
			project.Prompt,
		)

		content, err := utils.StreamTextFromLLM(
			ctx,
			model,
			prompt,
			filePrompt,
			apiKey,
			func(chunk string) {
				sendEvent("file_chunk", responses.FileOperationEvent{
					Action:   "create",
					FilePath: filePlan.Path,
					FileType: filePlan.Type,
					Chunk:    chunk,
				})
			},
		)
		if err != nil {
			slog.Error("failed to generate app file", "error", err, "path", filePlan.Path)
			content = c.getAppFileFallback(filePlan)
		}

		content = cleanAppFileOutput(content, filePlan.Type)

		// Strip forbidden imports (tailwindcss, Node.js builtins, etc.)
		if filePlan.Type == "tsx" || filePlan.Type == "ts" {
			content, _ = stripForbiddenImports(content, filePlan.Path)
		}

		// Save to DB
		file := models.AppGeneratedFile{
			AppProjectID: project.ID,
			Platform:     "nextjs",
			FilePath:     filePlan.Path,
			Content:      content,
			FileType:     filePlan.Type,
			IsGenerated:  true,
		}

		createdFile, err := c.DB.CreateAppGeneratedFile(&file)
		if err != nil {
			slog.Error("failed to save app file", "error", err, "path", filePlan.Path)
			continue
		}

		if !sendEvent("file_complete", responses.FileCompleteEvent{
			Action:   "create",
			FilePath: filePlan.Path,
			FileID:   createdFile.ID,
			FileType: filePlan.Type,
		}) {
			return
		}
	}

	// Step 6b: Validate imports — find missing files and generate them
	generatedFiles := make(map[string]string)
	for _, filePlan := range structure.Files {
		dbFile, err := c.DB.GetAppGeneratedFileByPath(project.ID.String(), "nextjs", filePlan.Path)
		if err == nil && dbFile != nil {
			generatedFiles[filePlan.Path] = dbFile.Content
		}
	}

	missingImports := findMissingImports(generatedFiles)
	if len(missingImports) > 0 {
		slog.Info("Found missing imports, generating files", "project_id", project.ID, "missing", missingImports)

		for _, importPath := range missingImports {
			if ctx.Err() != nil {
				return
			}

			filePath, fileType := guessFileType(importPath)
			prompt := c.getAppFilePrompt(fileType)

			// Build context about what files import this missing module
			importedBy := []string{}
			for path, content := range generatedFiles {
				if strings.Contains(content, "@/"+importPath) {
					importedBy = append(importedBy, path)
				}
			}

			filePrompt := fmt.Sprintf(
				"App name: %s\nColor scheme: %s\nTypography: %v\nAll files: %s\nCurrent file: %s\nDescription: This file is imported by: %s. Generate the implementation based on how it is used in those files.\n\nUser's original description: %s",
				structure.Name,
				string(colorSchemeJSON),
				structure.Typography,
				allFilesContext+", "+filePath,
				filePath,
				strings.Join(importedBy, ", "),
				project.Prompt,
			)

			content, err := utils.StreamTextFromLLM(
				ctx,
				model,
				prompt,
				filePrompt,
				apiKey,
				func(chunk string) {
					sendEvent("file_chunk", responses.FileOperationEvent{
						Action:   "create",
						FilePath: filePath,
						FileType: fileType,
						Chunk:    chunk,
					})
				},
			)
			if err != nil {
				slog.Error("failed to generate missing import file", "error", err, "path", filePath)
				// Generate a minimal export stub so the build doesn't fail
				if fileType == "tsx" {
					baseName := importPath
					if idx := strings.LastIndex(importPath, "/"); idx >= 0 {
						baseName = importPath[idx+1:]
					}
					// Convert kebab-case to PascalCase for component name
					parts := strings.Split(baseName, "-")
					for i, p := range parts {
						if len(p) > 0 {
							parts[i] = strings.ToUpper(p[:1]) + p[1:]
						}
					}
					componentName := strings.Join(parts, "")
					content = fmt.Sprintf("export default function %s() {\n  return <div>%s</div>\n}", componentName, importPath)
				} else {
					content = "// Auto-generated stub\nexport {}"
				}
			}

			content = cleanAppFileOutput(content, fileType)

			file := models.AppGeneratedFile{
				AppProjectID: project.ID,
				Platform:     "nextjs",
				FilePath:     filePath,
				Content:      content,
				FileType:     fileType,
				IsGenerated:  true,
			}

			createdFile, err := c.DB.CreateAppGeneratedFile(&file)
			if err != nil {
				slog.Error("failed to save missing import file", "error", err, "path", filePath)
				continue
			}

			generatedFiles[filePath] = content

			sendEvent("file_complete", responses.FileCompleteEvent{
				Action:   "create",
				FilePath: filePath,
				FileID:   createdFile.ID,
				FileType: fileType,
			})
		}
	}

	// Step 6c: Ensure package.json includes lucide-react
	if fixedPkgJSON := ensureLucideReact(generatedFiles); fixedPkgJSON != "" {
		dbFile, err := c.DB.GetAppGeneratedFileByPath(project.ID.String(), "nextjs", "package.json")
		if err == nil && dbFile != nil {
			dbFile.Content = fixedPkgJSON
			c.DB.UpdateAppGeneratedFile(dbFile)
			slog.Info("Added lucide-react to package.json", "project_id", project.ID)
		}
	}

	// Step 7: Update conversation with assistant message
	if conv != nil {
		assistantMsg := fmt.Sprintf("Created app \"%s\" with %d files.", project.Name, len(structure.Files)+len(missingImports))
		convMessages = []models.AppConversationMessage{
			{Role: "user", Content: project.Prompt, Timestamp: time.Now().Unix()},
			{Role: "assistant", Content: assistantMsg, Timestamp: time.Now().Unix()},
		}
		convJSON, _ = json.Marshal(convMessages)
		conv.Messages = convJSON
		if _, err := c.DB.UpdateAppConversation(conv); err != nil {
			slog.Error("failed to update conversation", "error", err, "project_id", project.ID)
		}
	}

	// Step 8: Update project status
	c.UpdateAppProjectStatus(project, "ready")

	// Step 9: Auto-deploy to Vercel
	if c.VercelService != nil {
		sendEvent("deploying", map[string]string{"status": "starting"})

		deployment, err := c.DeployToVercel(project, user, "production")
		if err != nil {
			slog.Error("Auto-deploy failed", "project_id", project.ID, "error", err)
			sendEvent("deploy_error", map[string]string{"error": err.Error()})
		} else {
			sendEvent("deployed", responses.AppDeploymentResponse{
				ID:           deployment.ID,
				AppProjectID: deployment.AppProjectID,
				Platform:     deployment.Platform,
				Provider:     deployment.Provider,
				Status:       deployment.Status,
				DeployedURL:  deployment.DeployedURL,
				Version:      deployment.Version,
				CreatedAt:    deployment.CreatedAt,
				UpdatedAt:    deployment.UpdatedAt,
			})
		}
	}

	// Step 9: Send done event
	sendEvent("done", map[string]any{
		"project_id": project.ID,
		"success":    true,
		"message":    "app generation completed",
	})
}

// importPathRegex matches import statements like: import X from '@/components/foo' or from "@/lib/bar"
var importPathRegex = regexp.MustCompile(`from\s+['"]@/([^'"]+)['"]`)

// forbiddenImportRegex matches imports of build-time/Node.js packages in component files
var forbiddenImportRegex = regexp.MustCompile(`(?m)^import\s+.*\s+from\s+['"](tailwindcss|tailwindcss/.*|postcss|autoprefixer|eslint|webpack|jiti|fs|path|os|module|crypto|stream|child_process|net|http|https|url|util|assert|buffer|cluster|dgram|dns|events|querystring|readline|repl|string_decoder|tls|tty|v8|vm|zlib)['"].*$`)

// stripForbiddenImports removes import lines for build-time/Node.js packages from component code.
// These packages cannot be bundled for the browser and cause build failures.
func stripForbiddenImports(content string, filePath string) (string, bool) {
	matches := forbiddenImportRegex.FindAllString(content, -1)
	if len(matches) == 0 {
		return content, false
	}

	cleaned := forbiddenImportRegex.ReplaceAllString(content, "// [removed: build-time import not allowed in browser code]")
	slog.Warn("Stripped forbidden imports from generated file", "path", filePath, "removed", matches)
	return cleaned, true
}

// findMissingImports scans all generated files for @/ imports and returns paths that don't exist in the file set.
func findMissingImports(files map[string]string) []string {
	// Build a set of known file paths (without extensions for flexible matching)
	knownPaths := make(map[string]bool)
	for path := range files {
		knownPaths[path] = true
		// Also register without extension for import resolution
		withoutExt := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(path, ".tsx"), ".ts"), ".js")
		knownPaths[withoutExt] = true
		// Handle index files: components/foo/index.tsx -> components/foo
		if strings.HasSuffix(withoutExt, "/index") {
			knownPaths[strings.TrimSuffix(withoutExt, "/index")] = true
		}
	}

	missingSet := make(map[string]bool)
	for _, content := range files {
		matches := importPathRegex.FindAllStringSubmatch(content, -1)
		for _, match := range matches {
			importPath := match[1]
			// Check if the import resolves to any known file
			if !knownPaths[importPath] &&
				!knownPaths[importPath+".tsx"] &&
				!knownPaths[importPath+".ts"] &&
				!knownPaths[importPath+"/index.tsx"] &&
				!knownPaths[importPath+"/index.ts"] {
				missingSet[importPath] = true
			}
		}
	}

	missing := make([]string, 0, len(missingSet))
	for path := range missingSet {
		missing = append(missing, path)
	}
	return missing
}

// guessFileType returns the likely file type for a missing import path
func guessFileType(importPath string) (filePath string, fileType string) {
	if strings.HasPrefix(importPath, "lib/") || strings.HasPrefix(importPath, "utils/") || strings.HasPrefix(importPath, "data/") || strings.HasPrefix(importPath, "types/") {
		return importPath + ".ts", "ts"
	}
	return importPath + ".tsx", "tsx"
}

// ensureLucideReact checks if package.json includes lucide-react and adds it if missing
func ensureLucideReact(files map[string]string) string {
	pkgJSON, ok := files["package.json"]
	if !ok {
		return ""
	}

	if strings.Contains(pkgJSON, "lucide-react") {
		return "" // already present
	}

	// Simple string insertion: add lucide-react to dependencies
	pkgJSON = strings.Replace(pkgJSON,
		`"react-dom"`,
		`"react-dom"`,
		1)

	// Try to add after "react-dom" line in dependencies
	lines := strings.Split(pkgJSON, "\n")
	var result []string
	for _, line := range lines {
		result = append(result, line)
		if strings.Contains(line, `"react-dom"`) && strings.Contains(line, ":") {
			// Add comma to current line if not present, then add lucide-react
			trimmed := strings.TrimRight(line, " \t")
			if !strings.HasSuffix(trimmed, ",") {
				result[len(result)-1] = trimmed + ","
			}
			// Match indentation
			indent := ""
			for _, ch := range line {
				if ch == ' ' || ch == '\t' {
					indent += string(ch)
				} else {
					break
				}
			}
			result = append(result, indent+`"lucide-react": "^0.460.0"`)
		}
	}

	return strings.Join(result, "\n")
}

// cleanAppFileOutput strips markdown code fences from LLM output
func cleanAppFileOutput(content string, fileType string) string {
	content = strings.TrimSpace(content)
	switch fileType {
	case "tsx":
		content = strings.TrimPrefix(content, "```tsx")
		content = strings.TrimPrefix(content, "```typescript")
		content = strings.TrimPrefix(content, "```")
		content = strings.TrimSuffix(content, "```")
	case "ts":
		content = strings.TrimPrefix(content, "```typescript")
		content = strings.TrimPrefix(content, "```ts")
		content = strings.TrimPrefix(content, "```")
		content = strings.TrimSuffix(content, "```")
	case "css":
		content = strings.TrimPrefix(content, "```css")
		content = strings.TrimPrefix(content, "```")
		content = strings.TrimSuffix(content, "```")
	case "json":
		content = strings.TrimPrefix(content, "```json")
		content = strings.TrimPrefix(content, "```")
		content = strings.TrimSuffix(content, "```")
	default:
		content = strings.TrimPrefix(content, "```")
		content = strings.TrimSuffix(content, "```")
	}
	return strings.TrimSpace(content)
}
