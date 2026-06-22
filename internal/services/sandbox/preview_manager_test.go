package sandbox

import (
	"context"
	"strings"
	"testing"

	"github.com/knid/wemob/internal/models"
)

func TestManagerEnsurePreviewStartsOneSupervisorSession(t *testing.T) {
	fp := newFakeProvider()
	manager := NewManager(fp)
	project := managerProject()
	project.FrontendRuntime = models.FrontendRuntimeViteReact
	project.BackendEnabled = true
	project.BackendRuntime = models.BackendRuntimeNextJSAPI

	status, err := manager.EnsurePreview(context.Background(), project, map[string]string{
		"frontend/package.json": "{}",
		"backend/package.json":  "{}",
	}, map[string]string{"DATABASE_URL": "postgres://example"})
	if err != nil {
		t.Fatal(err)
	}

	if status.State != PreviewStarting && status.State != PreviewReady {
		t.Fatalf("state = %q", status.State)
	}
	if status.URL == "" {
		t.Fatalf("preview url was empty")
	}
	if len(fp.startedSpecs) != 1 {
		t.Fatalf("started specs = %#v, want one supervisor", fp.startedSpecs)
	}
	spec := fp.startedSpecs[0]
	if spec.Name != ProcessPreview {
		t.Fatalf("process name = %q, want %q", spec.Name, ProcessPreview)
	}
	if !strings.Contains(spec.Command, "/tmp/wemob-preview.sh") {
		t.Fatalf("supervisor command = %q", spec.Command)
	}
	var script string
	for _, file := range fp.files {
		if file.Path == "tmp/wemob-preview.sh" {
			script = file.Content
			break
		}
	}
	if !strings.Contains(script, "write_state installing") || !strings.Contains(script, "/workspace/.wemob/logs/web.log") {
		t.Fatalf("supervisor script not uploaded or incomplete: %q", script)
	}
}

func TestManagerPreviewStatusUsesSupervisorState(t *testing.T) {
	fp := newFakeProvider()
	fp.executeStdout = `{"state":"installing","updated_at":"2026-06-20T00:00:00Z"}`
	manager := NewManager(fp)
	project := managerProject()
	project.SandboxID = "daytona-test"

	status, err := manager.PreviewStatus(context.Background(), project)
	if err != nil {
		t.Fatal(err)
	}

	if status.State != PreviewInstalling {
		t.Fatalf("state = %q, want %q", status.State, PreviewInstalling)
	}
	if len(fp.startedSpecs) != 0 {
		t.Fatalf("PreviewStatus started preview process: %#v", fp.startedSpecs)
	}
}

func TestManagerPreviewStatusMissingWhenSupervisorStateAbsent(t *testing.T) {
	fp := newFakeProvider()
	manager := NewManager(fp)
	project := managerProject()
	project.SandboxID = "daytona-test"

	status, err := manager.PreviewStatus(context.Background(), project)
	if err != nil {
		t.Fatal(err)
	}

	if status.State != PreviewMissing {
		t.Fatalf("state = %q, want %q", status.State, PreviewMissing)
	}
	if !strings.Contains(status.Error, "preview supervisor") {
		t.Fatalf("error = %q, want supervisor explanation", status.Error)
	}
}

