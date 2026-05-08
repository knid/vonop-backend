package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/services/integrations"
	"github.com/knid/wemob/internal/utils"
)

// integrationCredentialsKey returns the AES-256-GCM hex key used to wrap
// AppIntegration.Credentials and EnvVars at rest. INTEGRATION_CREDENTIALS_ENCRYPTION_KEY
// is the logical name; if unset we fall back to GITHUB_TOKEN_ENCRYPTION_KEY
// so this PR doesn't require a new Infisical secret on day one.
func (c *Controller) integrationCredentialsKey() string {
	if k := c.SecretManager.GetSecret("INTEGRATION_CREDENTIALS_ENCRYPTION_KEY"); k != "" {
		return k
	}
	return c.SecretManager.GetSecret("GITHUB_TOKEN_ENCRYPTION_KEY")
}

// DecryptedIntegrationCredentials returns the credentials JSON as a map with
// any enc:v1: prefixed values decrypted in place. Used by every read path
// that talks to a third-party API with the stored credential.
func (c *Controller) DecryptedIntegrationCredentials(integ *models.AppIntegration) (map[string]any, error) {
	var creds map[string]any
	if len(integ.Credentials) == 0 {
		return creds, nil
	}
	if err := json.Unmarshal(integ.Credentials, &creds); err != nil {
		return nil, err
	}
	return utils.DecryptIntegrationMapAny(creds, c.integrationCredentialsKey()), nil
}

// DecryptedIntegrationEnvVars returns the EnvVars map with enc:v1: prefixed
// values decrypted. Called from CollectAllEnvVars and the dashboard query
// fallback that reads DATABASE_URL out of EnvVars.
func (c *Controller) DecryptedIntegrationEnvVars(integ *models.AppIntegration) map[string]string {
	return utils.DecryptIntegrationMapStr(integ.EnvVars, c.integrationCredentialsKey())
}

// GetIntegrationProviders returns the provider list derived from the single
// source of truth in constants.IntegrationProviders.
func (c *Controller) GetIntegrationProviders() []responses.IntegrationProviderInfo {
	providers := make([]responses.IntegrationProviderInfo, 0, len(constants.IntegrationProviders))
	for _, p := range constants.IntegrationProviders {
		providers = append(providers, responses.IntegrationProviderInfo{
			Provider:       p.Provider,
			Name:           p.Name,
			Description:    p.Description,
			Category:       p.Category,
			RequiredFields: p.RequiredFields,
			OptionalFields: p.OptionalFields,
			Website:        p.Website,
		})
	}
	return providers
}

func (c *Controller) GetAppIntegrations(projectID string) ([]models.AppIntegration, error) {
	return c.DB.GetAppIntegrationsByProjectID(projectID)
}

func (c *Controller) ConnectAppIntegration(project *models.AppProject, req requests.ConnectIntegrationRequest) (*models.AppIntegration, error) {
	validate := validator.New()
	if err := req.Validate(validate); err != nil {
		return nil, err
	}

	// Validate credentials with provider-specific rules (URL format, key prefixes, etc.)
	if err := integrations.ValidateIntegrationConfig(req.Provider, req.Credentials); err != nil {
		return nil, err
	}

	// Test credentials with a live API call
	if err := integrations.TestConnection(req.Provider, req.Credentials); err != nil {
		return nil, fmt.Errorf("credential test failed: %w", err)
	}

	// Check if integration already exists
	existing, err := c.DB.GetAppIntegrationByProvider(project.ID.String(), req.Provider)
	if err == nil && existing.ID.String() != "" {
		return nil, fmt.Errorf("integration %s already connected", req.Provider)
	}

	// Stripe webhook auto-registration is disabled: the generated tenant app
	// hosts its own /api/webhooks/stripe route, and the merchant configures
	// the endpoint + signing secret in their Stripe dashboard once the app
	// has a public URL. If the user supplies a webhook_secret in
	// credentials, it flows through env vars as-is.

	// Build env vars
	envVars, err := integrations.BuildEnvVars(req.Provider, req.Credentials)
	if err != nil {
		return nil, err
	}

	encKey := c.integrationCredentialsKey()
	encCreds := utils.EncryptIntegrationMapAny(req.Credentials, encKey)
	encEnvVars := utils.EncryptIntegrationMapStr(envVars, encKey)
	credsJSON, _ := json.Marshal(encCreds)

	integration := &models.AppIntegration{
		AppProjectID: project.ID,
		Provider:     req.Provider,
		Credentials:  credsJSON,
		Config:       map[string]any{},
		Status:       "connected",
		EnvVars:      encEnvVars,
	}

	created, err := c.DB.CreateAppIntegration(integration)
	if err != nil {
		return nil, err
	}

	if req.Provider == constants.IntegrationIyzico || req.Provider == constants.IntegrationStripe {
		c.enablePaymentProviderInTenantDB(project.ID.String(), req.Provider)
	}

	return created, nil
}

