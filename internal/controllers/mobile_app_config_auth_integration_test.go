//go:build integration

package controllers

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
)

// TestMobileAppConfig_AuthFieldsRoundTrip verifies that the new auth_*
// columns persist correctly through UpsertMobileAppConfig + GetMobileAppConfig.
// Catches regressions where a future ALTER batch drops a column or the
// upsert SQL forgets a Set() call (the Bun whole-row footgun pattern from
// memory: feedback_bun_whole_row_update.md).
func TestMobileAppConfig_AuthFieldsRoundTrip(t *testing.T) {
	db := database.NewTestDB(t,
		(*models.User)(nil),
		(*models.Organization)(nil),
		(*models.AppProject)(nil),
		(*models.MobileAppConfig)(nil),
	)

	ctx := context.Background()
	user := &models.User{
		ID:    uuid.New(),
		Name:  "Owner",
		Email: "owner-" + uuid.NewString()[:8] + "@example.com",
		Password: "x",
	}
	if _, err := db.DB.NewInsert().Model(user).Exec(ctx); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	org := &models.Organization{
		ID: uuid.New(), Name: "Acme", Slug: "acme-auth-" + uuid.NewString()[:8],
		OwnerID: user.ID, IsPersonal: true,
	}
	if _, err := db.DB.NewInsert().Model(org).Exec(ctx); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	project := &models.AppProject{
		ID: uuid.New(), UserID: user.ID, OrganizationID: org.ID,
		Name: "Auth Test", Slug: "auth-test-" + uuid.NewString()[:8], Prompt: "x",
		ProjectType: models.ProjectTypeMobileAppNative,
	}
	if _, err := db.DB.NewInsert().Model(project).Exec(ctx); err != nil {
		t.Fatalf("insert project: %v", err)
	}

	cfg := &models.MobileAppConfig{
		ProjectID:     project.ID,
		AuthEnabled:   true,
		AuthProviders: []string{"email"},
		AuthScheme:    "auth-test",
	}
	if _, err := db.UpsertMobileAppConfig(cfg); err != nil {
		t.Fatalf("upsert config: %v", err)
	}

	got, err := db.GetMobileAppConfig(project.ID.String())
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	if !got.AuthEnabled {
		t.Errorf("AuthEnabled: got false, want true")
	}
	if got.AuthScheme != "auth-test" {
		t.Errorf("AuthScheme: got %q, want %q", got.AuthScheme, "auth-test")
	}
	if len(got.AuthProviders) != 1 || got.AuthProviders[0] != "email" {
		t.Errorf("AuthProviders: got %v, want [email]", got.AuthProviders)
	}

	// Toggle the flag off and confirm the upsert clears it.
	cfg.AuthEnabled = false
	cfg.AuthProviders = []string{}
	cfg.AuthScheme = ""
	if _, err := db.UpsertMobileAppConfig(cfg); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	got2, err := db.GetMobileAppConfig(project.ID.String())
	if err != nil {
		t.Fatalf("re-get: %v", err)
	}
	if got2.AuthEnabled {
		t.Errorf("AuthEnabled: got true after toggle off, want false")
	}
	if got2.AuthScheme != "" {
		t.Errorf("AuthScheme: got %q after clear, want empty", got2.AuthScheme)
	}
}

