package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/templates"
)

const genericProvisionDatabaseResultTemplate = `Wemob db is ready. Tables created and seeded with realistic demo data (including AI-generated images for any image columns): %s.

What is now true:
- lib/db.ts has been written. Import the database client with: import { db } from '@/lib/db'
- You MUST now write lib/schema.ts yourself with Drizzle pgTable definitions matching every table above. lib/db.ts imports './schema', so the file must exist before any page can build. Use snake_case column names in the SQL but camelCase property names in the Drizzle definitions.
- The execute_sql tool now works against this database. Use readonly=true for one-off SELECTs while exploring, readonly=false for ALTER TABLE and any extra INSERTs you need.
- Pages that read from @/lib/db must export: export const dynamic = "force-dynamic"
- In every user-facing message, call this "your database" / "wemob db" / "veritabanın". NEVER say Neon, Postgres, Drizzle, Supabase, or Firebase to the user.

Next steps for this run:
1. write_file lib/schema.ts with Drizzle pgTable definitions matching the schema you just provisioned.
2. Replace any hardcoded mock data in pages and components with real db.select / db.insert calls.
3. Add export const dynamic = "force-dynamic" to every page.tsx that reads from the DB.`

// mobileNativeProvisionDatabaseResultTemplate is returned by ProvisionDatabaseTool.Execute when the project is mobile_app_native. Teaches the API-route proxy pattern that mobile screens use to reach the backend Postgres database.
const mobileNativeProvisionDatabaseResultTemplate = `Wemob db is ready. Tables created and seeded with realistic demo data (including AI-generated images for any image columns): %s.

What is now true:
- backend/lib/db.ts has been written. Import the database client from a Next.js API route with:
    import { db } from '@/lib/db';
- backend/package.json now lists @neondatabase/serverless and drizzle-orm. A minimal Next.js backend was scaffolded under backend/ if it didn't already exist.
- You MUST now write backend/lib/schema.ts with Drizzle pgTable definitions matching every table above. backend/lib/db.ts imports './schema', so the file must exist before any backend page can build. Use snake_case column names in the SQL but camelCase property names in the Drizzle definitions.
- The execute_sql tool now works against this database. Use readonly=true for SELECTs while exploring, readonly=false for ALTER TABLE / extra INSERTs.

How CRUD reaches the mobile app:

  CRITICAL ARCHITECTURE: backend/lib/db.ts MUST only be imported from Next.js
  API routes under backend/app/api/**/route.ts. The mobile app (mobile/) NEVER
  imports it — DATABASE_URL is server-side-only and must never reach the
  React Native binary.

1. Backend route (backend/app/api/<resource>/route.ts):
    import { db } from '@/lib/db';
    import { %s } from '@/lib/schema';
    import { NextRequest, NextResponse } from 'next/server';
    export async function GET() {
      const rows = await db.select().from(%s);
      return NextResponse.json(rows);
    }
    export async function POST(req: NextRequest) {
      const body = await req.json();
      const [row] = await db.insert(%s).values(body).returning();
      return NextResponse.json(row);
    }

2. Mobile screen (mobile/app/<route>.tsx):
    import { apiFetch } from '@/lib/api';
    const rows = await apiFetch<Item[]>('/api/<resource>');

Rules:
1. NEVER import '@/lib/db' from anything under mobile/. NEVER add DATABASE_URL
   to mobile/ source. NEVER expose connection strings through API responses.
2. Backend API responses must shape the data — strip secrets, derive computed
   fields, paginate. The route handler is the trust boundary.
3. In every user-facing message, call this "your database" / "wemob db" /
   "veritabanın". NEVER say Neon, Postgres, Drizzle, Supabase, or Firebase
   to the user.

Next steps for this run:
1. write_file backend/lib/schema.ts with Drizzle pgTable definitions matching the schema you just provisioned.
2. write_file backend/app/api/<resource>/route.ts handlers for each table the UI needs (GET list, POST create, optionally PUT/DELETE on /<id>).
3. Wire mobile screens to call those routes via apiFetch instead of any hardcoded mock data.`

