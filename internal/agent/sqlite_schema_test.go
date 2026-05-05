package agent

import (
	"strings"
	"testing"

	"github.com/knid/wemob/internal/models"
)

func TestRenderSqliteSchema_AllSupportedTypes(t *testing.T) {
	tables := []models.DataModel{{
		Name: "items",
		Columns: []models.DataColumn{
			{Name: "id", Type: "uuid", PrimaryKey: true},
			{Name: "title", Type: "text"},
			{Name: "count", Type: "integer"},
			{Name: "is_done", Type: "boolean"},
			{Name: "created_at", Type: "timestamp"},
			{Name: "metadata", Type: "jsonb"},
			{Name: "price", Type: "numeric"},
		},
	}}

	out := RenderSqliteSchema(tables)

	wantContains := []string{
		`import { sqliteTable, text, integer, real } from "drizzle-orm/sqlite-core"`,
		`export const items = sqliteTable("items"`,
		`id: text("id").primaryKey()`,
		`title: text("title")`,
		`count: integer("count")`,
		`isDone: integer("is_done", { mode: "boolean" })`,
		`createdAt: integer("created_at", { mode: "timestamp" })`,
		`metadata: text("metadata", { mode: "json" })`,
		`price: real("price")`,
	}
	for _, s := range wantContains {
		if !strings.Contains(out, s) {
			t.Errorf("expected output to contain %q\n--- output ---\n%s", s, out)
		}
	}
}

func TestRenderSqliteMigrations_GeneratesIdempotentCreateTable(t *testing.T) {
	tables := []models.DataModel{{
		Name: "items",
		Columns: []models.DataColumn{
			{Name: "id", Type: "uuid", PrimaryKey: true},
			{Name: "title", Type: "text"},
		},
	}}
	out := RenderSqliteMigrations(tables)
	if !strings.Contains(out, `CREATE TABLE IF NOT EXISTS "items"`) {
		t.Error("expected idempotent CREATE TABLE IF NOT EXISTS")
	}
	if !strings.Contains(out, `"id" TEXT PRIMARY KEY`) {
		t.Error("expected primary key column")
	}
	if !strings.Contains(out, `"title" TEXT`) {
		t.Error("expected text column")
	}
}
