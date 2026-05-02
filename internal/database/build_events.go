package database

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/models"
)

// BuildEventStore is the interface used by webhook handlers + controllers
// for appending and listing build events. Defined here at the database
// boundary so callers can fake it in tests.
type BuildEventStore interface {
	AppendBuildEvent(ctx context.Context, e *models.BuildEvent) error
	ListBuildEventsByTask(ctx context.Context, taskID uuid.UUID) ([]*models.BuildEvent, error)
}

// AppendBuildEvent inserts an event. Duplicate (task_id, message_hash) pairs
// are silently no-op (idempotent re-delivery). Compile-time assertion that
// *Database satisfies BuildEventStore lives in build_events_test.go.
func (db *Database) AppendBuildEvent(ctx context.Context, e *models.BuildEvent) error {
	_, err := db.DB.NewInsert().
		Model(e).
		On("CONFLICT (task_id, message_hash) DO NOTHING").
		Exec(ctx)
	return err
}

// ListBuildEventsByTask returns events for a task ordered by occurred_at ASC.
func (db *Database) ListBuildEventsByTask(ctx context.Context, taskID uuid.UUID) ([]*models.BuildEvent, error) {
	var out []*models.BuildEvent
	err := db.DB.NewSelect().
		Model(&out).
		Where("task_id = ?", taskID).
		Order("occurred_at ASC").
		Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return out, err
}
