package daytona

import (
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	daytonasdk "github.com/daytonaio/daytona/libs/sdk-go/pkg/daytona"
	sdkoptions "github.com/daytonaio/daytona/libs/sdk-go/pkg/options"
	sdktypes "github.com/daytonaio/daytona/libs/sdk-go/pkg/types"
	"github.com/knid/wemob/internal/services/sandbox"
)

const (
	defaultImage       = "node:22-bookworm"
	defaultCPU         = 1
	defaultMemory      = 1024
	defaultDisk        = 3072
	defaultExecTimeout = 30 * time.Second
)

var envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type sdkClient interface {
	Create(ctx context.Context, params any, opts ...func(*sdkoptions.CreateSandbox)) (sandboxHandle, error)
	Get(ctx context.Context, sandboxIDOrName string) (sandboxHandle, error)
}

type sandboxHandle interface {
	ID() string
	State() string
	ErrorReason() *string
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Archive(ctx context.Context) error
	Delete(ctx context.Context) error
	GetPreviewLink(ctx context.Context, port int) (string, error)
	FileSystem() fileSystemAPI
	Process() processAPI
}

type fileSystemAPI interface {
	CreateFolder(ctx context.Context, path string) error
	DeleteFile(ctx context.Context, path string, recursive bool) error
	UploadFile(ctx context.Context, source any, destination string) error
}

type processAPI interface {
	ExecuteCommand(ctx context.Context, command string, opts ...func(*sdkoptions.ExecuteCommand)) (*sdktypes.ExecuteResponse, error)
	CreateSession(ctx context.Context, sessionID string) error
	DeleteSession(ctx context.Context, sessionID string) error
	ExecuteSessionCommand(ctx context.Context, sessionID, command string, runAsync bool, suppressInputEcho bool) (map[string]any, error)
	CreatePty(ctx context.Context, id string, opts ...func(*sdkoptions.CreatePty)) (ptyHandle, error)
}

type ptyHandle interface {
	SessionID() string
	WaitForConnection(ctx context.Context) error
	SendInput([]byte) error
	Disconnect() error
	Kill(ctx context.Context) error
}

type Provider struct {
	client        sdkClient
	previewDomain string
	terminals     map[string]ptyHandle
	mu            sync.Mutex
}

type sdkClientAdapter struct {
	client *daytonasdk.Client
}

func (a sdkClientAdapter) Create(ctx context.Context, params any, opts ...func(*sdkoptions.CreateSandbox)) (sandboxHandle, error) {
	sb, err := a.client.Create(ctx, params, opts...)
	if err != nil {
		return nil, err
	}
	return sdkSandbox{sandbox: sb}, nil
}

func (a sdkClientAdapter) Get(ctx context.Context, sandboxIDOrName string) (sandboxHandle, error) {
	sb, err := a.client.Get(ctx, sandboxIDOrName)
	if err != nil {
		return nil, err
	}
	return sdkSandbox{sandbox: sb}, nil
}

type sdkSandbox struct {
	sandbox *daytonasdk.Sandbox
}

func (s sdkSandbox) ID() string {
	if s.sandbox == nil {
		return ""
	}
	return s.sandbox.ID
}

func (s sdkSandbox) State() string {
	if s.sandbox == nil {
		return ""
	}
	return string(s.sandbox.State)
}

func (s sdkSandbox) ErrorReason() *string {
	if s.sandbox == nil {
		return nil
	}
	return s.sandbox.ErrorReason
}

func (s sdkSandbox) Start(ctx context.Context) error {
	return s.sandbox.Start(ctx)
}

func (s sdkSandbox) Stop(ctx context.Context) error {
	return s.sandbox.Stop(ctx)
}

func (s sdkSandbox) Archive(ctx context.Context) error {
	return s.sandbox.Archive(ctx)
}

func (s sdkSandbox) Delete(ctx context.Context) error {
	return s.sandbox.Delete(ctx)
}

func (s sdkSandbox) GetPreviewLink(ctx context.Context, port int) (string, error) {
	link, err := s.sandbox.GetPreviewLink(ctx, port)
	if err != nil {
		return "", err
	}
	if link == nil {
		return "", nil
	}
	return link.URL, nil
}

func (s sdkSandbox) FileSystem() fileSystemAPI {
	return sdkFileSystem{fileSystem: s.sandbox.FileSystem}
}

