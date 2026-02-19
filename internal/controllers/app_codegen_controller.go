package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
	"github.com/posthog/posthog-go"
)

// --- Clarification types ---

// ClarificationQuestion is a single question the LLM generates to gather context.
type ClarificationQuestion struct {
	ID       string   `json:"id"`
	Question string   `json:"question"`
	Type     string   `json:"type"`     // "text" or "choice"
	Options  []string `json:"options"`  // non-empty when Type == "choice"
	Required bool     `json:"required"`
}

// ClarificationResponse is the LLM output containing generated questions.
type ClarificationResponse struct {
	Questions []ClarificationQuestion `json:"questions"`
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
	ColorScheme     map[string]any     `json:"colorScheme"`
	DarkColorScheme map[string]any     `json:"darkColorScheme,omitempty"`
	Typography      map[string]any     `json:"typography,omitempty"`
	Files           []appFilePlanEntry `json:"files"`
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

// llmFixResponse is the expected JSON from the fix LLM pass
type llmFixResponse struct {
	Fixes []struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	} `json:"fixes"`
}

// --- Regex for env var detection ---
var processEnvRegex = regexp.MustCompile(`process\.env\.([A-Z_][A-Z0-9_]+)`)

// detectEnvVarsFromCode scans all generated file contents for process.env.* references
// and returns a sorted list of unique env var keys, excluding auto-injected ones.
func detectEnvVarsFromCode(files map[string]string) []string {
	excluded := map[string]bool{
		"NODE_ENV":                  true,
		"NEXT_PUBLIC_STORE_ID":      true,
		"NEXT_PUBLIC_WEMOB_API_URL": true,
	}
	seen := map[string]bool{}
	for _, content := range files {
		for _, m := range processEnvRegex.FindAllStringSubmatch(content, -1) {
			if !excluded[m[1]] {
				seen[m[1]] = true
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

// computeMissingEnvVars merges LLM-declared env vars with code-scanned keys,
// then returns only those not yet configured in the project.
func (c *Controller) computeMissingEnvVars(project *models.AppProject, llmVars []RequiredEnvVar, codeKeys []string) []RequiredEnvVar {
	existing := c.CollectAllEnvVars(project)

	required := map[string]RequiredEnvVar{}
	for _, v := range llmVars {
		required[v.Key] = v
	}
	for _, key := range codeKeys {
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
		if existing[v.Key] == "" {
			missing = append(missing, v)
		}
	}
	sort.Slice(missing, func(i, j int) bool { return missing[i].Key < missing[j].Key })
	return missing
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
var namedExportIdentRegex = regexp.MustCompile(`(?m)^export\s+(?:function|const|class|type|interface|enum)\s+(\w+)`)
var singleNamedImportRegex = regexp.MustCompile(`import\s+\{\s*(\w+)\s*\}\s+from\s+['"]@/([^'"]+)['"]`)

// namedValueExportPattern matches only runtime named exports (excludes type/interface)
var namedValueExportPattern = regexp.MustCompile(`(?m)^export\s+(function|const|class|enum)\s+`)
var namedValueExportIdentRegex = regexp.MustCompile(`(?m)^export\s+(?:function|const|class|enum)\s+(\w+)`)

// exportDefaultFuncNameRegex extracts function name from "export default function Foo"
var exportDefaultFuncNameRegex = regexp.MustCompile(`(?m)^export\s+default\s+function\s+(\w+)`)

// multiNamedImportRegex matches: import { A, B } from '@/path' (2+ names)
var multiNamedImportRegex = regexp.MustCompile(`import\s+\{\s*(\w+(?:\s*,\s*\w+)+)\s*\}\s+from\s+['"]@/([^'"]+)['"]`)

// reExportBraces matches barrel-export patterns: export { Foo }, export { Foo } from '...', export type { Foo }
var reExportBraces = regexp.MustCompile(`(?m)^export\s+(?:type\s+)?\{([^}]+)\}`)

// fallbackSentinel is a machine-readable marker injected into fallback content.
// Used instead of human-readable text to avoid false positives when LLM-generated
// code legitimately contains phrases like "Generation failed" in UI strings.
const fallbackSentinel = "/* __WEMOB_FALLBACK__ */"

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

// buildGlobalsCSSTheme generates the globals.css content with Tailwind v4 @theme colors,
// body base styles, dark mode overrides, and mobile utilities when isMobile is true.
func buildGlobalsCSSTheme(colorScheme, darkColorScheme map[string]any, isMobile bool) string {
	var sb strings.Builder

	// --- @import + @theme ---
	sb.WriteString(fmt.Sprintf(`@import "tailwindcss";

@theme {
  --color-primary: %s;
  --color-secondary: %s;
  --color-accent: %s;
  --color-background: %s;
  --color-surface: %s;
  --color-text: %s;
}
`,
		getColor(colorScheme, "primary", "#3B82F6"),
		getColor(colorScheme, "secondary", "#6366F1"),
		getColor(colorScheme, "accent", "#F59E0B"),
		getColor(colorScheme, "background", "#FFFFFF"),
		getColor(colorScheme, "surface", "#F3F4F6"),
		getColor(colorScheme, "text", "#111827"),
	))

	// --- @layer base: html + body ---
	sb.WriteString(`
@layer base {
  html {
    scroll-behavior: smooth;
    -webkit-text-size-adjust: 100%;
  }
  body {
    background-color: var(--color-background);
    color: var(--color-text);
    font-synthesis: none;
    -webkit-font-smoothing: antialiased;
    -moz-osx-font-smoothing: grayscale;
  }
`)

	// Mobile-specific base styles
	if isMobile {
		sb.WriteString(`  html {
    overscroll-behavior: none;
  }
  body {
    padding-top: env(safe-area-inset-top);
    padding-bottom: env(safe-area-inset-bottom);
    overscroll-behavior-y: contain;
    -webkit-tap-highlight-color: transparent;
    min-height: 100dvh;
  }
`)
	}

	sb.WriteString("}\n")

	// --- Dark mode: override theme colors via prefers-color-scheme ---
	if len(darkColorScheme) > 0 {
		sb.WriteString(fmt.Sprintf(`
@media (prefers-color-scheme: dark) {
  :root {
    --color-primary: %s;
    --color-secondary: %s;
    --color-accent: %s;
    --color-background: %s;
    --color-surface: %s;
    --color-text: %s;
  }
}
`,
			getColor(darkColorScheme, "primary", getColor(colorScheme, "primary", "#3B82F6")),
			getColor(darkColorScheme, "secondary", getColor(colorScheme, "secondary", "#6366F1")),
			getColor(darkColorScheme, "accent", getColor(colorScheme, "accent", "#F59E0B")),
			getColor(darkColorScheme, "background", getColor(colorScheme, "background", "#111827")),
			getColor(darkColorScheme, "surface", getColor(colorScheme, "surface", "#1F2937")),
			getColor(darkColorScheme, "text", getColor(colorScheme, "text", "#F9FAFB")),
		))
	}

	return sb.String()
}

// getTemplateContent returns the hardcoded content for a template file
func getTemplateContent(path string, colorScheme, darkColorScheme map[string]any, projectType string) string {
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
		return buildGlobalsCSSTheme(colorScheme, darkColorScheme, projectType == "mobile_app")
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
// Returns tiers 1-5 as separate slices:
//   - Tier 1: lib/, utils/, types/, data/, hooks/, contexts/, store/
//   - Tier 2: components/ui/ (UI primitives — generated first)
//   - Tier 3: components/* (non-ui components — can read ui/ files)
//   - Tier 4: app pages and API routes
//   - Tier 5: layout + globals.css
func categorizeFiles(files []appFilePlanEntry) [][]appFilePlanEntry {
	tiers := make([][]appFilePlanEntry, 5)

	for _, f := range files {
		switch {
		// Tier 5: root layout and globals.css
		case f.Path == "app/layout.tsx" || f.Path == "app/globals.css":
			tiers[4] = append(tiers[4], f)
		// Tier 1: lib, utils, types, data, hooks, contexts, stores
		case strings.HasPrefix(f.Path, "lib/") ||
			strings.HasPrefix(f.Path, "utils/") ||
			strings.HasPrefix(f.Path, "types/") ||
			strings.HasPrefix(f.Path, "data/") ||
			strings.HasPrefix(f.Path, "hooks/") ||
			strings.HasPrefix(f.Path, "contexts/") ||
			strings.HasPrefix(f.Path, "store/"):
			tiers[0] = append(tiers[0], f)
		// Tier 2: components/ui/ (UI primitives)
		case strings.HasPrefix(f.Path, "components/ui/"):
			tiers[1] = append(tiers[1], f)
		// Tier 3: components/* (non-ui — can reference ui/ files)
		case strings.HasPrefix(f.Path, "components/"):
			tiers[2] = append(tiers[2], f)
		// Tier 4: pages (app/**/page.tsx) and API routes
		default:
			tiers[3] = append(tiers[3], f)
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

// buildExportManifest scans already-generated .ts/.tsx files and produces a compact
// text listing of each file's exports, e.g.:
//
//	components/ui/button.tsx: export { Button }
//	lib/utils.ts: export { cn }
//	components/hero.tsx: export default Hero
func buildExportManifest(files map[string]string) string {
	paths := make([]string, 0, len(files))
	for p := range files {
		if strings.HasSuffix(p, ".ts") || strings.HasSuffix(p, ".tsx") {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)

	var sb strings.Builder
	for _, p := range paths {
		content := files[p]
		var exports []string

		// Default export function name
		if m := exportDefaultFuncNameRegex.FindStringSubmatch(content); m != nil {
			exports = append(exports, "default "+m[1])
		} else if exportDefaultPattern.MatchString(content) {
			exports = append(exports, "default")
		}

		// Named value exports (function, const, class, enum)
		for _, m := range namedValueExportIdentRegex.FindAllStringSubmatch(content, -1) {
			exports = append(exports, m[1])
		}

		// Named type exports (type, interface) — useful for TS imports
		for _, m := range namedExportIdentRegex.FindAllStringSubmatch(content, -1) {
			// Skip if already captured as a value export
			found := false
			for _, e := range exports {
				if e == m[1] {
					found = true
					break
				}
			}
			if !found {
				exports = append(exports, m[1])
			}
		}

		// Barrel exports: export { Foo, Bar }, export { Foo } from '...', export type { Foo }
		for _, m := range reExportBraces.FindAllStringSubmatch(content, -1) {
			names := strings.Split(m[1], ",")
			for _, raw := range names {
				name := strings.TrimSpace(raw)
				// Handle "default as Foo" → capture "Foo"
				if strings.HasPrefix(name, "default as ") {
					name = strings.TrimPrefix(name, "default as ")
					name = strings.TrimSpace(name)
				}
				// Handle "type Foo" → capture "Foo"
				if strings.HasPrefix(name, "type ") {
					name = strings.TrimPrefix(name, "type ")
					name = strings.TrimSpace(name)
				}
				// Handle "Foo as Bar" → capture "Bar" (the exported name)
				if idx := strings.Index(name, " as "); idx >= 0 {
					name = strings.TrimSpace(name[idx+4:])
				}
				if name == "" {
					continue
				}
				// Skip if already captured
				already := false
				for _, e := range exports {
					if e == name {
						already = true
						break
					}
				}
				if !already {
					exports = append(exports, name)
				}
			}
		}

		if len(exports) > 0 {
			sb.WriteString(fmt.Sprintf("%s: export { %s }\n", p, strings.Join(exports, ", ")))
		}
	}
	return sb.String()
}

// autoFixImportMismatches performs deterministic fixes for import/export style mismatches.
// Case 1: default import from a named-export-only file → rewrite to named import.
// Case 2: single named import from a default-export-only file → rewrite to default import.
// Returns the number of fixed files and the list of changed file paths.
func autoFixImportMismatches(files map[string]string) (int, []string) {
	var changedPaths []string

	for filePath, content := range files {
		if isTemplateFile(filePath) || isConfigFile(filePath) {
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

// isPascalCase returns true if the string starts with an uppercase letter (component naming convention).
func isPascalCase(s string) bool {
	if len(s) == 0 {
		return false
	}
	return s[0] >= 'A' && s[0] <= 'Z'
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
	model := c.SecretManager.GetSecret("WEBSITE_BUILDER_LLM_MODEL")
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
		nil,
		executor,
		nil,
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
			fixes[fix.Path] = utils.StripCodeFences(fix.Content)
		}
	}

	return fixes
}

// validateAndFixFiles runs deterministic checks and LLM fix loop. Max 2 iterations.
// Returns total number of fixed files and the list of changed file paths.
func (c *Controller) validateAndFixFiles(ctx context.Context, project *models.AppProject, files map[string]string, structure *appFileStructure, sendEvent func(string, any) bool) (int, []string) {
	totalFixed := 0
	allChangedPaths := make(map[string]bool)

	// Phase 0: deterministic auto-fix for import/export mismatches (before LLM pass)
	autoFixed, changedPaths := autoFixImportMismatches(files)
	if autoFixed > 0 {
		for _, path := range changedPaths {
			if dbFile, err := c.DB.GetAppGeneratedFileByPath(project.ID.String(), "nextjs", path); err == nil && dbFile != nil {
				dbFile.Content = files[path]
				dbFile.UpdatedAt = time.Now()
				c.DB.UpdateAppGeneratedFile(dbFile)
			}
			allChangedPaths[path] = true
		}
		totalFixed += autoFixed
		slog.Info("Auto-fixed import/export mismatches", "project_id", project.ID, "files_fixed", autoFixed)
	}

	collectChanged := func() []string {
		result := make([]string, 0, len(allChangedPaths))
		for p := range allChangedPaths {
			result = append(result, p)
		}
		sort.Strings(result)
		return result
	}

	for iteration := 1; iteration <= 2; iteration++ {
		if ctx.Err() != nil {
			return totalFixed, collectChanged()
		}

		valErrors := runDeterministicChecks(files)
		if len(valErrors) == 0 {
			slog.Info("Validation passed with no errors", "project_id", project.ID, "iteration", iteration)
			sendEvent("validation", responses.ValidationEvent{
				Iteration: iteration,
				Errors:    []string{},
				Fixing:    false,
			})
			return totalFixed, collectChanged()
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
			// Only break if this is the last iteration or no errors changed
			if iteration >= 2 {
				break
			}
			// Continue to next iteration — LLM might succeed on retry with different context
			continue
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
			allChangedPaths[path] = true
			totalFixed++
		}

		slog.Info("Applied LLM fixes", "project_id", project.ID, "iteration", iteration, "files_fixed", len(fixes))

		// Post-LLM auto-fix: LLM fixes may introduce new import/export mismatches
		postAutoFixed, postChangedPaths := autoFixImportMismatches(files)
		if postAutoFixed > 0 {
			for _, path := range postChangedPaths {
				if dbFile, err := c.DB.GetAppGeneratedFileByPath(project.ID.String(), "nextjs", path); err == nil && dbFile != nil {
					dbFile.Content = files[path]
					dbFile.UpdatedAt = time.Now()
					c.DB.UpdateAppGeneratedFile(dbFile)
				}
				allChangedPaths[path] = true
			}
			totalFixed += postAutoFixed
			slog.Info("Post-LLM auto-fixed import/export mismatches", "project_id", project.ID, "iteration", iteration, "files_fixed", postAutoFixed)
		}
	}

	return totalFixed, collectChanged()
}

// GenerateClarificationQuestions calls the LLM to generate context-specific questions.
func (c *Controller) GenerateClarificationQuestions(ctx context.Context, prompt string, projectType string) ([]ClarificationQuestion, error) {
	provider := c.llmProvider()
	model := c.SecretManager.GetSecret("WEBSITE_BUILDER_LLM_MODEL")
	if model == "" {
		model = constants.DEFAULT_NEXTJS_LLM_MODEL
	}

	userMsg := fmt.Sprintf("User prompt: %s\nProject type: %s", prompt, projectType)
	responseJSON, err := utils.GetJSONFromLLM(ctx, model, constants.NEXTJS_CLARIFICATION_PROMPT, userMsg, provider)
	if err != nil {
		return nil, fmt.Errorf("failed to generate clarification questions: %w", err)
	}

	var resp ClarificationResponse
	if err := json.Unmarshal([]byte(responseJSON), &resp); err != nil {
		cleaned := utils.ExtractJSON(responseJSON)
		if err2 := json.Unmarshal([]byte(cleaned), &resp); err2 != nil {
			return nil, fmt.Errorf("failed to parse clarification questions: %w", err2)
		}
	}

	if len(resp.Questions) == 0 {
		return nil, fmt.Errorf("LLM returned no clarification questions")
	}

	return resp.Questions, nil
}

// waitForClarification emits a clarification_needed SSE event, sets job status to awaiting_input,
// and blocks until the user responds or the timeout expires.
// Returns the enriched prompt (original prompt + Q&A context) or the original prompt on timeout/skip.
func (c *Controller) waitForClarification(ctx context.Context, jobID string, prompt string, questions []ClarificationQuestion, sendEvent func(string, any) bool) string {
	ch := make(chan ClarificationResult, 1)
	c.PendingClarifications.Store(jobID, ch)
	defer c.PendingClarifications.Delete(jobID)

	// Set job status to awaiting_input
	c.DB.UpdateGenerationJobStatus(jobID, "awaiting_input", "")

	// Emit SSE event
	if !sendEvent("clarification_needed", responses.ClarificationNeededEvent{
		JobID:          jobID,
		Questions:      questions,
		TimeoutSeconds: 90,
	}) {
		return prompt
	}

	// Block with 90-second timeout
	select {
	case <-ctx.Done():
		return prompt
	case result := <-ch:
		if result.Skip || len(result.Answers) == 0 {
			slog.Info("Clarification skipped", "job_id", jobID)
			// Restore job status to running
			c.DB.UpdateGenerationJobStatus(jobID, "running", "")
			return prompt
		}
		slog.Info("Clarification answers received", "job_id", jobID, "answers", len(result.Answers))

		// Emit received event
		sendEvent("clarification_received", responses.ClarificationReceivedEvent{
			Message: "Answers received, starting generation...",
		})

		// Restore job status to running
		c.DB.UpdateGenerationJobStatus(jobID, "running", "")

		return buildEnrichedPrompt(prompt, questions, result.Answers)
	case <-time.After(90 * time.Second):
		slog.Info("Clarification timed out, proceeding with original prompt", "job_id", jobID)
		// Restore job status to running
		c.DB.UpdateGenerationJobStatus(jobID, "running", "")
		return prompt
	}
}

// buildEnrichedPrompt combines the original prompt with clarification Q&A.
func buildEnrichedPrompt(original string, questions []ClarificationQuestion, answers []ClarificationAnswer) string {
	answerMap := make(map[string]string, len(answers))
	for _, a := range answers {
		answerMap[a.ID] = a.Answer
	}

	var sb strings.Builder
	sb.WriteString(original)
	sb.WriteString("\n\n### Additional Context from User\n")
	for _, q := range questions {
		if answer, ok := answerMap[q.ID]; ok && answer != "" {
			sb.WriteString(fmt.Sprintf("Q: %s\nA: %s\n\n", q.Question, answer))
		}
	}
	return sb.String()
}

// PlanAppFileStructure calls LLM to generate a file plan for the app
func (c *Controller) PlanAppFileStructure(ctx context.Context, prompt string, projectType string) (*appFileStructure, error) {
	provider := c.llmProvider()
	model := c.SecretManager.GetSecret("WEBSITE_BUILDER_LLM_MODEL")
	if model == "" {
		model = constants.DEFAULT_NEXTJS_LLM_MODEL
	}

	systemPrompt := constants.NEXTJS_STRUCTURE_PROMPT
	if projectType == "ecommerce" {
		systemPrompt = constants.NEXTJS_ECOMMERCE_STRUCTURE_PROMPT
	} else if projectType == "mobile_app" {
		systemPrompt = constants.NEXTJS_MOBILE_STRUCTURE_PROMPT
	}

	structureJSON, err := utils.GetJSONFromLLM(ctx, model, systemPrompt, prompt, provider)
	if err != nil {
		slog.Error("failed to generate app structure", "error", err)
		return nil, fmt.Errorf("failed to generate app structure. try again please")
	}

	var structure appFileStructure
	if err := json.Unmarshal([]byte(structureJSON), &structure); err != nil {
		// LLM sometimes wraps JSON in markdown code fences — try extracting
		cleaned := utils.ExtractJSON(structureJSON)
		if err2 := json.Unmarshal([]byte(cleaned), &structure); err2 != nil {
			slog.Error("failed to parse app structure", "error", err2, "json", structureJSON)
			return nil, fmt.Errorf("failed to parse app structure. try again please")
		}
	}

	if len(structure.Files) == 0 {
		return nil, fmt.Errorf("failed to generate app files. try again please")
	}

	return &structure, nil
}

// PlanAppFileStructureStream is the streaming variant of PlanAppFileStructure.
// It sends planning chunks via onChunk as the LLM generates the structure.
func (c *Controller) PlanAppFileStructureStream(ctx context.Context, prompt string, projectType string, onChunk func(chunk string), onReasoning func(chunk string)) (*appFileStructure, error) {
	provider := c.llmProvider()
	model := c.SecretManager.GetSecret("WEBSITE_BUILDER_LLM_MODEL")
	if model == "" {
		model = constants.DEFAULT_NEXTJS_LLM_MODEL
	}

	systemPrompt := constants.NEXTJS_STRUCTURE_PROMPT
	if projectType == "ecommerce" {
		systemPrompt = constants.NEXTJS_ECOMMERCE_STRUCTURE_PROMPT
	} else if projectType == "mobile_app" {
		systemPrompt = constants.NEXTJS_MOBILE_STRUCTURE_PROMPT
	}

	structureJSON, err := utils.StreamJSONFromLLM(ctx, model, systemPrompt, prompt, provider, onChunk, onReasoning)
	if err != nil {
		slog.Error("failed to generate app structure", "error", err)
		return nil, fmt.Errorf("failed to generate app structure. try again please")
	}

	var structure appFileStructure
	if err := json.Unmarshal([]byte(structureJSON), &structure); err != nil {
		// LLM sometimes wraps JSON in markdown code fences — try extracting
		cleaned := utils.ExtractJSON(structureJSON)
		if err2 := json.Unmarshal([]byte(cleaned), &structure); err2 != nil {
			slog.Error("failed to parse app structure", "error", err2, "json", structureJSON)
			return nil, fmt.Errorf("failed to parse app structure. try again please")
		}
	}

	if len(structure.Files) == 0 {
		return nil, fmt.Errorf("failed to generate app files. try again please")
	}

	return &structure, nil
}

// PlanAppFileStructureFromScreenshot uses vision LLM to plan files from a screenshot
func (c *Controller) PlanAppFileStructureFromScreenshot(ctx context.Context, imageBase64 string) (*appFileStructure, error) {
	provider := c.llmProvider()
	model := c.SecretManager.GetSecret("WEBSITE_BUILDER_LLM_MODEL")
	if model == "" {
		model = constants.DEFAULT_NEXTJS_LLM_MODEL
	}

	response, err := utils.GetJSONFromLLMWithVision(ctx, model, constants.NEXTJS_SCREENSHOT_TO_FILES_PROMPT, "Analyze this screenshot and generate the file structure plan.", imageBase64, provider)
	if err != nil {
		return nil, fmt.Errorf("failed to analyze screenshot")
	}

	var structure appFileStructure
	if err := json.Unmarshal([]byte(response), &structure); err != nil {
		// LLM sometimes wraps JSON in markdown code fences — try extracting
		cleaned := utils.ExtractJSON(response)
		if err2 := json.Unmarshal([]byte(cleaned), &structure); err2 != nil {
			slog.Error("failed to parse screenshot structure", "error", err2, "json", response)
			return nil, fmt.Errorf("failed to parse generated structure")
		}
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

// getAppFileFallback returns fallback content if LLM generation fails for a file.
// Each fallback is prefixed with fallbackSentinel so findBrokenFiles can detect it
// without false positives from LLM-generated code that contains "Generation failed".
func (c *Controller) getAppFileFallback(filePlan appFilePlanEntry) string {
	switch filePlan.Type {
	case "tsx":
		return fallbackSentinel + fmt.Sprintf("\nexport default function Page() {\n  return <div><h1>%s</h1><p>Generation failed. Please edit this file.</p></div>\n}", filePlan.Description)
	case "css":
		return fallbackSentinel + "\n@import \"tailwindcss\";\n\n/* Generation failed. Please edit manually. */"
	case "ts":
		return fallbackSentinel + "\n// Generation failed. Please edit manually.\nexport {}"
	default:
		return ""
	}
}

// generateSingleAppFile generates a single app file with a per-file timeout.
// It reads generatedFiles under mu lock for context building, and returns the raw content.
func (c *Controller) generateSingleAppFile(
	ctx context.Context,
	project *models.AppProject,
	filePlan appFilePlanEntry,
	structure *appFileStructure,
	model string,
	provider *utils.LLMProvider,
	generatedFiles map[string]string,
	mu *sync.Mutex,
	allFilePaths []string,
	colorSchemeJSON []byte,
	storeContext string,
	sendEvent func(string, any) bool,
) (string, error) {
	fileCtx, fileCancel := context.WithTimeout(ctx, 5*time.Minute)
	defer fileCancel()

	basePrompt := c.getAppFilePrompt(filePlan.Type)

	// Build system prompt with project-level context (sent once, cached by providers)
	var systemPrompt strings.Builder
	systemPrompt.WriteString(basePrompt)
	systemPrompt.WriteString(fmt.Sprintf("\n\n### Project Context\nApp: %s\nColor scheme: %s\nTypography: %v\nAll files in project: %s\n\nUser's original description: %s",
		structure.Name,
		string(colorSchemeJSON),
		structure.Typography,
		strings.Join(allFilePaths, ", "),
		project.Prompt,
	))
	// Inline wemob-store.ts and store context in system prompt for e-commerce
	if project.ProjectType == "ecommerce" {
		mu.Lock()
		if storeContent, ok := generatedFiles["lib/wemob-store.ts"]; ok {
			systemPrompt.WriteString(fmt.Sprintf("\n\n--- lib/wemob-store.ts (INJECTED SDK — use these exports, do NOT create custom API utilities) ---\n%s", storeContent))
		}
		mu.Unlock()
		if storeContext != "" {
			systemPrompt.WriteString(fmt.Sprintf("\n\n### Store Context\n%s", storeContext))
		}
	}

	// Inject mobile design rules for mobile_app projects
	if project.ProjectType == "mobile_app" {
		systemPrompt.WriteString("\n\n" + constants.NEXTJS_MOBILE_DESIGN_RULES)
	}

	// Build compact per-file user prompt
	mu.Lock()
	var generatedFileList strings.Builder
	if len(generatedFiles) > 0 {
		generatedFileList.WriteString("\nAlready-generated files (only use read_file if you need to check a specific import or type — do NOT read all files):\n")
		genPaths := make([]string, 0, len(generatedFiles))
		for p := range generatedFiles {
			genPaths = append(genPaths, p)
		}
		sort.Strings(genPaths)
		for _, p := range genPaths {
			generatedFileList.WriteString(fmt.Sprintf("- %s\n", p))
		}
	}
	// Snapshot the file map for the tool executor
	fileSnapshot := make(map[string]string, len(generatedFiles))
	for k, v := range generatedFiles {
		fileSnapshot[k] = v
	}
	mu.Unlock()

	// Build export manifest from already-generated files so LLM knows exact exports
	exportManifest := buildExportManifest(fileSnapshot)

	var manifestSection string
	if exportManifest != "" {
		manifestSection = "\n\nAvailable exports from already-generated files:\n" + exportManifest
	}

	userPrompt := fmt.Sprintf("Generate: %s\nDescription: %s%s%s",
		filePlan.Path,
		filePlan.Description,
		generatedFileList.String(),
		manifestSection,
	)

	codegenLocalExecutor := c.buildChatToolExecutor(nil, nil, fileSnapshot)
	codegenExecutor := c.compositeToolExecutor(codegenLocalExecutor)
	codegenTools := append([]utils.Tool{}, appCodegenTools...)

	content, err := utils.StreamTextWithTools(
		fileCtx,
		model,
		systemPrompt.String(),
		userPrompt,
		provider,
		codegenTools,
		nil,
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
		nil,
	)
	if err != nil {
		// If it was a per-file timeout (not parent ctx cancellation), use fallback
		if fileCtx.Err() != nil && ctx.Err() == nil {
			slog.Warn("Per-file timeout, using fallback", "path", filePlan.Path)
			return c.getAppFileFallback(filePlan), nil
		}
		return "", err
	}

	return content, nil
}

// repairSingleAppFile generates a replacement for a broken file using a focused repair prompt.
// It includes the failure reason and broken content as context so the LLM can produce a better result.
func (c *Controller) repairSingleAppFile(
	ctx context.Context,
	project *models.AppProject,
	filePlan appFilePlanEntry,
	structure *appFileStructure,
	model string,
	provider *utils.LLMProvider,
	generatedFiles map[string]string,
	mu *sync.Mutex,
	allFilePaths []string,
	colorSchemeJSON []byte,
	sendEvent func(string, any) bool,
	failReason string,
	brokenContent string,
) (string, error) {
	fileCtx, fileCancel := context.WithTimeout(ctx, 5*time.Minute)
	defer fileCancel()

	// Build context about files that import this broken file
	mu.Lock()
	var importContext strings.Builder
	for path, content := range generatedFiles {
		if strings.Contains(content, "@/"+strings.TrimSuffix(strings.TrimSuffix(filePlan.Path, ".tsx"), ".ts")) {
			// Extract just the import lines referencing this file
			for _, line := range strings.Split(content, "\n") {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "import") && strings.Contains(line, "@/"+strings.TrimSuffix(strings.TrimSuffix(filePlan.Path, ".tsx"), ".ts")) {
					importContext.WriteString(fmt.Sprintf("  %s (from %s)\n", trimmed, path))
				}
			}
		}
	}

	fileSnapshot := make(map[string]string, len(generatedFiles))
	for k, v := range generatedFiles {
		fileSnapshot[k] = v
	}
	mu.Unlock()

	repairPrompt := constants.NEXTJS_REPAIR_PROMPT
	// Inject mobile design rules for mobile_app projects
	if project.ProjectType == "mobile_app" {
		repairPrompt += "\n\n" + constants.NEXTJS_MOBILE_DESIGN_RULES
	}

	var userPrompt strings.Builder
	userPrompt.WriteString(fmt.Sprintf("File to repair: %s\n", filePlan.Path))
	userPrompt.WriteString(fmt.Sprintf("File type: %s\n", filePlan.Type))
	userPrompt.WriteString(fmt.Sprintf("Description: %s\n", filePlan.Description))
	userPrompt.WriteString(fmt.Sprintf("App name: %s\n", structure.Name))
	userPrompt.WriteString(fmt.Sprintf("Color scheme: %s\n", string(colorSchemeJSON)))
	if failReason != "" {
		userPrompt.WriteString(fmt.Sprintf("\nPrevious failure reason: %s\n", failReason))
	}
	if brokenContent != "" && len(brokenContent) < 500 {
		userPrompt.WriteString(fmt.Sprintf("\nCurrent broken content:\n%s\n", brokenContent))
	}
	if importContext.Len() > 0 {
		userPrompt.WriteString(fmt.Sprintf("\nFiles that import this module:\n%s", importContext.String()))
	}
	userPrompt.WriteString(fmt.Sprintf("\nUser's original description: %s\n", project.Prompt))

	codegenLocalExecutor := c.buildChatToolExecutor(nil, nil, fileSnapshot)
	codegenExecutor := c.compositeToolExecutor(codegenLocalExecutor)
	codegenTools := append([]utils.Tool{}, appCodegenTools...)

	content, err := utils.StreamTextWithTools(
		fileCtx,
		model,
		repairPrompt,
		userPrompt.String(),
		provider,
		codegenTools,
		nil,
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
		nil,
	)
	if err != nil {
		if fileCtx.Err() != nil && ctx.Err() == nil {
			slog.Warn("Repair per-file timeout, using fallback", "path", filePlan.Path)
			return c.getAppFileFallback(filePlan), nil
		}
		return "", err
	}

	return content, nil
}

// StreamGenerateAppFiles generates app files one by one with SSE streaming.
// Uses tiered generation with context accumulation and deterministic validation.
func (c *Controller) StreamGenerateAppFiles(ctx context.Context, project *models.AppProject, user *models.User, prompt string, projectType string, events chan<- responses.SSEEvent, jobID string) {
	var closeEvents sync.Once
	defer func() { closeEvents.Do(func() { close(events) }) }()

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

	defer func() {
		if r := recover(); r != nil {
			slog.Error("StreamGenerateAppFiles panicked", "error", r, "project_id", project.ID)
			sendEvent("error", responses.GenerationErrorEvent{
				Error: fmt.Sprintf("internal error: %v", r),
				Phase: "generation",
			})
		}
	}()

	// Timing and metrics variables (hoisted for defer closure access)
	genStart := time.Now()
	var planDuration, codegenDuration, repairDuration, validationDuration float64
	var generatedFiles map[string]string
	var appFiles []appFilePlanEntry
	var missingImportsCount, brokenFilesCount, fixedCount int
	var genSuccess bool

	provider := c.llmProvider()
	model := c.SecretManager.GetSecret("WEBSITE_BUILDER_LLM_MODEL")
	if model == "" {
		model = constants.DEFAULT_NEXTJS_LLM_MODEL
	}
	concurrency := c.llmConcurrency()
	slog.Info("LLM concurrency limit", "concurrency", concurrency)

	// Token usage tracking
	tracker := utils.NewTokenTracker()
	ctx = utils.WithTokenTracker(ctx, tracker)
	defer func() {
		// Close events channel FIRST so the job finalizes promptly,
		// even if tracking/credit operations below are slow.
		closeEvents.Do(func() { close(events) })

		c.RecordTokenUsage(user.ID, "generate_app", model, provider.Name, project.ID.String(), tracker)
		// Token-based credit deduction
		prompt, completion, total, callCount := tracker.Totals()
		if total > 0 {
			credits := models.TokensToCredits(total)
			if err := c.DeductAICreditsUnchecked(user, credits, "generate_app", project.ID.String()); err != nil {
				slog.Warn("failed to deduct AI credits from tokens", "error", err,
					"user_id", user.ID, "credits", credits, "tokens", total)
			}
			genDuration := time.Since(genStart).Seconds()
			c.TrackUserEvent(user, "app_generation_completed", posthog.NewProperties().
				Set("app_id", project.ID.String()).
				Set("project_type", projectType).
				Set("duration_seconds", genDuration).
				Set("total_tokens", total).
				Set("prompt_tokens", prompt).
				Set("completion_tokens", completion).
				Set("llm_call_count", callCount).
				Set("credits_used", credits).
				Set("file_count", len(generatedFiles)).
				Set("planned_file_count", len(appFiles)).
				Set("missing_imports_count", missingImportsCount).
				Set("broken_files_count", brokenFilesCount).
				Set("validation_fixes", fixedCount).
				Set("model", model).
				Set("is_ecommerce", projectType == "ecommerce").
				Set("success", genSuccess).
				Set("phase_plan_seconds", planDuration).
				Set("phase_codegen_seconds", codegenDuration).
				Set("phase_repair_seconds", repairDuration).
				Set("phase_validation_seconds", validationDuration))
		}
	}()

	// Start SSE keepalive goroutine to prevent proxy/firewall timeouts
	keepaliveDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-keepaliveDone:
				return
			case <-ticker.C:
				sendEvent("keepalive", map[string]any{"ts": time.Now().Unix()})
			}
		}
	}()
	defer close(keepaliveDone)

	// Step 0: Clarification — ask the user context-specific questions before generating
	if jobID != "" {
		clarifyCtx, clarifyCancel := context.WithTimeout(ctx, 30*time.Second)
		questions, err := c.GenerateClarificationQuestions(clarifyCtx, prompt, projectType)
		clarifyCancel()

		if err != nil {
			slog.Warn("Skipping clarification: failed to generate questions", "error", err, "job_id", jobID)
		} else if len(questions) > 0 {
			enriched := c.waitForClarification(ctx, jobID, prompt, questions, sendEvent)
			if enriched != prompt {
				prompt = enriched
				slog.Info("Using enriched prompt after clarification", "job_id", jobID)
			}
		}

		if ctx.Err() != nil {
			return
		}
	}

	// Step 1: Plan file structure (streamed)
	structure, err := c.PlanAppFileStructureStream(ctx, prompt, projectType, func(chunk string) {
		sendEvent("planning_chunk", responses.PlanningChunkEvent{
			Chunk: chunk,
			Phase: "structure",
		})
	}, func(chunk string) {
		sendEvent("chat_thinking", responses.ChatThinkingEvent{Chunk: chunk})
	})
	if err != nil {
		sendError("structure", err.Error())
		return
	}
	planDuration = time.Since(genStart).Seconds()

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
	for _, f := range structure.Files {
		if !isTemplateFile(f.Path) {
			appFiles = append(appFiles, f)
		}
	}

	// Step 3b: Auto-detect ecommerce from planned files if not already set
	if projectType != "ecommerce" {
		for _, f := range appFiles {
			if strings.HasPrefix(f.Path, "app/cart/") ||
				strings.HasPrefix(f.Path, "app/shop/") ||
				strings.HasPrefix(f.Path, "app/product/") ||
				strings.HasPrefix(f.Path, "components/cart") ||
				strings.HasPrefix(f.Path, "components/product") {
				projectType = "ecommerce"
				project.ProjectType = "ecommerce"
				project, _ = c.DB.UpdateAppProject(project)
				slog.Info("Auto-detected ecommerce project from file plan", "project_id", project.ID)

				// Auto-create store if it doesn't exist yet (user typed a free-form prompt
				// instead of using the explicit ecommerce endpoint)
				existingStore, _ := c.DB.GetStoreByAppProjectID(project.ID.String())
				if existingStore == nil || existingStore.ID.String() == "00000000-0000-0000-0000-000000000000" {
					if _, err := c.CreateStore(project, user, project.Name+" Store"); err != nil {
						slog.Error("Failed to auto-create store for detected ecommerce project", "error", err, "project_id", project.ID)
					} else {
						slog.Info("Auto-created store for detected ecommerce project", "project_id", project.ID)
					}
				}
				break
			}
		}
	}

	// Step 3c: When ecommerce is detected (explicitly or auto), strip LLM-planned lib files
	// that duplicate wemob-store.ts functionality. The template SDK covers cart, products,
	// store config, and API calls — generating custom versions causes the SDK to be ignored.
	if projectType == "ecommerce" {
		ecommerceDuplicateLibFiles := map[string]bool{
			"lib/products.ts":     true,
			"lib/cart.ts":         true,
			"lib/cart.tsx":        true,
			"lib/cart-context.ts": true,
			"lib/cart-context.tsx": true,
			"lib/store.ts":       true,
			"lib/store-api.ts":   true,
			"lib/api.ts":         true,
			"lib/types.ts":       true,
		}
		filtered := appFiles[:0]
		for _, f := range appFiles {
			if ecommerceDuplicateLibFiles[f.Path] {
				slog.Info("Stripping duplicate ecommerce lib file from plan (covered by wemob-store.ts)", "path", f.Path, "project_id", project.ID)
				continue
			}
			filtered = append(filtered, f)
		}
		appFiles = filtered
	}

	// Build total file list (template + app files)
	templatePaths := constants.NEXTJS_TEMPLATE_FILE_PATHS
	allFilePaths := make([]string, 0, len(templatePaths)+len(appFiles)+len(constants.NEXTJS_ECOMMERCE_TEMPLATE_FILE_PATHS))
	allFilePaths = append(allFilePaths, templatePaths...)
	if projectType == "ecommerce" {
		allFilePaths = append(allFilePaths, constants.NEXTJS_ECOMMERCE_TEMPLATE_FILE_PATHS...)
		allFilePaths = append(allFilePaths, ".env.local")
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
	generatedFiles = make(map[string]string) // accumulated content for context
	for _, tp := range templatePaths {
		content := getTemplateContent(tp, structure.ColorScheme, structure.DarkColorScheme, projectType)
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
			createdFile, err := c.DB.UpsertAppGeneratedFile(&file)
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

	// Step 6c: Generate .env.local for ecommerce projects so NEXT_PUBLIC_* vars are available
	if projectType == "ecommerce" {
		envVars := c.CollectAllEnvVars(project)
		if dotEnv := BuildDotEnvLocal(envVars); dotEnv != "" {
			file := models.AppGeneratedFile{
				AppProjectID: project.ID,
				Platform:     "nextjs",
				FilePath:     ".env.local",
				Content:      dotEnv,
				FileType:     "env",
				IsGenerated:  false, // managed by system
			}
			createdFile, err := c.DB.UpsertAppGeneratedFile(&file)
			if err != nil {
				slog.Error("failed to save .env.local file", "error", err)
			} else {
				generatedFiles[".env.local"] = dotEnv
				sendEvent("file_complete", responses.FileCompleteEvent{
					Action:   "create",
					FilePath: ".env.local",
					FileID:   createdFile.ID,
					FileType: "env",
				})
			}
		}
	}

	// Step 7: Build e-commerce store context for per-file codegen
	var storeContext string
	if projectType == "ecommerce" {
		store, err := c.DB.GetStoreByAppProjectID(project.ID.String())
		if err == nil && store != nil {
			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("This is an e-commerce project with a REAL backend store. ALL product/category data MUST be fetched from the API — NEVER hardcode products, prices, or categories.\n"))
			sb.WriteString(fmt.Sprintf("Store: %s (ID: %s, Currency: %s, Locale: %s, Tax: %.2f%%)\n", store.StoreName, store.ID.String(), store.Currency, store.Locale, store.TaxRate))
			products, _ := c.DB.GetProductsByStoreID(store.ID.String())
			sb.WriteString(fmt.Sprintf("Products in database: %d\n", len(products)))
			categories, _ := c.DB.GetCategoriesByStoreID(store.ID.String())
			if len(categories) > 0 {
				var catNames []string
				for _, cat := range categories {
					catNames = append(catNames, cat.Name)
				}
				sb.WriteString(fmt.Sprintf("Categories: %s\n", strings.Join(catNames, ", ")))
			}
			sb.WriteString("Use import { getProducts, getProduct, getCategories, useProducts, useProduct, useCategories, useCart, useStoreConfig, CartProvider, formatPrice, getSessionId } from \"@/lib/wemob-store\";\n")
			sb.WriteString("Prices are in cents — use formatPrice(cents) for display.\n")
			storeContext = sb.String()
		}
	}

	// Categorize remaining files into tiers and generate with context accumulation
	tiers := categorizeFiles(appFiles)
	colorSchemeJSON, _ := json.Marshal(structure.ColorScheme)

	var mu sync.Mutex // protects generatedFiles map and failureReasons
	failureReasons := make(map[string]string) // tracks why each file failed (for repair context)

	for tierIdx, tier := range tiers {
		if len(tier) == 0 {
			continue
		}

		slog.Info("Generating tier", "project_id", project.ID, "tier", tierIdx+1, "files", len(tier))

		sem := make(chan struct{}, concurrency)
		var wg sync.WaitGroup

		for _, filePlan := range tier {
			if ctx.Err() != nil {
				break
			}

			wg.Add(1)
			// Acquire semaphore with context check to avoid blocking after timeout
			select {
			case sem <- struct{}{}:
				// got slot
			case <-ctx.Done():
				wg.Done()
				continue
			}
			go func(fp appFilePlanEntry) {
				defer wg.Done()
				defer func() { <-sem }() // release semaphore slot

				var content string
				var lastFailReason string
				for attempt := 0; attempt < 3; attempt++ {
					if ctx.Err() != nil {
						lastFailReason = "context cancelled"
						content = c.getAppFileFallback(fp)
						break
					}
					if attempt > 0 {
						// Respect context during retry backoff
						select {
						case <-time.After(time.Duration(attempt) * 2 * time.Second):
						case <-ctx.Done():
						}
						if ctx.Err() != nil {
							lastFailReason = "context cancelled during retry"
							content = c.getAppFileFallback(fp)
							break
						}
						slog.Info("Retrying file generation", "path", fp.Path, "attempt", attempt+1)
					}
					var err error
					content, err = c.generateSingleAppFile(ctx, project, fp, structure, model, provider, generatedFiles, &mu, allFilePaths, colorSchemeJSON, storeContext, sendEvent)
					if err != nil {
						slog.Error("failed to generate app file", "error", err, "path", fp.Path, "attempt", attempt+1)
						lastFailReason = fmt.Sprintf("error: %s", err.Error())
						content = c.getAppFileFallback(fp)
					}
					stripped := utils.StripCodeFences(content)
					if len(content)-len(stripped) > 100 {
						slog.Debug("StripCodeFences removed significant content", "path", fp.Path, "removed_chars", len(content)-len(stripped))
					}
					content = stripped
					if strings.TrimSpace(content) == "" {
						lastFailReason = "empty response from LLM"
						content = c.getAppFileFallback(fp)
					}
					if fp.Type == "tsx" || fp.Type == "ts" {
						content, _ = stripForbiddenImports(content, fp.Path)
						checkMultilineForbiddenImports(content, fp.Path)
					}
					// If we got real content (not fallback), stop retrying
					if !strings.Contains(content, fallbackSentinel) {
						lastFailReason = ""
						break
					}
					if lastFailReason == "" {
						lastFailReason = "LLM returned placeholder/fallback content"
					}
				}

				// Surface error if all retries failed and track reason for repair pass
				if strings.Contains(content, fallbackSentinel) {
					if lastFailReason == "" {
						lastFailReason = "all retries exhausted"
					}
					mu.Lock()
					failureReasons[fp.Path] = lastFailReason
					mu.Unlock()
					sendEvent("file_error", map[string]any{
						"path":    fp.Path,
						"message": fmt.Sprintf("All retries exhausted for %s — using placeholder content", fp.Path),
					})
				}

				// Save to DB
				file := models.AppGeneratedFile{
					AppProjectID: project.ID,
					Platform:     "nextjs",
					FilePath:     fp.Path,
					Content:      content,
					FileType:     fp.Type,
					IsGenerated:  true,
				}

				createdFile, dbErr := c.DB.UpsertAppGeneratedFile(&file)
				if dbErr != nil {
					slog.Error("failed to save app file", "error", dbErr, "path", fp.Path)
					return
				}

				mu.Lock()
				generatedFiles[fp.Path] = content
				mu.Unlock()

				sendEvent("file_complete", responses.FileCompleteEvent{
					Action:   "create",
					FilePath: fp.Path,
					FileID:   createdFile.ID,
					FileType: fp.Type,
				})
			}(filePlan)
		}

		wg.Wait()

		if ctx.Err() != nil {
			return
		}
	}

	// Step 8: Find and generate missing imports (parallelized)
	missingImports := findMissingImports(generatedFiles)
	missingImportsCount = len(missingImports)
	if len(missingImports) > 0 {
		slog.Info("Found missing imports, generating files", "project_id", project.ID, "missing", missingImports)

		missingSem := make(chan struct{}, concurrency)
		var missingWg sync.WaitGroup

		for _, importPath := range missingImports {
			if ctx.Err() != nil {
				break
			}

			missingWg.Add(1)
			select {
			case missingSem <- struct{}{}:
			case <-ctx.Done():
				missingWg.Done()
				continue
			}
			go func(impPath string) {
				defer missingWg.Done()
				defer func() { <-missingSem }()

				filePath, fileType := guessFileType(impPath)
				basePrompt := c.getAppFilePrompt(fileType)

				fileCtx, fileCancel := context.WithTimeout(ctx, 5*time.Minute)
				defer fileCancel()

				// Build context: actual import lines from consuming files (shows exactly what to export)
				mu.Lock()
				var importLines strings.Builder
				for path, content := range generatedFiles {
					if strings.Contains(content, "@/"+impPath) {
						for _, line := range strings.Split(content, "\n") {
							trimmed := strings.TrimSpace(line)
							if strings.HasPrefix(trimmed, "import") && strings.Contains(line, "@/"+impPath) {
								importLines.WriteString(fmt.Sprintf("  %s (from %s)\n", trimmed, path))
							}
						}
					}
				}

				fileSnapshot := make(map[string]string, len(generatedFiles))
				for k, v := range generatedFiles {
					fileSnapshot[k] = v
				}
				mu.Unlock()

				// System prompt with project context
				var sysPrompt strings.Builder
				sysPrompt.WriteString(basePrompt)
				sysPrompt.WriteString(fmt.Sprintf("\n\n### Project Context\nApp: %s\nColor scheme: %s\nUser's description: %s",
					structure.Name, string(colorSchemeJSON), project.Prompt))
				if project.ProjectType == "ecommerce" {
					if storeContent, ok := fileSnapshot["lib/wemob-store.ts"]; ok {
						sysPrompt.WriteString(fmt.Sprintf("\n\n--- lib/wemob-store.ts (INJECTED SDK) ---\n%s", storeContent))
					}
				}

				// Compact user prompt with import context
				var userPrompt strings.Builder
				userPrompt.WriteString(fmt.Sprintf("Generate: %s\n", filePath))
				if importLines.Len() > 0 {
					userPrompt.WriteString(fmt.Sprintf("\nThis file is imported with these exact import statements — match these exports:\n%s", importLines.String()))
				}
				userPrompt.WriteString("\nGenerate the complete implementation based on how it is used in consuming files.")

				missingLocalExecutor := c.buildChatToolExecutor(nil, nil, fileSnapshot)
				missingExecutor := c.compositeToolExecutor(missingLocalExecutor)
				missingTools := append([]utils.Tool{}, appCodegenTools...)
				content, err := utils.StreamTextWithTools(
					fileCtx,
					model,
					sysPrompt.String(),
					userPrompt.String(),
					provider,
					missingTools,
					nil,
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
					nil,
				)
				if err != nil {
					slog.Error("failed to generate missing import file", "error", err, "path", filePath)
					if fileType == "tsx" {
						baseName := impPath
						if idx := strings.LastIndex(impPath, "/"); idx >= 0 {
							baseName = impPath[idx+1:]
						}
						parts := strings.Split(baseName, "-")
						for i, p := range parts {
							if len(p) > 0 {
								parts[i] = strings.ToUpper(p[:1]) + p[1:]
							}
						}
						componentName := strings.Join(parts, "")
						content = fmt.Sprintf("export default function %s() {\n  return <div>%s</div>\n}", componentName, impPath)
					} else {
						content = "// Auto-generated stub\nexport {}"
					}
				}

				content = utils.StripCodeFences(content)

				file := models.AppGeneratedFile{
					AppProjectID: project.ID,
					Platform:     "nextjs",
					FilePath:     filePath,
					Content:      content,
					FileType:     fileType,
					IsGenerated:  true,
				}

				createdFile, dbErr := c.DB.UpsertAppGeneratedFile(&file)
				if dbErr != nil {
					slog.Error("failed to save missing import file", "error", dbErr, "path", filePath)
					return
				}

				mu.Lock()
				generatedFiles[filePath] = content
				mu.Unlock()

				sendEvent("file_complete", responses.FileCompleteEvent{
					Action:   "create",
					FilePath: filePath,
					FileID:   createdFile.ID,
					FileType: fileType,
				})
			}(importPath)
		}

		missingWg.Wait()
	}
	codegenDuration = time.Since(genStart).Seconds() - planDuration

	// Step 9: Repair pass — regenerate files that contain fallback sentinel placeholder content
	brokenFiles := findBrokenFiles(generatedFiles)
	brokenFilesCount = len(brokenFiles)
	if len(brokenFiles) > 0 {
		slog.Info("Found broken files, running repair pass", "project_id", project.ID, "broken_count", len(brokenFiles))
		sendEvent("repair", map[string]any{
			"phase":        "start",
			"broken_files": brokenFiles,
		})

		repairSem := make(chan struct{}, concurrency)
		var repairWg sync.WaitGroup

		for _, brokenPath := range brokenFiles {
			if ctx.Err() != nil {
				break
			}

			// Find the original file plan entry for this path
			var fp appFilePlanEntry
			found := false
			for _, f := range appFiles {
				if f.Path == brokenPath {
					fp = f
					found = true
					break
				}
			}
			if !found {
				continue
			}

			// Get failure reason for context
			mu.Lock()
			failReason := failureReasons[brokenPath]
			brokenContent := generatedFiles[brokenPath]
			mu.Unlock()

			repairWg.Add(1)
			select {
			case repairSem <- struct{}{}:
			case <-ctx.Done():
				repairWg.Done()
				continue
			}
			go func(fp appFilePlanEntry, failReason string, brokenContent string) {
				defer repairWg.Done()
				defer func() { <-repairSem }()

				content, err := c.repairSingleAppFile(ctx, project, fp, structure, model, provider, generatedFiles, &mu, allFilePaths, colorSchemeJSON, sendEvent, failReason, brokenContent)
				if err != nil {
					slog.Warn("Repair pass failed for file", "path", fp.Path, "error", err)
					return
				}
				stripped := utils.StripCodeFences(content)
				if len(content)-len(stripped) > 100 {
					slog.Debug("StripCodeFences removed significant content", "path", fp.Path, "removed_chars", len(content)-len(stripped))
				}
				content = stripped
				if fp.Type == "tsx" || fp.Type == "ts" {
					content, _ = stripForbiddenImports(content, fp.Path)
					checkMultilineForbiddenImports(content, fp.Path)
				}

				// Only apply if the repair actually produced real content
				if strings.Contains(content, fallbackSentinel) {
					slog.Warn("Repair pass still produced placeholder", "path", fp.Path)
					return
				}

				slog.Info("Repair pass fixed broken file", "path", fp.Path)

				// Update DB — only update in-memory map after successful write
				dbFile, dbErr := c.DB.GetAppGeneratedFileByPath(project.ID.String(), "nextjs", fp.Path)
				if dbErr != nil || dbFile == nil {
					slog.Error("failed to find file for repair update", "error", dbErr, "path", fp.Path)
					sendEvent("file_error", map[string]any{
						"path":    fp.Path,
						"message": fmt.Sprintf("Repair succeeded but failed to save %s to database", fp.Path),
					})
					return
				}
				dbFile.Content = content
				dbFile.UpdatedAt = time.Now()
				if _, updateErr := c.DB.UpdateAppGeneratedFile(dbFile); updateErr != nil {
					slog.Error("failed to save repaired file", "error", updateErr, "path", fp.Path)
					sendEvent("file_error", map[string]any{
						"path":    fp.Path,
						"message": fmt.Sprintf("Repair succeeded but failed to save %s to database", fp.Path),
					})
					return
				}

				mu.Lock()
				generatedFiles[fp.Path] = content
				mu.Unlock()

				sendEvent("repair", map[string]any{
					"phase": "fixed",
					"path":  fp.Path,
				})
			}(fp, failReason, brokenContent)
		}

		repairWg.Wait()

		// Report how many were fixed
		remainingBroken := findBrokenFiles(generatedFiles)
		slog.Info("Repair pass complete",
			"project_id", project.ID,
			"repaired", len(brokenFiles)-len(remainingBroken),
			"still_broken", len(remainingBroken),
		)

		// Send file_error for each file that remains broken
		for _, brokenPath := range remainingBroken {
			sendEvent("file_error", map[string]any{
				"path":    brokenPath,
				"message": fmt.Sprintf("File %s still contains placeholder content after repair", brokenPath),
			})
		}

		sendEvent("repair", map[string]any{
			"phase":             "complete",
			"repaired":          len(brokenFiles) - len(remainingBroken),
			"still_broken":      len(remainingBroken),
			"broken_file_paths": remainingBroken,
		})
	}

	// Step 10: Run deterministic validation + LLM fix loop
	repairDuration = time.Since(genStart).Seconds() - planDuration - codegenDuration
	fixedCount, _ = c.validateAndFixFiles(ctx, project, generatedFiles, structure, sendEvent)
	validationDuration = time.Since(genStart).Seconds() - planDuration - codegenDuration - repairDuration
	if fixedCount > 0 {
		slog.Info("Validation fixed files", "project_id", project.ID, "fixed_count", fixedCount)
	}

	// Step 11: Augment package.json with any discovered third-party deps
	augmented := augmentPackageJSON(generatedFiles["package.json"], generatedFiles)
	if augmented != generatedFiles["package.json"] {
		dbFile, err := c.DB.GetAppGeneratedFileByPath(project.ID.String(), "nextjs", "package.json")
		if err == nil && dbFile != nil {
			dbFile.Content = augmented
			c.DB.UpdateAppGeneratedFile(dbFile)
			slog.Info("Augmented package.json with discovered deps", "project_id", project.ID)
		}
	}

	// Step 11c: Detect required env vars
	codeKeys := detectEnvVarsFromCode(generatedFiles)
	missingVars := c.computeMissingEnvVars(project, structure.RequiredEnvVars, codeKeys)

	// Persist all required vars in project settings
	if project.Settings == nil {
		project.Settings = make(map[string]any)
	}
	if len(structure.RequiredEnvVars) > 0 {
		project.Settings["required_env_vars"] = structure.RequiredEnvVars
		c.DB.UpdateAppProject(project)
	}

	// Step 11b: Auto-seed e-commerce store with mock data
	var seedSummary string
	if projectType == "ecommerce" {
		store, err := c.DB.GetStoreByAppProjectID(project.ID.String())
		if err == nil && store != nil {
			seedCtx, seedCancel := context.WithTimeout(ctx, 2*time.Minute)
			summary, seedErr := c.SeedEcommerceData(seedCtx, store, project.Prompt, sendEvent)
			seedCancel()
			if seedErr != nil {
				slog.Warn("Failed to seed e-commerce data", "error", seedErr, "project_id", project.ID)
			} else {
				seedSummary = summary
			}
		}
	}

	// Step 12: Update conversation with assistant message
	totalFiles := len(templatePaths) + len(appFiles) + len(missingImports)
	if conv != nil {
		assistantMsg := fmt.Sprintf("Created app \"%s\" with %d files.", project.Name, totalFiles)
		if seedSummary != "" {
			assistantMsg += " " + seedSummary
		}
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

	// Step 13: Update project status
	if _, err := c.UpdateAppProjectStatus(project, "ready"); err != nil {
		slog.Error("failed to update project status to ready", "project_id", project.ID, "error", err)
	}

	// Step 14: Auto-start preview if service is configured
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

	// Step 15: Send done event
	genSuccess = true
	sendEvent("done", map[string]any{
		"app_project_id":   project.ID,
		"success":          true,
		"message":          "app generation completed",
		"total_files":      len(generatedFiles),
		"missing_env_vars": missingVars,
	})
}

// importPathRegex matches import statements like: import X from '@/components/foo' or from "@/lib/bar"
var importPathRegex = regexp.MustCompile(`from\s+['"]@/([^'"]+)['"]`)

// forbiddenImportRegex matches imports of build-time/Node.js packages in component files
var forbiddenImportRegex = regexp.MustCompile(`(?m)^import\s+.*\s+from\s+['"](tailwindcss|tailwindcss/.*|@tailwindcss/postcss|postcss|autoprefixer|eslint|webpack|jiti|fs|path|os|module|crypto|stream|child_process|net|http|https|url|util|assert|buffer|cluster|dgram|dns|events|querystring|readline|repl|string_decoder|tls|tty|v8|vm|zlib)['"].*$`)

// forbiddenFromRegex detects multiline imports by matching `from '<forbidden-pkg>'` anywhere.
// Used as a secondary warning-only check since forbiddenImportRegex is line-anchored.
var forbiddenFromRegex = regexp.MustCompile(`from\s+['"](tailwindcss|tailwindcss/[^'"]*|@tailwindcss/postcss|postcss|autoprefixer|eslint|webpack|jiti|fs|path|os|module|crypto|stream|child_process|net|http|https|url|util|assert|buffer|cluster|dgram|dns|events|querystring|readline|repl|string_decoder|tls|tty|v8|vm|zlib)['"]`)

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

// checkMultilineForbiddenImports logs a warning when multiline imports of forbidden
// packages are detected that the line-anchored forbiddenImportRegex would miss.
// Warning-only for now to measure frequency before auto-fixing.
func checkMultilineForbiddenImports(content string, filePath string) {
	if isConfigFile(filePath) {
		return
	}
	multilineMatches := forbiddenFromRegex.FindAllStringSubmatch(content, -1)
	if len(multilineMatches) == 0 {
		return
	}
	// Filter out matches already caught by the line-anchored regex
	lineMatches := forbiddenImportRegex.FindAllString(content, -1)
	for _, mm := range multilineMatches {
		alreadyCaught := false
		for _, lm := range lineMatches {
			if strings.Contains(lm, mm[1]) {
				alreadyCaught = true
				break
			}
		}
		if !alreadyCaught {
			slog.Warn("Detected multiline forbidden import (not auto-stripped)", "path", filePath, "package", mm[1])
		}
	}
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

// findBrokenFiles returns paths of files that contain the fallback sentinel marker.
func findBrokenFiles(files map[string]string) []string {
	var broken []string
	for path, content := range files {
		if strings.Contains(content, fallbackSentinel) {
			broken = append(broken, path)
		}
	}
	sort.Strings(broken)
	return broken
}

