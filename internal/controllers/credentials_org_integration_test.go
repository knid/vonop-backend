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

// setupOrgCredTest creates the shared fixture used by every org-credential
// test: an owner user (already a member), a non-member "other" user, and an
// organization owned by the owner.
func setupOrgCredTest(t *testing.T) (*Controller, *database.Database, *models.User, *models.User, *models.Organization) {
	t.Helper()
	db := database.NewTestDB(t,
		(*models.User)(nil),
		(*models.Organization)(nil),
		(*models.OrganizationMember)(nil),
		(*models.AppleConnection)(nil),
		(*models.GooglePlayConnection)(nil),
	)
	c, _ := NewTestController(t, db, TestControllerOptions{})
	ctx := context.Background()

	owner := &models.User{
		ID: uuid.New(), Name: "Owner", Email: "owner@example.com", Password: "x",
	}
	other := &models.User{
		ID: uuid.New(), Name: "Other", Email: "other@example.com", Password: "x",
	}
	if _, err := db.DB.NewInsert().Model(owner).Exec(ctx); err != nil {
		t.Fatalf("insert owner: %v", err)
	}
	if _, err := db.DB.NewInsert().Model(other).Exec(ctx); err != nil {
		t.Fatalf("insert other: %v", err)
	}

	org := &models.Organization{
		ID:      uuid.New(),
		Name:    "Acme",
		Slug:    "acme-" + uuid.NewString()[:8],
		OwnerID: owner.ID,
	}
	if _, err := db.DB.NewInsert().Model(org).Exec(ctx); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	member := &models.OrganizationMember{
		OrganizationID: org.ID,
		UserID:         owner.ID,
		Role:           "owner",
	}
	if _, err := db.DB.NewInsert().Model(member).Exec(ctx); err != nil {
		t.Fatalf("insert owner member: %v", err)
	}
	return c, db, owner, other, org
}