func (s sdkSandbox) Process() processAPI {
	return sdkProcess{process: s.sandbox.Process}
}

type sdkFileSystem struct {
	fileSystem *daytonasdk.FileSystemService
}

func (f sdkFileSystem) CreateFolder(ctx context.Context, path string) error {
	return f.fileSystem.CreateFolder(ctx, path)
}

func (f sdkFileSystem) DeleteFile(ctx context.Context, path string, recursive bool) error {
	return f.fileSystem.DeleteFile(ctx, path, recursive)
}

func (f sdkFileSystem) UploadFile(ctx context.Context, source any, destination string) error {
	return f.fileSystem.UploadFile(ctx, source, destination)
}

type sdkProcess struct {
	process *daytonasdk.ProcessService
}

func (p sdkProcess) ExecuteCommand(ctx context.Context, command string, opts ...func(*sdkoptions.ExecuteCommand)) (*sdktypes.ExecuteResponse, error) {
	return p.process.ExecuteCommand(ctx, command, opts...)
}

func (p sdkProcess) CreateSession(ctx context.Context, sessionID string) error {
	return p.process.CreateSession(ctx, sessionID)
}

func (p sdkProcess) DeleteSession(ctx context.Context, sessionID string) error {
	return p.process.DeleteSession(ctx, sessionID)
}

func (p sdkProcess) ExecuteSessionCommand(ctx context.Context, sessionID, command string, runAsync bool, suppressInputEcho bool) (map[string]any, error) {
	return p.process.ExecuteSessionCommand(ctx, sessionID, command, runAsync, suppressInputEcho)
}

func (p sdkProcess) CreatePty(ctx context.Context, id string, opts ...func(*sdkoptions.CreatePty)) (ptyHandle, error) {
	return p.process.CreatePty(ctx, id, opts...)
}

func New(apiKey, target, previewDomain string) (*Provider, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, errors.New("daytona api key is required")
	}
	client, err := daytonasdk.NewClientWithConfig(&sdktypes.DaytonaConfig{
		APIKey: apiKey,
		Target: target,
	})
	if err != nil {
		return nil, fmt.Errorf("create daytona client: %w", err)
	}
	return NewWithClient(sdkClientAdapter{client: client}, previewDomain), nil
}

func NewWithClient(client sdkClient, previewDomain string) *Provider {
	return &Provider{
		client:        client,
		previewDomain: previewDomain,
		terminals:     map[string]ptyHandle{},
	}
}

func (p *Provider) Create(ctx context.Context, plan sandbox.RuntimePlan) (string, error) {
	if err := p.ready(); err != nil {
		return "", err
	}
	name := sandboxName(plan)
	created, err := p.client.Create(ctx, sdktypes.ImageParams{
		SandboxBaseParams: sdktypes.SandboxBaseParams{
			Name:     name,
			Language: sdktypes.CodeLanguageTypeScript,
			Public:   true,
			Labels: map[string]string{
				"wemob":    "true",
				"app_id":   plan.AppID,
				"provider": string(sandbox.ProviderDaytona),
			},
		},
		Image: defaultImage,
		Resources: &sdktypes.Resources{
			CPU:    defaultCPU,
			Memory: defaultMemory,
			Disk:   defaultDisk,
		},
	}, sdkoptions.WithWaitForStart(false))
	if err != nil {
		return "", fmt.Errorf("create daytona sandbox: %w", err)
	}
	if created == nil || created.ID() == "" {
		return "", errors.New("create daytona sandbox: empty sandbox id")
	}
	return created.ID(), nil
}

func (p *Provider) GetStatus(ctx context.Context, sandboxID string, plan sandbox.RuntimePlan) (*sandbox.Status, error) {
	sb, err := p.get(ctx, sandboxID)
	if err != nil {
		return nil, err
	}
	state, recommended := mapState(sb.State())
	st := &sandbox.Status{
		Provider:          sandbox.ProviderDaytona,
		SandboxID:         sb.ID(),
		State:             state,
		RecommendedAction: recommended,
	}
	if reason := sb.ErrorReason(); reason != nil {
		st.LastError = *reason
	}
	return st, nil
}

func (p *Provider) Start(ctx context.Context, sandboxID string) error {
	sb, err := p.get(ctx, sandboxID)
	if err != nil {
		return err
	}
	if err := sb.Start(ctx); err != nil {
		return fmt.Errorf("start daytona sandbox %s: %w", sandboxID, err)
	}
	return nil
}

