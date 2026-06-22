package sandbox

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
)

func previewRuntimeProject() *models.AppProject {
	return &models.AppProject{ID: uuid.New(), ProjectType: models.ProjectTypeApp}
}

func TestBuildPreviewRuntimeDefaultWeb(t *testing.T) {
	project := previewRuntimeProject()
	project.FrontendRuntime = models.FrontendRuntimeNextJSLegacy

	runtime := BuildPreviewRuntime(project)

	if runtime.Main != "web" {
		t.Fatalf("main = %q, want web", runtime.Main)
	}
	if len(runtime.InstallDirs) != 1 || runtime.InstallDirs[0] != ProjectRoot {
		t.Fatalf("install dirs = %#v", runtime.InstallDirs)
	}
	if got := runtime.Port("web"); got != 3000 {
		t.Fatalf("web port = %d, want 3000", got)
	}
}

func TestBuildPreviewRuntimeSplitWeb(t *testing.T) {
	project := previewRuntimeProject()
	project.FrontendRuntime = models.FrontendRuntimeViteReact
	project.BackendEnabled = true
	project.BackendRuntime = models.BackendRuntimeNextJSAPI

	runtime := BuildPreviewRuntime(project)

	if runtime.Main != "web" {
		t.Fatalf("main = %q, want web", runtime.Main)
	}
	if got := strings.Join(runtime.InstallDirs, ","); got != BackendRoot+","+ProjectRoot+"/frontend" {
		t.Fatalf("install dirs = %q", got)
	}
	if got := runtime.Port("backend"); got != 3001 {
		t.Fatalf("backend port = %d, want 3001", got)
	}
	if got := runtime.Port("web"); got != 3000 {
		t.Fatalf("web port = %d, want 3000", got)
	}
	if !strings.Contains(runtime.Processes[1].Command, "vite.config.preview.ts") {
		t.Fatalf("web command should use generated preview config, got %q", runtime.Processes[1].Command)
	}
}

func TestPreviewSupervisorScriptWritesManagedVitePreviewConfig(t *testing.T) {
	project := previewRuntimeProject()
	project.FrontendRuntime = models.FrontendRuntimeViteReact
	project.BackendEnabled = true
	project.BackendRuntime = models.BackendRuntimeNextJSAPI

	script := BuildPreviewSupervisorScript(BuildPreviewRuntime(project))

	for _, want := range []string{
		"cat > '/workspace/project/frontend/vite.config.preview.ts'",
		`target: "http://127.0.0.1:3001"`,
		"npm run dev -- --config vite.config.preview.ts",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("script missing %q:\n%s", want, script)
		}
	}
}

func TestBuildPreviewRuntimeMobileNative(t *testing.T) {
	project := previewRuntimeProject()
	project.ProjectType = models.ProjectTypeMobileAppNative

	runtime := BuildPreviewRuntime(project)

	if runtime.Main != "web" {
		t.Fatalf("main = %q, want web", runtime.Main)
	}
	if got := runtime.Port("web"); got != 8081 {
		t.Fatalf("web port = %d, want 8081", got)
	}
	if got := runtime.Port("expo"); got != 19000 {
		t.Fatalf("expo port = %d, want 19000", got)
	}
	if !strings.HasPrefix(runtime.Processes[1].Command, "export EXPO_GO_MODE=1 && export NODE_OPTIONS=") {
		t.Fatalf("expo command should export env before starting, got %q", runtime.Processes[1].Command)
	}
	if !runtime.Processes[1].Optional {
		t.Fatalf("expo go process should be optional in web preview supervisor")
	}
	for _, process := range runtime.Processes {
		if strings.Contains(process.Command, "--host 0.0.0.0") {
			t.Fatalf("expo command uses invalid host mode: %q", process.Command)
		}
	}
}

