package controllers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/utils"

	_ "github.com/lib/pq"
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

// DataModel represents a database table designed by the AI for the app.
type DataModel struct {
	Name    string       `json:"name"`
	Columns []DataColumn `json:"columns"`
}

// DataColumn represents a column in a DataModel table.
type DataColumn struct {
	Name       string `json:"name"`
	Type       string `json:"type"`                  // text, integer, boolean, timestamp, uuid, jsonb
	PrimaryKey bool   `json:"primary_key"`
	Nullable   bool   `json:"nullable"`
	Default    string `json:"default,omitempty"`
	References string `json:"references,omitempty"`  // e.g. "categories.id"
}

type appFileStructure struct {
	Name            string             `json:"name"`
	Slug            string             `json:"slug"`
	NeedsAuth       bool               `json:"needs_auth,omitempty"`
	ColorScheme     map[string]any     `json:"colorScheme"`
	DarkColorScheme map[string]any     `json:"darkColorScheme,omitempty"`
	Typography      map[string]any     `json:"typography,omitempty"`
	Files           []appFilePlanEntry `json:"files"`
	DataModels      []DataModel        `json:"data_models,omitempty"`
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
var processEnvRegex = regexp.MustCompile(`process\.env\.([A-Z_][A-Z0-9_]+)`)

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
			if !alwaysExcludedEnvVars[m[1]] {
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
		if existing[v.Key] == "" {
			missing = append(missing, v)
		}
	}
	sort.Slice(missing, func(i, j int) bool { return missing[i].Key < missing[j].Key })
	return missing
}

// nonVariableFonts maps Google Fonts that are NOT variable-width and require
// an explicit weight parameter in next/font/google. The value is the list of
// available weights. If a font is not in this map, we assume it's a variable
// font and omit the weight parameter (Next.js handles it automatically).
var nonVariableFonts = map[string][]string{
	"Bebas Neue":         {"400"},
	"Racing Sans One":    {"400"},
	"Bungee":             {"400"},
	"Bungee Shade":       {"400"},
	"Creepster":          {"400"},
	"Nosifer":            {"400"},
	"Faster One":         {"400"},
	"Monoton":            {"400"},
	"Press Start 2P":     {"400"},
	"Rubik Mono One":     {"400"},
	"Vast Shadow":        {"400"},
	"Fredericka the Great": {"400"},
	"Lobster":            {"400"},
	"Pacifico":           {"400"},
	"Righteous":          {"400"},
	"Permanent Marker":   {"400"},
	"Alfa Slab One":      {"400"},
	"Abril Fatface":      {"400"},
	"Playfair Display SC": {"400", "700", "900"},
	"Merriweather":       {"300", "400", "700", "900"},
	"Lora":               {"400", "500", "600", "700"},
	"Roboto Condensed":   {"300", "400", "700"},
	"Oswald":             {"200", "300", "400", "500", "600", "700"},
}

// fontWeightParam returns the weight option string for a font constructor.
// For non-variable fonts it returns ', weight: "400"' (or the appropriate weight).
// For variable fonts it returns an empty string.
func fontWeightParam(fontName string) string {
	if weights, ok := nonVariableFonts[fontName]; ok {
		// Use "400" if available, otherwise the first weight
		w := weights[0]
		for _, ww := range weights {
			if ww == "400" {
				w = "400"
				break
			}
		}
		return fmt.Sprintf(`, weight: "%s"`, w)
	}
	return ""
}

// --- Regex patterns for deterministic checks ---
var hookPattern = regexp.MustCompile(`\b(useState|useEffect|useRef|useCallback|useMemo|useReducer|useContext)\s*[\(<]`)
var getApiSessionArgsRegex = regexp.MustCompile(`getApiSession\s*\([^)]+\)`)
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

// jsxTagPattern detects JSX syntax in .ts files that should be .tsx.
var jsxTagPattern = regexp.MustCompile(`<[A-Za-z][A-Za-z0-9.]*(?:\s|>|/)`)

// metadataExportPattern matches "export const metadata" in layout/page files.
var metadataExportPattern = regexp.MustCompile(`(?m)^export\s+const\s+metadata\b`)

// internalHrefRegex matches href="/path" or href='/path' (skips dynamic href={...})
var internalHrefRegex = regexp.MustCompile(`href=["'](/[^"']*?)["']`)

// reExportBraces matches barrel-export patterns: export { Foo }, export { Foo } from '...', export type { Foo }
var reExportBraces = regexp.MustCompile(`(?m)^export\s+(?:type\s+)?\{([^}]+)\}`)

// fallbackSentinel is a machine-readable marker injected into fallback content.
// Used instead of human-readable text to avoid false positives when LLM-generated
// code legitimately contains phrases like "Generation failed" in UI strings.
const fallbackSentinel = "/* __WEMOB_FALLBACK__ */"

// --- Template helpers ---

// isProtectedTemplateFile returns true for template-managed files the LLM must not overwrite.
// globals.css is excluded because it is LLM-generated, not template-injected.
func isProtectedTemplateFile(path string) bool {
	if path == "app/globals.css" {
		return false
	}
	return isTemplateFile(path)
}

// looksLikeEcommerce returns true if the prompt strongly suggests an e-commerce app.
func (c *Controller) classifyProjectType(ctx context.Context, prompt string) string {
	provider := c.llmProvider()
	// Use a fast/cheap model for classification
	model := "anthropic/claude-haiku-4-5-20251001"
	if provider.IsAnthropic() {
		model = "claude-haiku-4-5-20251001"
	}

	system := `Classify the user's project description into exactly one category.
Reply with ONLY the category name, nothing else.

Categories:
- ecommerce: Online store, shop, marketplace with products, cart, checkout, payments
- app: Any other web application (blog, dashboard, portfolio, SaaS, etc.)

Examples:
- "build me an online clothing store" → ecommerce
- "kozmetik markamız için e ticaret sitesi" → ecommerce
- "create a project management dashboard" → app
- "boutique de vêtements en ligne" → ecommerce`

	result, err := utils.GetTextFromLLM(ctx, model, system, prompt, provider)
	if err != nil {
		slog.Warn("LLM classification failed, falling back to keyword detection", "error", err)
		if looksLikeEcommerce(prompt) {
			return "ecommerce"
		}
		return ""
	}
	result = strings.TrimSpace(strings.ToLower(result))
	if result == "ecommerce" || result == "e-commerce" {
		return "ecommerce"
	}
	return "app"
}

