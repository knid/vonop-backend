package controllers

import (
	"fmt"

	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
)

// AdminListProjects returns a paginated list of legacy projects with owner info.
func (c *Controller) AdminListProjects(params requests.AdminListParams) (*responses.PaginatedResponse, error) {
	params.Defaults()

	dbParams := database.AdminProjectListParams{
		Page:     params.Page,
		PageSize: params.PageSize,
		Search:   params.Search,
		SortDir:  params.SortDir,
		From:     params.From,
		To:       params.To,
	}

	projects, total, err := c.DB.AdminListProjects(dbParams)
	if err != nil {
		return nil, fmt.Errorf("failed to list projects: %w", err)
	}

	items := make([]responses.AdminProjectResponse, len(projects))
	for i, p := range projects {
		userEmail := ""
		if p.User != nil {
			userEmail = p.User.Email
		}
		items[i] = responses.AdminProjectResponse{
			ID:          p.ID,
			UserID:      p.UserID,
			UserEmail:   userEmail,
			URL:         p.URL,
			PackageName: p.PackageName,
			CreatedAt:   p.CreatedAt,
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

// AdminGetProject returns detailed info for a single legacy project.
func (c *Controller) AdminGetProject(projectID string) (*responses.AdminProjectResponse, error) {
	project, err := c.DB.AdminGetProjectByID(projectID)
	if err != nil {
		return nil, fmt.Errorf("project not found: %w", err)
	}

	userEmail := ""
	if project.User != nil {
		userEmail = project.User.Email
	}

	return &responses.AdminProjectResponse{
		ID:          project.ID,
		UserID:      project.UserID,
		UserEmail:   userEmail,
		URL:         project.URL,
		PackageName: project.PackageName,
		CreatedAt:   project.CreatedAt,
	}, nil
}

// AdminDeleteProject soft-deletes a legacy project.
func (c *Controller) AdminDeleteProject(projectID string) error {
	project, err := c.DB.GetProjectById(projectID)
	if err != nil {
		return fmt.Errorf("project not found: %w", err)
	}
	if err := c.DB.DeleteProject(project); err != nil {
		return fmt.Errorf("failed to delete project: %w", err)
	}
	return nil
}
