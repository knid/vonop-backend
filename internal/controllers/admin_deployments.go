package controllers

import (
	"fmt"

	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
)

// AdminListDeployments returns a paginated list of deployments with app and user info.
func (c *Controller) AdminListDeployments(params requests.AdminListParams) (*responses.PaginatedResponse, error) {
	params.Defaults()

	dbParams := database.AdminDeploymentListParams{
		Page:     params.Page,
		PageSize: params.PageSize,
		Search:   params.Search,
		Status:   params.Status,
		SortDir:  params.SortDir,
	}

	deployments, total, err := c.DB.AdminListDeployments(dbParams)
	if err != nil {
		return nil, fmt.Errorf("failed to list deployments: %w", err)
	}

	items := make([]responses.AdminDeploymentResponse, len(deployments))
	for i, d := range deployments {
		appName := ""
		if d.AppProject != nil {
			appName = d.AppProject.Name
		}
		userEmail := ""
		if d.User != nil {
			userEmail = d.User.Email
		}
		items[i] = responses.AdminDeploymentResponse{
			ID:        d.ID,
			AppID:     d.AppProjectID,
			AppName:   appName,
			UserEmail: userEmail,
			Platform:  d.Platform,
			Status:    d.Status,
			URL:       d.DeployedURL,
			CreatedAt: d.CreatedAt,
		}
	}

	totalPages := total / params.PageSize
	if total%params.PageSize != 0 {
		totalPages++
	}

	return &responses.PaginatedResponse{
		Data:       items,
		Total:      total,
		Page:       params.Page,
		PageSize:   params.PageSize,
		TotalPages: totalPages,
	}, nil
}
