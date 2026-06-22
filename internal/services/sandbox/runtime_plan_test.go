package sandbox

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
)

func testProject() *models.AppProject {
	return &models.AppProject{
		ID:          uuid.New(),
		ProjectType: models.ProjectTypeApp,
	}
}

func processNames(plan RuntimePlan) map[ProcessName]bool {
	out := map[ProcessName]bool{}
	for _, p := range plan.Processes {
		out[p.Name] = true
	}
	return out
}

func TestRuntimePlanNextJS(t *testing.T) {
	p := testProject()
	p.ProjectType = models.ProjectTypeApp
	p.FrontendRuntime = models.FrontendRuntimeNextJSLegacy
	plan := BuildRuntimePlan(p)
	if plan.DefaultPreview != PreviewApp {
		t.Fatalf("default preview = %q", plan.DefaultPreview)
	}
	if !processNames(plan)[ProcessFrontend] {
		t.Fatalf("frontend process missing")
	}
	if plan.Previews[PreviewApp] != 3000 {
		t.Fatalf("app preview port = %d", plan.Previews[PreviewApp])
	}
}

func TestRuntimePlanNodeHeapFitsDefaultSandboxMemory(t *testing.T) {
	projects := []*models.AppProject{
		func() *models.AppProject {
			p := testProject()
			p.FrontendRuntime = models.FrontendRuntimeNextJSLegacy
			return p
		}(),
		func() *models.AppProject {
			p := testProject()
			p.FrontendRuntime = models.FrontendRuntimeViteReact
			p.BackendEnabled = true
			p.BackendRuntime = models.BackendRuntimeNextJSAPI
			return p
		}(),
		func() *models.AppProject {
			p := testProject()
			p.ProjectType = models.ProjectTypeMobileAppNative
			return p
		}(),
	}
	for _, project := range projects {
		plan := BuildRuntimePlan(project)
		for _, process := range plan.Processes {
			if process.Name == ProcessArtifacts {
				continue
			}
			if !strings.Contains(process.Command, "--max-old-space-size=768") {
				t.Fatalf("process %s command does not cap Node heap for 1GB sandbox: %q", process.Name, process.Command)
			}
			if strings.Contains(process.Command, "--max-old-space-size=3072") {
				t.Fatalf("process %s still requests 3GB heap: %q", process.Name, process.Command)
			}
		}
	}
}

func TestRuntimePlanViteSplitWeb(t *testing.T) {
	p := testProject()
	p.FrontendRuntime = models.FrontendRuntimeViteReact
	p.BackendEnabled = true
	p.BackendRuntime = models.BackendRuntimeNextJSAPI
	plan := BuildRuntimePlan(p)
	names := processNames(plan)
	if !names[ProcessFrontend] || !names[ProcessBackend] {
		t.Fatalf("split-web processes missing: %#v", names)
	}
	if plan.Previews[PreviewApp] != 3000 || plan.Previews[PreviewBackend] != 3001 {
		t.Fatalf("preview ports = %#v", plan.Previews)
	}
	var frontend ProcessSpec
	for _, process := range plan.Processes {
		if process.Name == ProcessFrontend {
			frontend = process
		}
	}
	if frontend.Cwd != ProjectRoot+"/frontend" {
		t.Fatalf("frontend cwd = %q", frontend.Cwd)
	}
	if !strings.Contains(frontend.Command, "--config vite.config.preview.ts") {
		t.Fatalf("frontend command does not use preview config: %q", frontend.Command)
	}
}

func TestRuntimePlanMobileNative(t *testing.T) {
	p := testProject()
	p.ProjectType = models.ProjectTypeMobileAppNative
	plan := BuildRuntimePlan(p)
	names := processNames(plan)
	if !names[ProcessExpoWeb] || !names[ProcessExpoGo] {
		t.Fatalf("expo processes missing: %#v", names)
	}
	if plan.Previews[PreviewApp] != 8081 || plan.Previews[PreviewExpo] != 19000 {
		t.Fatalf("mobile preview ports = %#v", plan.Previews)
	}
	for _, process := range plan.Processes {
		if process.Name != ProcessExpoWeb && process.Name != ProcessExpoGo {
			continue
		}
		if strings.Contains(process.Command, "CI=1") {
			t.Fatalf("expo preview command should allow Metro reloads, got %q", process.Command)
		}
	}
}

func TestRuntimePlanBackendOnly(t *testing.T) {
	p := testProject()
	p.FrontendRuntime = models.FrontendRuntimeNone
	p.BackendEnabled = true
	p.BackendRuntime = models.BackendRuntimeNextJSAPI
	plan := BuildRuntimePlan(p)
	if plan.DefaultPreview != PreviewBackend {
		t.Fatalf("default preview = %q", plan.DefaultPreview)
	}
	if !processNames(plan)[ProcessBackend] {
		t.Fatalf("backend process missing")
	}
}

func TestRuntimePlanBackendOnlyUnsupportedRuntime(t *testing.T) {
	p := testProject()
	p.FrontendRuntime = models.FrontendRuntimeNone
	p.BackendEnabled = true
	p.BackendRuntime = models.BackendRuntimeNone
	plan := BuildRuntimePlan(p)
	if processNames(plan)[ProcessBackend] {
		t.Fatalf("backend process present")
	}
	if _, ok := plan.Previews[PreviewBackend]; ok {
		t.Fatalf("backend preview present: %#v", plan.Previews)
	}
}
