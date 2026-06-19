package sandbox

import (
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
