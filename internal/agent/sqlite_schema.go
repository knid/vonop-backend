package agent

import (
	"fmt"
	"strings"

	"github.com/knid/wemob/internal/models"
)

// camelCaseSnake converts snake_case to camelCase for Drizzle property names.
// "is_done" → "isDone"; "id" → "id"; "created_at" → "createdAt".
func camelCaseSnake(s string) string {
	parts := strings.Split(s, "_")
	if len(parts) == 1 {
		return parts[0]
	}
	out := parts[0]
	for _, p := range parts[1:] {
		if p == "" {
			continue
		}
		out += strings.ToUpper(p[:1]) + p[1:]
	}
	return out
}

// sqliteDrizzleColumn renders one column as a Drizzle sqlite-core property.
// SQLite has only 5 storage classes; PG types collapse to TEXT or INTEGER
// with mode hints where needed.
func sqliteDrizzleColumn(c models.DataColumn) string {
	prop := camelCaseSnake(c.Name)
	col := fmt.Sprintf("%q", c.Name)
	var expr string
	switch strings.ToLower(c.Type) {
	case "text", "uuid":
		expr = fmt.Sprintf(`text(%s)`, col)
	case "integer":
		expr = fmt.Sprintf(`integer(%s)`, col)
	case "boolean":
		expr = fmt.Sprintf(`integer(%s, { mode: "boolean" })`, col)
	case "timestamp":
		expr = fmt.Sprintf(`integer(%s, { mode: "timestamp" })`, col)
	case "jsonb", "json":
		expr = fmt.Sprintf(`text(%s, { mode: "json" })`, col)
	case "numeric", "real", "double", "float":
		expr = fmt.Sprintf(`real(%s)`, col)
	default:
		expr = fmt.Sprintf(`text(%s)`, col)
	}
	if c.PrimaryKey {
		expr += ".primaryKey()"
	}
	if !c.Nullable && !c.PrimaryKey {
		expr += ".notNull()"
	}
	return fmt.Sprintf("  %s: %s,", prop, expr)
}

// RenderSqliteSchema produces the body of mobile/lib/schema.ts — Drizzle
// sqliteTable definitions for each input table.
func RenderSqliteSchema(tables []models.DataModel) string {
	var b strings.Builder
	b.WriteString(`import { sqliteTable, text, integer, real } from "drizzle-orm/sqlite-core";` + "\n\n")
	for _, t := range tables {
		fmt.Fprintf(&b, "export const %s = sqliteTable(%q, {\n", camelCaseSnake(t.Name), t.Name)
		for _, c := range t.Columns {
			b.WriteString(sqliteDrizzleColumn(c))
			b.WriteString("\n")
		}
		b.WriteString("});\n\n")
	}
	return b.String()
}

// sqliteRawColumnDecl renders a SQLite CREATE TABLE column declaration.
func sqliteRawColumnDecl(c models.DataColumn) string {
	var sqliteType string
	switch strings.ToLower(c.Type) {
	case "integer", "boolean", "timestamp":
		sqliteType = "INTEGER"
	case "numeric", "real", "double", "float":
		sqliteType = "REAL"
	default:
		sqliteType = "TEXT"
	}
	parts := []string{fmt.Sprintf("%q", c.Name), sqliteType}
	if c.PrimaryKey {
		parts = append(parts, "PRIMARY KEY")
	} else if !c.Nullable {
		parts = append(parts, "NOT NULL")
	}
	return strings.Join(parts, " ")
}

// RenderSqliteMigrations produces the body of mobile/lib/migrations.ts — a
// runSync(db) function that issues idempotent CREATE TABLE IF NOT EXISTS for
// each input table. One db.runSync call per table — single SQLite statement
// per call, no embedded newlines, so the template literal stays simple.
func RenderSqliteMigrations(tables []models.DataModel) string {
	var b strings.Builder
	b.WriteString("import type { SQLiteDatabase } from \"expo-sqlite\";\n\n")
	b.WriteString("// runSync applies every CREATE TABLE IF NOT EXISTS / ALTER TABLE statement\n")
	b.WriteString("// the app needs at startup. Idempotent — safe to call on every launch.\n")
	b.WriteString("//\n")
	b.WriteString("// When you add a column or a new table, append a fresh db.runSync(...) line.\n")
	b.WriteString("// The next launch picks it up automatically. Don't reorder or rewrite\n")
	b.WriteString("// existing statements — older builds in the wild rely on the stable order.\n")
	b.WriteString("export function runSync(db: SQLiteDatabase) {\n")
	for _, t := range tables {
		b.WriteString("  db.runSync(`")
		fmt.Fprintf(&b, "CREATE TABLE IF NOT EXISTS %q (", t.Name)
		for i, c := range t.Columns {
			b.WriteString(sqliteRawColumnDecl(c))
			if i < len(t.Columns)-1 {
				b.WriteString(", ")
			}
		}
		b.WriteString(")`);\n")
	}
	b.WriteString("}\n")
	return b.String()
}
