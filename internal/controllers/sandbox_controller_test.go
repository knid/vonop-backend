package controllers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/gitrepo"
	"github.com/knid/wemob/internal/services/sandbox"
)

func TestAddSandboxEnvFilesWritesBackendEnvForSplitWeb(t *testing.T) {
	project := &models.AppProject{
		ID:              uuid.New(),
		ProjectType:     models.ProjectTypeApp,
		FrontendRuntime: models.FrontendRuntimeViteReact,
		BackendEnabled:  true,
		BackendRuntime:  models.BackendRuntimeNextJSAPI,
	}
	files := map[string]string{"backend/package.json": "{}"}
	env := map[string]string{"DATABASE_URL": "postgres://example", "NEXT_PUBLIC_SITE_URL": "https://example.test"}

	addSandboxEnvFiles(project, files, env)

	if _, ok := files[".env.local"]; ok {
		t.Fatalf("root .env.local should not be written for split web")
	}
	if got := files["backend/.env.local"]; got == "" {
		t.Fatalf("backend .env.local was not written")
	} else if !containsAll(got, []string{"DATABASE_URL=postgres://example", "NEXT_PUBLIC_SITE_URL=https://example.test"}) {
		t.Fatalf("backend .env.local = %q", got)
	}
}

func TestAddSandboxEnvFilesWritesRootEnvForSingleNext(t *testing.T) {
	project := &models.AppProject{
		ID:              uuid.New(),
		ProjectType:     models.ProjectTypeApp,
		FrontendRuntime: models.FrontendRuntimeNextJSLegacy,
	}
	files := map[string]string{"package.json": "{}"}

	addSandboxEnvFiles(project, files, map[string]string{"DATABASE_URL": "postgres://example"})

	if got := files[".env.local"]; got != "DATABASE_URL=postgres://example\n" {
		t.Fatalf(".env.local = %q", got)
	}
}

func TestAddSandboxEnvFilesClearsStaleEnvForSplitWeb(t *testing.T) {
	project := &models.AppProject{
		ID:              uuid.New(),
		ProjectType:     models.ProjectTypeApp,
		FrontendRuntime: models.FrontendRuntimeViteReact,
		BackendEnabled:  true,
		BackendRuntime:  models.BackendRuntimeNextJSAPI,
	}
	files := map[string]string{"backend/.env.local": "DATABASE_URL=old\n"}

	addSandboxEnvFiles(project, files, nil)

	if got, ok := files["backend/.env.local"]; !ok || got != "" {
		t.Fatalf("backend .env.local = %q, exists=%v", got, ok)
	}
}

func TestSandboxExecOutputMarksInfrastructureError(t *testing.T) {
	out := sandboxExecOutput("", "", 0, "sandbox-1", errors.New("sandbox is not started"))

	if out["error"] != "sandbox is not started" {
		t.Fatalf("error = %#v", out["error"])
	}
	if out["exit_code"] != -1 {
		t.Fatalf("exit_code = %#v", out["exit_code"])
	}
}

func TestAdoptLatestSandboxIDCopiesPersistedIDWhenCurrentProjectIsStale(t *testing.T) {
	project := &models.AppProject{ID: uuid.New()}
	latest := &models.AppProject{ID: project.ID, SandboxID: "daytona-live"}

	if !adoptLatestSandboxID(project, latest) {
		t.Fatalf("expected sandbox id to be adopted")
	}
	if project.SandboxID != "daytona-live" {
		t.Fatalf("sandbox id = %q", project.SandboxID)
	}
}

func TestAdoptLatestSandboxIDDoesNotOverwriteExistingID(t *testing.T) {
	project := &models.AppProject{ID: uuid.New(), SandboxID: "daytona-current"}
	latest := &models.AppProject{ID: project.ID, SandboxID: "daytona-new"}

	if adoptLatestSandboxID(project, latest) {
		t.Fatalf("did not expect sandbox id to be adopted")
	}
	if project.SandboxID != "daytona-current" {
		t.Fatalf("sandbox id = %q", project.SandboxID)
	}
}

