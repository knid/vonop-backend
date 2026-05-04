package controllers

import (
	"errors"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
)

func (c *Controller) GetBuilds(project *models.Project) ([]responses.BuildInformationResponse, error) {
	builds, err := c.DB.GetProjectBuilds(project)
	if err != nil {
		return nil, err
	}

	projectInformationResponses := make([]responses.BuildInformationResponse, 0, len(builds))
	for _, build := range builds {
		buildInformationResponse := responses.BuildInformationResponse{
			ID:                  build.ID,
			Title:               build.Title,
			Description:         build.Description,
			Icon:                build.Icon,
			SplashScreenColor:   build.SplashScreenColor,
			RemoveWemobBranding: build.RemoveWemobBranding,
			BuildNumber:         build.BuildNumber,
			CreatedAt:           build.CreatedAt,
		}
		projectInformationResponses = append(projectInformationResponses, buildInformationResponse)
	}
	return projectInformationResponses, nil
}

func (c *Controller) GetBuild(user *models.User, id string) (*responses.BuildInformationResponse, error) {
	build, err := c.DB.GetProjectBuildById(id)
	if err != nil {
		return nil, err
	}

	project, err := c.DB.GetProjectById(build.ProjectID.String())
	if err != nil {
		return nil, err
	}

	if project.UserID.String() != user.ID.String() {
		return nil, errors.New("you do not have access to this resource")
	}

	buildInformationResponse := responses.BuildInformationResponse{
		ID:                  build.ID,
		Title:               build.Title,
		Description:         build.Description,
		Icon:                build.Icon,
		SplashScreenColor:   build.SplashScreenColor,
		RemoveWemobBranding: build.RemoveWemobBranding,
		BuildNumber:         build.BuildNumber,
		CreatedAt:           build.CreatedAt,
	}

	return &buildInformationResponse, nil
}

