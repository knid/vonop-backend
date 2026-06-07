package views

import (
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/knid/wemob/internal/agent"
	"github.com/knid/wemob/internal/responses"
)

// agentSSEAdapter adapts agent.EventEmitter onto the responses.SSEEvent channel
// consumed by handleStreamOrAsync. Each agent.Event becomes one SSEEvent frame
// with the same type tag and data payload.
//
// The adapter does NOT maintain its own ring buffer — reconnection replay uses
// the existing controllers.EventBus attached to the job by handleStreamOrAsync.
type agentSSEAdapter struct {
	ch     chan<- responses.SSEEvent
	runID  string
	mu     sync.Mutex
	closed bool
}

func newAgentSSEAdapter(ch chan<- responses.SSEEvent, runID string) *agentSSEAdapter {
	return &agentSSEAdapter{ch: ch, runID: runID}
}

// Emit implements agent.EventEmitter.Emit — non-blocking 10s send with
// closed-channel protection. Drops on timeout are logged so operators have a
// signal when the consumer can't keep up; callers ignore the bool.
func (a *agentSSEAdapter) Emit(eventType string, data any) bool {
	events := []responses.SSEEvent{{Event: eventType, Data: data}}
	if normalized, ok := normalizeAgentEventForUIMessage(eventType, data); ok {
		events = append(events, normalized)
	}

	for _, ev := range events {
		if !a.emitOne(ev) {
			return false
		}
	}
	return true
}

func (a *agentSSEAdapter) emitOne(ev responses.SSEEvent) bool {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return false
	}
	a.mu.Unlock()

	select {
	case a.ch <- ev:
		return true
	case <-time.After(10 * time.Second):
		slog.Warn("agentSSEAdapter dropped event",
			"run_id", a.runID,
			"event_type", ev.Event,
			"buffer_full", true,
		)
		return false
	}
}

func normalizeAgentEventForUIMessage(eventType string, data any) (responses.SSEEvent, bool) {
	part, runID, ok := agentEventToUIMessagePart(eventType, data)
	if !ok {
		return responses.SSEEvent{}, false
	}
	return responses.SSEEvent{
		Event: responses.EventUIMessagePart,
		Data: responses.UIMessagePartEvent{
			RunID:       runID,
			SourceEvent: eventType,
			Part:        part,
		},
	}, true
}

