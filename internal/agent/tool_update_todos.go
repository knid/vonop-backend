package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/knid/wemob/internal/models"
)

type UpdateTodosTool struct{}

func (UpdateTodosTool) Name() string { return "update_todos" }

func (UpdateTodosTool) Description() string {
	return "Update the durable build checklist. Call when starting, completing, blocking, or discovering todo items. Mark done only with concrete evidence."
}

func (UpdateTodosTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"updates": map[string]any{
				"type":        "array",
				"description": "Todo updates. Use an existing id to update, or provide title without id to add a discovered todo.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id": map[string]any{
							"type":        "string",
							"description": "Existing todo id, e.g. todo-1. Omit only when adding a new todo.",
						},
						"title": map[string]any{
							"type":        "string",
							"description": "New title when adding a todo, or a refined title for an existing todo.",
						},
						"status": map[string]any{
							"type": "string",
							"enum": []string{
								models.AgentTodoStatusPending,
								models.AgentTodoStatusInProgress,
								models.AgentTodoStatusDone,
								models.AgentTodoStatusBlocked,
							},
						},
						"evidence": map[string]any{
							"type":        "object",
							"description": "Concrete evidence for done/blocked items. Include touched files, tools used, or validation checks.",
							"properties": map[string]any{
								"files":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
								"tools":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
								"validation": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
							},
						},
					},
					"required": []string{"status"},
				},
			},
		},
		"required": []string{"updates"},
	}
}

func (UpdateTodosTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	if rc == nil || rc.Todos == nil {
		return "todo state is unavailable", true, nil
	}
	var args struct {
		Updates []TodoUpdate `json:"updates"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return fmt.Sprintf("invalid input: %v", err), true, nil
	}
	changed, err := rc.Todos.Apply(args.Updates)
	if err != nil {
		return err.Error(), true, nil
	}
	if rc.Deps != nil {
		rc.Todos.Persist(ctx, rc.Deps.DB, rc.RunID)
	}
	emitTodoSnapshot(rc)

	statuses := map[string]int{}
	for _, item := range rc.Todos.Snapshot() {
		statuses[item.Status]++
	}
	return fmt.Sprintf("updated %d todo(s): %d pending, %d in_progress, %d done, %d blocked",
		len(changed),
		statuses[models.AgentTodoStatusPending],
		statuses[models.AgentTodoStatusInProgress],
		statuses[models.AgentTodoStatusDone],
		statuses[models.AgentTodoStatusBlocked],
	), false, nil
}
