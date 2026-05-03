//go:build integration

package controllers

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
)

// TestCodemagicKeystore_RegisterFetchRoundTrip exercises the
// Register → Fetch lifecycle end-to-end: encrypt + persist + decrypt and
// confirm the round-tripped bytes match. AES-256-GCM in
// utils.EncryptBytesEnvelope is exercised through the fake CredStore (the
// AWS KMS network call is faked deterministically).
func TestCodemagicKeystore_RegisterFetchRoundTrip(t *testing.T) {
	db := database.NewTestDB(t,
		(*models.User)(nil),
		(*models.Organization)(nil),
		(*models.AppProject)(nil),
		(*models.AndroidKeystore)(nil),
	)
	c, _ := NewTestController(t, db, TestControllerOptions{})
	ctx := context.Background()

	owner := &models.User{ID: uuid.New(), Email: "owner@example.com", Name: "Owner", Password: "x"}
	if _, err := db.DB.NewInsert().Model(owner).Exec(ctx); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	org := &models.Organization{ID: uuid.New(), Name: "Acme", Slug: "acme-rt-" + uuid.NewString()[:8], OwnerID: owner.ID}
	if _, err := db.DB.NewInsert().Model(org).Exec(ctx); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	project := &models.AppProject{
		ID: uuid.New(), UserID: owner.ID, OrganizationID: org.ID,
		Name: "test", Slug: "test-rt-" + uuid.NewString()[:8], Prompt: "n/a",
		ProjectType: models.ProjectTypeMobileAppNative,
		Status:      "draft",
	}
	if _, err := db.DB.NewInsert().Model(project).Exec(ctx); err != nil {
		t.Fatalf("insert project: %v", err)
	}

	originalKeystore := []byte("PRETEND-THIS-IS-A-JKS-FILE")
	originalProps := []byte("storePassword=x\nkeyAlias=upload\nstoreFile=upload-keystore.jks\n")

	keystoreB64 := base64.StdEncoding.EncodeToString(originalKeystore)
	propsB64 := base64.StdEncoding.EncodeToString(originalProps)

	if err := c.RegisterCodemagicKeystore(ctx, project.ID.String(), keystoreB64, propsB64); err != nil {
		t.Fatalf("register: %v", err)
	}

	var row models.AndroidKeystore
	if err := db.DB.NewSelect().Model(&row).Where("project_id = ?", project.ID).Scan(ctx); err != nil {
		t.Fatalf("scan AndroidKeystore: %v", err)
	}
	if row.KeystoreCredRef == nil || row.PropertiesCredRef == nil {
		t.Fatalf("CredRefs not populated: %+v", row)
	}
	if row.KeystoreCredRef.Kind != "android-keystore" {
		t.Errorf("KeystoreCredRef.Kind = %q, want android-keystore", row.KeystoreCredRef.Kind)
	}
	if row.PropertiesCredRef.Kind != "android-keystore-properties" {
		t.Errorf("PropertiesCredRef.Kind = %q, want android-keystore-properties", row.PropertiesCredRef.Kind)
	}
	if row.OrganizationID != org.ID {
		t.Errorf("OrganizationID mismatch: got %v want %v", row.OrganizationID, org.ID)
	}

	gotKeystoreB64, gotPropsB64, err := c.FetchCodemagicKeystore(ctx, project.ID.String())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	gotKeystore, err := base64.StdEncoding.DecodeString(gotKeystoreB64)
	if err != nil {
		t.Fatalf("decode keystore b64: %v", err)
	}
	gotProps, err := base64.StdEncoding.DecodeString(gotPropsB64)
	if err != nil {
		t.Fatalf("decode props b64: %v", err)
	}
	if !bytes.Equal(gotKeystore, originalKeystore) {
		t.Errorf("keystore bytes round-trip mismatch")
	}
	if !bytes.Equal(gotProps, originalProps) {
		t.Errorf("properties bytes round-trip mismatch")
	}
}