func agentEventToUIMessagePart(eventType string, data any) (responses.UIMessagePart, string, bool) {
	switch eventType {
	case agent.EventReasoning:
		ev, ok := data.(agent.ReasoningEvent)
		if !ok || ev.Delta == "" {
			return responses.UIMessagePart{}, "", false
		}
		return responses.UIMessagePart{
			Type: "reasoning-delta",
			Text: ev.Delta,
		}, ev.RunID.String(), true
	case agent.EventNarration:
		ev, ok := data.(agent.NarrationEvent)
		if !ok || ev.Delta == "" {
			return responses.UIMessagePart{}, "", false
		}
		return responses.UIMessagePart{
			Type: "text-delta",
			Text: ev.Delta,
		}, ev.RunID.String(), true
	case agent.EventToolCallStart:
		ev, ok := data.(agent.ToolCallStartEvent)
		if !ok {
			return responses.UIMessagePart{}, "", false
		}
		return responses.UIMessagePart{
			Type:       "tool",
			ToolCallID: ev.ToolCallID,
			ToolName:   ev.ToolName,
			State:      "input-available",
			Input:      decodeRawJSON(ev.InputPreview),
			RiskLevel:  ev.RiskLevel,
		}, ev.RunID.String(), true
	case agent.EventToolCallResult:
		ev, ok := data.(agent.ToolCallResultEvent)
		if !ok {
			return responses.UIMessagePart{}, "", false
		}
		state := "output-available"
		var errorText string
		if ev.IsError || ev.Status == "error" {
			state = "output-error"
			errorText = ev.Summary
		}
		return responses.UIMessagePart{
			Type:       "tool",
			ToolCallID: ev.ToolCallID,
			ToolName:   ev.ToolName,
			State:      state,
			Output:     ev.Summary,
			ErrorText:  errorText,
			DurationMs: ev.DurationMs,
			RiskLevel:  ev.RiskLevel,
		}, ev.RunID.String(), true
	case agent.EventToolProgress:
		ev, ok := data.(agent.ToolProgressEvent)
		if !ok {
			return responses.UIMessagePart{}, "", false
		}
		return responses.UIMessagePart{
			Type:     "tool",
			ToolName: ev.ToolName,
			State:    "output-streaming",
			Output: map[string]any{
				"batch_id":    ev.BatchID,
				"batch_index": ev.BatchIndex,
				"batch_total": ev.BatchTotal,
				"path":        ev.Path,
				"operation":   ev.Operation,
				"bytes":       ev.Bytes,
				"message":     ev.Message,
			},
		}, ev.RunID.String(), true
	case agent.EventToolApprovalRequired:
		ev, ok := data.(agent.ToolApprovalRequiredEvent)
		if !ok {
			return responses.UIMessagePart{}, "", false
		}
		return responses.UIMessagePart{
			Type:         "tool-approval",
			State:        "approval-requested",
			ApprovalID:   ev.ApprovalID,
			ToolCallID:   ev.ToolCallID,
			ToolName:     ev.ToolName,
			ConnectionID: ev.ConnectionID,
			RiskLevel:    ev.RiskLevel,
			Input:        decodeRawJSON(ev.InputPreview),
		}, ev.RunID.String(), true
	case agent.EventToolApprovalResolved:
		ev, ok := data.(agent.ToolApprovalResolvedEvent)
		if !ok {
			return responses.UIMessagePart{}, "", false
		}
		return responses.UIMessagePart{
			Type:         "tool-approval",
			State:        "approval-responded",
			ApprovalID:   ev.ApprovalID,
			ToolName:     ev.ToolName,
			ConnectionID: ev.ConnectionID,
			Approved:     &ev.Approved,
		}, ev.RunID.String(), true
	case agent.EventTodoSnapshot:
		ev, ok := data.(agent.TodoSnapshotEvent)
		if !ok {
			return responses.UIMessagePart{}, "", false
		}
		return responses.UIMessagePart{
			Type:  "plan",
			State: "output-available",
			Todos: ev.Todos,
		}, ev.RunID.String(), true
	case agent.EventFileWritten:
		ev, ok := data.(agent.FileWrittenEvent)
		if !ok {
			return responses.UIMessagePart{}, "", false
		}
		return responses.UIMessagePart{
			Type:     "tool",
			ToolName: "write_file",
			State:    "output-available",
			Output: map[string]any{
				"path":        ev.Path,
				"operation":   ev.Operation,
				"bytes":       ev.Bytes,
				"content":     ev.Content,
				"batch_id":    ev.BatchID,
				"batch_index": ev.BatchIndex,
				"batch_total": ev.BatchTotal,
			},
		}, ev.RunID.String(), true
	case agent.EventFileDeleted:
		ev, ok := data.(agent.FileDeletedEvent)
		if !ok {
			return responses.UIMessagePart{}, "", false
		}
		return responses.UIMessagePart{
			Type:     "tool",
			ToolName: "delete_file",
			State:    "output-available",
			Output: map[string]any{
				"path":      ev.Path,
				"operation": "delete",
			},
		}, ev.RunID.String(), true
	default:
		return responses.UIMessagePart{}, "", false
	}
}

func decodeRawJSON(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	return v
}

// Buffer implements agent.EventEmitter.Buffer. This adapter does not buffer
// — replay goes through the EventBus registered by handleStreamOrAsync.
func (a *agentSSEAdapter) Buffer() []agent.Event {
	return nil
}

// Close marks the adapter closed. Does NOT close the underlying channel —
// that's owned by handleStreamOrAsync's goroutine.
func (a *agentSSEAdapter) Close() {
	a.mu.Lock()
	a.closed = true
	a.mu.Unlock()
}

// Compile-time check that the adapter satisfies the agent emitter interface.
var _ agent.EventEmitter = (*agentSSEAdapter)(nil)
