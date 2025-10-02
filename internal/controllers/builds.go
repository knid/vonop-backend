package controllers

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/slack-go/slack"
)

func (c *Controller) GetBuilds(project *models.Project) ([]models.Build, error) {
	return c.DB.GetProjectBuilds(project)
}

func (c *Controller) GetBuild(user *models.User, id string) (*models.Build, error) {
	build, err := c.DB.GetProjectBuildById(id)
	if err != nil {
		return nil, err
	}

	if build.Project.User.ID.String() != user.ID.String() {
		return nil, errors.New("you do not have access this resources")
	}

	return build, nil
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

	pendingTasks, err := c.DB.GetPendingAppBuildTasks(project)
	fmt.Println("Pending tasks:", pendingTasks)
	if err != nil {
		return nil, err
	}

	if len(pendingTasks) > 0 {
		return nil, errors.New("you have a pending build task. Please wait until it is finished")
	}

	build := models.Build{
		ProjectID:         project.ID,
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

	// Create template project
	// Make build
	// Release the new build

	cmd := exec.Command("python3", "scripts/create_build.py")
	cmd.Args = append(cmd.Args, "--build-task-id", appBuildTask.ID.String())
	cmd.Args = append(cmd.Args, "--build-id", createdBuild.ID.String())
	cmd.Args = append(cmd.Args, "--title", createdBuild.Title)
	cmd.Args = append(cmd.Args, "--url", project.URL)
	cmd.Args = append(cmd.Args, "--icon", createdBuild.Icon)
	cmd.Args = append(cmd.Args, "--sscolor", createdBuild.SplashScreenColor)

	if project.BuildForAndroid {
		cmd.Args = append(cmd.Args, "--build-android")
		if project.UseAutoDeployForAndroid {
			c.SlackService.Client.PostMessage(
				constants.SLACK_CHANNEL_ID_NEW_ANDROID_DEPLOY_REQUEST,
				slack.MsgOptionText("New android deploy request: ("+project.URL+") "+createdBuild.ID.String(), false),
			)
		}
	}

	if project.BuildForIOS {
		c.SlackService.Client.PostMessage(
			constants.SLACK_CHANNEL_ID_NEW_IOS_BUILD_REQUEST,
			slack.MsgOptionText("New ios build request: ("+project.URL+"), ("+project.UserAppleTeamID+") "+createdBuild.ID.String(), false),
		)

		if project.UseAutoDeployForIOS {
			c.SlackService.Client.PostMessage(
				constants.SLACK_CHANNEL_ID_NEW_IOS_DEPLOY_REQUEST,
				slack.MsgOptionText("New ios deploy request: ("+project.URL+"), ("+project.UserAppleTeamID+") "+createdBuild.ID.String(), false),
			)
		}
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	return &responses.BuildCreateResponse{
		SuccessResponse: responses.SuccessResponse{Success: true},
		TaskID:          appBuildTask.ID,
	}, nil
}
