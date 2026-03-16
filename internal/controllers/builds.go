package controllers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/hibiken/asynq"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/tasks"
	"github.com/knid/wemob/internal/utils"
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

func (c *Controller) CreateBuild(user *models.User, buildCreateReq requests.BuildCreateRequest) (*responses.BuildCreateResponse, error) {
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := buildCreateReq.Validate(validate); err != nil {
		return nil, err
	}

	// Enforce brand logo removal permission at controller level
	if buildCreateReq.RemoveWemobBranding {
		limits, limitsErr := c.DB.GetStandartUserLimitsByUser(user)
		if limitsErr != nil {
			return nil, fmt.Errorf("failed to verify subscription limits: %w", limitsErr)
		}
		if !limits.CanRemoveBrandLogo {
			return nil, errors.New("your subscription does not allow removing Wemob branding")
		}
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

	// Fetch mobile app config for permissions and deep linking
	mobileConfig, _ := c.DB.GetMobileAppConfig(project.ID.String())

	// Track created tasks for rollback on failure
	var createdTaskIDs []string

	if project.BuildForAndroid {
		appBuildTask, err := c.DB.CreateAppBuildTask(*createdBuild, "Android", "Queued", project.UseAutoDeployForAndroid)
		if err != nil {
			return nil, err
		}
		createdTaskIDs = append(createdTaskIDs, appBuildTask.ID.String())

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
			Permissions: func() []string {
				if mobileConfig != nil {
					return mobileConfig.Permissions
				}
				return nil
			}(),
			URLScheme: func() string {
				if mobileConfig != nil {
					return mobileConfig.URLScheme
				}
				return ""
			}(),
			UniversalLinkDomain: func() string {
				if mobileConfig != nil {
					return mobileConfig.UniversalLinkDomain
				}
				return ""
			}(),
		}
		taskPayload, err := json.Marshal(androidBuildPayload)
		if err != nil {
			c.cancelCreatedTasks(createdTaskIDs)
			return nil, err
		}
		task := asynq.NewTask(tasks.TypeBuildAndroid, taskPayload)
		if _, err := c.TaskService.Enqueue(task, asynq.MaxRetry(2), asynq.Timeout(time.Minute*10)); err != nil {
			c.cancelCreatedTasks(createdTaskIDs)
			return nil, fmt.Errorf("failed to enqueue Android build task: %w", err)
		}
	}

	if project.BuildForIOS {
		appBuildTask, err := c.DB.CreateAppBuildTask(*createdBuild, "IOS", "Queued", project.UseAutoDeployForIOS)
		if err != nil {
			c.cancelCreatedTasks(createdTaskIDs)
			return nil, err
		}
		createdTaskIDs = append(createdTaskIDs, appBuildTask.ID.String())

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
			Permissions: func() []string {
				if mobileConfig != nil {
					return mobileConfig.Permissions
				}
				return nil
			}(),
			URLScheme: func() string {
				if mobileConfig != nil {
					return mobileConfig.URLScheme
				}
				return ""
			}(),
			UniversalLinkDomain: func() string {
				if mobileConfig != nil {
					return mobileConfig.UniversalLinkDomain
				}
				return ""
			}(),
		}
		taskPayload, err := json.Marshal(iosBuildPayload)
		if err != nil {
			c.cancelCreatedTasks(createdTaskIDs)
			return nil, err
		}
		task := asynq.NewTask(tasks.TypeBuildUnsignedIpa, taskPayload)
		if _, err := c.TaskService.Enqueue(task, asynq.MaxRetry(2), asynq.Timeout(time.Minute*10)); err != nil {
			c.cancelCreatedTasks(createdTaskIDs)
			return nil, fmt.Errorf("failed to enqueue iOS build task: %w", err)
		}
	}

	return &responses.BuildCreateResponse{
		SuccessResponse: responses.SuccessResponse{Success: true},
	}, nil
}

// cancelCreatedTasks cancels build tasks that were created before a failure occurred.
func (c *Controller) cancelCreatedTasks(taskIDs []string) {
	for _, id := range taskIDs {
		_ = c.DB.CancelAppBuildTask(id)
	}
}
