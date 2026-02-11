package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
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

// validationError represents a deterministic check failure
type validationError struct {
	File  string `json:"file"`
	Error string `json:"error"`
	Type  string `json:"type"`
}

// llmFixResponse is the expected JSON from the fix LLM pass
type llmFixResponse struct {
	Fixes []struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	} `json:"fixes"`
}

// --- Regex patterns for deterministic checks ---
var hookPattern = regexp.MustCompile(`\b(useState|useEffect|useRef|useCallback|useMemo|useReducer|useContext)\s*[\(<]`)
var framerMotionImportPattern = regexp.MustCompile(`import\s+.*\s+from\s+['"]framer-motion['"]`)
var fontVariableUsagePattern = regexp.MustCompile(`\.\s*variable\b`)
var fontVariableOptionPattern = regexp.MustCompile(`variable\s*:\s*['"]--font-`)
var fontConstructorPattern = regexp.MustCompile(`(?:const|let|var)\s+\w+\s*=\s*\w+\s*\(`)
var namedImportRegex = regexp.MustCompile(`import\s+\{[^}]+\}\s+from\s+['"]@/([^'"]+)['"]`)
var defaultImportRegex = regexp.MustCompile(`import\s+([A-Z]\w*)\s+from\s+['"]@/([^'"]+)['"]`)
var exportDefaultPattern = regexp.MustCompile(`(?m)^export\s+default\s+`)
var namedExportPattern = regexp.MustCompile(`(?m)^export\s+(function|const|class|type|interface|enum)\s+`)

// --- Template helpers ---

// isTemplateFile checks if a file path is managed by hardcoded templates
func isTemplateFile(path string) bool {
	for _, tp := range constants.NEXTJS_TEMPLATE_FILE_PATHS {
		if path == tp {
			return true
		}
	}
	for _, tp := range constants.NEXTJS_ECOMMERCE_TEMPLATE_FILE_PATHS {
		if path == tp {
			return true
		}
	}
	return false
}

// getColor safely extracts a color hex value from a scheme map
func getColor(scheme map[string]any, key, fallback string) string {
	if v, ok := scheme[key]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return fallback
}

// buildGlobalsCSSTheme generates the globals.css content with Tailwind v4 @theme colors
func buildGlobalsCSSTheme(colorScheme map[string]any) string {
	return fmt.Sprintf(`@import "tailwindcss";

@theme {
  --color-primary: %s;
  --color-secondary: %s;
  --color-accent: %s;
  --color-background: %s;
  --color-surface: %s;
  --color-text: %s;
}

@layer base {
  html {
    scroll-behavior: smooth;
  }
}
`,
		getColor(colorScheme, "primary", "#3B82F6"),
		getColor(colorScheme, "secondary", "#6366F1"),
		getColor(colorScheme, "accent", "#F59E0B"),
		getColor(colorScheme, "background", "#FFFFFF"),
		getColor(colorScheme, "surface", "#F3F4F6"),
		getColor(colorScheme, "text", "#111827"),
	)
}

// getTemplateContent returns the hardcoded content for a template file
func getTemplateContent(path string, colorScheme map[string]any) string {
	switch path {
	case "package.json":
		return constants.NEXTJS_TEMPLATE_PACKAGE_JSON
	case "tsconfig.json":
		return constants.NEXTJS_TEMPLATE_TSCONFIG
	case "postcss.config.mjs":
		return constants.NEXTJS_TEMPLATE_POSTCSS_CONFIG
	case "next.config.mjs":
		return constants.NEXTJS_TEMPLATE_NEXT_CONFIG
	case "app/globals.css":
		return buildGlobalsCSSTheme(colorScheme)
	}
	return ""
}

// getEcommerceTemplateContent returns the hardcoded content for an e-commerce template file
func getEcommerceTemplateContent(path string) string {
	switch path {
	case "lib/wemob-store.ts":
		return constants.NEXTJS_TEMPLATE_WEMOB_STORE
	}
	return ""
}

// appCodegenTools defines the tools available to per-file codegen for reading sibling files.
var appCodegenTools = []utils.Tool{
	{
		Type: "function",
		Function: utils.ToolFunction{
			Name:        "read_file",
			Description: "Read the content of an already-generated project file. Use this to see exact exports, types, and implementations from sibling files so your imports match.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file_path": map[string]any{
						"type":        "string",
						"description": "Relative file path, e.g. 'components/header.tsx'",
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
			Description: "List all already-generated files in the project.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{},
			},
		},
	},
}

