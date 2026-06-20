package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
)

const (
	sandboxRootProject   = "/workspace/project"
	sandboxRootBackend   = "/workspace/backend"
	sandboxRootArtifacts = "/workspace/artifacts"
	sandboxRootTmp       = "/tmp"
)

var blockedSandboxCommandPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(^|\s)sudo(\s|$)`),
	regexp.MustCompile(`(?i)(^|\s)(apt|apt-get|apk|yum|dnf|pacman)(\s|$)`),
	regexp.MustCompile(`(?i)rm\s+-[^\n;]*r[^\n;]*\s+/(?:\s|$|\*)`),
	regexp.MustCompile(`(?i)(^|\s)(ssh|scp|nc|netcat|socat|ngrok|cloudflared)(\s|$)`),
	regexp.MustCompile(`(?i)(cat|less|more|tail|head)\s+[^\n;]*(\.env|/\.env|id_rsa|id_ed25519)`),
}

func registerSandboxTools(r *Registry, deps *RunDependencies) {
	r.Register(SandboxStatusTool{Deps: deps})
	r.Register(SandboxStartTool{Deps: deps})
	r.Register(SandboxSyncTool{Deps: deps})
	r.Register(SandboxExecTool{Deps: deps})
	r.Register(SandboxReadLogsTool{Deps: deps})
	r.Register(ProcessRestartTool{Deps: deps})
	r.Register(ProcessLogsTool{Deps: deps})
	r.Register(SandboxServeArtifactTool{Deps: deps})
	r.Register(PreviewSetURLTool{Deps: deps})
	r.Register(PreviewOpenRouteTool{Deps: deps})
	r.Register(PreviewOpenArtifactTool{Deps: deps})
}

func sandboxControl(ctx context.Context, rc *RunContext, req SandboxControlRequest) (map[string]any, bool, error) {
	if rc == nil || rc.Deps == nil || rc.Deps.SandboxControl == nil {
		return nil, true, nil
	}
	if rc.Project == nil || rc.User == nil {
		return nil, true, nil
	}
	if (req.Action == "start" || req.Action == "sync") && rc.FileMap != nil {
		req.Files = rc.FileMap.Snapshot()
	}
	out, err := rc.Deps.SandboxControl(ctx, rc.Project, rc.User, req)
	if err != nil {
		if out != nil {
			return out, true, nil
		}
		return map[string]any{"error": err.Error()}, true, nil
	}
	return out, false, nil
}

func renderSandboxResult(out map[string]any) string {
	if out == nil {
		return "sandbox control is not configured"
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", out)
	}
	return string(b)
}

func cleanSandboxPath(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("path is required")
	}
	if strings.Contains(raw, "\x00") {
		return "", fmt.Errorf("path contains a null byte")
	}
	if !strings.HasPrefix(raw, "/") {
		return "", fmt.Errorf("path must be absolute inside the sandbox")
	}
	cleaned := path.Clean(raw)
	if cleaned == "." || cleaned == "/" {
		return "", fmt.Errorf("path must be inside an approved sandbox root")
	}
	return cleaned, nil
}

func sandboxPathInRoots(p string, roots ...string) bool {
	for _, root := range roots {
		if p == root || strings.HasPrefix(p, root+"/") {
			return true
		}
	}
	return false
}

func sandboxPathExposesSecret(p string) bool {
	lower := strings.ToLower(p)
	base := path.Base(lower)
	if strings.HasPrefix(base, ".env") {
		return true
	}
	return strings.Contains(lower, "/.git/") ||
		strings.Contains(lower, "id_rsa") ||
		strings.Contains(lower, "id_ed25519") ||
		strings.Contains(lower, "credentials")
}

func validateSandboxReadPath(raw string) (string, error) {
	p, err := cleanSandboxPath(raw)
	if err != nil {
		return "", err
	}
	if !sandboxPathInRoots(p, sandboxRootProject, sandboxRootBackend, sandboxRootArtifacts, sandboxRootTmp) {
		return "", fmt.Errorf("path %q is outside approved sandbox roots", p)
	}
	if sandboxPathExposesSecret(p) {
		return "", fmt.Errorf("reading %q is blocked because it may expose secrets", p)
	}
	return p, nil
}

func validateSandboxWritePath(raw string) (string, error) {
	p, err := cleanSandboxPath(raw)
	if err != nil {
		return "", err
	}
	if !sandboxPathInRoots(p, sandboxRootArtifacts, sandboxRootTmp) {
		return "", fmt.Errorf("sandbox writes are only allowed under %s or %s; use Git-backed app file tools for source files", sandboxRootArtifacts, sandboxRootTmp)
	}
	if sandboxPathExposesSecret(p) {
		return "", fmt.Errorf("writing %q is blocked because it may affect secrets", p)
	}
	return p, nil
}

func validateSandboxCwd(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return sandboxRootProject, nil
	}
	p, err := cleanSandboxPath(raw)
	if err != nil {
		return "", err
	}
	if !sandboxPathInRoots(p, sandboxRootProject, sandboxRootBackend, sandboxRootArtifacts, sandboxRootTmp) {
		return "", fmt.Errorf("cwd %q is outside approved sandbox roots", p)
	}
	if sandboxPathExposesSecret(p) {
		return "", fmt.Errorf("cwd %q is blocked because it may expose secrets", p)
	}
	return p, nil
}

func validateSandboxCommand(cmd string) error {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return fmt.Errorf("command is required")
	}
	if len(cmd) > 4000 {
		return fmt.Errorf("command is too long")
	}
	for _, re := range blockedSandboxCommandPatterns {
		if re.MatchString(cmd) {
			return fmt.Errorf("command blocked by sandbox policy")
		}
	}
	return nil
}

func allowedPreviewURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Hostname() == "" {
		return fmt.Errorf("preview URL is invalid")
	}
	if u.Scheme != "https" {
		return fmt.Errorf("preview URL must use https")
	}
	host := strings.ToLower(u.Hostname())
	if host == "preview.wemob.app" || strings.HasSuffix(host, ".preview.wemob.app") {
		return nil
	}
	return fmt.Errorf("preview URL host %q is not approved", host)
}

func joinPreviewRoute(baseURL, route string) (string, error) {
	if strings.TrimSpace(route) == "" {
		route = "/"
	}
	if !strings.HasPrefix(route, "/") {
		route = "/" + route
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("sandbox preview URL is unavailable")
	}
	u.Path = path.Clean(route)
	if route == "/" {
		u.Path = "/"
	}
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

type SandboxStatusTool struct{ Deps *RunDependencies }

func (SandboxStatusTool) Name() string { return "sandbox_status" }
func (SandboxStatusTool) Description() string {
	return "Inspect the current Daytona-backed sandbox state, preview URLs, known ports, dev processes, and log availability for this app."
}
func (SandboxStatusTool) Schema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (t SandboxStatusTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	out, isErr, err := sandboxControl(ctx, rc, SandboxControlRequest{Action: "status"})
	return renderSandboxResult(out), isErr, err
}

type SandboxStartTool struct{ Deps *RunDependencies }

func (SandboxStartTool) Name() string { return "sandbox_start" }
func (SandboxStartTool) Description() string {
	return "Start or resume the Daytona-backed sandbox, sync current Git-backed source files, and start the app preview processes."
}
func (SandboxStartTool) Schema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (t SandboxStartTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	out, isErr, err := sandboxControl(ctx, rc, SandboxControlRequest{Action: "start"})
	return renderSandboxResult(out), isErr, err
}

type SandboxSyncTool struct{ Deps *RunDependencies }

func (SandboxSyncTool) Name() string { return "sandbox_sync" }
func (SandboxSyncTool) Description() string {
	return "Sync the current Git-backed project source files into the existing Daytona-backed sandbox."
}
func (SandboxSyncTool) Schema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (t SandboxSyncTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	out, isErr, err := sandboxControl(ctx, rc, SandboxControlRequest{Action: "sync"})
	return renderSandboxResult(out), isErr, err
}

type SandboxExecTool struct{ Deps *RunDependencies }

func (SandboxExecTool) Name() string { return "sandbox_exec" }
func (SandboxExecTool) Description() string {
	return "Run a non-interactive shell command in the Daytona-backed sandbox from an approved cwd. Use /workspace/project for frontend/mobile work and /workspace/backend for backend work. Use Git-backed tools for app source edits; use this for tests, diagnostics, conversions, and artifact work."
}
func (SandboxExecTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command":    map[string]any{"type": "string", "description": "Shell command to run. Set cwd for the target app root; do not use cd prefixes."},
			"cwd":        map[string]any{"type": "string", "description": "Absolute sandbox cwd. Defaults to /workspace/project. Use /workspace/project for frontend/mobile commands, /workspace/backend for backend commands, /workspace/artifacts for artifact work, or /tmp for temporary work."},
			"timeout_ms": map[string]any{"type": "integer", "description": "Optional command timeout in milliseconds."},
		},
		"required": []string{"command"},
	}
}
func (t SandboxExecTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var args struct {
		Command   string `json:"command"`
		Cwd       string `json:"cwd"`
		TimeoutMs int    `json:"timeout_ms"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return fmt.Sprintf("invalid input: %v", err), true, nil
	}
	if err := validateSandboxCommand(args.Command); err != nil {
		return err.Error(), true, nil
	}
	cwd, err := validateSandboxCwd(args.Cwd)
	if err != nil {
		return err.Error(), true, nil
	}
	out, isErr, execErr := sandboxControl(ctx, rc, SandboxControlRequest{Action: "exec", Command: strings.TrimSpace(args.Command), Cwd: cwd, TimeoutMs: args.TimeoutMs})
	return renderSandboxResult(out), isErr, execErr
}

