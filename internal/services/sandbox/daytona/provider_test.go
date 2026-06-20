package daytona

import (
	"context"
	"errors"
	"strings"
	"testing"

	sdkoptions "github.com/daytonaio/daytona/libs/sdk-go/pkg/options"
	sdktypes "github.com/daytonaio/daytona/libs/sdk-go/pkg/types"
	"github.com/knid/wemob/internal/services/sandbox"
)

type fakeClient struct {
	sb               *fakeSandbox
	lastCreateParams any
	createErr        error
	getIDOrName      string
}

func (c *fakeClient) Create(_ context.Context, params any, _ ...func(*sdkoptions.CreateSandbox)) (sandboxHandle, error) {
	c.lastCreateParams = params
	if c.createErr != nil {
		return nil, c.createErr
	}
	if c.sb != nil {
		return c.sb, nil
	}
	return newFakeSandbox(), nil
}

func (c *fakeClient) Get(_ context.Context, idOrName string) (sandboxHandle, error) {
	c.getIDOrName = idOrName
	if c.sb != nil {
		return c.sb, nil
	}
	return newFakeSandbox(), nil
}

type fakeSandbox struct {
	id       string
	state    string
	reason   *string
	fs       *fakeFileSystem
	process  *fakeProcess
	preview  map[int]string
	started  bool
	startErr error
	stopped  bool
}

func newFakeSandbox() *fakeSandbox {
	return &fakeSandbox{
		id:      "sandbox-123",
		state:   "started",
		fs:      &fakeFileSystem{},
		process: &fakeProcess{},
		preview: map[int]string{3000: "https://daytona.example/3000"},
	}
}

func (s *fakeSandbox) ID() string           { return s.id }
func (s *fakeSandbox) State() string        { return s.state }
func (s *fakeSandbox) ErrorReason() *string { return s.reason }
func (s *fakeSandbox) Start(context.Context) error {
	s.started = true
	return s.startErr
}
func (s *fakeSandbox) Stop(context.Context) error {
	s.stopped = true
	return nil
}
func (s *fakeSandbox) Archive(context.Context) error { return nil }
func (s *fakeSandbox) Delete(context.Context) error  { return nil }
func (s *fakeSandbox) GetPreviewLink(_ context.Context, port int) (string, error) {
	return s.preview[port], nil
}
func (s *fakeSandbox) FileSystem() fileSystemAPI { return s.fs }
func (s *fakeSandbox) Process() processAPI       { return s.process }

type fakeFileSystem struct {
	folders []string
	deletes []string
	uploads map[string]string
}

func (f *fakeFileSystem) CreateFolder(_ context.Context, path string) error {
	f.folders = append(f.folders, path)
	return nil
}

func (f *fakeFileSystem) DeleteFile(_ context.Context, path string, _ bool) error {
	f.deletes = append(f.deletes, path)
	return nil
}

func (f *fakeFileSystem) UploadFile(_ context.Context, source any, destination string) error {
	if f.uploads == nil {
		f.uploads = map[string]string{}
	}
	switch content := source.(type) {
	case []byte:
		f.uploads[destination] = string(content)
	case string:
		f.uploads[destination] = content
	}
	return nil
}

type fakeProcess struct {
	executedCommands []string
	executeResult    *sdktypes.ExecuteResponse
	sessionsCreated  []string
	sessionsDeleted  []string
	sessionCommands  []string
	pty              *fakePty
}

func (p *fakeProcess) ExecuteCommand(_ context.Context, command string, _ ...func(*sdkoptions.ExecuteCommand)) (*sdktypes.ExecuteResponse, error) {
	p.executedCommands = append(p.executedCommands, command)
	if p.executeResult != nil {
		return p.executeResult, nil
	}
	return &sdktypes.ExecuteResponse{ExitCode: 0, Result: "ok"}, nil
}

func (p *fakeProcess) CreateSession(_ context.Context, sessionID string) error {
	p.sessionsCreated = append(p.sessionsCreated, sessionID)
	return nil
}

func (p *fakeProcess) DeleteSession(_ context.Context, sessionID string) error {
	p.sessionsDeleted = append(p.sessionsDeleted, sessionID)
	return nil
}

func (p *fakeProcess) ExecuteSessionCommand(_ context.Context, _ string, command string, _ bool, _ bool) (map[string]any, error) {
	p.sessionCommands = append(p.sessionCommands, command)
	return map[string]any{"id": "cmd-1"}, nil
}

