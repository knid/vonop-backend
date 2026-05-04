package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
)

// fakeGatewayModels returns a non-empty stub model list so the tool's
// "tier has no chat access" early-exit doesn't fire. We don't care about
// the exact model IDs in these tests — just that the gateway lookup
// succeeds.
func fakeGatewayModels() []string {
	return []string{"anthropic/claude-haiku-4-5", "openai/gpt-4.1-mini"}
}

func newSetupAITestContext(t *testing.T, projectType string) (*RunContext, *bool) {
	t.Helper()

	enableAICalled := false
	rc := &RunContext{
		RunID: uuid.New(),
		Project: &models.AppProject{
			ID:          uuid.New(),
			ProjectType: projectType,
			Settings:    map[string]any{},
		},
		FileMap: NewSafeFileMap(map[string]string{}),
		Emitter: noopEmitter{},
		Deps: &RunDependencies{
			EnableAI: func(_ context.Context, _ *models.AppProject) error {
				enableAICalled = true
				return nil
			},
			GatewayAllowedModels: fakeGatewayModels,
		},
	}
	return rc, &enableAICalled
}

// TestSetupAIGatewayTool_MobileNativePaths verifies that for
// mobile_app_native projects, the helper file lands at
// backend/lib/wemob-ai.ts (not lib/wemob-ai.ts) and the openai dep is
// added to backend/package.json (not the root package.json).
//
// Without this branching the AI key would be embedded into the React
// Native binary (when the agent imports @/lib/wemob-ai from a screen)
// or the dep wouldn't be installed at deploy time and the route would
// crash on first hit.
func TestSetupAIGatewayTool_MobileNativePaths(t *testing.T) {
	rc, called := newSetupAITestContext(t, models.ProjectTypeMobileAppNative)
	tool := SetupAIGatewayTool{Deps: rc.Deps}

	out, isErr, err := tool.Execute(context.Background(), rc, json.RawMessage(`{"purpose":"chatbot"}`))
	if err != nil {
		t.Fatalf("Execute returned infra error: %v", err)
	}
	if isErr {
		t.Fatalf("Execute returned tool error: %s", out)
	}

	// AI helper written under backend/.
	if _, ok := rc.FileMap.Get("backend/lib/wemob-ai.ts"); !ok {
		t.Error("expected backend/lib/wemob-ai.ts to be written for mobile_app_native")
	}
	if _, ok := rc.FileMap.Get("lib/wemob-ai.ts"); ok {
		t.Error("lib/wemob-ai.ts should NOT be written for mobile_app_native (would leak key into RN binary)")
	}

	// Backend package.json scaffolded with openai dep.
	pkg, ok := rc.FileMap.Get("backend/package.json")
	if !ok {
		t.Fatal("expected backend/package.json to be scaffolded")
	}
	if !strings.Contains(pkg, `"openai"`) {
		t.Errorf("backend/package.json missing openai dep; got: %s", pkg)
	}
	if !strings.Contains(pkg, `"next"`) {
		t.Errorf("backend/package.json missing next dep; got: %s", pkg)
	}
	if _, ok := rc.FileMap.Get("package.json"); ok {
		t.Error("root package.json should NOT be created for mobile_app_native — backend/ holds the Next.js scaffold")
	}

	// tsconfig.json + next.config.mjs scaffolded so the route is
	// build-able the moment the agent writes its first handler.
	if _, ok := rc.FileMap.Get("backend/tsconfig.json"); !ok {
		t.Error("expected backend/tsconfig.json to be scaffolded")
	}
	if _, ok := rc.FileMap.Get("backend/next.config.mjs"); !ok {
		t.Error("expected backend/next.config.mjs to be scaffolded")
	}

	// Result describes the cross-tier (mobile→backend) pattern.
	if !strings.Contains(out, "backend/lib/wemob-ai.ts") {
		t.Errorf("result missing backend/lib/wemob-ai.ts reference; got first 400 chars: %s", out[:min(400, len(out))])
	}
	if !strings.Contains(out, "apiFetch") {
		t.Errorf("result should describe the mobile→backend apiFetch pattern; got first 400 chars: %s", out[:min(400, len(out))])
	}

	if !*called {
		t.Error("EnableAI callback was not invoked")
	}
}

// TestSetupAIGatewayTool_WebProjectKeepsRootPaths is the regression
// counterpart: web projects (default project_type) MUST still land the
// helper at lib/wemob-ai.ts and the dep on root package.json. We
// pre-seed a root package.json because addDependenciesToPackageJSONAt
// no-ops when the file is missing.
func TestSetupAIGatewayTool_WebProjectKeepsRootPaths(t *testing.T) {
	rc, _ := newSetupAITestContext(t, models.ProjectTypeApp)
	rc.FileMap.Set("package.json", `{"name":"app","dependencies":{}}`)

	tool := SetupAIGatewayTool{Deps: rc.Deps}
	out, isErr, err := tool.Execute(context.Background(), rc, json.RawMessage(`{"purpose":"summarizer"}`))
	if err != nil {
		t.Fatalf("Execute returned infra error: %v", err)
	}
	if isErr {
		t.Fatalf("Execute returned tool error: %s", out)
	}

	if _, ok := rc.FileMap.Get("lib/wemob-ai.ts"); !ok {
		t.Error("expected lib/wemob-ai.ts to be written for web project")
	}
	if _, ok := rc.FileMap.Get("backend/lib/wemob-ai.ts"); ok {
		t.Error("backend/lib/wemob-ai.ts should NOT be written for web project")
	}
	pkg, _ := rc.FileMap.Get("package.json")
	if !strings.Contains(pkg, `"openai"`) {
		t.Errorf("root package.json missing openai dep; got: %s", pkg)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
