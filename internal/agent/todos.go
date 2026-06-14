package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
)

type TodoList struct {
	mu    sync.RWMutex
	items []models.AgentTodo
}

func NewTodoList(_ string) *TodoList {
	return &TodoList{}
}

func (t *TodoList) Snapshot() []models.AgentTodo {
	if t == nil {
		return nil
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]models.AgentTodo, len(t.items))
	copy(out, t.items)
	return out
}

func (t *TodoList) RawJSON() json.RawMessage {
	raw, err := json.Marshal(t.Snapshot())
	if err != nil || len(raw) == 0 {
		return json.RawMessage("[]")
	}
	return raw
}

func (t *TodoList) Persist(ctx context.Context, db *database.Database, runID uuid.UUID) {
	if t == nil || db == nil || runID == uuid.Nil {
		return
	}
	if err := db.UpdateAgentRunTodos(ctx, runID, t.RawJSON()); err != nil {
		// Todo persistence must not fail the generation; the current snapshot
		// still rides in memory and over SSE for this run.
		return
	}
}

func (t *TodoList) Render() string {
	items := t.Snapshot()
	if len(items) == 0 {
		return ""
	}

	open := make([]models.AgentTodo, 0, len(items))
	done := make([]models.AgentTodo, 0, len(items))
	blocked := make([]models.AgentTodo, 0, len(items))
	for _, item := range items {
		switch item.Status {
		case models.AgentTodoStatusDone:
			done = append(done, item)
		case models.AgentTodoStatusBlocked:
			blocked = append(blocked, item)
		default:
			open = append(open, item)
		}
	}

	var sb strings.Builder
	sb.WriteString("TODO STATE\n")
	sb.WriteString("==========\n\n")
	writeTodoGroup(&sb, "Open todos", open)
	writeTodoGroup(&sb, "Blocked todos", blocked)
	writeTodoGroup(&sb, "Completed todos", done)
	if len(open) == 0 && len(blocked) == 0 {
		sb.WriteString("No open todos remain. Validate the result and finish instead of starting unrelated work.\n")
	} else {
		sb.WriteString("Use this snapshot only to avoid repeating completed work. Continue with the next unresolved item using the normal file and execution tools.\n")
	}
	return sb.String()
}

type TodoUpdate struct {
	ID       string                   `json:"id"`
	Title    string                   `json:"title"`
	Status   string                   `json:"status"`
	Evidence models.AgentTodoEvidence `json:"evidence"`
}

