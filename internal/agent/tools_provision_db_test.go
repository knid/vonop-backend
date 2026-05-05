package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
)

// fakeRunContextForDB builds a RunContext for tool-execute unit tests with
// the file map seeded from the caller. Reused by provision-local-db tests.
func fakeRunContextForDB(t *testing.T, project *models.AppProject, seed map[string]string) *RunContext {
	t.Helper()
	if project.ID == uuid.Nil {
		project.ID = uuid.New()
	}
	return &RunContext{
		RunID:   uuid.New(),
		Project: project,
		FileMap: NewSafeFileMap(seed),
		Emitter: noopEmitter{},
	}
}

func TestProvisionDatabase_NativeWritesBackendPaths(t *testing.T) {
	project := &models.AppProject{
		ProjectType: models.ProjectTypeMobileAppNative,
		Settings:    map[string]any{},
	}
	rc := fakeRunContextForDB(t, project, map[string]string{
		"mobile/package.json": `{"name":"mobile","dependencies":{}}`,
	})
	deps := &RunDependencies{
		ProvisionDatabase: func(_ context.Context, _ *models.AppProject, _ []models.DataModel) error { return nil },
		NeonConnString:    func(_ string) (string, error) { return "", nil },
	}
	tool := ProvisionDatabaseTool{Deps: deps}

	input, _ := json.Marshal(map[string]any{
		"tables": []map[string]any{{
			"name": "todos",
			"columns": []map[string]any{
				{"name": "id", "type": "uuid", "primary_key": true},
				{"name": "title", "type": "text"},
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

	if _, ok := rc.FileMap.Get("backend/lib/db.ts"); !ok {
		t.Error("expected backend/lib/db.ts to be written")
	}
	if _, ok := rc.FileMap.Get("backend/package.json"); !ok {
		t.Error("expected backend/package.json to be written (auto-scaffold)")
	}
	if _, ok := rc.FileMap.Get("backend/tsconfig.json"); !ok {
		t.Error("expected backend/tsconfig.json (auto-scaffold)")
	}

	pkg, _ := rc.FileMap.Get("backend/package.json")
	if !strings.Contains(pkg, "@neondatabase/serverless") {
		t.Error("expected @neondatabase/serverless in backend/package.json deps")
	}
	if !strings.Contains(pkg, "drizzle-orm") {
		t.Error("expected drizzle-orm in backend/package.json deps")
	}

	if _, ok := rc.FileMap.Get("lib/db.ts"); ok {
		t.Error("native run must NOT write root lib/db.ts")
	}

	if !strings.Contains(result, "API route") && !strings.Contains(result, "backend/app/api") {
		t.Error("expected native result to mention API routes")
	}
	if strings.Contains(result, "force-dynamic") {
		t.Error("native result must NOT mention force-dynamic")
	}
}

func TestProvisionDatabase_NativeReusesExistingBackendPackageJSON(t *testing.T) {
	project := &models.AppProject{
		ProjectType: models.ProjectTypeMobileAppNative,
		Settings:    map[string]any{},
	}
	existingPkg := `{"name":"backend","dependencies":{"next":"^15.0.0","react":"^19.0.0","react-dom":"^19.0.0","custom-dep":"^1.0.0"}}`
	rc := fakeRunContextForDB(t, project, map[string]string{"backend/package.json": existingPkg})
	deps := &RunDependencies{
		ProvisionDatabase: func(_ context.Context, _ *models.AppProject, _ []models.DataModel) error { return nil },
		NeonConnString:    func(_ string) (string, error) { return "", nil },
	}
	tool := ProvisionDatabaseTool{Deps: deps}

	input, _ := json.Marshal(map[string]any{
		"tables": []map[string]any{{"name": "items", "columns": []map[string]any{{"name": "id", "type": "uuid"}}}},
	})
	if _, _, err := tool.Execute(context.Background(), rc, input); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pkg, _ := rc.FileMap.Get("backend/package.json")
	if !strings.Contains(pkg, "custom-dep") {
		t.Error("scaffold must NOT overwrite an existing backend/package.json")
	}
	if !strings.Contains(pkg, "@neondatabase/serverless") {
		t.Error("expected @neondatabase/serverless added")
	}
	if !strings.Contains(pkg, "drizzle-orm") {
		t.Error("expected drizzle-orm added")
	}
}

func TestProvisionDatabase_RejectsEmptyTablesNative(t *testing.T) {
	project := &models.AppProject{
		ProjectType: models.ProjectTypeMobileAppNative,
		Settings:    map[string]any{},
	}
	rc := fakeRunContextForDB(t, project, nil)
	tool := ProvisionDatabaseTool{Deps: &RunDependencies{
		ProvisionDatabase: func(_ context.Context, _ *models.AppProject, _ []models.DataModel) error { return nil },
		NeonConnString:    func(_ string) (string, error) { return "", nil },
	}}
	input, _ := json.Marshal(map[string]any{"tables": []any{}})
	result, isErr, _ := tool.Execute(context.Background(), rc, input)
	if !isErr {
		t.Fatalf("expected isError=true on empty tables, got result=%q", result)
	}
}

func TestProvisionDatabase_WebProjectUnchanged(t *testing.T) {
	project := &models.AppProject{ProjectType: models.ProjectTypeApp, Settings: map[string]any{}}
	rc := fakeRunContextForDB(t, project, map[string]string{"package.json": `{"name":"app","dependencies":{}}`})
	deps := &RunDependencies{
		ProvisionDatabase: func(_ context.Context, _ *models.AppProject, _ []models.DataModel) error { return nil },
		NeonConnString:    func(_ string) (string, error) { return "", nil },
	}
	tool := ProvisionDatabaseTool{Deps: deps}

	input, _ := json.Marshal(map[string]any{
		"tables": []map[string]any{{"name": "posts", "columns": []map[string]any{{"name": "id", "type": "uuid"}}}},
	})
	result, _, _ := tool.Execute(context.Background(), rc, input)

	if _, ok := rc.FileMap.Get("lib/db.ts"); !ok {
		t.Error("web project should write root lib/db.ts")
	}
	if _, ok := rc.FileMap.Get("backend/lib/db.ts"); ok {
		t.Error("web project must NOT write backend/lib/db.ts")
	}
	if !strings.Contains(result, "force-dynamic") {
		t.Error("web result should still mention force-dynamic")
	}
}
