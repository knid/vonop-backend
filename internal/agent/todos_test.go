package agent

import (
	"strings"
	"testing"

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