func (t *TodoList) Apply(updates []TodoUpdate) ([]models.AgentTodo, error) {
	if t == nil {
		return nil, fmt.Errorf("todo state is unavailable")
	}
	if len(updates) == 0 {
		return nil, fmt.Errorf("updates must contain at least one todo update")
	}
	if len(updates) > 20 {
		return nil, fmt.Errorf("too many todo updates: %d (max 20)", len(updates))
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	changed := make([]models.AgentTodo, 0, len(updates))
	for _, update := range updates {
		update.ID = strings.TrimSpace(update.ID)
		update.Title = cleanTodoTitle(update.Title)
		update.Status = cleanTodoStatus(update.Status)
		update.Evidence = cleanTodoEvidence(update.Evidence)

		if update.Status == "" {
			return nil, fmt.Errorf("status is required for todo %q", firstNonEmpty(update.ID, update.Title))
		}

		idx := -1
		if update.ID != "" {
			idx = t.indexByIDLocked(update.ID)
			if idx < 0 {
				return nil, fmt.Errorf("unknown todo id: %s", update.ID)
			}
		} else if update.Title != "" {
			idx = t.indexByTitleLocked(update.Title)
		}

		now := time.Now().UTC()
		if idx < 0 {
			if update.Status == models.AgentTodoStatusDone && evidenceEmpty(update.Evidence) {
				return nil, fmt.Errorf("todo %q cannot be marked done without file, tool, or validation evidence", firstNonEmpty(update.ID, update.Title))
			}
			if update.Status == models.AgentTodoStatusBlocked && evidenceEmpty(update.Evidence) {
				return nil, fmt.Errorf("todo %q cannot be marked blocked without evidence explaining the blocker", firstNonEmpty(update.ID, update.Title))
			}
			if update.Title == "" {
				return nil, fmt.Errorf("title is required when adding a new todo")
			}
			item := models.AgentTodo{
				ID:        t.nextIDLocked(),
				Title:     update.Title,
				Status:    update.Status,
				Evidence:  update.Evidence,
				UpdatedAt: now,
			}
			t.items = append(t.items, item)
			changed = append(changed, item)
			continue
		}

		item := t.items[idx]
		if update.Title != "" {
			item.Title = update.Title
		}
		mergedEvidence := mergeTodoEvidence(item.Evidence, update.Evidence)
		if update.Status == models.AgentTodoStatusDone && evidenceEmpty(mergedEvidence) {
			return nil, fmt.Errorf("todo %q cannot be marked done without file, tool, or validation evidence", firstNonEmpty(update.ID, update.Title))
		}
		if update.Status == models.AgentTodoStatusBlocked && evidenceEmpty(mergedEvidence) {
			return nil, fmt.Errorf("todo %q cannot be marked blocked without evidence explaining the blocker", firstNonEmpty(update.ID, update.Title))
		}
		item.Status = update.Status
		item.Evidence = mergedEvidence
		item.UpdatedAt = now
		t.items[idx] = item
		changed = append(changed, item)
	}
	return changed, nil
}

func (t *TodoList) indexByIDLocked(id string) int {
	for i, item := range t.items {
		if item.ID == id {
			return i
		}
	}
	return -1
}

func (t *TodoList) indexByTitleLocked(title string) int {
	title = strings.ToLower(strings.TrimSpace(title))
	for i, item := range t.items {
		if strings.ToLower(item.Title) == title {
			return i
		}
	}
	return -1
}

func (t *TodoList) nextIDLocked() string {
	used := map[string]bool{}
	for _, item := range t.items {
		used[item.ID] = true
	}
	for i := len(t.items) + 1; ; i++ {
		id := fmt.Sprintf("todo-%d", i)
		if !used[id] {
			return id
		}
	}
}

func writeTodoGroup(sb *strings.Builder, label string, items []models.AgentTodo) {
	if len(items) == 0 {
		return
	}
	sb.WriteString(label)
	sb.WriteString(":\n")
	for _, item := range items {
		fmt.Fprintf(sb, "- [%s] %s: %s", item.Status, item.ID, item.Title)
		evidence := formatEvidence(item.Evidence)
		if evidence != "" {
			fmt.Fprintf(sb, " (%s)", evidence)
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
}

func formatEvidence(e models.AgentTodoEvidence) string {
	parts := []string{}
	if len(e.Files) > 0 {
		parts = append(parts, "files: "+strings.Join(limitStrings(e.Files, 3), ", "))
	}
	if len(e.Tools) > 0 {
		parts = append(parts, "tools: "+strings.Join(limitStrings(e.Tools, 3), ", "))
	}
	if len(e.Validation) > 0 {
		parts = append(parts, "validation: "+strings.Join(limitStrings(e.Validation, 2), ", "))
	}
	return strings.Join(parts, "; ")
}

func cleanTodoTitle(title string) string {
	title = strings.Join(strings.Fields(strings.TrimSpace(title)), " ")
	if len(title) > 120 {
		title = title[:120]
	}
	return title
}

func cleanTodoStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case models.AgentTodoStatusPending:
		return models.AgentTodoStatusPending
	case models.AgentTodoStatusInProgress:
		return models.AgentTodoStatusInProgress
	case models.AgentTodoStatusDone:
		return models.AgentTodoStatusDone
	case models.AgentTodoStatusBlocked:
		return models.AgentTodoStatusBlocked
	default:
		return ""
	}
}

func cleanTodoEvidence(e models.AgentTodoEvidence) models.AgentTodoEvidence {
	return models.AgentTodoEvidence{
		Files:      cleanStringList(e.Files, 12, 160),
		Tools:      cleanStringList(e.Tools, 12, 80),
		Validation: cleanStringList(e.Validation, 12, 180),
	}
}

func cleanStringList(in []string, maxItems, maxLen int) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range in {
		value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
		if value == "" {
			continue
		}
		if len(value) > maxLen {
			value = value[:maxLen]
		}
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
		if len(out) >= maxItems {
			break
		}
	}
	sort.Strings(out)
	return out
}

func evidenceEmpty(e models.AgentTodoEvidence) bool {
	return len(e.Files) == 0 && len(e.Tools) == 0 && len(e.Validation) == 0
}

func mergeTodoEvidence(a, b models.AgentTodoEvidence) models.AgentTodoEvidence {
	return models.AgentTodoEvidence{
		Files:      mergeStringLists(a.Files, b.Files, 12),
		Tools:      mergeStringLists(a.Tools, b.Tools, 12),
		Validation: mergeStringLists(a.Validation, b.Validation, 12),
	}
}

func mergeStringLists(a, b []string, max int) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, list := range [][]string{a, b} {
		for _, value := range list {
			if value == "" || seen[value] {
				continue
			}
			seen[value] = true
			out = append(out, value)
			if len(out) >= max {
				return out
			}
		}
	}
	return out
}

func limitStrings(in []string, max int) []string {
	if len(in) <= max {
		return in
	}
	out := append([]string(nil), in[:max]...)
	out = append(out, fmt.Sprintf("+%d more", len(in)-max))
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return "unknown"
}

func emitTodoSnapshot(rc *RunContext) {
	if rc == nil || rc.Todos == nil || rc.Emitter == nil {
		return
	}
	rc.Emitter.Emit(EventTodoSnapshot, TodoSnapshotEvent{
		RunID: rc.RunID,
		Todos: rc.Todos.Snapshot(),
	})
}