func TestEnsureAgentSandboxStartsAndAttachesMissingSandbox(t *testing.T) {
	ctx := context.Background()
	project := &models.AppProject{
		ID:              uuid.New(),
		ProjectType:     models.ProjectTypeApp,
		FrontendRuntime: models.FrontendRuntimeViteReact,
		Slug:            "todo",
	}
	repos := gitrepo.NewService(t.TempDir())
	repo, err := repos.Init(project.ID.String())
	if err != nil {
		t.Fatalf("init repo: %v", err)
	}
	if err := repo.WriteFile("frontend/package.json", []byte(`{"scripts":{"dev":"vite"}}`)); err != nil {
		t.Fatalf("write repo file: %v", err)
	}

	provider := newControllerFakeSandboxProvider()
	c := &Controller{
		GitRepoService: repos,
		SandboxManager: sandbox.NewManager(provider),
	}

	status, err := c.EnsureAgentSandbox(ctx, project)
	if err != nil {
		t.Fatalf("EnsureAgentSandbox: %v", err)
	}
	if project.SandboxID != "daytona-test" {
		t.Fatalf("project sandbox id = %q", project.SandboxID)
	}
	if provider.startCalls != 1 {
		t.Fatalf("start calls = %d", provider.startCalls)
	}
	if len(provider.files) == 0 {
		t.Fatal("expected source files to be synced")
	}
	if status == nil || status.SandboxID != "daytona-test" {
		t.Fatalf("status = %#v", status)
	}
}

func TestEnsureAgentSandboxSkipsFreshProjectWithoutGeneratedFiles(t *testing.T) {
	ctx := context.Background()
	project := &models.AppProject{
		ID:              uuid.New(),
		ProjectType:     models.ProjectTypeApp,
		FrontendRuntime: models.FrontendRuntimeViteReact,
		Slug:            "fresh",
	}

	provider := newControllerFakeSandboxProvider()
	c := &Controller{
		GitRepoService: gitrepo.NewService(t.TempDir()),
		SandboxManager: sandbox.NewManager(provider),
	}

	status, err := c.EnsureAgentSandbox(ctx, project)
	if err != nil {
		t.Fatalf("EnsureAgentSandbox: %v", err)
	}
	if project.SandboxID != "" {
		t.Fatalf("project sandbox id = %q", project.SandboxID)
	}
	if provider.startCalls != 0 {
		t.Fatalf("start calls = %d, want 0", provider.startCalls)
	}
	if len(provider.files) != 0 {
		t.Fatalf("synced files = %d, want 0", len(provider.files))
	}
	if status == nil || status.State != sandbox.PreviewMissing {
		t.Fatalf("status = %#v", status)
	}
}

func TestSyncSandboxDeltasStartsMissingSandboxBeforeSync(t *testing.T) {
	ctx := context.Background()
	project := &models.AppProject{
		ID:              uuid.New(),
		ProjectType:     models.ProjectTypeApp,
		FrontendRuntime: models.FrontendRuntimeViteReact,
		Slug:            "todo",
	}
	repos := gitrepo.NewService(t.TempDir())
	repo, err := repos.Init(project.ID.String())
	if err != nil {
		t.Fatalf("init repo: %v", err)
	}
	if err := repo.WriteFile("frontend/package.json", []byte(`{"scripts":{"dev":"vite"}}`)); err != nil {
		t.Fatalf("write package: %v", err)
	}
	if err := repo.WriteFile("frontend/src/App.tsx", []byte(`export default function App() { return null }`)); err != nil {
		t.Fatalf("write app: %v", err)
	}

	provider := newControllerFakeSandboxProvider()
	c := &Controller{
		GitRepoService: repos,
		SandboxManager: sandbox.NewManager(provider),
	}

	err = c.SyncSandboxDeltas(ctx, project, []sandbox.FileDelta{
		{Path: "frontend/src/App.tsx", Content: `export default function App() { return null }`},
	})
	if err != nil {
		t.Fatalf("SyncSandboxDeltas: %v", err)
	}
	if project.SandboxID != "daytona-test" {
		t.Fatalf("project sandbox id = %q", project.SandboxID)
	}
	if provider.startCalls == 0 {
		t.Fatal("expected missing sandbox to be started")
	}
	if len(provider.files) == 0 {
		t.Fatal("expected files to be synced")
	}
}

func TestAgentPreviewShouldEnsureWhenPreviewIsSyncStale(t *testing.T) {
	status := &sandbox.PreviewStatus{
		State:     sandbox.PreviewReady,
		SyncStale: true,
	}

	if !agentPreviewShouldEnsure(status) {
		t.Fatal("expected stale ready preview to be ensured")
	}
}

