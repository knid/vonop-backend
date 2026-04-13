package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/remoteconfig"
	"github.com/knid/wemob/internal/templates"
	"github.com/knid/wemob/internal/utils"

	_ "github.com/lib/pq"
)

// --- Conversation types ---

// ConversationReply is a natural language reply from the user during the pre-build conversation.
type ConversationReply struct {
	Message string `json:"message"`
}

// --- Clarification types (deprecated — use ConversationReply instead) ---

// ClarificationQuestion is a single question the LLM generates to gather context.
type ClarificationQuestion struct {
	ID       string   `json:"id"`
	Question string   `json:"question"`
	Type     string   `json:"type"`     // "text" or "choice"
	Options  []string `json:"options"`  // non-empty when Type == "choice"
	Required bool     `json:"required"`
}

// ClarificationAnswer is a single user answer to a clarification question.
type ClarificationAnswer struct {
	ID     string `json:"id"`
	Answer string `json:"answer"`
}

// ClarificationResult is sent through the pending channel from the clarify endpoint.
type ClarificationResult struct {
	Answers []ClarificationAnswer
	Skip    bool
}

type appFileStructure struct {
	Name            string             `json:"name"`
	Slug            string             `json:"slug"`
	NeedsAuth       bool               `json:"needs_auth,omitempty"`
	ColorScheme     map[string]any     `json:"colorScheme"`
	DarkColorScheme map[string]any     `json:"darkColorScheme,omitempty"`
	Typography      map[string]any     `json:"typography,omitempty"`
	Files           []appFilePlanEntry `json:"files"`
	DataModels      []models.DataModel `json:"data_models,omitempty"`
	RequiredEnvVars []RequiredEnvVar   `json:"required_env_vars,omitempty"`
}

