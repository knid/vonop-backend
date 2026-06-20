# Daytona Sandbox System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the E2B sandbox system with a backend-owned Daytona sandbox control plane that powers user previews and LLM sandbox tools for all app types.

**Architecture:** The Go backend becomes the single sandbox authority through `internal/services/sandbox`, with Daytona as the only provider implementation. Git/Gitea remains the source of truth; source edits continue through existing Git-backed file tools and then sync into the shared live Daytona sandbox. The frontend replaces `/api/e2b/*` calls with backend `/v2/apps/{appId}/sandbox/*` endpoints and renders backend-managed preview state.

**Tech Stack:** Go, chi, Bun/Postgres, Redis/SSE, Daytona Go SDK (`github.com/daytonaio/daytona/libs/sdk-go`), Next.js frontend, Caddy, existing Git/Gitea project storage.

---

## Scope Check

This is intentionally a big-bang plan. The approved spec spans backend sandbox management, frontend preview hooks, Caddy preview routing, LLM tools, and E2B cleanup. These pieces are coupled by the cutover requirement: once E2B routes are removed, user preview and agent sandbox tools must both use the new Daytona-backed backend control plane.

## File Structure

Backend files:

- Create `internal/services/sandbox/types.go` — provider-neutral sandbox domain types.
- Create `internal/services/sandbox/runtime_plan.go` — runtime plan selection and app-type process definitions.
- Create `internal/services/sandbox/provider.go` — provider interface.
- Create `internal/services/sandbox/manager.go` — high-level orchestration.
- Create `internal/services/sandbox/fake_provider_test.go` — fake provider helpers for manager tests.
- Create `internal/services/sandbox/manager_test.go` — start/sync/process/preview behavior tests.
- Create `internal/services/sandbox/runtime_plan_test.go` — runtime plan coverage for all app types.
- Create `internal/services/sandbox/daytona/provider.go` — Daytona SDK adapter.
- Create `internal/controllers/sandbox_controller.go` — controller methods for sandbox operations.
- Create `internal/views/sandbox_views.go` — authenticated app sandbox HTTP handlers.
- Create `internal/views/preview_proxy_views.go` — public/internal preview resolver and proxy handlers.
- Modify `internal/controllers/controller.go` — replace `E2BService` with `SandboxManager`.
- Modify `internal/bootstrap/bootstrap.go` — construct `SandboxManager` with Daytona provider.
- Modify `cmd/main.go` — replace E2B routes with sandbox routes and preview proxy routes.
- Modify `internal/agent/dependencies.go` — rename sandbox dependency comments and expand request shape.
- Modify `internal/controllers/agent_deps.go` — route agent sandbox calls to `SandboxManager`.
- Modify `internal/agent/tools_sandbox.go` — replace E2B-specific tools with provider-neutral tools.
- Modify `internal/agent/agent_policy.go` — update tool risk policy.
- Modify `internal/agent/prompt_compact.go` — remove E2B wording and document Daytona-backed sandbox roots.
- Modify `internal/controllers/app_build_controller.go` — keep bundle generation but rename E2B comments to sandbox runtime comments.
- Modify `internal/views/app_deploy_views.go` — remove E2B-specific sandbox handlers after replacement.
- Delete `internal/services/e2b/e2b.go`.
- Delete `internal/services/e2b/e2b_test.go`.

Frontend files in `/Users/knid/Projects/wemob/wemob-app`:

- Create `src/hooks/use-sandbox-preview.ts` — backend-backed preview hook.
- Modify `src/components/studio/studio-workspace.tsx` — use `useSandboxPreview`.
- Modify `src/components/studio/preview-view.tsx` — accept new preview hook type.
- Modify `src/components/studio/mobile-preview-view.tsx` — accept new preview hook type.
- Modify `src/components/studio/backend-output-view.tsx` — accept new preview hook type.
- Modify `src/components/studio/no-frontend-view.tsx` — accept new preview hook type.
- Modify `src/lib/api.ts` — add sandbox API methods and remove E2B-specific methods after call sites are gone.
- Delete `src/hooks/use-e2b-preview.ts`.
- Delete `src/app/api/e2b/start/route.ts`.
- Delete `src/app/api/e2b/files/route.ts`.
- Delete `src/app/api/e2b/logs/route.ts`.
- Delete `src/app/api/e2b/agent/[...action]/route.ts`.
- Remove E2B dependency and template scripts from `package.json`.

Proxy/config files:

- Modify `Caddyfile` — route `*.preview.wemob.app` to backend preview proxy.
- Modify `caddy/static/sandbox-expired.html` and `caddy/static/sandbox-expired-shared.html` only if the host parsing assumes `port-sandboxId`.
- Modify frontend `public/sandbox-expired.html` with the same parsing update if still used.

---

### Task 1: Add Daytona Configuration And Dependency

**Files:**
- Modify: `go.mod`
- Modify: `internal/services/secret/keys.go`
- Modify: `internal/bootstrap/bootstrap.go`
- Test: `go.mod`, `go.sum`

- [ ] **Step 1: Add the Daytona Go SDK**

Run:

```bash
go get github.com/daytonaio/daytona/libs/sdk-go
```

Expected: `go.mod` gains `github.com/daytonaio/daytona/libs/sdk-go`, and `go.sum` gains Daytona entries.

- [ ] **Step 2: Add Daytona secret keys**

Modify `internal/services/secret/keys.go` to include:

```go
"DAYTONA_API_KEY",
"DAYTONA_TARGET",
"DAYTONA_PREVIEW_DOMAIN",
```

`DAYTONA_TARGET` can be empty for Daytona default target selection. `DAYTONA_PREVIEW_DOMAIN` is optional and defaults to Daytona's provider proxy domain when empty.

- [ ] **Step 3: Verify dependency resolution**

Run:

```bash
go test ./internal/services/secret ./internal/bootstrap
```

Expected: packages compile or existing unrelated tests pass.

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum internal/services/secret/keys.go internal/bootstrap/bootstrap.go
git commit -m "chore(sandbox): add Daytona configuration"
```

---

### Task 2: Define Provider-Neutral Sandbox Types

**Files:**
- Create: `internal/services/sandbox/types.go`
- Create: `internal/services/sandbox/provider.go`
- Test: compile through later tasks

- [ ] **Step 1: Create `types.go`**

Create `internal/services/sandbox/types.go`:

```go
package sandbox

import "time"

type Provider string

const ProviderDaytona Provider = "daytona"

type State string

const (
	StateMissing    State = "missing"
	StateCreating   State = "creating"
	StateSyncing    State = "syncing"
	StateInstalling State = "installing"
	StateStarting   State = "starting"
	StateReady      State = "ready"
	StateDegraded   State = "degraded"
	StateStopped    State = "stopped"
	StateArchived   State = "archived"
	StateError      State = "error"
)

type ProcessName string

const (
	ProcessFrontend  ProcessName = "frontend"
	ProcessBackend   ProcessName = "backend"
	ProcessExpoWeb   ProcessName = "expo_web"
	ProcessExpoGo    ProcessName = "expo_go"
	ProcessArtifacts ProcessName = "artifacts"
)

type ProcessStatus string

const (
	ProcessStopped  ProcessStatus = "stopped"
	ProcessStarting ProcessStatus = "starting"
	ProcessReady    ProcessStatus = "ready"
	ProcessError    ProcessStatus = "error"
)

type PreviewKind string

const (
	PreviewApp       PreviewKind = "app"
	PreviewBackend   PreviewKind = "backend"
	PreviewExpo      PreviewKind = "expo"
	PreviewArtifacts PreviewKind = "artifacts"
	PreviewRoute     PreviewKind = "route"
)

type RootName string

const (
	RootProject   RootName = "project"
	RootBackend   RootName = "backend"
	RootArtifacts RootName = "artifacts"
	RootTmp       RootName = "tmp"
)

type Root struct {
	Name RootName `json:"name"`
	Path string   `json:"path"`
}

type ReadinessProbe struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

type ProcessSpec struct {
	Name        ProcessName       `json:"name"`
	Command     string            `json:"command"`
	Cwd         string            `json:"cwd"`
	Env         map[string]string `json:"env,omitempty"`
	Port        int               `json:"port"`
	Probe       ReadinessProbe    `json:"probe"`
	LogPath     string            `json:"log_path"`
	AutoRestart bool              `json:"auto_restart"`
}

type RuntimePlan struct {
	AppID          string                 `json:"app_id"`
	ProjectType   string                 `json:"project_type"`
	FrontendRuntime string               `json:"frontend_runtime"`
	BackendEnabled bool                  `json:"backend_enabled"`
	BackendRuntime string                `json:"backend_runtime"`
	Roots          []Root                `json:"roots"`
	Processes      []ProcessSpec         `json:"processes"`
	DefaultPreview PreviewKind           `json:"default_preview"`
	Previews       map[PreviewKind]int   `json:"previews"`
	Metadata       map[string]string     `json:"metadata,omitempty"`
}