func TestManagerEnsurePreviewRestartsStoppedSandbox(t *testing.T) {
	fp := newFakeProvider()
	fp.statusState = StateStopped
	manager := NewManager(fp)
	project := managerProject()
	project.SandboxID = "daytona-test"

	status, err := manager.EnsurePreview(context.Background(), project, map[string]string{
		"package.json": `{"scripts":{"dev":"next dev"}}`,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if fp.startCalls != 1 {
		t.Fatalf("start calls = %d, want 1", fp.startCalls)
	}
	if status.State != PreviewStarting && status.State != PreviewReady {
		t.Fatalf("state = %q, want starting or ready", status.State)
	}
}

func TestManagerPreviewStatusReportsDeadSupervisorProcess(t *testing.T) {
	fp := newFakeProvider()
	fp.executeStdout = `{"state":"ready","updated_at":"2026-06-20T00:00:00Z"}`
	fp.processStates = map[ProcessName]ProcessState{
		ProcessFrontend: {Name: ProcessFrontend, Status: ProcessStopped, Port: 3000},
	}
	manager := NewManager(fp)
	project := managerProject()
	project.SandboxID = "daytona-test"

	status, err := manager.PreviewStatus(context.Background(), project)
	if err != nil {
		t.Fatal(err)
	}

	if status.State != PreviewError {
		t.Fatalf("state = %q, want %q", status.State, PreviewError)
	}
	if !strings.Contains(status.Error, "web process is stopped") {
		t.Fatalf("error = %q, want stopped process detail", status.Error)
	}
}

func TestManagerPreviewLogsFiltersByTarget(t *testing.T) {
	fp := newFakeProvider()
	manager := NewManager(fp)
	project := managerProject()
	project.SandboxID = "daytona-test"
	project.FrontendRuntime = models.FrontendRuntimeViteReact
	project.BackendEnabled = true
	project.BackendRuntime = models.BackendRuntimeNextJSAPI

	if _, err := manager.PreviewLogs(context.Background(), project, "backend", 25); err != nil {
		t.Fatal(err)
	}
	if len(fp.executed) == 0 {
		t.Fatalf("expected Execute to be called")
	}
	command := fp.executed[len(fp.executed)-1]
	if !strings.Contains(command, "/workspace/.wemob/logs/backend.log") {
		t.Fatalf("backend log path missing from command: %q", command)
	}
	if strings.Contains(command, "/workspace/.wemob/logs/web.log") {
		t.Fatalf("web log path should not be tailed for backend target: %q", command)
	}
}

func TestManagerSyncDeltasSyncsSourceWithoutRestartingPreview(t *testing.T) {
	fp := newFakeProvider()
	fp.executeStdout = `{"state":"ready","updated_at":"2026-06-20T00:00:00Z"}`
	manager := NewManager(fp)
	project := managerProject()
	project.SandboxID = "daytona-test"
	project.FrontendRuntime = models.FrontendRuntimeViteReact
	project.BackendEnabled = true
	project.BackendRuntime = models.BackendRuntimeNextJSAPI

	err := manager.SyncDeltas(context.Background(), project, []FileDelta{
		{Path: "frontend/src/App.tsx", Content: "export default function App() { return null }"},
		{Path: "frontend/src/old.tsx", Delete: true},
	}, map[string]string{"EXPO_PUBLIC_API_URL": "https://example.test"})
	if err != nil {
		t.Fatal(err)
	}

	var sawDelete, sawPreviewConfig, sawSupervisorScript bool
	for _, file := range fp.files {
		switch file.Path {
		case "frontend/src/old.tsx":
			sawDelete = file.Delete
		case "frontend/vite.config.preview.ts":
			sawPreviewConfig = !file.Delete && strings.Contains(file.Content, `"/api"`)
		case "tmp/wemob-preview.sh":
			sawSupervisorScript = !file.Delete && strings.Contains(file.Content, "write_state starting")
		}
	}
	if !sawDelete {
		t.Fatalf("deleted file delta was not synced: %#v", fp.files)
	}
	if sawPreviewConfig {
		t.Fatalf("managed Vite preview config should not be re-synced for source-only deltas: %#v", fp.files)
	}
	if sawSupervisorScript {
		t.Fatalf("preview supervisor script should not be synced for source-only changes: %#v", fp.files)
	}
	if len(fp.started) != 0 {
		t.Fatalf("started = %#v, want no preview restart", fp.started)
	}
}

func TestManagerSyncDeltasRestartsPreviewForPackageChanges(t *testing.T) {
	fp := newFakeProvider()
	fp.executeStdout = `{"state":"ready","updated_at":"2026-06-20T00:00:00Z"}`
	manager := NewManager(fp)
	project := managerProject()
	project.SandboxID = "daytona-test"
	project.FrontendRuntime = models.FrontendRuntimeViteReact
	project.BackendEnabled = true
	project.BackendRuntime = models.BackendRuntimeNextJSAPI

	err := manager.SyncDeltas(context.Background(), project, []FileDelta{
		{Path: "frontend/package.json", Content: `{"scripts":{"dev":"vite"}}`},
	}, map[string]string{"EXPO_PUBLIC_API_URL": "https://example.test"})
	if err != nil {
		t.Fatal(err)
	}

	var sawSupervisorScript bool
	for _, file := range fp.files {
		if file.Path == "tmp/wemob-preview.sh" && strings.Contains(file.Content, "write_state starting") {
			sawSupervisorScript = true
		}
	}
	if !sawSupervisorScript {
		t.Fatalf("preview supervisor script was not synced: %#v", fp.files)
	}
	if len(fp.started) != 1 || fp.started[0] != ProcessPreview {
		t.Fatalf("started = %#v, want preview restart", fp.started)
	}
	if got := fp.startedSpecs[0].Env["EXPO_PUBLIC_API_URL"]; got != "https://example.test" {
		t.Fatalf("preview restart env = %#v", fp.startedSpecs[0].Env)
	}
}

func TestManagerSyncRestartsPreviewWithEnv(t *testing.T) {
	fp := newFakeProvider()
	fp.executeStdout = `{"state":"ready","updated_at":"2026-06-20T00:00:00Z"}`
	manager := NewManager(fp)
	project := managerProject()
	project.SandboxID = "daytona-test"

	err := manager.Sync(context.Background(), project, map[string]string{
		"package.json": `{"scripts":{"dev":"next dev"}}`,
	}, map[string]string{"NEXT_PUBLIC_FLAG": "enabled"})
	if err != nil {
		t.Fatal(err)
	}

	if len(fp.started) != 1 || fp.started[0] != ProcessPreview {
		t.Fatalf("started = %#v, want preview restart", fp.started)
	}
	if got := fp.startedSpecs[0].Env["NEXT_PUBLIC_FLAG"]; got != "enabled" {
		t.Fatalf("preview restart env = %#v", fp.startedSpecs[0].Env)
	}
}
