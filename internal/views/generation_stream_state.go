package views

import (
	"encoding/json"
	"sync"

	"github.com/knid/wemob/internal/agent"
	"github.com/knid/wemob/internal/responses"
)

// streamStateToolCall mirrors the SSE tool_call payload shape for rehydration.
// Result/Status/Summary are only populated after the matching tool_result event
// has been folded in; before that the entry is in either Pending or "running"
// state depending on whether the args have arrived yet.
type streamStateToolCall struct {
	Tool      string `json:"tool"`
	Arguments string `json:"arguments,omitempty"`
	Pending   bool   `json:"pending,omitempty"`
	Result    string `json:"result,omitempty"`
	Summary   string `json:"summary,omitempty"`
	Status    string `json:"status,omitempty"` // "ok" | "error"
}

// streamStateProgress mirrors responses.ProgressEvent but only the display-relevant fields.
type streamStateProgress struct {
	Phase   string `json:"phase,omitempty"`
	Message string `json:"message,omitempty"`
	Current int    `json:"current,omitempty"`
	Total   int    `json:"total,omitempty"`
}

// streamStateBuffer accumulates the partial streaming data for a running job so
// clients that disconnect mid-stream can rehydrate on reconnect. All mutations must
// go through its methods; the internal mutex makes it safe to flush from a separate
// goroutine while the event loop keeps ingesting events.
type streamStateBuffer struct {
	mu               sync.Mutex
	streamingText    string
	thinkingText     string
	conversationText string
	toolCalls        []streamStateToolCall
	progress         *streamStateProgress
	phase            string
	dirty            bool
}

func newStreamStateBuffer() *streamStateBuffer {
	return &streamStateBuffer{}
}