type ProcessState struct {
	Name         ProcessName   `json:"name"`
	Status       ProcessStatus `json:"status"`
	Port         int           `json:"port"`
	URL          string        `json:"url,omitempty"`
	LastExitCode *int          `json:"last_exit_code,omitempty"`
	LastError    string        `json:"last_error,omitempty"`
	StartedAt    *time.Time    `json:"started_at,omitempty"`
}

type PreviewTarget struct {
	URL       string      `json:"url"`
	Label     string      `json:"label"`
	Kind      PreviewKind `json:"kind"`
	SetBy     string      `json:"set_by"`
	UpdatedAt time.Time   `json:"updated_at"`
}

type Status struct {
	Provider       Provider                 `json:"provider"`
	SandboxID      string                   `json:"sandbox_id"`
	State          State                    `json:"state"`
	LastSyncCommit string                   `json:"last_sync_commit,omitempty"`
	LastError      string                   `json:"last_error,omitempty"`
	ActivePreview  *PreviewTarget          `json:"active_preview,omitempty"`
	PreviewURLs    map[PreviewKind]string  `json:"preview_urls,omitempty"`
	Processes      map[ProcessName]ProcessState `json:"processes,omitempty"`
	RecommendedAction string                `json:"recommended_action,omitempty"`
}

type FileDelta struct {
	Path    string `json:"path"`
	Content string `json:"content,omitempty"`
	Delete  bool   `json:"delete,omitempty"`
}

type LogsResult struct {
	Process ProcessName `json:"process"`
	Content string      `json:"content"`
}

type Terminal struct {
	ID string `json:"id"`
}

type Snapshot struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
}

type DiagnosticsResult struct {
	Summary string   `json:"summary"`
	Items   []string `json:"items"`
}
```

- [ ] **Step 2: Create `provider.go`**

Create `internal/services/sandbox/provider.go`:

```go
package sandbox

import (
	"context"

	"github.com/knid/wemob/internal/models"
)

type ProviderClient interface {
	Create(ctx context.Context, project *models.AppProject, plan RuntimePlan) (string, error)
	GetStatus(ctx context.Context, sandboxID string, plan RuntimePlan) (*Status, error)
	Start(ctx context.Context, sandboxID string) error
	Stop(ctx context.Context, sandboxID string) error
	Archive(ctx context.Context, sandboxID string) error
	Delete(ctx context.Context, sandboxID string) error

	WriteFiles(ctx context.Context, sandboxID string, files []FileDelta, plan RuntimePlan) error
	Execute(ctx context.Context, sandboxID, command, cwd string, timeoutMs int) (stdout, stderr string, exitCode int, err error)

	StartProcess(ctx context.Context, sandboxID string, spec ProcessSpec) error
	StopProcess(ctx context.Context, sandboxID string, name ProcessName) error
	ProcessLogs(ctx context.Context, sandboxID string, spec ProcessSpec, lines int) (*LogsResult, error)
	ProcessStates(ctx context.Context, sandboxID string, plan RuntimePlan) (map[ProcessName]ProcessState, error)

	PreviewURL(ctx context.Context, sandboxID string, port int) (string, error)

	CreateTerminal(ctx context.Context, sandboxID string, cols, rows int) (*Terminal, error)
	SendTerminalInput(ctx context.Context, sandboxID, terminalID, input string) error
	CloseTerminal(ctx context.Context, sandboxID, terminalID string) error

	CreateSnapshot(ctx context.Context, sandboxID string) (*Snapshot, error)
	RestoreSnapshot(ctx context.Context, sandboxID, snapshotID string) error
	RunDiagnostics(ctx context.Context, sandboxID string, plan RuntimePlan) (*DiagnosticsResult, error)
}
```

- [ ] **Step 3: Compile the new package**

Run:

```bash
go test ./internal/services/sandbox
```

Expected: package compiles with no test files.

- [ ] **Step 4: Commit**

```bash
git add internal/services/sandbox/types.go internal/services/sandbox/provider.go
git commit -m "feat(sandbox): define provider-neutral sandbox types"
```

---

### Task 3: Implement Runtime Plans For All App Types

**Files:**
- Create: `internal/services/sandbox/runtime_plan.go`
- Create: `internal/services/sandbox/runtime_plan_test.go`

- [ ] **Step 1: Write runtime plan tests**

Create `internal/services/sandbox/runtime_plan_test.go`:

```go
package sandbox

import (
	"testing"

	"github.com/knid/wemob/internal/models"
	"github.com/google/uuid"
)

