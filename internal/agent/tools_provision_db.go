package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/templates"
)

// ProvisionDatabaseTool is the agent-callable entry point for creating the
// project's wemob db on demand. It is registered unconditionally; the tool
// internally rejects calls when a database already exists for the project so
// the model never accidentally provisions twice.
type ProvisionDatabaseTool struct {
	Deps *RunDependencies
}

func (ProvisionDatabaseTool) Name() string { return "provision_database" }

func (ProvisionDatabaseTool) Description() string {
	return "Create the project's wemob database. Call this exactly once when the user asks for a database without naming a specific provider. For most projects, provide a `tables` array (each table needs a name and a columns array); the tool creates the database, runs CREATE TABLE for each table, seeds it with realistic demo data (including AI-generated images for image columns), writes lib/db.ts, and unlocks the execute_sql tool. For ecommerce projects, call this with NO tables argument — the canonical ecommerce schema (stores, products, product_categories, discounts, carts, cart_items, orders, order_items, product_reviews, wishlists) is seeded automatically and any tables you pass are ignored. Do NOT call this if the user explicitly named a third-party provider (Supabase, Firebase, etc.) — use those integrations directly instead."
}

func (ProvisionDatabaseTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"tables": map[string]any{
				"type":        "array",
				"description": "Database tables to create. Infer from the user's request and the existing UI (e.g. for a blog: posts, categories, authors; for a todo app: tasks, lists; for a contact form: submissions). Always include an id primary key column.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name": map[string]any{
							"type":        "string",
							"description": "snake_case table name, e.g. 'posts' or 'order_items'",
						},
						"columns": map[string]any{
							"type": "array",
							"items": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"name": map[string]any{"type": "string"},
									"type": map[string]any{
										"type":        "string",
										"description": "PostgreSQL type: text, integer, boolean, timestamp, uuid, jsonb, numeric",
									},
									"primary_key": map[string]any{"type": "boolean"},
									"nullable":    map[string]any{"type": "boolean"},
									"default": map[string]any{
										"type":        "string",
										"description": "SQL default expression, e.g. 'gen_random_uuid()' or 'NOW()'",
									},
									"references": map[string]any{
										"type":        "string",
										"description": "Foreign key target, e.g. 'users.id'",
									},
								},
								"required": []string{"name", "type"},
							},
						},
					},
					"required": []string{"name", "columns"},
				},
			},
		},
	}
}

func (p ProvisionDatabaseTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var args struct {
		Tables []models.DataModel `json:"tables"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return fmt.Sprintf("invalid input: %v", err), true, nil
	}
	// Empty tables is valid for ecommerce projects (canonical schema seeded
	// automatically). For other projects the controller layer below will
	// reject empty and return a descriptive error.
	for i, t := range args.Tables {
		if strings.TrimSpace(t.Name) == "" {
			return fmt.Sprintf("table[%d] is missing a name", i), true, nil
		}
		if len(t.Columns) == 0 {
			return fmt.Sprintf("table %q has no columns — at least an id primary key is required", t.Name), true, nil
		}
	}

	if p.Deps == nil || p.Deps.ProvisionDatabase == nil {
		return "database provisioning is not available in this environment", true, nil
	}

	// Reject if a wemob db already exists — the model should write code
	// against the existing @/lib/db and use execute_sql for ad-hoc queries.
	if p.Deps.NeonConnString != nil {
		if cs, _ := p.Deps.NeonConnString(rc.Project.ID.String()); cs != "" {
			return "this project already has a wemob db connected — import { db } from '@/lib/db' and use execute_sql for ad-hoc queries; do not call provision_database again", true, nil
		}
	}

	if err := p.Deps.ProvisionDatabase(ctx, rc.Project, args.Tables); err != nil {
		return fmt.Sprintf("provisioning failed: %v", err), true, nil
	}

	// Inject lib/db.ts so the model can `import { db } from '@/lib/db'`
	// immediately within the same run. lib/schema.ts is left for the model
	// to author so the Drizzle types match the live tables it just designed.
	if libDB := templates.GetTemplate("lib/db.ts"); libDB != "" {
		rc.FileMap.Set("lib/db.ts", libDB)
		rc.Emitter.Emit(EventFileWritten, FileWrittenEvent{
			RunID:     rc.RunID,
			Path:      "lib/db.ts",
			Operation: "create",
			Bytes:     len(libDB),
			Content:   libDB,
		})
	}

	// Add the packages lib/db.ts imports to package.json. The LLM never
	// wrote these imports, so it won't add them itself.
	addDependenciesToPackageJSON(rc, map[string]string{
		"@neondatabase/serverless": "^0.10.0",
		"drizzle-orm":              "^0.39.0",
	})

	tableNames := make([]string, 0, len(args.Tables))
	for _, t := range args.Tables {
		tableNames = append(tableNames, t.Name)
	}

	result := fmt.Sprintf(`Wemob db is ready. Tables created and seeded with realistic demo data (including AI-generated images for any image columns): %s.

What is now true:
- lib/db.ts has been written. Import the database client with: import { db } from '@/lib/db'
- You MUST now write lib/schema.ts yourself with Drizzle pgTable definitions matching every table above. lib/db.ts imports './schema', so the file must exist before any page can build. Use snake_case column names in the SQL but camelCase property names in the Drizzle definitions.
- The execute_sql tool now works against this database. Use readonly=true for one-off SELECTs while exploring, readonly=false for ALTER TABLE and any extra INSERTs you need.
- Pages that read from @/lib/db must export: export const dynamic = "force-dynamic"
- In every user-facing message, call this "your database" / "wemob db" / "veritabanın". NEVER say Neon, Postgres, Drizzle, Supabase, or Firebase to the user.

Next steps for this run:
1. write_file lib/schema.ts with Drizzle pgTable definitions matching the schema you just provisioned.
2. Replace any hardcoded mock data in pages and components with real db.select / db.insert calls.
3. Add export const dynamic = "force-dynamic" to every page.tsx that reads from the DB.`,
		strings.Join(tableNames, ", "))

	return result, false, nil
}
