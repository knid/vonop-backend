package events

import (
	"log/slog"
	"sync"

	"github.com/knid/wemob/internal/models"
)

// MulticastService fans an event out to N adapters in parallel.
// Each adapter is responsible for its own retries and error logging;
// MulticastService never propagates errors and never blocks the caller
// beyond goroutine spawn.
type MulticastService struct {
	adapters []EventService
}

func NewMulticastService(adapters ...EventService) *MulticastService {
	return &MulticastService{adapters: adapters}
}

func (m *MulticastService) SendEvent(user *models.User, event string, attributes map[string]any) error {
	if user == nil {
		slog.Warn("events.Multicast: nil user", "event", event)
		return nil
	}

	var wg sync.WaitGroup
	for _, a := range m.adapters {
		a := a
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					slog.Error("events.Multicast: adapter panicked",
						"event", event, "user_id", user.ID.String(), "panic", r)
				}
			}()
			_ = a.SendEvent(user, event, attributes)
		}()
	}
	wg.Wait()
	return nil
}
