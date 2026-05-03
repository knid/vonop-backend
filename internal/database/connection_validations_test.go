package database

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/models"
)

// fakeConnectionValidationStore is an in-memory ConnectionValidationStore for tests.
type fakeConnectionValidationStore struct {
	rows []*models.ConnectionValidation
}

func (s *fakeConnectionValidationStore) AppendConnectionValidation(_ context.Context, v *models.ConnectionValidation) error {
	s.rows = append(s.rows, v)
	return nil
}

func (s *fakeConnectionValidationStore) LatestConnectionValidation(_ context.Context, kind string, id uuid.UUID) (*models.ConnectionValidation, error) {
	var latest *models.ConnectionValidation
	for _, v := range s.rows {
		if v.ConnectionKind != kind || v.ConnectionID != id {
			continue
		}
		if latest == nil || v.ValidatedAt.After(latest.ValidatedAt) {
			latest = v
		}
	}
	return latest, nil
}

func (s *fakeConnectionValidationStore) ListConnectionValidations(_ context.Context, kind string) ([]*models.ConnectionValidation, error) {
	var out []*models.ConnectionValidation
	for _, v := range s.rows {
		if v.ConnectionKind == kind {
			out = append(out, v)
		}
	}
	return out, nil
}

func TestConnectionValidationStore_AppendAndLatest(t *testing.T) {
	var store ConnectionValidationStore = &fakeConnectionValidationStore{}
	ctx := context.Background()
	connID := uuid.New()

	if err := store.AppendConnectionValidation(ctx, &models.ConnectionValidation{
		ConnectionKind: "apple",
		ConnectionID:   connID,
		Status:         "valid",
		Message:        "ok",
		AppCount:       12,
		ValidatedAt:    time.Now().UTC().Add(-1 * time.Hour),
	}); err != nil {
		t.Fatalf("first append: %v", err)
	}
	if err := store.AppendConnectionValidation(ctx, &models.ConnectionValidation{
		ConnectionKind: "apple",
		ConnectionID:   connID,
		Status:         "invalid",
		Message:        "key revoked",
		ValidatedAt:    time.Now().UTC(),
	}); err != nil {
		t.Fatalf("second append: %v", err)
	}

	latest, err := store.LatestConnectionValidation(ctx, "apple", connID)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if latest == nil {
		t.Fatal("expected latest, got nil")
	}
	if latest.Status != "invalid" {
		t.Errorf("latest status: got %q want invalid", latest.Status)
	}
}

func TestConnectionValidationStore_LatestReturnsNilWhenNone(t *testing.T) {
	var store ConnectionValidationStore = &fakeConnectionValidationStore{}
	latest, err := store.LatestConnectionValidation(context.Background(), "apple", uuid.New())
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if latest != nil {
		t.Errorf("expected nil for missing connection, got %+v", latest)
	}
}

func TestDatabaseImplementsConnectionValidationStore(t *testing.T) {
	var _ ConnectionValidationStore = (*Database)(nil)
}