func testProject() *models.AppProject {
	return &models.AppProject{ID: uuid.New()}
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
	p.ProjectType = models.ProjectTypeWebsite
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
```

- [ ] **Step 2: Run tests and verify failure**

Run:

```bash
go test ./internal/services/sandbox -run TestRuntimePlan -v
```

Expected: FAIL because `BuildRuntimePlan` is undefined.

- [ ] **Step 3: Implement runtime plans**

Create `internal/services/sandbox/runtime_plan.go`:

```go
package sandbox

import "github.com/knid/wemob/internal/models"

const (
	ProjectRoot   = "/workspace/project"
	BackendRoot   = "/workspace/backend"
	ArtifactsRoot = "/workspace/artifacts"
	TmpRoot       = "/tmp"
)

func BuildRuntimePlan(project *models.AppProject) RuntimePlan {
	plan := RuntimePlan{
		AppID:           project.ID.String(),
		ProjectType:     project.ProjectType,
		FrontendRuntime: project.FrontendRuntime,
		BackendEnabled:  project.BackendEnabled,
		BackendRuntime:  project.BackendRuntime,
		Roots: []Root{
			{Name: RootProject, Path: ProjectRoot},
			{Name: RootBackend, Path: BackendRoot},
			{Name: RootArtifacts, Path: ArtifactsRoot},
			{Name: RootTmp, Path: TmpRoot},
		},
		Previews: map[PreviewKind]int{},
	}

	switch {
	case project.ProjectType == models.ProjectTypeMobileAppNative:
		plan.DefaultPreview = PreviewApp
		plan.Previews[PreviewApp] = 8081
		plan.Previews[PreviewExpo] = 19000
		plan.Processes = append(plan.Processes,
			ProcessSpec{
				Name: ProcessExpoWeb,
				Command: "export NODE_OPTIONS='--max-old-space-size=3072' && CI=1 npx expo start --web --clear --port 8081",
				Cwd: ProjectRoot,
				Port: 8081,
				Probe: ReadinessProbe{Type: "http", URL: "http://localhost:8081"},
				LogPath: "/tmp/wemob-expo-web.log",
				AutoRestart: true,
			},
			ProcessSpec{
				Name: ProcessExpoGo,
				Command: "export EXPO_GO_MODE=1 && export NODE_OPTIONS='--max-old-space-size=3072' && CI=1 npx expo start --clear --port 19000",
				Cwd: ProjectRoot,
				Port: 19000,
				Probe: ReadinessProbe{Type: "http", URL: "http://localhost:19000"},
				LogPath: "/tmp/wemob-expo-go.log",
				AutoRestart: true,
			},
		)
	case project.FrontendRuntime == models.FrontendRuntimeNone && project.BackendEnabled:
		plan.DefaultPreview = PreviewBackend
		plan.Previews[PreviewBackend] = 3001
		plan.Processes = append(plan.Processes, backendProcess())
	case project.UsesSplitWebRuntime():
		plan.DefaultPreview = PreviewApp
		plan.Previews[PreviewApp] = 3000
		if project.BackendEnabled && project.BackendRuntime == models.BackendRuntimeNextJSAPI {
			plan.Previews[PreviewBackend] = 3001
			plan.Processes = append(plan.Processes, backendProcess())
		}
		plan.Processes = append(plan.Processes, ProcessSpec{
			Name: ProcessFrontend,
			Command: "export NODE_OPTIONS='--max-old-space-size=3072' && npm run dev -- --config vite.config.preview.ts --host 0.0.0.0 --port 3000",
			Cwd: ProjectRoot,
			Port: 3000,
			Probe: ReadinessProbe{Type: "http", URL: "http://localhost:3000"},
			LogPath: "/tmp/wemob-frontend.log",
			AutoRestart: true,
		})
	default:
		plan.DefaultPreview = PreviewApp
		plan.Previews[PreviewApp] = 3000
		plan.Processes = append(plan.Processes, ProcessSpec{
			Name: ProcessFrontend,
			Command: "export NODE_OPTIONS='--max-old-space-size=3072' && npm run dev -- --hostname 0.0.0.0 --port 3000",
			Cwd: ProjectRoot,
			Port: 3000,
			Probe: ReadinessProbe{Type: "http", URL: "http://localhost:3000"},
			LogPath: "/tmp/wemob-frontend.log",
			AutoRestart: true,
		})
	}

	plan.Previews[PreviewArtifacts] = 7000
	plan.Processes = append(plan.Processes, ProcessSpec{
		Name: ProcessArtifacts,
		Command: "python3 -m http.server 7000",
		Cwd: ArtifactsRoot,
		Port: 7000,
		Probe: ReadinessProbe{Type: "http", URL: "http://localhost:7000"},
		LogPath: "/tmp/wemob-artifacts.log",
		AutoRestart: false,
	})
	return plan
}

func backendProcess() ProcessSpec {
	return ProcessSpec{
		Name: ProcessBackend,
		Command: "export NODE_OPTIONS='--max-old-space-size=3072' && npm run dev -- -p 3001",
		Cwd: BackendRoot,
		Port: 3001,
		Probe: ReadinessProbe{Type: "http", URL: "http://localhost:3001"},
		LogPath: "/tmp/wemob-backend.log",
		AutoRestart: true,
	}
}
```

- [ ] **Step 4: Run tests**

Run:

```bash
go test ./internal/services/sandbox -run TestRuntimePlan -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/services/sandbox/runtime_plan.go internal/services/sandbox/runtime_plan_test.go
git commit -m "feat(sandbox): add runtime plans"
```

---

### Task 4: Implement Sandbox Manager With Fake Provider Tests

**Files:**
- Create: `internal/services/sandbox/manager.go`
- Create: `internal/services/sandbox/manager_test.go`
- Create: `internal/services/sandbox/fake_provider_test.go`

- [ ] **Step 1: Write fake provider**

Create `internal/services/sandbox/fake_provider_test.go`:

```go
package sandbox

import (
	"context"
	"fmt"

	"github.com/knid/wemob/internal/models"
)

type fakeProvider struct {
	sandboxID string
	files     []FileDelta
	started   []ProcessName
	previews  map[int]string
}

func newFakeProvider() *fakeProvider {
	return &fakeProvider{sandboxID: "daytona-test", previews: map[int]string{}}
}

func (f *fakeProvider) Create(ctx context.Context, project *models.AppProject, plan RuntimePlan) (string, error) {
	return f.sandboxID, nil
}
func (f *fakeProvider) GetStatus(ctx context.Context, sandboxID string, plan RuntimePlan) (*Status, error) {
	return &Status{Provider: ProviderDaytona, SandboxID: sandboxID, State: StateReady, PreviewURLs: map[PreviewKind]string{}}, nil
}
func (f *fakeProvider) Start(ctx context.Context, sandboxID string) error { return nil }
func (f *fakeProvider) Stop(ctx context.Context, sandboxID string) error { return nil }
func (f *fakeProvider) Archive(ctx context.Context, sandboxID string) error { return nil }
func (f *fakeProvider) Delete(ctx context.Context, sandboxID string) error { return nil }
func (f *fakeProvider) WriteFiles(ctx context.Context, sandboxID string, files []FileDelta, plan RuntimePlan) error {
	f.files = append(f.files, files...)
	return nil
}
func (f *fakeProvider) Execute(ctx context.Context, sandboxID, command, cwd string, timeoutMs int) (string, string, int, error) {
	return "", "", 0, nil
}
func (f *fakeProvider) StartProcess(ctx context.Context, sandboxID string, spec ProcessSpec) error {
	f.started = append(f.started, spec.Name)
	return nil
}
func (f *fakeProvider) StopProcess(ctx context.Context, sandboxID string, name ProcessName) error { return nil }
func (f *fakeProvider) ProcessLogs(ctx context.Context, sandboxID string, spec ProcessSpec, lines int) (*LogsResult, error) {
	return &LogsResult{Process: spec.Name, Content: "ok"}, nil
}
func (f *fakeProvider) ProcessStates(ctx context.Context, sandboxID string, plan RuntimePlan) (map[ProcessName]ProcessState, error) {
	out := map[ProcessName]ProcessState{}
	for _, spec := range plan.Processes {
		out[spec.Name] = ProcessState{Name: spec.Name, Status: ProcessReady, Port: spec.Port}
	}
	return out, nil
}
func (f *fakeProvider) PreviewURL(ctx context.Context, sandboxID string, port int) (string, error) {
	if f.previews[port] == "" {
		f.previews[port] = fmt.Sprintf("https://provider-%d.example.test", port)
	}
	return f.previews[port], nil
}
func (f *fakeProvider) CreateTerminal(ctx context.Context, sandboxID string, cols, rows int) (*Terminal, error) {
	return &Terminal{ID: "term-1"}, nil
}
func (f *fakeProvider) SendTerminalInput(ctx context.Context, sandboxID, terminalID, input string) error { return nil }
func (f *fakeProvider) CloseTerminal(ctx context.Context, sandboxID, terminalID string) error { return nil }
func (f *fakeProvider) CreateSnapshot(ctx context.Context, sandboxID string) (*Snapshot, error) { return &Snapshot{ID: "snap-1"}, nil }
func (f *fakeProvider) RestoreSnapshot(ctx context.Context, sandboxID, snapshotID string) error { return nil }
func (f *fakeProvider) RunDiagnostics(ctx context.Context, sandboxID string, plan RuntimePlan) (*DiagnosticsResult, error) {
	return &DiagnosticsResult{Summary: "ok"}, nil
}
```

- [ ] **Step 2: Write manager tests**

Create `internal/services/sandbox/manager_test.go`:

```go
package sandbox

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
)

func managerProject() *models.AppProject {
	return &models.AppProject{ID: uuid.New(), ProjectType: models.ProjectTypeWebsite}
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
```

- [ ] **Step 3: Run tests and verify failure**

Run:

```bash
go test ./internal/services/sandbox -run TestManager -v
```

Expected: FAIL because `NewManager`, `Start`, and `OpenRoute` are undefined.

- [ ] **Step 4: Implement manager**

Create `internal/services/sandbox/manager.go`:

```go
package sandbox

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"sort"
	"time"

	"github.com/knid/wemob/internal/models"
)

type Manager struct {
	provider ProviderClient
}

func NewManager(provider ProviderClient) *Manager {
	return &Manager{provider: provider}
}

func (m *Manager) Start(ctx context.Context, project *models.AppProject, files map[string]string, env map[string]string) (*Status, error) {
	plan := BuildRuntimePlan(project)
	sandboxID := project.SandboxID
	var err error
	if sandboxID == "" {
		sandboxID, err = m.provider.Create(ctx, project, plan)
		if err != nil {
			return nil, fmt.Errorf("create sandbox: %w", err)
		}
		project.SandboxID = sandboxID
	} else if err := m.provider.Start(ctx, sandboxID); err != nil {
		return nil, fmt.Errorf("start sandbox: %w", err)
	}
	if err := m.Sync(ctx, project, files); err != nil {
		return nil, err
	}
	for _, spec := range plan.Processes {
		if spec.Name == ProcessArtifacts {
			continue
		}
		if err := m.provider.StartProcess(ctx, sandboxID, spec); err != nil {
			return nil, fmt.Errorf("start process %s: %w", spec.Name, err)
		}
	}
	return m.Status(ctx, project)
}

func (m *Manager) Status(ctx context.Context, project *models.AppProject) (*Status, error) {
	if project.SandboxID == "" {
		return &Status{Provider: ProviderDaytona, State: StateMissing}, nil
	}
	plan := BuildRuntimePlan(project)
	st, err := m.provider.GetStatus(ctx, project.SandboxID, plan)
	if err != nil {
		return nil, err
	}
	st.PreviewURLs = map[PreviewKind]string{}
	kinds := make([]PreviewKind, 0, len(plan.Previews))
	for kind := range plan.Previews {
		kinds = append(kinds, kind)
	}
	sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })
	for _, kind := range kinds {
		u, err := m.provider.PreviewURL(ctx, project.SandboxID, plan.Previews[kind])
		if err == nil {
			st.PreviewURLs[kind] = u
		}
	}
	processes, err := m.provider.ProcessStates(ctx, project.SandboxID, plan)
	if err == nil {
		st.Processes = processes
	}
	return st, nil
}

func (m *Manager) Sync(ctx context.Context, project *models.AppProject, files map[string]string) error {
	if project.SandboxID == "" {
		return fmt.Errorf("sandbox is not started")
	}
	deltas := make([]FileDelta, 0, len(files))
	for p, content := range files {
		deltas = append(deltas, FileDelta{Path: p, Content: content})
	}
	sort.Slice(deltas, func(i, j int) bool { return deltas[i].Path < deltas[j].Path })
	if err := m.provider.WriteFiles(ctx, project.SandboxID, deltas, BuildRuntimePlan(project)); err != nil {
		return fmt.Errorf("sync sandbox files: %w", err)
	}
	return nil
}

