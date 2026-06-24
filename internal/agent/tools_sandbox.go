package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/knid/wemob/internal/models"
	sandboxService "github.com/knid/wemob/internal/services/sandbox"
)

func registerSandboxTools(r *Registry, deps *RunDependencies, project *models.AppProject) {
	r.Register(PreviewStatusTool{Deps: deps})
	r.Register(PreviewLogsTool{Deps: deps})
	r.Register(SandboxExecTool{Deps: deps})
}

func sandboxControl(ctx context.Context, rc *RunContext, req SandboxControlRequest) (map[string]any, bool, error) {
	if rc == nil || rc.Deps == nil || rc.Deps.SandboxControl == nil {
		return nil, true, nil
	}
	if rc.Project == nil || rc.User == nil {
		return nil, true, nil
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
		return "preview control is not configured"
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", out)
	}
	return string(b)
}

type PreviewStatusTool struct{ Deps *RunDependencies }

func (PreviewStatusTool) Name() string { return "preview_status" }
func (PreviewStatusTool) Description() string {
	return "Read the current backend-managed preview status. This tool does not start, stop, sync, restart, or modify the preview."
}
func (PreviewStatusTool) Schema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (t PreviewStatusTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	out, isErr, err := sandboxControl(ctx, rc, SandboxControlRequest{Action: "preview_status"})
	return renderSandboxResult(out), isErr, err
}

type PreviewLogsTool struct{ Deps *RunDependencies }

func (PreviewLogsTool) Name() string { return "preview_logs" }
func (PreviewLogsTool) Description() string {
	return "Read recent backend-managed preview logs. Optional target can be web, backend, expo, or all."
}
func (PreviewLogsTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"target": map[string]any{"type": "string", "description": "Optional log target: web, backend, expo, or all."},
		},
	}
}
func (t PreviewLogsTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var args struct {
		Target string `json:"target"`
	}
	_ = json.Unmarshal(input, &args)
	target := strings.TrimSpace(args.Target)
	if target == "" {
		target = "all"
	}
	switch target {
	case "all", "web", "backend", "expo":
	default:
		return "target must be one of: all, web, backend, expo", true, nil
	}
	out, isErr, err := sandboxControl(ctx, rc, SandboxControlRequest{Action: "preview_logs", Target: target})
	return renderSandboxResult(out), isErr, err
}

type SandboxExecTool struct{ Deps *RunDependencies }

func (SandboxExecTool) Name() string { return "sandbox_exec" }
func (SandboxExecTool) Description() string {
	return "Run one finite command inside the isolated Daytona preview sandbox for builds, tests, installs, diagnostics, curl, pandoc, artifact work, temporary files, and sandbox-local shell operations. Persistent app source changes must still be made with Git-backed file tools."
}
func (SandboxExecTool) Schema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []any{"command"},
		"properties": map[string]any{
			"command": map[string]any{"type": "string", "description": "One finite shell command, for example npm run build, npx tsc --noEmit, npm install, mkdir -p /tmp/out && cp package.json /tmp/out, curl -I http://localhost:3000, or pandoc input.md -o /workspace/artifacts/out.pdf. Sandbox-local filesystem changes are allowed, but persistent source changes must use Git-backed file tools."},
			"cwd": map[string]any{
				"type":        "string",
				"description": "Working directory alias or absolute workspace path. Use frontend, backend, project, artifacts, or an absolute path under /workspace/project, /workspace/backend, /workspace/artifacts, or /tmp.",
			},
			"timeout_ms": map[string]any{"type": "number", "description": "Optional timeout in milliseconds. Defaults to 30000 and is capped at 120000."},
		},
	}
}
func (t SandboxExecTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var args struct {
		Command   string `json:"command"`
		Cwd       string `json:"cwd"`
		TimeoutMs int    `json:"timeout_ms"`
	}
	_ = json.Unmarshal(input, &args)
	command := strings.TrimSpace(args.Command)
	if command == "" {
		return "command is required", true, nil
	}
	if msg := validateSandboxExecCommand(command); msg != "" {
		return msg, true, nil
	}
	var project *models.AppProject
	if rc != nil {
		project = rc.Project
	}
	cwd, msg := normalizeSandboxExecCwd(project, args.Cwd)
	if msg != "" {
		return msg, true, nil
	}
	if msg := syncChangedFilesBeforeSandboxExec(ctx, rc); msg != "" {
		return msg, true, nil
	}
	out, isErr, err := sandboxControl(ctx, rc, SandboxControlRequest{
		Action:    "exec",
		Command:   command,
		Cwd:       cwd,
		TimeoutMs: clampSandboxExecTimeout(args.TimeoutMs),
	})
	out = truncateSandboxExecOutput(out)
	return renderSandboxResult(out), isErr, err
}