// categorizeFiles sorts files into generation tiers.
// Tier 0 is not returned (template files are filtered out before calling this).
// Returns tiers 1-4 as separate slices.
func categorizeFiles(files []appFilePlanEntry) [][]appFilePlanEntry {
	tiers := make([][]appFilePlanEntry, 4) // index 0=tier1, 1=tier2, 2=tier3, 3=tier4

	for _, f := range files {
		switch {
		// Tier 4: root layout and globals.css
		case f.Path == "app/layout.tsx" || f.Path == "app/globals.css":
			tiers[3] = append(tiers[3], f)
		// Tier 1: lib, utils, types, data, hooks, contexts, stores
		case strings.HasPrefix(f.Path, "lib/") ||
			strings.HasPrefix(f.Path, "utils/") ||
			strings.HasPrefix(f.Path, "types/") ||
			strings.HasPrefix(f.Path, "data/") ||
			strings.HasPrefix(f.Path, "hooks/") ||
			strings.HasPrefix(f.Path, "contexts/") ||
			strings.HasPrefix(f.Path, "store/"):
			tiers[0] = append(tiers[0], f)
		// Tier 2: components
		case strings.HasPrefix(f.Path, "components/"):
			tiers[1] = append(tiers[1], f)
		// Tier 3: pages (app/**/page.tsx) and API routes
		default:
			tiers[2] = append(tiers[2], f)
		}
	}

	return tiers
}

// augmentPackageJSON scans generated files for third-party npm imports and adds them
// to the base template package.json if not already present
func augmentPackageJSON(baseTemplate string, files map[string]string) string {
	// Known packages that are already in the template or are built-in
	knownPackages := map[string]bool{
		"react": true, "react-dom": true, "next": true,
		"lucide-react": true, "clsx": true, "framer-motion": true,
		"typescript": true, "tailwindcss": true, "postcss": true, "@tailwindcss/postcss": true,
		// Next.js built-in
		"next/image": true, "next/link": true, "next/font": true,
		"next/navigation": true, "next/headers": true, "next/server": true,
		"next/font/google": true, "next/font/local": true, "next/dynamic": true,
		// React built-in
		"react/jsx-runtime": true,
	}

	// Scan all files for import statements
	importRegex := regexp.MustCompile(`(?m)^import\s+.*\s+from\s+['"]([^'"@./][^'"]*?)(?:/[^'"]*)?['"]`)

	foundPackages := make(map[string]bool)
	for _, content := range files {
		matches := importRegex.FindAllStringSubmatch(content, -1)
		for _, match := range matches {
			pkg := match[1]
			if !knownPackages[pkg] && !strings.HasPrefix(pkg, "@types/") {
				foundPackages[pkg] = true
			}
		}
	}

	if len(foundPackages) == 0 {
		return baseTemplate
	}

	// Parse base template and add packages
	var pkgJSON map[string]any
	if err := json.Unmarshal([]byte(baseTemplate), &pkgJSON); err != nil {
		return baseTemplate
	}

	deps, ok := pkgJSON["dependencies"].(map[string]any)
	if !ok {
		deps = make(map[string]any)
		pkgJSON["dependencies"] = deps
	}

	// Known version mappings for common packages
	versionMap := map[string]string{
		"zustand":             "^4.5.0",
		"axios":               "^1.7.0",
		"date-fns":            "^3.6.0",
		"zod":                 "^3.23.0",
		"react-hook-form":     "^7.51.0",
		"@hookform/resolvers": "^3.3.0",
		"embla-carousel-react": "^8.0.0",
		"recharts":            "^2.12.0",
		"react-icons":         "^5.0.0",
		"swiper":              "^11.0.0",
		"@radix-ui/react-dialog":   "^1.0.0",
		"@radix-ui/react-dropdown-menu": "^2.0.0",
		"@radix-ui/react-tabs":     "^1.0.0",
		"@radix-ui/react-slot":     "^1.0.0",
		"class-variance-authority":  "^0.7.0",
		"tailwind-merge":            "^2.2.0",
	}

	for pkg := range foundPackages {
		if _, exists := deps[pkg]; !exists {
			if version, ok := versionMap[pkg]; ok {
				deps[pkg] = version
			} else {
				deps[pkg] = "latest"
			}
			slog.Info("Added discovered dependency to package.json", "package", pkg)
		}
	}

	result, err := json.MarshalIndent(pkgJSON, "", "  ")
	if err != nil {
		return baseTemplate
	}
	return string(result)
}