func (m *Manager) OpenRoute(ctx context.Context, project *models.AppProject, route, setBy string) (*PreviewTarget, error) {
	if project.SandboxID == "" {
		return nil, fmt.Errorf("sandbox is not started")
	}
	if route == "" {
		route = "/"
	}
	if route[0] != '/' {
		route = "/" + route
	}
	plan := BuildRuntimePlan(project)
	port, ok := plan.Previews[plan.DefaultPreview]
	if !ok {
		return nil, fmt.Errorf("default preview is unavailable")
	}
	base, err := m.provider.PreviewURL(ctx, project.SandboxID, port)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	u.Path = path.Clean(route)
	if route == "/" {
		u.Path = "/"
	}
	u.RawQuery = ""
	u.Fragment = ""
	return &PreviewTarget{
		URL: u.String(),
		Label: "App preview",
		Kind: PreviewRoute,
		SetBy: setBy,
		UpdatedAt: time.Now().UTC(),
	}, nil
}
```

- [ ] **Step 5: Run manager tests**

Run:

```bash
go test ./internal/services/sandbox -run 'TestRuntimePlan|TestManager' -v
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/services/sandbox
git commit -m "feat(sandbox): add sandbox manager"
```

---

### Task 5: Add Daytona Provider Adapter

**Files:**
- Create: `internal/services/sandbox/daytona/provider.go`
- Test: `go test ./internal/services/sandbox/...`

- [ ] **Step 1: Create Daytona provider skeleton**

Create `internal/services/sandbox/daytona/provider.go`:

```go
package daytona

import (
	"context"
	"fmt"
	"strings"

	daytonasdk "github.com/daytonaio/daytona/libs/sdk-go/pkg/daytona"
	"github.com/daytonaio/daytona/libs/sdk-go/pkg/types"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/sandbox"
)

type Provider struct {
	client        *daytonasdk.Client
	previewDomain string
}

func New(apiKey, target, previewDomain string) (*Provider, error) {
	cfg := &types.DaytonaConfig{APIKey: apiKey}
	if strings.TrimSpace(target) != "" {
		cfg.Target = target
	}
	client, err := daytonasdk.NewClientWithConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &Provider{client: client, previewDomain: strings.TrimSpace(previewDomain)}, nil
}

func (p *Provider) Create(ctx context.Context, project *models.AppProject, plan sandbox.RuntimePlan) (string, error) {
	sb, err := p.client.Create(ctx, nil)
	if err != nil {
		return "", err
	}
	return sb.ID, nil
}

func (p *Provider) get(ctx context.Context, sandboxID string) (*daytonasdk.Sandbox, error) {
	if sandboxID == "" {
		return nil, fmt.Errorf("sandbox id is required")
	}
	return p.client.Get(ctx, sandboxID)
}
```

- [ ] **Step 2: Implement lifecycle and preview methods**

Append to `internal/services/sandbox/daytona/provider.go`:

```go
func (p *Provider) GetStatus(ctx context.Context, sandboxID string, plan sandbox.RuntimePlan) (*sandbox.Status, error) {
	sb, err := p.get(ctx, sandboxID)
	if err != nil {
		return nil, err
	}
	state := sandbox.StateReady
	switch strings.ToLower(string(sb.State)) {
	case "stopped":
		state = sandbox.StateStopped
	case "archived":
		state = sandbox.StateArchived
	case "error":
		state = sandbox.StateError
	}
	out := &sandbox.Status{
		Provider: sandbox.ProviderDaytona,
		SandboxID: sb.ID,
		State: state,
	}
	if sb.ErrorReason != nil {
		out.LastError = *sb.ErrorReason
	}
	return out, nil
}

func (p *Provider) Start(ctx context.Context, sandboxID string) error {
	sb, err := p.get(ctx, sandboxID)
	if err != nil {
		return err
	}
	return sb.Start(ctx)
}

func (p *Provider) Stop(ctx context.Context, sandboxID string) error {
	sb, err := p.get(ctx, sandboxID)
	if err != nil {
		return err
	}
	return sb.Stop(ctx)
}

func (p *Provider) Archive(ctx context.Context, sandboxID string) error {
	sb, err := p.get(ctx, sandboxID)
	if err != nil {
		return err
	}
	return sb.Archive(ctx)
}

func (p *Provider) Delete(ctx context.Context, sandboxID string) error {
	sb, err := p.get(ctx, sandboxID)
	if err != nil {
		return err
	}
	return sb.Delete(ctx)
}

func (p *Provider) PreviewURL(ctx context.Context, sandboxID string, port int) (string, error) {
	if port <= 0 {
		return "", fmt.Errorf("port is required")
	}
	if p.previewDomain != "" {
		return fmt.Sprintf("https://%d-%s.%s", port, sandboxID, p.previewDomain), nil
	}
	sb, err := p.get(ctx, sandboxID)
	if err != nil {
		return "", err
	}
	return sb.GetPreviewLink(port), nil
}
```

- [ ] **Step 3: Implement filesystem and process methods**

Append:

```go
func (p *Provider) WriteFiles(ctx context.Context, sandboxID string, files []sandbox.FileDelta, plan sandbox.RuntimePlan) error {
	sb, err := p.get(ctx, sandboxID)
	if err != nil {
		return err
	}
	for _, f := range files {
		target, err := mapSourcePath(plan, f.Path)
		if err != nil {
			return err
		}
		if f.Delete {
			_, err = sb.Process.ExecuteCommand(ctx, "rm -f "+shellQuote(target))
			if err != nil {
				return err
			}
			continue
		}
		dir := target[:strings.LastIndex(target, "/")]
		if _, err := sb.Process.ExecuteCommand(ctx, "mkdir -p "+shellQuote(dir)); err != nil {
			return err
		}
		if err := sb.FileSystem.WriteFile(ctx, target, []byte(f.Content)); err != nil {
			return err
		}
	}
	return nil
}

func mapSourcePath(plan sandbox.RuntimePlan, p string) (string, error) {
	p = strings.TrimPrefix(p, "/")
	if strings.Contains(p, "..") || strings.Contains(p, "\x00") {
		return "", fmt.Errorf("invalid source path")
	}
	if plan.ProjectType == models.ProjectTypeMobileAppNative && strings.HasPrefix(p, "mobile/") {
		return sandbox.ProjectRoot + "/" + strings.TrimPrefix(p, "mobile/"), nil
	}
	if plan.FrontendRuntime == models.FrontendRuntimeViteReact || plan.FrontendRuntime == models.FrontendRuntimeNone {
		if strings.HasPrefix(p, "frontend/") {
			return sandbox.ProjectRoot + "/" + strings.TrimPrefix(p, "frontend/"), nil
		}
		if strings.HasPrefix(p, "backend/") {
			return sandbox.BackendRoot + "/" + strings.TrimPrefix(p, "backend/"), nil
		}
	}
	return sandbox.ProjectRoot + "/" + p, nil
}

func (p *Provider) Execute(ctx context.Context, sandboxID, command, cwd string, timeoutMs int) (string, string, int, error) {
	sb, err := p.get(ctx, sandboxID)
	if err != nil {
		return "", "", 0, err
	}
	res, err := sb.Process.ExecuteCommand(ctx, command, types.ExecuteCommandOptions{Cwd: cwd, Timeout: timeoutMs})
	if err != nil {
		return "", "", 0, err
	}
	return res.Result, "", int(res.ExitCode), nil
}

func (p *Provider) StartProcess(ctx context.Context, sandboxID string, spec sandbox.ProcessSpec) error {
	sb, err := p.get(ctx, sandboxID)
	if err != nil {
		return err
	}
	cmd := fmt.Sprintf("mkdir -p %s && cd %s && (%s) > %s 2>&1 &", shellQuote(spec.Cwd), shellQuote(spec.Cwd), spec.Command, shellQuote(spec.LogPath))
	_, err = sb.Process.ExecuteCommand(ctx, cmd)
	return err
}

func (p *Provider) StopProcess(ctx context.Context, sandboxID string, name sandbox.ProcessName) error {
	sb, err := p.get(ctx, sandboxID)
	if err != nil {
		return err
	}
	_, err = sb.Process.ExecuteCommand(ctx, "pkill -f "+shellQuote(string(name))+" || true")
	return err
}

func (p *Provider) ProcessLogs(ctx context.Context, sandboxID string, spec sandbox.ProcessSpec, lines int) (*sandbox.LogsResult, error) {
	if lines <= 0 {
		lines = 160
	}
	sb, err := p.get(ctx, sandboxID)
	if err != nil {
		return nil, err
	}
	res, err := sb.Process.ExecuteCommand(ctx, fmt.Sprintf("test -f %s && tail -n %d %s || true", shellQuote(spec.LogPath), lines, shellQuote(spec.LogPath)))
	if err != nil {
		return nil, err
	}
	return &sandbox.LogsResult{Process: spec.Name, Content: res.Result}, nil
}

func (p *Provider) ProcessStates(ctx context.Context, sandboxID string, plan sandbox.RuntimePlan) (map[sandbox.ProcessName]sandbox.ProcessState, error) {
	out := map[sandbox.ProcessName]sandbox.ProcessState{}
	for _, spec := range plan.Processes {
		out[spec.Name] = sandbox.ProcessState{Name: spec.Name, Status: sandbox.ProcessStopped, Port: spec.Port}
	}
	return out, nil
}