// TestCodemagicKeystore_FetchOnUnregisteredProjectReturnsErrNotRegistered
// asserts the sentinel-error contract that the HTTP view uses to map 404 vs
// 500. The project exists but no AndroidKeystore row has ever been
// registered → the controller must return ErrKeystoreNotRegistered.
func TestCodemagicKeystore_FetchOnUnregisteredProjectReturnsErrNotRegistered(t *testing.T) {
	db := database.NewTestDB(t,
		(*models.User)(nil),
		(*models.Organization)(nil),
		(*models.AppProject)(nil),
		(*models.AndroidKeystore)(nil),
	)
	c, _ := NewTestController(t, db, TestControllerOptions{})
	ctx := context.Background()

	owner := &models.User{ID: uuid.New(), Email: "u@example.com", Name: "U", Password: "x"}
	if _, err := db.DB.NewInsert().Model(owner).Exec(ctx); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	org := &models.Organization{ID: uuid.New(), Name: "A", Slug: "a-nr-" + uuid.NewString()[:8], OwnerID: owner.ID}
	if _, err := db.DB.NewInsert().Model(org).Exec(ctx); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	project := &models.AppProject{
		ID: uuid.New(), UserID: owner.ID, OrganizationID: org.ID,
		Name: "x", Slug: "x-nr-" + uuid.NewString()[:8], Prompt: "n/a",
		ProjectType: models.ProjectTypeMobileAppNative, Status: "draft",
	}
	if _, err := db.DB.NewInsert().Model(project).Exec(ctx); err != nil {
		t.Fatalf("insert project: %v", err)
	}

	_, _, err := c.FetchCodemagicKeystore(ctx, project.ID.String())
	if err == nil {
		t.Fatal("expected error for unregistered project")
	}
	if !errors.Is(err, ErrKeystoreNotRegistered) {
		t.Errorf("expected ErrKeystoreNotRegistered, got: %v", err)
	}
}

// TestCodemagicKeystore_RegisterIsIdempotentOnConflict verifies the
// ON CONFLICT (project_id) DO UPDATE upsert: registering twice for the
// same project leaves a single row, and the second blob's bytes win on
// fetch (i.e. the second Register overwrites the CredRefs).
func TestCodemagicKeystore_RegisterIsIdempotentOnConflict(t *testing.T) {
	db := database.NewTestDB(t,
		(*models.User)(nil),
		(*models.Organization)(nil),
		(*models.AppProject)(nil),
		(*models.AndroidKeystore)(nil),
	)
	c, _ := NewTestController(t, db, TestControllerOptions{})
	ctx := context.Background()

	owner := &models.User{ID: uuid.New(), Email: "u@example.com", Name: "U", Password: "x"}
	if _, err := db.DB.NewInsert().Model(owner).Exec(ctx); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	org := &models.Organization{ID: uuid.New(), Name: "A", Slug: "a-id-" + uuid.NewString()[:8], OwnerID: owner.ID}
	if _, err := db.DB.NewInsert().Model(org).Exec(ctx); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	project := &models.AppProject{
		ID: uuid.New(), UserID: owner.ID, OrganizationID: org.ID,
		Name: "x", Slug: "x-id-" + uuid.NewString()[:8], Prompt: "n/a",
		ProjectType: models.ProjectTypeMobileAppNative, Status: "draft",
	}
	if _, err := db.DB.NewInsert().Model(project).Exec(ctx); err != nil {
		t.Fatalf("insert project: %v", err)
	}

	first := base64.StdEncoding.EncodeToString([]byte("first-keystore"))
	firstProps := base64.StdEncoding.EncodeToString([]byte("first-props"))
	if err := c.RegisterCodemagicKeystore(ctx, project.ID.String(), first, firstProps); err != nil {
		t.Fatalf("first register: %v", err)
	}

	second := base64.StdEncoding.EncodeToString([]byte("second-keystore"))
	secondProps := base64.StdEncoding.EncodeToString([]byte("second-props"))
	if err := c.RegisterCodemagicKeystore(ctx, project.ID.String(), second, secondProps); err != nil {
		t.Fatalf("second register: %v", err)
	}

	count, err := db.DB.NewSelect().Model((*models.AndroidKeystore)(nil)).
		Where("project_id = ?", project.ID).Count(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 row after re-register, got %d", count)
	}

	gotKeystoreB64, _, err := c.FetchCodemagicKeystore(ctx, project.ID.String())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	gotKeystore, _ := base64.StdEncoding.DecodeString(gotKeystoreB64)
	if !bytes.Equal(gotKeystore, []byte("second-keystore")) {
		t.Errorf("expected second-keystore bytes, got: %s", gotKeystore)
	}
}