// ProvisionDatabaseTool is the agent-callable entry point for creating the
// project's wemob db on demand. It is registered unconditionally; the tool
// internally rejects calls when a database already exists for the project so
// the model never accidentally provisions twice.
type ProvisionDatabaseTool struct {
	Deps *RunDependencies
}

func (ProvisionDatabaseTool) Name() string { return "provision_database" }

func (ProvisionDatabaseTool) Description() string {
	return "Create the project's wemob database. Call this exactly once when the user asks for a database without naming a specific provider. Provide a `tables` array (each table needs a name and a columns array); the tool creates the database, runs CREATE TABLE for each table, seeds it with realistic demo data (including AI-generated images for image columns), writes the database client (`lib/db.ts` for web projects, `backend/lib/db.ts` for native mobile projects — the latter scaffolds a minimal Next.js backend if `backend/` doesn't exist yet), and unlocks the execute_sql tool. Do NOT call this if the user explicitly named a third-party provider (Supabase, Firebase, etc.) — use those integrations directly instead. Do NOT call this on ecommerce projects — their canonical schema is seeded automatically at project creation time. For on-device SQLite (offline-first apps with no backend), use `provision_local_database` instead — that tool is only available on native mobile projects."
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
		"required": []string{"tables"},
	}
}

func (p ProvisionDatabaseTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	// Ecommerce projects get their DB provisioned at project-creation time
	// with the canonical starter schema. There is no scenario where the model
	// should call provision_database for an ecommerce project.
	if rc.Project != nil && rc.Project.ProjectType == "ecommerce" {
		return "ecommerce projects have their database seeded automatically at project creation — do not call provision_database. Use execute_sql for any schema changes and import { db } from '@/lib/db' for compiled queries.", true, nil
	}

	var args struct {
		Tables []models.DataModel `json:"tables"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return fmt.Sprintf("invalid input: %v", err), true, nil
	}
	if len(args.Tables) == 0 {
		return "tables is required — provide at least one table with an id primary key column", true, nil
	}
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

	isMobileNative := rc.Project != nil && rc.Project.ProjectType == models.ProjectTypeMobileAppNative
	libDBPath := "lib/db.ts"
	pkgJSONPath := "package.json"
	resultTemplate := genericProvisionDatabaseResultTemplate
	if isMobileNative {
		libDBPath = "backend/lib/db.ts"
		pkgJSONPath = "backend/package.json"
		resultTemplate = mobileNativeProvisionDatabaseResultTemplate
		if _, ok := rc.FileMap.Get(pkgJSONPath); !ok {
			scaffoldMinimalBackend(rc)
		}
	}

	if libDB := templates.GetTemplate("lib/db.ts"); libDB != "" {
		rc.FileMap.Set(libDBPath, libDB)
		rc.Emitter.Emit(EventFileWritten, FileWrittenEvent{
			RunID:     rc.RunID,
			Path:      libDBPath,
			Operation: "create",
			Bytes:     len(libDB),
			Content:   libDB,
		})
	}

	addDependenciesToPackageJSONAt(rc, pkgJSONPath, map[string]string{
		"@neondatabase/serverless": "^0.10.0",
		"drizzle-orm":              "^0.39.0",
	})

	tableNames := make([]string, 0, len(args.Tables))
	for _, t := range args.Tables {
		tableNames = append(tableNames, t.Name)
	}

	var result string
	if isMobileNative {
		first := tableNames[0]
		firstCamel := camelCaseSnake(first)
		result = fmt.Sprintf(resultTemplate, strings.Join(tableNames, ", "), firstCamel, firstCamel, firstCamel)
	} else {
		result = fmt.Sprintf(resultTemplate, strings.Join(tableNames, ", "))
	}

	return result, false, nil
}