// --- Deterministic Validators ---

// runDeterministicChecks runs regex-based checks on generated files and returns errors
func runDeterministicChecks(files map[string]string) []validationError {
	var errors []validationError

	// Build known paths set for import resolution
	knownPaths := make(map[string]bool)
	for path := range files {
		knownPaths[path] = true
		withoutExt := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(path, ".tsx"), ".ts"), ".js")
		knownPaths[withoutExt] = true
		if strings.HasSuffix(withoutExt, "/index") {
			knownPaths[strings.TrimSuffix(withoutExt, "/index")] = true
		}
	}

	for filePath, content := range files {
		// Skip template/config files
		if isTemplateFile(filePath) || isConfigFile(filePath) {
			continue
		}

		if !strings.HasSuffix(filePath, ".tsx") && !strings.HasSuffix(filePath, ".ts") {
			continue // only check TS/TSX files
		}

		// Check 1: Missing @/ imports
		importMatches := importPathRegex.FindAllStringSubmatch(content, -1)
		for _, match := range importMatches {
			importPath := match[1]
			if !knownPaths[importPath] &&
				!knownPaths[importPath+".tsx"] &&
				!knownPaths[importPath+".ts"] &&
				!knownPaths[importPath+"/index.tsx"] &&
				!knownPaths[importPath+"/index.ts"] {
				errors = append(errors, validationError{
					File:  filePath,
					Error: fmt.Sprintf("Import '@/%s' does not resolve to any file in the project", importPath),
					Type:  "missing_import",
				})
			}
		}

		// Check 2: React hooks without "use client"
		if hookPattern.MatchString(content) {
			hasUseClient := strings.Contains(content, `"use client"`) || strings.Contains(content, `'use client'`)
			if !hasUseClient {
				errors = append(errors, validationError{
					File:  filePath,
					Error: "File uses React hooks (useState/useEffect/etc.) but is missing \"use client\" directive",
					Type:  "missing_use_client",
				})
			}
		}

		// Check 2b: framer-motion import without "use client"
		if framerMotionImportPattern.MatchString(content) {
			hasUseClient := strings.Contains(content, `"use client"`) || strings.Contains(content, `'use client'`)
			if !hasUseClient {
				errors = append(errors, validationError{
					File:  filePath,
					Error: "File imports from framer-motion but is missing \"use client\" directive. framer-motion components only work in Client Components.",
					Type:  "missing_use_client",
				})
			}
		}

		// Check 3: Font .variable used without variable: option in constructor
		if fontVariableUsagePattern.MatchString(content) {
			// Check if any font constructor has variable: option
			if !fontVariableOptionPattern.MatchString(content) {
				errors = append(errors, validationError{
					File:  filePath,
					Error: "Uses font.variable but no font constructor has variable: '--font-...' option. Add variable option to the font constructor.",
					Type:  "font_variable",
				})
			}
		}

		// Check 4: Forbidden Node.js/build-time imports
		forbiddenMatches := forbiddenImportRegex.FindAllString(content, -1)
		for _, m := range forbiddenMatches {
			errors = append(errors, validationError{
				File:  filePath,
				Error: fmt.Sprintf("Forbidden import in browser code: %s", strings.TrimSpace(m)),
				Type:  "forbidden_import",
			})
		}

		// Check 5: Named import from a file that only has export default (or vice versa)
		namedMatches := namedImportRegex.FindAllStringSubmatch(content, -1)
		for _, match := range namedMatches {
			importPath := match[1]
			targetContent := resolveFileContent(files, importPath)
			if targetContent == "" {
				continue // file doesn't exist yet, skip
			}
			hasDefault := exportDefaultPattern.MatchString(targetContent)
			hasNamed := namedExportPattern.MatchString(targetContent)
			if hasDefault && !hasNamed {
				errors = append(errors, validationError{
					File:  filePath,
					Error: fmt.Sprintf("Named import { ... } from '@/%s' but that file only has a default export. Use: import ComponentName from '@/%s'", importPath, importPath),
					Type:  "import_mismatch",
				})
			}
		}

		defaultMatches := defaultImportRegex.FindAllStringSubmatch(content, -1)
		for _, match := range defaultMatches {
			importPath := match[2]
			targetContent := resolveFileContent(files, importPath)
			if targetContent == "" {
				continue
			}
			hasDefault := exportDefaultPattern.MatchString(targetContent)
			hasNamed := namedExportPattern.MatchString(targetContent)
			if !hasDefault && hasNamed {
				errors = append(errors, validationError{
					File:  filePath,
					Error: fmt.Sprintf("Default import from '@/%s' but that file has no default export, only named exports. Use: import { ... } from '@/%s'", importPath, importPath),
					Type:  "import_mismatch",
				})
			}
		}
	}

	return errors
}