// Apply folds a single SSE event into the buffer. It returns true when the event
// actually mutated the buffer, which lets the caller gate unnecessary flushes.
func (b *streamStateBuffer) Apply(event responses.SSEEvent) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch event.Event {
	case "chat_text":
		if d, ok := event.Data.(responses.ChatTextEvent); ok && d.Chunk != "" {
			b.streamingText += d.Chunk
			b.dirty = true
			return true
		}
		if m, ok := event.Data.(map[string]any); ok {
			if chunk, _ := m["chunk"].(string); chunk != "" {
				b.streamingText += chunk
				b.dirty = true
				return true
			}
		}

	case "chat_thinking":
		if d, ok := event.Data.(responses.ChatThinkingEvent); ok && d.Chunk != "" {
			b.thinkingText += d.Chunk
			b.dirty = true
			return true
		}
		if m, ok := event.Data.(map[string]any); ok {
			if chunk, _ := m["chunk"].(string); chunk != "" {
				b.thinkingText += chunk
				b.dirty = true
				return true
			}
		}

	case "conversation_chunk":
		if m, ok := event.Data.(map[string]any); ok {
			if chunk, _ := m["chunk"].(string); chunk != "" {
				b.conversationText += chunk
				b.dirty = true
				return true
			}
		}

	case "conversation_done":
		// Authoritative full text supersedes the streamed chunks; the next phase starts
		// from a clean conversation buffer after the client has rehydrated it.
		if m, ok := event.Data.(map[string]any); ok {
			if text, _ := m["text"].(string); text != "" {
				b.conversationText = text
				b.dirty = true
				return true
			}
		}

	case "tool_start":
		if d, ok := event.Data.(map[string]string); ok {
			b.toolCalls = append(b.toolCalls, streamStateToolCall{Tool: d["tool"], Pending: true})
			b.dirty = true
			return true
		}
		if m, ok := event.Data.(map[string]any); ok {
			if tool, _ := m["tool"].(string); tool != "" {
				b.toolCalls = append(b.toolCalls, streamStateToolCall{Tool: tool, Pending: true})
				b.dirty = true
				return true
			}
		}

	case "tool_call":
		if d, ok := event.Data.(responses.ToolCallEvent); ok {
			b.replaceOrAppendToolCall(d.Tool, d.Arguments)
			b.dirty = true
			return true
		}
		if m, ok := event.Data.(map[string]any); ok {
			tool, _ := m["tool"].(string)
			args, _ := m["arguments"].(string)
			if tool != "" {
				b.replaceOrAppendToolCall(tool, args)
				b.dirty = true
				return true
			}
		}

	case "tool_result":
		if d, ok := event.Data.(responses.ToolResultEvent); ok {
			b.attachToolResult(d.Tool, d.Preview, d.Summary, d.Status)
			b.dirty = true
			return true
		}
		if m, ok := event.Data.(map[string]any); ok {
			tool, _ := m["tool"].(string)
			preview, _ := m["preview"].(string)
			summary, _ := m["summary"].(string)
			status, _ := m["status"].(string)
			if tool != "" {
				b.attachToolResult(tool, preview, summary, status)
				b.dirty = true
				return true
			}
		}

	case "progress":
		if d, ok := event.Data.(responses.ProgressEvent); ok {
			b.progress = &streamStateProgress{
				Phase:   d.Phase,
				Message: d.Message,
				Current: d.Current,
				Total:   d.Total,
			}
			b.dirty = true
			return true
		}
		if m, ok := event.Data.(map[string]any); ok {
			p := &streamStateProgress{}
			p.Phase, _ = m["phase"].(string)
			p.Message, _ = m["message"].(string)
			if cur, ok := m["current"].(float64); ok {
				p.Current = int(cur)
			}
			if tot, ok := m["total"].(float64); ok {
				p.Total = int(tot)
			}
			b.progress = p
			b.dirty = true
			return true
		}

	case "phase_change":
		if m, ok := event.Data.(map[string]any); ok {
			if phase, _ := m["phase"].(string); phase != "" {
				b.phase = phase
				// When we enter the code-gen phase, the chat-phase streaming text
				// has already been captured in chat_complete, so reset the buffer.
				if phase == "codegen" {
					b.streamingText = ""
					b.thinkingText = ""
				}
				b.dirty = true
				return true
			}
		}

	case "chat_complete":
		// The full summary lives in the milestone event so the frontend can
		// rehydrate the assistant bubble from messages[]. We deliberately keep
		// streamingText/thinkingText in the buffer here: if the client
		// reconnects in the narrow window between chat_complete and the next
		// round's first live event, the stream_state replay still shows the
		// previous content instead of a blank gap. The buffers get cleared on
		// the terminal `done` event or on the next phase_change.
		b.toolCalls = nil
		b.progress = nil
		b.dirty = true
		return true

	case "done":
		// Terminal event — wipe everything so rehydrate is a no-op.
		b.streamingText = ""
		b.thinkingText = ""
		b.conversationText = ""
		b.toolCalls = nil
		b.progress = nil
		b.phase = ""
		b.dirty = true
		return true

	case "clarification_received":
		// User answered — stop replaying the last question on the next reconnect.
		b.conversationText = ""
		b.dirty = true
		return true

	// --- Agent runtime events (2026-04 rewrite) ---

	case "narration":
		// Agent narration streams into streamingText so reconnected clients
		// see the partial assistant reply replayed before live events resume.
		if d, ok := event.Data.(agent.NarrationEvent); ok && d.Delta != "" {
			b.streamingText += d.Delta
			b.dirty = true
			return true
		}
		if m, ok := event.Data.(map[string]any); ok {
			if delta, _ := m["delta"].(string); delta != "" {
				b.streamingText += delta
				b.dirty = true
				return true
			}
		}

	case "tool_call_start":
		// Append a placeholder so an in-flight tool shows up on reconnect.
		var tool, args string
		if d, ok := event.Data.(agent.ToolCallStartEvent); ok {
			tool = d.ToolName
			if len(d.InputPreview) > 0 {
				args = string(d.InputPreview)
			}
		} else if m, ok := event.Data.(map[string]any); ok {
			tool, _ = m["tool_name"].(string)
			if raw, ok := m["input_preview"]; ok && raw != nil {
				if s, ok := raw.(string); ok {
					args = s
				} else if buf, err := json.Marshal(raw); err == nil {
					args = string(buf)
				}
			}
		}
		if tool != "" {
			if args != "" {
				b.toolCalls = append(b.toolCalls, streamStateToolCall{Tool: tool, Arguments: args})
			} else {
				b.toolCalls = append(b.toolCalls, streamStateToolCall{Tool: tool, Pending: true})
			}
			b.dirty = true
			return true
		}

	case "tool_call_result":
		var tool, summary, status string
		var isError bool
		if d, ok := event.Data.(agent.ToolCallResultEvent); ok {
			tool = d.ToolName
			summary = d.Summary
			status = d.Status
			isError = d.IsError
		} else if m, ok := event.Data.(map[string]any); ok {
			tool, _ = m["tool_name"].(string)
			summary, _ = m["summary"].(string)
			status, _ = m["status"].(string)
			isError, _ = m["is_error"].(bool)
		}
		if tool != "" {
			if isError && status == "" {
				status = "error"
			}
			b.attachToolResult(tool, summary, summary, status)
			b.dirty = true
			return true
		}

	case "turn_end":
		// Mid-run turn boundary: no state change, but we accept the event
		// so the dirty flag isn't falsely tripped for a no-op.
		return false

	// Agent error/rollback: terminal for the partial buffer, same as done.
	// NOTE: the agent emits "error" and "rollback" (see internal/agent/events.go).
	// A previous version of this reducer checked "agent_error"/"agent_rollback"
	// and silently never cleared the buffer on failure — reconnects then replayed
	// ghost partial text forever. Keep these names aligned with the emitter.
	case "error", "rollback":
		b.streamingText = ""
		b.thinkingText = ""
		b.toolCalls = nil
		b.progress = nil
		b.phase = ""
		b.dirty = true
		return true
	}

	return false
}

