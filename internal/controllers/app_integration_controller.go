package controllers

import (
	"encoding/json"
	"fmt"

	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/services/integrations"
)

var integrationProviders = []responses.IntegrationProviderInfo{
	{
		Provider:       "supabase",
		Name:           "Supabase",
		Description:    "Database, authentication, and storage. Use as your website's backend.",
		RequiredFields: []string{"url", "anon_key"},
		Website:        "https://supabase.com",
	},
	{
		Provider:       "firebase",
		Name:           "Firebase",
		Description:    "Database, authentication, and storage by Google.",
		RequiredFields: []string{"config"},
		Website:        "https://firebase.google.com",
	},
	{
		Provider:       "stripe",
		Name:           "Stripe",
		Description:    "Accept payments and sell products on your website.",
		RequiredFields: []string{"publishable_key"},
		Website:        "https://stripe.com",
	},
	{
		Provider:       "iyzico",
		Name:           "Iyzico",
		Description:    "Accept payments in Turkey and surrounding regions.",
		RequiredFields: []string{"api_key"},
		Website:        "https://www.iyzico.com",
	},
	{
		Provider:       "resend",
		Name:           "Resend",
		Description:    "Send transactional emails from your website.",
		RequiredFields: []string{"api_key"},
		Website:        "https://resend.com",
	},
	{
		Provider:       "cloudinary",
		Name:           "Cloudinary",
		Description:    "Image and video optimization, upload, and transformation.",
		RequiredFields: []string{"cloud_name", "upload_preset"},
		Website:        "https://cloudinary.com",
	},
	{
		Provider:       "google_sheets",
		Name:           "Google Sheets",
		Description:    "Use Google Sheets as a simple database for your website content.",
		RequiredFields: []string{"api_key", "sheet_id"},
		Website:        "https://sheets.google.com",
	},
}

func (c *Controller) GetIntegrationProviders() []responses.IntegrationProviderInfo {
	return integrationProviders
}

func (c *Controller) GetAppIntegrations(projectID string) ([]models.AppIntegration, error) {
	return c.DB.GetAppIntegrationsByProjectID(projectID)
}

func (c *Controller) ConnectAppIntegration(project *models.AppProject, req requests.ConnectIntegrationRequest) (*models.AppIntegration, error) {
	validate := validator.New()
	if err := req.Validate(validate); err != nil {
		return nil, err
	}

	// Validate credentials for the provider
	if err := integrations.ValidateCredentials(req.Provider, req.Credentials); err != nil {
		return nil, err
	}

	// Check if integration already exists
	existing, err := c.DB.GetAppIntegrationByProvider(project.ID.String(), req.Provider)
	if err == nil && existing.ID.String() != "" {
		return nil, fmt.Errorf("integration %s already connected", req.Provider)
	}

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

	if err := integrations.ValidateCredentials(integ.Provider, req.Credentials); err != nil {
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
