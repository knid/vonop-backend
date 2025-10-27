package controllers

import (
	"database/sql"
	"errors"
	"log"
	"log/slog"
	"os"
	"os/exec"

	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
	"github.com/slack-go/slack"
)

func (c *Controller) GetBuilds(project *models.Project) ([]responses.BuildInformationResponse, error) {
	var projectInformationResponses []responses.BuildInformationResponse

	builds, err := c.DB.GetProjectBuilds(project)
	if err != nil {
		return nil, err
	}

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
		return nil, errors.New("you do not have access this resources")
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

func (c *Controller) CreateBuild(user *models.User, buildCreateReq requests.BuildCreateRequest) (*responses.BuildCreateResponse, error) {
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := validate.Struct(buildCreateReq); err != nil {
		return nil, err
	}

	project, err := c.DB.GetProjectById(buildCreateReq.ProjectID.String())
	if err != nil {
		return nil, err
	}

	pendingTasks, err := c.DB.GetPendingAppBuildTasks(project)
	if err != nil {
		return nil, err
	}

	if len(pendingTasks) > 0 {
		return nil, errors.New("you have a pending build task. Please wait until it is finished")
	}

	var lastBuildNumber int
	lastBuild, err := c.DB.GetLastProjectBuild(project)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		lastBuildNumber = 0
	} else {
		lastBuildNumber = lastBuild.BuildNumber
	}

	build := models.Build{
		ProjectID:           project.ID,
		Active:              false,
		Title:               buildCreateReq.Title,
		Description:         buildCreateReq.Description,
		SplashScreenColor:   buildCreateReq.SplashScreenColor,
		RemoveWemobBranding: buildCreateReq.RemoveWemobBranding,
		BuildNumber:         lastBuildNumber + 1,
		VersionString:       utils.CreateVersionString(lastBuildNumber + 1),
	}

	createdBuild, err := c.DB.CreateProjectBuild(&build)
	if err != nil {
		return nil, err
	}

	if len(buildCreateReq.Icon) < 256 && /* strings.Contains(buildCreateReq.Icon, "wemob.io") && */ project.IconPath != "" {

	} else {
		_, err = utils.ConvertBase64ToImageSave(buildCreateReq.Icon, "icons/"+createdBuild.ID.String())
		if err != nil {
			return nil, err
		}
		project.IconPath = createdBuild.ID.String() + ".png"

		_, err = c.DB.UpdateProject(project)
		if err != nil {
			return nil, err
		}
	}

	// Create template project
	// Make build
	// Release the new build

	var appBuildTask *models.AppBuildTask

	cmd := exec.Command("python3", "scripts/create_build.py")
	cmd.Args = append(cmd.Args, "--project-id", project.ID.String())
	cmd.Args = append(cmd.Args, "--build-id", createdBuild.ID.String())
	cmd.Args = append(cmd.Args, "--title", createdBuild.Title)
	cmd.Args = append(cmd.Args, "--url", project.URL)
	cmd.Args = append(cmd.Args, "--icon", project.IconPath)
	cmd.Args = append(cmd.Args, "--sscolor", createdBuild.SplashScreenColor)
	cmd.Args = append(cmd.Args, "--onesignal-app-id", project.OneSignalAppID)
	cmd.Args = append(cmd.Args, "--package-name", project.PackageName)
	if createdBuild.RemoveWemobBranding {
		cmd.Args = append(cmd.Args, "--remove-wemob-branding")
	}
	if project.BuildForAndroid {
		appBuildTask, err := c.DB.CreateAppBuildTask(*createdBuild, "Android", "Building", project.UseAutoDeployForAndroid)
		if err != nil {
			return nil, err
		}
		cmd.Args = append(cmd.Args, "--build-task-id", appBuildTask.ID.String())

		cmd.Args = append(cmd.Args, "--build-android")
		if project.UseAutoDeployForAndroid {
			go c.SlackService.Client.PostMessage(
				constants.SLACK_CHANNEL_ID_NEW_ANDROID_DEPLOY_REQUEST,
				slack.MsgOptionText("New android deploy request: ("+project.URL+") "+createdBuild.ID.String(), false),
			)
		}
	}

	if project.BuildForIOS {
		_, err := c.DB.CreateAppBuildTask(*createdBuild, "IOS", "Reviewing", project.UseAutoDeployForIOS)
		if err != nil {
			return nil, err
		}
		go c.SlackService.Client.PostMessage(
			constants.SLACK_CHANNEL_ID_NEW_IOS_BUILD_REQUEST,
			slack.MsgOptionText("New ios build request: ("+project.URL+"), ("+project.UserAppleTeamID+") "+createdBuild.ID.String(), false),
		)

		if project.UseAutoDeployForIOS {
			go c.SlackService.Client.PostMessage(
				constants.SLACK_CHANNEL_ID_NEW_IOS_DEPLOY_REQUEST,
				slack.MsgOptionText("New ios deploy request: ("+project.URL+"), ("+project.UserAppleTeamID+") "+createdBuild.ID.String(), false),
			)
		}
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	go func() {
		c.ConcurrentBuildLimiter <- struct{}{}
		defer func() { <-c.ConcurrentBuildLimiter }()
		if err := cmd.Run(); err != nil {
			log.Println(err)
			slog.Error("Build process failed", slog.String("build_id", createdBuild.ID.String()), slog.String("error", err.Error()))
		} else {
			var taskID string
			if appBuildTask != nil {
				taskID = appBuildTask.ID.String()
			}
			c.CRMService.SendEvent(user, constants.BREVO_EVENT_NEW_BUILD, map[string]any{
				"build_id":       createdBuild.ID.String(),
				"task_id":        taskID,
				"project_id":     project.ID.String(),
				"android_build":  project.BuildForAndroid,
				"android_deploy": project.UseAutoDeployForAndroid,
				"ios_build":      project.BuildForIOS,
				"ios_deploy":     project.UseAutoDeployForIOS,
			})
		}
	}()

	return &responses.BuildCreateResponse{
		SuccessResponse: responses.SuccessResponse{Success: true},
	}, nil
}
