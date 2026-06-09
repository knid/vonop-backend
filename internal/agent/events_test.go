package agent

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestSubagentEventSerialization(t *testing.T) {
	runID := uuid.New()

	start := SubagentStartEvent{
		RunID:       runID,
		SubagentID:  "sub-1",
		Name:        "planner",
		Description: "plan tasks",
		Parent:      "main",
	}
	raw, err := json.Marshal(start)
	if err != nil {
		t.Fatalf("marshal start: %v", err)
	}
	var gotStart SubagentStartEvent
	if err := json.Unmarshal(raw, &gotStart); err != nil {
		t.Fatalf("unmarshal start: %v", err)
	}
	if gotStart.RunID != runID || gotStart.SubagentID != start.SubagentID || gotStart.Name != start.Name {
		t.Fatalf("roundtrip start mismatch: %#v -> %#v", start, gotStart)
	}

	end := SubagentEndEvent{
		RunID:      runID,
		SubagentID: "sub-1",
		Name:       "planner",
		Status:     "ok",
		Summary:    "done",
	}
	raw, err = json.Marshal(end)
	if err != nil {
		t.Fatalf("marshal end: %v", err)
	}
	var gotEnd SubagentEndEvent
	if err := json.Unmarshal(raw, &gotEnd); err != nil {
		t.Fatalf("unmarshal end: %v", err)
	}
	if gotEnd.RunID != runID || gotEnd.Status != end.Status {
		t.Fatalf("roundtrip end mismatch: %#v -> %#v", end, gotEnd)
	}

	progress := SubagentProgressEvent{
		RunID:      runID,
		SubagentID: "sub-1",
		Name:       "planner",
		Message:    "working",
	}
	raw, err = json.Marshal(progress)
	if err != nil {
		t.Fatalf("marshal progress: %v", err)
	}
	var gotProgress SubagentProgressEvent
	if err := json.Unmarshal(raw, &gotProgress); err != nil {
		t.Fatalf("unmarshal progress: %v", err)
	}
	if gotProgress.Message != progress.Message || gotProgress.Name != progress.Name {
		t.Fatalf("roundtrip progress mismatch: %#v -> %#v", progress, gotProgress)
	}
}
