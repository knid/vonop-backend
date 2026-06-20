package sandbox

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
)

func managerProject() *models.AppProject {
	return &models.AppProject{ID: uuid.New(), Slug: "todo", ProjectType: models.ProjectTypeApp, FrontendRuntime: models.FrontendRuntimeNextJSLegacy}
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
	if fp.startCalls != 1 {
		t.Fatalf("start calls = %d", fp.startCalls)
	}
	if project.SandboxID != fp.sandboxID {
		t.Fatalf("project sandbox id = %q", project.SandboxID)
	}
}

func TestManagerStartPropagatesEnvToProcesses(t *testing.T) {
	fp := newFakeProvider()
	m := NewManager(fp)
	project := managerProject()
	env := map[string]string{
		"API_KEY":          "secret",
		"NEXT_PUBLIC_FLAG": "enabled",
	}

	if _, err := m.Start(context.Background(), project, map[string]string{"package.json": "{}"}, env); err != nil {
		t.Fatal(err)
	}

	if len(fp.startedSpecs) == 0 {
		t.Fatalf("no process specs started")
	}
	got := fp.startedSpecs[0].Env
	if got["API_KEY"] != "secret" || got["NEXT_PUBLIC_FLAG"] != "enabled" {
		t.Fatalf("process env = %#v", got)
	}
}

