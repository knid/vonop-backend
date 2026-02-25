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
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
	"github.com/posthog/posthog-go"

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

// getAuthTemplateContent returns the hardcoded content for an auth template file
func getAuthTemplateContent(path string) string {
	switch path {
	case "lib/wemob-auth.ts":
		return constants.NEXTJS_TEMPLATE_WEMOB_AUTH
	case "lib/wemob-auth-client.tsxx":
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

	model := constants.DEFAULT_NEXTJS_LLM_MODEL
	provider := c.llmProvider()

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

// appMegaStreamTools defines the tools available to the mega-stream codegen (write_file + read_file + list_files).
var appMegaStreamTools = []utils.Tool{
	{
		Type: "function",
		Function: utils.ToolFunction{
			Name:        "write_file",
			Description: "Write a complete file to the project. Use this tool for each file you generate.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file_path": map[string]any{
						"type":        "string",
						"description": "Relative file path, e.g. 'components/header.tsx'",
					},
					"content": map[string]any{
						"type":        "string",
						"description": "The complete file content",
					},
				},
				"required": []string{"file_path", "content"},
			},
		},
	},
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

// buildCodegenToolExecutor creates a tool executor for the mega-stream codegen path.
// It handles write_file (strip fences, upsert DB, update map, emit SSE), read_file, and list_files.
func (c *Controller) buildCodegenToolExecutor(
	project *models.AppProject,
	generatedFiles map[string]string,
	mu *sync.Mutex,
	sendEvent func(string, any) bool,
) utils.ToolExecutor {
	return func(name string, arguments string) (string, error) {
		switch name {
		case "write_file":
			var args struct {
				FilePath string `json:"file_path"`
				Content  string `json:"content"`
			}
			if err := json.Unmarshal([]byte(arguments), &args); err != nil {
				return fmt.Sprintf("Error: invalid arguments: %s", err.Error()), nil
			}
			if args.FilePath == "" || args.Content == "" {
				return "Error: file_path and content are required", nil
			}

			content := utils.StripCodeFences(args.Content)
			_, fileType := guessFileType(args.FilePath)
			// Override fileType for known extensions
			if strings.HasSuffix(args.FilePath, ".css") {
				fileType = "css"
			} else if strings.HasSuffix(args.FilePath, ".tsx") {
				fileType = "tsx"
			} else if strings.HasSuffix(args.FilePath, ".ts") {
				fileType = "ts"
			}

			if fileType == "tsx" || fileType == "ts" {
				content, _ = stripForbiddenImports(content, args.FilePath)
				checkMultilineForbiddenImports(content, args.FilePath)
			}

			// Upsert to DB
			file := models.AppGeneratedFile{
				AppProjectID: project.ID,
				Platform:     "nextjs",
				FilePath:     args.FilePath,
				Content:      content,
				FileType:     fileType,
				IsGenerated:  true,
			}
			createdFile, dbErr := c.DB.UpsertAppGeneratedFile(&file)
			if dbErr != nil {
				slog.Error("mega-stream: failed to save file", "error", dbErr, "path", args.FilePath)
				return fmt.Sprintf("Error: failed to save file: %s", dbErr.Error()), nil
			}

			// Update shared map
			mu.Lock()
			generatedFiles[args.FilePath] = content
			mu.Unlock()

			// Emit SSE event
			sendEvent("file_complete", responses.FileCompleteEvent{
				Action:   "create",
				FilePath: args.FilePath,
				FileID:   createdFile.ID,
				FileType: fileType,
			})

			lineCount := strings.Count(content, "\n") + 1
			return fmt.Sprintf("Created %s (%d lines)", args.FilePath, lineCount), nil

		case "read_file":
			var args struct {
				FilePath string `json:"file_path"`
			}
			json.Unmarshal([]byte(arguments), &args)
			mu.Lock()
			content, ok := generatedFiles[args.FilePath]
			mu.Unlock()
			if !ok {
				return fmt.Sprintf("Error: file '%s' not found in project", args.FilePath), nil
			}
			return content, nil

		case "list_files":
			mu.Lock()
			var lines []string
			for path, content := range generatedFiles {
				lines = append(lines, fmt.Sprintf("%s (%d chars)", path, len(content)))
			}
			mu.Unlock()
			sort.Strings(lines)
			return strings.Join(lines, "\n"), nil

		default:
			return fmt.Sprintf("Unknown tool: %s", name), nil
		}
	}
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

// llmFixPass sends validation errors to LLM with tool access for auto-fix.
// Always uses Sonnet regardless of the main model — fix passes are simple tasks.
func (c *Controller) llmFixPass(ctx context.Context, files map[string]string, valErrors []validationError, structure *appFileStructure) map[string]string {
	provider := c.llmProvider()
	model := constants.DEFAULT_NEXTJS_LLM_MODEL

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

	// Include export manifest so the LLM can see all available exports without needing tool calls
	exportManifest := buildExportManifest(files)
	var manifestSection string
	if exportManifest != "" {
		manifestSection = "\nExport manifest (shows what each file exports):\n" + exportManifest + "\n"
	}

	userMessage := fileList.String() + errList.String() + manifestSection + "\nUse the export manifest above to understand available exports. Use read_file to inspect files that need fixing, then provide fixes."

	fixLocalExecutor := c.buildChatToolExecutor(nil, nil, files, nil)
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

// validateAndFixFiles runs deterministic checks and LLM fix loop. Max 3 iterations.
// Returns total number of fixed files and the list of changed file paths.
func (c *Controller) validateAndFixFiles(ctx context.Context, project *models.AppProject, files map[string]string, structure *appFileStructure, sendEvent func(string, any) bool) (int, []string) {
	totalFixed := 0
	allChangedPaths := make(map[string]bool)

	// Phase 0: deterministic auto-fixes (before LLM pass)

	// Fix .ts files with JSX → rename to .tsx
	tsxRenamed, tsxPaths := autoFixTSFilesWithJSX(files)
	if tsxRenamed > 0 {
		for i := 0; i+1 < len(tsxPaths); i += 2 {
			oldPath := tsxPaths[i]
			newPath := tsxPaths[i+1]
			if oldFile, err := c.DB.GetAppGeneratedFileByPath(project.ID.String(), "nextjs", oldPath); err == nil {
				c.DB.DeleteAppGeneratedFile(oldFile)
			}
			_, fileType := guessFileType(newPath)
			newFile := models.AppGeneratedFile{
				AppProjectID: project.ID,
				Platform:     "nextjs",
				FilePath:     newPath,
				Content:      files[newPath],
				FileType:     fileType,
				IsGenerated:  true,
			}
			c.DB.CreateAppGeneratedFile(&newFile)
			allChangedPaths[newPath] = true
		}
		totalFixed += tsxRenamed
		slog.Info("Auto-fixed .ts→.tsx renames", "project_id", project.ID, "count", tsxRenamed)
	}

	// Fix metadata export in "use client" components
	metaFixed, metaPaths := autoFixMetadataInClientComponent(files)
	if metaFixed > 0 {
		for _, path := range metaPaths {
			if dbFile, err := c.DB.GetAppGeneratedFileByPath(project.ID.String(), "nextjs", path); err == nil && dbFile != nil {
				dbFile.Content = files[path]
				dbFile.UpdatedAt = time.Now()
				c.DB.UpdateAppGeneratedFile(dbFile)
			}
			allChangedPaths[path] = true
		}
		totalFixed += metaFixed
		slog.Info("Auto-fixed metadata in client components", "project_id", project.ID, "files_fixed", metaFixed)
	}

	// Fix root layout missing <html> and <body> tags
	if layoutFixed, layoutPath := autoFixRootLayoutTags(files); layoutFixed {
		if dbFile, err := c.DB.GetAppGeneratedFileByPath(project.ID.String(), "nextjs", layoutPath); err == nil && dbFile != nil {
			dbFile.Content = files[layoutPath]
			dbFile.UpdatedAt = time.Now()
			c.DB.UpdateAppGeneratedFile(dbFile)
		}
		allChangedPaths[layoutPath] = true
		totalFixed++
		slog.Info("Auto-fixed root layout missing <html>/<body> tags", "project_id", project.ID)
	}

	// Fix import/export mismatches
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

	for iteration := 1; iteration <= 3; iteration++ {
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
			if iteration >= 3 {
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

	// Final validation: run checks one more time and report remaining errors to the frontend
	finalErrors := runDeterministicChecks(files)
	if len(finalErrors) > 0 {
		errStrings := make([]string, len(finalErrors))
		for i, e := range finalErrors {
			errStrings[i] = fmt.Sprintf("[%s] %s: %s", e.Type, e.File, e.Error)
		}
		slog.Warn("Fix loop ended with remaining errors", "project_id", project.ID, "count", len(finalErrors))
		sendEvent("validation", responses.ValidationEvent{
			Iteration: 0,
			Errors:    errStrings,
			Fixing:    false,
		})
	} else {
		sendEvent("validation", responses.ValidationEvent{
			Iteration: 0,
			Errors:    []string{},
			Fixing:    false,
		})
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

// fileNeedsStoreSDK returns true if the file is likely to need the full wemob-store.ts SDK.
func fileNeedsStoreSDK(filePath string) bool {
	lower := strings.ToLower(filePath)
	if strings.HasPrefix(lower, "components/ui/") || lower == "lib/utils.ts" {
		return false
	}
	return strings.Contains(lower, "cart") ||
		strings.Contains(lower, "product") ||
		strings.Contains(lower, "checkout") ||
		strings.Contains(lower, "shop") ||
		strings.Contains(lower, "store") ||
		strings.Contains(lower, "order") ||
		strings.Contains(lower, "layout") // layout wraps CartProvider
}

// fileNeedsAuth returns true if the file is likely to need the full auth templates.
func fileNeedsAuth(filePath string) bool {
	lower := strings.ToLower(filePath)
	if strings.HasPrefix(lower, "components/ui/") || lower == "lib/utils.ts" {
		return false
	}
	return strings.Contains(lower, "auth") ||
		strings.Contains(lower, "login") ||
		strings.Contains(lower, "register") ||
		strings.Contains(lower, "signup") ||
		strings.Contains(lower, "sign-up") ||
		strings.Contains(lower, "profile") ||
		strings.Contains(lower, "account") ||
		strings.Contains(lower, "dashboard") ||
		strings.Contains(lower, "layout") // layout wraps AuthProvider
}

// condensedStoreExports is a compact summary of wemob-store.ts for files that don't need the full SDK.
const condensedStoreExports = `lib/wemob-store.ts exports (client-only "use client" module — any file importing it MUST have "use client"):
Functions: getStoreConfig, getProducts, getProduct, getCategories, createCart, getCart, addCartItem, updateCartItem, removeCartItem, applyDiscountCode, removeDiscount, checkout, confirmCheckout, getOrder, formatPrice, renderPaymentForm, getSessionId, clearSession
Components: CartProvider (wrap in layout), useCart, useProducts, useProduct, useCategories, useStoreConfig
Types: StoreConfig, StorefrontProduct, StorefrontVariant, StorefrontCategory, StorefrontCart, StorefrontCartItem, StorefrontCheckout, CheckoutConfirm, CheckoutParams, OrderTracking, ShippingAddress
Prices are in cents. Use formatPrice(cents) for display.`

// condensedAuthExports is a compact summary of auth templates for files that don't need the full code.
const condensedAuthExports = `Auth exports:
Server-side (lib/wemob-auth.ts): register, login, logout, getSession, getCurrentUser, requireAuth, getApiSession — NEVER import in "use client" files
Client-side (lib/wemob-auth-client.tsx): AuthProvider (wrap in layout), useAuth → { user, isLoading, isAuthenticated, login, register, logout, refreshSession }
Users table: id (uuid), email, password_hash, name, avatar_url, role, email_verified, created_at, updated_at`

// generateTierMegaStream generates all files in a tier via a single LLM call using
// StreamWithTools + write_file tool. Returns the list of files that were NOT successfully
// generated (for fallback to per-file generation).
func (c *Controller) generateTierMegaStream(
	ctx context.Context,
	project *models.AppProject,
	tier []appFilePlanEntry,
	structure *appFileStructure,
	model string,
	provider *utils.LLMProvider,
	generatedFiles map[string]string,
	mu *sync.Mutex,
	allFilePaths []string,
	colorSchemeJSON []byte,
	storeContext string,
	authContext string,
	sendEvent func(string, any) bool,
) []appFilePlanEntry {
	tierCtx, tierCancel := context.WithTimeout(ctx, time.Duration(len(tier)+3)*2*time.Minute)
	defer tierCancel()

	// --- Block 1: Combined base prompt (TSX + CSS rules if tier has CSS files) ---
	hasCSSFile := false
	for _, fp := range tier {
		if fp.Type == "css" {
			hasCSSFile = true
			break
		}
	}
	basePrompt := constants.NEXTJS_FILE_TSX_PROMPT
	// Strip the "Tools Available" section (we define our own tools)
	if idx := strings.Index(basePrompt, "\n\n### Tools Available"); idx >= 0 {
		basePrompt = basePrompt[:idx]
	}
	if hasCSSFile {
		basePrompt += "\n\n### CSS File Rules\n" + constants.NEXTJS_FILE_CSS_PROMPT
	}
	if project.ProjectType == "mobile_app" {
		basePrompt += "\n\n" + constants.NEXTJS_MOBILE_DESIGN_RULES
	}

	// --- Block 2: Project context + SDK (identical to generateSingleAppFile) ---
	var projectContext strings.Builder
	projectContext.WriteString(fmt.Sprintf("### Project Context\nApp: %s\nColor scheme: %s\nTypography: %v\nAll files in project: %s\n\nUser's original description: %s",
		structure.Name,
		string(colorSchemeJSON),
		structure.Typography,
		strings.Join(allFilePaths, ", "),
		project.Prompt,
	))

	mu.Lock()
	_, hasDB := generatedFiles["lib/db.ts"]
	schemaContent, hasSchema := generatedFiles["lib/schema.ts"]
	mu.Unlock()
	if hasDB && hasSchema {
		projectContext.WriteString(fmt.Sprintf("\n\n--- lib/schema.ts (INJECTED — import tables from '@/lib/schema') ---\n%s", schemaContent))
		projectContext.WriteString("\n\n### Database Access\nUse `import { db } from '@/lib/db'` and `import { ... } from '@/lib/schema'` for data access. Use Drizzle queries: `db.select().from(tableName)`, `db.insert(tableName).values({...})`, etc. Import operators from 'drizzle-orm': `import { eq, and, desc, sql } from 'drizzle-orm'`.\n")
	}

	// Check if any file in tier needs store SDK or auth
	tierNeedsStore := false
	tierNeedsAuth := false
	for _, fp := range tier {
		if fileNeedsStoreSDK(fp.Path) {
			tierNeedsStore = true
		}
		if fileNeedsAuth(fp.Path) {
			tierNeedsAuth = true
		}
	}

	if project.ProjectType == "ecommerce" {
		if tierNeedsStore {
			mu.Lock()
			if storeContent, ok := generatedFiles["lib/wemob-store.ts"]; ok {
				projectContext.WriteString(fmt.Sprintf("\n\n--- lib/wemob-store.ts (INJECTED SDK — use these exports, do NOT create custom API utilities) ---\n%s", storeContent))
			}
			mu.Unlock()
			if storeContext != "" {
				projectContext.WriteString(fmt.Sprintf("\n\n### Store Context\n%s", storeContext))
			}
		} else {
			projectContext.WriteString("\n\n### E-Commerce SDK (condensed)\n" + condensedStoreExports)
			if storeContext != "" {
				projectContext.WriteString(fmt.Sprintf("\n\n### Store Context\n%s", storeContext))
			}
		}
	}

	if authContext != "" {
		if tierNeedsAuth {
			mu.Lock()
			if authContent, ok := generatedFiles["lib/wemob-auth.ts"]; ok {
				projectContext.WriteString(fmt.Sprintf("\n\n--- lib/wemob-auth.ts (INJECTED AUTH — use these exports for server-side auth, do NOT create custom auth utilities) ---\n%s", authContent))
			}
			if authClientContent, ok := generatedFiles["lib/wemob-auth-client.tsx"]; ok {
				projectContext.WriteString(fmt.Sprintf("\n\n--- lib/wemob-auth-client.tsx (INJECTED AUTH CLIENT — use these exports for client-side auth) ---\n%s", authClientContent))
			}
			mu.Unlock()
			projectContext.WriteString(fmt.Sprintf("\n\n### Auth Context\n%s", authContext))
		} else {
			projectContext.WriteString("\n\n### Auth (condensed)\n" + condensedAuthExports)
		}
	}

	// --- Block 3: User prompt listing all files to generate ---
	mu.Lock()
	var generatedFileList strings.Builder
	if len(generatedFiles) > 0 {
		generatedFileList.WriteString("\nAlready-generated files:\n")
		genPaths := make([]string, 0, len(generatedFiles))
		for p := range generatedFiles {
			genPaths = append(genPaths, p)
		}
		sort.Strings(genPaths)
		for _, p := range genPaths {
			generatedFileList.WriteString(fmt.Sprintf("- %s\n", p))
		}
	}
	fileSnapshot := make(map[string]string, len(generatedFiles))
	for k, v := range generatedFiles {
		fileSnapshot[k] = v
	}
	mu.Unlock()

	exportManifest := buildExportManifest(fileSnapshot)
	var manifestSection string
	if exportManifest != "" {
		manifestSection = "\n\nAvailable exports from already-generated files:\n" + exportManifest
	}

	var userPrompt strings.Builder
	userPrompt.WriteString("Generate the following files using the write_file tool for EACH file. Call write_file once per file with the complete implementation.\n\n")
	for i, fp := range tier {
		userPrompt.WriteString(fmt.Sprintf("%d. %s (%s) — %s\n", i+1, fp.Path, fp.Type, fp.Description))
	}
	userPrompt.WriteString(generatedFileList.String())
	userPrompt.WriteString(manifestSection)
	userPrompt.WriteString(fmt.Sprintf("\n\nIMPORTANT: You MUST call write_file exactly %d times, once for each file listed above. Generate complete, production-quality implementations. Do not skip any file.", len(tier)))

	messages := []utils.Message{
		{Role: "system", Content: basePrompt},
		{Role: "system", Content: projectContext.String()},
		{Role: "user", Content: userPrompt.String()},
	}

	// Build tool executor
	executor := c.buildCodegenToolExecutor(project, generatedFiles, mu, sendEvent)

	// Configure: generous rounds for tool calls, high max tokens
	tierSize := len(tier)
	cfg := utils.ToolCallConfig{
		MaxRounds:  tierSize + 5,
		NudgeAfter: tierSize + 3,
		StripAfter: tierSize + 5,
		MaxTokens:  32768,
	}

	slog.Info("Starting mega-stream for tier",
		"project_id", project.ID,
		"files", tierSize,
		"max_rounds", cfg.MaxRounds,
	)

	_, err := utils.StreamWithTools(
		tierCtx,
		model,
		messages,
		appMegaStreamTools,
		provider,
		nil, // no response format
		nil, // no reasoning
		executor,
		func(toolName string, args string) {
			// Log tool calls for observability
			if toolName == "write_file" {
				var a struct {
					FilePath string `json:"file_path"`
				}
				json.Unmarshal([]byte(args), &a)
				slog.Info("mega-stream write_file", "project_id", project.ID, "path", a.FilePath)
			}
		},
		func(chunk string) {
			// Stream text chunks as batch progress
			sendEvent("file_chunk", responses.FileOperationEvent{
				Action: "create",
				Chunk:  chunk,
			})
		},
		nil, // no reasoning callback
		cfg,
	)

	if err != nil {
		slog.Error("mega-stream failed", "project_id", project.ID, "error", err)
	}

	// Determine which files were NOT generated by the mega-stream
	mu.Lock()
	var failedFiles []appFilePlanEntry
	for _, fp := range tier {
		if _, ok := generatedFiles[fp.Path]; !ok {
			failedFiles = append(failedFiles, fp)
		}
	}
	mu.Unlock()

	if len(failedFiles) > 0 {
		slog.Warn("mega-stream missed files, will fallback to per-file generation",
			"project_id", project.ID,
			"missed", len(failedFiles),
			"total", tierSize,
		)
	}

	return failedFiles
}

// generateSingleAppFile generates a single app file with a per-file timeout.
// It reads generatedFiles under mu lock for context building, and returns the raw content.
// System prompt is split into multiple blocks for Anthropic prompt caching:
//   Block 1 (base prompt) — stable across all files of same type
//   Block 2 (project context + SDK) — stable across all files in same project
//   Block 3 (per-file variable context) — user message, changes per file
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
	authContext string,
	sendEvent func(string, any) bool,
) (string, error) {
	fileCtx, fileCancel := context.WithTimeout(ctx, 5*time.Minute)
	defer fileCancel()

	// --- Block 1: Base prompt (stable across ALL files of same type, highly cacheable) ---
	basePrompt := c.getAppFilePrompt(filePlan.Type)

	// Strip the "Tools Available" section from the base prompt since we no longer use tools
	if idx := strings.Index(basePrompt, "\n\n### Tools Available"); idx >= 0 {
		basePrompt = basePrompt[:idx]
	}

	// Inject mobile design rules into the base prompt for mobile_app projects
	if project.ProjectType == "mobile_app" {
		basePrompt += "\n\n" + constants.NEXTJS_MOBILE_DESIGN_RULES
	}

	// --- Block 2: Project context + SDK templates (stable across all files in project) ---
	var projectContext strings.Builder
	projectContext.WriteString(fmt.Sprintf("### Project Context\nApp: %s\nColor scheme: %s\nTypography: %v\nAll files in project: %s\n\nUser's original description: %s",
		structure.Name,
		string(colorSchemeJSON),
		structure.Typography,
		strings.Join(allFilePaths, ", "),
		project.Prompt,
	))

	// Inject database schema context only when lib/db.ts was actually provisioned
	mu.Lock()
	_, hasDB := generatedFiles["lib/db.ts"]
	schemaContent, hasSchema := generatedFiles["lib/schema.ts"]
	mu.Unlock()
	if hasDB && hasSchema {
		projectContext.WriteString(fmt.Sprintf("\n\n--- lib/schema.ts (INJECTED — import tables from '@/lib/schema') ---\n%s", schemaContent))
		projectContext.WriteString("\n\n### Database Access\nUse `import { db } from '@/lib/db'` and `import { ... } from '@/lib/schema'` for data access. Use Drizzle queries: `db.select().from(tableName)`, `db.insert(tableName).values({...})`, etc. Import operators from 'drizzle-orm': `import { eq, and, desc, sql } from 'drizzle-orm'`.\n")
	}

	// Conditionally inject SDK templates based on what this file actually needs
	if project.ProjectType == "ecommerce" {
		if fileNeedsStoreSDK(filePlan.Path) {
			// Full SDK for files that use cart/products/checkout
			mu.Lock()
			if storeContent, ok := generatedFiles["lib/wemob-store.ts"]; ok {
				projectContext.WriteString(fmt.Sprintf("\n\n--- lib/wemob-store.ts (INJECTED SDK — use these exports, do NOT create custom API utilities) ---\n%s", storeContent))
			}
			mu.Unlock()
			if storeContext != "" {
				projectContext.WriteString(fmt.Sprintf("\n\n### Store Context\n%s", storeContext))
			}
		} else {
			// Condensed summary for files that don't directly use the SDK
			projectContext.WriteString("\n\n### E-Commerce SDK (condensed)\n" + condensedStoreExports)
			if storeContext != "" {
				projectContext.WriteString(fmt.Sprintf("\n\n### Store Context\n%s", storeContext))
			}
		}
	}

	// Conditionally inject auth templates
	if authContext != "" {
		if fileNeedsAuth(filePlan.Path) {
			// Full auth templates for auth-related files
			mu.Lock()
			if authContent, ok := generatedFiles["lib/wemob-auth.ts"]; ok {
				projectContext.WriteString(fmt.Sprintf("\n\n--- lib/wemob-auth.ts (INJECTED AUTH — use these exports for server-side auth, do NOT create custom auth utilities) ---\n%s", authContent))
			}
			if authClientContent, ok := generatedFiles["lib/wemob-auth-client.tsx"]; ok {
				projectContext.WriteString(fmt.Sprintf("\n\n--- lib/wemob-auth-client.tsx (INJECTED AUTH CLIENT — use these exports for client-side auth) ---\n%s", authClientContent))
			}
			mu.Unlock()
			projectContext.WriteString(fmt.Sprintf("\n\n### Auth Context\n%s", authContext))
		} else {
			// Condensed summary for files that don't directly use auth
			projectContext.WriteString("\n\n### Auth (condensed)\n" + condensedAuthExports)
		}
	}

	// --- Build per-file user prompt (variable per file) ---
	mu.Lock()
	var generatedFileList strings.Builder
	if len(generatedFiles) > 0 {
		generatedFileList.WriteString("\nAlready-generated files:\n")
		genPaths := make([]string, 0, len(generatedFiles))
		for p := range generatedFiles {
			genPaths = append(genPaths, p)
		}
		sort.Strings(genPaths)
		for _, p := range genPaths {
			generatedFileList.WriteString(fmt.Sprintf("- %s\n", p))
		}
	}
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

	// Build multi-block messages for caching efficiency:
	// - Block 1 (system): base prompt — same across all files of same type
	// - Block 2 (system): project context + SDK — same across all files in project
	// - Block 3 (user): per-file prompt — changes per file
	messages := []utils.Message{
		{Role: "system", Content: basePrompt},
		{Role: "system", Content: projectContext.String()},
		{Role: "user", Content: userPrompt},
	}

	content, err := utils.StreamTextFromLLMWithMessages(
		fileCtx,
		model,
		messages,
		provider,
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
// Always uses Sonnet regardless of the main model — repair is a simple task.
func (c *Controller) repairSingleAppFile(
	ctx context.Context,
	project *models.AppProject,
	filePlan appFilePlanEntry,
	structure *appFileStructure,
	_ string, // model param ignored — always uses Sonnet
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

	// Always use Sonnet for repair — simple task, no need for expensive models
	repairModel := constants.DEFAULT_NEXTJS_LLM_MODEL

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

	codegenLocalExecutor := c.buildChatToolExecutor(nil, nil, fileSnapshot, nil)
	codegenExecutor := c.compositeToolExecutor(codegenLocalExecutor)
	codegenTools := append([]utils.Tool{}, appCodegenTools...)

	content, err := utils.StreamTextWithTools(
		fileCtx,
		repairModel,
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
	var needsAuth bool

	provider := c.llmProvider()
	model := c.SecretManager.GetSecret("WEBSITE_BUILDER_LLM_MODEL")
	if model == "" {
		model = constants.DEFAULT_NEXTJS_LLM_MODEL
	}
	concurrency := c.llmConcurrency()
	if provider.IsAnthropic() {
		concurrency = 1
	}
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
				Set("needs_auth", needsAuth).
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

	// Step 2b: Store DataModels and provision Neon database
	dataModels := structure.DataModels
	slog.Info("Step 2b: DataModels from LLM", "count", len(dataModels), "project_id", project.ID)
	for _, dm := range dataModels {
		slog.Info("  DataModel table", "name", dm.Name, "columns", len(dm.Columns))
	}

	// For ecommerce: merge default ecommerce schema with any extra AI-designed models
	if projectType == "ecommerce" {
		var defaultModels []DataModel
		if err := json.Unmarshal([]byte(constants.ECOMMERCE_DEFAULT_DATA_MODELS), &defaultModels); err != nil {
			slog.Error("Failed to parse default ecommerce data models", "error", err)
		} else {
			// Build a set of existing table names from AI-designed models
			existingTables := make(map[string]bool)
			for _, m := range dataModels {
				existingTables[m.Name] = true
			}
			// Prepend default ecommerce models (only if not already defined by AI)
			for _, dm := range defaultModels {
				if !existingTables[dm.Name] {
					dataModels = append([]DataModel{dm}, dataModels...)
				}
			}
		}
	}

	// Detect needsAuth: primary flag from LLM, fallback heuristics
	needsAuth = structure.NeedsAuth
	if !needsAuth {
		// Fallback: check if data_models has a users table with password_hash column
		for _, dm := range dataModels {
			if dm.Name == "users" {
				for _, col := range dm.Columns {
					if col.Name == "password_hash" {
						needsAuth = true
						break
					}
				}
			}
			if needsAuth {
				break
			}
		}
	}
	if !needsAuth {
		// Fallback: check file plan for login/register/signup/auth pages
		for _, f := range structure.Files {
			lp := strings.ToLower(f.Path)
			if strings.Contains(lp, "login") || strings.Contains(lp, "register") ||
				strings.Contains(lp, "signup") || strings.Contains(lp, "sign-up") ||
				strings.Contains(lp, "auth/") {
				needsAuth = true
				break
			}
		}
	}
	if needsAuth {
		slog.Info("Auth detected for project", "project_id", project.ID, "explicit", structure.NeedsAuth)
		if project.Settings == nil {
			project.Settings = make(map[string]any)
		}
		project.Settings["needs_auth"] = true
	}

	// For auth: merge default users table if not already present
	if needsAuth {
		var authModels []DataModel
		if err := json.Unmarshal([]byte(constants.AUTH_DEFAULT_DATA_MODELS), &authModels); err != nil {
			slog.Error("Failed to parse default auth data models", "error", err)
		} else {
			existingTables := make(map[string]bool)
			for _, m := range dataModels {
				existingTables[m.Name] = true
			}
			for _, dm := range authModels {
				if !existingTables[dm.Name] {
					dataModels = append([]DataModel{dm}, dataModels...)
				}
			}
		}
	}

	// Store data models in the project
	if len(dataModels) > 0 {
		dmJSON, _ := json.Marshal(dataModels)
		project.DataModels = dmJSON
	}

	project, _ = c.DB.UpdateAppProject(project)

	// Step 2c: Provision Neon database (non-blocking on failure)
	neonProvisioned := false
	var neonConnStr string
	if c.NeonService != nil && len(dataModels) > 0 {
		sendEvent("neon_provision", map[string]any{"phase": "start"})
		connStr, neonProjectID, neonErr := c.provisionNeonDatabase(ctx, project, dataModels, sendEvent)

		// Store integration whenever we have a valid connection string + project ID,
		// even if schema creation failed (the Neon project exists and is usable).
		if connStr != "" && neonProjectID != "" {
			if neonErr != nil {
				slog.Warn("Neon schema setup had errors, but project exists — storing integration anyway",
					"error", neonErr, "project_id", project.ID, "neon_project_id", neonProjectID)
			}
			if err := c.storeNeonIntegration(project, connStr, neonProjectID); err != nil {
				slog.Error("Failed to store Neon integration", "error", err, "project_id", project.ID)
				sendEvent("neon_provision", map[string]any{"phase": "error", "error": "failed to store integration: " + err.Error()})
			} else {
				neonProvisioned = true
				neonConnStr = connStr
				slog.Info("Neon integration stored", "project_id", project.ID, "neon_project_id", neonProjectID)
				sendEvent("neon_provision", map[string]any{"phase": "ready"})
			}
		} else if neonErr != nil {
			slog.Error("Neon project creation failed (non-fatal)", "error", neonErr, "project_id", project.ID)
			sendEvent("neon_provision", map[string]any{"phase": "error", "error": neonErr.Error()})
		}
	} else {
		slog.Info("Skipping Neon provisioning", "neon_service_nil", c.NeonService == nil, "data_models_count", len(dataModels), "project_id", project.ID)
	}

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

	// Step 3d: When auth is detected, strip LLM-planned files that duplicate wemob-auth
	if needsAuth {
		authDuplicateLibFiles := map[string]bool{
			"lib/auth.ts":         true,
			"lib/auth.tsx":        true,
			"lib/auth-context.ts": true,
			"lib/auth-context.tsx": true,
			"lib/session.ts":     true,
			"lib/jwt.ts":         true,
			"lib/password.ts":    true,
			"lib/auth-utils.ts":  true,
			"lib/auth-helpers.ts": true,
			"middleware.ts":      true,
		}
		filtered := appFiles[:0]
		for _, f := range appFiles {
			if authDuplicateLibFiles[f.Path] {
				slog.Info("Stripping duplicate auth lib file from plan (covered by wemob-auth)", "path", f.Path, "project_id", project.ID)
				continue
			}
			filtered = append(filtered, f)
		}
		appFiles = filtered
	}

	// Step 3e: Generate AUTH_SECRET env var for auth projects
	if needsAuth && neonProvisioned {
		authSecret, err := utils.RandomHex(32)
		if err != nil {
			slog.Error("Failed to generate AUTH_SECRET", "error", err, "project_id", project.ID)
		} else {
			envVar := &models.AppEnvVar{
				AppProjectID: project.ID,
				Key:          "AUTH_SECRET",
				Value:        authSecret,
				IsPublic:     false,
			}
			if _, err := c.DB.CreateAppEnvVar(envVar); err != nil {
				slog.Error("Failed to store AUTH_SECRET env var", "error", err, "project_id", project.ID)
			} else {
				slog.Info("Generated AUTH_SECRET for auth project", "project_id", project.ID)
			}
		}
	}

	// Build total file list (template + app files)
	templatePaths := constants.NEXTJS_TEMPLATE_FILE_PATHS
	allFilePaths := make([]string, 0, len(templatePaths)+len(appFiles)+len(constants.NEXTJS_ECOMMERCE_TEMPLATE_FILE_PATHS)+len(constants.NEXTJS_AUTH_TEMPLATE_FILE_PATHS)+4)
	for _, tp := range templatePaths {
		allFilePaths = append(allFilePaths, tp)
	}
	// Include db.ts/schema.ts only when Neon database was successfully provisioned
	if neonProvisioned {
		allFilePaths = append(allFilePaths, constants.NEXTJS_DB_TEMPLATE_FILE_PATHS...)
	}
	if projectType == "ecommerce" {
		allFilePaths = append(allFilePaths, constants.NEXTJS_ECOMMERCE_TEMPLATE_FILE_PATHS...)
	}
	if needsAuth && neonProvisioned {
		allFilePaths = append(allFilePaths, constants.NEXTJS_AUTH_TEMPLATE_FILE_PATHS...)
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

	// Step 4: Create conversation early with user message (atomic upsert)
	convMessages := []models.AppConversationMessage{
		{Role: "user", Content: project.Prompt, Timestamp: time.Now().Unix()},
	}
	convJSON, _ := json.Marshal(convMessages)
	conv, convErr := c.DB.GetOrCreateAppConversation(&models.AppConversation{
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

	// Helper to inject a template file
	injectTemplate := func(tp, content string) {
		fileType := "json"
		switch {
		case strings.HasSuffix(tp, ".ts"):
			fileType = "ts"
		case strings.HasSuffix(tp, ".mjs"):
			fileType = "mjs"
		case strings.HasSuffix(tp, ".css"):
			fileType = "css"
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
			return
		}
		generatedFiles[tp] = content
		sendEvent("file_complete", responses.FileCompleteEvent{
			Action:   "create",
			FilePath: tp,
			FileID:   createdFile.ID,
			FileType: fileType,
		})
	}

	for _, tp := range templatePaths {
		content := getTemplateContent(tp, structure.ColorScheme, structure.DarkColorScheme, projectType, dataModels, "")
		if content == "" {
			continue
		}
		injectTemplate(tp, content)
	}

	// Step 6a: Inject database template files only when Neon was successfully provisioned
	if neonProvisioned {
		for _, tp := range constants.NEXTJS_DB_TEMPLATE_FILE_PATHS {
			content := getTemplateContent(tp, structure.ColorScheme, structure.DarkColorScheme, projectType, dataModels, neonConnStr)
			if content == "" {
				continue
			}
			injectTemplate(tp, content)
		}
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

	// Step 6b2: Inject auth template files (conditional on needsAuth + neonProvisioned)
	if needsAuth && neonProvisioned {
		for _, tp := range constants.NEXTJS_AUTH_TEMPLATE_FILE_PATHS {
			content := getAuthTemplateContent(tp)
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
				slog.Error("failed to save auth template file", "error", err, "path", tp)
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

	// Step 6c: Generate .env.local so DATABASE_URL and other env vars are available
	{
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
				if !sliceContains(allFilePaths, ".env.local") {
					allFilePaths = append(allFilePaths, ".env.local")
				}
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

	// Step 7b: Build auth context for per-file codegen
	var authContext string
	if needsAuth && neonProvisioned {
		var sb strings.Builder
		sb.WriteString("This project uses authentication. Auth libraries are injected as template-managed files.\n")
		sb.WriteString("Server-side (lib/wemob-auth.ts): import { register, login, logout, getSession, getCurrentUser, requireAuth, getApiSession } from '@/lib/wemob-auth'\n")
		sb.WriteString("Client-side (lib/wemob-auth-client.tsx): import { AuthProvider, useAuth } from '@/lib/wemob-auth-client'\n")
		sb.WriteString("RULES:\n")
		sb.WriteString("- Wrap root layout with <AuthProvider> for client-side auth state.\n")
		sb.WriteString("- Use requireAuth() in server components for protected pages. It redirects to /login if not authenticated.\n")
		sb.WriteString("- Use useAuth() in client components for auth state: { user, isLoading, isAuthenticated, login, register, logout, refreshSession }.\n")
		sb.WriteString("- Use getApiSession() in API routes to get the current session.\n")
		sb.WriteString("- Create app/api/auth/[...action]/route.ts that handles POST requests dispatching to register/login/logout/getSession.\n")
		sb.WriteString("- The users table has: id (uuid), email, password_hash, name, avatar_url, role, email_verified, created_at, updated_at.\n")
		sb.WriteString("- NEVER import from @/lib/wemob-auth in 'use client' files. Use @/lib/wemob-auth-client instead.\n")
		sb.WriteString("- NEVER create custom auth, JWT, password hashing, or session management code.\n")
		authContext = sb.String()
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

		// Mega-stream: generate all files in this tier via a single LLM call
		failedFiles := c.generateTierMegaStream(
			ctx, project, tier, structure, model, provider,
			generatedFiles, &mu, allFilePaths, colorSchemeJSON,
			storeContext, authContext, sendEvent,
		)

		// Fallback: per-file generation for any files the mega-stream missed
		if len(failedFiles) > 0 {
			slog.Info("Falling back to per-file generation",
				"project_id", project.ID,
				"tier", tierIdx+1,
				"missed_files", len(failedFiles),
			)

			sem := make(chan struct{}, concurrency)
			var wg sync.WaitGroup

			for _, filePlan := range failedFiles {
				if ctx.Err() != nil {
					break
				}

				wg.Add(1)
				select {
				case sem <- struct{}{}:
				case <-ctx.Done():
					wg.Done()
					continue
				}
				go func(fp appFilePlanEntry) {
					defer wg.Done()
					defer func() { <-sem }()

					var content string
					var lastFailReason string
					for attempt := 0; attempt < 3; attempt++ {
						if ctx.Err() != nil {
							lastFailReason = "context cancelled"
							content = c.getAppFileFallback(fp)
							break
						}
						if attempt > 0 {
							select {
							case <-time.After(time.Duration(attempt) * 2 * time.Second):
							case <-ctx.Done():
							}
							if ctx.Err() != nil {
								lastFailReason = "context cancelled during retry"
								content = c.getAppFileFallback(fp)
								break
							}
							slog.Info("Retrying file generation (fallback)", "path", fp.Path, "attempt", attempt+1)
						}
						var err error
						content, err = c.generateSingleAppFile(ctx, project, fp, structure, model, provider, generatedFiles, &mu, allFilePaths, colorSchemeJSON, storeContext, authContext, sendEvent)
						if err != nil {
							slog.Error("failed to generate app file (fallback)", "error", err, "path", fp.Path, "attempt", attempt+1)
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
						if !strings.Contains(content, fallbackSentinel) {
							lastFailReason = ""
							break
						}
						if lastFailReason == "" {
							lastFailReason = "LLM returned placeholder/fallback content"
						}
					}

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
						slog.Error("failed to save app file (fallback)", "error", dbErr, "path", fp.Path)
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
		}

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
				if authContext != "" {
					if authContent, ok := fileSnapshot["lib/wemob-auth.ts"]; ok {
						sysPrompt.WriteString(fmt.Sprintf("\n\n--- lib/wemob-auth.ts (INJECTED AUTH) ---\n%s", authContent))
					}
					if authClientContent, ok := fileSnapshot["lib/wemob-auth-client.tsx"]; ok {
						sysPrompt.WriteString(fmt.Sprintf("\n\n--- lib/wemob-auth-client.tsx (INJECTED AUTH CLIENT) ---\n%s", authClientContent))
					}
					sysPrompt.WriteString(fmt.Sprintf("\n\n### Auth Context\n%s", authContext))
				}

				// Compact user prompt with import context
				var userPrompt strings.Builder
				userPrompt.WriteString(fmt.Sprintf("Generate: %s\n", filePath))
				if importLines.Len() > 0 {
					userPrompt.WriteString(fmt.Sprintf("\nThis file is imported with these exact import statements — match these exports:\n%s", importLines.String()))
				}
				userPrompt.WriteString("\nGenerate the complete implementation based on how it is used in consuming files.")

				missingLocalExecutor := c.buildChatToolExecutor(nil, nil, fileSnapshot, nil)
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

	// Step 11b: Auto-seed data
	var seedSummary string
	if neonProvisioned && len(dataModels) > 0 {
		// Seed Neon database with LLM-generated sample data
		seedCtx, seedCancel := context.WithTimeout(ctx, 2*time.Minute)
		summary, seedErr := c.seedNeonDatabase(seedCtx, project, dataModels, sendEvent)
		seedCancel()
		if seedErr != nil {
			slog.Warn("Failed to seed Neon database", "error", seedErr, "project_id", project.ID)
		} else {
			seedSummary = summary
		}
	} else if projectType == "ecommerce" {
		// Fallback: seed via Wemob store API for ecommerce projects without Neon
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