func (p *Provider) Stop(ctx context.Context, sandboxID string) error {
	sb, err := p.get(ctx, sandboxID)
	if err != nil {
		return err
	}
	if err := sb.Stop(ctx); err != nil {
		return fmt.Errorf("stop daytona sandbox %s: %w", sandboxID, err)
	}
	return nil
}

func (p *Provider) Archive(ctx context.Context, sandboxID string) error {
	sb, err := p.get(ctx, sandboxID)
	if err != nil {
		return err
	}
	if err := sb.Archive(ctx); err != nil {
		return fmt.Errorf("archive daytona sandbox %s: %w", sandboxID, err)
	}
	return nil
}

func (p *Provider) Delete(ctx context.Context, sandboxID string) error {
	sb, err := p.get(ctx, sandboxID)
	if err != nil {
		return err
	}
	if err := sb.Delete(ctx); err != nil {
		return fmt.Errorf("delete daytona sandbox %s: %w", sandboxID, err)
	}
	return nil
}

func (p *Provider) WriteFiles(ctx context.Context, sandboxID string, files []sandbox.FileDelta, plan sandbox.RuntimePlan) error {
	sb, err := p.get(ctx, sandboxID)
	if err != nil {
		return err
	}
	for _, root := range plan.Roots {
		if root.Path == "" {
			continue
		}
		if err := sb.FileSystem().CreateFolder(ctx, root.Path); err != nil {
			return fmt.Errorf("create root %s: %w", root.Path, err)
		}
	}
	for _, file := range files {
		dest, err := destinationPath(file.Path, plan)
		if err != nil {
			return err
		}
		if file.Delete {
			if err := sb.FileSystem().DeleteFile(ctx, dest, true); err != nil {
				return fmt.Errorf("delete %s: %w", dest, err)
			}
			continue
		}
		dir := path.Dir(dest)
		if dir != "." && dir != "/" {
			if err := sb.FileSystem().CreateFolder(ctx, dir); err != nil {
				return fmt.Errorf("create parent %s: %w", dir, err)
			}
		}
		if err := sb.FileSystem().UploadFile(ctx, []byte(file.Content), dest); err != nil {
			return fmt.Errorf("upload %s: %w", dest, err)
		}
	}
	return nil
}

func (p *Provider) Execute(ctx context.Context, sandboxID, command, cwd string, timeoutMs int) (string, string, int, error) {
	sb, err := p.get(ctx, sandboxID)
	if err != nil {
		return "", "", 0, err
	}
	timeout := defaultExecTimeout
	if timeoutMs > 0 {
		timeout = time.Duration(timeoutMs) * time.Millisecond
	}
	opts := []func(*sdkoptions.ExecuteCommand){sdkoptions.WithExecuteTimeout(timeout)}
	if cwd != "" {
		opts = append(opts, sdkoptions.WithCwd(cwd))
	}
	resp, err := sb.Process().ExecuteCommand(ctx, command, opts...)
	if err != nil {
		return "", "", 0, fmt.Errorf("execute daytona command: %w", err)
	}
	stdout := resp.Result
	if resp.Artifacts != nil && resp.Artifacts.Stdout != "" {
		stdout = resp.Artifacts.Stdout
	}
	return stdout, "", resp.ExitCode, nil
}

func (p *Provider) StartProcess(ctx context.Context, sandboxID string, spec sandbox.ProcessSpec) error {
	if spec.Name == "" {
		return errors.New("process name is required")
	}
	if spec.Command == "" {
		return fmt.Errorf("process %s command is required", spec.Name)
	}
	sb, err := p.get(ctx, sandboxID)
	if err != nil {
		return err
	}
	sessionID := processSessionID(spec.Name)
	_ = sb.Process().DeleteSession(ctx, sessionID)
	if err := sb.Process().CreateSession(ctx, sessionID); err != nil {
		return fmt.Errorf("create process session %s: %w", spec.Name, err)
	}
	command := processCommand(spec)
	if _, err := sb.Process().ExecuteSessionCommand(ctx, sessionID, command, true, false); err != nil {
		return fmt.Errorf("start process %s: %w", spec.Name, err)
	}
	return nil
}

