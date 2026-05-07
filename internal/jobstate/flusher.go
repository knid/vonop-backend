package jobstate

import (
	"encoding/json"
	"sync"
)

// AsyncStateFlusher decouples the stream_state DB write from the SSE event
// loop. Previously the view goroutine called UpdateGenerationJobStreamState
// synchronously on every milestone and every 500ms ticker tick — a single
// slow DB write would block the event loop, fill the event channel, and
// trigger the agent emitter's 10s-timeout silent drop path.
//
// Now Trigger() is non-blocking:
//   - If no write is in flight, it spawns a goroutine that snapshots the
//     buffer and writes. After the write, it re-snapshots; if more events
//     landed during the write, it writes again. This drains the tail in one
//     goroutine without creating a goroutine per event.
//   - If a write is already in flight, it returns immediately. The running
//     goroutine's inner loop will pick up any new dirty state on its next
//     iteration.
//
// There's a narrow race where Apply() can dirty the buffer between the
// goroutine's last snapshot-returns-clean and its `running=false` assignment.
// The 500ms safety-net ticker in the main event loop closes that race — the
// next tick always schedules another Trigger().
//
// Wait() blocks until any in-flight goroutine completes. Call it at job
// finalisation, before the authoritative `{}` clear write, to avoid a race
// between the final clear and a background flush.
type AsyncStateFlusher struct {
	snapshot func() (json.RawMessage, bool)
	write    func(json.RawMessage)

	mu      sync.Mutex
	running bool
	wg      sync.WaitGroup
}

func NewAsyncStateFlusher(
	snapshot func() (json.RawMessage, bool),
	write func(json.RawMessage),
) *AsyncStateFlusher {
	return &AsyncStateFlusher{snapshot: snapshot, write: write}
}

// Trigger schedules a flush. Returns immediately.
func (f *AsyncStateFlusher) Trigger() {
	f.mu.Lock()
	if f.running {
		f.mu.Unlock()
		return
	}
	f.running = true
	f.wg.Add(1)
	f.mu.Unlock()

	go func() {
		defer func() {
			f.mu.Lock()
			f.running = false
			f.mu.Unlock()
			f.wg.Done()
		}()
		for {
			raw, ok := f.snapshot()
			if !ok {
				return
			}
			f.write(raw)
		}
	}()
}

// Wait blocks until any in-flight flush goroutine completes.
func (f *AsyncStateFlusher) Wait() {
	f.wg.Wait()
}
