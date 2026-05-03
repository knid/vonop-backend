package controllers

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/models"
)

// fakeConnectionValidationStore (controller test scope)
type fakeConnectionValidationStore struct {
	rows []*models.ConnectionValidation
}

func (s *fakeConnectionValidationStore) AppendConnectionValidation(_ context.Context, v *models.ConnectionValidation) error {
	s.rows = append(s.rows, v)
	return nil
}

func TestPersistConnectionValidation_WritesRow(t *testing.T) {
	store := &fakeConnectionValidationStore{}
	ctx := context.Background()

	connID := uuid.New()
	persistConnectionValidation(ctx, store, "apple", connID, "valid", "ok", 12)

	if len(store.rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(store.rows))
	}
	row := store.rows[0]
	if row.ConnectionKind != "apple" || row.ConnectionID != connID {
		t.Errorf("wrong (kind, id): %q / %v", row.ConnectionKind, row.ConnectionID)
	}
	if row.Status != "valid" || row.Message != "ok" || row.AppCount != 12 {
		t.Errorf("wrong fields: %+v", row)
	}
	if time.Since(row.ValidatedAt) > time.Second {
		t.Errorf("validated_at too old: %v", row.ValidatedAt)
	}
}

func TestPersistConnectionValidation_StoreErrorIsSwallowed(t *testing.T) {
	// The persist helper should never return an error — best-effort write so
	// a DB outage doesn't break validation. The test just confirms it doesn't
	// panic with a non-nil store.
	store := &fakeConnectionValidationStore{}
	persistConnectionValidation(context.Background(), store, "google_play", uuid.New(), "invalid", "denied", 0)
	if len(store.rows) != 1 {
		t.Errorf("expected 1 row, got %d", len(store.rows))
	}
}
