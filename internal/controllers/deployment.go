package controllers

import (
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
)

func (c *Controller) GetDeployments(user *models.User) ([]responses.DeploymentResponse, error) {
	deployments, err := c.DB.GetDeployments(user)
	if err != nil {
		return nil, err
	}

	var deploymentResponses []responses.DeploymentResponse
	for _, deployment := range deployments {
		deploymentResponses = append(deploymentResponses, responses.DeploymentResponse{
			ID:                     deployment.ID,
			UserID:                 deployment.UserID,
			ProjectID:              deployment.ProjectID,
			BuildID:                deployment.BuildID,
			Platforms:              deployment.Platforms,
			IconID:                 deployment.IconPath,
			Title:                  deployment.Title,
			Description:            deployment.Description,
			Status:                 deployment.Status,
			ScreenshotIDs:          deployment.ScreenshotPaths,
			Method:                 deployment.Method,
			AppleConnectionID:      deployment.AppleConnectionID,
			GooglePlayConnectionID: deployment.GooglePlayConnectionID,
			CreatedAt:              deployment.CreatedAt,
			UpdatedAt:              deployment.UpdatedAt,
		})
	}

	return deploymentResponses, nil
}

func (c *Controller) GetDeploymentByID(deploymentID string) (*responses.DeploymentResponse, error) {
	deployment, err := c.DB.GetDeploymentByID(deploymentID)
	if err != nil {
		return nil, err
	}

	return &responses.DeploymentResponse{
		ID:                     deployment.ID,
		UserID:                 deployment.UserID,
		ProjectID:              deployment.ProjectID,
		BuildID:                deployment.BuildID,
		Platforms:              deployment.Platforms,
		IconID:                 deployment.IconPath,
		Title:                  deployment.Title,
		Description:            deployment.Description,
		Status:                 deployment.Status,
		ScreenshotIDs:          deployment.ScreenshotPaths,
		Method:                 deployment.Method,
		AppleConnectionID:      deployment.AppleConnectionID,
		GooglePlayConnectionID: deployment.GooglePlayConnectionID,
		CreatedAt:              deployment.CreatedAt,
		UpdatedAt:              deployment.UpdatedAt,
	}, nil
}
