package controllers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/knid/wemob/internal/models"
)

// BetterAuthSecretKind is the CredStore kind tag for Better-Auth secrets.
const BetterAuthSecretKind = "better-auth-secret"

// EnsureBetterAuthSecret returns the project's BETTER_AUTH_SECRET in plaintext,
// generating + persisting a fresh 32-byte hex-encoded secret on first call.
// Idempotent: subsequent calls return the existing decrypted value. The caller
// must NOT log the return value.
//
// Returns ("", nil) when MobileAppConfig.AuthEnabled is false (or no config
// exists) so callers can invoke unconditionally.
func (c *Controller) EnsureBetterAuthSecret(ctx context.Context, project *models.AppProject) (string, error) {
	if project == nil {
		return "", fmt.Errorf("EnsureBetterAuthSecret: nil project")
	}
	cfg, err := c.DB.GetMobileAppConfig(project.ID.String())
	if err != nil || cfg == nil || !cfg.AuthEnabled {
		// Missing config / disabled => no secret. Not an error — auth is opt-in.
		return "", nil
	}
	if c.CredStore == nil {
		return "", fmt.Errorf("EnsureBetterAuthSecret: CredStore not configured")
	}
	if project.BetterAuthSecretCredRef != nil {
		plain, gerr := c.CredStore.Get(ctx, project.OrganizationID, credRefToStorage(project.BetterAuthSecretCredRef))
		if gerr != nil {
			return "", fmt.Errorf("EnsureBetterAuthSecret: decrypt: %w", gerr)
		}
		return string(plain), nil
	}
	raw := make([]byte, 32)
	if _, rerr := rand.Read(raw); rerr != nil {
		return "", fmt.Errorf("EnsureBetterAuthSecret: rand: %w", rerr)
	}
	secretHex := hex.EncodeToString(raw)
	ref, perr := c.CredStore.Put(ctx, project.OrganizationID, BetterAuthSecretKind, []byte(secretHex))
	if perr != nil {
		return "", fmt.Errorf("EnsureBetterAuthSecret: put: %w", perr)
	}
	persistedRef := credRefFromStorage(ref)
	// Targeted UPDATE to avoid the Bun whole-row update footgun
	// (memory: feedback_bun_whole_row_update.md). Only writes the new column.
	if _, uerr := c.DB.DB.NewUpdate().
		Model((*models.AppProject)(nil)).
		Set("better_auth_secret_cred_ref = ?", persistedRef).
		Where("id = ?", project.ID).
		Exec(ctx); uerr != nil {
		return "", fmt.Errorf("EnsureBetterAuthSecret: persist ref: %w", uerr)
	}
	project.BetterAuthSecretCredRef = persistedRef
	return secretHex, nil
}

// CollectBetterAuthEnvVars returns the env-var map (BETTER_AUTH_SECRET +
// BETTER_AUTH_URL) the deploy worker injects into Fly secrets. Empty map
// when auth is disabled. Lazily generates the secret on first call.
//
// Caller is responsible for not logging the returned map.
func (c *Controller) CollectBetterAuthEnvVars(ctx context.Context, project *models.AppProject) (map[string]string, error) {
	secret, err := c.EnsureBetterAuthSecret(ctx, project)
	if err != nil {
		return nil, err
	}
	if secret == "" {
		return map[string]string{}, nil
	}
	return map[string]string{
		"BETTER_AUTH_SECRET": secret,
		"BETTER_AUTH_URL":    c.BackendDeployURL(project),
	}, nil
}