type SandboxReadLogsTool struct{ Deps *RunDependencies }

func (SandboxReadLogsTool) Name() string { return "sandbox_read_logs" }
func (SandboxReadLogsTool) Description() string {
	return "Read recent sandbox dev logs. target can be frontend, dev, expo_web, expo_go, backend, or artifacts."
}
func (SandboxReadLogsTool) Schema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{"target": map[string]any{"type": "string"}}}
}
func (t SandboxReadLogsTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var args struct {
		Target string `json:"target"`
	}
	_ = json.Unmarshal(input, &args)
	target := strings.TrimSpace(args.Target)
	if target == "" {
		target = "frontend"
	}
	if target == "dev" {
		target = "frontend"
	}
	if target != "frontend" && target != "expo_web" && target != "expo_go" && target != "backend" && target != "artifacts" {
		return "target must be one of: frontend, dev, expo_web, expo_go, backend, artifacts", true, nil
	}
	out, isErr, execErr := sandboxControl(ctx, rc, SandboxControlRequest{Action: "logs", Target: target})
	return renderSandboxResult(out), isErr, execErr
}

type ProcessRestartTool struct{ Deps *RunDependencies }

func (ProcessRestartTool) Name() string { return "process_restart" }
func (ProcessRestartTool) Description() string {
	return "Restart a named Daytona sandbox process such as frontend, backend, expo_web, expo_go, or artifacts."
}
func (ProcessRestartTool) Schema() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{"process": map[string]any{"type": "string"}},
		"required":   []string{"process"},
	}
}
func (t ProcessRestartTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var args struct {
		Process string `json:"process"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return fmt.Sprintf("invalid input: %v", err), true, nil
	}
	if strings.TrimSpace(args.Process) == "" {
		return "process is required", true, nil
	}
	out, isErr, execErr := sandboxControl(ctx, rc, SandboxControlRequest{Action: "process_restart", Process: strings.TrimSpace(args.Process)})
	return renderSandboxResult(out), isErr, execErr
}

type ProcessLogsTool struct{ Deps *RunDependencies }

func (ProcessLogsTool) Name() string { return "process_logs" }
func (ProcessLogsTool) Description() string {
	return "Read recent logs for a named Daytona sandbox process such as frontend, backend, expo_web, expo_go, or artifacts."
}
func (ProcessLogsTool) Schema() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{"process": map[string]any{"type": "string"}},
	}
}
func (t ProcessLogsTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var args struct {
		Process string `json:"process"`
	}
	_ = json.Unmarshal(input, &args)
	process := strings.TrimSpace(args.Process)
	if process == "" {
		process = "frontend"
	}
	out, isErr, execErr := sandboxControl(ctx, rc, SandboxControlRequest{Action: "process_logs", Process: process})
	return renderSandboxResult(out), isErr, execErr
}

type SandboxServeArtifactTool struct{ Deps *RunDependencies }

func (SandboxServeArtifactTool) Name() string { return "sandbox_serve_artifact" }
func (SandboxServeArtifactTool) Description() string {
	return "Serve a file under /workspace/artifacts and return its public Wemob preview URL. The path must be absolute, for example /workspace/artifacts/report.pdf."
}
func (SandboxServeArtifactTool) Schema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string", "description": "Absolute sandbox path under /workspace/artifacts, for example /workspace/artifacts/report.pdf."}}, "required": []string{"path"}}
}
func (t SandboxServeArtifactTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return fmt.Sprintf("invalid input: %v", err), true, nil
	}
	p, err := validateSandboxReadPath(args.Path)
	if err != nil {
		return err.Error(), true, nil
	}
	if !sandboxPathInRoots(p, sandboxRootArtifacts) {
		return "only files under /workspace/artifacts can be served publicly", true, nil
	}
	out, isErr, execErr := sandboxControl(ctx, rc, SandboxControlRequest{Action: "serve-artifact", Path: p})
	return renderSandboxResult(out), isErr, execErr
}

type PreviewSetURLTool struct{ Deps *RunDependencies }

func (PreviewSetURLTool) Name() string { return "preview_set_url" }
func (PreviewSetURLTool) Description() string {
	return "Set the user's active preview pane URL to an approved Wemob preview URL."
}
func (PreviewSetURLTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url":   map[string]any{"type": "string"},
			"label": map[string]any{"type": "string"},
			"kind":  map[string]any{"type": "string", "description": "app, artifact, logs, or external"},
		},
		"required": []string{"url"},
	}
}
func (t PreviewSetURLTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var args struct {
		URL   string `json:"url"`
		Label string `json:"label"`
		Kind  string `json:"kind"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return fmt.Sprintf("invalid input: %v", err), true, nil
	}
	if err := allowedPreviewURL(args.URL); err != nil {
		return err.Error(), true, nil
	}
	if args.Kind == "" {
		args.Kind = "external"
	}
	if args.Label == "" {
		args.Label = "Preview"
	}
	if rc == nil || rc.Deps == nil || rc.Deps.SetPreviewURL == nil || rc.Project == nil {
		return "preview URL persistence is not configured", true, nil
	}
	if err := rc.Deps.SetPreviewURL(ctx, rc.Project, args.URL, args.Label, args.Kind); err != nil {
		return fmt.Sprintf("failed to set preview URL: %v", err), true, nil
	}
	return renderSandboxResult(map[string]any{"url": args.URL, "label": args.Label, "kind": args.Kind}), false, nil
}