func (p *Provider) StopProcess(ctx context.Context, sandboxID string, name sandbox.ProcessName) error {
	if name == "" {
		return errors.New("process name is required")
	}
	sb, err := p.get(ctx, sandboxID)
	if err != nil {
		return err
	}
	sessionID := processSessionID(name)
	deleteErr := sb.Process().DeleteSession(ctx, sessionID)
	if port, ok := fallbackPort(name); ok {
		_, _, _, _ = p.Execute(ctx, sandboxID, fmt.Sprintf("bash -lc %s", shellQuote(fmt.Sprintf("fuser -k %d/tcp 2>/dev/null || true", port))), "", 10000)
	}
	if deleteErr != nil {
		return fmt.Errorf("stop process %s: %w", name, deleteErr)
	}
	return nil
}

func (p *Provider) ProcessLogs(ctx context.Context, sandboxID string, spec sandbox.ProcessSpec, lines int) (*sandbox.LogsResult, error) {
	if lines <= 0 {
		lines = 200
	}
	if spec.LogPath == "" {
		return &sandbox.LogsResult{Process: spec.Name}, nil
	}
	command := fmt.Sprintf("bash -lc %s", shellQuote(fmt.Sprintf("test -f %s && tail -n %d %s || true", shellQuote(spec.LogPath), lines, shellQuote(spec.LogPath))))
	stdout, stderr, _, err := p.Execute(ctx, sandboxID, command, "", 10000)
	if err != nil {
		return nil, err
	}
	return &sandbox.LogsResult{Process: spec.Name, Content: stdout + stderr}, nil
}

func (p *Provider) ProcessStates(ctx context.Context, sandboxID string, plan sandbox.RuntimePlan) (map[sandbox.ProcessName]sandbox.ProcessState, error) {
	states := make(map[sandbox.ProcessName]sandbox.ProcessState, len(plan.Processes))
	for _, spec := range plan.Processes {
		state := sandbox.ProcessState{
			Name:   spec.Name,
			Status: sandbox.ProcessStopped,
			Port:   spec.Port,
		}
		if spec.Port > 0 {
			state.URL = fmt.Sprintf("http://localhost:%d", spec.Port)
			command := fmt.Sprintf("bash -lc %s", shellQuote(fmt.Sprintf("curl -fsS --max-time 2 http://127.0.0.1:%d >/dev/null", spec.Port)))
			_, stderr, exitCode, err := p.Execute(ctx, sandboxID, command, "", 5000)
			switch {
			case err != nil:
				state.Status = sandbox.ProcessError
				state.LastError = err.Error()
			case exitCode == 0:
				state.Status = sandbox.ProcessReady
			default:
				state.Status = sandbox.ProcessStopped
				state.LastExitCode = &exitCode
				if stderr != "" {
					state.LastError = stderr
				}
			}
		}
		states[spec.Name] = state
	}
	return states, nil
}

func (p *Provider) PreviewURL(ctx context.Context, sandboxID string, port int) (string, error) {
	if port <= 0 {
		return "", fmt.Errorf("invalid preview port %d", port)
	}
	sb, err := p.get(ctx, sandboxID)
	if err != nil {
		return "", err
	}
	link, err := sb.GetPreviewLink(ctx, port)
	if err != nil {
		return "", fmt.Errorf("get daytona preview link for port %d: %w", port, err)
	}
	if link == "" {
		return "", fmt.Errorf("daytona preview link for port %d is empty", port)
	}
	return link, nil
}

func (p *Provider) CreateTerminal(ctx context.Context, sandboxID string, cols, rows int) (*sandbox.Terminal, error) {
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	sb, err := p.get(ctx, sandboxID)
	if err != nil {
		return nil, err
	}
	id := fmt.Sprintf("wemob-pty-%d", time.Now().UnixNano())
	handle, err := sb.Process().CreatePty(ctx, id, sdkoptions.WithCreatePtySize(sdktypes.PtySize{Cols: cols, Rows: rows}))
	if err != nil {
		return nil, fmt.Errorf("create daytona pty: %w", err)
	}
	if err := handle.WaitForConnection(ctx); err != nil {
		_ = handle.Disconnect()
		_ = handle.Kill(ctx)
		return nil, fmt.Errorf("connect daytona pty: %w", err)
	}
	p.mu.Lock()
	p.terminals[terminalKey(sandboxID, handle.SessionID())] = handle
	p.mu.Unlock()
	return &sandbox.Terminal{ID: handle.SessionID()}, nil
}

