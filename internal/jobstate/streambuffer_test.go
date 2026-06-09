package jobstate

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/agent"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
)

func TestStreamStateBufferAgentToolIDsAndCredits(t *testing.T) {
	b := NewStreamStateBuffer()
	runID := uuid.New()

	b.Apply(responses.SSEEvent{
		Seq:   10,
		Event: "tool_call_start",
		Data: agent.ToolCallStartEvent{
			RunID:        runID,
			Turn:         1,
			ToolCallID:   "tc-1",
			ToolName:     "read_files",
			InputPreview: json.RawMessage(`{"paths":["a.ts"]}`),
		},
	})
	b.Apply(responses.SSEEvent{
		Seq:   11,
		Event: "tool_call_start",
		Data: agent.ToolCallStartEvent{
			RunID:        runID,
			Turn:         1,
			ToolCallID:   "tc-2",
			ToolName:     "read_files",
			InputPreview: json.RawMessage(`{"paths":["b.ts"]}`),
		},
	})
	b.Apply(responses.SSEEvent{
		Seq:   12,
		Event: "tool_call_result",
		Data: agent.ToolCallResultEvent{
			RunID:      runID,
			ToolCallID: "tc-1",
			ToolName:   "read_files",
			Status:     "ok",
			Summary:    "read a.ts",
		},
	})
	b.Apply(responses.SSEEvent{
		Seq:   13,
		Event: "credits_update",
		Data: agent.CreditsUpdateEvent{
			RunID:       runID,
			Turn:        1,
			CreditsUsed: 12,
		},
	})

	raw, ok := b.Snapshot()
	if !ok {
		t.Fatal("expected dirty snapshot")
	}
	var got struct {
		CreditsUsed int `json:"credits_used"`
		CursorSeq   int `json:"cursor_seq"`
		ToolCalls   []struct {
			ToolCallID string `json:"tool_call_id"`
			Tool       string `json:"tool"`
			Result     string `json:"result"`
			Pending    bool   `json:"pending"`
		} `json:"tool_calls"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	if got.CreditsUsed != 12 {
		t.Fatalf("credits_used = %d, want 12", got.CreditsUsed)
	}
	if got.CursorSeq != 13 {
		t.Fatalf("cursor_seq = %d, want 13", got.CursorSeq)
	}
	if len(got.ToolCalls) != 1 {
		t.Fatalf("tool_calls len = %d, want 1", len(got.ToolCalls))
	}
	if got.ToolCalls[0].ToolCallID != "tc-2" || got.ToolCalls[0].Result != "" {
		t.Fatalf("completed tool should be removed; in-flight tool should remain: %+v", got.ToolCalls[0])
	}

	b.Apply(responses.SSEEvent{Event: "done", Data: map[string]any{}})
	raw, ok = b.Snapshot()
	if !ok {
		t.Fatal("expected terminal snapshot")
	}
	var terminal map[string]any
	if err := json.Unmarshal(raw, &terminal); err != nil {
		t.Fatalf("unmarshal terminal snapshot: %v", err)
	}
	if _, exists := terminal["credits_used"]; exists {
		t.Fatalf("credits_used should be cleared on done: %s", raw)
	}
}

func TestStreamStateBufferTodoSnapshot(t *testing.T) {
	b := NewStreamStateBuffer()
	runID := uuid.New()

	ok := b.Apply(responses.SSEEvent{
		Seq:   8,
		Event: "todo_snapshot",
		Data: agent.TodoSnapshotEvent{
			RunID: runID,
			Todos: []models.AgentTodo{{
				ID:     "todo-1",
				Title:  "dashboard",
				Status: models.AgentTodoStatusInProgress,
			}},
		},
	})
	if !ok {
		t.Fatal("expected todo_snapshot to mutate buffer")
	}
	raw, ok := b.Snapshot()
	if !ok {
		t.Fatal("expected dirty snapshot")
	}
	var got struct {
		CursorSeq int                `json:"cursor_seq"`
		Todos     []models.AgentTodo `json:"todos"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	if got.CursorSeq != 8 {
		t.Fatalf("cursor_seq = %d, want 8", got.CursorSeq)
	}
	if len(got.Todos) != 1 || got.Todos[0].Status != models.AgentTodoStatusInProgress {
		t.Fatalf("unexpected todos snapshot: %+v", got.Todos)
	}
}

func TestStreamStateBufferIgnoresSubagentProgressEvents(t *testing.T) {
	b := NewStreamStateBuffer()
	runID := uuid.New()

	b.Apply(responses.SSEEvent{
		Seq:   1,
		Event: "narration",
		Data: agent.NarrationEvent{
			RunID: runID,
			Turn:  1,
			Delta: "starting",
		},
	})
	raw, ok := b.Snapshot()
	if !ok {
		t.Fatal("expected dirty snapshot after narration")
	}

	var replay struct {
		StreamingText string `json:"streaming_text"`
	}
	if err := json.Unmarshal(raw, &replay); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	if replay.StreamingText != "starting" {
		t.Fatalf("streaming_text = %q, want %q", replay.StreamingText, "starting")
	}

	changed := b.Apply(responses.SSEEvent{
		Seq:   2,
		Event: "subagent_progress",
		Data: agent.SubagentProgressEvent{
			RunID:      runID,
			SubagentID: "sub-1",
			Name:       "planner",
			Message:    "creating plan",
		},
	})
	if changed {
		t.Fatal("expected subagent_progress to be ignored by stream state")
	}

	if _, ok := b.Snapshot(); ok {
		t.Fatal("expected no snapshot update for ignored subagent_progress event")
	}
}
