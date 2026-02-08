package controllers

import (
	"encoding/json"
	"fmt"

	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/services/integrations"
)

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
