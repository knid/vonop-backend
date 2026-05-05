package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/knid/wemob/internal/models"
)

// ProvisionLocalDatabaseTool wires an on-device SQLite database into the
// mobile/ subtree of a mobile_app_native project. Sibling of
// ProvisionDatabaseTool — same `tables` argument, completely different
// runtime: data lives on the user's device, never reaches the network,
// and there is no server-side credential.
//
// Gated to mobile_app_native at registration time; Execute double-checks.
type ProvisionLocalDatabaseTool struct {
	Deps *RunDependencies
}

func (ProvisionLocalDatabaseTool) Name() string { return "provision_local_database" }

func (ProvisionLocalDatabaseTool) Description() string {
	return "Create an on-device SQLite database for a native mobile app (offline-first apps, instant local reads, journaling/calculator/checklist apps with no server). Provide a `tables` array (same shape as provision_database); the tool writes mobile/lib/db.ts (Drizzle expo-sqlite client + DatabaseProvider), mobile/lib/schema.ts (Drizzle sqliteTable defs), mobile/lib/migrations.ts (idempotent CREATE TABLE IF NOT EXISTS run on every launch), and adds expo-sqlite + drizzle-orm to mobile/package.json. The data lives on the user's device and never reaches the network — for shared / multi-device data use `provision_database` instead. Native projects only."
}

func (ProvisionLocalDatabaseTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"tables": map[string]any{
				"type":        "array",
				"description": "Tables to create on-device. Same shape as provision_database.tables. Always include an id primary key column. Boolean columns map to SQLite INTEGER with mode='boolean'; timestamp columns to INTEGER with mode='timestamp'; jsonb to TEXT with mode='json'.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name": map[string]any{"type": "string", "description": "snake_case table name"},
						"columns": map[string]any{
							"type": "array",
							"items": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"name":        map[string]any{"type": "string"},
									"type":        map[string]any{"type": "string", "description": "text, integer, boolean, timestamp, uuid, jsonb, numeric"},
									"primary_key": map[string]any{"type": "boolean"},
									"nullable":    map[string]any{"type": "boolean"},
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

func (p ProvisionLocalDatabaseTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	if rc.Project == nil || rc.Project.ProjectType != models.ProjectTypeMobileAppNative {
		return "provision_local_database is only available on native mobile projects. For server-side data, use provision_database.", true, nil
	}

	if rc.Project.Settings != nil {
		if v, ok := rc.Project.Settings["has_local_database"].(bool); ok && v {
			return "this project already has an on-device SQLite database wired — use the existing schema (edit mobile/lib/schema.ts and mobile/lib/migrations.ts to add tables/columns) and import { db } from '@/lib/db' from screens; do not call provision_local_database again", true, nil
		}
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

	if p.Deps == nil || p.Deps.EnableLocalDatabase == nil {
		return "local database provisioning is not available in this environment", true, nil
	}

	dbTS := localDatabaseClientTS
	rc.FileMap.Set("mobile/lib/db.ts", dbTS)
	rc.Emitter.Emit(EventFileWritten, FileWrittenEvent{
		RunID: rc.RunID, Path: "mobile/lib/db.ts", Operation: "create",
		Bytes: len(dbTS), Content: dbTS,
	})

	schema := RenderSqliteSchema(args.Tables)
	rc.FileMap.Set("mobile/lib/schema.ts", schema)
	rc.Emitter.Emit(EventFileWritten, FileWrittenEvent{
		RunID: rc.RunID, Path: "mobile/lib/schema.ts", Operation: "create",
		Bytes: len(schema), Content: schema,
	})

	migrations := RenderSqliteMigrations(args.Tables)
	rc.FileMap.Set("mobile/lib/migrations.ts", migrations)
	rc.Emitter.Emit(EventFileWritten, FileWrittenEvent{
		RunID: rc.RunID, Path: "mobile/lib/migrations.ts", Operation: "create",
		Bytes: len(migrations), Content: migrations,
	})

	addDependenciesToPackageJSONAt(rc, "mobile/package.json", map[string]string{
		"expo-sqlite": "~16.0.0",
		"drizzle-orm": "^0.39.0",
	})

	if err := p.Deps.EnableLocalDatabase(ctx, rc.Project); err != nil {
		return fmt.Sprintf("persisting local-database flag failed: %v", err), true, nil
	}

	tableNames := make([]string, 0, len(args.Tables))
	for _, t := range args.Tables {
		tableNames = append(tableNames, t.Name)
	}
	first := tableNames[0]
	return fmt.Sprintf(localDatabaseResultTemplate, strings.Join(tableNames, ", "), first, first, first, first), false, nil
}

// localDatabaseClientTS is mobile/lib/db.ts. The DatabaseProvider runs
// migrations on first render and exposes the Drizzle client through context;
// useDb returns the typed client. Screens use useLiveQuery for reactive reads.
const localDatabaseClientTS = `import { drizzle } from "drizzle-orm/expo-sqlite";
import * as SQLite from "expo-sqlite";
import { createContext, useContext, useEffect, useState, type ReactNode } from "react";

import { runSync } from "@/lib/migrations";
import * as schema from "@/lib/schema";

const sqlite = SQLite.openDatabaseSync("app.db");
export const db = drizzle(sqlite, { schema });

const DatabaseContext = createContext<typeof db | null>(null);

export function DatabaseProvider({ children }: { children: ReactNode }) {
  const [ready, setReady] = useState(false);
  useEffect(() => {
    try {
      runSync(sqlite);
    } catch (err) {
      // Migration failures shouldn't crash the app — log and continue. The
      // app may render with missing tables; user-visible errors will surface
      // on the first query.
      console.warn("[db] runSync failed", err);
    }
    setReady(true);
  }, []);
  if (!ready) return null;
  return <DatabaseContext.Provider value={db}>{children}</DatabaseContext.Provider>;
}

export function useDb() {
  const ctx = useContext(DatabaseContext);
  if (!ctx) throw new Error("useDb must be called inside DatabaseProvider");
  return ctx;
}
`

const localDatabaseResultTemplate = `On-device storage is ready. Tables created in mobile/lib/schema.ts and mobile/lib/migrations.ts: %s.

What is now true:
- mobile/lib/db.ts has been written. Import the client with:
    import { db } from '@/lib/db';
- mobile/lib/schema.ts has been written with Drizzle sqliteTable definitions.
- mobile/lib/migrations.ts has been written. Migrations run automatically on every launch (CREATE TABLE IF NOT EXISTS — idempotent).
- mobile/package.json now lists expo-sqlite and drizzle-orm.

YOU MUST add the DatabaseProvider to mobile/app/_layout.tsx as the innermost wrapper so all screens can read/write data. Edit _layout.tsx and wrap the existing tree:

  import { DatabaseProvider } from '@/lib/db';

  return (
    <QueryClientProvider client={queryClient}>
      <SafeAreaProvider>
        <GestureHandlerRootView style={{ flex: 1 }}>
          <DatabaseProvider>
            <Stack>{/* existing stack screens */}</Stack>
          </DatabaseProvider>
        </GestureHandlerRootView>
      </SafeAreaProvider>
    </QueryClientProvider>
  );

How to use it in screens:

1. Reactive reads — replaces useEffect + setState patterns:
    import { useLiveQuery } from "drizzle-orm/expo-sqlite";
    import { db } from "@/lib/db";
    import { %s } from "@/lib/schema";

    export default function Screen() {
      const { data } = useLiveQuery(db.select().from(%s));
      return <FlatList data={data} renderItem={({ item }) => <Text>{item.title}</Text>} />;
    }

2. Writes — from event handlers, NEVER inside render:
    import { db } from "@/lib/db";
    import { %s } from "@/lib/schema";

    async function addItem(title: string) {
      await db.insert(%s).values({ title });
    }

Rules:
1. mobile/lib/db.ts MUST NOT be imported from anything under backend/. The
   SQLite handle is device-only — the backend has no access to it.
2. Never persist auth tokens or other secrets in this database — use
   expo-secure-store (already in the Expo template) for those.
3. Schema changes go in mobile/lib/schema.ts AND mobile/lib/migrations.ts —
   append a CREATE TABLE IF NOT EXISTS / ALTER TABLE … ADD COLUMN line.
   Don't call provision_local_database again.
4. In every user-facing message, call this "your data" / "this app's storage" /
   "veritabanın". NEVER say SQLite, expo-sqlite, or Drizzle to the user.

Next steps for this run:
1. Edit mobile/app/_layout.tsx to wrap with <DatabaseProvider>.
2. Replace any hardcoded mock data in screens with useLiveQuery + db.insert/update/delete.
3. If the user asked for cross-device sync, also call provision_database to add a backend Postgres copy.`
