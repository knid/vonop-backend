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
	if !strings.Contains(spec.Command, "/workspace/project/tmp/wemob-preview.sh") {
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
