package controllers

import (
	"errors"

	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils/external"
)

func (c *Controller) GetProjects(user *models.User) ([]models.Project, error) {
	return c.DB.GetProjects(user)
}

func (c *Controller) GetProjectModelById(id string) (*models.Project, error) {
	return c.DB.GetProjectById(id)
}

func (c *Controller) GetProject(user *models.User, project_id string) (*responses.ProjectInformationResponse, error) {
	project, err := c.DB.GetProjectById(project_id)
	if err != nil {
		return nil, err
	}

	if project.User.ID.String() != user.ID.String() {
		return nil, errors.New("you do not have access this resources")
	}

	activeBuild, err := c.DB.GetActiveProjectBuild(project)
	if err != nil {
		return nil, err
	}

	return &responses.ProjectInformationResponse{
		ID:            project.ID,
		Title:         activeBuild.Title,
		Description:   activeBuild.Description,
		Icon:          activeBuild.Icon,
		Version:       activeBuild.BuildNumber,
		URL:           project.URL,
		PackageName:   project.PackageName,
		ActiveBuildID: activeBuild.ID,
	}, nil
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

func (c *Controller) DeleteProject(user *models.User, projectId string) (*responses.ProjectDeleteResponse, error) {
	project, err := c.DB.GetProjectById(projectId)
	if err != nil {
		return nil, err
	}

	if project.User.ID.String() != user.ID.String() {
		return nil, errors.New("you do not have access this resources")
	}

	/* projectBuilds, err := c.GetBuilds(*project)
	if err != nil {
		return nil, err
	} */

	// TODO:
	//
	// Delete builds
	// Delete onesignal app
	// Delete app store and google play apps || push msg to signal

	return nil, nil

}
