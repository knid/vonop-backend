//go:build integration

package controllers

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
)

// TestUpdateAppleConnection_PrefixesNewP8 validates the T3 audit fix:
// passing a bare UUID in req.P8Key results in a P8KeyPath in the DB
// that starts with "p8_keys/", not the bare UUID. This is the regression
// guard for "applyP8Prefix is not wired into UpdateAppleConnection".
func TestUpdateAppleConnection_PrefixesNewP8(t *testing.T) {
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

	conn := &models.AppleConnection{
		ID: uuid.New(), UserID: user.ID, OrganizationID: org.ID,
		Name: "test", KeyID: "K", TeamID: "T", IssuerID: "I",
		P8KeyPath: "p8_keys/old.p8",
	}
	if _, err := db.DB.NewInsert().Model(conn).Exec(ctx); err != nil {
		t.Fatalf("insert conn: %v", err)
	}

	bareUUID := uuid.New().String()
	_, err := c.UpdateAppleConnection(user, conn.ID.String(),
		requests.AppleConnectionUpdateRequest{
			Name: "renamed", KeyID: "K", TeamID: "T", IssuerID: "I",
			P8Key: bareUUID, // bare UUID — must be prefixed by applyP8Prefix
			// APNSP8Key + APNSKeyID intentionally empty so we exercise the bare-UUID
			// path in isolation.
		})
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	var saved models.AppleConnection
	if err := db.DB.NewSelect().Model(&saved).Where("id = ?", conn.ID).Scan(ctx); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !strings.HasPrefix(saved.P8KeyPath, "p8_keys/") {
		t.Errorf("P8KeyPath = %q, want prefix p8_keys/", saved.P8KeyPath)
	}
	if !strings.Contains(saved.P8KeyPath, bareUUID) {
		t.Errorf("P8KeyPath = %q, want to contain bare UUID %q", saved.P8KeyPath, bareUUID)
	}
}