func looksLikeEcommerce(prompt string) bool {
	lower := strings.ToLower(prompt)

	// Strong signals — any one suffices
	strongSignals := []string{
		"e-commerce", "ecommerce", "online store", "online shop",
		"shopping cart", "storefront", "product catalog",
		"product listing", "product page", "checkout page",
		"add to cart", "buy now", "shopping platform",
	}
	for _, s := range strongSignals {
		if strings.Contains(lower, s) {
			return true
		}
	}

	// Combination signals — "store"/"shop" + commerce indicator
	storeWords := []string{"store", "shop", "marketplace", "boutique"}
	commerceIndicators := []string{
		"product", "cart", "payment", "price", "order",
		"checkout", "inventory", "catalog", "shipping",
		"purchase", "sell", "buy",
	}
	for _, sw := range storeWords {
		if strings.Contains(lower, sw) {
			for _, ci := range commerceIndicators {
				if strings.Contains(lower, ci) {
					return true
				}
			}
		}
	}

	return false
}

// isTemplateFile checks if a file path is managed by hardcoded templates
func isTemplateFile(path string) bool {
	for _, tp := range constants.NEXTJS_TEMPLATE_FILE_PATHS {
		if path == tp {
			return true
		}
	}
	for _, tp := range constants.NEXTJS_DB_TEMPLATE_FILE_PATHS {
		if path == tp {
			return true
		}
	}
	for _, tp := range constants.NEXTJS_ECOMMERCE_TEMPLATE_FILE_PATHS {
		if path == tp {
			return true
		}
	}
	for _, tp := range constants.NEXTJS_AUTH_TEMPLATE_FILE_PATHS {
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
// Includes semantic tokens (foreground, muted, border, input, ring) derived from
// the color scheme so that shadcn-style utility classes work (bg-foreground,
// text-muted-foreground, border-border, etc.).
func buildGlobalsCSSTheme(colorScheme, darkColorScheme map[string]any, isMobile bool) string {
	var sb strings.Builder

	fg := getColor(colorScheme, "text", "#111827")
	bg := getColor(colorScheme, "background", "#FFFFFF")
	surface := getColor(colorScheme, "surface", "#F3F4F6")
	primary := getColor(colorScheme, "primary", "#3B82F6")
	secondary := getColor(colorScheme, "secondary", "#6366F1")
	accent := getColor(colorScheme, "accent", "#F59E0B")

	// --- @import + @theme ---
	// Defines both the raw palette tokens AND the shadcn-style semantic tokens
	// (foreground, muted, border, card, destructive, *-foreground) that the
	// UI component templates (button, card, input, badge) and e-commerce
	// templates rely on.
	sb.WriteString(fmt.Sprintf(`@import "tailwindcss";

@theme {
  --color-primary: %s;
  --color-primary-foreground: %s;
  --color-secondary: %s;
  --color-secondary-foreground: %s;
  --color-accent: %s;
  --color-accent-foreground: %s;
  --color-background: %s;
  --color-surface: %s;
  --color-text: %s;
  --color-foreground: %s;
  --color-card: %s;
  --color-card-foreground: %s;
  --color-muted: %s;
  --color-muted-foreground: color-mix(in srgb, %s 60%%, %s);
  --color-border: color-mix(in srgb, %s 15%%, %s);
  --color-input: color-mix(in srgb, %s 15%%, %s);
  --color-ring: %s;
  --color-destructive: #DC2626;
  --color-destructive-foreground: #FFFFFF;
}
`,
		primary,
		bg,
		secondary,
		bg,
		accent,
		fg,
		bg,
		surface,
		fg,
		fg,
		bg,
		fg,
		surface,
		fg, bg,
		fg, bg,
		fg, bg,
		primary,
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
		darkFg := getColor(darkColorScheme, "text", "#F9FAFB")
		darkBg := getColor(darkColorScheme, "background", "#111827")
		darkSurface := getColor(darkColorScheme, "surface", "#1F2937")
		darkPrimary := getColor(darkColorScheme, "primary", primary)
		darkSecondary := getColor(darkColorScheme, "secondary", secondary)
		darkAccent := getColor(darkColorScheme, "accent", accent)
		sb.WriteString(fmt.Sprintf(`
@media (prefers-color-scheme: dark) {
  :root {
    --color-primary: %s;
    --color-primary-foreground: %s;
    --color-secondary: %s;
    --color-secondary-foreground: %s;
    --color-accent: %s;
    --color-accent-foreground: %s;
    --color-background: %s;
    --color-surface: %s;
    --color-text: %s;
    --color-foreground: %s;
    --color-card: %s;
    --color-card-foreground: %s;
    --color-muted: %s;
    --color-muted-foreground: color-mix(in srgb, %s 60%%, %s);
    --color-border: color-mix(in srgb, %s 15%%, %s);
    --color-input: color-mix(in srgb, %s 15%%, %s);
    --color-destructive: #EF4444;
    --color-destructive-foreground: #FFFFFF;
  }
}
`,
			darkPrimary,
			darkBg,
			darkSecondary,
			darkBg,
			darkAccent,
			darkFg,
			darkBg,
			darkSurface,
			darkFg,
			darkFg,
			darkBg,
			darkFg,
			darkSurface,
			darkFg, darkBg,
			darkFg, darkBg,
			darkFg, darkBg,
		))
	}

	return sb.String()
}

// buildEcommerceGlobalsCSSTheme generates the globals.css content for e-commerce projects.
// It extends the standard theme with e-commerce-specific utility classes and animations.
func buildEcommerceGlobalsCSSTheme(colorScheme, darkColorScheme map[string]any) string {
	base := buildGlobalsCSSTheme(colorScheme, darkColorScheme, false)

	primary := getColor(colorScheme, "primary", "#3B82F6")

	return base + fmt.Sprintf(`
/* E-commerce utilities */
@layer utilities {
  .gradient-text {
    background: linear-gradient(135deg, var(--color-primary), var(--color-accent));
    -webkit-background-clip: text;
    -webkit-text-fill-color: transparent;
    background-clip: text;
  }

  .glow-primary {
    box-shadow: 0 0 20px color-mix(in srgb, var(--color-primary) 30%%, transparent);
  }

  .glow-accent {
    box-shadow: 0 0 20px color-mix(in srgb, var(--color-accent) 30%%, transparent);
  }

  .glass {
    background: color-mix(in srgb, var(--color-surface) 80%%, transparent);
    backdrop-filter: blur(12px);
    -webkit-backdrop-filter: blur(12px);
  }

  .glass-dark {
    background: color-mix(in srgb, var(--color-background) 60%%, transparent);
    backdrop-filter: blur(16px);
    -webkit-backdrop-filter: blur(16px);
  }

  .grid-pattern {
    background-image: radial-gradient(circle, color-mix(in srgb, var(--color-text) 10%%, transparent) 1px, transparent 1px);
    background-size: 24px 24px;
  }
}

/* Custom scrollbar */
::-webkit-scrollbar {
  width: 6px;
  height: 6px;
}
::-webkit-scrollbar-track {
  background: transparent;
}
::-webkit-scrollbar-thumb {
  background: %s40;
  border-radius: 3px;
}
::-webkit-scrollbar-thumb:hover {
  background: %s60;
}

/* E-commerce animations */
@keyframes marquee {
  from { transform: translateX(0); }
  to { transform: translateX(-50%%); }
}
@keyframes float {
  0%%, 100%% { transform: translateY(0); }
  50%% { transform: translateY(-10px); }
}
@keyframes pulse-glow {
  0%%, 100%% { opacity: 0.6; }
  50%% { opacity: 1; }
}
@keyframes shimmer {
  0%% { background-position: -200%% 0; }
  100%% { background-position: 200%% 0; }
}

.animate-marquee { animation: marquee 30s linear infinite; }
.animate-float { animation: float 3s ease-in-out infinite; }
.animate-pulse-glow { animation: pulse-glow 2s ease-in-out infinite; }
.animate-shimmer {
  background: linear-gradient(90deg, transparent 25%%, color-mix(in srgb, var(--color-primary) 10%%, transparent) 50%%, transparent 75%%);
  background-size: 200%% 100%%;
  animation: shimmer 2s infinite;
}
`, primary, primary)
}

// buildEcommerceLayout generates the root layout.tsx for e-commerce projects.
// It includes font imports, metadata, and wraps children with Providers, SiteHeader, SiteFooter, and Toaster.
func buildEcommerceLayout(storeName string, typography map[string]any) string {
	if storeName == "" {
		storeName = "Store"
	}

	headingFont := "Inter"
	bodyFont := "Inter"
	if typography != nil {
		if v, ok := typography["heading_font"].(string); ok && v != "" {
			headingFont = v
		}
		if v, ok := typography["body_font"].(string); ok && v != "" {
			bodyFont = v
		}
	}

	// Convert font names to import and variable names
	// e.g. "Space Grotesk" -> import "Space_Grotesk", var "spaceGrotesk"
	toImportName := func(name string) string {
		return strings.ReplaceAll(name, " ", "_")
	}
	toVarName := func(name string) string {
		parts := strings.Fields(name)
		if len(parts) == 1 {
			r := []rune(parts[0])
			r[0] = []rune(strings.ToLower(string(r[0])))[0]
			return string(r)
		}
		result := strings.ToLower(parts[0])
		for _, p := range parts[1:] {
			if len(p) > 0 {
				result += strings.ToUpper(p[:1]) + p[1:]
			}
		}
		return result
	}

	headingImport := toImportName(headingFont)
	headingVar := toVarName(headingFont)
	bodyImport := toImportName(bodyFont)
	bodyVar := toVarName(bodyFont)

	sameFont := headingFont == bodyFont

	var fontImports string
	var fontConstructors string
	var bodyClassName string

	headingWeight := fontWeightParam(headingFont)
	bodyWeight := fontWeightParam(bodyFont)

	if sameFont {
		fontImports = fmt.Sprintf(`import { %s } from "next/font/google";`, headingImport)
		fontConstructors = fmt.Sprintf(`const %s = %s({ subsets: ["latin"], display: "swap"%s });`, headingVar, headingImport, headingWeight)
		bodyClassName = fmt.Sprintf("${%s.className}", headingVar)
	} else {
		fontImports = fmt.Sprintf(`import { %s, %s } from "next/font/google";`, headingImport, bodyImport)
		fontConstructors = fmt.Sprintf(`const %s = %s({ subsets: ["latin"], display: "swap", variable: "--font-heading"%s });
const %s = %s({ subsets: ["latin"], display: "swap"%s });`, headingVar, headingImport, headingWeight, bodyVar, bodyImport, bodyWeight)
		bodyClassName = fmt.Sprintf("${%s.variable} ${%s.className}", headingVar, bodyVar)
	}

	return fmt.Sprintf(`import type { Metadata } from "next";
%s
import { Toaster } from "sonner";
import { Providers } from "@/components/ecommerce/providers";
import { SiteHeader } from "@/components/ecommerce/site-header";
import { SiteFooter } from "@/components/ecommerce/site-footer";
import "./globals.css";

%s

export const metadata: Metadata = {
  title: "%s",
  description: "Welcome to %s — browse our collection and find what you love.",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" suppressHydrationWarning>
      <body className={`+"`"+`%s antialiased`+"`"+`}>
        <Providers>
          <SiteHeader />
          <main className="min-h-screen">{children}</main>
          <SiteFooter />
          <Toaster position="bottom-right" richColors />
        </Providers>
      </body>
    </html>
  );
}
`, fontImports, fontConstructors, storeName, storeName, bodyClassName)
}

// getTemplateContent returns the hardcoded content for a template file.
// dataModels is used to generate the dynamic lib/schema.ts file.
func getTemplateContent(path string, colorScheme, darkColorScheme map[string]any, projectType string, dataModels []DataModel) string {
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
	case ".gitignore":
		return constants.NEXTJS_TEMPLATE_GITIGNORE
	case "lib/db.ts":
		return constants.NEXTJS_TEMPLATE_DB_TS
	case "lib/schema.ts":
		return buildDrizzleSchema(dataModels)
	}
	return ""
}

// getEcommerceTemplateContent returns the hardcoded content for an e-commerce template file
func getEcommerceTemplateContent(path string) string {
	switch path {
	case "lib/wemob-store.ts":
		return constants.NEXTJS_TEMPLATE_WEMOB_STORE
	case "components/ecommerce/product-card.tsx":
		return constants.ECOMMERCE_TEMPLATE_PRODUCT_CARD
	case "components/ecommerce/cart-drawer.tsx":
		return constants.ECOMMERCE_TEMPLATE_CART_DRAWER
	case "components/ecommerce/price-display.tsx":
		return constants.ECOMMERCE_TEMPLATE_PRICE_DISPLAY
	case "components/ecommerce/quantity-selector.tsx":
		return constants.ECOMMERCE_TEMPLATE_QUANTITY_SELECTOR
	case "components/ecommerce/product-gallery.tsx":
		return constants.ECOMMERCE_TEMPLATE_PRODUCT_GALLERY
	case "components/ecommerce/checkout-form.tsx":
		return constants.ECOMMERCE_TEMPLATE_CHECKOUT_FORM
	case "components/ecommerce/category-nav.tsx":
		return constants.ECOMMERCE_TEMPLATE_CATEGORY_NAV
	case "components/ecommerce/cart-item-row.tsx":
		return constants.ECOMMERCE_TEMPLATE_CART_ITEM_ROW
	case "components/ecommerce/order-summary.tsx":
		return constants.ECOMMERCE_TEMPLATE_ORDER_SUMMARY
	case "components/ecommerce/empty-state.tsx":
		return constants.ECOMMERCE_TEMPLATE_EMPTY_STATE
	case "components/ecommerce/review-list.tsx":
		return constants.ECOMMERCE_TEMPLATE_REVIEW_LIST
	case "components/ecommerce/wishlist-button.tsx":
		return constants.ECOMMERCE_TEMPLATE_WISHLIST_BUTTON
	case "components/ecommerce/site-header.tsx":
		return constants.ECOMMERCE_TEMPLATE_SITE_HEADER
	case "components/ecommerce/site-footer.tsx":
		return constants.ECOMMERCE_TEMPLATE_SITE_FOOTER
	case "components/ecommerce/providers.tsx":
		return constants.ECOMMERCE_TEMPLATE_PROVIDERS
	case "components/ecommerce/newsletter-section.tsx":
		return constants.ECOMMERCE_TEMPLATE_NEWSLETTER
	case "app/cart/page.tsx":
		return constants.ECOMMERCE_TEMPLATE_CART_PAGE
	case "app/checkout/page.tsx":
		return constants.ECOMMERCE_TEMPLATE_CHECKOUT_PAGE
	case "app/checkout/success/page.tsx":
		return constants.ECOMMERCE_TEMPLATE_CHECKOUT_SUCCESS_PAGE
	case "app/checkout/cancel/page.tsx":
		return constants.ECOMMERCE_TEMPLATE_CHECKOUT_CANCEL_PAGE
	case "app/products/page.tsx":
		return constants.ECOMMERCE_TEMPLATE_PRODUCTS_PAGE
	case "app/products/[slug]/page.tsx":
		return constants.ECOMMERCE_TEMPLATE_PRODUCT_DETAIL_PAGE
	case "app/categories/[slug]/page.tsx":
		return constants.ECOMMERCE_TEMPLATE_CATEGORY_PAGE
	case "app/policies/shipping/page.tsx":
		return constants.ECOMMERCE_TEMPLATE_SHIPPING_POLICY
	case "app/policies/returns/page.tsx":
		return constants.ECOMMERCE_TEMPLATE_RETURNS_POLICY
	case "app/policies/privacy/page.tsx":
		return constants.ECOMMERCE_TEMPLATE_PRIVACY_POLICY
	case "app/policies/terms/page.tsx":
		return constants.ECOMMERCE_TEMPLATE_TERMS_POLICY
	}
	return ""
}

// getUITemplateContent returns the hardcoded content for a pre-built UI component template file.
func getUITemplateContent(path string) string {
	switch path {
	case "lib/utils.ts":
		return constants.NEXTJS_TEMPLATE_UTILS_TS
	case "components/ui/button.tsx":
		return constants.NEXTJS_TEMPLATE_BUTTON_TSX
	case "components/ui/card.tsx":
		return constants.NEXTJS_TEMPLATE_CARD_TSX
	case "components/ui/input.tsx":
		return constants.NEXTJS_TEMPLATE_INPUT_TSX
	case "components/ui/badge.tsx":
		return constants.NEXTJS_TEMPLATE_BADGE_TSX
	case "components/ui/skeleton.tsx":
		return constants.NEXTJS_TEMPLATE_SKELETON_TSX
	}
	return ""
}

// getAuthTemplateContent returns the hardcoded content for an auth template file
func getAuthTemplateContent(path string) string {
	switch path {
	case "lib/wemob-auth.ts":
		return constants.NEXTJS_TEMPLATE_WEMOB_AUTH
	case "lib/wemob-auth-client.tsx":
		return constants.NEXTJS_TEMPLATE_WEMOB_AUTH_CLIENT
	case "app/api/auth/[...action]/route.ts":
		return constants.NEXTJS_TEMPLATE_AUTH_ROUTE
	}
	return ""
}

// mergeDataModelColumns keeps all auth columns and appends non-conflicting LLM columns.
// This ensures auth-required columns (email, password_hash, etc.) always exist while
// preserving any app-specific columns the LLM designed (e.g., bestScore, avatarUrl).
func mergeDataModelColumns(auth, llm DataModel) DataModel {
	merged := DataModel{Name: auth.Name, Columns: make([]DataColumn, len(auth.Columns))}
	copy(merged.Columns, auth.Columns)

	authCols := make(map[string]bool)
	for _, col := range auth.Columns {
		authCols[col.Name] = true
	}

	for _, col := range llm.Columns {
		if !authCols[col.Name] {
			merged.Columns = append(merged.Columns, col)
		}
	}
	return merged
}

// --- Neon / DataModel helpers ---

// sqlTypeMap maps DataModel column types to PostgreSQL types.
var sqlTypeMap = map[string]string{
	"text":      "TEXT",
	"integer":   "INTEGER",
	"boolean":   "BOOLEAN",
	"timestamp": "TIMESTAMPTZ",
	"uuid":      "UUID",
	"jsonb":     "JSONB",
	"serial":    "SERIAL",
}

// buildCreateTableSQL generates CREATE TABLE statements from DataModels.
func buildCreateTableSQL(models []DataModel) string {
	var sb strings.Builder
	for _, m := range models {
		sb.WriteString(fmt.Sprintf("CREATE TABLE IF NOT EXISTS %q (\n", m.Name))
		for i, col := range m.Columns {
			pgType := sqlTypeMap[col.Type]
			if pgType == "" {
				pgType = "TEXT"
			}
			sb.WriteString(fmt.Sprintf("  %q %s", col.Name, pgType))
			if col.PrimaryKey {
				sb.WriteString(" PRIMARY KEY")
				if col.Type == "uuid" {
					sb.WriteString(" DEFAULT gen_random_uuid()")
				}
			}
			if !col.Nullable && !col.PrimaryKey {
				sb.WriteString(" NOT NULL")
			}
			if col.Default != "" && !col.PrimaryKey {
				sb.WriteString(fmt.Sprintf(" DEFAULT %s", col.Default))
			}
			if i < len(m.Columns)-1 {
				sb.WriteString(",")
			}
			sb.WriteString("\n")
		}
		sb.WriteString(");\n\n")

		// Add foreign key constraints after table creation
		for _, col := range m.Columns {
			if col.References != "" {
				parts := strings.SplitN(col.References, ".", 2)
				if len(parts) == 2 {
					sb.WriteString(fmt.Sprintf("ALTER TABLE %q ADD CONSTRAINT fk_%s_%s FOREIGN KEY (%q) REFERENCES %q(%q) ON DELETE CASCADE;\n",
						m.Name, m.Name, col.Name, col.Name, parts[0], parts[1]))
				}
			}
		}
	}
	return sb.String()
}

// drizzleTypeMap maps DataModel column types to Drizzle pgTable column functions.
var drizzleTypeMap = map[string]string{
	"text":      "text",
	"integer":   "integer",
	"boolean":   "boolean",
	"timestamp": "timestamp",
	"uuid":      "uuid",
	"jsonb":     "jsonb",
	"serial":    "serial",
}

// buildDrizzleSchema generates a lib/schema.ts file with Drizzle pgTable definitions.
func buildDrizzleSchema(dataModels []DataModel) string {
	// Scan all columns to determine which imports are needed
	needsSql := false
	for _, m := range dataModels {
		for _, col := range m.Columns {
			if col.Default != "" && !col.PrimaryKey && isRawSQLDefault(col.Default) {
				needsSql = true
			}
		}
	}

	var sb strings.Builder
	sb.WriteString("import { pgTable, text, integer, boolean, timestamp, uuid, jsonb, serial } from 'drizzle-orm/pg-core';\n")
	if needsSql {
		sb.WriteString("import { sql } from 'drizzle-orm';\n")
	}
	sb.WriteString("\n")

	for _, m := range dataModels {
		// Convert table name to camelCase for the export name
		exportName := toCamelCase(m.Name)
		sb.WriteString(fmt.Sprintf("export const %s = pgTable('%s', {\n", exportName, m.Name))
		for _, col := range m.Columns {
			drizzleType := drizzleTypeMap[col.Type]
			if drizzleType == "" {
				drizzleType = "text"
			}
			sb.WriteString(fmt.Sprintf("  %s: %s('%s')", toCamelCase(col.Name), drizzleType, col.Name))

			// Chain modifiers
			if col.PrimaryKey {
				sb.WriteString(".primaryKey()")
				if col.Type == "uuid" {
					sb.WriteString(".defaultRandom()")
				}
			}
			if !col.Nullable && !col.PrimaryKey {
				sb.WriteString(".notNull()")
			}
			if col.Default != "" && !col.PrimaryKey {
				if isRawSQLDefault(col.Default) {
					// Wrap raw SQL expressions with sql`` template literal
					sb.WriteString(fmt.Sprintf(".default(sql`%s`)", col.Default))
				} else {
					sb.WriteString(fmt.Sprintf(".default(%s)", col.Default))
				}
			}
			if col.References != "" {
				parts := strings.SplitN(col.References, ".", 2)
				if len(parts) == 2 {
					sb.WriteString(fmt.Sprintf(".references(() => %s.%s, { onDelete: 'cascade' })", toCamelCase(parts[0]), toCamelCase(parts[1])))
				}
			}
			sb.WriteString(",\n")
		}
		sb.WriteString("});\n\n")
	}

	return sb.String()
}

// isRawSQLDefault returns true if the default value looks like a raw SQL
// expression (e.g. "now()", "gen_random_uuid()") rather than a JS literal.
func isRawSQLDefault(def string) bool {
	d := strings.TrimSpace(strings.ToLower(def))
	// SQL function calls
	if strings.HasSuffix(d, "()") {
		return true
	}
	// CURRENT_TIMESTAMP and similar SQL keywords
	if strings.HasPrefix(d, "current_") {
		return true
	}
	return false
}

// neonConnStrWithEndpoint appends the endpoint ID option to a Neon connection string
// so that lib/pq (which lacks SNI support) can route to the correct Neon compute.
// e.g. postgresql://user:pass@ep-cool-name-123456.us-east-2.aws.neon.tech/neondb?sslmode=require
// → adds &options=endpoint%3Dep-cool-name-123456
func neonConnStrWithEndpoint(connStr string) string {
	u, err := url.Parse(connStr)
	if err != nil || u.Host == "" {
		return connStr
	}
	// Endpoint ID is the first part of the hostname (before the first dot)
	host := u.Hostname()
	parts := strings.SplitN(host, ".", 2)
	if len(parts) == 0 || !strings.HasPrefix(parts[0], "ep-") {
		return connStr
	}
	endpointID := parts[0]
	q := u.Query()
	// Merge with existing options if present, don't overwrite
	if existing := q.Get("options"); existing != "" {
		q.Set("options", existing+" endpoint="+endpointID)
	} else {
		q.Set("options", "endpoint="+endpointID)
	}
	u.RawQuery = q.Encode()
	return u.String()
}



// validateSeedSQL ensures LLM-generated seed SQL only contains INSERT statements.
// Blocks DROP, DELETE, UPDATE, ALTER, CREATE, TRUNCATE, and other dangerous statements.
func validateSeedSQL(sqlStr string) error {
	// Split by semicolons to get individual statements
	statements := strings.Split(sqlStr, ";")
	for _, stmt := range statements {
		trimmed := strings.TrimSpace(stmt)
		if trimmed == "" {
			continue
		}
		upper := strings.ToUpper(trimmed)
		// Each statement must start with INSERT (or a comment)
		if strings.HasPrefix(upper, "--") || strings.HasPrefix(upper, "/*") {
			continue
		}
		if !strings.HasPrefix(upper, "INSERT") {
			return fmt.Errorf("only INSERT statements are allowed in seed SQL, found: %.40s", trimmed)
		}
	}
	return nil
}

// toCamelCase converts snake_case to camelCase.
func toCamelCase(s string) string {
	parts := strings.Split(s, "_")
	for i := 1; i < len(parts); i++ {
		if len(parts[i]) > 0 {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}

// provisionNeonDatabase creates a Neon project, runs the schema SQL, and returns the connection string and project ID.
func (c *Controller) provisionNeonDatabase(ctx context.Context, project *models.AppProject, dataModels []DataModel, sendEvent func(string, any) bool) (string, string, error) {
	if c.NeonService == nil {
		return "", "", fmt.Errorf("Neon service not configured")
	}

	sendEvent("neon_provision", map[string]any{"phase": "creating_project"})

	projectName := fmt.Sprintf("wemob-%s", project.Slug)
	if len(projectName) > 60 {
		projectName = projectName[:60]
	}

	neonProjectID, connStr, err := c.NeonService.CreateProject(projectName, "")
	if err != nil {
		return "", "", fmt.Errorf("create Neon project: %w", err)
	}

	slog.Info("Neon project created", "neon_project_id", neonProjectID, "conn_str_len", len(connStr), "app_project_id", project.ID)

	// Build and execute schema SQL directly via the connection string.
	schemaSQL := buildCreateTableSQL(dataModels)
	if schemaSQL != "" {
		sendEvent("neon_provision", map[string]any{"phase": "creating_schema"})
		slog.Info("Executing schema SQL on Neon", "neon_project_id", neonProjectID, "sql_length", len(schemaSQL), "sql", schemaSQL)

		neonDB, err := sql.Open("postgres", neonConnStrWithEndpoint(connStr))
		if err != nil {
			slog.Error("Failed to open Neon database driver", "error", err, "neon_project_id", neonProjectID)
			return connStr, neonProjectID, fmt.Errorf("open Neon driver: %w", err)
		}
		defer neonDB.Close()

		// Neon compute needs time to wake up after project creation.
		// Retry ping up to 5 times with increasing delay.
		var pingErr error
		for attempt := 1; attempt <= 5; attempt++ {
			pingCtx, pingCancel := context.WithTimeout(ctx, 10*time.Second)
			pingErr = neonDB.PingContext(pingCtx)
			pingCancel()
			if pingErr == nil {
				slog.Info("Neon database ping succeeded", "attempt", attempt, "neon_project_id", neonProjectID)
				break
			}
			slog.Warn("Neon database not ready, retrying...", "attempt", attempt, "error", pingErr, "neon_project_id", neonProjectID)
			time.Sleep(time.Duration(attempt*2) * time.Second)
		}
		if pingErr != nil {
			slog.Error("Neon database not reachable after retries", "error", pingErr, "neon_project_id", neonProjectID)
			return connStr, neonProjectID, fmt.Errorf("neon database not reachable: %w", pingErr)
		}

		execCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		if _, err := neonDB.ExecContext(execCtx, schemaSQL); err != nil {
			slog.Error("Failed to execute schema SQL on Neon", "error", err, "neon_project_id", neonProjectID, "sql", schemaSQL)
			return connStr, neonProjectID, fmt.Errorf("execute schema SQL: %w", err)
		}
		slog.Info("Neon schema created successfully", "neon_project_id", neonProjectID, "tables", len(dataModels))
	}

	return connStr, neonProjectID, nil
}

// storeNeonIntegration saves the Neon connection as an AppIntegration (upsert).
func (c *Controller) storeNeonIntegration(project *models.AppProject, connStr, neonProjectID string) error {
	// Check if a Neon integration already exists — update it instead of creating a duplicate
	existing, err := c.DB.GetAppIntegrationByProvider(project.ID.String(), "neon")
	if err == nil && existing.ID.String() != "00000000-0000-0000-0000-000000000000" {
		existing.Config = map[string]any{
			"project_id": neonProjectID,
		}
		existing.Status = "connected"
		existing.EnvVars = map[string]string{
			"DATABASE_URL": connStr,
		}
		_, err := c.DB.UpdateAppIntegration(existing)
		return err
	}

	integration := &models.AppIntegration{
		AppProjectID: project.ID,
		Provider:     "neon",
		Config: map[string]any{
			"project_id": neonProjectID,
		},
		Status: "connected",
		EnvVars: map[string]string{
			"DATABASE_URL": connStr,
		},
	}

	_, err = c.DB.CreateAppIntegration(integration)
	return err
}

// seedNeonDatabase uses an LLM to generate seed SQL and executes it on the app's Neon database.
func (c *Controller) seedNeonDatabase(ctx context.Context, project *models.AppProject, dataModels []DataModel, sendEvent func(string, any) bool) (string, error) {
	// Get connection string from the stored integration
	connStr := c.neonConnString(project.ID.String())
	if connStr == "" {
		return "", fmt.Errorf("no Neon connection string found for project")
	}

	// Build schema description for the LLM
	var schemaDesc strings.Builder
	for _, m := range dataModels {
		schemaDesc.WriteString(fmt.Sprintf("Table: %s\nColumns: ", m.Name))
		for i, col := range m.Columns {
			if i > 0 {
				schemaDesc.WriteString(", ")
			}
			schemaDesc.WriteString(fmt.Sprintf("%s (%s)", col.Name, col.Type))
		}
		schemaDesc.WriteString("\n")
	}

	seedPrompt := fmt.Sprintf(`Generate realistic seed data SQL INSERT statements for a database with the following schema:

%s

The app description: %s

Rules:
1. Generate 5-15 rows of realistic data for each table.
2. Use gen_random_uuid() for UUID primary keys.
3. Respect foreign key relationships — insert parent rows before child rows.
4. Use NOW() for timestamp columns.
5. Make the data contextually relevant to the app description.
6. For JSONB columns, use valid JSON strings.
7. Return ONLY valid SQL INSERT statements, no explanations.
8. Use single-line INSERT statements (one per row).
9. Do NOT include CREATE TABLE or DROP statements.`, schemaDesc.String(), project.Prompt)

	sendEvent("neon_provision", map[string]any{"phase": "seeding_data"})

	provider := c.llmProvider()
	model := provider.DefaultModel()

	seedSQL, err := utils.GetTextFromLLM(ctx, model, "You are a SQL expert. Generate realistic seed data. Return ONLY valid SQL.", seedPrompt, provider)
	if err != nil {
		return "", fmt.Errorf("LLM seed generation: %w", err)
	}

	// Clean the SQL (strip markdown fences if present)
	seedSQL = strings.TrimSpace(seedSQL)
	seedSQL = strings.TrimPrefix(seedSQL, "```sql")
	seedSQL = strings.TrimPrefix(seedSQL, "```")
	seedSQL = strings.TrimSuffix(seedSQL, "```")
	seedSQL = strings.TrimSpace(seedSQL)

	if seedSQL != "" {
		// Validate that the seed SQL only contains INSERT statements
		if err := validateSeedSQL(seedSQL); err != nil {
			slog.Warn("LLM generated invalid seed SQL", "error", err, "project_id", project.ID)
			return "", fmt.Errorf("invalid seed SQL: %w", err)
		}

		neonDB, err := sql.Open("postgres", neonConnStrWithEndpoint(connStr))
		if err != nil {
			return "", fmt.Errorf("connect to Neon for seeding: %w", err)
		}
		defer neonDB.Close()

		execCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		if _, err := neonDB.ExecContext(execCtx, seedSQL); err != nil {
			slog.Warn("Failed to seed Neon database", "error", err, "project_id", project.ID)
			return "", fmt.Errorf("execute seed SQL: %w", err)
		}
		slog.Info("Neon database seeded", "project_id", project.ID)
	}

	return "Seeded database with sample data.", nil
}

// getNeonIntegration returns the Neon integration for a project, if any.
func (c *Controller) getNeonIntegration(projectID string) *models.AppIntegration {
	integrations, err := c.DB.GetAppIntegrationsByProjectID(projectID)
	if err != nil {
		slog.Error("getNeonIntegration: failed to query integrations", "project_id", projectID, "error", err)
		return nil
	}
	slog.Info("getNeonIntegration: queried integrations", "project_id", projectID, "count", len(integrations))
	for i, integ := range integrations {
		slog.Info("getNeonIntegration: checking integration", "provider", integ.Provider, "id", integ.ID, "status", integ.Status)
		if integ.Provider == "neon" {
			return &integrations[i]
		}
	}
	return nil
}

// PlanAppFileStructure calls the LLM to generate a file structure plan from a prompt (non-streaming).
func (c *Controller) PlanAppFileStructure(ctx context.Context, prompt string, projectType string) (*appFileStructure, error) {
	provider := c.llmProvider()
	model := c.SecretManager.GetSecret("WEBSITE_BUILDER_LLM_MODEL")
	if model == "" {
		model = provider.DefaultModel()
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

// GenerateClarificationQuestions asks a fast LLM to produce 2-5 clarifying
// questions based on the user's app prompt so the builder can gather context
// before generating code.
func (c *Controller) GenerateClarificationQuestions(ctx context.Context, prompt string, projectType string) (*ClarificationResponse, error) {
	provider := c.llmProvider()
	model := provider.FastModel()

	userMsg := fmt.Sprintf("Project type: %s\n\nUser prompt:\n%s", projectType, prompt)
	resultJSON, err := utils.GetJSONFromLLM(ctx, model, constants.APP_CLARIFICATION_PROMPT, userMsg, provider)
	if err != nil {
		slog.Warn("failed to generate clarification questions", "error", err)
		return nil, err
	}

	var resp ClarificationResponse
	if err := json.Unmarshal([]byte(resultJSON), &resp); err != nil {
		cleaned := utils.ExtractJSON(resultJSON)
		if err2 := json.Unmarshal([]byte(cleaned), &resp); err2 != nil {
			slog.Warn("failed to parse clarification questions", "error", err2)
			return nil, err2
		}
	}

	// Cap at 5 questions
	if len(resp.Questions) > 5 {
		resp.Questions = resp.Questions[:5]
	}

	return &resp, nil
}

// PlanAppFileStructureFromScreenshot uses vision LLM to plan files from a screenshot.
func (c *Controller) PlanAppFileStructureFromScreenshot(ctx context.Context, imageBase64 string) (*appFileStructure, error) {
	provider := c.llmProvider()
	model := c.SecretManager.GetSecret("WEBSITE_BUILDER_LLM_MODEL")
	if model == "" {
		model = provider.DefaultModel()
	}

	response, err := utils.GetJSONFromLLMWithVision(ctx, model, constants.NEXTJS_SCREENSHOT_TO_FILES_PROMPT, "Analyze this screenshot and generate the file structure plan.", imageBase64, provider)
	if err != nil {
		return nil, fmt.Errorf("failed to analyze screenshot")
	}

	var structure appFileStructure
	if err := json.Unmarshal([]byte(response), &structure); err != nil {
		cleaned := utils.ExtractJSON(response)
		if err2 := json.Unmarshal([]byte(cleaned), &structure); err2 != nil {
			slog.Error("failed to parse screenshot structure", "error", err2, "json", response)
			return nil, fmt.Errorf("failed to parse generated structure")
		}
	}

	return &structure, nil
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
		dmJSON, _ := json.Marshal(structure.DataModels)
		project.DataModels = dmJSON
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

// augmentPackageJSON scans generated files for third-party npm imports and adds them
// to the base template package.json if not already present
func augmentPackageJSON(baseTemplate string, files map[string]string) string {
	// Build knownPackages dynamically from the base template to stay in sync
	knownPackages := map[string]bool{
		// Next.js built-in subpaths (not real npm packages)
		"next/image": true, "next/link": true, "next/font": true,
		"next/navigation": true, "next/headers": true, "next/server": true,
		"next/font/google": true, "next/font/local": true, "next/dynamic": true,
		"react/jsx-runtime": true,
	}
	var basePkg map[string]any
	if err := json.Unmarshal([]byte(baseTemplate), &basePkg); err == nil {
		if deps, ok := basePkg["dependencies"].(map[string]any); ok {
			for pkg := range deps {
				knownPackages[pkg] = true
			}
		}
		if devDeps, ok := basePkg["devDependencies"].(map[string]any); ok {
			for pkg := range devDeps {
				knownPackages[pkg] = true
			}
		}
	}

	// Scan all files for import statements
	// Non-scoped packages: import ... from 'package-name' or 'package-name/subpath'
	importRegex := regexp.MustCompile(`(?m)^import\s+.*\s+from\s+['"]([^'"@./][^'"]*?)(?:/[^'"]*)?['"]`)
	// Scoped packages: import ... from '@scope/package' or '@scope/package/subpath'
	scopedImportRegex := regexp.MustCompile(`(?m)^import\s+.*\s+from\s+['"](@[^'"\/]+\/[^'"\/]+)`)

	foundPackages := make(map[string]bool)
	for _, content := range files {
		matches := importRegex.FindAllStringSubmatch(content, -1)
		for _, match := range matches {
			pkg := match[1]
			if !knownPackages[pkg] && !strings.HasPrefix(pkg, "@types/") {
				foundPackages[pkg] = true
			}
		}
		scopedMatches := scopedImportRegex.FindAllStringSubmatch(content, -1)
		for _, match := range scopedMatches {
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
		"@radix-ui/react-accordion":      "^1.2.0",
		"@radix-ui/react-alert-dialog":   "^1.1.0",
		"@radix-ui/react-aspect-ratio":   "^1.1.0",
		"@radix-ui/react-avatar":         "^1.1.0",
		"@radix-ui/react-checkbox":       "^1.1.0",
		"@radix-ui/react-collapsible":    "^1.1.0",
		"@radix-ui/react-context-menu":   "^2.2.0",
		"@radix-ui/react-dialog":         "^1.1.0",
		"@radix-ui/react-dropdown-menu":  "^2.1.0",
		"@radix-ui/react-hover-card":     "^1.1.0",
		"@radix-ui/react-label":          "^2.1.0",
		"@radix-ui/react-menubar":        "^1.1.0",
		"@radix-ui/react-navigation-menu": "^1.2.0",
		"@radix-ui/react-popover":        "^1.1.0",
		"@radix-ui/react-progress":       "^1.1.0",
		"@radix-ui/react-radio-group":    "^1.2.0",
		"@radix-ui/react-scroll-area":    "^1.1.0",
		"@radix-ui/react-select":         "^2.1.0",
		"@radix-ui/react-separator":      "^1.1.0",
		"@radix-ui/react-slider":         "^1.2.0",
		"@radix-ui/react-slot":           "^1.1.0",
		"@radix-ui/react-switch":         "^1.1.0",
		"@radix-ui/react-tabs":           "^1.1.0",
		"@radix-ui/react-toast":          "^1.2.0",
		"@radix-ui/react-toggle":         "^1.1.0",
		"@radix-ui/react-toggle-group":   "^1.1.0",
		"@radix-ui/react-tooltip":        "^1.1.0",
		"class-variance-authority":  "^0.7.0",
		"tailwind-merge":            "^2.2.0",
		"jose":                      "^5.9.0",
		"bcryptjs":                  "^2.4.3",
		"@types/bcryptjs":           "^2.4.6",
		// Drizzle/Neon (only added for DB projects via import scanning)
		"drizzle-orm":              "^0.39.0",
		"@neondatabase/serverless": "^0.10.0",
		"drizzle-kit":              "^0.30.0",
		// Commonly LLM-generated packages
		"@tanstack/react-query":     "^5.0.0",
		"@tanstack/react-table":     "^8.0.0",
		"sonner":                    "^1.0.0",
		"next-themes":               "^0.3.0",
		"vaul":                      "^0.9.0",
		"cmdk":                      "^0.2.0",
		"input-otp":                 "^1.0.0",
		"react-day-picker":          "^8.10.0",
		"react-resizable-panels":    "^2.0.0",
		"swr":                       "^2.2.0",
		"uuid":                      "^9.0.0",
		"lodash":                    "^4.17.0",
		"sharp":                     "^0.33.0",
		"stripe":                    "^14.0.0",
		"next-intl":                 "^3.0.0",
		"@stripe/stripe-js":         "^2.0.0",
		"@stripe/react-stripe-js":   "^2.0.0",
		"resend":                    "^3.0.0",
		"nodemailer":                "^6.9.0",
		"react-hot-toast":           "^2.4.0",
		"react-markdown":            "^9.0.0",
		"chart.js":                  "^4.4.0",
		"react-chartjs-2":           "^5.2.0",
	}

	for pkg := range foundPackages {
		if _, exists := deps[pkg]; !exists {
			if version, ok := versionMap[pkg]; ok {
				deps[pkg] = version
			} else {
				slog.Warn("Unknown package falling back to 'latest' — consider adding to versionMap", "package", pkg)
				deps[pkg] = "latest"
			}
			slog.Info("Added discovered dependency to package.json", "package", pkg)
		}
	}

	// Add @types/* packages to devDependencies when their corresponding packages are found
	if _, hasBcryptjs := deps["bcryptjs"]; hasBcryptjs {
		devDeps, ok := pkgJSON["devDependencies"].(map[string]any)
		if !ok {
			devDeps = make(map[string]any)
			pkgJSON["devDependencies"] = devDeps
		}
		if _, exists := devDeps["@types/bcryptjs"]; !exists {
			if version, ok := versionMap["@types/bcryptjs"]; ok {
				devDeps["@types/bcryptjs"] = version
			}
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
		if strings.Contains(content, `@/lib/wemob-auth-client`) {
			hasUseClient := strings.Contains(content, `"use client"`) || strings.Contains(content, `'use client'`)
			if !hasUseClient {
				errors = append(errors, validationError{
					File:  filePath,
					Error: "File imports from @/lib/wemob-auth-client but is missing \"use client\" directive. wemob-auth-client.tsx is a client module — add \"use client\" at the top of this file.",
					Type:  "missing_use_client",
				})
			}
		}

		// Check 2e: @/lib/wemob-auth (not -client) imported in "use client" file — server-only module
		if strings.Contains(content, `@/lib/wemob-auth`) && !strings.Contains(content, `@/lib/wemob-auth-client`) {
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
		if isTemplateFile(filePath) || isConfigFile(filePath) {
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

	// --- Auth-specific checks (only when wemob-auth.ts exists) ---
	if _, hasAuth := files["lib/wemob-auth.ts"]; hasAuth {
		// Check A1: session.userId / session.username → should be session.user.id
		sessionDirectAccessRegex := regexp.MustCompile(`session\.(userId|username|email|name|role)\b`)
		for fp, fc := range files {
			if isTemplateFile(fp) || isConfigFile(fp) {
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
			if isTemplateFile(fp) || isConfigFile(fp) {
				continue
			}
			if !strings.HasPrefix(fp, "app/api/") {
				continue
			}
			if strings.Contains(fc, "wemob-auth-client") {
				errors = append(errors, validationError{
					File:  fp,
					Error: "API route imports from wemob-auth-client (client-only module). API routes must use server-side '@/lib/wemob-auth' instead (getApiSession, register, login, logout).",
					Type:  "auth_client_in_api",
				})
			}
			// getApiSession() takes no arguments — LLM often passes request/req
			if getApiSessionArgsRegex.MatchString(fc) {
				errors = append(errors, validationError{
					File:  fp,
					Error: "getApiSession() takes no arguments (it reads cookies internally via Next.js headers). Remove the argument: use `await getApiSession()` instead of `getApiSession(request)` or `getApiSession(req)`.",
					Type:  "auth_api_session_args",
				})
			}
		}
	}

	// --- E-Commerce specific checks (only when wemob-store.ts exists) ---
	if _, hasStore := files["lib/wemob-store.ts"]; hasStore {

		// Check E2: Manual price formatting (should use formatPrice())
		manualPriceRegex := regexp.MustCompile(`(?:price|cents|amount)\s*[/]\s*100|\.toFixed\(\s*2\s*\)`)
		for fp, fc := range files {
			if isTemplateFile(fp) || isConfigFile(fp) || !strings.HasSuffix(fp, ".tsx") && !strings.HasSuffix(fp, ".ts") {
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
			if isTemplateFile(fp) || isConfigFile(fp) {
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
			if isTemplateFile(fp) || isConfigFile(fp) {
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
			if isTemplateFile(fp) {
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

			weights, isNonVariable := nonVariableFonts[fontName]
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
		if isTemplateFile(filePath) || isConfigFile(filePath) {
			continue
		}
		if !strings.HasSuffix(filePath, ".tsx") && !strings.HasSuffix(filePath, ".ts") {
			continue
		}

		hasUseClient := strings.Contains(content, `"use client"`) || strings.Contains(content, `'use client'`)
		if hasUseClient {
			continue
		}

		// Never add "use client" to files that export metadata — metadata requires
		// Server Components. If the file also uses hooks, it needs restructuring
		// (extract client logic into a wrapper), not a "use client" directive.
		if metadataExportPattern.MatchString(content) {
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
		if isTemplateFile(filePath) || isConfigFile(filePath) {
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
		if isTemplateFile(filePath) || isConfigFile(filePath) {
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
		if returnIdx == -1 {
			returnIdx = strings.LastIndex(body, "return")
		}

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

