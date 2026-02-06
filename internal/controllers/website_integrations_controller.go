package controllers

import (
	"errors"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
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

func (c *Controller) GetWebsiteIntegrations(user *models.User, websiteId string) ([]responses.IntegrationResponse, error) {
	_, err := c.verifyWebsiteOwnership(user, websiteId)
	if err != nil {
		return nil, err
	}

	integrations, err := c.DB.GetIntegrationsByWebsiteId(websiteId)
	if err != nil {
		return nil, err
	}

	result := make([]responses.IntegrationResponse, len(integrations))
	for i, integ := range integrations {
		result[i] = responses.IntegrationResponse{
			ID:        integ.ID,
			WebsiteID: integ.WebsiteID,
			Provider:  integ.Provider,
			Config:    integ.Config,
			Status:    integ.Status,
			CreatedAt: integ.CreatedAt,
			UpdatedAt: integ.UpdatedAt,
		}
	}
	return result, nil
}

func (c *Controller) ConnectIntegration(user *models.User, websiteId string, req requests.IntegrationConnectRequest) (*responses.IntegrationResponse, error) {
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := utils.ValidateFunc[requests.IntegrationConnectRequest](req, validate); err != nil {
		return nil, err
	}

	website, err := c.verifyWebsiteOwnership(user, websiteId)
	if err != nil {
		return nil, err
	}

	// Check if provider is already connected
	existing, err := c.DB.GetIntegrationByWebsiteIdAndProvider(websiteId, req.Provider)
	if err == nil && existing != nil {
		return nil, errors.New("this provider is already connected. disconnect it first to reconnect")
	}

	// Validate required fields
	for _, provider := range integrationProviders {
		if provider.Provider == req.Provider {
			for _, field := range provider.RequiredFields {
				if _, ok := req.Credentials[field]; !ok {
					return nil, errors.New("missing required credential: " + field)
				}
			}
			break
		}
	}

	config := req.Config
	if config == nil {
		config = map[string]any{}
	}

	integration := models.WebsiteIntegration{
		WebsiteID:   website.ID,
		Provider:    req.Provider,
		Credentials: req.Credentials,
		Config:      config,
		Status:      "connected",
	}

	created, err := c.DB.CreateIntegration(&integration)
	if err != nil {
		return nil, err
	}

	return &responses.IntegrationResponse{
		ID:        created.ID,
		WebsiteID: created.WebsiteID,
		Provider:  created.Provider,
		Config:    created.Config,
		Status:    created.Status,
		CreatedAt: created.CreatedAt,
		UpdatedAt: created.UpdatedAt,
	}, nil
}

func (c *Controller) UpdateIntegration(user *models.User, websiteId string, integrationId string, req requests.IntegrationUpdateRequest) (*responses.IntegrationResponse, error) {
	_, err := c.verifyWebsiteOwnership(user, websiteId)
	if err != nil {
		return nil, err
	}

	integration, err := c.DB.GetIntegrationById(integrationId)
	if err != nil {
		return nil, errors.New("integration not found")
	}

	if integration.WebsiteID.String() != websiteId {
		return nil, errors.New("integration does not belong to this website")
	}

	if req.Credentials != nil {
		integration.Credentials = req.Credentials
	}
	if req.Config != nil {
		integration.Config = req.Config
	}
	integration.UpdatedAt = time.Now()

	updated, err := c.DB.UpdateIntegration(integration)
	if err != nil {
		return nil, err
	}

	return &responses.IntegrationResponse{
		ID:        updated.ID,
		WebsiteID: updated.WebsiteID,
		Provider:  updated.Provider,
		Config:    updated.Config,
		Status:    updated.Status,
		CreatedAt: updated.CreatedAt,
		UpdatedAt: updated.UpdatedAt,
	}, nil
}

func (c *Controller) DisconnectIntegration(user *models.User, websiteId string, integrationId string) error {
	_, err := c.verifyWebsiteOwnership(user, websiteId)
	if err != nil {
		return err
	}

	integration, err := c.DB.GetIntegrationById(integrationId)
	if err != nil {
		return errors.New("integration not found")
	}

	if integration.WebsiteID.String() != websiteId {
		return errors.New("integration does not belong to this website")
	}

	return c.DB.DeleteIntegration(integration)
}
