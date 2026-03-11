package controllers

import (
	"sync"

	"github.com/knid/wemob/internal/responses"
)

// EventBus fans out SSE events to multiple subscribers (e.g. the original SSE
// connection and any reconnected clients). Thread-safe.
type EventBus struct {
	mu          sync.RWMutex
	subscribers map[int]chan responses.SSEEvent
	nextID      int
	closed      bool
}

// NewEventBus creates a new event bus.
func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make(map[int]chan responses.SSEEvent),
	}
}

// Subscribe returns a buffered channel that receives all future events.
// Call Unsubscribe with the returned ID when done.
func (eb *EventBus) Subscribe() (int, <-chan responses.SSEEvent) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if eb.closed {
		ch := make(chan responses.SSEEvent)
		close(ch)
		return -1, ch
	}

	id := eb.nextID
	eb.nextID++
	ch := make(chan responses.SSEEvent, 100)
	eb.subscribers[id] = ch
	return id, ch
}

// Unsubscribe removes a subscriber. Safe to call multiple times.
func (eb *EventBus) Unsubscribe(id int) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if ch, ok := eb.subscribers[id]; ok {
		close(ch)
		delete(eb.subscribers, id)
	}
}

// Publish sends an event to all subscribers. Non-blocking — slow subscribers
// will miss events (their channel buffer fills up).
func (eb *EventBus) Publish(event responses.SSEEvent) {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	for _, ch := range eb.subscribers {
		select {
		case ch <- event:
		default:
			// Subscriber too slow — drop the event
		}
	}
}

// Close closes all subscriber channels and marks the bus as closed.
func (eb *EventBus) Close() {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if eb.closed {
		return
	}
	eb.closed = true
	for id, ch := range eb.subscribers {
		close(ch)
		delete(eb.subscribers, id)
	}
}
