package controllers

import (
	"errors"
	"log/slog"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
)

func (c *Controller) GetProjects(user *models.User) ([]responses.ProjectInformationResponse, error) {
	projects, err := c.DB.GetProjects(user)
	if err != nil {
		return nil, err
	}

	var projectList []responses.ProjectInformationResponse
	for _, v := range projects {
		project, err := c.GetProject(user, v.ID.String())
		if err != nil {
			// Skip projects without builds.
			slog.Warn("skipping project in list", "project_id", v.ID, "error", err)
			continue
		}
		projectList = append(projectList, *project)
	}

	return projectList, nil
}

func (c *Controller) GetProjectModelById(id string) (*models.Project, error) {
	return c.DB.GetProjectById(id)
}

func (c *Controller) GetProject(user *models.User, project_id string) (*responses.ProjectInformationResponse, error) {
	project, err := c.DB.GetProjectById(project_id)
	if err != nil {
		return nil, err
	}

	if project.UserID.String() != user.ID.String() {
		return nil, errors.New("you do not have access to this resource")
	}

	resp := &responses.ProjectInformationResponse{
		ID:          project.ID,
		URL:         project.URL,
		PackageName: project.PackageName,
		Icon:        project.IconPath,
		CreatedAt:   project.CreatedAt,
	}

	// Title/Description previously came from the most recent legacy Build row,
	// but the v1 builds.project_id FK was dropped 2026-05-07. The legacy v1
	// web project info endpoint now leaves these fields zero — clients should
	// read project metadata from AppProject going forward.

	return resp, nil
}

func (c *Controller) DeleteProject(user *models.User, projectId string) (*responses.ProjectDeleteResponse, error) {
	project, err := c.DB.GetProjectById(projectId)
	if err != nil {
		return nil, err
	}

	if project.UserID.String() != user.ID.String() {
		return nil, errors.New("you do not have access to this resource")
	}

	err = c.DB.DeleteProject(project)
	if err != nil {
		return nil, err
	}

	go c.EventService.SendEvent(user, constants.EVENT_PROJECT_DELETED, map[string]any{
		"project_id": project.ID.String(),
	})

	return nil, nil
}
