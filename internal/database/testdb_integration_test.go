//go:build integration

package database

import (
	"context"
	"testing"

	"github.com/knid/wemob/internal/models"
)

// TestNewTestDB_CreatesTablesAndAcceptsInserts is a smoke test that verifies
// the NewTestDB helper actually starts a Postgres container, creates the
// requested model table, and round-trips an insert/select.
func TestNewTestDB_CreatesTablesAndAcceptsInserts(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: skipping in short mode")
	}

	db := NewTestDB(t, (*models.User)(nil))

	ctx := context.Background()

	want := &models.User{
		Name:     "Smoke Test",
		Email:    "smoke-test@wemob.test",
		Password: "x",
	}
	if _, err := db.DB.NewInsert().Model(want).Exec(ctx); err != nil {
		t.Fatalf("insert: %v", err)
	}

	got := new(models.User)
	if err := db.DB.NewSelect().Model(got).Where("email = ?", want.Email).Scan(ctx); err != nil {
		t.Fatalf("select: %v", err)
	}
	if got.Name != want.Name {
		t.Errorf("Name = %q, want %q", got.Name, want.Name)
	}
}
