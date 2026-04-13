package views

import (
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
	mu     sync.Mutex
	closed bool
}

func newAgentSSEAdapter(ch chan<- responses.SSEEvent) *agentSSEAdapter {
	return &agentSSEAdapter{ch: ch}
}

// Emit implements agent.EventEmitter.Emit — non-blocking 10s send with
// closed-channel protection.
func (a *agentSSEAdapter) Emit(eventType string, data any) bool {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return false
	}
	a.mu.Unlock()

	select {
	case a.ch <- responses.SSEEvent{Event: eventType, Data: data}:
		return true
	case <-time.After(10 * time.Second):
		return false
	}
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
