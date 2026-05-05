package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/knid/wemob/internal/models"
)

func TestProvisionLocalDatabase_NativeWritesMobilePaths(t *testing.T) {
	project := &models.AppProject{
		ProjectType: models.ProjectTypeMobileAppNative,
		Settings:    map[string]any{},
	}
	rc := fakeRunContextForDB(t, project, map[string]string{
		"mobile/package.json": `{"name":"mobile","dependencies":{"expo":"~54.0.0"}}`,
	})
	called := false
	deps := &RunDependencies{
		EnableLocalDatabase: func(_ context.Context, p *models.AppProject) error {
			called = true
			if p.Settings == nil {
				p.Settings = map[string]any{}
			}
			p.Settings["has_local_database"] = true
			return nil
		},
	}
	tool := ProvisionLocalDatabaseTool{Deps: deps}

	input, _ := json.Marshal(map[string]any{
		"tables": []map[string]any{{
			"name": "todos",
			"columns": []map[string]any{
				{"name": "id", "type": "uuid", "primary_key": true},
				{"name": "title", "type": "text"},
				{"name": "is_done", "type": "boolean"},
			},
		}},
	})
	result, isErr, err := tool.Execute(context.Background(), rc, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if isErr {
		t.Fatalf("unexpected isError=true: %s", result)
	}
	if !called {
		t.Error("expected EnableLocalDatabase callback to be invoked")
	}

	for _, p := range []string{"mobile/lib/db.tsx", "mobile/lib/schema.ts", "mobile/lib/migrations.ts"} {
		if _, ok := rc.FileMap.Get(p); !ok {
			t.Errorf("expected %s to be written", p)
		}
	}

	pkg, _ := rc.FileMap.Get("mobile/package.json")
	if !strings.Contains(pkg, "expo-sqlite") {
		t.Error("expected expo-sqlite in mobile/package.json")
	}
	if !strings.Contains(pkg, "drizzle-orm") {
		t.Error("expected drizzle-orm in mobile/package.json")
	}

	schema, _ := rc.FileMap.Get("mobile/lib/schema.ts")
	if !strings.Contains(schema, `from "drizzle-orm/sqlite-core"`) {
		t.Error("schema should import from drizzle-orm/sqlite-core")
	}
	if !strings.Contains(schema, `isDone: integer("is_done", { mode: "boolean" })`) {
		t.Error("expected boolean column rendered as integer with boolean mode")
	}

	if !strings.Contains(result, "useLiveQuery") {
		t.Error("expected result text to teach useLiveQuery")
	}
	if !strings.Contains(result, "DatabaseProvider") {
		t.Error("expected result text to mention DatabaseProvider in _layout.tsx")
	}
}

func TestProvisionLocalDatabase_MultiWordTableUsesCamelCaseInResult(t *testing.T) {
	project := &models.AppProject{
		ProjectType: models.ProjectTypeMobileAppNative,
		Settings:    map[string]any{},
	}
	rc := fakeRunContextForDB(t, project, map[string]string{
		"mobile/package.json": `{"name":"mobile","dependencies":{}}`,
	})
	deps := &RunDependencies{
		EnableLocalDatabase: func(_ context.Context, p *models.AppProject) error {
			if p.Settings == nil {
				p.Settings = map[string]any{}
			}
			p.Settings["has_local_database"] = true
			return nil
		},
	}
	tool := ProvisionLocalDatabaseTool{Deps: deps}
	input, _ := json.Marshal(map[string]any{
		"tables": []map[string]any{{
			"name":    "user_profiles",
			"columns": []map[string]any{{"name": "id", "type": "uuid", "primary_key": true}},
		}},
	})
	result, isErr, _ := tool.Execute(context.Background(), rc, input)
	if isErr {
		t.Fatalf("unexpected isError: %s", result)
	}
	if !strings.Contains(result, "import { userProfiles }") {
		t.Errorf("expected camelCase 'userProfiles' in result imports, got:\n%s", result)
	}
	if strings.Contains(result, "import { user_profiles }") {
		t.Errorf("result should NOT use snake_case in imports")
	}
	if !strings.Contains(result, ": user_profiles.") {
		// Tables-created header should keep raw SQL name
		t.Errorf("expected 'user_profiles' in tables-created header (raw SQL name)")
	}
}

func TestProvisionLocalDatabase_RejectsWebProject(t *testing.T) {
	project := &models.AppProject{ProjectType: models.ProjectTypeApp, Settings: map[string]any{}}
	rc := fakeRunContextForDB(t, project, nil)
	tool := ProvisionLocalDatabaseTool{Deps: &RunDependencies{}}

	input, _ := json.Marshal(map[string]any{
		"tables": []map[string]any{{"name": "x", "columns": []map[string]any{{"name": "id", "type": "uuid"}}}},
	})
	result, isErr, _ := tool.Execute(context.Background(), rc, input)
	if !isErr {
		t.Fatalf("expected isError=true on web project, got result=%q", result)
	}
	if rc.FileMap.Len() != 0 {
		t.Error("expected no file writes on rejection")
	}
}

func TestProvisionLocalDatabase_RejectsAlreadyProvisioned(t *testing.T) {
	project := &models.AppProject{
		ProjectType: models.ProjectTypeMobileAppNative,
		Settings:    map[string]any{"has_local_database": true},
	}
	rc := fakeRunContextForDB(t, project, nil)
	tool := ProvisionLocalDatabaseTool{Deps: &RunDependencies{
		EnableLocalDatabase: func(_ context.Context, _ *models.AppProject) error {
			t.Fatal("EnableLocalDatabase should not be invoked when already provisioned")
			return nil
		},
	}}

	input, _ := json.Marshal(map[string]any{
		"tables": []map[string]any{{"name": "x", "columns": []map[string]any{{"name": "id", "type": "uuid"}}}},
	})
	result, isErr, _ := tool.Execute(context.Background(), rc, input)
	if !isErr {
		t.Fatalf("expected isError=true on already-provisioned, got result=%q", result)
	}
}