func syncChangedFilesBeforeSandboxExec(ctx context.Context, rc *RunContext) string {
	if rc == nil || rc.Deps == nil || rc.Project == nil || rc.BuildState == nil {
		return ""
	}
	paths := rc.BuildState.ChangedPaths()
	if len(paths) == 0 {
		return ""
	}
	if rc.Deps.SyncSandboxDeltas != nil && rc.FileMap != nil {
		if err := rc.Deps.SyncSandboxDeltas(ctx, rc.Project, sandboxDeltasFromFileMap(rc.FileMap, paths)); err != nil {
			return fmt.Sprintf("sandbox sync failed before exec: %v. Do not change source or package scripts to work around this; retry preview sync/restart first.", err)
		}
		return ""
	}
	if rc.Deps.SyncSandboxFiles == nil {
		return ""
	}
	if err := rc.Deps.SyncSandboxFiles(ctx, rc.Project, paths); err != nil {
		return fmt.Sprintf("sandbox sync failed before exec: %v. Do not change source or package scripts to work around this; retry preview sync/restart first.", err)
	}
	return ""
}

func sandboxDeltasFromFileMap(fileMap *SafeFileMap, paths []string) []sandboxService.FileDelta {
	deltas := make([]sandboxService.FileDelta, 0, len(paths))
	for _, p := range paths {
		if content, ok := fileMap.Get(p); ok {
			deltas = append(deltas, sandboxService.FileDelta{Path: p, Content: content})
			continue
		}
		deltas = append(deltas, sandboxService.FileDelta{Path: p, Delete: true})
	}
	return deltas
}

func sandboxToolNames() []string {
	names := []string{"preview_status", "preview_logs", "sandbox_exec"}
	sort.Strings(names)
	return names
}

const (
	sandboxExecDefaultTimeoutMs = 30000
	sandboxExecMaxTimeoutMs     = 120000
	sandboxExecOutputLimit      = 12000
)

func validateSandboxExecCommand(command string) string {
	if strings.ContainsAny(command, "\r\n") {
		return "sandbox_exec accepts one finite command per call; do not pass multi-line shell scripts"
	}
	return ""
}

func normalizeSandboxExecCwd(project *models.AppProject, cwd string) (string, string) {
	clean := strings.TrimSpace(cwd)
	if clean == "" || clean == "default" {
		return defaultSandboxExecCwd(project), ""
	}
	switch clean {
	case "project":
		return sandboxService.ProjectRoot, ""
	case "frontend", "web":
		if project != nil && project.UsesSplitWebRuntime() {
			return sandboxService.ProjectRoot + "/frontend", ""
		}
		return sandboxService.ProjectRoot, ""
	case "mobile":
		return sandboxService.ProjectRoot, ""
	case "backend", "api":
		return sandboxService.BackendRoot, ""
	case "artifacts", "artifact":
		return sandboxService.ArtifactsRoot, ""
	}
	if !strings.HasPrefix(clean, "/") {
		return "", "cwd must be one of: frontend, backend, project, artifacts, or an absolute workspace path"
	}
	clean = path.Clean(clean)
	if clean == sandboxService.ProjectRoot || strings.HasPrefix(clean, sandboxService.ProjectRoot+"/") ||
		clean == sandboxService.BackendRoot || strings.HasPrefix(clean, sandboxService.BackendRoot+"/") ||
		clean == sandboxService.ArtifactsRoot || strings.HasPrefix(clean, sandboxService.ArtifactsRoot+"/") ||
		clean == "/tmp" || strings.HasPrefix(clean, "/tmp/") {
		return clean, ""
	}
	return "", "cwd must stay under /workspace/project, /workspace/backend, /workspace/artifacts, or /tmp"
}

func defaultSandboxExecCwd(project *models.AppProject) string {
	if project == nil {
		return sandboxService.ProjectRoot
	}
	if project.ProjectType == models.ProjectTypeApp && project.FrontendRuntime == models.FrontendRuntimeNone {
		return sandboxService.BackendRoot
	}
	if project.UsesSplitWebRuntime() {
		return sandboxService.ProjectRoot + "/frontend"
	}
	return sandboxService.ProjectRoot
}

func clampSandboxExecTimeout(timeoutMs int) int {
	if timeoutMs <= 0 {
		return sandboxExecDefaultTimeoutMs
	}
	if timeoutMs > sandboxExecMaxTimeoutMs {
		return sandboxExecMaxTimeoutMs
	}
	return timeoutMs
}

func truncateSandboxExecOutput(out map[string]any) map[string]any {
	if out == nil {
		return nil
	}
	copied := make(map[string]any, len(out))
	for key, value := range out {
		if s, ok := value.(string); ok && (key == "stdout" || key == "stderr") {
			copied[key] = truncateSandboxExecString(s)
			continue
		}
		copied[key] = value
	}
	return copied
}

func truncateSandboxExecString(value string) string {
	if len(value) <= sandboxExecOutputLimit {
		return value
	}
	return value[:sandboxExecOutputLimit] + "\n... truncated ..."
}