func containsAll(s string, wants []string) bool {
	for _, want := range wants {
		if !strings.Contains(s, want) {
			return false
		}
	}
	return true
}

type controllerFakeSandboxProvider struct {
	sandboxID  string
	files      []sandbox.FileDelta
	startCalls int
}

func newControllerFakeSandboxProvider() *controllerFakeSandboxProvider {
	return &controllerFakeSandboxProvider{sandboxID: "daytona-test"}
}

func (f *controllerFakeSandboxProvider) Create(ctx context.Context, plan sandbox.RuntimePlan) (string, error) {
	return f.sandboxID, nil
}

func (f *controllerFakeSandboxProvider) GetStatus(ctx context.Context, sandboxID string, plan sandbox.RuntimePlan) (*sandbox.Status, error) {
	return &sandbox.Status{
		Provider:    sandbox.ProviderDaytona,
		SandboxID:   sandboxID,
		State:       sandbox.StateReady,
		PreviewURLs: map[sandbox.PreviewKind]string{},
	}, nil
}

func (f *controllerFakeSandboxProvider) Start(ctx context.Context, sandboxID string) error {
	f.startCalls++
	return nil
}

func (f *controllerFakeSandboxProvider) Stop(ctx context.Context, sandboxID string) error {
	return nil
}

func (f *controllerFakeSandboxProvider) Archive(ctx context.Context, sandboxID string) error {
	return nil
}

func (f *controllerFakeSandboxProvider) Delete(ctx context.Context, sandboxID string) error {
	return nil
}

func (f *controllerFakeSandboxProvider) WriteFiles(ctx context.Context, sandboxID string, files []sandbox.FileDelta, plan sandbox.RuntimePlan) error {
	f.files = append(f.files, files...)
	return nil
}

func (f *controllerFakeSandboxProvider) Execute(ctx context.Context, sandboxID, command, cwd string, timeoutMs int) (string, string, int, error) {
	return "", "", 0, nil
}

func (f *controllerFakeSandboxProvider) StartProcess(ctx context.Context, sandboxID string, spec sandbox.ProcessSpec) error {
	return nil
}

func (f *controllerFakeSandboxProvider) StopProcess(ctx context.Context, sandboxID string, name sandbox.ProcessName) error {
	return nil
}

func (f *controllerFakeSandboxProvider) ProcessLogs(ctx context.Context, sandboxID string, spec sandbox.ProcessSpec, lines int) (*sandbox.LogsResult, error) {
	return &sandbox.LogsResult{Process: spec.Name, Content: "ok"}, nil
}

func (f *controllerFakeSandboxProvider) ProcessStates(ctx context.Context, sandboxID string, plan sandbox.RuntimePlan) (map[sandbox.ProcessName]sandbox.ProcessState, error) {
	out := map[sandbox.ProcessName]sandbox.ProcessState{}
	for _, spec := range plan.Processes {
		out[spec.Name] = sandbox.ProcessState{Name: spec.Name, Status: sandbox.ProcessReady, Port: spec.Port}
	}
	return out, nil
}

func (f *controllerFakeSandboxProvider) PreviewURL(ctx context.Context, sandboxID string, port int) (string, error) {
	return fmt.Sprintf("https://provider-%d.example.test", port), nil
}

func (f *controllerFakeSandboxProvider) CreateTerminal(ctx context.Context, sandboxID string, cols, rows int) (*sandbox.Terminal, error) {
	return &sandbox.Terminal{ID: "term-1"}, nil
}

func (f *controllerFakeSandboxProvider) SendTerminalInput(ctx context.Context, sandboxID, terminalID, input string) error {
	return nil
}

func (f *controllerFakeSandboxProvider) CloseTerminal(ctx context.Context, sandboxID, terminalID string) error {
	return nil
}

func (f *controllerFakeSandboxProvider) CreateSnapshot(ctx context.Context, sandboxID string) (*sandbox.Snapshot, error) {
	return &sandbox.Snapshot{ID: "snapshot-1"}, nil
}

func (f *controllerFakeSandboxProvider) RestoreSnapshot(ctx context.Context, sandboxID, snapshotID string) error {
	return nil
}

func (f *controllerFakeSandboxProvider) RunDiagnostics(ctx context.Context, sandboxID string, plan sandbox.RuntimePlan) (*sandbox.DiagnosticsResult, error) {
	return &sandbox.DiagnosticsResult{Summary: "ok"}, nil
}
