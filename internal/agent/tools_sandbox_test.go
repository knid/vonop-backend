package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/knid/wemob/internal/models"
)

func TestValidateSandboxWritePathArtifactsOnly(t *testing.T) {
	if _, err := validateSandboxWritePath("/workspace/artifacts/run/report.md"); err != nil {
		t.Fatalf("artifact path rejected: %v", err)
	}
	if _, err := validateSandboxWritePath("/tmp/report.md"); err != nil {
		t.Fatalf("tmp path rejected: %v", err)
	}
	if _, err := validateSandboxWritePath("/workspace/project/app/page.tsx"); err == nil {
		t.Fatal("expected project source write to be rejected")
	}
	if _, err := validateSandboxWritePath("/workspace/artifacts/.env"); err == nil {
		t.Fatal("expected secret-looking artifact path to be rejected")
	}
}

func TestValidateSandboxReadPathBlocksSecretsAndOutsideRoots(t *testing.T) {
	if _, err := validateSandboxReadPath("/workspace/project/package.json"); err != nil {
		t.Fatalf("project read rejected: %v", err)
	}
	if _, err := validateSandboxReadPath("/workspace/project/.env.local"); err == nil {
		t.Fatal("expected env read to be rejected")
	}
	if _, err := validateSandboxReadPath("/etc/passwd"); err == nil {
		t.Fatal("expected outside root read to be rejected")
	}
}

func TestValidateSandboxCommandBlocksRiskyCommands(t *testing.T) {
	if err := validateSandboxCommand("npm test"); err != nil {
		t.Fatalf("safe command rejected: %v", err)
	}
	for _, cmd := range []string{
		"sudo whoami",
		"apt-get update",
		"rm -rf /",
		"rm -rf /*",
		"cat /workspace/project/.env.local",
		"ngrok http 3000",
	} {
		if err := validateSandboxCommand(cmd); err == nil {
			t.Fatalf("expected command %q to be blocked", cmd)
		}
	}
}

func TestSandboxExecAllowedByDefault(t *testing.T) {
	called := false
	deps := &RunDependencies{
		SandboxControl: func(ctx context.Context, project *models.AppProject, user *models.User, req SandboxControlRequest) (map[string]any, error) {
			called = true
			if req.Action != "exec" {
				t.Fatalf("action: got %q want exec", req.Action)
			}
			if req.Command != "npm test" {
				t.Fatalf("command: got %q want npm test", req.Command)
			}
			if req.Cwd != sandboxRootProject {
				t.Fatalf("cwd: got %q want %q", req.Cwd, sandboxRootProject)
			}
			return map[string]any{"ok": true}, nil
		},
	}
	rc := &RunContext{
		Project: &models.AppProject{Settings: map[string]any{}},
		User:    &models.User{},
		Deps:    deps,
	}

	result, isErr, err := (SandboxExecTool{Deps: deps}).Execute(context.Background(), rc, json.RawMessage(`{"command":"npm test"}`))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if isErr {
		t.Fatalf("Execute returned isErr=true: %s", result)
	}
	if !called {
		t.Fatal("SandboxControl was not called")
	}
}

func TestSandboxExecSchemaDocumentsRuntimeLayout(t *testing.T) {
	schema := (SandboxExecTool{}).Schema()
	joined := fmt.Sprint(schema)

	for _, want := range []string{
		"/workspace/project",
		"/workspace/backend",
		"Set cwd",
		"do not use cd",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected sandbox_exec schema to document %q", want)
		}
	}
}

func TestArtifactToolSchemasRequireAbsoluteArtifactPaths(t *testing.T) {
	for _, schema := range []map[string]any{
		(SandboxServeArtifactTool{}).Schema(),
		(PreviewOpenArtifactTool{}).Schema(),
	} {
		joined := fmt.Sprint(schema)
		for _, want := range []string{"/workspace/artifacts", "Absolute sandbox path"} {
			if !strings.Contains(joined, want) {
				t.Fatalf("expected artifact schema to document %q in %#v", want, schema)
			}
		}
	}
}

func TestAllowedPreviewURL(t *testing.T) {
	if err := allowedPreviewURL("https://3000-sbx.preview.wemob.app/report.pdf"); err != nil {
		t.Fatalf("preview URL rejected: %v", err)
	}
	if err := allowedPreviewURL("https://3000-sbx.provider.test/report.pdf"); err == nil {
		t.Fatal("expected provider URL to be rejected")
	}
	if err := allowedPreviewURL("https://example.com/report.pdf"); err == nil {
		t.Fatal("expected external URL to be rejected")
	}
	if err := allowedPreviewURL("http://3000-sbx.preview.wemob.app/report.pdf"); err == nil {
		t.Fatal("expected non-https URL to be rejected")
	}
}

