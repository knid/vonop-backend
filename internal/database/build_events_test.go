package database

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/models"
)

// fakeBuildEventStore is an in-memory implementation of BuildEventStore for testing.
// Mirrors the pattern from internal/agent/context_test.go (mobileConfigGetter fake).
type fakeBuildEventStore struct {
	events []*models.BuildEvent
}

func (s *fakeBuildEventStore) AppendBuildEvent(_ context.Context, e *models.BuildEvent) error {
	// Simulate UNIQUE (task_id, message_hash) — silently no-op on dup
	for _, existing := range s.events {
		if existing.TaskID == e.TaskID && existing.MessageHash == e.MessageHash {
			return nil
		}
	}
	s.events = append(s.events, e)
	return nil
}

func (s *fakeBuildEventStore) ListBuildEventsByTask(_ context.Context, taskID uuid.UUID) ([]*models.BuildEvent, error) {
	var out []*models.BuildEvent
	for _, e := range s.events {
		if e.TaskID == taskID {
			out = append(out, e)
		}
	}
	return out, nil
}

func TestBuildEventStore_AppendAndList(t *testing.T) {
	var store BuildEventStore = &fakeBuildEventStore{}
	ctx := context.Background()
	taskID := uuid.New()

	e1 := &models.BuildEvent{
		TaskID:      taskID,
		EventType:   "preparing",
		OccurredAt:  time.Now().UTC().Add(-3 * time.Second),
		MessageHash: "h1",
	}
	e2 := &models.BuildEvent{
		TaskID:      taskID,
		EventType:   "building",
		OccurredAt:  time.Now().UTC().Add(-2 * time.Second),
		MessageHash: "h2",
	}
	if err := store.AppendBuildEvent(ctx, e1); err != nil {
		t.Fatalf("append e1: %v", err)
	}
	if err := store.AppendBuildEvent(ctx, e2); err != nil {
		t.Fatalf("append e2: %v", err)
	}

	list, err := store.ListBuildEventsByTask(ctx, taskID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 events, got %d", len(list))
	}
}

func TestBuildEventStore_DedupByMessageHash(t *testing.T) {
	var store BuildEventStore = &fakeBuildEventStore{}
	ctx := context.Background()
	taskID := uuid.New()

	e := &models.BuildEvent{
		TaskID:      taskID,
		EventType:   "built",
		OccurredAt:  time.Now().UTC(),
		MessageHash: "duplicate",
	}
	if err := store.AppendBuildEvent(ctx, e); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := store.AppendBuildEvent(ctx, e); err != nil {
		t.Fatalf("dup: %v", err)
	}

	list, _ := store.ListBuildEventsByTask(ctx, taskID)
	if len(list) != 1 {
		t.Errorf("expected 1 event after dedup, got %d", len(list))
	}
}

// TestDatabaseImplementsBuildEventStore is a compile-time check that the
// concrete *Database type satisfies BuildEventStore. If methods are renamed
// or signatures drift, this fails to compile.
func TestDatabaseImplementsBuildEventStore(t *testing.T) {
	var _ BuildEventStore = (*Database)(nil)
}