// replaceOrAppendToolCall matches a previous placeholder (from tool_start) with the
// concrete tool_call payload, or appends a fresh entry when no placeholder exists.
func (b *streamStateBuffer) replaceOrAppendToolCall(tool, arguments string) {
	for i := len(b.toolCalls) - 1; i >= 0; i-- {
		if b.toolCalls[i].Tool == tool && b.toolCalls[i].Pending {
			b.toolCalls[i] = streamStateToolCall{Tool: tool, Arguments: arguments}
			return
		}
	}
	b.toolCalls = append(b.toolCalls, streamStateToolCall{Tool: tool, Arguments: arguments})
}

// attachToolResult folds a tool_result event into the most recent matching
// tool entry that doesn't already carry a result. This keeps reconnect state
// consistent with what the client would have seen live.
func (b *streamStateBuffer) attachToolResult(tool, preview, summary, status string) {
	for i := len(b.toolCalls) - 1; i >= 0; i-- {
		if b.toolCalls[i].Tool == tool && b.toolCalls[i].Result == "" {
			b.toolCalls[i].Result = preview
			b.toolCalls[i].Summary = summary
			b.toolCalls[i].Status = status
			return
		}
	}
	// No prior tool_call entry — shouldn't happen, but record it defensively.
	b.toolCalls = append(b.toolCalls, streamStateToolCall{
		Tool:    tool,
		Result:  preview,
		Summary: summary,
		Status:  status,
	})
}

// Snapshot returns the current state as a JSON-serialisable map and clears the
// dirty flag. It returns (nil, false) when nothing has changed since the last
// snapshot, which the caller can use to skip writes.
func (b *streamStateBuffer) Snapshot() (json.RawMessage, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.dirty {
		return nil, false
	}
	b.dirty = false

	payload := map[string]any{
		"streaming_text":    b.streamingText,
		"thinking_text":     b.thinkingText,
		"conversation_text": b.conversationText,
		"tool_calls":        b.toolCalls,
	}
	if b.progress != nil {
		payload["progress"] = b.progress
	}
	if b.phase != "" {
		payload["phase"] = b.phase
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, false
	}
	return raw, true
}

// CurrentJSON always serialises the current state, regardless of the dirty flag.
// Used for the final flush at job finalisation.
func (b *streamStateBuffer) CurrentJSON() json.RawMessage {
	b.mu.Lock()
	defer b.mu.Unlock()

	payload := map[string]any{
		"streaming_text":    b.streamingText,
		"thinking_text":     b.thinkingText,
		"conversation_text": b.conversationText,
		"tool_calls":        b.toolCalls,
	}
	if b.progress != nil {
		payload["progress"] = b.progress
	}
	if b.phase != "" {
		payload["phase"] = b.phase
	}
	raw, _ := json.Marshal(payload)
	return raw
}
