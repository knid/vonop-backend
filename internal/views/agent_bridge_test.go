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

func TestAgentSSEAdapterNormalizesReasoning(t *testing.T) {
	runID := uuid.New()
	ch := make(chan responses.SSEEvent, 4)
	adapter := newAgentSSEAdapter(ch, runID.String())

	ok := adapter.Emit(agent.EventReasoning, agent.ReasoningEvent{
		RunID: runID,
		Turn:  1,
		Delta: "thinking",
	})
	if !ok {
		t.Fatal("Emit returned false")
	}

	legacy := <-ch
	if legacy.Event != agent.EventReasoning {
		t.Fatalf("legacy event = %q, want %q", legacy.Event, agent.EventReasoning)
	}

	normalized := <-ch
	if normalized.Event != responses.EventUIMessagePart {
		t.Fatalf("normalized event = %q, want %q", normalized.Event, responses.EventUIMessagePart)
	}
	payload, ok := normalized.Data.(responses.UIMessagePartEvent)
	if !ok {
		t.Fatalf("normalized data type = %T, want UIMessagePartEvent", normalized.Data)
	}
	if payload.SourceEvent != agent.EventReasoning {
		t.Fatalf("source_event = %q, want %q", payload.SourceEvent, agent.EventReasoning)
	}
	if payload.Part.Type != "reasoning-delta" || payload.Part.Text != "thinking" {
		t.Fatalf("part = %#v, want reasoning-delta thinking", payload.Part)
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

func TestAgentSSEAdapterNormalizesToolProgress(t *testing.T) {
	runID := uuid.New()
	ch := make(chan responses.SSEEvent, 4)
	adapter := newAgentSSEAdapter(ch, runID.String())

	if !adapter.Emit(agent.EventToolProgress, agent.ToolProgressEvent{
		RunID:      runID,
		ToolName:   "write_files",
		BatchID:    "batch-1",
		BatchIndex: 2,
		BatchTotal: 4,
		Path:       "app/page.tsx",
		Operation:  "create",
		Bytes:      128,
		Message:    "create app/page.tsx (2/4)",
	}) {
		t.Fatal("progress Emit returned false")
	}

	legacy := <-ch
	if legacy.Event != agent.EventToolProgress {
		t.Fatalf("legacy event = %q, want %q", legacy.Event, agent.EventToolProgress)
	}
	progressEvent := (<-ch).Data.(responses.UIMessagePartEvent)
	if progressEvent.SourceEvent != agent.EventToolProgress {
		t.Fatalf("source event = %q", progressEvent.SourceEvent)
	}
	if progressEvent.Part.Type != "tool" || progressEvent.Part.ToolName != "write_files" || progressEvent.Part.State != "output-streaming" {
		t.Fatalf("progress part = %#v", progressEvent.Part)
	}
	output, ok := progressEvent.Part.Output.(map[string]any)
	if !ok {
		t.Fatalf("progress output type = %T", progressEvent.Part.Output)
	}
	if output["path"] != "app/page.tsx" || output["batch_index"] != 2 || output["batch_total"] != 4 {
		t.Fatalf("progress output = %#v", output)
	}
}

func TestAgentSSEAdapterIgnoresUnknownEventForUIMessage(t *testing.T) {
	runID := uuid.New()
	ch := make(chan responses.SSEEvent, 2)
	adapter := newAgentSSEAdapter(ch, runID.String())

	if !adapter.Emit(agent.EventSubagentProgress, agent.SubagentProgressEvent{
		RunID:      runID,
		SubagentID: "sub-1",
		Name:       "planner",
		Message:    "working",
	}) {
		t.Fatal("Emit returned false")
	}

	legacy := <-ch
	if legacy.Event != agent.EventSubagentProgress {
		t.Fatalf("legacy event = %q, want %q", legacy.Event, agent.EventSubagentProgress)
	}

	select {
	case <-ch:
		t.Fatal("expected no normalized ui_message_part for unknown subagent progress event")
	default:
	}
}