func shellQuote(v string) string {
	return "'" + strings.ReplaceAll(v, "'", "'\\''") + "'"
}
```

If the Daytona SDK method names differ from the docs version installed by `go get`, adjust only this adapter file. Keep the provider-neutral interface unchanged.

- [ ] **Step 4: Implement terminal/snapshot/diagnostics wrappers**

Append:

```go
func (p *Provider) CreateTerminal(ctx context.Context, sandboxID string, cols, rows int) (*sandbox.Terminal, error) {
	if cols <= 0 {
		cols = 120
	}
	if rows <= 0 {
		rows = 30
	}
	sb, err := p.get(ctx, sandboxID)
	if err != nil {
		return nil, err
	}
	id := fmt.Sprintf("wemob-%d", time.Now().UnixNano())
	_, err = sb.Process.CreatePty(ctx, id, types.PtySize{Cols: cols, Rows: rows})
	if err != nil {
		return nil, err
	}
	return &sandbox.Terminal{ID: id}, nil
}

func (p *Provider) SendTerminalInput(ctx context.Context, sandboxID, terminalID, input string) error {
	sb, err := p.get(ctx, sandboxID)
	if err != nil {
		return err
	}
	handle, err := sb.Process.GetPty(ctx, terminalID)
	if err != nil {
		return err
	}
	return handle.SendInput([]byte(input))
}

func (p *Provider) CloseTerminal(ctx context.Context, sandboxID, terminalID string) error {
	sb, err := p.get(ctx, sandboxID)
	if err != nil {
		return err
	}
	handle, err := sb.Process.GetPty(ctx, terminalID)
	if err != nil {
		return err
	}
	return handle.Kill(ctx)
}

func (p *Provider) CreateSnapshot(ctx context.Context, sandboxID string) (*sandbox.Snapshot, error) {
	sb, err := p.get(ctx, sandboxID)
	if err != nil {
		return nil, err
	}
	id, err := sb.CreateSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	return &sandbox.Snapshot{ID: id, CreatedAt: time.Now().UTC()}, nil
}

func (p *Provider) RestoreSnapshot(ctx context.Context, sandboxID, snapshotID string) error {
	return fmt.Errorf("snapshot restore requires sandbox recreation from snapshot %q", snapshotID)
}

func (p *Provider) RunDiagnostics(ctx context.Context, sandboxID string, plan sandbox.RuntimePlan) (*sandbox.DiagnosticsResult, error) {
	sb, err := p.get(ctx, sandboxID)
	if err != nil {
		return nil, err
	}
	items := []string{}
	for _, spec := range plan.Processes {
		res, err := sb.Process.ExecuteCommand(ctx, fmt.Sprintf("cd %s && npm run typecheck --if-present", shellQuote(spec.Cwd)))
		if err == nil && res.ExitCode != 0 {
			items = append(items, fmt.Sprintf("%s: %s", spec.Name, res.Result))
		}
	}
	if len(items) == 0 {
		return &sandbox.DiagnosticsResult{Summary: "No diagnostics reported"}, nil
	}
	return &sandbox.DiagnosticsResult{Summary: "Diagnostics reported issues", Items: items}, nil
}
```

Add missing imports `time` if not already present.

- [ ] **Step 5: Compile provider**

Run:

```bash
go test ./internal/services/sandbox/...
```

Expected: compile errors may identify exact Daytona SDK method names. Fix only `internal/services/sandbox/daytona/provider.go` until tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/services/sandbox/daytona/provider.go
git commit -m "feat(sandbox): add Daytona provider"
```

---

### Task 6: Wire SandboxManager Into Backend Bootstrap

**Files:**
- Modify: `internal/controllers/controller.go`
- Modify: `internal/bootstrap/bootstrap.go`
- Modify: `CLAUDE.md`
- Test: `go test ./internal/controllers ./internal/bootstrap`

- [ ] **Step 1: Replace controller dependency**

In `internal/controllers/controller.go`, replace:

```go
e2bService "github.com/knid/wemob/internal/services/e2b"
```

with:

```go
sandboxService "github.com/knid/wemob/internal/services/sandbox"
```

Replace the field:

```go
E2BService *e2bService.Service
```

with:

```go
SandboxManager *sandboxService.Manager
```

Replace constructor argument:

```go
e2b *e2bService.Service,
```

with:

```go
sandboxManager *sandboxService.Manager,
```

Replace assignment:

```go
E2BService: e2b,
```

with:

```go
SandboxManager: sandboxManager,
```

- [ ] **Step 2: Wire Daytona provider in bootstrap**

In `internal/bootstrap/bootstrap.go`, replace the E2B import with:

```go
sandboxService "github.com/knid/wemob/internal/services/sandbox"
daytonaSandbox "github.com/knid/wemob/internal/services/sandbox/daytona"
```

Replace the E2B service construction with:

```go
daytonaProvider, err := daytonaSandbox.New(
	sm.GetSecret("DAYTONA_API_KEY"),
	sm.GetSecret("DAYTONA_TARGET"),
	sm.GetSecret("DAYTONA_PREVIEW_DOMAIN"),
)
if err != nil {
	slog.Warn("daytona sandbox provider not configured", "error", err)
}
var sandboxManager *sandboxService.Manager
if daytonaProvider != nil {
	sandboxManager = sandboxService.NewManager(daytonaProvider)
}
```

Pass `sandboxManager` into `controllers.NewController`.

- [ ] **Step 3: Update repo guidance**

In `CLAUDE.md`, replace:

```text
- `e2b.Service` — E2B sandbox environments
```

with:

```text
- `sandbox.Manager` — Daytona-backed sandbox preview and agent runtime control
```

- [ ] **Step 4: Compile**

Run:

```bash
go test ./internal/controllers ./internal/bootstrap
```

Expected: compile errors point to remaining `E2BService` references. Leave route/controller method cleanup for later tasks, but fix constructor signature errors now.

- [ ] **Step 5: Commit**

```bash
git add internal/controllers/controller.go internal/bootstrap/bootstrap.go CLAUDE.md
git commit -m "feat(sandbox): wire Daytona sandbox manager"
```

---

### Task 7: Add Backend Sandbox Controllers And Views

**Files:**
- Create: `internal/controllers/sandbox_controller.go`
- Create: `internal/views/sandbox_views.go`
- Modify: `cmd/main.go`
- Test: `go test ./internal/controllers ./internal/views`

- [ ] **Step 1: Add controller methods**

Create `internal/controllers/sandbox_controller.go`:

```go
package controllers

import (
	"context"
	"fmt"
	"time"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/sandbox"
)

func (c *Controller) SandboxStatus(ctx context.Context, project *models.AppProject) (*sandbox.Status, error) {
	if c.SandboxManager == nil {
		return nil, fmt.Errorf("sandbox manager is not configured")
	}
	return c.SandboxManager.Status(ctx, project)
}

func (c *Controller) StartSandbox(ctx context.Context, project *models.AppProject) (*sandbox.Status, error) {
	if c.SandboxManager == nil {
		return nil, fmt.Errorf("sandbox manager is not configured")
	}
	bundle, err := c.GetPreviewBundle(project)
	if err != nil {
		return nil, err
	}
	files, _ := bundle["files"].(map[string]string)
	if files == nil {
		files = map[string]string{}
	}
	envVars, _ := bundle["env_vars"].(map[string]string)
	status, err := c.SandboxManager.Start(ctx, project, files, envVars)
	if err != nil {
		return nil, err
	}
	if project.SandboxID != "" {
		_ = c.DB.UpdateAppProjectSandboxID(project.ID.String(), project.SandboxID)
	}
	return status, nil
}

func (c *Controller) SyncSandbox(ctx context.Context, project *models.AppProject) error {
	if c.SandboxManager == nil {
		return fmt.Errorf("sandbox manager is not configured")
	}
	bundle, err := c.GetPreviewBundle(project)
	if err != nil {
		return err
	}
	files, _ := bundle["files"].(map[string]string)
	return c.SandboxManager.Sync(ctx, project, files)
}

func (c *Controller) OpenSandboxRoute(ctx context.Context, project *models.AppProject, route, setBy string) (*sandbox.PreviewTarget, error) {
	if c.SandboxManager == nil {
		return nil, fmt.Errorf("sandbox manager is not configured")
	}
	target, err := c.SandboxManager.OpenRoute(ctx, project, route, setBy)
	if err != nil {
		return nil, err
	}
	if project.Settings == nil {
		project.Settings = map[string]any{}
	}
	project.Settings["preview_target"] = map[string]any{
		"url": target.URL,
		"label": target.Label,
		"kind": string(target.Kind),
		"set_by": target.SetBy,
		"updated_at": target.UpdatedAt.Format(time.RFC3339),
	}
	return target, c.DB.UpdateAppProjectPreviewTarget(project.ID.String(), project.Settings["preview_target"].(map[string]any))
}
```