func (p *fakeProcess) CreatePty(_ context.Context, id string, _ ...func(*sdkoptions.CreatePty)) (ptyHandle, error) {
	p.pty = &fakePty{id: id}
	return p.pty, nil
}

type fakePty struct {
	id           string
	input        string
	disconnected bool
	killed       bool
}

func (p *fakePty) SessionID() string                       { return p.id }
func (p *fakePty) WaitForConnection(context.Context) error { return nil }
func (p *fakePty) SendInput(input []byte) error {
	p.input += string(input)
	return nil
}
func (p *fakePty) Disconnect() error {
	p.disconnected = true
	return nil
}
func (p *fakePty) Kill(context.Context) error {
	p.killed = true
	return nil
}

func TestNewRequiresAPIKey(t *testing.T) {
	provider, err := New(" ", "", "")
	if err == nil {
		t.Fatal("expected error")
	}
	if provider != nil {
		t.Fatal("expected nil provider")
	}
}

func TestNewWithClientConfiguresProvider(t *testing.T) {
	provider := NewWithClient(&fakeClient{}, "preview.wemob.app")
	if err := provider.ready(); err != nil {
		t.Fatalf("expected provider to be ready: %v", err)
	}
	if provider.previewDomain != "preview.wemob.app" {
		t.Fatalf("preview domain = %q", provider.previewDomain)
	}
}