// enablePaymentProviderInTenantDB flips the provider's enabled flag inside the
// tenant Neon DB's config.payment_config jsonb so the storefront /checkout flow
// stops showing "Payment not yet configured". Best-effort: if neon isn't wired
// or the table is missing we log and return nil — the dashboard connect flow
// must not fail when the tenant DB lags behind.
func (c *Controller) enablePaymentProviderInTenantDB(projectID, provider string) {
	db, err := c.openNeonDB(projectID)
	if err != nil {
		slog.Warn("payment_config: skip tenant DB enable", "err", err, "project_id", projectID, "provider", provider)
		return
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const q = `
UPDATE config SET
  payment_config = jsonb_set(
    COALESCE(payment_config, '{}'::jsonb),
    ARRAY[$1],
    '{"enabled": true}'::jsonb,
    true
  ),
  updated_at = NOW()
WHERE id = (SELECT id FROM config ORDER BY id LIMIT 1)`

	if _, err := db.ExecContext(ctx, q, provider); err != nil {
		slog.Warn("payment_config: tenant UPDATE failed", "err", err, "project_id", projectID, "provider", provider)
		return
	}
	slog.Info("payment_config: enabled in tenant DB", "project_id", projectID, "provider", provider)
}

func (c *Controller) UpdateAppIntegration(integrationID string, project *models.AppProject, req requests.UpdateIntegrationRequest) (*models.AppIntegration, error) {
	integ, err := c.DB.GetAppIntegrationByID(integrationID)
	if err != nil {
		return nil, fmt.Errorf("integration not found")
	}
	if integ.AppProjectID != project.ID {
		return nil, fmt.Errorf("unauthorized")
	}

	// Validate with provider-specific rules
	if err := integrations.ValidateIntegrationConfig(integ.Provider, req.Credentials); err != nil {
		return nil, err
	}

	envVars, _ := integrations.BuildEnvVars(integ.Provider, req.Credentials)

	encKey := c.integrationCredentialsKey()
	encCreds := utils.EncryptIntegrationMapAny(req.Credentials, encKey)
	encEnvVars := utils.EncryptIntegrationMapStr(envVars, encKey)
	credsJSON, _ := json.Marshal(encCreds)
	integ.Credentials = credsJSON
	integ.EnvVars = encEnvVars
	if req.Config != nil {
		integ.Config = req.Config
	}

	return c.DB.UpdateAppIntegration(integ)
}

func (c *Controller) DisconnectAppIntegration(integrationID string, project *models.AppProject) error {
	integ, err := c.DB.GetAppIntegrationByID(integrationID)
	if err != nil {
		return fmt.Errorf("integration not found")
	}
	if integ.AppProjectID != project.ID {
		return fmt.Errorf("unauthorized")
	}
	return c.DB.DeleteAppIntegration(integ)
}