type RequiredEnvVar struct {
	Key         string `json:"key"`
	Description string `json:"description"`
	IsPublic    bool   `json:"is_public"`
	Provider    string `json:"provider,omitempty"`
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


// --- Regex for env var detection ---
// Matches both dot notation (process.env.FOO) and bracket notation (process.env["FOO"] / process.env['FOO'])
var processEnvRegex = regexp.MustCompile(`process\.env\.([A-Z_][A-Z0-9_]*)|process\.env\[["']([A-Z_][A-Z0-9_]*)["']\]`)

// alwaysExcludedEnvVars are env vars that are always provided by the system
// and should never appear in "missing" detection.
var alwaysExcludedEnvVars = map[string]bool{
	"NODE_ENV":                  true,
	"NEXT_PUBLIC_STORE_ID":      true,
	"NEXT_PUBLIC_WEMOB_API_URL": true,
}

// detectEnvVarsFromCode scans all generated file contents for process.env.* references
// and returns a sorted list of unique env var keys, excluding system-injected ones.
// Integration-provided vars (DATABASE_URL, AUTH_SECRET, etc.) are NOT excluded here
// so they show as missing when the integration is disconnected.
func detectEnvVarsFromCode(files map[string]string) []string {
	seen := map[string]bool{}
	for _, content := range files {
		for _, m := range processEnvRegex.FindAllStringSubmatch(content, -1) {
			// m[1] is dot notation capture, m[2] is bracket notation capture
			key := m[1]
			if key == "" {
				key = m[2]
			}
			if key != "" && !alwaysExcludedEnvVars[key] {
				seen[key] = true
			}
		}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// envVarNameRegex validates that an env var name contains only uppercase letters, digits, and underscores.
var envVarNameRegex = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,100}$`)

// computeMissingEnvVars merges LLM-declared env vars with code-scanned keys,
// then returns only those not yet configured in the project.
func (c *Controller) computeMissingEnvVars(project *models.AppProject, llmVars []RequiredEnvVar, codeKeys []string) []RequiredEnvVar {
	existing := c.CollectAllEnvVars(project)

	required := map[string]RequiredEnvVar{}
	for _, v := range llmVars {
		if !envVarNameRegex.MatchString(v.Key) {
			slog.Warn("skipping invalid env var name from LLM", "key", v.Key, "project_id", project.ID)
			continue
		}
		required[v.Key] = v
	}
	for _, key := range codeKeys {
		if !envVarNameRegex.MatchString(key) {
			continue
		}
		if _, ok := required[key]; !ok {
			required[key] = RequiredEnvVar{
				Key:         key,
				Description: "Used in generated code",
				IsPublic:    strings.HasPrefix(key, "NEXT_PUBLIC_"),
			}
		}
	}

	var missing []RequiredEnvVar
	for _, v := range required {
		// Check both the raw key and the NEXT_PUBLIC_ prefixed form,
		// since CollectAllEnvVars auto-prefixes public custom vars.
		if existing[v.Key] == "" && existing["NEXT_PUBLIC_"+v.Key] == "" {
			missing = append(missing, v)
		}
	}
	sort.Slice(missing, func(i, j int) bool { return missing[i].Key < missing[j].Key })
	return missing
}

// --- Regex patterns for deterministic checks ---
var hookPattern = regexp.MustCompile(`\b(useState|useEffect|useRef|useCallback|useMemo|useReducer|useContext)\s*[\(<]`)
var getSessionArgsRegex = regexp.MustCompile(`getSession\s*\([^)]+\)`)
var framerMotionImportPattern = regexp.MustCompile(`import\s+.*\s+from\s+['"]framer-motion['"]`)
var fontVariableUsagePattern = regexp.MustCompile(`\.\s*variable\b`)
var fontVariableOptionPattern = regexp.MustCompile(`variable\s*:\s*['"]--font-`)
var fontConstructorPattern = regexp.MustCompile(`(?:const|let|var)\s+\w+\s*=\s*\w+\s*\(`)
var namedImportRegex = regexp.MustCompile(`import\s+\{[^}]+\}\s+from\s+['"]@/([^'"]+)['"]`)
var defaultImportRegex = regexp.MustCompile(`import\s+([A-Z]\w*)\s+from\s+['"]@/([^'"]+)['"]`)
var exportDefaultPattern = regexp.MustCompile(`(?m)^export\s+default\s+`)
var namedExportPattern = regexp.MustCompile(`(?m)^export\s+(?:async\s+)?(function|const|class|type|interface|enum)\s+`)
var namedExportIdentRegex = regexp.MustCompile(`(?m)^export\s+(?:async\s+)?(?:function|const|class|type|interface|enum)\s+(\w+)`)
var singleNamedImportRegex = regexp.MustCompile(`import\s+\{\s*(\w+)\s*\}\s+from\s+['"]@/([^'"]+)['"]`)

// namedValueExportPattern matches only runtime named exports (excludes type/interface)
var namedValueExportPattern = regexp.MustCompile(`(?m)^export\s+(?:async\s+)?(function|const|class|enum)\s+`)
var namedValueExportIdentRegex = regexp.MustCompile(`(?m)^export\s+(?:async\s+)?(?:function|const|class|enum)\s+(\w+)`)

// exportDefaultFuncNameRegex extracts function name from "export default function Foo"
var exportDefaultFuncNameRegex = regexp.MustCompile(`(?m)^export\s+default\s+function\s+(\w+)`)

// multiNamedImportRegex matches: import { A, B } from '@/path' (2+ names)
var multiNamedImportRegex = regexp.MustCompile(`import\s+\{\s*(\w+(?:\s*,\s*\w+)+)\s*\}\s+from\s+['"]@/([^'"]+)['"]`)

// jsxTagPattern detects JSX syntax in .ts files that should be .tsx.
// The (?:^|[^a-zA-Z0-9_]) prefix prevents false positives on TypeScript generics
// like Promise<AuthResult> or Array<string> where < is preceded by a word character.
var jsxTagPattern = regexp.MustCompile(`(?:^|[^a-zA-Z0-9_])<[A-Za-z][A-Za-z0-9.]*(?:\s|>|/)`)

// metadataExportPattern matches "export const metadata" in layout/page files.
var metadataExportPattern = regexp.MustCompile(`(?m)^export\s+const\s+metadata\b`)

// internalHrefRegex matches href="/path" or href='/path' (skips dynamic href={...})
var internalHrefRegex = regexp.MustCompile(`href=["'](/[^"']*?)["']`)

// routeGroupRegex strips Next.js route group segments like (auth), (dashboard) from paths.
// Route groups don't affect the URL — app/(auth)/login/page.tsx serves /login.
var routeGroupRegex = regexp.MustCompile(`\([^)]+\)/`)

// fallbackSentinel is a machine-readable marker injected into fallback content.
// Used instead of human-readable text to avoid false positives when LLM-generated
// code legitimately contains phrases like "Generation failed" in UI strings.
const fallbackSentinel = "/* __WEMOB_FALLBACK__ */"

// PlanAppFileStructure calls the LLM to generate a file structure plan from a prompt (non-streaming).
func (c *Controller) PlanAppFileStructure(ctx context.Context, prompt string, projectType string) (*appFileStructure, error) {
	provider := c.llmProvider()
	model := c.SecretManager.GetSecret("WEBSITE_BUILDER_LLM_MODEL")
	if model == "" {
		model = provider.DefaultModel()
	}

	maxFiles := c.RemoteConfig.GetInt(remoteconfig.AIMaxStructureFiles, remoteconfig.Global)
	systemPrompt := constants.NextjsStructurePrompt(maxFiles)
	if projectType == "ecommerce" {
		systemPrompt = constants.NEXTJS_ECOMMERCE_STRUCTURE_PROMPT
	} else if projectType == "mobile_app" {
		systemPrompt = constants.NextjsMobileStructurePrompt(maxFiles)
	}

	// Try up to 2 times — structure planning is critical for app quality
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		attemptModel := model
		if attempt > 0 {
			// Retry with fast model as fallback — it's cheaper and may succeed
			// where the main model hit rate limits
			attemptModel = provider.FastModel()
			slog.Info("Retrying structure planning with fast model",
				"attempt", attempt+1, "model", attemptModel)
		}

		// Wrap prompt with anti-conversation guard: when the prompt contains
		// pre-build conversation history, the model sometimes continues chatting
		// instead of producing JSON. This explicit instruction prevents that.
		planPrompt := prompt
		if strings.Contains(prompt, "Pre-build conversation") {
			planPrompt = "CRITICAL: You are in STRUCTURED JSON OUTPUT mode. The conversation below is context only — do NOT continue the conversation. Output ONLY the JSON object as specified in the system prompt.\n\n" + prompt
		}
		structureJSON, err := utils.GetJSONFromLLMWithMaxTokens(ctx, attemptModel, systemPrompt, planPrompt, provider, 32768)
		if err != nil {
			slog.Error("failed to generate app structure",
				"error", err, "attempt", attempt+1, "model", attemptModel)
			lastErr = err
			continue
		}

		var structure appFileStructure
		if err := json.Unmarshal([]byte(structureJSON), &structure); err != nil {
			cleaned := utils.ExtractJSON(structureJSON)
			if err2 := json.Unmarshal([]byte(cleaned), &structure); err2 != nil {
				// Attempt to repair truncated JSON (e.g. LLM hit token limit mid-output)
				repaired := repairTruncatedStructureJSON(cleaned)
				if err3 := json.Unmarshal([]byte(repaired), &structure); err3 != nil {
					slog.Error("failed to parse app structure",
						"error", err3, "attempt", attempt+1, "json_length", len(structureJSON))
					lastErr = fmt.Errorf("failed to parse app structure: %w", err3)
					continue
				}
				slog.Info("repaired truncated structure JSON",
					"attempt", attempt+1, "files", len(structure.Files))
			}
		}

		if len(structure.Files) == 0 {
			lastErr = fmt.Errorf("structure planning returned no files")
			continue
		}

		return &structure, nil
	}

	return nil, fmt.Errorf("structure planning failed after retries: %w", lastErr)
}

// repairTruncatedStructureJSON attempts to fix JSON that was truncated mid-output
// (e.g. when the LLM hits the max_tokens limit). It closes open strings, arrays,
// and objects so the JSON can be parsed — we get a partial but usable structure.
func repairTruncatedStructureJSON(s string) string {
	if len(s) == 0 {
		return s
	}

	// Strip any trailing incomplete key-value pair (e.g. `"path": "app/some`)
	// by finding the last complete JSON element
	result := []byte(s)

	// Remove trailing characters that are clearly mid-value
	for len(result) > 0 {
		last := result[len(result)-1]
		if last == ',' || last == ' ' || last == '\n' || last == '\r' || last == '\t' {
			result = result[:len(result)-1]
		} else {
			break
		}
	}

	// If we're mid-string, close it
	inString := false
	escaped := false
	for _, ch := range result {
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' && inString {
			escaped = true
			continue
		}
		if ch == '"' {
			inString = !inString
		}
	}
	if inString {
		result = append(result, '"')
	}

	// Count open brackets/braces and close them
	openBraces := 0
	openBrackets := 0
	inStr := false
	esc := false
	for _, ch := range result {
		if esc {
			esc = false
			continue
		}
		if ch == '\\' && inStr {
			esc = true
			continue
		}
		if ch == '"' {
			inStr = !inStr
			continue
		}
		if inStr {
			continue
		}
		switch ch {
		case '{':
			openBraces++
		case '}':
			openBraces--
		case '[':
			openBrackets++
		case ']':
			openBrackets--
		}
	}

	// Remove trailing comma before we close
	trimmed := bytes.TrimRight(result, " \t\n\r,")
	result = trimmed

	for openBrackets > 0 {
		result = append(result, ']')
		openBrackets--
	}
	for openBraces > 0 {
		result = append(result, '}')
		openBraces--
	}

	return string(result)
}

// ApplyStructureToProject saves the structure plan output (file plan, data models,
// needs_auth, required_env_vars) to the project so it can be injected into the
// generation context. Call this from views after PlanAppFileStructure.
func (c *Controller) ApplyStructureToProject(project *models.AppProject, structure *appFileStructure) {
	if project.Settings == nil {
		project.Settings = make(map[string]any)
	}

	// Save file plan
	if len(structure.Files) > 0 {
		project.Settings["file_plan"] = structure.Files
	}

	// Save data models
	if len(structure.DataModels) > 0 {
		if dmJSON, err := models.MarshalDataModels(structure.DataModels); err != nil {
			slog.Warn("invalid data models from structure plan, skipping", "error", err)
		} else {
			project.DataModels = dmJSON
		}
	}

	// Save needs_auth
	if structure.NeedsAuth {
		project.Settings["needs_auth"] = true
	}

	// Save required env vars
	if len(structure.RequiredEnvVars) > 0 {
		project.Settings["required_env_vars"] = structure.RequiredEnvVars
	}
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
		if templates.IsTemplateFile(filePath) || isConfigFile(filePath) {
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
		// Skip if file exports metadata — metadata requires Server Components, and the
		// auto-fix will extract client logic into a separate wrapper.
		if hookPattern.MatchString(content) && !metadataExportPattern.MatchString(content) {
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

		// Check 2c: @/lib/wemob-store import without "use client"
		// wemob-store.ts is a "use client" module — all exports (getProducts, useCart, etc.)
		// are client-only. Any file importing from it MUST be a Client Component.
		if strings.Contains(content, `@/lib/wemob-store`) {
			hasUseClient := strings.Contains(content, `"use client"`) || strings.Contains(content, `'use client'`)
			if !hasUseClient {
				errors = append(errors, validationError{
					File:  filePath,
					Error: "File imports from @/lib/wemob-store but is missing \"use client\" directive. wemob-store.ts is a client module — all its exports (getProducts, useCart, etc.) can only be used in Client Components. Add \"use client\" at the top of this file.",
					Type:  "missing_use_client",
				})
			}
		}

		// Check 2d: @/lib/wemob-auth-client import without "use client"
		// Skip layout files — server components can render client components (e.g. AuthProvider) directly.
		isLayout := strings.HasSuffix(filePath, "/layout.tsx") || filePath == "layout.tsx"
		if strings.Contains(content, `@/lib/wemob-auth-client`) && !isLayout {
			hasUseClient := strings.Contains(content, `"use client"`) || strings.Contains(content, `'use client'`)
			if !hasUseClient {
				errors = append(errors, validationError{
					File:  filePath,
					Error: "File imports from @/lib/wemob-auth-client but is missing \"use client\" directive. wemob-auth-client.tsx is a client module — add \"use client\" at the top of this file.",
					Type:  "missing_use_client",
				})
			}
		}

		// Check 2e: @/lib/wemob-auth (server-only) imported in "use client" file
		// Use quote-terminated match to avoid false negatives from @/lib/wemob-auth-client substring
		hasServerAuthImport := strings.Contains(content, `@/lib/wemob-auth'`) || strings.Contains(content, `@/lib/wemob-auth"`)
		if hasServerAuthImport {
			hasUseClient := strings.Contains(content, `"use client"`) || strings.Contains(content, `'use client'`)
			if hasUseClient {
				errors = append(errors, validationError{
					File:  filePath,
					Error: "File imports from @/lib/wemob-auth (server-only module) but has \"use client\" directive. Use @/lib/wemob-auth-client for client components instead.",
					Type:  "server_import_in_client",
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

		// Check 3b: JSX in .ts file (should be .tsx)
		if strings.HasSuffix(filePath, ".ts") && !strings.HasSuffix(filePath, ".d.ts") {
			if jsxTagPattern.MatchString(content) {
				errors = append(errors, validationError{
					File:  filePath,
					Error: fmt.Sprintf("File '%s' contains JSX but has .ts extension. Rename to .tsx.", filePath),
					Type:  "ts_with_jsx",
				})
			}
		}

		// Check 3c: metadata export in "use client" component
		hasUseClientDirective := strings.Contains(content, `"use client"`) || strings.Contains(content, `'use client'`)
		if hasUseClientDirective && metadataExportPattern.MatchString(content) {
			errors = append(errors, validationError{
				File:  filePath,
				Error: "File exports 'metadata' but has \"use client\" directive. Next.js metadata must be in Server Components — remove \"use client\".",
				Type:  "metadata_in_client",
			})
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
		// Use namedValueExportPattern to ignore type/interface exports (they don't exist at runtime)
		namedMatches := namedImportRegex.FindAllStringSubmatch(content, -1)
		for _, match := range namedMatches {
			importPath := match[1]
			targetContent := resolveFileContent(files, importPath)
			if targetContent == "" {
				continue // file doesn't exist yet, skip
			}
			hasDefault := exportDefaultPattern.MatchString(targetContent)
			hasNamedValue := namedValueExportPattern.MatchString(targetContent)
			if hasDefault && !hasNamedValue {
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
			hasNamedValue := namedValueExportPattern.MatchString(targetContent)
			if !hasDefault && hasNamedValue {
				errors = append(errors, validationError{
					File:  filePath,
					Error: fmt.Sprintf("Default import from '@/%s' but that file has no default export, only named exports. Use: import { ... } from '@/%s'", importPath, importPath),
					Type:  "import_mismatch",
				})
			}
		}

		// Check 6: Verify named import names actually exist in target file exports
		for _, match := range namedImportRegex.FindAllStringSubmatch(content, -1) {
			importPath := match[1]
			targetContent := resolveFileContent(files, importPath)
			if targetContent == "" {
				continue
			}

			// Build set of all export names from target (values + types + default func name)
			targetExports := make(map[string]bool)
			for _, ne := range namedExportIdentRegex.FindAllStringSubmatch(targetContent, -1) {
				targetExports[ne[1]] = true
			}
			if dm := exportDefaultFuncNameRegex.FindStringSubmatch(targetContent); dm != nil {
				targetExports[dm[1]] = true
			}

			if len(targetExports) == 0 {
				continue // can't verify if no exports found
			}

			// Extract individual imported names from the { A, B, C } portion
			fullImport := match[0]
			braceStart := strings.Index(fullImport, "{")
			braceEnd := strings.Index(fullImport, "}")
			if braceStart < 0 || braceEnd < 0 {
				continue
			}
			namesPart := fullImport[braceStart+1 : braceEnd]
			importedNames := strings.Split(namesPart, ",")
			for _, name := range importedNames {
				name = strings.TrimSpace(name)
				// Skip type imports like "type Foo"
				if strings.HasPrefix(name, "type ") {
					continue
				}
				if name == "" {
					continue
				}
				if !targetExports[name] {
					available := make([]string, 0, len(targetExports))
					for e := range targetExports {
						available = append(available, e)
					}
					sort.Strings(available)
					errors = append(errors, validationError{
						File:  filePath,
						Error: fmt.Sprintf("Named import '%s' does not exist in '@/%s'. Available exports: %s", name, importPath, strings.Join(available, ", ")),
						Type:  "invalid_named_import",
					})
				}
			}
		}
	}

	// --- Check 7: Internal links pointing to non-existent pages ---

	// Build known routes from page files in the files map
	knownRoutes := make(map[string]bool)
	knownRoutes["/"] = true // root always exists if app/page.tsx exists
	var dynamicRoutePatterns []*regexp.Regexp
	for path := range files {
		if !strings.HasSuffix(path, "/page.tsx") && !strings.HasSuffix(path, "/page.ts") {
			continue
		}
		// Strip "app/" prefix and "/page.tsx" or "/page.ts" suffix
		route := strings.TrimPrefix(path, "app/")
		route = strings.TrimSuffix(route, "/page.tsx")
		route = strings.TrimSuffix(route, "/page.ts")
		if route == "page.tsx" || route == "page.ts" || route == "" {
			// root page
			knownRoutes["/"] = true
			continue
		}
		// Strip route group segments — (auth)/login → login
		route = routeGroupRegex.ReplaceAllString(route, "")
		route = strings.TrimSuffix(route, "/") // clean trailing slash if group was at end
		if route == "" {
			knownRoutes["/"] = true
			continue
		}
		knownRoutes["/"+route] = true

		// Check if it's a dynamic route (contains [param])
		if strings.Contains(route, "[") {
			// Convert dynamic segments to regex: [slug] → [^/]+, [...slug] → .+
			escaped := regexp.QuoteMeta(route)
			// Handle catch-all [...param]
			escaped = regexp.MustCompile(`\\\[\\\.\\\.\\\.\\w+\\\]`).ReplaceAllString(escaped, ".+")
			// Handle single dynamic [param]
			escaped = regexp.MustCompile(`\\\[\\w+\\\]`).ReplaceAllString(escaped, "[^/]+")
			if compiled, err := regexp.Compile("^/" + escaped + "$"); err == nil {
				dynamicRoutePatterns = append(dynamicRoutePatterns, compiled)
			}
		}
	}

	for filePath, content := range files {
		if !strings.HasSuffix(filePath, ".tsx") && !strings.HasSuffix(filePath, ".ts") {
			continue
		}
		if templates.IsTemplateFile(filePath) || isConfigFile(filePath) {
			continue
		}

		hrefMatches := internalHrefRegex.FindAllStringSubmatch(content, -1)
		for _, match := range hrefMatches {
			href := match[1]

			// Skip anchors-only, API routes, and hash links
			if strings.HasPrefix(href, "/#") || href == "/" {
				continue
			}
			// Skip API routes
			if strings.HasPrefix(href, "/api/") {
				continue
			}

			// Normalize: strip query params and trailing slash
			cleaned := href
			if idx := strings.Index(cleaned, "?"); idx >= 0 {
				cleaned = cleaned[:idx]
			}
			if idx := strings.Index(cleaned, "#"); idx >= 0 {
				cleaned = cleaned[:idx]
			}
			cleaned = strings.TrimRight(cleaned, "/")
			if cleaned == "" {
				cleaned = "/"
			}

			// Check static routes
			if knownRoutes[cleaned] {
				continue
			}

			// Check dynamic route patterns
			matchesDynamic := false
			for _, pattern := range dynamicRoutePatterns {
				if pattern.MatchString(cleaned) {
					matchesDynamic = true
					break
				}
			}
			if matchesDynamic {
				continue
			}

			errors = append(errors, validationError{
				File:  filePath,
				Error: fmt.Sprintf("Link href=\"%s\" points to a page that does not exist. Either create the page file (app%s/page.tsx) or remove the link.", href, cleaned),
				Type:  "broken_link",
			})
		}
	}

	// --- Check 8: Root layout must include <html> and <body> tags ---
	if layoutContent, ok := files["app/layout.tsx"]; ok {
		hasHTML := strings.Contains(layoutContent, "<html")
		hasBody := strings.Contains(layoutContent, "<body")
		if !hasHTML || !hasBody {
			missing := []string{}
			if !hasHTML {
				missing = append(missing, "<html>")
			}
			if !hasBody {
				missing = append(missing, "<body>")
			}
			errors = append(errors, validationError{
				File:  "app/layout.tsx",
				Error: fmt.Sprintf("Root layout is missing %s tags. Next.js App Router requires app/layout.tsx to include <html> and <body> tags wrapping {children}.", strings.Join(missing, " and ")),
				Type:  "missing_root_layout_tags",
			})
		}
	}

	// --- Check 9: Root layout must import globals.css ---
	if layoutContent, ok := files["app/layout.tsx"]; ok {
		hasGlobalsCSSImport := strings.Contains(layoutContent, `import './globals.css'`) ||
			strings.Contains(layoutContent, `import "./globals.css"`) ||
			strings.Contains(layoutContent, `import '../globals.css'`) ||
			strings.Contains(layoutContent, `import "../globals.css"`)
		if !hasGlobalsCSSImport {
			errors = append(errors, validationError{
				File:  "app/layout.tsx",
				Error: "Root layout is missing `import './globals.css'`. Without this import, all Tailwind styles and theme tokens are broken. Add `import './globals.css'` as the first import in app/layout.tsx.",
				Type:  "missing_globals_css_import",
			})
		}
	}

	// --- Check 10: Duplicate auth files (wemob-auth.ts + wemob-auth.tsx) ---
	if _, hasTS := files["lib/wemob-auth.ts"]; hasTS {
		if _, hasTSX := files["lib/wemob-auth.tsx"]; hasTSX {
			errors = append(errors, validationError{
				File:  "lib/wemob-auth.tsx",
				Error: "Duplicate auth file. Only lib/wemob-auth.ts should exist (server-only auth library). Remove lib/wemob-auth.tsx — it conflicts with the .ts file.",
				Type:  "duplicate_auth_file",
			})
		}
	}

	// --- Auth-specific checks (only when wemob-auth.ts exists) ---
	if authContent, hasAuth := files["lib/wemob-auth.ts"]; hasAuth {
		// Check A0: Auth file must export required functions (not just types)
		requiredAuthExports := []string{"getSession", "login", "register", "logout"}
		for _, fn := range requiredAuthExports {
			// Match "export async function getSession" or "export function getSession" or "export const getSession"
			if !strings.Contains(authContent, "export") || !strings.Contains(authContent, fn) {
				errors = append(errors, validationError{
					File:  "lib/wemob-auth.ts",
					Error: fmt.Sprintf("Auth library is missing required export '%s'. lib/wemob-auth.ts must export: getSession, login, register, logout (as async functions). Currently it may only have type exports.", fn),
					Type:  "auth_missing_export",
				})
				break // one error is enough — all are likely missing
			}
		}

		// Check A1: session.userId / session.username → should be session.user.id
		sessionDirectAccessRegex := regexp.MustCompile(`session\.(userId|username|email|name|role)\b`)
		for fp, fc := range files {
			if templates.IsTemplateFile(fp) || isConfigFile(fp) {
				continue
			}
			if !strings.HasSuffix(fp, ".tsx") && !strings.HasSuffix(fp, ".ts") {
				continue
			}
			if matches := sessionDirectAccessRegex.FindAllString(fc, -1); len(matches) > 0 {
				errors = append(errors, validationError{
					File:  fp,
					Error: fmt.Sprintf("Incorrect session access: '%s'. Session properties are nested under session.user — use session.user.id, session.user.email, etc.", matches[0]),
					Type:  "auth_session_access",
				})
			}
		}

		// Check A2: wemob-auth-client imported in API route files (should use server-side wemob-auth)
		for fp, fc := range files {
			if templates.IsTemplateFile(fp) || isConfigFile(fp) {
				continue
			}
			if !strings.HasPrefix(fp, "app/api/") {
				continue
			}
			if strings.Contains(fc, "wemob-auth-client") {
				errors = append(errors, validationError{
					File:  fp,
					Error: "API route imports from wemob-auth-client (client-only module). API routes must use server-side '@/lib/wemob-auth' instead (getSession, register, login, logout).",
					Type:  "auth_client_in_api",
				})
			}
			// getSession() takes no arguments — LLM often passes request/req
			if getSessionArgsRegex.MatchString(fc) {
				errors = append(errors, validationError{
					File:  fp,
					Error: "getSession() takes no arguments (it reads cookies internally via Next.js headers). Remove the argument: use `await getSession()` instead of `getSession(request)` or `getSession(req)`.",
					Type:  "auth_session_args",
				})
			}
		}

		// Check A3: wemob-auth.ts must import 'server-only' to enforce boundary
		if !strings.Contains(authContent, `"server-only"`) && !strings.Contains(authContent, `'server-only'`) {
			errors = append(errors, validationError{
				File:  "lib/wemob-auth.ts",
				Error: "Auth server library is missing `import 'server-only'` at the top. This import prevents accidental client-side usage and must be the first import.",
				Type:  "auth_missing_server_only",
			})
		}

		// Check A4: wemob-auth-client.tsx login/register must set user state directly from API response.
		// Do NOT call refreshSession() after login — the browser may not have processed the Set-Cookie
		// header yet, causing the session endpoint to return null and triggering a redirect loop.
		// Instead, the login API returns the user object and the client sets state directly.
		if clientAuth, hasClientAuth := files["lib/wemob-auth-client.tsx"]; hasClientAuth {
			hasLoginFunc := strings.Contains(clientAuth, "login") && strings.Contains(clientAuth, "fetch")
			setsUserDirectly := strings.Contains(clientAuth, "setUser(data.user)") || strings.Contains(clientAuth, "setUser(data?.user)")
			if hasLoginFunc && !setsUserDirectly {
				errors = append(errors, validationError{
					File:  "lib/wemob-auth-client.tsx",
					Error: "Auth client login/register functions must set user state directly from the API response: `if (data.success && data.user) { setUser(data.user); }`. Do NOT call refreshSession() after login — the browser may not have processed the Set-Cookie header yet, causing a redirect loop.",
					Type:  "auth_missing_direct_setuser",
				})
			}
		}

		// Check A5: Login/register pages must not redirect while isLoading is true.
		// The LLM often writes: useEffect(() => { if (user) router.push('/') }, [user])
		// which fires during initial load when user is briefly set, causing redirect loops.
		for fp, fc := range files {
			isLoginPage := strings.Contains(fp, "login") || strings.Contains(fp, "register")
			if !isLoginPage || !strings.HasSuffix(fp, ".tsx") {
				continue
			}
			if templates.IsTemplateFile(fp) || isConfigFile(fp) {
				continue
			}
			// Detect redirect based on user state without isLoading guard
			hasUserRedirect := strings.Contains(fc, "router.push") || strings.Contains(fc, "router.replace") || strings.Contains(fc, "redirect(")
			hasUseAuth := strings.Contains(fc, "useAuth")
			hasLoadingCheck := strings.Contains(fc, "isLoading")
			if hasUserRedirect && hasUseAuth && !hasLoadingCheck {
				errors = append(errors, validationError{
					File:  fp,
					Error: "Login/register page redirects based on auth state but does not check isLoading. This causes a redirect loop: user is null during initial session fetch, page redirects to itself. Add an isLoading check: `if (!isLoading && user) router.push('/')`.",
					Type:  "auth_redirect_race",
				})
			}
		}
	}

	// --- E-Commerce specific checks (only when wemob-store.ts exists) ---
	if _, hasStore := files["lib/wemob-store.ts"]; hasStore {

		// Check E2: Manual price formatting (should use formatPrice())
		manualPriceRegex := regexp.MustCompile(`(?:price|cents|amount)\s*[/]\s*100|\.toFixed\(\s*2\s*\)`)
		for fp, fc := range files {
			if templates.IsTemplateFile(fp) || isConfigFile(fp) || !strings.HasSuffix(fp, ".tsx") && !strings.HasSuffix(fp, ".ts") {
				continue
			}
			if fp == "lib/wemob-store.ts" {
				continue
			}
			if manualPriceRegex.MatchString(fc) {
				errors = append(errors, validationError{
					File:  fp,
					Error: "Manual price formatting detected (dividing by 100 or .toFixed(2)). Use formatPrice() from @/lib/wemob-store instead for proper currency formatting.",
					Type:  "ecommerce_manual_price",
				})
			}
		}

		// Check E3: Hardcoded product/category arrays (should use SDK)
		hardcodedDataPatterns := []*regexp.Regexp{
			regexp.MustCompile(`(?i)const\s+(?:products|items|bestSellers|best_sellers|featuredProducts|featured_products|featured|newArrivals|new_arrivals|topProducts|top_products|deals|popularProducts|popular_products|relatedProducts|related_products|allProducts|all_products)\s*(?::\s*\w+(?:\[\])?\s*)?=\s*\[`),
			regexp.MustCompile(`(?i)const\s+(?:categories|collections|productCategories|product_categories)\s*(?::\s*\w+(?:\[\])?\s*)?=\s*\[`),
		}
		for fp, fc := range files {
			if templates.IsTemplateFile(fp) || isConfigFile(fp) {
				continue
			}
			if !strings.HasSuffix(fp, ".tsx") && !strings.HasSuffix(fp, ".ts") {
				continue
			}
			// Skip files that also import from SDK (may define local filtered subsets)
			if strings.Contains(fc, "@/lib/wemob-store") {
				continue
			}
			for _, pat := range hardcodedDataPatterns {
				if pat.MatchString(fc) {
					errors = append(errors, validationError{
						File:  fp,
						Error: "Hardcoded product/category data detected. E-commerce projects must fetch all product and category data from the Wemob Storefront SDK (@/lib/wemob-store). Use useProducts(), useCategories(), or getProducts() instead.",
						Type:  "ecommerce_hardcoded_products",
					})
					break // one error per file
				}
			}
		}

		// Check E4: Required LLM-generated pages
		// Products, product detail, category, and policy pages are now templates — no required LLM pages remain.

		// Check E5: Product detail page (any dynamic slug page under products)
		hasProductDetail := false
		for fp := range files {
			if strings.HasPrefix(fp, "app/products/[") && strings.HasSuffix(fp, "/page.tsx") {
				hasProductDetail = true
				break
			}
		}
		if !hasProductDetail {
			errors = append(errors, validationError{
				File:  "app/products/[slug]/page.tsx",
				Error: "Missing product detail page. E-commerce projects need a dynamic route like app/products/[slug]/page.tsx to display individual products.",
				Type:  "ecommerce_missing_page",
			})
		}

		// Check E5b: Category page (any dynamic slug page under categories)
		hasCategoryPage := false
		for fp := range files {
			if strings.HasPrefix(fp, "app/categories/[") && strings.HasSuffix(fp, "/page.tsx") {
				hasCategoryPage = true
				break
			}
		}
		if !hasCategoryPage {
			errors = append(errors, validationError{
				File:  "app/categories/[slug]/page.tsx",
				Error: "Missing category page. E-commerce projects need a dynamic route like app/categories/[slug]/page.tsx to display products by category.",
				Type:  "ecommerce_missing_page",
			})
		}

		// Check E6: href="#" placeholder links (threshold > 2 per file)
		placeholderHrefRegex := regexp.MustCompile(`href=["']#["']`)
		for fp, fc := range files {
			if templates.IsTemplateFile(fp) || isConfigFile(fp) {
				continue
			}
			if !strings.HasSuffix(fp, ".tsx") && !strings.HasSuffix(fp, ".ts") {
				continue
			}
			matches := placeholderHrefRegex.FindAllStringIndex(fc, -1)
			if len(matches) > 2 {
				errors = append(errors, validationError{
					File:  fp,
					Error: fmt.Sprintf("Found %d placeholder href=\"#\" links. Every link must point to a real route or valid external URL. Replace with actual page routes (e.g., /products, /about, /contact).", len(matches)),
					Type:  "ecommerce_placeholder_links",
				})
			}
		}

		// Check E7: SDK usage in LLM-generated pages
		sdkUsageChecks := map[string][]string{
			"app/page.tsx": {"useProducts", "useCategories"},
		}
		for fp, requiredImports := range sdkUsageChecks {
			fc, exists := files[fp]
			if !exists {
				continue
			}
			if templates.IsTemplateFile(fp) {
				continue
			}
			hasSDK := false
			for _, imp := range requiredImports {
				if strings.Contains(fc, imp) {
					hasSDK = true
					break
				}
			}
			if !hasSDK {
				errors = append(errors, validationError{
					File:  fp,
					Error: fmt.Sprintf("E-commerce page %s does not use SDK hooks (%s). All product/category data must come from the Wemob Storefront SDK, not hardcoded data.", fp, strings.Join(requiredImports, " or ")),
					Type:  "ecommerce_missing_sdk_usage",
				})
			}
		}

		// Check E8: Providers wrapper in layout
		if layoutContent, ok := files["app/layout.tsx"]; ok {
			hasProviders := strings.Contains(layoutContent, "Providers") && (strings.Contains(layoutContent, "@/components/ecommerce/providers") || strings.Contains(layoutContent, "components/providers"))
			hasDirectCartProvider := strings.Contains(layoutContent, "CartProvider")
			if !hasProviders && !hasDirectCartProvider {
				errors = append(errors, validationError{
					File:  "app/layout.tsx",
					Error: "E-commerce layout must use <Providers> from @/components/ecommerce/providers (or directly wrap with <CartProvider>). Import and wrap {children} with it in app/layout.tsx.",
					Type:  "ecommerce_missing_providers",
				})
			}
		}

		// Check E9: SiteHeader and SiteFooter in layout
		if layoutContent, ok := files["app/layout.tsx"]; ok {
			if !strings.Contains(layoutContent, "SiteHeader") {
				errors = append(errors, validationError{
					File:  "app/layout.tsx",
					Error: "E-commerce layout must include <SiteHeader /> from @/components/ecommerce/site-header. This template provides navigation, cart icon, and CartDrawer.",
					Type:  "ecommerce_missing_layout_components",
				})
			}
			if !strings.Contains(layoutContent, "SiteFooter") {
				errors = append(errors, validationError{
					File:  "app/layout.tsx",
					Error: "E-commerce layout must include <SiteFooter /> from @/components/ecommerce/site-footer. This template provides footer links to all pages.",
					Type:  "ecommerce_missing_layout_components",
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


// autoFixImportMismatches performs deterministic fixes for import/export style mismatches.
// Case 1: default import from a named-export-only file → rewrite to named import.
// Case 2: single named import from a default-export-only file → rewrite to default import.
// Returns the number of fixed files and the list of changed file paths.
func autoFixImportMismatches(files map[string]string) (int, []string) {
	var changedPaths []string

	for filePath, content := range files {
		if templates.IsTemplateFile(filePath) || isConfigFile(filePath) {
			continue
		}
		if !strings.HasSuffix(filePath, ".tsx") && !strings.HasSuffix(filePath, ".ts") {
			continue
		}

		updated := content
		changed := false

		// Case 1: Default import from named-export-only file
		// import Button from '@/components/ui/button' → import { Button } from '@/components/ui/button'
		for _, match := range defaultImportRegex.FindAllStringSubmatch(updated, -1) {
			fullMatch := match[0]
			importName := match[1]
			importPath := match[2]

			targetContent := resolveFileContent(files, importPath)
			if targetContent == "" {
				continue
			}

			hasDefault := exportDefaultPattern.MatchString(targetContent)
			hasNamedValue := namedValueExportPattern.MatchString(targetContent)

			if !hasDefault && hasNamedValue {
				// Verify the imported name matches a named export in the target
				for _, ne := range namedValueExportIdentRegex.FindAllStringSubmatch(targetContent, -1) {
					if ne[1] == importName {
						newImport := fmt.Sprintf("import { %s } from '@/%s'", importName, importPath)
						updated = strings.Replace(updated, fullMatch, newImport, 1)
						changed = true
						break
					}
				}
			}
		}

		// Case 2: Single named import from default-export-only file
		// import { Hero } from '@/components/hero' → import Hero from '@/components/hero'
		for _, match := range singleNamedImportRegex.FindAllStringSubmatch(updated, -1) {
			fullMatch := match[0]
			importName := match[1]
			importPath := match[2]

			targetContent := resolveFileContent(files, importPath)
			if targetContent == "" {
				continue
			}

			hasDefault := exportDefaultPattern.MatchString(targetContent)
			hasNamedValue := namedValueExportPattern.MatchString(targetContent)

			if hasDefault && !hasNamedValue {
				newImport := fmt.Sprintf("import %s from '@/%s'", importName, importPath)
				updated = strings.Replace(updated, fullMatch, newImport, 1)
				changed = true
			}
		}

		// Case 3: Multi-named import from default-export-only file (type exports don't count)
		// import { Hero, HeroProps } from '@/components/hero' → import Hero from '@/components/hero' + import type { HeroProps } from '...'
		for _, match := range multiNamedImportRegex.FindAllStringSubmatch(updated, -1) {
			fullMatch := match[0]
			namesList := match[1]
			importPath := match[2]

			targetContent := resolveFileContent(files, importPath)
			if targetContent == "" {
				continue
			}

			hasDefault := exportDefaultPattern.MatchString(targetContent)
			hasNamedValue := namedValueExportPattern.MatchString(targetContent)
			if !hasDefault || hasNamedValue {
				continue
			}

			// Extract default export function name
			defaultNameMatch := exportDefaultFuncNameRegex.FindStringSubmatch(targetContent)
			if defaultNameMatch == nil {
				continue
			}
			defaultName := defaultNameMatch[1]

			// Split import names, find which is the default export
			names := strings.Split(namesList, ",")
			var typeNames []string
			foundDefault := false
			for _, n := range names {
				n = strings.TrimSpace(n)
				if n == defaultName {
					foundDefault = true
				} else {
					typeNames = append(typeNames, n)
				}
			}
			if !foundDefault {
				continue
			}

			newImport := fmt.Sprintf("import %s from '@/%s'", defaultName, importPath)
			if len(typeNames) > 0 {
				newImport += fmt.Sprintf("\nimport type { %s } from '@/%s'", strings.Join(typeNames, ", "), importPath)
			}
			updated = strings.Replace(updated, fullMatch, newImport, 1)
			changed = true
		}

		// Case 4: Single named import with wrong name from a file that has exactly one named value export
		// import { Badge } from '@/components/ui/button' where button only exports Button
		// → import { Button } from '@/components/ui/button'
		for _, match := range singleNamedImportRegex.FindAllStringSubmatch(updated, -1) {
			fullMatch := match[0]
			importName := match[1]
			importPath := match[2]

			targetContent := resolveFileContent(files, importPath)
			if targetContent == "" {
				continue
			}

			// Check if the import name actually exists as an export — if so, skip
			allNamedExports := namedValueExportIdentRegex.FindAllStringSubmatch(targetContent, -1)
			found := false
			for _, ne := range allNamedExports {
				if ne[1] == importName {
					found = true
					break
				}
			}
			if found {
				continue // import is correct
			}

			// Only fix when target has exactly one named value export and both names are PascalCase
			if len(allNamedExports) != 1 {
				continue
			}
			actualExport := allNamedExports[0][1]
			if !isPascalCase(importName) || !isPascalCase(actualExport) {
				continue
			}

			newImport := fmt.Sprintf("import { %s } from '@/%s'", actualExport, importPath)
			updated = strings.Replace(updated, fullMatch, newImport, 1)
			changed = true
		}

		if changed {
			files[filePath] = updated
			changedPaths = append(changedPaths, filePath)
		}
	}

	return len(changedPaths), changedPaths
}

// autoFixFontWeights scans layout files for non-variable Google Fonts that are
// missing the required weight parameter and injects it. This fixes the common
// "Missing weight for X. Available weights: 400" Next.js build error.
func autoFixFontWeights(files map[string]string) (int, []string) {
	var changedPaths []string

	// Pattern: FontName({ subsets: [...], ... }) — match font constructors to check for weight
	fontCallRegex := regexp.MustCompile(`(\w+)\s*\(\s*\{([^}]*)\}\s*\)`)

	for filePath, content := range files {
		if !strings.Contains(content, `next/font/google`) {
			continue
		}

		updated := content
		changed := false

		matches := fontCallRegex.FindAllStringSubmatchIndex(updated, -1)
		// Process matches in reverse order to preserve indices
		for i := len(matches) - 1; i >= 0; i-- {
			m := matches[i]
			funcName := updated[m[2]:m[3]]    // e.g. "Bebas_Neue"
			argsBlock := updated[m[4]:m[5]]    // e.g. `subsets: ["latin"], display: "swap"`

			// Convert import name back to font name (Bebas_Neue -> Bebas Neue)
			fontName := strings.ReplaceAll(funcName, "_", " ")

			weights, isNonVariable := templates.NonVariableFonts[fontName]
			if !isNonVariable {
				continue
			}

			// Already has weight specified
			if strings.Contains(argsBlock, "weight") {
				continue
			}

			// Pick the best weight (prefer 400)
			w := weights[0]
			for _, ww := range weights {
				if ww == "400" {
					w = "400"
					break
				}
			}

			// Insert weight before the closing of the args block
			newArgs := strings.TrimRight(argsBlock, " \t\n") + fmt.Sprintf(`, weight: "%s"`, w)
			updated = updated[:m[4]] + newArgs + updated[m[5]:]
			changed = true
		}

		if changed {
			files[filePath] = updated
			changedPaths = append(changedPaths, filePath)
		}
	}

	return len(changedPaths), changedPaths
}

// autoFixMissingUseClient scans files for patterns that require "use client" and
// prepends the directive if missing. This is a fast deterministic fix that avoids
// needing an LLM call for common cases (React hooks, framer-motion, wemob-store).
func autoFixMissingUseClient(files map[string]string) (int, []string) {
	var changedPaths []string

	for filePath, content := range files {
		if templates.IsTemplateFile(filePath) || isConfigFile(filePath) {
			continue
		}
		if !strings.HasSuffix(filePath, ".tsx") && !strings.HasSuffix(filePath, ".ts") {
			continue
		}

		hasUseClient := strings.Contains(content, `"use client"`) || strings.Contains(content, `'use client'`)
		if hasUseClient {
			continue
		}

		// Never add "use client" to layout files — they are server components that
		// may legitimately import client components (e.g. AuthProvider).
		if strings.HasSuffix(filePath, "/layout.tsx") || filePath == "layout.tsx" {
			continue
		}

		// Files that export metadata are Server Components. If the file also uses
		// hooks, remove the metadata export and add "use client" — a working page
		// is more important than SEO metadata, and extracting a wrapper is fragile.
		if metadataExportPattern.MatchString(content) {
			needsClient := hookPattern.MatchString(content) ||
				framerMotionImportPattern.MatchString(content) ||
				strings.Contains(content, `@/lib/wemob-store`) ||
				strings.Contains(content, `@/lib/wemob-auth-client`)
			if !needsClient {
				continue
			}
			// Remove metadata export so we can safely add "use client"
			content = removeMetadataExport(content)
			files[filePath] = "\"use client\"\n\n" + content
			changedPaths = append(changedPaths, filePath)
			continue
		}

		needsUseClient := false
		// React hooks
		if hookPattern.MatchString(content) {
			needsUseClient = true
		}
		// framer-motion
		if framerMotionImportPattern.MatchString(content) {
			needsUseClient = true
		}
		// @/lib/wemob-store (client-only module)
		if strings.Contains(content, `@/lib/wemob-store`) {
			needsUseClient = true
		}
		// @/lib/wemob-auth-client (client-only module)
		if strings.Contains(content, `@/lib/wemob-auth-client`) {
			needsUseClient = true
		}

		if needsUseClient {
			files[filePath] = "\"use client\"\n\n" + content
			changedPaths = append(changedPaths, filePath)
		}
	}

	return len(changedPaths), changedPaths
}

// autoFixTSFilesWithJSX renames .ts files to .tsx when they contain JSX syntax.
// This is a common AI mistake — writing JSX into a .ts file, which causes parse errors.
func autoFixTSFilesWithJSX(files map[string]string) (int, []string) {
	var changedPaths []string
	// Collect renames first to avoid mutating map during iteration
	type rename struct{ from, to string }
	var renames []rename

	for filePath, content := range files {
		if !strings.HasSuffix(filePath, ".ts") || strings.HasSuffix(filePath, ".d.ts") {
			continue
		}
		if templates.IsTemplateFile(filePath) || isConfigFile(filePath) {
			continue
		}
		// Check for JSX syntax: <Component, <div, etc.
		if jsxTagPattern.MatchString(content) {
			newPath := strings.TrimSuffix(filePath, ".ts") + ".tsx"
			// Only rename if the .tsx version doesn't already exist
			if _, exists := files[newPath]; !exists {
				renames = append(renames, rename{from: filePath, to: newPath})
			}
		}
	}

	for _, r := range renames {
		files[r.to] = files[r.from]
		delete(files, r.from)
		changedPaths = append(changedPaths, r.from, r.to)
	}

	return len(renames), changedPaths
}

// autoFixMetadataInClientComponent removes "use client" from files that export metadata.
// In Next.js App Router, metadata must be exported from Server Components — having
// "use client" alongside metadata export causes a build error.
//
// For app/layout.tsx specifically, if the file also uses React hooks (useState, useEffect,
// etc.), simply removing "use client" would break the hooks. In that case, we extract the
// client body into a separate "components/layout-client.tsx" wrapper and make the root
// layout a thin Server Component that imports it.
func autoFixMetadataInClientComponent(files map[string]string) (int, []string) {
	var changedPaths []string

	for filePath, content := range files {
		if !strings.HasSuffix(filePath, ".tsx") && !strings.HasSuffix(filePath, ".ts") {
			continue
		}
		if templates.IsTemplateFile(filePath) || isConfigFile(filePath) {
			continue
		}

		hasUseClient := strings.Contains(content, `"use client"`) || strings.Contains(content, `'use client'`)
		if !hasUseClient {
			continue
		}

		// Check if file exports metadata (Next.js page/layout metadata)
		if !metadataExportPattern.MatchString(content) {
			continue
		}

		// Special handling for root layout: if it also uses hooks, extract a client wrapper
		if filePath == "app/layout.tsx" && hookPattern.MatchString(content) {
			serverLayout, clientWrapper := splitLayoutIntoServerAndClient(content)
			if serverLayout != "" && clientWrapper != "" {
				files[filePath] = serverLayout
				files["components/layout-client.tsx"] = clientWrapper
				changedPaths = append(changedPaths, filePath, "components/layout-client.tsx")
				continue
			}
		}

		// Remove the "use client" directive — metadata must be in Server Components
		updated := removeUseClientDirective(content)

		if updated != content {
			files[filePath] = updated
			changedPaths = append(changedPaths, filePath)
		}
	}

	return len(changedPaths), changedPaths
}

// removeUseClientDirective strips the "use client" directive from file content.
func removeUseClientDirective(content string) string {
	for _, pattern := range []string{
		"\"use client\"\n\n",
		"\"use client\"\n",
		"'use client'\n\n",
		"'use client'\n",
		"\"use client\"",
		"'use client'",
	} {
		if replaced := strings.Replace(content, pattern, "", 1); replaced != content {
			return replaced
		}
	}
	return content
}

// removeMetadataExport strips the "export const metadata = { ... }" block from file content.
// Used when a page file has both metadata and hooks — we sacrifice metadata to make it a
// valid Client Component, since hooks are required for the page to function.
func removeMetadataExport(content string) string {
	idx := strings.Index(content, "export const metadata")
	if idx == -1 {
		return content
	}

	// Find the end of the metadata block by matching braces (string-literal-aware)
	end := findClosingBrace(content, idx)
	if end == -1 {
		return content
	}

	// Skip optional semicolon and trailing newlines
	for end < len(content) && (content[end] == ';' || content[end] == '\n' || content[end] == '\r') {
		end++
	}
	// Also remove the Metadata type import if present
	result := content[:idx] + content[end:]
	result = strings.Replace(result, "import type { Metadata } from \"next\"\n", "", 1)
	result = strings.Replace(result, "import type { Metadata } from 'next'\n", "", 1)
	result = strings.Replace(result, "import { Metadata } from \"next\"\n", "", 1)
	result = strings.Replace(result, "import { Metadata } from 'next'\n", "", 1)
	return result
}

// splitLayoutIntoServerAndClient splits a root layout.tsx that incorrectly has both
// "use client" and metadata export into two files:
//  1. A Server Component layout (with metadata, fonts, <html>/<body>) that imports LayoutClient
//  2. A Client Component wrapper (with hooks, providers, event handlers)
//
// This is a best-effort extraction — it preserves the metadata block and font setup in the
// server layout while moving everything else into the client wrapper.
func splitLayoutIntoServerAndClient(content string) (serverLayout string, clientWrapper string) {
	// Remove the "use client" directive for analysis
	cleaned := removeUseClientDirective(content)

	// Extract metadata block
	metaStart := strings.Index(cleaned, "export const metadata")
	if metaStart == -1 {
		return "", ""
	}
	// Find the closing of the metadata object — match braces
	metaEnd := findClosingBrace(cleaned, metaStart)
	if metaEnd == -1 {
		return "", ""
	}
	// Include trailing semicolon and newline
	if metaEnd < len(cleaned) && cleaned[metaEnd] == ';' {
		metaEnd++
	}
	metadataBlock := cleaned[metaStart:metaEnd]

	// Extract import lines (fonts, types, CSS)
	var serverImports []string
	var clientImports []string
	lines := strings.Split(cleaned, "\n")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "import") {
			continue
		}
		// Font and type imports stay in server layout
		if strings.Contains(trimmed, "next/font") ||
			strings.Contains(trimmed, "type { Metadata") ||
			strings.Contains(trimmed, "type {Metadata") ||
			strings.Contains(trimmed, "globals.css") {
			serverImports = append(serverImports, line)
		} else {
			// Everything else (components, providers) goes to client
			clientImports = append(clientImports, line)
		}
	}

	// Extract font constructors (const fontName = FontName({...}))
	var fontConstructors []string
	fontConstRegex := regexp.MustCompile(`(?m)^const\s+\w+\s*=\s*\w+\s*\([^)]*\)\s*;?\s*$`)
	for _, match := range fontConstRegex.FindAllString(cleaned, -1) {
		// Only include if it looks like a font constructor (references a capitalized function)
		if regexp.MustCompile(`=\s*[A-Z]`).MatchString(match) {
			fontConstructors = append(fontConstructors, match)
		}
	}

	// Extract the body className from the original layout
	bodyClassRegex := regexp.MustCompile(`<body[^>]*className=\{?` + "`" + `([^` + "`" + `]*)` + "`" + `\}?`)
	bodyClass := "antialiased"
	if m := bodyClassRegex.FindStringSubmatch(cleaned); len(m) > 1 {
		bodyClass = m[1]
	} else {
		// Try simpler className="..." pattern
		simpleClassRegex := regexp.MustCompile(`<body[^>]*className="([^"]*)"`)
		if m := simpleClassRegex.FindStringSubmatch(cleaned); len(m) > 1 {
			bodyClass = m[1]
		}
	}

	// Build server layout
	var sb strings.Builder
	for _, imp := range serverImports {
		sb.WriteString(imp + "\n")
	}
	sb.WriteString(`import { LayoutClient } from "@/components/layout-client";` + "\n")
	sb.WriteString(`import "./globals.css";` + "\n\n")
	for _, fc := range fontConstructors {
		sb.WriteString(fc + "\n")
	}
	sb.WriteString("\n" + metadataBlock + "\n\n")

	// Build body class expression — check if it has template literal interpolation
	if strings.Contains(bodyClass, "${") {
		sb.WriteString(fmt.Sprintf("export default function RootLayout({ children }: { children: React.ReactNode }) {\n  return (\n    <html lang=\"en\" suppressHydrationWarning>\n      <body className={`%s`}>\n        <LayoutClient>{children}</LayoutClient>\n      </body>\n    </html>\n  );\n}\n", bodyClass))
	} else {
		sb.WriteString(fmt.Sprintf("export default function RootLayout({ children }: { children: React.ReactNode }) {\n  return (\n    <html lang=\"en\" suppressHydrationWarning>\n      <body className=\"%s\">\n        <LayoutClient>{children}</LayoutClient>\n      </body>\n    </html>\n  );\n}\n", bodyClass))
	}
	serverLayout = sb.String()

	// Build client wrapper — extract the return body from the original component
	var cb strings.Builder
	cb.WriteString("\"use client\"\n\n")
	for _, imp := range clientImports {
		cb.WriteString(imp + "\n")
	}
	cb.WriteString("\n")

	// Extract hook declarations and state from the original function body
	funcBodyRegex := regexp.MustCompile(`(?s)export\s+default\s+function\s+\w+\s*\([^)]*\)\s*\{(.+)\}`)
	funcMatch := funcBodyRegex.FindStringSubmatch(cleaned)

	if len(funcMatch) > 1 {
		body := strings.TrimSpace(funcMatch[1])
		// Find the return statement
		returnIdx := strings.LastIndex(body, "return")

		preReturn := ""
		returnBlock := body
		if returnIdx > 0 {
			preReturn = strings.TrimSpace(body[:returnIdx])
			returnBlock = strings.TrimSpace(body[returnIdx:])
		}

		// Strip <html> and <body> wrappers from the return — just keep inner content
		innerContent := extractInnerJSXContent(returnBlock)

		cb.WriteString("export function LayoutClient({ children }: { children: React.ReactNode }) {\n")
		if preReturn != "" {
			cb.WriteString("  " + strings.ReplaceAll(preReturn, "\n", "\n  ") + "\n\n")
		}
		if innerContent != "" {
			cb.WriteString("  return (\n    " + strings.TrimSpace(innerContent) + "\n  );\n")
		} else {
			cb.WriteString("  return <>{children}</>;\n")
		}
		cb.WriteString("}\n")
	} else {
		// Fallback: simple pass-through wrapper
		cb.WriteString("export function LayoutClient({ children }: { children: React.ReactNode }) {\n  return <>{children}</>;\n}\n")
	}
	clientWrapper = cb.String()

	return serverLayout, clientWrapper
}

// extractInnerJSXContent strips <html> and <body> tags from a return block,
// returning only the inner content that should go into the client wrapper.
func extractInnerJSXContent(returnBlock string) string {
	// Remove "return (" and trailing ");" or ")"
	inner := returnBlock
	if idx := strings.Index(inner, "return"); idx >= 0 {
		inner = inner[idx+6:]
	}
	inner = strings.TrimSpace(inner)
	if strings.HasPrefix(inner, "(") {
		inner = inner[1:]
	}
	inner = strings.TrimSpace(inner)
	if strings.HasSuffix(inner, ");") {
		inner = inner[:len(inner)-2]
	} else if strings.HasSuffix(inner, ")") {
		inner = inner[:len(inner)-1]
	}
	inner = strings.TrimSpace(inner)

	// Strip <html ...> ... </html>
	htmlOpenRegex := regexp.MustCompile(`(?s)^<html[^>]*>\s*`)
	if htmlOpenRegex.MatchString(inner) {
		inner = htmlOpenRegex.ReplaceAllString(inner, "")
		if idx := strings.LastIndex(inner, "</html>"); idx >= 0 {
			inner = strings.TrimSpace(inner[:idx])
		}
	}

	// Strip <body ...> ... </body>
	bodyOpenRegex := regexp.MustCompile(`(?s)^<body[^>]*>\s*`)
	if bodyOpenRegex.MatchString(inner) {
		inner = bodyOpenRegex.ReplaceAllString(inner, "")
		if idx := strings.LastIndex(inner, "</body>"); idx >= 0 {
			inner = strings.TrimSpace(inner[:idx])
		}
	}

	return strings.TrimSpace(inner)
}

// findClosingBrace finds the position after the closing brace that matches the first
// opening brace found at or after startPos.
func findClosingBrace(s string, startPos int) int {
	depth := 0
	inString := false
	var stringChar byte
	for i := startPos; i < len(s); i++ {
		ch := s[i]
		if inString {
			if ch == stringChar && (i == 0 || s[i-1] != '\\') {
				inString = false
			}
			continue
		}
		if ch == '"' || ch == '\'' || ch == '`' {
			inString = true
			stringChar = ch
			continue
		}
		if ch == '{' {
			depth++
		} else if ch == '}' {
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return -1
}

// autoFixRootLayoutTags wraps the root layout's returned JSX with <html> and <body>
// tags when they are missing. Next.js App Router requires these in app/layout.tsx.
func autoFixRootLayoutTags(files map[string]string) (bool, string) {
	content, ok := files["app/layout.tsx"]
	if !ok {
		return false, ""
	}

	hasHTML := strings.Contains(content, "<html")
	hasBody := strings.Contains(content, "<body")
	if hasHTML && hasBody {
		return false, ""
	}

	// Strategy: find the return statement's JSX and wrap it.
	// Look for patterns like "return (\n" or "return <" and wrap the JSX.

	// If missing both, wrap {children} or the return JSX with <html><body>...</body></html>
	// If only missing one, add just that tag.

	if !hasHTML && !hasBody {
		// Most common case: layout returns a fragment or div instead of <html>
		// Try to find {children} and wrap it
		if strings.Contains(content, "{children}") {
			// Replace the first {children} occurrence that's in JSX context
			// with <html lang="en"><body>{children}</body></html>
			content = strings.Replace(content, "{children}", `<html lang="en"><body>{children}</body></html>`, 1)
			files["app/layout.tsx"] = content
			return true, "app/layout.tsx"
		}
	} else if !hasHTML {
		// Has <body> but no <html> — wrap <body>...</body> with <html>
		content = strings.Replace(content, "<body", "<html lang=\"en\"><body", 1)
		content = strings.Replace(content, "</body>", "</body></html>", 1)
		files["app/layout.tsx"] = content
		return true, "app/layout.tsx"
	} else if !hasBody {
		// Has <html> but no <body> — add <body> inside <html> around {children}
		if strings.Contains(content, "{children}") {
			content = strings.Replace(content, "{children}", "<body>{children}</body>", 1)
			files["app/layout.tsx"] = content
			return true, "app/layout.tsx"
		}
	}

	return false, ""
}

// autoFixClassNameConcat replaces JSX className attributes that use string concatenation
// with template literals. This fixes a common LLM mistake where the concatenation expression
// produces malformed JSX that fails to parse (e.g. unclosed braces, misplaced parens).
//
// Pattern: className={"base " + (condition ? "a" : "b")}
// Fixed:   className={`base ${condition ? "a" : "b"}`}
var classNameConcatRegex = regexp.MustCompile(
	`className=\{("(?:[^"\\]|\\.)*")\s*\+\s*\(([^)]+)\)\}`,
)

func autoFixClassNameConcat(files map[string]string) (int, []string) {
	var changedPaths []string
	fixed := 0

	for filePath, content := range files {
		if !strings.HasSuffix(filePath, ".tsx") && !strings.HasSuffix(filePath, ".jsx") {
			continue
		}
		if templates.IsTemplateFile(filePath) || isConfigFile(filePath) {
			continue
		}

		updated := classNameConcatRegex.ReplaceAllStringFunc(content, func(match string) string {
			parts := classNameConcatRegex.FindStringSubmatch(match)
			if len(parts) < 3 {
				return match
			}
			// parts[1] = "base string" (quoted), parts[2] = condition ? "a" : "b"
			base := parts[1]
			// Remove surrounding quotes from the base string
			base = strings.Trim(base, `"`)
			expr := parts[2]
			return fmt.Sprintf("className={`%s${%s}`}", base, expr)
		})

		if updated != content {
			files[filePath] = updated
			changedPaths = append(changedPaths, filePath)
			fixed++
		}
	}

	return fixed, changedPaths
}

// autoFixUnterminatedJSXStrings fixes unterminated JSX string expressions where the
// LLM opens {"text but forgets the closing "}. The parser then interprets the / in
// the next closing tag (e.g. </h2>) as a regexp literal, causing a build error.
//
// Pattern:  {"some text\n           </h2>
// Fixed:    {"some text"}\n           </h2>
var unterminatedJSXStringRegex = regexp.MustCompile(`(?m)\{"([^"\n]*)$`)

func autoFixUnterminatedJSXStrings(files map[string]string) (int, []string) {
	var changedPaths []string
	fixed := 0

	for filePath, content := range files {
		if !strings.HasSuffix(filePath, ".tsx") && !strings.HasSuffix(filePath, ".jsx") {
			continue
		}
		if templates.IsTemplateFile(filePath) || isConfigFile(filePath) {
			continue
		}

		updated := unterminatedJSXStringRegex.ReplaceAllString(content, `{"$1"}`)
		if updated != content {
			files[filePath] = updated
			changedPaths = append(changedPaths, filePath)
			fixed++
			slog.Warn("Auto-fixed unterminated JSX string expression", "path", filePath)
		}
	}

	return fixed, changedPaths
}

// autoFixMissingForceDynamic adds `export const dynamic = "force-dynamic"` to page.tsx files
// that import from @/lib/db but don't already have a dynamic route segment config.
// Without this, Next.js statically renders DB-backed pages at build time, serving stale data.
func autoFixMissingForceDynamic(files map[string]string) (int, []string) {
	var changedPaths []string
	fixed := 0

	for filePath, content := range files {
		// Only apply to page files
		if !strings.HasSuffix(filePath, "/page.tsx") && !strings.HasSuffix(filePath, "/page.ts") && filePath != "page.tsx" {
			continue
		}
		// Must import from @/lib/db
		if !strings.Contains(content, `@/lib/db`) {
			continue
		}
		// Skip if already has dynamic export
		if strings.Contains(content, `export const dynamic`) {
			continue
		}
		// Skip "use client" files — they don't do server-side data fetching
		if strings.Contains(content, `"use client"`) || strings.Contains(content, `'use client'`) {
			continue
		}

		// Insert after the last import line
		lines := strings.Split(content, "\n")
		lastImportIdx := -1
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "import ") || strings.HasPrefix(trimmed, "import{") {
				lastImportIdx = i
			}
		}
		if lastImportIdx == -1 {
			continue
		}

		// Insert the dynamic export after the last import
		newLines := make([]string, 0, len(lines)+2)
		newLines = append(newLines, lines[:lastImportIdx+1]...)
		newLines = append(newLines, "", `export const dynamic = "force-dynamic";`)
		newLines = append(newLines, lines[lastImportIdx+1:]...)
		files[filePath] = strings.Join(newLines, "\n")
		changedPaths = append(changedPaths, filePath)
		fixed++
		slog.Info("Auto-fixed missing force-dynamic export", "path", filePath)
	}

	return fixed, changedPaths
}

// isPascalCase returns true if the string starts with an uppercase letter (component naming convention).
func isPascalCase(s string) bool {
	if len(s) == 0 {
		return false
	}
	return s[0] >= 'A' && s[0] <= 'Z'
}

// appFixTools defines the tools available to the fix LLM for on-demand file access.



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

// guessFileType returns the likely file path (with extension) and the file type string
// for a given import path (which may omit the extension).
func guessFileType(importPath string) (filePath string, fileType string) {
	if strings.HasSuffix(importPath, ".ts") || strings.HasSuffix(importPath, ".tsx") ||
		strings.HasSuffix(importPath, ".css") || strings.HasSuffix(importPath, ".json") ||
		strings.HasSuffix(importPath, ".mjs") {
		filePath = importPath
	} else {
		filePath = importPath + ".tsx"
	}
	switch {
	case strings.HasSuffix(filePath, ".tsx"):
		fileType = "tsx"
	case strings.HasSuffix(filePath, ".ts"):
		fileType = "ts"
	case strings.HasSuffix(filePath, ".css"):
		fileType = "css"
	case strings.HasSuffix(filePath, ".json"):
		fileType = "json"
	case strings.HasSuffix(filePath, ".mjs"):
		fileType = "mjs"
	default:
		fileType = "tsx"
	}
	return
}

