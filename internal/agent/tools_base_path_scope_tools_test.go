package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/models"
)

// noopEmitter satisfies EventEmitter without doing anything. Used so
// write tools can emit EventFileWritten without test wiring.
type noopEmitter struct{}

func (noopEmitter) Emit(eventType string, data any) bool { return true }
func (noopEmitter) Buffer() []Event                      { return nil }
func (noopEmitter) Close()                               {}

type captureEmitter struct {
	events []Event
}

func (e *captureEmitter) Emit(eventType string, data any) bool {
	e.events = append(e.events, Event{Type: eventType, Data: data})
	return true
}
func (e *captureEmitter) Buffer() []Event { return e.events }
func (e *captureEmitter) Close()          {}

func newPathScopeTestRC(projectType string) *RunContext {
	return &RunContext{
		RunID:         uuid.New(),
		Project:       &models.AppProject{ID: uuid.New(), ProjectType: projectType},
		FileMap:       NewSafeFileMap(nil),
		Emitter:       noopEmitter{},
		ImageCache:    make(map[string]string),
		ImageInflight: make(map[string]*imageInflightCall),
	}
}

func TestWriteFilesTool_RejectsRootPathForMobileAppNative(t *testing.T) {
	tool := WriteFilesTool{}
	rc := newPathScopeTestRC(models.ProjectTypeMobileAppNative)

	out, isError, err := tool.Execute(context.Background(), rc,
		json.RawMessage(`{"files":[{"path":"package.json","content":"x"}]}`))
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

func TestWriteFilesTool_AllowsMobilePathForMobileAppNative(t *testing.T) {
	tool := WriteFilesTool{}
	rc := newPathScopeTestRC(models.ProjectTypeMobileAppNative)

	_, isError, err := tool.Execute(context.Background(), rc,
		json.RawMessage(`{"files":[{"path":"mobile/app/index.tsx","content":"export default function () { return null }"}]}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if isError {
		t.Error("expected isError=false for mobile/ path on mobile_app_native")
	}
}

func TestWriteFilesTool_AllowsBackendPathForMobileAppNative(t *testing.T) {
	tool := WriteFilesTool{}
	rc := newPathScopeTestRC(models.ProjectTypeMobileAppNative)

	_, isError, err := tool.Execute(context.Background(), rc,
		json.RawMessage(`{"files":[{"path":"backend/app/api/users/route.ts","content":"export const GET = () => Response.json({ok:true})"}]}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if isError {
		t.Error("expected isError=false for backend/ path on mobile_app_native")
	}
}

func TestWriteFilesTool_NonMobileProjectsCanWriteAnywhere(t *testing.T) {
	tool := WriteFilesTool{}
	rc := newPathScopeTestRC(models.ProjectTypeApp)

	_, isError, err := tool.Execute(context.Background(), rc,
		json.RawMessage(`{"files":[{"path":"package.json","content":"x"}]}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if isError {
		t.Error("expected isError=false for non-mobile root write")
	}
}

func TestEditFilesTool_AppliesPathScope(t *testing.T) {
	// The EditFilesTool requires the file to already exist — pre-seed via the FileMap.
	rc := newPathScopeTestRC(models.ProjectTypeMobileAppNative)
	rc.FileMap.Set("package.json", `{"name":"x"}`)

	tool := EditFilesTool{}
	out, isError, err := tool.Execute(context.Background(), rc,
		json.RawMessage(`{"files":[{"path":"package.json","edits":[{"old_string":"x","new_string":"y"}]}]}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !isError {
		t.Errorf("expected isError=true for root edit on mobile_app_native; out=%s", out)
	}
}

func TestWriteFilesTool_RejectsMalformedDesignBrief(t *testing.T) {
	tool := WriteFilesTool{}
	rc := newPathScopeTestRC(models.ProjectTypeApp)

	out, isError, err := tool.Execute(context.Background(), rc,
		json.RawMessage(`{"files":[{"path":"DESIGN.md","content":"# Design Brief\n\n## Identity\nMara Voss\n"}]}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !isError {
		t.Fatalf("expected malformed DESIGN.md to fail, out=%s", out)
	}
	for _, want := range []string{"register", "design direction"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in output, got: %s", want, out)
		}
	}
}

func TestCreateDesignTool_WritesValidDesignBrief(t *testing.T) {
	tool := CreateDesignTool{}
	rc := newPathScopeTestRC(models.ProjectTypeApp)

	out, isError, err := tool.Execute(context.Background(), rc, json.RawMessage(`{
		"register":"brand",
		"design_direction":"Editorial portfolio with restrained luxury and asymmetric whitespace.",
		"color_system":"Background #FAFAF7, foreground #171713, primary #123C32, accent #C2410C.",
		"typography":"Serif display for headlines, sans for UI and body copy.",
		"layout":"Max width 6xl, generous section rhythm, responsive project grids.",
		"components":"Nav, footer, project cards, contact form with visible states.",
		"imagery":"Art-directed portfolio photography and case study crops.",
		"accessibility":"High contrast text, visible focus states, no overlapping labels."
	}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if isError {
		t.Fatalf("expected create_design success, got: %s", out)
	}
	content, ok := rc.FileMap.Get("DESIGN.md")
	if !ok {
		t.Fatal("expected DESIGN.md to be written")
	}
	if err := validateDesignBriefContent(content); err != nil {
		t.Fatalf("expected valid design brief, got: %v\n%s", err, content)
	}
}

func TestWriteFilesTool_EmitsBatchMetadata(t *testing.T) {
	tool := WriteFilesTool{}
	emitter := &captureEmitter{}
	rc := newPathScopeTestRC(models.ProjectTypeApp)
	rc.Emitter = emitter

	out, isError, err := tool.Execute(context.Background(), rc, json.RawMessage(`{
		"files":[
			{"path":"app/a.tsx","content":"export default function A(){return null}"},
			{"path":"app/b.tsx","content":"export default function B(){return null}"}
		]
	}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if isError {
		t.Fatalf("expected write_files success, got: %s", out)
	}
	if len(emitter.events) != 4 {
		t.Fatalf("expected 2 tool_progress and 2 file_written events, got %d", len(emitter.events))
	}
	if emitter.events[0].Type != EventToolProgress || emitter.events[1].Type != EventFileWritten ||
		emitter.events[2].Type != EventToolProgress || emitter.events[3].Type != EventFileWritten {
		t.Fatalf("unexpected event order: %#v", emitter.events)
	}
	firstProgress := emitter.events[0].Data.(ToolProgressEvent)
	secondProgress := emitter.events[2].Data.(ToolProgressEvent)
	first := emitter.events[1].Data.(FileWrittenEvent)
	second := emitter.events[3].Data.(FileWrittenEvent)
	if first.BatchID == "" || first.BatchID != second.BatchID {
		t.Fatalf("expected shared batch id, got %q and %q", first.BatchID, second.BatchID)
	}
	if first.BatchIndex != 1 || second.BatchIndex != 2 || first.BatchTotal != 2 || second.BatchTotal != 2 {
		t.Fatalf("unexpected batch indexes: %+v %+v", first, second)
	}
	if firstProgress.BatchID != first.BatchID || secondProgress.BatchID != first.BatchID {
		t.Fatalf("expected progress events to share batch id %q, got %q and %q", first.BatchID, firstProgress.BatchID, secondProgress.BatchID)
	}
	if firstProgress.BatchIndex != 1 || secondProgress.BatchIndex != 2 || firstProgress.ToolName != "write_files" {
		t.Fatalf("unexpected progress metadata: %+v %+v", firstProgress, secondProgress)
	}
}

func TestGenerateImageTool_DedupesConcurrentIdenticalRequests(t *testing.T) {
	var calls int32
	tool := GenerateImageTool{Deps: &RunDependencies{
		GenerateImage: func(ctx context.Context, prompt string, w, h int, style, projectID string) (string, error) {
			atomic.AddInt32(&calls, 1)
			return "https://example.com/image.png", nil
		},
	}}
	rc := newPathScopeTestRC(models.ProjectTypeApp)
	input := json.RawMessage(`{"prompt":"Editorial hero image","width":1200,"height":900,"style":"photo"}`)

	var wg sync.WaitGroup
	errs := make(chan string, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, isError, err := tool.Execute(context.Background(), rc, input)
			if err != nil || isError {
				errs <- out
			}
		}()
	}
	wg.Wait()
	close(errs)
	for out := range errs {
		t.Fatalf("expected image generation success, got: %s", out)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected one underlying image generation, got %d", got)
	}
}