func (p *Provider) SendTerminalInput(ctx context.Context, sandboxID, terminalID, input string) error {
	handle, err := p.terminal(sandboxID, terminalID)
	if err != nil {
		return err
	}
	if err := handle.SendInput([]byte(input)); err != nil {
		return fmt.Errorf("send terminal input: %w", err)
	}
	return nil
}

func (p *Provider) CloseTerminal(ctx context.Context, sandboxID, terminalID string) error {
	key := terminalKey(sandboxID, terminalID)
	p.mu.Lock()
	handle := p.terminals[key]
	delete(p.terminals, key)
	p.mu.Unlock()
	if handle == nil {
		return fmt.Errorf("terminal %s not found", terminalID)
	}
	var errs []string
	if err := handle.Disconnect(); err != nil {
		errs = append(errs, err.Error())
	}
	if err := handle.Kill(ctx); err != nil {
		errs = append(errs, err.Error())
	}
	if len(errs) > 0 {
		return fmt.Errorf("close terminal %s: %s", terminalID, strings.Join(errs, "; "))
	}
	return nil
}

func (p *Provider) CreateSnapshot(ctx context.Context, sandboxID string) (*sandbox.Snapshot, error) {
	if strings.TrimSpace(sandboxID) == "" {
		return nil, errors.New("sandbox id is required")
	}
	return nil, errors.New("daytona live sandbox snapshots are not supported by the current provider adapter")
}

func (p *Provider) RestoreSnapshot(ctx context.Context, sandboxID, snapshotID string) error {
	if strings.TrimSpace(sandboxID) == "" {
		return errors.New("sandbox id is required")
	}
	if strings.TrimSpace(snapshotID) == "" {
		return errors.New("snapshot id is required")
	}
	return errors.New("daytona live sandbox snapshot restore is not supported by the current provider adapter")
}

func (p *Provider) RunDiagnostics(ctx context.Context, sandboxID string, plan sandbox.RuntimePlan) (*sandbox.DiagnosticsResult, error) {
	items := []string{}
	for _, command := range []string{"pwd", "node --version", "npm --version", "python3 --version"} {
		stdout, stderr, exitCode, err := p.Execute(ctx, sandboxID, command, sandbox.ProjectRoot, 10000)
		switch {
		case err != nil:
			items = append(items, fmt.Sprintf("%s: error: %v", command, err))
		case exitCode != 0:
			items = append(items, fmt.Sprintf("%s: exit %d %s", command, exitCode, strings.TrimSpace(stderr)))
		default:
			items = append(items, fmt.Sprintf("%s: %s", command, strings.TrimSpace(stdout)))
		}
	}
	processes, err := p.ProcessStates(ctx, sandboxID, plan)
	if err != nil {
		items = append(items, "process probes: "+err.Error())
	} else {
		names := make([]string, 0, len(processes))
		for name := range processes {
			names = append(names, string(name))
		}
		sort.Strings(names)
		for _, name := range names {
			state := processes[sandbox.ProcessName(name)]
			items = append(items, fmt.Sprintf("process %s: %s", name, state.Status))
		}
	}
	return &sandbox.DiagnosticsResult{Summary: "Daytona diagnostics complete", Items: items}, nil
}

