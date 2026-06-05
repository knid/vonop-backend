package views

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/agent"
	"github.com/knid/wemob/internal/responses"
)

func TestAgentSSEAdapterEmitsLegacyAndUIMessagePart(t *testing.T) {
	runID := uuid.New()
	ch := make(chan responses.SSEEvent, 4)
	adapter := newAgentSSEAdapter(ch, runID.String())

	ok := adapter.Emit(agent.EventNarration, agent.NarrationEvent{
		RunID: runID,
		Turn:  1,
		Delta: "hello",
	})
	if !ok {
		t.Fatal("Emit returned false")
	}

	legacy := <-ch
	if legacy.Event != agent.EventNarration {
		t.Fatalf("legacy event = %q, want %q", legacy.Event, agent.EventNarration)
	}

	normalized := <-ch
	if normalized.Event != responses.EventUIMessagePart {
		t.Fatalf("normalized event = %q, want %q", normalized.Event, responses.EventUIMessagePart)
	}
	payload, ok := normalized.Data.(responses.UIMessagePartEvent)
	if !ok {
		t.Fatalf("normalized data type = %T, want UIMessagePartEvent", normalized.Data)
	}
	if payload.RunID != runID.String() {
		t.Fatalf("run_id = %q, want %q", payload.RunID, runID.String())
	}
	if payload.SourceEvent != agent.EventNarration {
		t.Fatalf("source_event = %q, want %q", payload.SourceEvent, agent.EventNarration)
	}
	if payload.Part.Type != "text-delta" || payload.Part.Text != "hello" {
		t.Fatalf("part = %#v, want text-delta hello", payload.Part)
	}
}

func TestAgentSSEAdapterNormalizesToolLifecycle(t *testing.T) {
	runID := uuid.New()
	ch := make(chan responses.SSEEvent, 8)
	adapter := newAgentSSEAdapter(ch, runID.String())
	input := json.RawMessage(`{"path":"app/page.tsx"}`)

	if !adapter.Emit(agent.EventToolCallStart, agent.ToolCallStartEvent{
		RunID:        runID,
		Turn:         1,
		ToolCallID:   "toolu_1",
		ToolName:     "write_file",
		RiskLevel:    "low",
		InputPreview: input,
	}) {
		t.Fatal("start Emit returned false")
	}
	if !adapter.Emit(agent.EventToolCallResult, agent.ToolCallResultEvent{
		RunID:      runID,
		ToolCallID: "toolu_1",
		ToolName:   "write_file",
		RiskLevel:  "low",
		Status:     "ok",
		DurationMs: 42,
		Summary:    "Wrote app/page.tsx",
	}) {
		t.Fatal("result Emit returned false")
	}

	<-ch // legacy start
	startEvent := (<-ch).Data.(responses.UIMessagePartEvent)
	if startEvent.Part.Type != "tool" || startEvent.Part.State != "input-available" {
		t.Fatalf("start part = %#v", startEvent.Part)
	}
	if startEvent.Part.ToolCallID != "toolu_1" || startEvent.Part.ToolName != "write_file" {
		t.Fatalf("start tool identity = %#v", startEvent.Part)
	}
	inputMap, ok := startEvent.Part.Input.(map[string]any)
	if !ok || inputMap["path"] != "app/page.tsx" {
		t.Fatalf("input = %#v, want decoded path", startEvent.Part.Input)
	}

	<-ch // legacy result
	resultEvent := (<-ch).Data.(responses.UIMessagePartEvent)
	if resultEvent.Part.State != "output-available" {
		t.Fatalf("result state = %q", resultEvent.Part.State)
	}
	if resultEvent.Part.Output != "Wrote app/page.tsx" {
		t.Fatalf("result output = %#v", resultEvent.Part.Output)
	}
	if resultEvent.Part.DurationMs != 42 {
		t.Fatalf("duration = %d, want 42", resultEvent.Part.DurationMs)
	}
}