// TestEnsureBetterAuthSecret_GeneratesAndRoundTrips verifies the full
// CredStore round-trip: lazy-generate on first call, persist the ref,
// reload from DB, decrypt, return the same secret on subsequent calls.
func TestEnsureBetterAuthSecret_GeneratesAndRoundTrips(t *testing.T) {
	db := database.NewTestDB(t,
		(*models.User)(nil),
		(*models.Organization)(nil),
		(*models.AppProject)(nil),
		(*models.MobileAppConfig)(nil),
	)
	c, _ := NewTestController(t, db, TestControllerOptions{})

	ctx := context.Background()
	user := &models.User{
		ID:    uuid.New(),
		Name:  "Owner",
		Email: "owner-" + uuid.NewString()[:8] + "@example.com",
		Password: "x",
	}
	if _, err := db.DB.NewInsert().Model(user).Exec(ctx); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	org := &models.Organization{
		ID: uuid.New(), Name: "Acme", Slug: "acme-secret-" + uuid.NewString()[:8],
		OwnerID: user.ID, IsPersonal: true,
	}
	if _, err := db.DB.NewInsert().Model(org).Exec(ctx); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	project := &models.AppProject{
		ID: uuid.New(), UserID: user.ID, OrganizationID: org.ID,
		Name: "Secret Test", Slug: "secret-test-" + uuid.NewString()[:8], Prompt: "x",
		ProjectType: models.ProjectTypeMobileAppNative,
	}
	if _, err := db.DB.NewInsert().Model(project).Exec(ctx); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	cfg := &models.MobileAppConfig{
		ProjectID:     project.ID,
		AuthEnabled:   true,
		AuthProviders: []string{"email"},
		AuthScheme:    project.Slug,
	}
	if _, err := db.UpsertMobileAppConfig(cfg); err != nil {
		t.Fatalf("upsert config: %v", err)
	}

	secret1, err := c.EnsureBetterAuthSecret(ctx, project)
	if err != nil {
		t.Fatalf("first EnsureBetterAuthSecret: %v", err)
	}
	if len(secret1) != 64 { // 32 bytes hex-encoded
		t.Errorf("secret length: got %d, want 64", len(secret1))
	}
	if project.BetterAuthSecretCredRef == nil {
		t.Errorf("BetterAuthSecretCredRef not set on project struct")
	}

	// Reload project from DB to verify the ref persisted.
	reloaded, err := db.GetAppProjectByID(project.ID.String())
	if err != nil {
		t.Fatalf("reload project: %v", err)
	}
	if reloaded.BetterAuthSecretCredRef == nil {
		t.Fatalf("BetterAuthSecretCredRef did not persist to DB")
	}

	// Idempotency: calling again returns the same secret.
	secret2, err := c.EnsureBetterAuthSecret(ctx, reloaded)
	if err != nil {
		t.Fatalf("second EnsureBetterAuthSecret: %v", err)
	}
	if secret2 != secret1 {
		t.Errorf("secret not idempotent: first=%q second=%q", secret1, secret2)
	}

	// CollectBetterAuthEnvVars exposes both expected keys.
	envs, err := c.CollectBetterAuthEnvVars(ctx, reloaded)
	if err != nil {
		t.Fatalf("CollectBetterAuthEnvVars: %v", err)
	}
	if envs["BETTER_AUTH_SECRET"] != secret1 {
		t.Errorf("BETTER_AUTH_SECRET in env map mismatch")
	}
	if envs["BETTER_AUTH_URL"] == "" {
		t.Errorf("BETTER_AUTH_URL empty")
	}
}

// TestCollectBetterAuthEnvVars_DisabledReturnsEmpty verifies the deploy
// worker can call CollectBetterAuthEnvVars unconditionally — disabled-auth
// projects get an empty map, not an error.
func TestCollectBetterAuthEnvVars_DisabledReturnsEmpty(t *testing.T) {
	db := database.NewTestDB(t,
		(*models.User)(nil),
		(*models.Organization)(nil),
		(*models.AppProject)(nil),
		(*models.MobileAppConfig)(nil),
	)
	c, _ := NewTestController(t, db, TestControllerOptions{})

	ctx := context.Background()
	user := &models.User{
		ID:    uuid.New(),
		Name:  "Owner",
		Email: "owner-" + uuid.NewString()[:8] + "@example.com",
		Password: "x",
	}
	if _, err := db.DB.NewInsert().Model(user).Exec(ctx); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	org := &models.Organization{
		ID: uuid.New(), Name: "Acme", Slug: "acme-off-" + uuid.NewString()[:8],
		OwnerID: user.ID, IsPersonal: true,
	}
	if _, err := db.DB.NewInsert().Model(org).Exec(ctx); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	project := &models.AppProject{
		ID: uuid.New(), UserID: user.ID, OrganizationID: org.ID,
		Slug: "off-test-" + uuid.NewString()[:8], Name: "Off", Prompt: "x",
		ProjectType: models.ProjectTypeMobileAppNative,
	}
	if _, err := db.DB.NewInsert().Model(project).Exec(ctx); err != nil {
		t.Fatalf("insert project: %v", err)
	}

	envs, err := c.CollectBetterAuthEnvVars(ctx, project)
	if err != nil {
		t.Fatalf("CollectBetterAuthEnvVars: %v", err)
	}
	if len(envs) != 0 {
		t.Errorf("expected empty env vars when auth disabled, got %v", envs)
	}
	if project.BetterAuthSecretCredRef != nil {
		t.Errorf("BetterAuthSecretCredRef should NOT be generated when auth disabled")
	}
}
