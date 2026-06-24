package controllers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/knid/wemob/internal/models"
)

const BetterAuthSecretKind = "better-auth-secret"

func (c *Controller) EnsureBetterAuthSecret(ctx context.Context, project *models.AppProject) (string, error) {
	if project == nil {
		return "", fmt.Errorf("EnsureBetterAuthSecret: nil project")
	}
	if c.CredStore == nil {
		return "", nil
	}
	if project.BetterAuthSecretCredRef != nil {
		plain, err := c.CredStore.Get(ctx, project.OrganizationID, credRefToStorage(project.BetterAuthSecretCredRef))
		if err != nil {
			return "", fmt.Errorf("EnsureBetterAuthSecret: decrypt: %w", err)
		}
		return string(plain), nil
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("EnsureBetterAuthSecret: rand: %w", err)
	}
	secretHex := hex.EncodeToString(raw)
	ref, err := c.CredStore.Put(ctx, project.OrganizationID, BetterAuthSecretKind, []byte(secretHex))
	if err != nil {
		return "", fmt.Errorf("EnsureBetterAuthSecret: put: %w", err)
	}
	persistedRef := credRefFromStorage(ref)
	if _, err := c.DB.DB.NewUpdate().
		Model((*models.AppProject)(nil)).
		Set("better_auth_secret_cred_ref = ?", persistedRef).
		Where("id = ?", project.ID).
		Exec(ctx); err != nil {
		return "", fmt.Errorf("EnsureBetterAuthSecret: persist ref: %w", err)
	}
	project.BetterAuthSecretCredRef = persistedRef
	return secretHex, nil
}

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
