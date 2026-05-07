package database

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/knid/wemob/internal/models"
)

// --- Neon / DataModel helpers ---

// nonIdentifierChars matches characters that are not valid in SQL identifier names.
var nonIdentifierChars = regexp.MustCompile(`[^a-zA-Z0-9_]`)

// safeSQLDefaultPattern matches safe SQL DEFAULT values: numbers, booleans, NULL,
// string literals with optional type cast, CURRENT_* keywords, and no-arg function calls.
var safeSQLDefaultPattern = regexp.MustCompile(`(?i)^(?:` +
	`-?\d+(?:\.\d+)?` + // numeric literals (including negative)
	`|true|false` + // boolean
	`|null` + // NULL
	`|'[^']*'(?:::[a-zA-Z_]+(?:\[\])?)?` + // string literal with optional type cast
	`|current_[a-z_]+` + // CURRENT_TIMESTAMP etc.
	`|[a-z_][a-z0-9_]*\(\)` + // no-arg function calls like now(), gen_random_uuid()
	`)$`)

// quoteIdentifier wraps an identifier in double quotes, escaping internal quotes.
func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// sqlTypeMap maps DataModel column types to PostgreSQL types.
var sqlTypeMap = map[string]string{
	"text":      "TEXT",
	"integer":   "INTEGER",
	"boolean":   "BOOLEAN",
	"timestamp": "TIMESTAMPTZ",
	"uuid":      "UUID",
	"jsonb":     "JSONB",
	"serial":    "SERIAL",
	"float":     "DOUBLE PRECISION",
	"decimal":   "NUMERIC",
	"numeric":   "NUMERIC",
	"real":      "REAL",
	"bigint":    "BIGINT",
}

// BuildCreateTableSQL generates CREATE TABLE statements from DataModels.
// All tables are created first, then all foreign key constraints are added,
// so that tables can reference each other regardless of order.
func BuildCreateTableSQL(dataModels []models.DataModel) string {
	var sb strings.Builder
	var fks strings.Builder

	// First pass: create all tables
	for _, m := range dataModels {
		sb.WriteString(fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n", quoteIdentifier(m.Name)))
		for i, col := range m.Columns {
			pgType := sqlTypeMap[col.Type]
			if pgType == "" {
				pgType = "TEXT"
			}
			sb.WriteString(fmt.Sprintf("  %s %s", quoteIdentifier(col.Name), pgType))
			if col.PrimaryKey {
				sb.WriteString(" PRIMARY KEY")
				if col.Type == "uuid" {
					sb.WriteString(" DEFAULT gen_random_uuid()")
				}
			}
			if !col.Nullable && !col.PrimaryKey {
				sb.WriteString(" NOT NULL")
			}
			if col.Default != "" && !col.PrimaryKey && safeSQLDefaultPattern.MatchString(strings.TrimSpace(col.Default)) {
				sb.WriteString(fmt.Sprintf(" DEFAULT %s", col.Default))
			} else if !col.PrimaryKey && (col.Name == "created_at" || col.Name == "updated_at") {
				sb.WriteString(" DEFAULT now()")
			}
			if i < len(m.Columns)-1 {
				sb.WriteString(",")
			}
			sb.WriteString("\n")
		}
		sb.WriteString(");\n\n")

		// Collect foreign key constraints for second pass
		for _, col := range m.Columns {
			if col.References != "" {
				parts := strings.SplitN(col.References, ".", 2)
				if len(parts) == 2 {
					constraintName := fmt.Sprintf("fk_%s_%s", nonIdentifierChars.ReplaceAllString(m.Name, ""), nonIdentifierChars.ReplaceAllString(col.Name, ""))
					fks.WriteString(fmt.Sprintf("DO $$ BEGIN ALTER TABLE %s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s(%s) ON DELETE CASCADE; EXCEPTION WHEN duplicate_object THEN NULL; END $$;\n",
						quoteIdentifier(m.Name), quoteIdentifier(constraintName), quoteIdentifier(col.Name), quoteIdentifier(parts[0]), quoteIdentifier(parts[1])))
				}
			}
		}
	}

	// Second pass: add all foreign key constraints after all tables exist
	sb.WriteString(fks.String())
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

// BuildDrizzleSchema generates a lib/schema.ts file with Drizzle pgTable definitions.
func BuildDrizzleSchema(dataModels []models.DataModel) string {
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

// NeonConnStrWithEndpoint appends the endpoint ID option to a Neon connection string
// so that lib/pq (which lacks SNI support) can route to the correct Neon compute.
// e.g. postgresql://user:pass@ep-cool-name-123456.us-east-2.aws.neon.tech/neondb?sslmode=require
// → adds &options=endpoint%3Dep-cool-name-123456
func NeonConnStrWithEndpoint(connStr string) string {
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
