package sandbox

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
)

const (
	vitePreviewConfigPath = "frontend/vite.config.preview.ts"
	vitePreviewConfig     = `import { defineConfig, mergeConfig } from "vite";
import baseConfig from "./vite.config";

export default mergeConfig(
  baseConfig,
  defineConfig({
    server: {
      proxy: {
        "/api": {
          target: "http://127.0.0.1:3001",
          changeOrigin: true,
        },
      },
    },
  }),
);
`
)

type Manager struct {
	provider ProviderClient
}

func NewManager(provider ProviderClient) *Manager {
	return &Manager{provider: provider}
}

func (m *Manager) Start(ctx context.Context, project *models.AppProject, files map[string]string, env map[string]string) (*Status, error) {
	provider, err := m.providerClient()
	if err != nil {
		return nil, err
	}
	if project == nil {
		return nil, errors.New("project is nil")
	}
	plan := BuildRuntimePlan(project)
	sandboxID := project.SandboxID
	newSandbox := sandboxID == ""
	if sandboxID == "" {
		sandboxID, err = provider.Create(ctx, plan)
		if err != nil {
			return nil, fmt.Errorf("create sandbox: %w", err)
		}
		if sandboxID == "" {
			return nil, errors.New("create sandbox: empty sandbox id")
		}
	}
	if err := provider.Start(ctx, sandboxID); err != nil {
		return nil, fmt.Errorf("start sandbox: %w", err)
	}
	if err := m.syncFiles(ctx, sandboxID, plan, files); err != nil {
		return nil, err
	}
	if err := m.installDependencies(ctx, provider, sandboxID, plan); err != nil {
		return nil, err
	}
	for _, spec := range plan.Processes {
		if spec.Name == ProcessArtifacts {
			continue
		}
		spec.Env = mergeProcessEnv(spec.Env, env)
		if err := provider.StartProcess(ctx, sandboxID, spec); err != nil {
			return nil, fmt.Errorf("start process %s: %w", spec.Name, err)
		}
	}
	if newSandbox {
		project.SandboxID = sandboxID
	}
	return m.Status(ctx, project)
}

func (m *Manager) installDependencies(ctx context.Context, provider ProviderClient, sandboxID string, plan RuntimePlan) error {
	seen := map[string]bool{}
	for _, spec := range plan.Processes {
		if spec.Name == ProcessArtifacts || spec.Cwd == "" || seen[spec.Cwd] {
			continue
		}
		seen[spec.Cwd] = true
		command := "test ! -f package.json || npm install"
		_, stderr, exitCode, err := provider.Execute(ctx, sandboxID, command, spec.Cwd, 120000)
		if err != nil {
			return fmt.Errorf("install dependencies in %s: %w", spec.Cwd, err)
		}
		if exitCode != 0 {
			msg := strings.TrimSpace(stderr)
			if msg == "" {
				msg = fmt.Sprintf("exit code %d", exitCode)
			}
			return fmt.Errorf("install dependencies in %s: %s", spec.Cwd, msg)
		}
	}
	return nil
}

func mergeProcessEnv(base, extra map[string]string) map[string]string {
	if len(base) == 0 && len(extra) == 0 {
		return nil
	}
	merged := make(map[string]string, len(base)+len(extra))
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range extra {
		merged[key] = value
	}
	return merged
}

