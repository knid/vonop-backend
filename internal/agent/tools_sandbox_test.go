package agent

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	sandboxService "github.com/knid/wemob/internal/services/sandbox"
)

func TestSandboxToolNamesIncludePreviewAndExecTools(t *testing.T) {
	got := strings.Join(sandboxToolNames(), ",")
	if got != "preview_logs,preview_status,sandbox_exec" {
		t.Fatalf("sandbox tools = %q", got)
	}
}

func TestPreviewStatusToolIsReadOnly(t *testing.T) {
	var action string
	deps := &RunDependencies{
		SandboxControl: func(ctx context.Context, project *models.AppProject, user *models.User, req SandboxControlRequest) (map[string]any, error) {
			action = req.Action
			return map[string]any{"state": "ready"}, nil
		},
	}
	rc := &RunContext{Project: &models.AppProject{}, User: &models.User{}, Deps: deps}

	result, isErr, err := (PreviewStatusTool{Deps: deps}).Execute(context.Background(), rc, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if isErr {
		t.Fatalf("Execute returned isErr=true: %s", result)
	}
	if action != "preview_status" {
		t.Fatalf("action = %q, want preview_status", action)
	}
}

func TestPreviewLogsToolAllowsOnlyTargetSelection(t *testing.T) {
	var got SandboxControlRequest
	deps := &RunDependencies{
		SandboxControl: func(ctx context.Context, project *models.AppProject, user *models.User, req SandboxControlRequest) (map[string]any, error) {
			got = req
			return map[string]any{"logs": "ok"}, nil
		},
	}
	rc := &RunContext{Project: &models.AppProject{}, User: &models.User{}, Deps: deps}

	result, isErr, err := (PreviewLogsTool{Deps: deps}).Execute(context.Background(), rc, json.RawMessage(`{"target":"backend"}`))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if isErr {
		t.Fatalf("Execute returned isErr=true: %s", result)
	}
	if got.Action != "preview_logs" || got.Target != "backend" {
		t.Fatalf("request = %#v", got)
	}
}

func TestSandboxExecToolExecutesFiniteCommandWithCwdAlias(t *testing.T) {
	var got SandboxControlRequest
	deps := &RunDependencies{
		SandboxControl: func(ctx context.Context, project *models.AppProject, user *models.User, req SandboxControlRequest) (map[string]any, error) {
			got = req
			return map[string]any{"stdout": "ok", "exit_code": 0}, nil
		},
	}
	rc := &RunContext{
		Project: &models.AppProject{
			ProjectType:     models.ProjectTypeApp,
			FrontendRuntime: models.FrontendRuntimeViteReact,
		},
		User: &models.User{},
		Deps: deps,
	}

	result, isErr, err := (SandboxExecTool{Deps: deps}).Execute(context.Background(), rc, json.RawMessage(`{"command":"npm run build","cwd":"frontend","timeout_ms":60000}`))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if isErr {
		t.Fatalf("Execute returned isErr=true: %s", result)
	}
	if got.Action != "exec" || got.Command != "npm run build" || got.Cwd != "/workspace/project/frontend" || got.TimeoutMs != 60000 {
		t.Fatalf("request = %#v", got)
	}
}

func TestSandboxExecToolSyncsGitBackedChangesBeforeExec(t *testing.T) {
	var syncPaths []string
	var execCalled bool
	deps := &RunDependencies{
		SyncSandboxFiles: func(ctx context.Context, project *models.AppProject, paths []string) error {
			syncPaths = append([]string(nil), paths...)
			return nil
		},
		SandboxControl: func(ctx context.Context, project *models.AppProject, user *models.User, req SandboxControlRequest) (map[string]any, error) {
			execCalled = true
			return map[string]any{"stdout": "ok", "exit_code": 0}, nil
		},
	}
	state := NewBuildState("build app", false)
	state.RecordFileWrite("frontend/src/components/Hero.tsx", "create")
	rc := &RunContext{
		Project: &models.AppProject{
			ProjectType:     models.ProjectTypeApp,
			FrontendRuntime: models.FrontendRuntimeViteReact,
		},
		User:       &models.User{},
		Deps:       deps,
		BuildState: state,
	}

	result, isErr, err := (SandboxExecTool{Deps: deps}).Execute(context.Background(), rc, json.RawMessage(`{"command":"npm run build","cwd":"frontend"}`))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if isErr {
		t.Fatalf("Execute returned isErr=true: %s", result)
	}
	if !execCalled {
		t.Fatal("expected sandbox exec after sync")
	}
	if !slices.Contains(syncPaths, "frontend/src/components/Hero.tsx") {
		t.Fatalf("expected changed path to sync before exec, got %#v", syncPaths)
	}
}

func TestSandboxExecToolSyncsCurrentFileMapDeltasBeforeExec(t *testing.T) {
	var gotDeltas []sandboxService.FileDelta
	var legacyPathSyncCalled bool
	deps := &RunDependencies{
		SyncSandboxFiles: func(ctx context.Context, project *models.AppProject, paths []string) error {
			legacyPathSyncCalled = true
			return nil
		},
		SyncSandboxDeltas: func(ctx context.Context, project *models.AppProject, deltas []sandboxService.FileDelta) error {
			gotDeltas = append([]sandboxService.FileDelta(nil), deltas...)
			return nil
		},
		SandboxControl: func(ctx context.Context, project *models.AppProject, user *models.User, req SandboxControlRequest) (map[string]any, error) {
			return map[string]any{"stdout": "ok", "exit_code": 0}, nil
		},
	}
	state := NewBuildState("build app", false)
	state.RecordFileWrite("frontend/src/pages/AdminPage.tsx", "update")
	rc := &RunContext{
		Project: &models.AppProject{
			ProjectType:     models.ProjectTypeApp,
			FrontendRuntime: models.FrontendRuntimeViteReact,
		},
		User:       &models.User{},
		Deps:       deps,
		BuildState: state,
		FileMap:    NewSafeFileMap(map[string]string{"frontend/src/pages/AdminPage.tsx": "fresh import"}),
	}

	result, isErr, err := (SandboxExecTool{Deps: deps}).Execute(context.Background(), rc, json.RawMessage(`{"command":"npm run build","cwd":"frontend"}`))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if isErr {
		t.Fatalf("expected success, got: %s", result)
	}
	if legacyPathSyncCalled {
		t.Fatal("expected exact delta sync, not stale path-based repo sync")
	}
	if len(gotDeltas) != 1 {
		t.Fatalf("deltas = %#v, want one delta", gotDeltas)
	}
	if gotDeltas[0].Path != "frontend/src/pages/AdminPage.tsx" || gotDeltas[0].Content != "fresh import" || gotDeltas[0].Delete {
		t.Fatalf("delta = %#v, want fresh content delta", gotDeltas[0])
	}
}

func TestSandboxExecToolSyncsDeletedFilesAsDeleteDeltasBeforeExec(t *testing.T) {
	var gotDeltas []sandboxService.FileDelta
	deps := &RunDependencies{
		SyncSandboxDeltas: func(ctx context.Context, project *models.AppProject, deltas []sandboxService.FileDelta) error {
			gotDeltas = append([]sandboxService.FileDelta(nil), deltas...)
			return nil
		},
		SandboxControl: func(ctx context.Context, project *models.AppProject, user *models.User, req SandboxControlRequest) (map[string]any, error) {
			return map[string]any{"stdout": "ok", "exit_code": 0}, nil
		},
	}
	state := NewBuildState("build app", false)
	state.RecordFileDelete("frontend/src/Old.tsx")
	rc := &RunContext{
		Project:    &models.AppProject{ProjectType: models.ProjectTypeApp, FrontendRuntime: models.FrontendRuntimeViteReact},
		User:       &models.User{},
		Deps:       deps,
		BuildState: state,
		FileMap:    NewSafeFileMap(map[string]string{}),
	}

	result, isErr, err := (SandboxExecTool{Deps: deps}).Execute(context.Background(), rc, json.RawMessage(`{"command":"npm run build","cwd":"frontend"}`))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if isErr {
		t.Fatalf("expected success, got: %s", result)
	}
	if len(gotDeltas) != 1 || gotDeltas[0].Path != "frontend/src/Old.tsx" || !gotDeltas[0].Delete {
		t.Fatalf("deltas = %#v, want delete delta", gotDeltas)
	}
}

func TestSandboxExecToolRejectsSourceMutationCommands(t *testing.T) {
	called := false
	deps := &RunDependencies{
		SandboxControl: func(ctx context.Context, project *models.AppProject, user *models.User, req SandboxControlRequest) (map[string]any, error) {
			called = true
			return nil, nil
		},
	}
	rc := &RunContext{Project: &models.AppProject{ProjectType: models.ProjectTypeApp}, User: &models.User{}, Deps: deps}

	result, isErr, err := (SandboxExecTool{Deps: deps}).Execute(context.Background(), rc, json.RawMessage(`{"command":"echo hacked > frontend/src/App.tsx","cwd":"project"}`))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !isErr {
		t.Fatalf("expected tool error, got success: %s", result)
	}
	if called {
		t.Fatal("sandbox control should not be called for rejected mutation commands")
	}
	if !strings.Contains(result, "file tools") {
		t.Fatalf("expected file tool guidance, got: %s", result)
	}
}

func TestSandboxExecToolIgnoresLegacyKillSwitch(t *testing.T) {
	called := false
	deps := &RunDependencies{
		SandboxControl: func(ctx context.Context, project *models.AppProject, user *models.User, req SandboxControlRequest) (map[string]any, error) {
			called = true
			return map[string]any{"stdout": "ok", "exit_code": 0}, nil
		},
	}
	rc := &RunContext{
		Project: &models.AppProject{ProjectType: models.ProjectTypeApp, Settings: map[string]any{constants.AgentSettingAllowSandboxExec: false}},
		User:    &models.User{},
		Deps:    deps,
	}

	result, isErr, err := (SandboxExecTool{Deps: deps}).Execute(context.Background(), rc, json.RawMessage(`{"command":"npm run build"}`))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if isErr {
		t.Fatalf("Execute returned isErr=true: %s", result)
	}
	if !called {
		t.Fatal("sandbox control should be called because sandbox_exec is available for every project")
	}
	if !strings.Contains(result, "ok") {
		t.Fatalf("expected sandbox output, got: %s", result)
	}
}

func TestSandboxExecToolRejectsChainedAndShellWrappedMutations(t *testing.T) {
	rc := &RunContext{Project: &models.AppProject{ProjectType: models.ProjectTypeApp}, User: &models.User{}, Deps: &RunDependencies{
		SandboxControl: func(ctx context.Context, project *models.AppProject, user *models.User, req SandboxControlRequest) (map[string]any, error) {
			t.Fatalf("sandbox control should not be called for %q", req.Command)
			return nil, nil
		},
	}}

	for _, input := range []string{
		`{"command":"npm test; rm -rf src","cwd":"project"}`,
		`{"command":"npm test && touch src/x","cwd":"project"}`,
		`{"command":"CI=1 sh -c 'touch src/x'","cwd":"project"}`,
	} {
		t.Run(input, func(t *testing.T) {
			result, isErr, err := (SandboxExecTool{}).Execute(context.Background(), rc, json.RawMessage(input))
			if err != nil {
				t.Fatalf("Execute returned error: %v", err)
			}
			if !isErr {
				t.Fatalf("expected tool error, got success: %s", result)
			}
			if !strings.Contains(result, "Git-backed file tools") {
				t.Fatalf("expected file tool guidance, got: %s", result)
			}
		})
	}
}

func TestSandboxExecToolHandlesMissingRunContext(t *testing.T) {
	result, isErr, err := (SandboxExecTool{}).Execute(context.Background(), nil, json.RawMessage(`{"command":"npm run build"}`))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !isErr {
		t.Fatalf("expected tool error, got success: %s", result)
	}
	if !strings.Contains(result, "preview control is not configured") {
		t.Fatalf("expected missing-control message, got: %s", result)
	}
}