- [ ] **Step 2: Add views**

Create `internal/views/sandbox_views.go`:

```go
package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) GetSandboxStatus(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	project, err := v.Controller.GetAppProject(chi.URLParam(r, "appId"), user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}
	status, err := v.Controller.SandboxStatus(r.Context(), project)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}
	utils.JSONResponse(w, status, http.StatusOK)
}

func (v *HttpViews) StartSandbox(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	project, err := v.Controller.GetAppProject(chi.URLParam(r, "appId"), user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}
	status, err := v.Controller.StartSandbox(r.Context(), project)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}
	utils.JSONResponse(w, status, http.StatusOK)
}

func (v *HttpViews) SyncSandbox(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	project, err := v.Controller.GetAppProject(chi.URLParam(r, "appId"), user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}
	if err := v.Controller.SyncSandbox(r.Context(), project); err != nil {
		utils.JSONInternalError(w, err)
		return
	}
	utils.JSONResponse(w, map[string]bool{"ok": true}, http.StatusOK)
}

func (v *HttpViews) OpenSandboxRoute(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	project, err := v.Controller.GetAppProject(chi.URLParam(r, "appId"), user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}
	var body struct {
		Route string `json:"route"`
	}
	if err := utils.ParseJSONBody(r, &body); err != nil {
		utils.ValidationError(w, err)
		return
	}
	target, err := v.Controller.OpenSandboxRoute(r.Context(), project, body.Route, "user")
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}
	utils.JSONResponse(w, target, http.StatusOK)
}
```

- [ ] **Step 3: Update app routes**

In `cmd/main.go`, replace the old sandbox block:

```go
// Sandbox ID persistence for E2B preview
r.Get("/sandbox", httpViews.GetAppSandbox)
r.Put("/sandbox", httpViews.SetAppSandbox)
// Sandbox lifecycle for mobile preview
r.Post("/sandbox/start", httpViews.StartAppSandbox)
r.Get("/sandbox/status", httpViews.GetAppSandboxStatus)
```

with:

```go
// Daytona-backed sandbox lifecycle and preview control
r.Route("/sandbox", func(r chi.Router) {
	r.Get("/status", httpViews.GetSandboxStatus)
	r.Post("/start", httpViews.StartSandbox)
	r.Post("/sync", httpViews.SyncSandbox)
	r.Post("/previews/open-route", httpViews.OpenSandboxRoute)
})
```

- [ ] **Step 4: Compile route/view layer**

Run:

```bash
go test ./internal/views ./internal/controllers
```

Expected: compile errors identify old E2B handler references that are removed in Task 12. Fix only duplicate function names and route references here.

- [ ] **Step 5: Commit**

```bash
git add internal/controllers/sandbox_controller.go internal/views/sandbox_views.go cmd/main.go
git commit -m "feat(sandbox): expose backend sandbox endpoints"
```

---

### Task 8: Replace Agent Sandbox Tools

**Files:**
- Modify: `internal/agent/dependencies.go`
- Modify: `internal/controllers/agent_deps.go`
- Modify: `internal/agent/tools_sandbox.go`
- Modify: `internal/agent/agent_policy.go`
- Modify: `internal/agent/context_test.go`
- Modify: `internal/agent/tools_sandbox_test.go`

- [ ] **Step 1: Update dependency comment and request fields**

In `internal/agent/dependencies.go`, replace the E2B comment above `SandboxControlRequest` with:

```go
// SandboxControlRequest is an agent-runtime request for the backend-owned
// Daytona-backed sandbox control plane. App source files remain backed by Git;
// this is for live sandbox state, named process control, logs, terminals,
// previews, diagnostics, snapshots, and sandbox-only artifacts.
```

Add fields:

```go
Process string `json:"process,omitempty"`
Input   string `json:"input,omitempty"`
Cols    int    `json:"cols,omitempty"`
Rows    int    `json:"rows,omitempty"`
SnapshotID string `json:"snapshot_id,omitempty"`
```

- [ ] **Step 2: Route controller agent deps to SandboxManager**

In `internal/controllers/agent_deps.go`, replace `sandboxControlForAgent` with a switch that calls `c.SandboxManager`. Use this shape:

```go
func (c *Controller) sandboxControlForAgent(ctx context.Context, project *models.AppProject, user *models.User, req agent.SandboxControlRequest) (map[string]any, error) {
	if project == nil {
		return nil, fmt.Errorf("project is required")
	}
	if user == nil {
		return nil, fmt.Errorf("user is required")
	}
	if c.SandboxManager == nil {
		return nil, fmt.Errorf("sandbox manager is not configured")
	}
	switch req.Action {
	case "status":
		st, err := c.SandboxManager.Status(ctx, project)
		return structToMap(st), err
	case "start":
		st, err := c.StartSandbox(ctx, project)
		return structToMap(st), err
	case "sync":
		err := c.SyncSandbox(ctx, project)
		return map[string]any{"ok": err == nil}, err
	case "preview_open_route":
		target, err := c.OpenSandboxRoute(ctx, project, req.Route, "agent")
		return structToMap(target), err
	default:
		return nil, fmt.Errorf("unsupported sandbox action %q", req.Action)
	}
}

func structToMap(v any) map[string]any {
	b, _ := json.Marshal(v)
	out := map[string]any{}
	_ = json.Unmarshal(b, &out)
	return out
}
```

Add `encoding/json` to imports.

- [ ] **Step 3: Rename tool descriptions**

In `internal/agent/tools_sandbox.go`, replace E2B-specific descriptions:

```go
Inspect the current E2B sandbox state
```

with:

```go
Inspect the current Daytona-backed sandbox state
```

Replace approved URL hosts so `allowedPreviewURL` accepts:

```go
host == "preview.wemob.app" || strings.HasSuffix(host, ".preview.wemob.app")
```

and remove `e2b.app` acceptance.

- [ ] **Step 4: Add new tool action names**

Keep existing tool names that are already useful (`sandbox_status`, `sandbox_exec`, `sandbox_read_logs`, `sandbox_serve_artifact`, `preview_open_route`, `preview_open_artifact`) for compatibility with prompts, but add these provider-neutral tools in `registerSandboxTools`:

```go
r.Register(SandboxStartTool{Deps: deps})
r.Register(SandboxSyncTool{Deps: deps})
r.Register(ProcessRestartTool{Deps: deps})
r.Register(ProcessLogsTool{Deps: deps})
```

Implement each using `sandboxControl` with actions:

```go
start
sync
process_restart
process_logs
```

The `ProcessRestartTool` schema requires `process`. The `ProcessLogsTool` schema accepts `process`.

- [ ] **Step 5: Update policy**

In `internal/agent/agent_policy.go`, add low-risk/read-only tools:

```go
"process_logs",
```

Add medium-risk tools:

```go
"sandbox_start",
"sandbox_sync",
"process_restart",
```

Remove E2B wording in comments.

- [ ] **Step 6: Run agent tests**

Run:

```bash
go test ./internal/agent -run 'TestAllowedPreviewURL|TestSandbox|TestAgentPolicy|TestContext' -v
```

Expected: tests pass after updating expected allowed hostnames and tool lists.

- [ ] **Step 7: Commit**

```bash
git add internal/agent internal/controllers/agent_deps.go
git commit -m "feat(agent): route sandbox tools through SandboxManager"
```

---

### Task 9: Add Stable Preview Host Resolver

**Files:**
- Create: `internal/views/preview_proxy_views.go`
- Modify: `cmd/main.go`
- Modify: `Caddyfile`
- Test: `internal/views/edge_internal_views_test.go` or new view tests

- [ ] **Step 1: Add hostname parser**

Create `internal/views/preview_proxy_views.go`:

```go
package views

import (
	"net/http"
	"strings"

	"github.com/knid/wemob/internal/services/sandbox"
	"github.com/knid/wemob/internal/utils"
)

func parsePreviewHost(host string) (slug string, kind sandbox.PreviewKind) {
	host = strings.ToLower(strings.Split(host, ":")[0])
	host = strings.TrimSuffix(host, ".preview.wemob.app")
	switch {
	case strings.HasSuffix(host, "-backend"):
		return strings.TrimSuffix(host, "-backend"), sandbox.PreviewBackend
	case strings.HasSuffix(host, "-expo"):
		return strings.TrimSuffix(host, "-expo"), sandbox.PreviewExpo
	case strings.HasSuffix(host, "-artifacts"):
		return strings.TrimSuffix(host, "-artifacts"), sandbox.PreviewArtifacts
	default:
		return host, sandbox.PreviewApp
	}
}

func (v *HttpViews) ResolvePreviewHost(w http.ResponseWriter, r *http.Request) {
	slug, kind := parsePreviewHost(r.URL.Query().Get("host"))
	if slug == "" {
		utils.JSONErrorResponse(w, "invalid preview host", http.StatusBadRequest)
		return
	}
	utils.JSONResponse(w, map[string]any{"slug": slug, "kind": kind}, http.StatusOK)
}
```

