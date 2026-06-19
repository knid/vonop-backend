package sandbox

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
)

func managerProject() *models.AppProject {
	return &models.AppProject{ID: uuid.New(), ProjectType: models.ProjectTypeApp, FrontendRuntime: models.FrontendRuntimeNextJSLegacy}
}

func TestManagerStartCreatesSyncsAndStartsProcesses(t *testing.T) {
	fp := newFakeProvider()
	m := NewManager(fp)
	project := managerProject()
	files := map[string]string{"package.json": `{"scripts":{"dev":"next dev"}}`}

	status, err := m.Start(context.Background(), project, files, nil)
	if err != nil {
		t.Fatal(err)
	}
	if status.SandboxID != fp.sandboxID {
		t.Fatalf("sandbox id = %q", status.SandboxID)
	}
	if len(fp.files) != 1 || fp.files[0].Path != "package.json" {
		t.Fatalf("synced files = %#v", fp.files)
	}
	if len(fp.started) == 0 {
		t.Fatalf("no processes started")
	}
}

func TestManagerOpenRouteUpdatesPreviewTarget(t *testing.T) {
	fp := newFakeProvider()
	m := NewManager(fp)
	project := managerProject()
	project.SandboxID = fp.sandboxID

	target, err := m.OpenRoute(context.Background(), project, "/checkout", "agent")
	if err != nil {
		t.Fatal(err)
	}
	if target.Kind != PreviewRoute {
		t.Fatalf("kind = %q", target.Kind)
	}
	if target.SetBy != "agent" {
		t.Fatalf("set_by = %q", target.SetBy)
	}
	if target.URL != "https://provider-3000.example.test/checkout" {
		t.Fatalf("url = %q", target.URL)
	}
}