func TestPreviewSupervisorScriptIgnoresOnlyOptionalProcesses(t *testing.T) {
	project := previewRuntimeProject()
	project.ProjectType = models.ProjectTypeMobileAppNative

	script := BuildPreviewSupervisorScript(BuildPreviewRuntime(project))

	for _, want := range []string{
		"main_pid=\"\"",
		"required_pids=()",
		"pid=$!",
		"main_pid=\"$pid\"",
		"required_pids+=(\"$pid\")",
		"wait -n \"${required_pids[@]}\"",
		"main preview process web was not started",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("supervisor script missing %q:\n%s", want, script)
		}
	}
	if strings.Count(script, "required_pids+=(\"$pid\")") != 1 {
		t.Fatalf("mobile supervisor should track only the required web process:\n%s", script)
	}
}

func TestPreviewSupervisorScriptKeepsSplitWebBackendFailureVisible(t *testing.T) {
	project := previewRuntimeProject()
	project.FrontendRuntime = models.FrontendRuntimeViteReact
	project.BackendEnabled = true
	project.BackendRuntime = models.BackendRuntimeNextJSAPI

	script := BuildPreviewSupervisorScript(BuildPreviewRuntime(project))

	for _, want := range []string{
		"required_pids=()",
		"missing_required=\"\"",
		"required preview process ${missing_required} was not started",
		"wait -n \"${required_pids[@]}\"",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("supervisor script missing %q:\n%s", want, script)
		}
	}
	if strings.Count(script, "required_pids+=(\"$pid\")") != 2 {
		t.Fatalf("split web supervisor should track both backend and web as required:\n%s", script)
	}
}

func TestPreviewSupervisorScriptContainsSingleSessionProcessControl(t *testing.T) {
	runtime := PreviewRuntime{
		InstallDirs: []string{ProjectRoot, ProjectRoot},
		Main:        "web",
		Processes: []PreviewProcess{
			{Name: "web", Cwd: ProjectRoot, Port: 3000, Command: "npm run dev -- --port 3000"},
		},
	}

	script := BuildPreviewSupervisorScript(runtime)

	if strings.Count(script, "npm install") != 1 {
		t.Fatalf("script installs dependencies %d times, want 1:\n%s", strings.Count(script, "npm install"), script)
	}
	for _, want := range []string{
		"write_state installing",
		"write_state starting",
		"write_state ready",
		"trap cleanup EXIT",
		"/workspace/.wemob/logs/web.log",
		"http://127.0.0.1:3000",
		"wait -n \"${required_pids[@]}\"",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("script missing %q:\n%s", want, script)
		}
	}
}

func TestPreviewSupervisorScriptSkipsProcessesWithoutPackageJSON(t *testing.T) {
	runtime := PreviewRuntime{
		Root: ProjectRoot,
		Main: "web",
		Processes: []PreviewProcess{
			{Name: "backend", Cwd: BackendRoot, Port: 3001, Command: "npm run dev -- -p 3001"},
			{Name: "web", Cwd: ProjectRoot + "/frontend", Port: 3000, Command: "npm run dev -- --port 3000"},
		},
	}

	script := BuildPreviewSupervisorScript(runtime)

	for _, want := range []string{
		"started_processes=0",
		"if test -f '/workspace/backend/package.json'; then",
		"started_processes=$((started_processes + 1))",
		"skipped: /workspace/backend/package.json not found",
		"missing_required='backend'",
		"required preview process ${missing_required} was not started",
		"if test \"$started_processes\" -eq 0; then",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("script missing %q:\n%s", want, script)
		}
	}
}

func TestPreviewSupervisorScriptWritesErrorOnInstallOrReadinessFailure(t *testing.T) {
	script := BuildPreviewSupervisorScript(PreviewRuntime{
		Root:        ProjectRoot,
		InstallDirs: []string{ProjectRoot},
		Main:        "web",
		Processes: []PreviewProcess{
			{Name: "web", Cwd: ProjectRoot, Port: 3000, Command: "npm run dev"},
		},
	})

	for _, want := range []string{
		"write_error()",
		"trap 'write_error",
		"write_error \"preview did not become ready",
		"write_error \"preview process exited",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("supervisor script missing %q:\n%s", want, script)
		}
	}
}
