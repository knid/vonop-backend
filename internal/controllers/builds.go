package controllers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/hibiken/asynq"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/tasks"
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
	fmt.Printf("Project: %+v", project)

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
		Icon:                buildCreateReq.Icon,
		SplashScreenColor:   buildCreateReq.SplashScreenColor,
		NavBarStyle:         buildCreateReq.NavBarStyle,
		RemoveWemobBranding: buildCreateReq.RemoveWemobBranding,
		BuildNumber:         lastBuildNumber + 1,
		VersionString:       utils.CreateVersionString(lastBuildNumber + 1),
	}

	createdBuild, err := c.DB.CreateProjectBuild(&build)
	if err != nil {
		return nil, err
	}

	if project.BuildForAndroid {
		fmt.Println("Creating android build task")

		appBuildTask, err := c.DB.CreateAppBuildTask(*createdBuild, "Android", "Queued", project.UseAutoDeployForAndroid)
		if err != nil {
			return nil, err
		}

		androidBuildPayload := &tasks.AndroidBuildPayload{
			ProjectID:           project.ID.String(),
			Version:             createdBuild.BuildNumber,
			BuildID:             createdBuild.ID.String(),
			TaskID:              appBuildTask.ID.String(),
			Title:               createdBuild.Title,
			URL:                 project.URL,
			IconURL:             createdBuild.Icon,
			SplashScreenColor:   createdBuild.SplashScreenColor,
			NavBarStyle:         createdBuild.NavBarStyle,
			OneSignalAppID:      project.OneSignalAppID,
			PackageName:         project.PackageName,
			RemoveWemobBranding: createdBuild.RemoveWemobBranding,
			Secret:              appBuildTask.ID.String(),
		}
		taskPayload, err := json.Marshal(androidBuildPayload)
		if err != nil {
			return nil, err
		}
		task := asynq.NewTask(tasks.TypeBuildAndroid, taskPayload)
		c.TaskService.Enqueue(task, asynq.MaxRetry(2), asynq.Timeout(time.Minute*10))

		/* if project.UseAutoDeployForAndroid {
			go c.SlackService.Client.PostMessage(
				constants.SLACK_CHANNEL_ID_NEW_ANDROID_DEPLOY_REQUEST,
				slack.MsgOptionText("New android deploy request: ("+project.URL+") "+createdBuild.ID.String(), false),
			)
		} */
	}

	if project.BuildForIOS {
		fmt.Println("Creating ios build task")
		appBuildTask, err := c.DB.CreateAppBuildTask(*createdBuild, "IOS", "Queued", project.UseAutoDeployForIOS)
		if err != nil {
			return nil, err
		}

		iosBuildPayload := &tasks.BuildUnsignedIpaPayload{
			ProjectID:           project.ID.String(),
			Version:             createdBuild.BuildNumber,
			BuildID:             createdBuild.ID.String(),
			TaskID:              appBuildTask.ID.String(),
			Title:               createdBuild.Title,
			URL:                 project.URL,
			IconURL:             createdBuild.Icon,
			SplashScreenColor:   createdBuild.SplashScreenColor,
			OneSignalAppID:      project.OneSignalAppID,
			PackageName:         project.PackageName,
			RemoveWemobBranding: createdBuild.RemoveWemobBranding,
			Secret:              appBuildTask.ID.String(),
		}
		taskPayload, err := json.Marshal(iosBuildPayload)
		if err != nil {
			return nil, err
		}
		task := asynq.NewTask(tasks.TypeBuildUnsignedIpa, taskPayload)
		c.TaskService.Enqueue(task, asynq.MaxRetry(2), asynq.Timeout(time.Minute*10))

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

	/* meta := map[string]any{
		"build_id":   createdBuild.ID.String(),
		"project_id": project.ID.String(),
		"user_id":    user.ID.String(),
	} */

	/* cmd.Stdout = &utils.CommandLogWriter{Level: slog.LevelInfo, Meta: meta}
	cmd.Stderr = &utils.CommandLogWriter{Level: slog.LevelError, Meta: meta} */

	/* go func() {
		c.ConcurrentBuildLimiter <- struct{}{}
		defer func() { <-c.ConcurrentBuildLimiter }()
		slog.Default().With("from", "BUILD CMDS").Info("Starting build process", slog.Any("command_list", cmd.Args))
		if err := cmd.Run(); err != nil {
			slog.Default().With("from", "BUILD CMDS").Error("Build process failed", slog.String("build_id", createdBuild.ID.String()), slog.String("error", err.Error()))
		} else {
			var taskID string
			if appBuildTask != nil {
				taskID = appBuildTask.ID.String()
			}
			c.CRMService.SendEvent(user, constants.BREVO_EVENT_BUILD_FINISHED, map[string]any{
				"build_id":       createdBuild.ID.String(),
				"task_id":        taskID,
				"project_id":     project.ID.String(),
				"android_build":  project.BuildForAndroid,
				"android_deploy": project.UseAutoDeployForAndroid,
				"ios_build":      project.BuildForIOS,
				"ios_deploy":     project.UseAutoDeployForIOS,
			})
		}
	}() */

	return &responses.BuildCreateResponse{
		SuccessResponse: responses.SuccessResponse{Success: true},
	}, nil
}