func TestPreviewOpenRouteUsesManagedStableURL(t *testing.T) {
	deps := &RunDependencies{
		SandboxControl: func(ctx context.Context, project *models.AppProject, user *models.User, req SandboxControlRequest) (map[string]any, error) {
			if req.Action != "open_route" {
				t.Fatalf("action: got %q want open_route", req.Action)
			}
			if req.Route != "/report" {
				t.Fatalf("route: got %q", req.Route)
			}
			return map[string]any{"url": "https://todo.preview.wemob.app/report"}, nil
		},
		SetPreviewURL: func(ctx context.Context, project *models.AppProject, url, label, kind string) error {
			if url != "https://todo.preview.wemob.app/report" || kind != "app" {
				t.Fatalf("preview target = %q %q", url, kind)
			}
			return nil
		},
	}
	rc := &RunContext{Project: &models.AppProject{Settings: map[string]any{}}, User: &models.User{}, Deps: deps}

	result, isErr, err := (PreviewOpenRouteTool{Deps: deps}).Execute(context.Background(), rc, json.RawMessage(`{"route":"/report"}`))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if isErr {
		t.Fatalf("Execute returned isErr=true: %s", result)
	}
}

func TestSandboxStartAndSyncToolsUseManagedActions(t *testing.T) {
	actions := []string{}
	deps := &RunDependencies{
		SandboxControl: func(ctx context.Context, project *models.AppProject, user *models.User, req SandboxControlRequest) (map[string]any, error) {
			actions = append(actions, req.Action)
			if req.Files["backend/package.json"] != `{"scripts":{"dev":"next dev"}}` {
				t.Fatalf("%s did not include live backend files: %#v", req.Action, req.Files)
			}
			if req.Files["frontend/src/App.tsx"] != "export default function App() { return null }" {
				t.Fatalf("%s did not include live frontend files: %#v", req.Action, req.Files)
			}
			return map[string]any{"ok": true}, nil
		},
	}
	rc := &RunContext{
		Project: &models.AppProject{Settings: map[string]any{}},
		User:    &models.User{},
		Deps:    deps,
		FileMap: NewSafeFileMap(map[string]string{
			"backend/package.json": `{"scripts":{"dev":"next dev"}}`,
			"frontend/src/App.tsx": "export default function App() { return null }",
		}),
	}

	for _, tool := range []ToolExecutor{SandboxStartTool{Deps: deps}, SandboxSyncTool{Deps: deps}} {
		result, isErr, err := tool.Execute(context.Background(), rc, json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("%s returned error: %v", tool.Name(), err)
		}
		if isErr {
			t.Fatalf("%s returned isErr=true: %s", tool.Name(), result)
		}
	}

	if got, want := strings.Join(actions, ","), "start,sync"; got != want {
		t.Fatalf("actions = %q, want %q", got, want)
	}
}

func TestProcessToolsUseNamedProcessActions(t *testing.T) {
	var got []SandboxControlRequest
	deps := &RunDependencies{
		SandboxControl: func(ctx context.Context, project *models.AppProject, user *models.User, req SandboxControlRequest) (map[string]any, error) {
			got = append(got, req)
			return map[string]any{"ok": true}, nil
		},
	}
	rc := &RunContext{Project: &models.AppProject{Settings: map[string]any{}}, User: &models.User{}, Deps: deps}

	if _, isErr, err := (ProcessRestartTool{Deps: deps}).Execute(context.Background(), rc, json.RawMessage(`{"process":"backend"}`)); err != nil || isErr {
		t.Fatalf("process_restart failed: isErr=%v err=%v", isErr, err)
	}
	if _, isErr, err := (ProcessLogsTool{Deps: deps}).Execute(context.Background(), rc, json.RawMessage(`{}`)); err != nil || isErr {
		t.Fatalf("process_logs failed: isErr=%v err=%v", isErr, err)
	}

	if len(got) != 2 {
		t.Fatalf("calls = %d", len(got))
	}
	if got[0].Action != "process_restart" || got[0].Process != "backend" {
		t.Fatalf("restart request = %#v", got[0])
	}
	if got[1].Action != "process_logs" || got[1].Process != "frontend" {
		t.Fatalf("logs request = %#v", got[1])
	}
}
