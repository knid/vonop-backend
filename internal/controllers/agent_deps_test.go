package controllers

import (
	"testing"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	sandboxService "github.com/knid/wemob/internal/services/sandbox"
)

func TestAgentSandboxStatusMapUsesStablePreviewURLs(t *testing.T) {
	projectID := uuid.New()
	project := &models.AppProject{ID: projectID, Slug: "todo"}
	status := &sandboxService.Status{
		Provider:  sandboxService.ProviderDaytona,
		SandboxID: "sandbox-123",
		State:     sandboxService.StateReady,
		PreviewURLs: map[sandboxService.PreviewKind]string{
			sandboxService.PreviewApp:       "https://3000-sandbox.daytonaproxy01.eu",
			sandboxService.PreviewBackend:   "https://3001-sandbox.daytonaproxy01.eu",
			sandboxService.PreviewArtifacts: "https://7000-sandbox.daytonaproxy01.eu",
		},
	}

	out := agentSandboxStatusMap(project, status)
	urls, ok := out["preview_urls"].(map[string]string)
	if !ok {
		t.Fatalf("preview_urls type = %T", out["preview_urls"])
	}
	wantAppURL := "https://id-" + projectID.String() + "-app.preview.wemob.app"
	if urls["app"] != wantAppURL {
		t.Fatalf("app URL = %q", urls["app"])
	}
	if urls["backend"] != "https://todo-backend.preview.wemob.app" {
		t.Fatalf("backend URL = %q", urls["backend"])
	}
	if urls["artifacts"] != "https://todo-artifacts.preview.wemob.app" {
		t.Fatalf("artifacts URL = %q", urls["artifacts"])
	}
	if out["preview_url"] != wantAppURL {
		t.Fatalf("preview_url = %q", out["preview_url"])
	}
}

func TestAgentSandboxStatusMapReturnsWritableMapForNilStatus(t *testing.T) {
	project := &models.AppProject{ID: uuid.New(), Slug: "todo"}

	out := agentSandboxStatusMap(project, nil)
	out["env_status"] = map[string]string{"DATABASE_URL": "set"}

	if out["env_status"] == nil {
		t.Fatal("env_status was not written")
	}
}
