package controllers

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
	"github.com/knid/wemob/internal/utils/external"
	"github.com/slack-go/slack"
)

func (c *Controller) GetProjects(user *models.User) ([]responses.ProjectInformationResponse, error) {
	// for loop for each project id

	projects, err := c.DB.GetProjects(user)
	if err != nil {
		return nil, err
	}

	var projectList []responses.ProjectInformationResponse
	for _, v := range projects {
		project, err := c.GetProject(user, v.ID.String())
		if err != nil {
			return nil, err
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
		return nil, errors.New("you do not have access this resources")
	}

	lastBuild, err := c.DB.GetLastProjectBuild(project)
	if err != nil {
		return nil, err
	}

	return &responses.ProjectInformationResponse{
		ID:          project.ID,
		Title:       lastBuild.Title,
		Description: lastBuild.Description,
		URL:         project.URL,
		PackageName: project.PackageName,
		Icon:        project.IconPath,
		CreatedAt:   project.CreatedAt,
	}, nil
}

func (c *Controller) CreateProject(user *models.User, projectCreateReq requests.ProjectCreateRequest) (*responses.ProjectCreateResponse, error) {
	// Create OneSignal App
	// Create new build
	// Release the new build

	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := utils.ValidateFunc[requests.ProjectCreateRequest](projectCreateReq, validate); err != nil {
		return nil, err
	}

	limits, err := c.DB.GetStandartUserLimitsByUser(user)
	if err != nil {
		return nil, err
	}

	projects, err := c.DB.GetProjects(user)
	if err != nil {
		return nil, err
	}

	if len(projects) >= limits.MaximumProjectCount {
		return nil, errors.New("you have reached your project limit")
	}

	if projectCreateReq.BuildForAndroid && !limits.CanBuildToAndroid {
		return nil, errors.New("your subscription does not allow android builds")
	}

	if projectCreateReq.BuildForIOS && !limits.CanBuildToIOS {
		return nil, errors.New("your subscription does not allow ios builds")
	}

	/* 	android, ios := false, false // Check subscription

	   	if ios && user.UserAppleTeamID == "" {
	   		return nil, errors.New("you need to set Apple Team ID in profile settings to use auto deploy for iOS")
	   	} else if projectCreateReq.UseAutoDeployForIOS && projectCreateReq.UserAppleTeamID != "" {
	   		user.UserAppleTeamID = projectCreateReq.UserAppleTeamID
	   		var err error
	   		user, err = c.DB.UpdateUser(user)
	   		if err != nil {
	   			return nil, err
	   		}
	   	} */

	pkName, err := utils.RandomHex(5)
	pkName = "com.wemob.app" + pkName
	if err != nil {
		return nil, err
	}
	project := models.Project{
		UserID:                  user.ID,
		URL:                     projectCreateReq.URL,
		UserAppleTeamID:         projectCreateReq.UserAppleTeamID,
		UseAutoDeployForIOS:     projectCreateReq.UseAutoDeployForIOS,
		UseAutoDeployForAndroid: projectCreateReq.UseAutoDeployForAndroid,
		BuildForAndroid:         projectCreateReq.BuildForAndroid,
		BuildForIOS:             projectCreateReq.BuildForIOS,
		PackageName:             pkName,
	}

	onesignal_app_id, err := external.CreateOneSignalApplication(project)
	if err != nil {
		return nil, err
	}

	project.OneSignalAppID = onesignal_app_id

	time.Sleep(2 * time.Second)

	onesignal_app_api_key, err := external.CreateOneSignalAppToken(project)
	if err != nil {
		return nil, err
	}
	project.OneSignalAppToken = onesignal_app_api_key

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

	projectCreateReq.AppIcon = ""
	jsonProjectCreateReq, err := json.Marshal(projectCreateReq)
	if err != nil {
		jsonProjectCreateReq = []byte(createdProject.ID.String())
	}

	go c.SlackService.Client.PostMessage(
		constants.SLACK_CHANNEL_ID_NEW_PROJECT,
		slack.MsgOptionText("New project: "+string(jsonProjectCreateReq), false),
	)

	createdBuildResponse, err := c.CreateBuild(buildCreateReq)
	if err != nil {
		return nil, err
	}

	return &responses.ProjectCreateResponse{
		TaskID:          createdBuildResponse.TaskID,
		SuccessResponse: responses.SuccessResponse{Success: true},
	}, nil
}

func (c *Controller) UpdateProject(r *http.Request, user *models.User, projectCreateReq requests.ProjectCreateRequest) (*responses.ProjectInformationResponse, error) {
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := utils.ValidateFunc[requests.ProjectCreateRequest](projectCreateReq, validate); err != nil {
		if err.Error() != "icon: is required" {
			return nil, err
		}
	}

	limits, err := c.DB.GetStandartUserLimitsByUser(user)
	if err != nil {
		return nil, err
	}

	if projectCreateReq.BuildForAndroid && !limits.CanBuildToAndroid {
		return nil, errors.New("your subscription does not allow android builds")
	}

	if projectCreateReq.BuildForIOS && !limits.CanBuildToIOS {
		return nil, errors.New("your subscription does not allow ios builds")
	}

	projectId := chi.URLParam(r, "projectId")
	if projectId == "" {
		return nil, errors.New("project_id is required")
	}

	project, err := c.DB.GetProjectById(projectId)
	if err != nil {
		return nil, err
	}

	if project.UserID.String() != user.ID.String() {
		return nil, errors.New("you do not have access this resources")
	}

	buildCreateReq := requests.BuildCreateRequest{
		ProjectID:         project.ID,
		Title:             projectCreateReq.Title,
		Description:       projectCreateReq.Description,
		URL:               projectCreateReq.URL,
		Icon:              projectCreateReq.AppIcon,
		SplashScreenColor: projectCreateReq.SplashScreenColor,
	}

	projectCreateReq.AppIcon = ""
	jsonProjectCreateReq, err := json.Marshal(projectCreateReq)
	if err != nil {
		jsonProjectCreateReq = []byte(project.ID.String())
	}

	go c.SlackService.Client.PostMessage(
		constants.SLACK_CHANNEL_ID_NEW_PROJECT,
		slack.MsgOptionText("New project update: "+string(jsonProjectCreateReq), false),
	)

	_, err = c.CreateBuild(buildCreateReq)
	if err != nil {
		return nil, err
	}

	project.UserAppleTeamID = projectCreateReq.UserAppleTeamID
	project.UseAutoDeployForIOS = projectCreateReq.UseAutoDeployForIOS
	project.UseAutoDeployForAndroid = projectCreateReq.UseAutoDeployForAndroid
	project.BuildForAndroid = projectCreateReq.BuildForAndroid
	project.BuildForIOS = projectCreateReq.BuildForIOS

	updatedProject, err := c.DB.UpdateProject(project)
	if err != nil {
		return nil, err
	}

	updatedProjectResponse, err := c.GetProject(user, updatedProject.ID.String())
	if err != nil {
		return nil, err
	}

	return updatedProjectResponse, nil
}

func (c *Controller) DeleteProject(user *models.User, projectId string) (*responses.ProjectDeleteResponse, error) {
	project, err := c.DB.GetProjectById(projectId)
	if err != nil {
		return nil, err
	}

	if project.UserID.String() != user.ID.String() {
		return nil, errors.New("you do not have access this resources")
	}

	err = c.DB.DeleteProject(project)
	if err != nil {
		return nil, err
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
