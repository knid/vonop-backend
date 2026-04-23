package controllers

import (
	"encoding/json"
	"fmt"

	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/services/integrations"
)

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

	credsJSON, _ := json.Marshal(req.Credentials)

	integration := &models.AppIntegration{
		AppProjectID: project.ID,
		Provider:     req.Provider,
		Credentials:  credsJSON,
		Config:       map[string]any{},
		Status:       "connected",
		EnvVars:      envVars,
	}

	return c.DB.CreateAppIntegration(integration)
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

	credsJSON, _ := json.Marshal(req.Credentials)
	integ.Credentials = credsJSON
	integ.EnvVars = envVars
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