func TestGetAppleConnectionForOrg_RejectsNonMember(t *testing.T) {
	c, db, owner, other, org := setupOrgCredTest(t)
	ctx := context.Background()

	conn := &models.AppleConnection{
		ID:             uuid.New(),
		UserID:         owner.ID,
		OrganizationID: org.ID,
		Name:           "test",
		KeyID:          "K", TeamID: "T", IssuerID: "I",
		P8KeyPath: "p8_keys/x.p8",
	}
	if _, err := db.DB.NewInsert().Model(conn).Exec(ctx); err != nil {
		t.Fatalf("insert conn: %v", err)
	}

	_, err := c.GetAppleConnectionForOrg(ctx, other, org.ID.String(), conn.ID.String())
	if err == nil {
		t.Fatal("expected error when non-member tries to read org credential")
	}
	// RequireOrgMembership returns "organization not found or access denied".
	if !strings.Contains(err.Error(), "organization not found or access denied") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestGetAppleConnectionForOrg_AllowsOrgMember(t *testing.T) {
	c, db, owner, _, org := setupOrgCredTest(t)
	ctx := context.Background()

	conn := &models.AppleConnection{
		ID: uuid.New(), UserID: owner.ID, OrganizationID: org.ID,
		Name: "test", KeyID: "K", TeamID: "T", IssuerID: "I",
		P8KeyPath: "p8_keys/x.p8",
	}
	if _, err := db.DB.NewInsert().Model(conn).Exec(ctx); err != nil {
		t.Fatalf("insert conn: %v", err)
	}

	got, err := c.GetAppleConnectionForOrg(ctx, owner, org.ID.String(), conn.ID.String())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != conn.ID {
		t.Errorf("got conn ID %v want %v", got.ID, conn.ID)
	}
}

// TestUpdateAppleConnectionForOrg_PreservesOriginalUserID is the
// security-critical test: when a non-creator member of the org updates the
// connection, Bun's whole-row UPDATE must NOT overwrite UserID with the
// requesting user. P1.0 review caught a similar class of bug.
func TestUpdateAppleConnectionForOrg_PreservesOriginalUserID(t *testing.T) {
	c, db, owner, _, org := setupOrgCredTest(t)
	ctx := context.Background()

	editor := &models.User{
		ID: uuid.New(), Name: "Editor", Email: "editor@example.com", Password: "x",
	}
	if _, err := db.DB.NewInsert().Model(editor).Exec(ctx); err != nil {
		t.Fatalf("insert editor: %v", err)
	}
	if _, err := db.DB.NewInsert().Model(&models.OrganizationMember{
		OrganizationID: org.ID, UserID: editor.ID, Role: "member",
	}).Exec(ctx); err != nil {
		t.Fatalf("insert editor member: %v", err)
	}

	conn := &models.AppleConnection{
		ID: uuid.New(), UserID: owner.ID, OrganizationID: org.ID,
		Name: "original", KeyID: "K", TeamID: "T", IssuerID: "I",
		P8KeyPath: "p8_keys/x.p8",
	}
	if _, err := db.DB.NewInsert().Model(conn).Exec(ctx); err != nil {
		t.Fatalf("insert conn: %v", err)
	}

	_, err := c.UpdateAppleConnectionForOrg(ctx, editor, org.ID.String(), conn.ID.String(),
		requests.AppleConnectionUpdateRequest{
			Name: "renamed", KeyID: "K2", TeamID: "T2", IssuerID: "I2",
			// P8Key/APNSP8Key/APNSKeyID intentionally empty so we don't trigger the
			// dual-write CredStore path that would need additional setup.
		})
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	var saved models.AppleConnection
	if err := db.DB.NewSelect().Model(&saved).Where("id = ?", conn.ID).Scan(ctx); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if saved.UserID != owner.ID {
		t.Errorf("UserID changed: got %v want %v (creator must be preserved)", saved.UserID, owner.ID)
	}
	if saved.OrganizationID != org.ID {
		t.Errorf("OrganizationID changed: got %v want %v", saved.OrganizationID, org.ID)
	}
	if saved.Name != "renamed" {
		t.Errorf("Name not updated: got %q want renamed", saved.Name)
	}
}

// ---- Google Play mirror ----

func TestGetGooglePlayConnectionForOrg_RejectsNonMember(t *testing.T) {
	c, db, owner, other, org := setupOrgCredTest(t)
	ctx := context.Background()
	conn := &models.GooglePlayConnection{
		ID: uuid.New(), UserID: owner.ID, OrganizationID: org.ID,
		Name: "test", ServiceAccountPath: "service_accounts/x.json",
	}
	if _, err := db.DB.NewInsert().Model(conn).Exec(ctx); err != nil {
		t.Fatalf("insert: %v", err)
	}
	_, err := c.GetGooglePlayConnectionForOrg(ctx, other, org.ID.String(), conn.ID.String())
	if err == nil {
		t.Fatal("expected error for non-member")
	}
	if !strings.Contains(err.Error(), "organization not found or access denied") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestGetGooglePlayConnectionForOrg_AllowsOrgMember(t *testing.T) {
	c, db, owner, _, org := setupOrgCredTest(t)
	ctx := context.Background()
	conn := &models.GooglePlayConnection{
		ID: uuid.New(), UserID: owner.ID, OrganizationID: org.ID,
		Name: "test", ServiceAccountPath: "service_accounts/x.json",
	}
	if _, err := db.DB.NewInsert().Model(conn).Exec(ctx); err != nil {
		t.Fatalf("insert: %v", err)
	}
	got, err := c.GetGooglePlayConnectionForOrg(ctx, owner, org.ID.String(), conn.ID.String())
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != conn.ID {
		t.Errorf("got %v want %v", got.ID, conn.ID)
	}
}

func TestUpdateGooglePlayConnectionForOrg_PreservesOriginalUserID(t *testing.T) {
	c, db, owner, _, org := setupOrgCredTest(t)
	ctx := context.Background()

	editor := &models.User{
		ID: uuid.New(), Name: "Editor", Email: "editor@example.com", Password: "x",
	}
	if _, err := db.DB.NewInsert().Model(editor).Exec(ctx); err != nil {
		t.Fatalf("insert editor: %v", err)
	}
	if _, err := db.DB.NewInsert().Model(&models.OrganizationMember{
		OrganizationID: org.ID, UserID: editor.ID, Role: "member",
	}).Exec(ctx); err != nil {
		t.Fatalf("insert editor member: %v", err)
	}

	conn := &models.GooglePlayConnection{
		ID: uuid.New(), UserID: owner.ID, OrganizationID: org.ID,
		Name: "original", ServiceAccountPath: "service_accounts/x.json",
	}
	if _, err := db.DB.NewInsert().Model(conn).Exec(ctx); err != nil {
		t.Fatalf("insert: %v", err)
	}

	_, err := c.UpdateGooglePlayConnectionForOrg(ctx, editor, org.ID.String(), conn.ID.String(),
		requests.GooglePlayConnectionUpdateRequest{Name: "renamed"})
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	var saved models.GooglePlayConnection
	if err := db.DB.NewSelect().Model(&saved).Where("id = ?", conn.ID).Scan(ctx); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if saved.UserID != owner.ID {
		t.Errorf("UserID changed: got %v want %v", saved.UserID, owner.ID)
	}
	if saved.OrganizationID != org.ID {
		t.Errorf("OrganizationID changed: got %v want %v", saved.OrganizationID, org.ID)
	}
	if saved.Name != "renamed" {
		t.Errorf("Name not updated: got %q want renamed", saved.Name)
	}
}
