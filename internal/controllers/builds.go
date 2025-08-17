package controllers

import (
	"os/exec"

	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
)

func (c *Controller) GetBuilds(project models.Project) ([]models.Build, error) {
	return c.DB.GetProjectBuilds(&project)
}

func (c *Controller) CreateBuild(buildCreateReq requests.BuildCreateRequest) (*responses.BuildCreateResponse, error) {
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := validate.Struct(buildCreateReq); err != nil {
		return nil, err
	}

	project, err := c.DB.GetProjectById(buildCreateReq.ProjectID.String())
	if err != nil {
		return nil, err
	}

	build := models.Build{
		Project:           *project,
		Active:            false,
		Title:             buildCreateReq.Title,
		Description:       buildCreateReq.Description,
		Icon:              buildCreateReq.Icon,
		SplashScreenColor: buildCreateReq.SplashScreenColor,
		BuildNumber:       1,
	}

	createdBuild, err := c.DB.CreateProjectBuild(&build)
	if err != nil {
		return nil, err
	}

	appBuildTask, err := c.DB.CreateAppBuildTask(*createdBuild)
	if err != nil {
		return nil, err
	}

	cmd := exec.Command("./scripts/create_build.py", createdBuild.ID.String(), createdBuild.Title, project.URL)
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	return &responses.BuildCreateResponse{
		SuccessResponse: responses.SuccessResponse{Success: true},
		TaskID:          appBuildTask.ID,
	}, nil
}