// resolveFileContent finds the content of a file by its import path (without extension)
func resolveFileContent(files map[string]string, importPath string) string {
	if content, ok := files[importPath]; ok {
		return content
	}
	if content, ok := files[importPath+".tsx"]; ok {
		return content
	}
	if content, ok := files[importPath+".ts"]; ok {
		return content
	}
	if content, ok := files[importPath+"/index.tsx"]; ok {
		return content
	}
	if content, ok := files[importPath+"/index.ts"]; ok {
		return content
	}
	return ""
}

// appFixTools defines the tools available to the fix LLM for on-demand file access.
var appFixTools = []utils.Tool{
	{
		Type: "function",
		Function: utils.ToolFunction{
			Name:        "read_file",
			Description: "Read the full content of a project file by its path. Use this to see the current implementation before fixing it.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file_path": map[string]any{
						"type":        "string",
						"description": "Relative file path, e.g. 'app/page.tsx'",
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
			Description: "List all files in the project.",
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
			Description: "Search for files containing a specific text pattern.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Text to search for (case-insensitive)",
					},
				},
				"required": []string{"query"},
			},
		},
	},
}

// llmFixPass sends validation errors to LLM with tool access for auto-fix
func (c *Controller) llmFixPass(ctx context.Context, files map[string]string, valErrors []validationError, structure *appFileStructure) map[string]string {
	provider := c.llmProvider()
	model := c.SecretManager.GetSecret("NEXTJS_BUILDER_LLM_MODEL")
	if model == "" {
		model = constants.DEFAULT_NEXTJS_LLM_MODEL
	}

	// Build lightweight file list + error list (LLM will use tools to read files)
	var fileList strings.Builder
	fileList.WriteString("Project files:\n")
	paths := make([]string, 0, len(files))
	for path := range files {
		if !isTemplateFile(path) {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	for _, path := range paths {
		fileList.WriteString(fmt.Sprintf("- %s (%d chars)\n", path, len(files[path])))
	}

	var errList strings.Builder
	errList.WriteString("\nValidation errors found:\n")
	for i, e := range valErrors {
		errList.WriteString(fmt.Sprintf("%d. [%s] %s: %s\n", i+1, e.Type, e.File, e.Error))
	}

	userMessage := fileList.String() + errList.String() + "\nUse read_file to inspect the files with errors, then provide fixes."

	fixLocalExecutor := c.buildChatToolExecutor(nil, nil, files)
	executor := c.compositeToolExecutor(fixLocalExecutor)
	fixTools := append([]utils.Tool{}, appFixTools...)
	fixTools = append(fixTools, c.mcpTools()...)
	responseJSON, err := utils.StreamWithTools(
		ctx,
		model,
		[]utils.Message{
			{Role: "system", Content: constants.NEXTJS_FIX_PROMPT},
			{Role: "user", Content: userMessage},
		},
		fixTools,
		provider,
		&utils.ResponseFormat{Type: "json_object"},
		c.reasoningConfig(),
		executor,
		nil,
		nil,
	)
	if err != nil {
		slog.Error("LLM fix pass failed", "error", err)
		return nil
	}

	var fixResp llmFixResponse
	if err := json.Unmarshal([]byte(responseJSON), &fixResp); err != nil {
		cleaned := utils.ExtractJSON(responseJSON)
		if err2 := json.Unmarshal([]byte(cleaned), &fixResp); err2 != nil {
			slog.Error("Failed to parse LLM fix response", "error", err2)
			return nil
		}
	}

	fixes := make(map[string]string)
	for _, fix := range fixResp.Fixes {
		if fix.Path != "" && fix.Content != "" && !isTemplateFile(fix.Path) {
			fixes[fix.Path] = cleanAppFileOutput(fix.Content, "tsx")
		}
	}

	return fixes
}

// validateAndFixFiles runs deterministic checks and LLM fix loop. Max 2 iterations.
// Returns total number of fixed files.
func (c *Controller) validateAndFixFiles(ctx context.Context, project *models.AppProject, files map[string]string, structure *appFileStructure, sendEvent func(string, any) bool) int {
	totalFixed := 0

	for iteration := 1; iteration <= 2; iteration++ {
		if ctx.Err() != nil {
			return totalFixed
		}

		valErrors := runDeterministicChecks(files)
		if len(valErrors) == 0 {
			slog.Info("Validation passed with no errors", "project_id", project.ID, "iteration", iteration)
			sendEvent("validation", responses.ValidationEvent{
				Iteration: iteration,
				Errors:    []string{},
				Fixing:    false,
			})
			return totalFixed
		}

		// Report errors
		errStrings := make([]string, len(valErrors))
		for i, e := range valErrors {
			errStrings[i] = fmt.Sprintf("[%s] %s: %s", e.Type, e.File, e.Error)
		}
		slog.Info("Validation found errors", "project_id", project.ID, "iteration", iteration, "count", len(valErrors))

		sendEvent("validation", responses.ValidationEvent{
			Iteration: iteration,
			Errors:    errStrings,
			Fixing:    true,
		})

		// Call LLM fix pass
		fixes := c.llmFixPass(ctx, files, valErrors, structure)
		if fixes == nil || len(fixes) == 0 {
			slog.Warn("LLM fix pass returned no fixes", "project_id", project.ID, "iteration", iteration)
			break
		}

		// Apply fixes to DB and in-memory map
		for path, content := range fixes {
			files[path] = content

			dbFile, err := c.DB.GetAppGeneratedFileByPath(project.ID.String(), "nextjs", path)
			if err == nil && dbFile != nil {
				dbFile.Content = content
				dbFile.UpdatedAt = time.Now()
				c.DB.UpdateAppGeneratedFile(dbFile)
			}
			totalFixed++
		}

		slog.Info("Applied LLM fixes", "project_id", project.ID, "iteration", iteration, "files_fixed", len(fixes))
	}

	return totalFixed
}

// PlanAppFileStructure calls LLM to generate a file plan for the app
func (c *Controller) PlanAppFileStructure(ctx context.Context, prompt string, projectType string) (*appFileStructure, error) {
	provider := c.llmProvider()
	model := c.SecretManager.GetSecret("NEXTJS_BUILDER_LLM_MODEL")
	if model == "" {
		model = constants.DEFAULT_NEXTJS_LLM_MODEL
	}

	systemPrompt := constants.NEXTJS_STRUCTURE_PROMPT
	if projectType == "ecommerce" {
		systemPrompt = constants.NEXTJS_ECOMMERCE_STRUCTURE_PROMPT
	}

	structureJSON, err := utils.GetJSONFromLLM(model, systemPrompt, prompt, provider)
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
	provider := c.llmProvider()
	model := c.SecretManager.GetSecret("NEXTJS_BUILDER_LLM_MODEL")
	if model == "" {
		model = constants.DEFAULT_NEXTJS_LLM_MODEL
	}

	systemPrompt := constants.NEXTJS_STRUCTURE_PROMPT
	if projectType == "ecommerce" {
		systemPrompt = constants.NEXTJS_ECOMMERCE_STRUCTURE_PROMPT
	}

	structureJSON, err := utils.StreamJSONFromLLM(ctx, model, systemPrompt, prompt, provider, onChunk)
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
	provider := c.llmProvider()
	model := c.SecretManager.GetSecret("NEXTJS_BUILDER_LLM_MODEL")
	if model == "" {
		model = constants.DEFAULT_NEXTJS_LLM_MODEL
	}

	response, err := utils.GetJSONFromLLMWithVision(model, constants.NEXTJS_SCREENSHOT_TO_FILES_PROMPT, "Analyze this screenshot and generate the file structure plan.", imageBase64, provider)
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
		return "@import \"tailwindcss\";\n\n/* Generation failed. Please edit manually. */"
	case "ts":
		return "// Generation failed. Please edit manually.\nexport {}"
	default:
		return ""
	}
}

// StreamGenerateAppFiles generates app files one by one with SSE streaming.
// Uses tiered generation with context accumulation and deterministic validation.
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

	provider := c.llmProvider()
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

	// Step 3: Filter template files from structure (strip them even if LLM included them)
	var appFiles []appFilePlanEntry
	for _, f := range structure.Files {
		if !isTemplateFile(f.Path) {
			appFiles = append(appFiles, f)
		}
	}

	// Build total file list (template + app files)
	templatePaths := constants.NEXTJS_TEMPLATE_FILE_PATHS
	allFilePaths := make([]string, 0, len(templatePaths)+len(appFiles)+len(constants.NEXTJS_ECOMMERCE_TEMPLATE_FILE_PATHS))
	allFilePaths = append(allFilePaths, templatePaths...)
	if projectType == "ecommerce" {
		allFilePaths = append(allFilePaths, constants.NEXTJS_ECOMMERCE_TEMPLATE_FILE_PATHS...)
	}
	for _, f := range appFiles {
		allFilePaths = append(allFilePaths, f.Path)
	}

	if !sendEvent("project_created", map[string]any{
		"id":          project.ID,
		"name":        project.Name,
		"slug":        project.Slug,
		"file_paths":  allFilePaths,
		"total_files": len(allFilePaths),
	}) {
		return
	}

	// Step 4: Create conversation early with user message
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

	// Step 6: Inject template files (tier 0)
	generatedFiles := make(map[string]string) // accumulated content for context
	for _, tp := range templatePaths {
		content := getTemplateContent(tp, structure.ColorScheme)
		fileType := "json"
		switch {
		case strings.HasSuffix(tp, ".ts"):
			fileType = "ts"
		case strings.HasSuffix(tp, ".mjs"):
			fileType = "mjs"
		}

		file := models.AppGeneratedFile{
			AppProjectID: project.ID,
			Platform:     "nextjs",
			FilePath:     tp,
			Content:      content,
			FileType:     fileType,
			IsGenerated:  false, // template-managed
		}
		createdFile, err := c.DB.CreateAppGeneratedFile(&file)
		if err != nil {
			slog.Error("failed to save template file", "error", err, "path", tp)
			continue
		}
		generatedFiles[tp] = content

		sendEvent("file_complete", responses.FileCompleteEvent{
			Action:   "create",
			FilePath: tp,
			FileID:   createdFile.ID,
			FileType: fileType,
		})
	}

	// Step 6b: Inject e-commerce template files (conditional on project type)
	if projectType == "ecommerce" {
		for _, tp := range constants.NEXTJS_ECOMMERCE_TEMPLATE_FILE_PATHS {
			content := getEcommerceTemplateContent(tp)
			if content == "" {
				continue
			}
			fileType := "ts"
			file := models.AppGeneratedFile{
				AppProjectID: project.ID,
				Platform:     "nextjs",
				FilePath:     tp,
				Content:      content,
				FileType:     fileType,
				IsGenerated:  false, // template-managed
			}
			createdFile, err := c.DB.CreateAppGeneratedFile(&file)
			if err != nil {
				slog.Error("failed to save e-commerce template file", "error", err, "path", tp)
				continue
			}
			generatedFiles[tp] = content

			sendEvent("file_complete", responses.FileCompleteEvent{
				Action:   "create",
				FilePath: tp,
				FileID:   createdFile.ID,
				FileType: fileType,
			})
		}
	}

	// Step 7: Categorize remaining files into tiers and generate with context accumulation
	tiers := categorizeFiles(appFiles)
	colorSchemeJSON, _ := json.Marshal(structure.ColorScheme)

	for tierIdx, tier := range tiers {
		if len(tier) == 0 {
			continue
		}

		slog.Info("Generating tier", "project_id", project.ID, "tier", tierIdx+1, "files", len(tier))

		for _, filePlan := range tier {
			if ctx.Err() != nil {
				return
			}

			filePrompt := c.getAppFilePrompt(filePlan.Type)

			// Build lightweight file list (paths + sizes) — LLM uses tools to read siblings
			var generatedFileList strings.Builder
			if len(generatedFiles) > 0 {
				generatedFileList.WriteString("\n\nAlready-generated files (use read_file to view):\n")
				genPaths := make([]string, 0, len(generatedFiles))
				for p := range generatedFiles {
					genPaths = append(genPaths, p)
				}
				sort.Strings(genPaths)
				for _, p := range genPaths {
					generatedFileList.WriteString(fmt.Sprintf("- %s (%d chars)\n", p, len(generatedFiles[p])))
				}
			}

			allFilesContext := strings.Join(allFilePaths, ", ")
			userPrompt := fmt.Sprintf(
				"App name: %s\nColor scheme: %s\nTypography: %v\nAll files: %s\nCurrent file: %s\nDescription: %s\n\nUser's original description: %s%s",
				structure.Name,
				string(colorSchemeJSON),
				structure.Typography,
				allFilesContext,
				filePlan.Path,
				filePlan.Description,
				project.Prompt,
				generatedFileList.String(),
			)

			codegenLocalExecutor := c.buildChatToolExecutor(nil, nil, generatedFiles)
			codegenExecutor := c.compositeToolExecutor(codegenLocalExecutor)
			codegenTools := append([]utils.Tool{}, appCodegenTools...)
			codegenTools = append(codegenTools, c.mcpTools()...)
			content, err := utils.StreamTextWithTools(
				ctx,
				model,
				filePrompt,
				userPrompt,
				provider,
				codegenTools,
				c.reasoningConfig(),
				codegenExecutor,
				nil,
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

			// Strip forbidden imports
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

			// Add to accumulated context
			generatedFiles[filePlan.Path] = content

			if !sendEvent("file_complete", responses.FileCompleteEvent{
				Action:   "create",
				FilePath: filePlan.Path,
				FileID:   createdFile.ID,
				FileType: filePlan.Type,
				Content:  content,
			}) {
				return
			}
		}
	}

	// Step 8: Find and generate missing imports
	missingImports := findMissingImports(generatedFiles)
	if len(missingImports) > 0 {
		slog.Info("Found missing imports, generating files", "project_id", project.ID, "missing", missingImports)

		for _, importPath := range missingImports {
			if ctx.Err() != nil {
				return
			}

			filePath, fileType := guessFileType(importPath)
			filePrompt := c.getAppFilePrompt(fileType)

			// Build context about what files import this missing module
			importedBy := []string{}
			for path, content := range generatedFiles {
				if strings.Contains(content, "@/"+importPath) {
					importedBy = append(importedBy, path)
				}
			}

			// Build lightweight file list for context
			var genFileList strings.Builder
			if len(generatedFiles) > 0 {
				genFileList.WriteString("\n\nAlready-generated files (use read_file to view):\n")
				genPaths := make([]string, 0, len(generatedFiles))
				for p := range generatedFiles {
					genPaths = append(genPaths, p)
				}
				sort.Strings(genPaths)
				for _, p := range genPaths {
					genFileList.WriteString(fmt.Sprintf("- %s (%d chars)\n", p, len(generatedFiles[p])))
				}
			}

			allFilesContext := strings.Join(allFilePaths, ", ")

			userPrompt := fmt.Sprintf(
				"App name: %s\nColor scheme: %s\nTypography: %v\nAll files: %s\nCurrent file: %s\nDescription: This file is imported by: %s. Generate the implementation based on how it is used in those files.\n\nUser's original description: %s%s",
				structure.Name,
				string(colorSchemeJSON),
				structure.Typography,
				allFilesContext+", "+filePath,
				filePath,
				strings.Join(importedBy, ", "),
				project.Prompt,
				genFileList.String(),
			)

			missingLocalExecutor := c.buildChatToolExecutor(nil, nil, generatedFiles)
			missingExecutor := c.compositeToolExecutor(missingLocalExecutor)
			missingTools := append([]utils.Tool{}, appCodegenTools...)
			missingTools = append(missingTools, c.mcpTools()...)
			content, err := utils.StreamTextWithTools(
				ctx,
				model,
				filePrompt,
				userPrompt,
				provider,
				missingTools,
				c.reasoningConfig(),
				missingExecutor,
				nil,
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
				if fileType == "tsx" {
					baseName := importPath
					if idx := strings.LastIndex(importPath, "/"); idx >= 0 {
						baseName = importPath[idx+1:]
					}
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
				Content:  content,
			})
		}
	}

	// Step 9: Run deterministic validation + LLM fix loop
	fixedCount := c.validateAndFixFiles(ctx, project, generatedFiles, structure, sendEvent)
	if fixedCount > 0 {
		slog.Info("Validation fixed files", "project_id", project.ID, "fixed_count", fixedCount)
	}

	// Step 10: Augment package.json with any discovered third-party deps
	augmented := augmentPackageJSON(generatedFiles["package.json"], generatedFiles)
	if augmented != generatedFiles["package.json"] {
		dbFile, err := c.DB.GetAppGeneratedFileByPath(project.ID.String(), "nextjs", "package.json")
		if err == nil && dbFile != nil {
			dbFile.Content = augmented
			c.DB.UpdateAppGeneratedFile(dbFile)
			slog.Info("Augmented package.json with discovered deps", "project_id", project.ID)
		}
	}

	// Step 11: Update conversation with assistant message
	totalFiles := len(templatePaths) + len(appFiles) + len(missingImports)
	if conv != nil {
		assistantMsg := fmt.Sprintf("Created app \"%s\" with %d files.", project.Name, totalFiles)
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

	// Step 12: Update project status
	if _, err := c.UpdateAppProjectStatus(project, "ready"); err != nil {
		slog.Error("failed to update project status to ready", "project_id", project.ID, "error", err)
	}

	// Step 13: Auto-start preview if service is configured
	if c.PreviewService != nil {
		go func() {
			status, err := c.StartPreview(project)
			if err != nil {
				slog.Error("Failed to auto-start preview", "error", err, "project_id", project.ID)
			} else {
				slog.Info("Auto-started preview", "project_id", project.ID, "preview_url", status.PreviewURL)
			}
		}()
	}

	// Step 14: Send done event
	sendEvent("done", map[string]any{
		"app_project_id": project.ID,
		"success":        true,
		"message":        "app generation completed",
	})
}

// importPathRegex matches import statements like: import X from '@/components/foo' or from "@/lib/bar"
var importPathRegex = regexp.MustCompile(`from\s+['"]@/([^'"]+)['"]`)

// forbiddenImportRegex matches imports of build-time/Node.js packages in component files
var forbiddenImportRegex = regexp.MustCompile(`(?m)^import\s+.*\s+from\s+['"](tailwindcss|tailwindcss/.*|@tailwindcss/postcss|postcss|autoprefixer|eslint|webpack|jiti|fs|path|os|module|crypto|stream|child_process|net|http|https|url|util|assert|buffer|cluster|dgram|dns|events|querystring|readline|repl|string_decoder|tls|tty|v8|vm|zlib)['"].*$`)

// isConfigFile returns true for config files that legitimately need build-time imports
func isConfigFile(filePath string) bool {
	lower := strings.ToLower(filePath)
	return strings.Contains(lower, "postcss.config") ||
		strings.Contains(lower, "next.config") ||
		strings.Contains(lower, "tsconfig")
}

// stripForbiddenImports removes import lines for build-time/Node.js packages from component code.
func stripForbiddenImports(content string, filePath string) (string, bool) {
	if isConfigFile(filePath) {
		return content, false
	}

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
	knownPaths := make(map[string]bool)
	for path := range files {
		knownPaths[path] = true
		withoutExt := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(path, ".tsx"), ".ts"), ".js")
		knownPaths[withoutExt] = true
		if strings.HasSuffix(withoutExt, "/index") {
			knownPaths[strings.TrimSuffix(withoutExt, "/index")] = true
		}
	}

	missingSet := make(map[string]bool)
	for _, content := range files {
		matches := importPathRegex.FindAllStringSubmatch(content, -1)
		for _, match := range matches {
			importPath := match[1]
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

// controlTokenRegex matches LLM control tokens like <|control619|>, <|endoftext|>, etc.
var controlTokenRegex = regexp.MustCompile(`<\|[a-zA-Z0-9_]+\|>`)

// cleanAppFileOutput strips markdown code fences, control tokens, and any
// reasoning/explanation text outside code fences from LLM output.
func cleanAppFileOutput(content string, fileType string) string {
	content = strings.TrimSpace(content)

	// Strip control tokens (e.g. <|control619|>, <|endoftext|>)
	content = controlTokenRegex.ReplaceAllString(content, "")

	// Determine which fence prefixes to check based on file type
	var fencePrefixes []string
	switch fileType {
	case "tsx":
		fencePrefixes = []string{"```tsx", "```typescript", "```"}
	case "ts":
		fencePrefixes = []string{"```typescript", "```ts", "```"}
	case "css":
		fencePrefixes = []string{"```css", "```"}
	case "json":
		fencePrefixes = []string{"```json", "```"}
	default:
		fencePrefixes = []string{"```"}
	}

	// Extract code between markdown fences, discarding any text after closing fence
	for _, prefix := range fencePrefixes {
		if strings.HasPrefix(content, prefix) {
			inner := content[len(prefix):]
			// Find the last closing fence on its own line
			closingIdx := strings.LastIndex(inner, "\n```")
			if closingIdx >= 0 {
				content = inner[:closingIdx]
			} else {
				// No closing fence on its own line — try plain suffix
				content = strings.TrimSuffix(inner, "```")
			}
			break
		}
	}

	return strings.TrimSpace(content)
}
