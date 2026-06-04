package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
)

func TestTodoListStartsEmptyForDirectRequests(t *testing.T) {
	todos := NewTodoList("make the button blue")

	if got := todos.Snapshot(); len(got) != 0 {
		t.Fatalf("expected no initial todos, got %+v", got)
	}
	rendered := todos.Render()
	if rendered != "" {
		t.Fatalf("expected no rendered checklist for empty todos, got:\n%s", rendered)
	}

	if _, err := todos.Apply([]TodoUpdate{{
		Title:  "make the button blue",
		Status: models.AgentTodoStatusDone,
		Evidence: models.AgentTodoEvidence{
			Files: []string{"app/page.tsx"},
			Tools: []string{"write_files"},
		},
	}}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	rendered = todos.Render()
	if !strings.Contains(rendered, "Completed todos") ||
		!strings.Contains(rendered, "[done] todo-1") {
		t.Fatalf("expected completed todo, got:\n%s", rendered)
	}
}

func TestTodoListLetsModelAddDiscoveredTodos(t *testing.T) {
	todos := NewTodoList("add auth, settings, and billing")
	changed, err := todos.Apply([]TodoUpdate{{
		Title:  "add settings page",
		Status: models.AgentTodoStatusPending,
	}})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(changed) != 1 || changed[0].ID != "todo-1" {
		t.Fatalf("expected new todo-1, got %+v", changed)
	}
	rendered := todos.Render()
	if !strings.Contains(rendered, "add settings page") {
		t.Fatalf("expected added todo in render, got:\n%s", rendered)
	}
}

func TestTodoListRequiresEvidenceForDone(t *testing.T) {
	todos := NewTodoList("build dashboard")
	_, err := todos.Apply([]TodoUpdate{{
		Title:  "build dashboard",
		Status: models.AgentTodoStatusDone,
	}})
	if err == nil {
		t.Fatal("expected evidence error")
	}
}

func TestUpdateTodosToolUpdatesAndEmitsSnapshot(t *testing.T) {
	emitter := &todoRecordingEmitter{}
	rc := &RunContext{
		RunID:   uuid.New(),
		Todos:   NewTodoList("build dashboard"),
		Emitter: emitter,
	}

	out, isErr, err := UpdateTodosTool{}.Execute(context.Background(), rc, json.RawMessage(`{
		"updates": [{
			"title": "build dashboard",
			"status": "in_progress"
		}]
	}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if isErr {
		t.Fatalf("expected success, got error result: %s", out)
	}
	if !strings.Contains(out, "updated 1 todo") {
		t.Fatalf("unexpected result: %s", out)
	}
	if len(emitter.events) != 1 || emitter.events[0].Type != EventTodoSnapshot {
		t.Fatalf("expected todo_snapshot event, got %+v", emitter.events)
	}
}

type todoRecordingEmitter struct {
	events []Event
}

func (e *todoRecordingEmitter) Emit(eventType string, data any) bool {
	e.events = append(e.events, Event{Type: eventType, Data: data})
	return true
}

func (e *todoRecordingEmitter) Buffer() []Event { return append([]Event(nil), e.events...) }
func (e *todoRecordingEmitter) Close()          {}