func (p *Provider) get(ctx context.Context, sandboxID string) (sandboxHandle, error) {
	if err := p.ready(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(sandboxID) == "" {
		return nil, errors.New("sandbox id is required")
	}
	sb, err := p.client.Get(ctx, sandboxID)
	if err != nil {
		return nil, fmt.Errorf("get daytona sandbox %s: %w", sandboxID, err)
	}
	if sb == nil {
		return nil, fmt.Errorf("get daytona sandbox %s: empty response", sandboxID)
	}
	if sb.ID() == "" {
		return nil, fmt.Errorf("get daytona sandbox %s: empty sandbox id", sandboxID)
	}
	return sb, nil
}

func (p *Provider) ready() error {
	if p == nil || p.client == nil {
		return errors.New("daytona provider is not configured")
	}
	return nil
}

func (p *Provider) terminal(sandboxID, terminalID string) (ptyHandle, error) {
	if strings.TrimSpace(sandboxID) == "" {
		return nil, errors.New("sandbox id is required")
	}
	if strings.TrimSpace(terminalID) == "" {
		return nil, errors.New("terminal id is required")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	handle := p.terminals[terminalKey(sandboxID, terminalID)]
	if handle == nil {
		return nil, fmt.Errorf("terminal %s not found", terminalID)
	}
	return handle, nil
}

func terminalKey(sandboxID, terminalID string) string {
	return sandboxID + "/" + terminalID
}

func sandboxName(plan sandbox.RuntimePlan) string {
	appID := strings.TrimSpace(plan.AppID)
	if appID == "" {
		appID = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	appID = strings.ToLower(appID)
	replacer := strings.NewReplacer("_", "-", "/", "-", " ", "-")
	return "wemob-" + replacer.Replace(appID)
}

func mapState(state string) (sandbox.State, string) {
	switch strings.ToLower(state) {
	case "started":
		return sandbox.StateReady, ""
	case "stopped", "stopping":
		return sandbox.StateStopped, "start sandbox"
	case "archived", "archiving":
		return sandbox.StateArchived, "restore or recreate sandbox"
	case "creating", "pending_build", "building_snapshot", "pulling_snapshot", "restoring", "forking":
		return sandbox.StateCreating, "wait for sandbox provisioning"
	case "starting", "resizing", "snapshotting":
		return sandbox.StateStarting, "wait for sandbox startup"
	case "error", "build_failed", "destroyed", "destroying":
		return sandbox.StateError, "recreate sandbox"
	default:
		return sandbox.StateDegraded, "run diagnostics"
	}
}

func destinationPath(filePath string, plan sandbox.RuntimePlan) (string, error) {
	cleaned, err := cleanRelativePath(filePath)
	if err != nil {
		return "", err
	}
	rootMap := map[sandbox.RootName]string{
		sandbox.RootProject:   sandbox.ProjectRoot,
		sandbox.RootBackend:   sandbox.BackendRoot,
		sandbox.RootArtifacts: sandbox.ArtifactsRoot,
		sandbox.RootTmp:       sandbox.TmpRoot,
	}
	for _, root := range plan.Roots {
		if root.Name != "" && root.Path != "" {
			rootMap[root.Name] = root.Path
		}
	}
	parts := strings.SplitN(cleaned, "/", 2)
	if len(parts) == 2 {
		if root, ok := rootMap[sandbox.RootName(parts[0])]; ok {
			return path.Join(root, parts[1]), nil
		}
	}
	return path.Join(rootMap[sandbox.RootProject], cleaned), nil
}

func cleanRelativePath(filePath string) (string, error) {
	normalized := strings.ReplaceAll(filePath, "\\", "/")
	if normalized == "" || strings.ContainsRune(normalized, '\x00') || path.IsAbs(normalized) {
		return "", fmt.Errorf("unsafe file path %q", filePath)
	}
	for _, part := range strings.Split(normalized, "/") {
		if part == ".." {
			return "", fmt.Errorf("unsafe file path %q", filePath)
		}
	}
	cleaned := path.Clean(normalized)
	if cleaned == "." || path.IsAbs(cleaned) {
		return "", fmt.Errorf("unsafe file path %q", filePath)
	}
	return cleaned, nil
}

func processSessionID(name sandbox.ProcessName) string {
	return "wemob-" + string(name)
}

func processCommand(spec sandbox.ProcessSpec) string {
	command := spec.Command
	if len(spec.Env) > 0 {
		command = envExports(spec.Env) + command
	}
	if spec.Cwd != "" {
		command = "cd " + shellQuote(spec.Cwd) + " && " + command
	}
	if spec.LogPath != "" {
		dir := path.Dir(spec.LogPath)
		command = "mkdir -p " + shellQuote(dir) + " && (" + command + ") > " + shellQuote(spec.LogPath) + " 2>&1"
	}
	return "bash -lc " + shellQuote(command)
}

func envExports(env map[string]string) string {
	keys := make([]string, 0, len(env))
	for key := range env {
		if envNamePattern.MatchString(key) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		b.WriteString("export ")
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(shellQuote(env[key]))
		b.WriteString(" && ")
	}
	return b.String()
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func fallbackPort(name sandbox.ProcessName) (int, bool) {
	switch name {
	case sandbox.ProcessFrontend:
		return 3000, true
	case sandbox.ProcessBackend:
		return 3001, true
	case sandbox.ProcessExpoWeb:
		return 8081, true
	case sandbox.ProcessExpoGo:
		return 19000, true
	case sandbox.ProcessArtifacts:
		return 7000, true
	default:
		return 0, false
	}
}

var _ sandbox.ProviderClient = (*Provider)(nil)