func (m *Manager) Status(ctx context.Context, project *models.AppProject) (*Status, error) {
	provider, err := m.providerClient()
	if err != nil {
		return nil, err
	}
	if project == nil {
		return nil, errors.New("project is nil")
	}
	if project.SandboxID == "" {
		return &Status{Provider: ProviderDaytona, State: StateMissing}, nil
	}
	plan := BuildRuntimePlan(project)
	st, err := provider.GetStatus(ctx, project.SandboxID, plan)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, errors.New("provider returned nil status")
	}
	var enrichmentErrs []string
	st.PreviewURLs = map[PreviewKind]string{}
	kinds := make([]PreviewKind, 0, len(plan.Previews))
	for kind := range plan.Previews {
		kinds = append(kinds, kind)
	}
	sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })
	for _, kind := range kinds {
		u, err := provider.PreviewURL(ctx, project.SandboxID, plan.Previews[kind])
		if err == nil {
			st.PreviewURLs[kind] = u
		} else {
			enrichmentErrs = append(enrichmentErrs, fmt.Sprintf("preview %s: %v", kind, err))
		}
	}
	processes, err := provider.ProcessStates(ctx, project.SandboxID, plan)
	if err == nil {
		st.Processes = processes
	} else {
		enrichmentErrs = append(enrichmentErrs, fmt.Sprintf("process states: %v", err))
	}
	if len(enrichmentErrs) > 0 {
		if st.State == StateReady {
			st.State = StateDegraded
		}
		st.LastError = appendLastError(st.LastError, strings.Join(enrichmentErrs, "; "))
		st.RecommendedAction = "retry status refresh or run diagnostics"
	}
	return st, nil
}

func (m *Manager) Sync(ctx context.Context, project *models.AppProject, files map[string]string) error {
	if _, _, err := m.startedProject(project); err != nil {
		return err
	}
	return m.syncFiles(ctx, project.SandboxID, BuildRuntimePlan(project), files)
}

func (m *Manager) syncFiles(ctx context.Context, sandboxID string, plan RuntimePlan, files map[string]string) error {
	files = withManagedPreviewFiles(plan, files)
	deltas := make([]FileDelta, 0, len(files))
	for p, content := range files {
		cleanPath, err := cleanFilePath(p)
		if err != nil {
			return err
		}
		deltas = append(deltas, FileDelta{Path: cleanPath, Content: content})
	}
	sort.Slice(deltas, func(i, j int) bool { return deltas[i].Path < deltas[j].Path })
	provider, err := m.providerClient()
	if err != nil {
		return err
	}
	if err := provider.WriteFiles(ctx, sandboxID, deltas, plan); err != nil {
		return fmt.Errorf("sync sandbox files: %w", err)
	}
	return nil
}

func withManagedPreviewFiles(plan RuntimePlan, files map[string]string) map[string]string {
	if !needsVitePreviewProxy(plan) {
		return files
	}
	copied := make(map[string]string, len(files)+1)
	for path, content := range files {
		copied[path] = content
	}
	copied[vitePreviewConfigPath] = vitePreviewConfig
	return copied
}

func needsVitePreviewProxy(plan RuntimePlan) bool {
	if !plan.BackendEnabled || plan.Previews[PreviewBackend] == 0 {
		return false
	}
	for _, spec := range plan.Processes {
		if spec.Name == ProcessFrontend && spec.Cwd == ProjectRoot+"/frontend" {
			return true
		}
	}
	return false
}

