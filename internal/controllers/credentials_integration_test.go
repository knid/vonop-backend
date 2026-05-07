//go:build integration

package controllers

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
)

// TestUpdateAppleConnection_NoP8KeepsExistingRef verifies that an update with
// an empty P8Key preserves the existing CredRef (i.e. Bun's whole-row UPDATE
// doesn't null it out). The post-2026-05-07 cleanup made CredRef the sole
// credential storage, so this is the regression guard for "nullzero on
// whole-row update wipes the encrypted blob pointer".
func TestUpdateAppleConnection_NoP8KeepsExistingRef(t *testing.T) {
	db := database.NewTestDB(t,
		(*models.User)(nil),
		(*models.Organization)(nil),
		(*models.AppleConnection)(nil),
	)
	c, _ := NewTestController(t, db, TestControllerOptions{})
	ctx := context.Background()

	user := &models.User{
		ID: uuid.New(), Name: "U", Email: "u@example.com", Password: "x",
	}
	if _, err := db.DB.NewInsert().Model(user).Exec(ctx); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	org := &models.Organization{
		ID: uuid.New(), Name: "Acme", Slug: "acme-" + uuid.NewString()[:8], OwnerID: user.ID,
	}
	if _, err := db.DB.NewInsert().Model(org).Exec(ctx); err != nil {
		t.Fatalf("insert org: %v", err)
	}

	// Seed an encrypted .p8 via CredStore so the connection row has a real ref
	// to preserve across the no-op update.
	ref, err := c.CredStore.Put(ctx, org.ID, "apple-p8", []byte("FAKE-P8"))
	if err != nil {
		t.Fatalf("seed cred: %v", err)
	}

	conn := &models.AppleConnection{
		ID: uuid.New(), UserID: user.ID, OrganizationID: org.ID,
		Name: "test", KeyID: "K", TeamID: "T", IssuerID: "I",
		P8KeyCredRef: credRefFromStorage(ref),
	}
	if _, err := db.DB.NewInsert().Model(conn).Exec(ctx); err != nil {
		t.Fatalf("insert conn: %v", err)
	}

	_, err = c.UpdateAppleConnection(user, conn.ID.String(),
		requests.AppleConnectionUpdateRequest{
			Name: "renamed", KeyID: "K", TeamID: "T", IssuerID: "I",
			// P8Key intentionally empty — keeps existing ref.
		})
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	var saved models.AppleConnection
	if err := db.DB.NewSelect().Model(&saved).Where("id = ?", conn.ID).Scan(ctx); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if saved.P8KeyCredRef == nil {
		t.Fatal("P8KeyCredRef nil after update — whole-row UPDATE clobbered the encrypted ref")
	}
	if saved.P8KeyCredRef.BlobS3Key != ref.BlobS3Key {
		t.Errorf("P8KeyCredRef changed: got %q want %q", saved.P8KeyCredRef.BlobS3Key, ref.BlobS3Key)
	}
	if saved.Name != "renamed" {
		t.Errorf("Name not updated: got %q want renamed", saved.Name)
	}
}
