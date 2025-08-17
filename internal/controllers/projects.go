package controllers

import (
	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils/external"
)

func (c *Controller) GetProjects(user *models.User) ([]models.Project, error) {
	return c.DB.GetProjects(user)
}

func (c *Controller) CreateProject(user *models.User, projectCreateReq requests.ProjectCreateRequest) (*responses.ProjectCreateResponse, error) {
	// Create OneSignal App
	// Create new build
	// Release the new build

	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := validate.Struct(projectCreateReq); err != nil {
		return nil, err
	}

	project := models.Project{
		User: *user,
		URL:  projectCreateReq.URL,
	}

	onesignal_app_id, err := external.CreateOneSignalApplication(project)
	if err != nil {
		return nil, err
	}

	project.OneSignalAppID = onesignal_app_id

	createdProject, err := c.DB.CreateProject(&project)
	if err != nil {
		return nil, err
	}

	buildCreateReq := requests.BuildCreateRequest{
		ProjectID:         createdProject.ID,
		Title:             projectCreateReq.Title,
		Description:       projectCreateReq.Description,
		URL:               projectCreateReq.URL,
		Icon:              projectCreateReq.AppIcon,
		SplashScreenColor: projectCreateReq.SplashScreenColor,
	}

	createdBuildResponse, err := c.CreateBuild(buildCreateReq)
	if err != nil {
		return nil, err
	}

	return &responses.ProjectCreateResponse{
		TaskID:          createdBuildResponse.TaskID,
		SuccessResponse: responses.SuccessResponse{Success: true},
	}, nil
}