func (m *Manager) OpenRoute(ctx context.Context, project *models.AppProject, route, setBy string) (*PreviewTarget, error) {
	provider, sandboxID, err := m.startedProject(project)
	if err != nil {
		return nil, err
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
	base, err := provider.PreviewURL(ctx, sandboxID, port)
	if err != nil {
		return nil, err
	}
	if stableBase := StablePreviewBaseURL(project, plan.DefaultPreview); stableBase != "" {
		base = stableBase
	}
	u, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	basePath := strings.TrimRight(u.Path, "/")
	routePath := path.Clean(route)
	if routePath == "." {
		routePath = "/"
	}
	if basePath == "" {
		u.Path = routePath
	} else if routePath == "/" {
		u.Path = basePath + "/"
	} else {
		u.Path = path.Join(basePath, routePath)
	}
	u.RawQuery = ""
	u.Fragment = ""
	return &PreviewTarget{
		URL:       u.String(),
		Label:     "App preview",
		Kind:      PreviewRoute,
		SetBy:     setBy,
		UpdatedAt: time.Now().UTC(),
	}, nil
}

func (m *Manager) OpenArtifact(ctx context.Context, project *models.AppProject, artifactPath, setBy string) (*PreviewTarget, error) {
	provider, sandboxID, spec, err := m.processSpec(project, ProcessArtifacts)
	if err != nil {
		return nil, err
	}
	cleanPath := path.Clean(strings.TrimSpace(artifactPath))
	if cleanPath == "." || cleanPath == "/" || !strings.HasPrefix(cleanPath, ArtifactsRoot+"/") {
		return nil, fmt.Errorf("artifact path must be under %s", ArtifactsRoot)
	}
	if err := provider.StartProcess(ctx, sandboxID, spec); err != nil {
		return nil, fmt.Errorf("start artifact server: %w", err)
	}
	base, err := provider.PreviewURL(ctx, sandboxID, spec.Port)
	if err != nil {
		return nil, err
	}
	if stableBase := StablePreviewBaseURL(project, PreviewArtifacts); stableBase != "" {
		base = stableBase
	}
	u, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	rel := strings.TrimPrefix(cleanPath, ArtifactsRoot+"/")
	basePath := strings.TrimRight(u.Path, "/")
	if basePath == "" {
		u.Path = "/" + rel
	} else {
		u.Path = path.Join(basePath, rel)
	}
	u.RawQuery = ""
	u.Fragment = ""
	return &PreviewTarget{
		URL:       u.String(),
		Label:     path.Base(cleanPath),
		Kind:      PreviewArtifacts,
		SetBy:     setBy,
		UpdatedAt: time.Now().UTC(),
	}, nil
}

const (
	previewDomain     = "preview.wemob.app"
	maxDNSLabelLength = 63
)

func StablePreviewBaseURL(project *models.AppProject, kind PreviewKind) string {
	label := StablePreviewLabel(project, kind)
	if label == "" {
		return ""
	}
	return "https://" + label + "." + previewDomain
}

func StablePreviewLabel(project *models.AppProject, kind PreviewKind) string {
	if project == nil {
		return ""
	}
	suffix, ok := previewKindSuffix(kind)
	if !ok {
		return ""
	}
	if kind == PreviewApp || kind == PreviewRoute {
		return idPreviewLabel(project, suffix)
	}
	slug := strings.TrimSpace(project.Slug)
	if slug != "" && len(slug)+len(suffix) <= maxDNSLabelLength {
		return slug + suffix
	}
	return idPreviewLabel(project, suffix)
}

func idPreviewLabel(project *models.AppProject, suffix string) string {
	if project.ID == uuid.Nil {
		return ""
	}
	id := project.ID.String()
	label := "id-" + id + suffix
	if len(label) > maxDNSLabelLength {
		return ""
	}
	return label
}

func previewKindSuffix(kind PreviewKind) (string, bool) {
	switch kind {
	case PreviewBackend:
		return "-backend", true
	case PreviewExpo:
		return "-expo", true
	case PreviewArtifacts:
		return "-artifacts", true
	case PreviewApp, PreviewRoute:
		return "-app", true
	default:
		return "", false
	}
}

func (m *Manager) Stop(ctx context.Context, project *models.AppProject) error {
	provider, sandboxID, err := m.startedProject(project)
	if err != nil {
		return err
	}
	return provider.Stop(ctx, sandboxID)
}

func (m *Manager) RestartProcess(ctx context.Context, project *models.AppProject, name ProcessName, env map[string]string) error {
	provider, sandboxID, spec, err := m.processSpec(project, name)
	if err != nil {
		return err
	}
	if name != ProcessArtifacts {
		spec.Env = mergeProcessEnv(spec.Env, env)
	}
	if err := provider.StopProcess(ctx, sandboxID, name); err != nil {
		return fmt.Errorf("stop process %s: %w", name, err)
	}
	if err := provider.StartProcess(ctx, sandboxID, spec); err != nil {
		return fmt.Errorf("start process %s: %w", name, err)
	}
	return nil
}

func (m *Manager) ProcessLogs(ctx context.Context, project *models.AppProject, name ProcessName, lines int) (*LogsResult, error) {
	provider, sandboxID, spec, err := m.processSpec(project, name)
	if err != nil {
		return nil, err
	}
	return provider.ProcessLogs(ctx, sandboxID, spec, lines)
}

func (m *Manager) Execute(ctx context.Context, project *models.AppProject, command, cwd string, timeoutMs int) (string, string, int, error) {
	provider, sandboxID, err := m.startedProject(project)
	if err != nil {
		return "", "", 0, err
	}
	return provider.Execute(ctx, sandboxID, command, cwd, timeoutMs)
}

func (m *Manager) CreateTerminal(ctx context.Context, project *models.AppProject, cols, rows int) (*Terminal, error) {
	provider, sandboxID, err := m.startedProject(project)
	if err != nil {
		return nil, err
	}
	return provider.CreateTerminal(ctx, sandboxID, cols, rows)
}

func (m *Manager) SendTerminalInput(ctx context.Context, project *models.AppProject, terminalID, input string) error {
	provider, sandboxID, err := m.startedProject(project)
	if err != nil {
		return err
	}
	return provider.SendTerminalInput(ctx, sandboxID, terminalID, input)
}

func (m *Manager) CloseTerminal(ctx context.Context, project *models.AppProject, terminalID string) error {
	provider, sandboxID, err := m.startedProject(project)
	if err != nil {
		return err
	}
	return provider.CloseTerminal(ctx, sandboxID, terminalID)
}

func (m *Manager) CreateSnapshot(ctx context.Context, project *models.AppProject) (*Snapshot, error) {
	provider, sandboxID, err := m.startedProject(project)
	if err != nil {
		return nil, err
	}
	return provider.CreateSnapshot(ctx, sandboxID)
}

func (m *Manager) RestoreSnapshot(ctx context.Context, project *models.AppProject, snapshotID string) error {
	provider, sandboxID, err := m.startedProject(project)
	if err != nil {
		return err
	}
	return provider.RestoreSnapshot(ctx, sandboxID, snapshotID)
}

func (m *Manager) RunDiagnostics(ctx context.Context, project *models.AppProject) (*DiagnosticsResult, error) {
	provider, sandboxID, err := m.startedProject(project)
	if err != nil {
		return nil, err
	}
	return provider.RunDiagnostics(ctx, sandboxID, BuildRuntimePlan(project))
}

func (m *Manager) startedProject(project *models.AppProject) (ProviderClient, string, error) {
	provider, err := m.providerClient()
	if err != nil {
		return nil, "", err
	}
	if project == nil {
		return nil, "", errors.New("project is nil")
	}
	if project.SandboxID == "" {
		return nil, "", fmt.Errorf("sandbox is not started")
	}
	return provider, project.SandboxID, nil
}

func (m *Manager) processSpec(project *models.AppProject, name ProcessName) (ProviderClient, string, ProcessSpec, error) {
	provider, sandboxID, err := m.startedProject(project)
	if err != nil {
		return nil, "", ProcessSpec{}, err
	}
	for _, spec := range BuildRuntimePlan(project).Processes {
		if spec.Name == name {
			return provider, sandboxID, spec, nil
		}
	}
	return nil, "", ProcessSpec{}, fmt.Errorf("process %s not found in runtime plan", name)
}

func (m *Manager) providerClient() (ProviderClient, error) {
	if m == nil || isNilProvider(m.provider) {
		return nil, errors.New("provider is nil")
	}
	return m.provider, nil
}

func isNilProvider(provider ProviderClient) bool {
	if provider == nil {
		return true
	}
	v := reflect.ValueOf(provider)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

func appendLastError(existing, next string) string {
	if existing == "" {
		return next
	}
	if next == "" {
		return existing
	}
	return existing + "; " + next
}

func cleanFilePath(filePath string) (string, error) {
	normalized := strings.ReplaceAll(filePath, "\\", "/")
	if normalized == "" ||
		strings.ContainsRune(normalized, '\x00') ||
		path.IsAbs(normalized) {
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