func TestCreateUsesDefaultMachineResources(t *testing.T) {
	client := &fakeClient{}
	provider := NewWithClient(client, "")

	id, err := provider.Create(context.Background(), sandbox.RuntimePlan{AppID: "app-123"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if id != "sandbox-123" {
		t.Fatalf("sandbox ID = %q, want sandbox-123", id)
	}
	params, ok := client.lastCreateParams.(sdktypes.ImageParams)
	if !ok {
		t.Fatalf("create params type = %T, want sdktypes.ImageParams", client.lastCreateParams)
	}
	if params.Resources == nil {
		t.Fatal("expected resources")
	}
	if params.Resources.CPU != 1 {
		t.Fatalf("CPU = %d, want 1", params.Resources.CPU)
	}
	if params.Resources.Memory != 1 {
		t.Fatalf("Memory = %d, want 1", params.Resources.Memory)
	}
	if params.Resources.Disk != 3 {
		t.Fatalf("Disk = %d, want 3", params.Resources.Disk)
	}
	if params.AutoStopInterval == nil || *params.AutoStopInterval != 60 {
		t.Fatalf("AutoStopInterval = %#v, want 60", params.AutoStopInterval)
	}
	if params.AutoDeleteInterval == nil || *params.AutoDeleteInterval != -1 {
		t.Fatalf("AutoDeleteInterval = %#v, want -1", params.AutoDeleteInterval)
	}
}

func TestCreateReturnsExistingSandboxOnNameConflict(t *testing.T) {
	existing := newFakeSandbox()
	existing.id = "existing-daytona"
	client := &fakeClient{
		sb:        existing,
		createErr: errors.New("Conflict: Sandbox with name wemob-app-123 already exists"),
	}
	provider := NewWithClient(client, "")

	id, err := provider.Create(context.Background(), sandbox.RuntimePlan{AppID: "app-123"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if id != "existing-daytona" {
		t.Fatalf("sandbox ID = %q, want existing-daytona", id)
	}
	if client.getIDOrName != "wemob-app-123" {
		t.Fatalf("Get called with %q, want sandbox name", client.getIDOrName)
	}
}

func TestStartTreatsStateChangeConflictAsAlreadyStarting(t *testing.T) {
	sb := newFakeSandbox()
	sb.startErr = errors.New("Conflict: Sandbox state change in progress")
	provider := NewWithClient(&fakeClient{sb: sb}, "")

	if err := provider.Start(context.Background(), "sandbox-123"); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if !sb.started {
		t.Fatal("expected Start to be called")
	}
}

func TestMapState(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want sandbox.State
	}{
		{name: "started", in: "started", want: sandbox.StateReady},
		{name: "stopped", in: "stopped", want: sandbox.StateStopped},
		{name: "archived", in: "archived", want: sandbox.StateArchived},
		{name: "pending build", in: "pending_build", want: sandbox.StateCreating},
		{name: "starting", in: "starting", want: sandbox.StateStarting},
		{name: "build failed", in: "build_failed", want: sandbox.StateError},
		{name: "unknown", in: "unexpected", want: sandbox.StateDegraded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := mapState(tt.in)
			if got != tt.want {
				t.Fatalf("mapState(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestDestinationPathDefaultsToProjectRoot(t *testing.T) {
	got, err := destinationPath("src/App.tsx", sandbox.RuntimePlan{})
	if err != nil {
		t.Fatalf("destinationPath returned error: %v", err)
	}
	if got != "/workspace/project/src/App.tsx" {
		t.Fatalf("destinationPath = %q", got)
	}
}

func TestDestinationPathSupportsFutureRootPrefix(t *testing.T) {
	plan := sandbox.RuntimePlan{
		Roots: []sandbox.Root{
			{Name: sandbox.RootBackend, Path: "/workspace/backend"},
		},
	}
	got, err := destinationPath("backend/package.json", plan)
	if err != nil {
		t.Fatalf("destinationPath returned error: %v", err)
	}
	if got != "/workspace/backend/package.json" {
		t.Fatalf("destinationPath = %q", got)
	}
}

func TestDestinationPathRejectsUnsafePaths(t *testing.T) {
	for _, filePath := range []string{"", "/abs", "../escape", "a/../b", "a\x00b"} {
		t.Run(filePath, func(t *testing.T) {
			if _, err := destinationPath(filePath, sandbox.RuntimePlan{}); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestProcessCommandWrapsCWDAndLogs(t *testing.T) {
	got := processCommand(sandbox.ProcessSpec{
		Command: "npm run dev -- --port 3000",
		Cwd:     "/workspace/project",
		LogPath: "/tmp/wemob-frontend.log",
	})
	for _, want := range []string{
		"bash -lc ",
		"mkdir -p",
		"/tmp",
		"cd",
		"/workspace/project",
		"npm run dev -- --port 3000",
		"/tmp/wemob-frontend.log",
		"2>&1",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("processCommand missing %q in %q", want, got)
		}
	}
}

func TestProcessCommandExportsEnv(t *testing.T) {
	exports := envExports(map[string]string{
		"API_KEY": "a'b",
		"EMPTY":   "",
	})
	for _, want := range []string{
		"export API_KEY='a'\"'\"'b'",
		"export EMPTY=''",
	} {
		if !strings.Contains(exports, want) {
			t.Fatalf("envExports missing %q in %q", want, exports)
		}
	}

	got := processCommand(sandbox.ProcessSpec{
		Command: "npm run dev",
		Cwd:     "/workspace/project",
		Env: map[string]string{
			"API_KEY": "a'b",
			"EMPTY":   "",
		},
	})

	for _, want := range []string{
		"export API_KEY",
		"export EMPTY",
		"npm run dev",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("processCommand missing %q in %q", want, got)
		}
	}
}

func TestShellQuoteEscapesSingleQuotes(t *testing.T) {
	got := shellQuote("don't")
	want := "'don'\"'\"'t'"
	if got != want {
		t.Fatalf("shellQuote = %q, want %q", got, want)
	}
}

func TestWriteFilesSyncsUploadsAndDeletes(t *testing.T) {
	sb := newFakeSandbox()
	provider := NewWithClient(&fakeClient{sb: sb}, "")
	err := provider.WriteFiles(context.Background(), "sandbox-123", []sandbox.FileDelta{
		{Path: "frontend/src/App.tsx", Content: "app"},
		{Path: "backend/app/api/route.ts", Content: "api"},
		{Path: "old.txt", Delete: true},
	}, sandbox.RuntimePlan{
		Roots: []sandbox.Root{
			{Name: sandbox.RootProject, Path: sandbox.ProjectRoot},
			{Name: sandbox.RootBackend, Path: sandbox.BackendRoot},
		},
	})
	if err != nil {
		t.Fatalf("WriteFiles returned error: %v", err)
	}
	if got := sb.fs.uploads["/workspace/project/frontend/src/App.tsx"]; got != "app" {
		t.Fatalf("frontend upload = %q", got)
	}
	if got := sb.fs.uploads["/workspace/backend/app/api/route.ts"]; got != "api" {
		t.Fatalf("backend upload = %q", got)
	}
	if len(sb.fs.deletes) != 1 || sb.fs.deletes[0] != "/workspace/project/old.txt" {
		t.Fatalf("deletes = %#v", sb.fs.deletes)
	}
}

func TestExecuteReturnsCommandResult(t *testing.T) {
	sb := newFakeSandbox()
	sb.process.executeResult = &sdktypes.ExecuteResponse{
		ExitCode: 7,
		Result:   "stdout",
	}
	provider := NewWithClient(&fakeClient{sb: sb}, "")
	stdout, stderr, exitCode, err := provider.Execute(context.Background(), "sandbox-123", "npm test", sandbox.ProjectRoot, 1000)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if stdout != "stdout" || stderr != "" || exitCode != 7 {
		t.Fatalf("Execute = stdout %q stderr %q exit %d", stdout, stderr, exitCode)
	}
	if len(sb.process.executedCommands) != 1 || sb.process.executedCommands[0] != "npm test" {
		t.Fatalf("executed commands = %#v", sb.process.executedCommands)
	}
}

func TestStartAndStopProcessUseNamedSessions(t *testing.T) {
	sb := newFakeSandbox()
	provider := NewWithClient(&fakeClient{sb: sb}, "")
	spec := sandbox.ProcessSpec{
		Name:    sandbox.ProcessFrontend,
		Command: "npm run dev",
		Cwd:     "/workspace/project/frontend",
		LogPath: "/tmp/wemob-frontend.log",
	}
	if err := provider.StartProcess(context.Background(), "sandbox-123", spec); err != nil {
		t.Fatalf("StartProcess returned error: %v", err)
	}
	if len(sb.process.sessionsCreated) != 1 || sb.process.sessionsCreated[0] != "wemob-frontend" {
		t.Fatalf("created sessions = %#v", sb.process.sessionsCreated)
	}
	if len(sb.process.sessionCommands) != 1 || !strings.Contains(sb.process.sessionCommands[0], "/workspace/project/frontend") {
		t.Fatalf("session commands = %#v", sb.process.sessionCommands)
	}
	if err := provider.StopProcess(context.Background(), "sandbox-123", sandbox.ProcessFrontend); err != nil {
		t.Fatalf("StopProcess returned error: %v", err)
	}
	if len(sb.process.sessionsDeleted) < 2 || sb.process.sessionsDeleted[1] != "wemob-frontend" {
		t.Fatalf("deleted sessions = %#v", sb.process.sessionsDeleted)
	}
}

func TestPreviewURLReturnsProviderLink(t *testing.T) {
	sb := newFakeSandbox()
	provider := NewWithClient(&fakeClient{sb: sb}, "")
	got, err := provider.PreviewURL(context.Background(), "sandbox-123", 3000)
	if err != nil {
		t.Fatalf("PreviewURL returned error: %v", err)
	}
	if got != "https://daytona.example/3000" {
		t.Fatalf("PreviewURL = %q", got)
	}
}

func TestTerminalLifecycleTracksHandle(t *testing.T) {
	sb := newFakeSandbox()
	provider := NewWithClient(&fakeClient{sb: sb}, "")
	terminal, err := provider.CreateTerminal(context.Background(), "sandbox-123", 120, 40)
	if err != nil {
		t.Fatalf("CreateTerminal returned error: %v", err)
	}
	if terminal.ID == "" {
		t.Fatal("expected terminal id")
	}
	if err := provider.SendTerminalInput(context.Background(), "sandbox-123", terminal.ID, "ls\n"); err != nil {
		t.Fatalf("SendTerminalInput returned error: %v", err)
	}
	if sb.process.pty.input != "ls\n" {
		t.Fatalf("pty input = %q", sb.process.pty.input)
	}
	if err := provider.CloseTerminal(context.Background(), "sandbox-123", terminal.ID); err != nil {
		t.Fatalf("CloseTerminal returned error: %v", err)
	}
	if !sb.process.pty.disconnected || !sb.process.pty.killed {
		t.Fatalf("pty closed = disconnected %v killed %v", sb.process.pty.disconnected, sb.process.pty.killed)
	}
}

func TestRunDiagnosticsReturnsCommandItems(t *testing.T) {
	sb := newFakeSandbox()
	provider := NewWithClient(&fakeClient{sb: sb}, "")
	result, err := provider.RunDiagnostics(context.Background(), "sandbox-123", sandbox.RuntimePlan{})
	if err != nil {
		t.Fatalf("RunDiagnostics returned error: %v", err)
	}
	if result.Summary == "" || len(result.Items) == 0 {
		t.Fatalf("diagnostics result = %#v", result)
	}
	if len(sb.process.executedCommands) < 4 {
		t.Fatalf("expected diagnostic commands, got %#v", sb.process.executedCommands)
	}
}
