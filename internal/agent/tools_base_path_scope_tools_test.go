package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/models"
)

// noopEmitter satisfies EventEmitter without doing anything. Used so
// WriteFileTool can emit its EventFileWritten event without test wiring.
type noopEmitter struct{}

func (noopEmitter) Emit(eventType string, data any) bool { return true }
func (noopEmitter) Buffer() []Event                      { return nil }
func (noopEmitter) Close()                               {}

func newPathScopeTestRC(projectType string) *RunContext {
	return &RunContext{
		RunID:   uuid.New(),
		Project: &models.AppProject{ID: uuid.New(), ProjectType: projectType},
		FileMap: NewSafeFileMap(nil),
		Emitter: noopEmitter{},
	}
}

func TestWriteFileTool_RejectsRootPathForMobileAppNative(t *testing.T) {
	tool := WriteFileTool{}
	rc := newPathScopeTestRC(models.ProjectTypeMobileAppNative)

	out, isError, err := tool.Execute(context.Background(), rc,
		json.RawMessage(`{"path":"package.json","content":"x"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !isError {
		t.Errorf("expected isError=true for root-path write on mobile_app_native; out=%s", out)
	}
	if !strings.Contains(out, "mobile/") {
		t.Errorf("expected guidance mentioning mobile/, got: %s", out)
	}
}

func TestWriteFileTool_AllowsMobilePathForMobileAppNative(t *testing.T) {
	tool := WriteFileTool{}
	rc := newPathScopeTestRC(models.ProjectTypeMobileAppNative)

	_, isError, err := tool.Execute(context.Background(), rc,
		json.RawMessage(`{"path":"mobile/app/index.tsx","content":"export default function () { return null }"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if isError {
		t.Error("expected isError=false for mobile/ path on mobile_app_native")
	}
}

func TestWriteFileTool_AllowsBackendPathForMobileAppNative(t *testing.T) {
	tool := WriteFileTool{}
	rc := newPathScopeTestRC(models.ProjectTypeMobileAppNative)

	_, isError, err := tool.Execute(context.Background(), rc,
		json.RawMessage(`{"path":"backend/app/api/users/route.ts","content":"export const GET = () => Response.json({ok:true})"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if isError {
		t.Error("expected isError=false for backend/ path on mobile_app_native")
	}
}

func TestWriteFileTool_NonMobileProjectsCanWriteAnywhere(t *testing.T) {
	tool := WriteFileTool{}
	rc := newPathScopeTestRC(models.ProjectTypeApp)

	_, isError, err := tool.Execute(context.Background(), rc,
		json.RawMessage(`{"path":"package.json","content":"x"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if isError {
		t.Error("expected isError=false for non-mobile root write")
	}
}

func TestEditFileTool_AppliesPathScope(t *testing.T) {
	// The EditFileTool requires the file to already exist — pre-seed via the FileMap.
	rc := newPathScopeTestRC(models.ProjectTypeMobileAppNative)
	rc.FileMap.Set("package.json", `{"name":"x"}`)

	tool := EditFileTool{}
	out, isError, err := tool.Execute(context.Background(), rc,
		json.RawMessage(`{"path":"package.json","old_string":"x","new_string":"y"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !isError {
		t.Errorf("expected isError=true for root edit on mobile_app_native; out=%s", out)
	}
}
