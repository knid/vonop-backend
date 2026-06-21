package controllers

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
)

func TestInjectViteStaticScripts(t *testing.T) {
	files := map[string]string{
		"frontend/index.html": `<!doctype html><html><body><div id="root"></div></body></html>`,
	}

	injectViteStaticScripts(files, "project-123", true)
	got := files["frontend/index.html"]
	for _, want := range []string{"data-wemob-badge", "data-wemob-analytics", "project-123"} {
		if !strings.Contains(got, want) {
			t.Fatalf("injected index.html missing %q: %s", want, got)
		}
	}
	if strings.Index(got, "data-wemob-badge") > strings.Index(got, "</body>") {
		t.Fatalf("scripts should be injected before </body>: %s", got)
	}

	injectViteStaticScripts(files, "project-123", true)
	if strings.Count(files["frontend/index.html"], "data-wemob-analytics") != 1 {
		t.Fatalf("analytics script should be idempotent: %s", files["frontend/index.html"])
	}
}

func TestStripPreviewSubtree(t *testing.T) {
	files := map[string]string{
		"frontend/package.json": "{}",
		"frontend/src/App.tsx":  "export default function App() { return null }",
		"backend/package.json":  "{}",
	}

	got := stripPreviewSubtree(files, "frontend/")
	if got["package.json"] != "{}" || got["src/App.tsx"] == "" {
		t.Fatalf("unexpected stripped frontend files: %#v", got)
	}
	if _, ok := got["backend/package.json"]; ok {
		t.Fatalf("stripped frontend files must not include backend files: %#v", got)
	}
}

func TestNormalizeSandboxChangedPathsForMobilePreview(t *testing.T) {
	project := &models.AppProject{ID: uuid.New(), ProjectType: models.ProjectTypeMobileAppNative}

	got := normalizeSandboxChangedPaths(project, []string{
		"mobile/app/index.tsx",
		"backend/app/api/route.ts",
		"mobile/app/index.tsx",
		`mobile\package.json`,
	})
	want := []string{"app/index.tsx", "package.json"}
	if len(got) != len(want) {
		t.Fatalf("paths = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("paths = %#v, want %#v", got, want)
		}
	}
}
