package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/knid/wemob/internal/models"
)

func TestSandboxToolNamesAreReadOnlyPreviewTools(t *testing.T) {
	got := strings.Join(sandboxToolNames(), ",")
	if got != "preview_logs,preview_status" {
		t.Fatalf("sandbox tools = %q", got)
	}
}

func TestPreviewStatusToolIsReadOnly(t *testing.T) {
	var action string
	deps := &RunDependencies{
		SandboxControl: func(ctx context.Context, project *models.AppProject, user *models.User, req SandboxControlRequest) (map[string]any, error) {
			action = req.Action
			return map[string]any{"state": "ready"}, nil
		},
	}
	rc := &RunContext{Project: &models.AppProject{}, User: &models.User{}, Deps: deps}

	result, isErr, err := (PreviewStatusTool{Deps: deps}).Execute(context.Background(), rc, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if isErr {
		t.Fatalf("Execute returned isErr=true: %s", result)
	}
	if action != "preview_status" {
		t.Fatalf("action = %q, want preview_status", action)
	}
}

func TestPreviewLogsToolAllowsOnlyTargetSelection(t *testing.T) {
	var got SandboxControlRequest
	deps := &RunDependencies{
		SandboxControl: func(ctx context.Context, project *models.AppProject, user *models.User, req SandboxControlRequest) (map[string]any, error) {
			got = req
			return map[string]any{"logs": "ok"}, nil
		},
	}
	rc := &RunContext{Project: &models.AppProject{}, User: &models.User{}, Deps: deps}

	result, isErr, err := (PreviewLogsTool{Deps: deps}).Execute(context.Background(), rc, json.RawMessage(`{"target":"backend"}`))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if isErr {
		t.Fatalf("Execute returned isErr=true: %s", result)
	}
	if got.Action != "preview_logs" || got.Target != "backend" {
		t.Fatalf("request = %#v", got)
	}
}