- [ ] **Step 2: Register internal resolve route**

In `cmd/main.go`, add:

```go
r.Get("/internal/preview/resolve", httpViews.ResolvePreviewHost)
```

near the existing internal edge routes.

- [ ] **Step 3: Update Caddy preview route**

In `Caddyfile`, replace the provider-specific `*.preview.wemob.app` E2B reverse proxy with a backend proxy:

```caddyfile
*.preview.wemob.app {
    reverse_proxy http://wemob-backend:8001 {
        header_up Host {host}
        header_up X-Forwarded-Host {host}
        header_up X-Wemob-Preview-Host {host}
    }
}
```

If production Caddy reaches backend by a different upstream name, use the existing backend upstream convention in the file.

- [ ] **Step 4: Run tests**

Run:

```bash
go test ./internal/views -run Preview -v
```

Expected: compile succeeds. Add a direct test for `parsePreviewHost` if no Preview tests exist.

- [ ] **Step 5: Commit**

```bash
git add internal/views/preview_proxy_views.go cmd/main.go Caddyfile
git commit -m "feat(preview): add stable preview host resolver"
```

---

### Task 10: Replace Frontend Preview Hook

**Files in `/Users/knid/Projects/wemob/wemob-app`:**
- Create: `src/hooks/use-sandbox-preview.ts`
- Modify: `src/components/studio/studio-workspace.tsx`
- Modify: `src/components/studio/preview-view.tsx`
- Modify: `src/components/studio/mobile-preview-view.tsx`
- Modify: `src/components/studio/backend-output-view.tsx`
- Modify: `src/components/studio/no-frontend-view.tsx`
- Modify: `src/lib/api.ts`

- [ ] **Step 1: Add API methods**

In `src/lib/api.ts`, add methods:

```ts
async getSandboxStatus(appId: string): Promise<SandboxStatus> {
  return this.request<SandboxStatus>(`/v2/apps/${appId}/sandbox/status`)
}

async startSandbox(appId: string): Promise<SandboxStatus> {
  return this.request<SandboxStatus>(`/v2/apps/${appId}/sandbox/start`, { method: "POST" })
}

async syncSandbox(appId: string): Promise<{ ok: boolean }> {
  return this.request<{ ok: boolean }>(`/v2/apps/${appId}/sandbox/sync`, { method: "POST" })
}

async openSandboxRoute(appId: string, route: string): Promise<PreviewTarget> {
  return this.request<PreviewTarget>(`/v2/apps/${appId}/sandbox/previews/open-route`, {
    method: "POST",
    body: JSON.stringify({ route }),
  })
}
```

Define `SandboxStatus` and `PreviewTarget` near existing app builder types:

```ts
export type SandboxState = "missing" | "creating" | "syncing" | "installing" | "starting" | "ready" | "degraded" | "stopped" | "archived" | "error"

export type PreviewTarget = {
  url: string
  label: string
  kind: "app" | "backend" | "expo" | "artifacts" | "route"
  set_by: "user" | "agent" | "system" | string
  updated_at: string
}

export type SandboxStatus = {
  provider: "daytona"
  sandbox_id: string
  state: SandboxState
  last_error?: string
  active_preview?: PreviewTarget
  preview_urls?: Record<string, string>
  recommended_action?: string
}
```

- [ ] **Step 2: Create `use-sandbox-preview.ts`**

Create `src/hooks/use-sandbox-preview.ts`:

```ts
"use client"

import { useCallback, useEffect, useMemo, useState } from "react"
import { apiClient, type SandboxStatus } from "@/lib/api"

export function useSandboxPreview(appId: string | null) {
  const [status, setStatus] = useState<SandboxStatus | null>(null)
  const [isStarting, setIsStarting] = useState(false)
  const [logs, setLogs] = useState("")
  const [iframeReloadKey, setIframeReloadKey] = useState(0)

  const previewUrl = status?.active_preview?.url
    ?? status?.preview_urls?.app
    ?? status?.preview_urls?.backend
    ?? null

  const phase = status?.state ?? "missing"
  const isReady = phase === "ready"
  const isError = phase === "error" || phase === "degraded"
  const errorMessage = status?.last_error ?? null

  const refreshStatus = useCallback(async () => {
    if (!appId) return
    const next = await apiClient.getSandboxStatus(appId)
    setStatus(next)
  }, [appId])

  const startPreview = useCallback(async () => {
    if (!appId) return
    setIsStarting(true)
    try {
      const next = await apiClient.startSandbox(appId)
      setStatus(next)
    } finally {
      setIsStarting(false)
    }
  }, [appId])

  const refreshPreview = useCallback(() => {
    setIframeReloadKey((k) => k + 1)
    void refreshStatus()
  }, [refreshStatus])

  const writeFile = useCallback(async () => {
    if (!appId) return
    await apiClient.syncSandbox(appId)
    refreshPreview()
  }, [appId, refreshPreview])

  const deleteFile = writeFile

  useEffect(() => {
    void refreshStatus()
  }, [refreshStatus])

  return useMemo(() => ({
    previewStatus: status ? { status: phase, preview_url: previewUrl ?? "", error: errorMessage ?? undefined } : null,
    previewUrl,
    sandboxId: status?.sandbox_id ?? null,
    isStarting,
    isReady,
    isError,
    phase,
    errorMessage,
    logs,
    errors: [],
    droppedErrorCount: 0,
    iframeReloadKey,
    expoGoUrl: status?.preview_urls?.expo ?? null,
    startPreview,
    refreshPreview,
    writeFile,
    deleteFile,
    clearLogs: () => setLogs(""),
  }), [status, phase, previewUrl, errorMessage, isStarting, isReady, isError, logs, iframeReloadKey, startPreview, refreshPreview, writeFile, deleteFile])
}
```

- [ ] **Step 3: Replace hook usage**

In `src/components/studio/studio-workspace.tsx`, replace:

```ts
import { useE2BPreview } from "@/hooks/use-e2b-preview";
```

with:

```ts
import { useSandboxPreview } from "@/hooks/use-sandbox-preview";
```

Replace:

```ts
const previewRef = useRef<ReturnType<typeof useE2BPreview> | null>(null);
const preview = useE2BPreview(previewAppId);
```

with:

```ts
const previewRef = useRef<ReturnType<typeof useSandboxPreview> | null>(null);
const preview = useSandboxPreview(previewAppId);
```

- [ ] **Step 4: Update component prop types**

In these files, replace `useE2BPreview` imports/types with `useSandboxPreview`:

```text
src/components/studio/preview-view.tsx
src/components/studio/mobile-preview-view.tsx
src/components/studio/backend-output-view.tsx
src/components/studio/no-frontend-view.tsx
```

Use:

```ts
import type { useSandboxPreview } from "@/hooks/use-sandbox-preview";
```

- [ ] **Step 5: Run frontend typecheck**

Run in `/Users/knid/Projects/wemob/wemob-app`:

```bash
npm run lint
```

Expected: no unresolved `useE2BPreview` import errors. Fix type mismatches in the hook return shape by matching existing consumers.

- [ ] **Step 6: Commit frontend changes**

```bash
git add src/hooks/use-sandbox-preview.ts src/components/studio src/lib/api.ts
git commit -m "feat(studio): use backend sandbox preview API"
```

---

### Task 11: Remove Frontend E2B API Routes And Dependency

**Files in `/Users/knid/Projects/wemob/wemob-app`:**
- Delete: `src/app/api/e2b/start/route.ts`
- Delete: `src/app/api/e2b/files/route.ts`
- Delete: `src/app/api/e2b/logs/route.ts`
- Delete: `src/app/api/e2b/agent/[...action]/route.ts`
- Delete: `src/hooks/use-e2b-preview.ts`
- Modify: `package.json`
- Modify: `package-lock.json`

- [ ] **Step 1: Remove E2B route files and old hook**

Run:

```bash
git rm src/hooks/use-e2b-preview.ts
git rm src/app/api/e2b/start/route.ts
git rm src/app/api/e2b/files/route.ts
git rm src/app/api/e2b/logs/route.ts
git rm 'src/app/api/e2b/agent/[...action]/route.ts'
```

Expected: files are staged for deletion.

- [ ] **Step 2: Remove E2B dependency and scripts**

Run:

```bash
npm uninstall e2b
```

Edit `package.json` to remove:

```json
"build:e2b-template": "node scripts/build-e2b-template.mjs"
```

Delete template scripts:

```bash
git rm scripts/build-e2b-template.mjs scripts/build-e2b-expo-template.mjs
```

- [ ] **Step 3: Search for frontend E2B references**

Run:

```bash
rg -n "E2B|e2b|useE2BPreview|/api/e2b" .
```

Expected: only historical docs or intentional migration notes remain. Update user-facing text in `src/components/pages/updates-page.tsx` so it does not advertise E2B.