type PreviewOpenRouteTool struct{ Deps *RunDependencies }

func (PreviewOpenRouteTool) Name() string { return "preview_open_route" }
func (PreviewOpenRouteTool) Description() string {
	return "Open an app route in the user's preview pane using the current sandbox preview URL."
}
func (PreviewOpenRouteTool) Schema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{"route": map[string]any{"type": "string"}}, "required": []string{"route"}}
}
func (t PreviewOpenRouteTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var args struct {
		Route string `json:"route"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return fmt.Sprintf("invalid input: %v", err), true, nil
	}
	out, isErr, err := sandboxControl(ctx, rc, SandboxControlRequest{Action: "open_route", Route: args.Route})
	if err != nil || isErr {
		return renderSandboxResult(out), true, err
	}
	targetURL, _ := out["url"].(string)
	if targetURL == "" {
		return renderSandboxResult(out), true, nil
	}
	if err := allowedPreviewURL(targetURL); err != nil {
		return err.Error(), true, nil
	}
	if rc == nil || rc.Deps == nil || rc.Deps.SetPreviewURL == nil || rc.Project == nil {
		return "preview URL persistence is not configured", true, nil
	}
	if err := rc.Deps.SetPreviewURL(ctx, rc.Project, targetURL, "App preview", "app"); err != nil {
		return fmt.Sprintf("failed to open route: %v", err), true, nil
	}
	return renderSandboxResult(map[string]any{"url": targetURL, "kind": "app"}), false, nil
}

func previewBaseURLForProject(rc *RunContext, out map[string]any) (string, bool) {
	if out == nil {
		return "", false
	}
	if rc != nil && rc.Project != nil {
		if rc.Project.ProjectType == "mobile_app_native" {
			if urls, ok := out["preview_urls"].(map[string]any); ok {
				if u, _ := urls["expo_web"].(string); u != "" {
					return u, true
				}
			}
			if u, _ := out["expo_web_url"].(string); u != "" {
				return u, true
			}
		}
		if rc.Project.FrontendRuntime == "none" && rc.Project.BackendEnabled {
			if urls, ok := out["preview_urls"].(map[string]any); ok {
				if u, _ := urls["backend"].(string); u != "" {
					return u, true
				}
			}
			if u, _ := out["backend_url"].(string); u != "" {
				return u, true
			}
		}
	}
	if u, _ := out["preview_url"].(string); u != "" {
		return u, true
	}
	return "", false
}

type PreviewOpenArtifactTool struct{ Deps *RunDependencies }

func (PreviewOpenArtifactTool) Name() string { return "preview_open_artifact" }
func (PreviewOpenArtifactTool) Description() string {
	return "Serve an artifact under /workspace/artifacts and open it in the user's preview pane. The path must be absolute, for example /workspace/artifacts/report.pdf."
}
func (PreviewOpenArtifactTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":  map[string]any{"type": "string", "description": "Absolute sandbox path under /workspace/artifacts, for example /workspace/artifacts/report.pdf."},
			"label": map[string]any{"type": "string"},
		},
		"required": []string{"path"},
	}
}
func (t PreviewOpenArtifactTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var args struct {
		Path  string `json:"path"`
		Label string `json:"label"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return fmt.Sprintf("invalid input: %v", err), true, nil
	}
	p, err := validateSandboxReadPath(args.Path)
	if err != nil {
		return err.Error(), true, nil
	}
	if !sandboxPathInRoots(p, sandboxRootArtifacts) {
		return "only files under /workspace/artifacts can be opened as artifacts", true, nil
	}
	out, isErr, execErr := sandboxControl(ctx, rc, SandboxControlRequest{Action: "serve-artifact", Path: p})
	if execErr != nil || isErr {
		return renderSandboxResult(out), true, execErr
	}
	artifactURL, _ := out["url"].(string)
	if artifactURL == "" {
		return renderSandboxResult(out), true, nil
	}
	if err := allowedPreviewURL(artifactURL); err != nil {
		return err.Error(), true, nil
	}
	label := args.Label
	if label == "" {
		label = path.Base(p)
	}
	if rc == nil || rc.Deps == nil || rc.Deps.SetPreviewURL == nil || rc.Project == nil {
		return "preview URL persistence is not configured", true, nil
	}
	if err := rc.Deps.SetPreviewURL(ctx, rc.Project, artifactURL, label, "artifact"); err != nil {
		return fmt.Sprintf("failed to open artifact: %v", err), true, nil
	}
	out["preview_set"] = true
	out["label"] = label
	return renderSandboxResult(out), false, nil
}

func sandboxToolNames() []string {
	names := []string{
		"sandbox_status",
		"sandbox_start",
		"sandbox_sync",
		"sandbox_exec",
		"sandbox_read_logs",
		"process_restart",
		"process_logs",
		"sandbox_serve_artifact",
		"preview_set_url",
		"preview_open_route",
		"preview_open_artifact",
	}
	sort.Strings(names)
	return names
}