func TestManagerStartInstallsDependenciesBeforeProcesses(t *testing.T) {
	fp := newFakeProvider()
	m := NewManager(fp)
	project := managerProject()
	project.FrontendRuntime = models.FrontendRuntimeViteReact
	project.BackendEnabled = true
	project.BackendRuntime = models.BackendRuntimeNextJSAPI

	if _, err := m.Start(context.Background(), project, map[string]string{
		"frontend/package.json": `{"scripts":{"dev":"vite"}}`,
		"backend/package.json":  `{"scripts":{"dev":"next dev"}}`,
	}, nil); err != nil {
		t.Fatal(err)
	}

	if len(fp.executed) != 2 {
		t.Fatalf("install commands = %#v", fp.executed)
	}
	if fp.executeCwds[0] != BackendRoot || fp.executeCwds[1] != ProjectRoot+"/frontend" {
		t.Fatalf("install cwds = %#v", fp.executeCwds)
	}
	if len(fp.startedSpecs) == 0 {
		t.Fatal("expected processes to start")
	}
	if fp.executeCwds[0] != fp.startedSpecs[0].Cwd {
		t.Fatalf("first install cwd %q should match first process cwd %q", fp.executeCwds[0], fp.startedSpecs[0].Cwd)
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
	wantURL := "https://id-" + project.ID.String() + "-app.preview.wemob.app/checkout"
	if target.URL != wantURL {
		t.Fatalf("url = %q", target.URL)
	}
}

func TestManagerOpenArtifactStartsServerAndBuildsPreviewTarget(t *testing.T) {
	fp := newFakeProvider()
	m := NewManager(fp)
	project := managerProject()
	project.SandboxID = fp.sandboxID

	target, err := m.OpenArtifact(context.Background(), project, "/workspace/artifacts/reports/summary.html", "agent")
	if err != nil {
		t.Fatal(err)
	}
	if target.Kind != PreviewArtifacts {
		t.Fatalf("kind = %q", target.Kind)
	}
	if target.Label != "summary.html" {
		t.Fatalf("label = %q", target.Label)
	}
	if target.SetBy != "agent" {
		t.Fatalf("set_by = %q", target.SetBy)
	}
	if target.URL != "https://todo-artifacts.preview.wemob.app/reports/summary.html" {
		t.Fatalf("url = %q", target.URL)
	}
	if len(fp.started) != 1 || fp.started[0] != ProcessArtifacts {
		t.Fatalf("started = %#v", fp.started)
	}
}

func TestStablePreviewBaseURLUsesAppSuffixForReservedSlug(t *testing.T) {
	project := managerProject()
	project.Slug = "todo-backend"

	got := StablePreviewBaseURL(project, PreviewApp)
	want := "https://id-" + project.ID.String() + "-app.preview.wemob.app"
	if got != want {
		t.Fatalf("stable app URL = %q", got)
	}
}

func TestStablePreviewBaseURLFallsBackToIDWhenLabelWouldBeTooLong(t *testing.T) {
	project := managerProject()
	project.Slug = strings.Repeat("a", 56)

	got := StablePreviewBaseURL(project, PreviewArtifacts)
	want := "https://id-" + project.ID.String() + "-artifacts.preview.wemob.app"
	if got != want {
		t.Fatalf("stable artifact URL = %q, want %q", got, want)
	}
	if label := strings.TrimSuffix(strings.TrimPrefix(got, "https://"), ".preview.wemob.app"); len(label) > 63 {
		t.Fatalf("preview label length = %d, want <= 63", len(label))
	}
}

func TestManagerOpenArtifactFallsBackToProviderURLWithoutSlug(t *testing.T) {
	fp := newFakeProvider()
	m := NewManager(fp)
	project := managerProject()
	project.Slug = strings.Repeat("a", 56)
	project.ID = uuid.Nil
	project.SandboxID = fp.sandboxID

	target, err := m.OpenArtifact(context.Background(), project, "/workspace/artifacts/hello.pdf", "agent")
	if err != nil {
		t.Fatal(err)
	}
	if target.URL != "https://provider-7000.example.test/hello.pdf" {
		t.Fatalf("url = %q", target.URL)
	}
}

func TestManagerMethodsRejectNilProject(t *testing.T) {
	m := NewManager(newFakeProvider())
	methods := map[string]func() error{
		"Start": func() error {
			_, err := m.Start(context.Background(), nil, nil, nil)
			return err
		},
		"Status": func() error {
			_, err := m.Status(context.Background(), nil)
			return err
		},
		"Sync": func() error {
			return m.Sync(context.Background(), nil, nil)
		},
		"OpenRoute": func() error {
			_, err := m.OpenRoute(context.Background(), nil, "/", "test")
			return err
		},
		"OpenArtifact": func() error {
			_, err := m.OpenArtifact(context.Background(), nil, "/workspace/artifacts/a.html", "test")
			return err
		},
	}

	for name, call := range methods {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panic = %v", r)
				}
			}()
			if err := call(); err == nil || !strings.Contains(err.Error(), "project is nil") {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestManagerMethodsRejectNilProvider(t *testing.T) {
	methods := map[string]func() error{
		"Start": func() error {
			_, err := NewManager(nil).Start(context.Background(), managerProject(), nil, nil)
			return err
		},
		"Status": func() error {
			project := managerProject()
			project.SandboxID = "sandbox-1"
			_, err := NewManager(nil).Status(context.Background(), project)
			return err
		},
		"Sync": func() error {
			project := managerProject()
			project.SandboxID = "sandbox-1"
			return NewManager(nil).Sync(context.Background(), project, nil)
		},
		"OpenRoute": func() error {
			project := managerProject()
			project.SandboxID = "sandbox-1"
			_, err := NewManager(nil).OpenRoute(context.Background(), project, "/", "test")
			return err
		},
		"OpenArtifact": func() error {
			project := managerProject()
			project.SandboxID = "sandbox-1"
			_, err := NewManager(nil).OpenArtifact(context.Background(), project, "/workspace/artifacts/a.html", "test")
			return err
		},
		"NilManager": func() error {
			var m *Manager
			_, err := m.Start(context.Background(), managerProject(), nil, nil)
			return err
		},
		"NilManagerStatus": func() error {
			var m *Manager
			_, err := m.Status(context.Background(), managerProject())
			return err
		},
		"NilManagerSync": func() error {
			var m *Manager
			return m.Sync(context.Background(), managerProject(), nil)
		},
		"NilManagerOpenRoute": func() error {
			var m *Manager
			_, err := m.OpenRoute(context.Background(), managerProject(), "/", "test")
			return err
		},
		"NilManagerOpenArtifact": func() error {
			var m *Manager
			_, err := m.OpenArtifact(context.Background(), managerProject(), "/workspace/artifacts/a.html", "test")
			return err
		},
	}

	for name, call := range methods {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panic = %v", r)
				}
			}()
			if err := call(); err == nil || !strings.Contains(err.Error(), "provider is nil") {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestManagerStartRejectsEmptyCreateID(t *testing.T) {
	fp := newFakeProvider()
	fp.sandboxID = ""
	m := NewManager(fp)
	project := managerProject()

	_, err := m.Start(context.Background(), project, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "empty sandbox id") {
		t.Fatalf("err = %v", err)
	}
	if project.SandboxID != "" {
		t.Fatalf("project sandbox id = %q", project.SandboxID)
	}
}

func TestManagerStartDoesNotAssignSandboxIDOnSyncFailure(t *testing.T) {
	fp := newFakeProvider()
	fp.writeErr = errors.New("sync failed")
	m := NewManager(fp)
	project := managerProject()

	_, err := m.Start(context.Background(), project, map[string]string{"package.json": "{}"}, nil)
	if err == nil || !strings.Contains(err.Error(), "sync sandbox files") {
		t.Fatalf("err = %v", err)
	}
	if project.SandboxID != "" {
		t.Fatalf("project sandbox id = %q", project.SandboxID)
	}
}

func TestManagerStartDoesNotAssignSandboxIDOnProcessFailure(t *testing.T) {
	fp := newFakeProvider()
	fp.startProcessErr = errors.New("process failed")
	m := NewManager(fp)
	project := managerProject()

	_, err := m.Start(context.Background(), project, map[string]string{"package.json": "{}"}, nil)
	if err == nil || !strings.Contains(err.Error(), "start process") {
		t.Fatalf("err = %v", err)
	}
	if project.SandboxID != "" {
		t.Fatalf("project sandbox id = %q", project.SandboxID)
	}
}

func TestManagerSyncRejectsUnsafePaths(t *testing.T) {
	project := managerProject()
	project.SandboxID = "sandbox-1"
	unsafePaths := []string{"", "/abs", "a\x00b", "../secret", "safe/../secret"}

	for _, unsafePath := range unsafePaths {
		t.Run(unsafePath, func(t *testing.T) {
			fp := newFakeProvider()
			m := NewManager(fp)
			err := m.Sync(context.Background(), project, map[string]string{unsafePath: "x"})
			if err == nil || !strings.Contains(err.Error(), "unsafe file path") {
				t.Fatalf("err = %v", err)
			}
			if len(fp.files) != 0 {
				t.Fatalf("synced files = %#v", fp.files)
			}
		})
	}
}

func TestManagerSyncSortsAndNormalizesPaths(t *testing.T) {
	fp := newFakeProvider()
	m := NewManager(fp)
	project := managerProject()
	project.SandboxID = fp.sandboxID

	err := m.Sync(context.Background(), project, map[string]string{
		"./z.txt":        "z",
		"dir//file.txt":  "dir",
		"alpha/file.txt": "alpha",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := []FileDelta{
		{Path: fp.files[0].Path, Content: fp.files[0].Content},
		{Path: fp.files[1].Path, Content: fp.files[1].Content},
		{Path: fp.files[2].Path, Content: fp.files[2].Content},
	}
	want := []FileDelta{
		{Path: "alpha/file.txt", Content: "alpha"},
		{Path: "dir/file.txt", Content: "dir"},
		{Path: "z.txt", Content: "z"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("files = %#v, want %#v", got, want)
	}
}

func TestManagerStatusDegradesOnPreviewFailure(t *testing.T) {
	fp := newFakeProvider()
	fp.previewErr = errors.New("preview unavailable")
	m := NewManager(fp)
	project := managerProject()
	project.SandboxID = fp.sandboxID

	status, err := m.Status(context.Background(), project)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != StateDegraded {
		t.Fatalf("state = %q", status.State)
	}
	if !strings.Contains(status.LastError, "preview unavailable") {
		t.Fatalf("last error = %q", status.LastError)
	}
	if status.RecommendedAction == "" {
		t.Fatalf("recommended action is empty")
	}
}

func TestManagerStatusDegradesOnProcessStateFailure(t *testing.T) {
	fp := newFakeProvider()
	fp.processStatesErr = errors.New("process states unavailable")
	m := NewManager(fp)
	project := managerProject()
	project.SandboxID = fp.sandboxID

	status, err := m.Status(context.Background(), project)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != StateDegraded {
		t.Fatalf("state = %q", status.State)
	}
	if !strings.Contains(status.LastError, "process states unavailable") {
		t.Fatalf("last error = %q", status.LastError)
	}
	if status.RecommendedAction == "" {
		t.Fatalf("recommended action is empty")
	}
}

func TestManagerStatusPreservesStoppedStateOnEnrichmentFailure(t *testing.T) {
	fp := newFakeProvider()
	fp.statusState = StateStopped
	fp.statusLastError = "sandbox stopped"
	fp.previewErr = errors.New("preview unavailable")
	fp.processStatesErr = errors.New("process states unavailable")
	m := NewManager(fp)
	project := managerProject()
	project.SandboxID = fp.sandboxID

	status, err := m.Status(context.Background(), project)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != StateStopped {
		t.Fatalf("state = %q", status.State)
	}
	if !strings.Contains(status.LastError, "sandbox stopped") ||
		!strings.Contains(status.LastError, "preview unavailable") ||
		!strings.Contains(status.LastError, "process states unavailable") {
		t.Fatalf("last error = %q", status.LastError)
	}
	if status.RecommendedAction == "" {
		t.Fatalf("recommended action is empty")
	}
}

func TestManagerOpenRoutePreservesProviderBasePath(t *testing.T) {
	fp := newFakeProvider()
	fp.previews[3000] = "https://host.example.test/sandbox/abc"
	m := NewManager(fp)
	project := managerProject()
	project.Slug = ""
	project.ID = uuid.Nil
	project.SandboxID = fp.sandboxID

	target, err := m.OpenRoute(context.Background(), project, "/checkout", "agent")
	if err != nil {
		t.Fatal(err)
	}
	if target.URL != "https://host.example.test/sandbox/abc/checkout" {
		t.Fatalf("url = %q", target.URL)
	}
}

func TestManagerRestartProcessStopsAndStartsKnownProcess(t *testing.T) {
	fp := newFakeProvider()
	m := NewManager(fp)
	project := managerProject()
	project.SandboxID = fp.sandboxID

	err := m.RestartProcess(context.Background(), project, ProcessFrontend, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fp.stopped) != 1 || fp.stopped[0] != ProcessFrontend {
		t.Fatalf("stopped = %#v", fp.stopped)
	}
	if len(fp.started) != 1 || fp.started[0] != ProcessFrontend {
		t.Fatalf("started = %#v", fp.started)
	}
}

func TestManagerRestartProcessPropagatesEnvToProcess(t *testing.T) {
	fp := newFakeProvider()
	m := NewManager(fp)
	project := managerProject()
	project.SandboxID = fp.sandboxID
	env := map[string]string{"DATABASE_URL": "postgres://example", "NEXT_PUBLIC_FLAG": "on"}

	err := m.RestartProcess(context.Background(), project, ProcessFrontend, env)
	if err != nil {
		t.Fatal(err)
	}
	if len(fp.startedSpecs) != 1 {
		t.Fatalf("started specs = %#v", fp.startedSpecs)
	}
	got := fp.startedSpecs[0].Env
	if got["DATABASE_URL"] != "postgres://example" || got["NEXT_PUBLIC_FLAG"] != "on" {
		t.Fatalf("process env = %#v", got)
	}
}

func TestManagerRestartProcessRejectsUnknownProcess(t *testing.T) {
	fp := newFakeProvider()
	m := NewManager(fp)
	project := managerProject()
	project.SandboxID = fp.sandboxID

	err := m.RestartProcess(context.Background(), project, ProcessName("missing"), nil)
	if err == nil || !strings.Contains(err.Error(), "process missing not found") {
		t.Fatalf("err = %v", err)
	}
	if len(fp.stopped) != 0 || len(fp.started) != 0 {
		t.Fatalf("stopped = %#v started = %#v", fp.stopped, fp.started)
	}
}

func TestManagerProcessLogsReturnsLogsForKnownProcess(t *testing.T) {
	fp := newFakeProvider()
	m := NewManager(fp)
	project := managerProject()
	project.SandboxID = fp.sandboxID

	logs, err := m.ProcessLogs(context.Background(), project, ProcessFrontend, 50)
	if err != nil {
		t.Fatal(err)
	}
	if logs.Process != ProcessFrontend || logs.Content != "ok" {
		t.Fatalf("logs = %#v", logs)
	}
}

func TestManagerProcessLogsRejectsUnknownProcess(t *testing.T) {
	fp := newFakeProvider()
	m := NewManager(fp)
	project := managerProject()
	project.SandboxID = fp.sandboxID

	logs, err := m.ProcessLogs(context.Background(), project, ProcessName("missing"), 50)
	if err == nil || !strings.Contains(err.Error(), "process missing not found") {
		t.Fatalf("err = %v", err)
	}
	if logs != nil {
		t.Fatalf("logs = %#v", logs)
	}
}