- [ ] **Step 4: Run frontend checks**

Run:

```bash
npm run lint
npm run build
```

Expected: both pass.

- [ ] **Step 5: Commit frontend cleanup**

```bash
git add package.json package-lock.json src scripts
git commit -m "chore(studio): remove E2B preview routes"
```

---

### Task 12: Remove Backend E2B Service And Routes

**Files:**
- Delete: `internal/services/e2b/e2b.go`
- Delete: `internal/services/e2b/e2b_test.go`
- Modify: `internal/views/app_deploy_views.go`
- Modify: `cmd/main.go`
- Modify: `internal/services/secret/keys.go`
- Modify: `internal/controllers/testcontroller_integration.go`

- [ ] **Step 1: Delete E2B service**

Run:

```bash
git rm internal/services/e2b/e2b.go internal/services/e2b/e2b_test.go
```

- [ ] **Step 2: Remove old app deploy sandbox handlers**

In `internal/views/app_deploy_views.go`, delete these functions:

```go
GetAppSandbox
SetAppSandbox
StartAppSandbox
GetAppSandboxStatus
AuthorizeE2BAgentSandbox
```

Do not delete `GetPreviewBundle`; the new sandbox manager still uses it as source sync input.

- [ ] **Step 3: Remove internal E2B agent route**

In `cmd/main.go`, delete:

```go
r.Post("/internal/e2b/agent/authorize", httpViews.AuthorizeE2BAgentSandbox)
```

- [ ] **Step 4: Remove old secret keys**

In `internal/services/secret/keys.go`, remove:

```go
"E2B_PREVIEW_DOMAIN",
"E2B_AGENT_CONTROL_TOKEN",
```

Keep any keys that are still referenced by old deploy docs only out of compiled code.

- [ ] **Step 5: Search backend E2B references**

Run:

```bash
rg -n "E2B|e2b|E2BService|internal/e2b|/api/e2b" .
```

Expected: no compiled Go references. Historical docs may remain if not compiled, but update `CLAUDE.md`, current comments, and tests.

- [ ] **Step 6: Run backend tests**

Run:

```bash
go test ./internal/services/sandbox ./internal/controllers ./internal/views ./internal/agent
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "chore(sandbox): remove E2B backend integration"
```

---

### Task 13: Sync Git-Backed File Writes Into Daytona

**Files:**
- Modify: `internal/agent/tools_base.go`
- Modify: `internal/agent/tools_base_batch_test.go`
- Modify: `internal/controllers/agent_deps.go`

- [ ] **Step 1: Locate file write completion points**

Run:

```bash
rg -n "file_written|write_file|edit_file|multi_edit|delete_file" internal/agent/tools_base.go internal/agent/run_execution.go internal/controllers/agent_deps.go
```

Expected: identify the functions that persist Git-backed edits and emit file events.

- [ ] **Step 2: Add sync callback to dependencies**

In `internal/agent/dependencies.go`, add:

```go
type SandboxFileSyncer func(ctx context.Context, project *models.AppProject, paths []string) error
```

Add to `RunDependencies`:

```go
SyncSandboxFiles SandboxFileSyncer
```

- [ ] **Step 3: Bind sync callback**

In `internal/controllers/agent_deps.go`, set:

```go
SyncSandboxFiles: func(ctx context.Context, project *models.AppProject, paths []string) error {
	return c.SyncSandbox(ctx, project)
},
```

This intentionally performs a full source sync for first cut correctness. Delta-only sync can replace it after the big-bang migration is stable.

- [ ] **Step 4: Invoke sync after successful source writes**

In the file write/edit/delete tool success paths in `internal/agent/tools_base.go`, after the Git-backed write succeeds and before returning success, add:

```go
if rc != nil && rc.Deps != nil && rc.Deps.SyncSandboxFiles != nil && rc.Project != nil {
	_ = rc.Deps.SyncSandboxFiles(ctx, rc.Project, []string{path})
}
```

For batch writes, pass all changed paths.

- [ ] **Step 5: Test sync callback invocation**

Add a test in `internal/agent/tools_base_batch_test.go` that sets `SyncSandboxFiles` to capture paths, runs a batch write tool, and asserts the callback saw the changed files.

Expected assertion:

```go
if !slices.Contains(gotPaths, "app/page.tsx") {
	t.Fatalf("sandbox sync paths = %#v", gotPaths)
}
```

- [ ] **Step 6: Run agent tests**

Run:

```bash
go test ./internal/agent -run 'Test.*Write|Test.*Edit|Test.*Batch' -v
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/agent internal/controllers/agent_deps.go
git commit -m "feat(agent): sync source writes into live sandbox"
```

---

### Task 14: Update Expired Preview Pages For Stable Hosts

**Files:**
- Modify: `caddy/static/sandbox-expired.html`
- Modify: `caddy/static/sandbox-expired-shared.html`
- Modify: `/Users/knid/Projects/wemob/wemob-app/public/sandbox-expired.html`

- [ ] **Step 1: Replace host parser assumptions**

In each expired page, replace comments and code that assume:

```text
{port}-{sandboxId}.preview.wemob.app
```

with:

```text
{appSlug}.preview.wemob.app or {appSlug}-{kind}.preview.wemob.app
```

The page no longer needs to extract a sandbox ID. It should post:

```js
window.parent.postMessage({
  type: "wemob:sandbox-expired",
  source: "preview-iframe",
  host: window.location.host
}, "*")
```

- [ ] **Step 2: Verify no sandbox ID parsing remains**

Run:

```bash
rg -n "sandboxId|sandbox id|port-.*sandbox|labels\\.3|e2b" caddy/static /Users/knid/Projects/wemob/wemob-app/public/sandbox-expired.html
```

Expected: no stale parser comments or E2B references in current expired-page code.

- [ ] **Step 3: Commit backend static page changes**

In backend repo:

```bash
git add caddy/static/sandbox-expired.html caddy/static/sandbox-expired-shared.html
git commit -m "chore(preview): update expired preview pages for stable hosts"
```

- [ ] **Step 4: Commit frontend public page changes**

In frontend repo:

```bash
git add public/sandbox-expired.html
git commit -m "chore(preview): update public expired preview page"
```

---

### Task 15: Final Verification And E2B Reference Audit

**Files:**
- Backend repo
- Frontend repo

- [ ] **Step 1: Backend audit**

Run in backend repo:

```bash
rg -n "E2B|e2b|/api/e2b|internal/e2b|E2BService|E2B_" .
```

Expected: no compiled code references. Historical docs may remain only if explicitly marked as historical; update current docs and comments.

- [ ] **Step 2: Frontend audit**

Run in frontend repo:

```bash
rg -n "E2B|e2b|/api/e2b|useE2BPreview|E2B_" .
```

Expected: no runtime code references. Historical docs may remain only if explicitly marked as historical.

- [ ] **Step 3: Backend test suite**

Run:

```bash
go test ./...
```

Expected: PASS. If integration-only tests are skipped by build tags, note that in the verification output.

- [ ] **Step 4: Frontend checks**

Run in frontend repo:

```bash
npm run lint
npm run build
```

Expected: PASS.

- [ ] **Step 5: Manual smoke tests**

Create or open one app of each type and verify:

```text
Next.js app preview starts at https://{slug}.preview.wemob.app
Vite split-web preview starts and proxies backend calls
backend-only preview starts at https://{slug}-backend.preview.wemob.app
mobile native Expo web preview starts at https://{slug}.preview.wemob.app
Expo Go preview URL exists at https://{slug}-expo.preview.wemob.app
LLM edit updates Git/Gitea source first and then updates live preview
LLM preview_open_route changes the user's iframe route
Sandbox recreate preserves the stable public preview URL
Expired preview page posts wemob:sandbox-expired inside Studio
```

- [ ] **Step 6: Commit final cleanup**

```bash
git status --short
git add -A
git commit -m "test(sandbox): verify Daytona sandbox cutover"
```

---

## Self-Review Notes

Spec coverage:

- Daytona backend control plane: Tasks 2 through 7.
- All app types: Task 3 runtime plans and Task 15 smoke tests.
- Git/Gitea source of truth with sandbox sync: Task 13.
- Shared live sandbox for user and LLM: Tasks 7, 8, and 10.
- Stable public preview URLs: Task 9 and Task 14.
- E2B removal: Tasks 11, 12, and 15.
- Frontend uses backend API only: Task 10 and Task 11.
- Agent tools manage user preview through backend state: Task 8.

Known implementation risks:

- Daytona Go SDK method names may differ slightly from the docs. Keep all SDK-specific fixes inside `internal/services/sandbox/daytona/provider.go`.
- Backend reverse proxying of WebSockets may need a focused follow-up if Caddy-to-backend-to-Daytona proxying does not preserve HMR.
- First-cut file sync uses full sync after agent writes for correctness. Delta sync can be optimized after the cutover is stable.
