package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
	"github.com/uptrace/bun"
)

// InsertWebhookEventTx attempts to insert a webhook_events dedup row inside the
// caller's transaction. Returns (true, nil) if the row was actually inserted,
// or (false, nil) if a row with the same event_id already existed (Stripe retry).
// The atomic dedup-vs-side-effect guarantee in HandleWebhook depends on this
// running in the same tx as the subsequent money-touching writes — without
// that, a process crash between insert and downstream work lets Stripe retry
// re-run the side-effects.
func (db *Database) InsertWebhookEventTx(tx bun.IDB, eventID, eventType string) (bool, error) {
	event := &models.WebhookEvent{
		EventID:   eventID,
		EventType: eventType,
	}
	res, err := tx.NewInsert().
		Model(event).
		On("CONFLICT (event_id) DO NOTHING").
		Exec(context.Background())
	if err != nil {
		return false, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}
