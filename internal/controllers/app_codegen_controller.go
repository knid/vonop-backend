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

// detectEnvVarsFromCode scans all generated file contents for process.env.* references
// and returns a sorted list of unique env var keys, excluding auto-injected ones.
func detectEnvVarsFromCode(files map[string]string) []string {
	excluded := map[string]bool{
		"NODE_ENV":                  true,
		"NEXT_PUBLIC_STORE_ID":      true,
		"NEXT_PUBLIC_WEMOB_API_URL": true,
		"DATABASE_URL":              true,
		"AUTH_SECRET":               true,
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

// getTemplateContent returns the hardcoded content for a template file.
// dataModels is used to generate the dynamic lib/schema.ts file.
// neonConnStr is embedded into lib/db.ts as a fallback when DATABASE_URL env var is not set.
func getTemplateContent(path string, colorScheme, darkColorScheme map[string]any, projectType string, dataModels []DataModel, neonConnStr string) string {
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
	case "lib/db.ts":
		return fmt.Sprintf(constants.NEXTJS_TEMPLATE_DB_TS, neonConnStr)
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
	}
	return ""
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
	q.Set("options", "endpoint="+endpointID)
	u.RawQuery = q.Encode()
	return u.String()
}

// sliceContains checks if a string slice contains the given value.
func sliceContains(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
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

// storeNeonIntegration saves the Neon connection as an AppIntegration.
func (c *Controller) storeNeonIntegration(project *models.AppProject, connStr, neonProjectID string) error {
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

	_, err := c.DB.CreateAppIntegration(integration)
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

		// Remove the "use client" directive — metadata must be in Server Components
		updated := strings.Replace(content, "\"use client\"\n\n", "", 1)
		if updated == content {
			updated = strings.Replace(content, "\"use client\"\n", "", 1)
		}
		if updated == content {
			updated = strings.Replace(content, "'use client'\n\n", "", 1)
		}
		if updated == content {
			updated = strings.Replace(content, "'use client'\n", "", 1)
		}
		if updated == content {
			updated = strings.Replace(content, "\"use client\"", "", 1)
		}
		if updated == content {
			updated = strings.Replace(content, "'use client'", "", 1)
		}

		if updated != content {
			files[filePath] = updated
			changedPaths = append(changedPaths, filePath)
		}
	}

	return len(changedPaths), changedPaths
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

