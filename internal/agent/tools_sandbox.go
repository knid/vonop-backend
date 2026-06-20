package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

func registerSandboxTools(r *Registry, deps *RunDependencies) {
	r.Register(PreviewStatusTool{Deps: deps})
	r.Register(PreviewLogsTool{Deps: deps})
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

func sandboxToolNames() []string {
	names := []string{"preview_status", "preview_logs"}
	sort.Strings(names)
	return names
}
