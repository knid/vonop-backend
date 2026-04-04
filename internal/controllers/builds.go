package controllers

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/services/codemagic"
	"github.com/knid/wemob/internal/utils"
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

func (c *Controller) CreateBuild(user *models.User, buildCreateReq requests.BuildCreateRequest) (*responses.BuildCreateResponse, error) {
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := buildCreateReq.Validate(validate); err != nil {
		return nil, err
	}

	if c.CodemagicService == nil {
		return nil, errors.New("build service not configured")
	}

	// Enforce brand logo removal permission at controller level
	if buildCreateReq.RemoveWemobBranding {
		limits, limitsErr := c.GetUserLimits(user)
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
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("failed to get last build: %w", err)
		}
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

	// Helper to extract config values safely
	permissions := getPermissions(mobileConfig)
	urlScheme := getURLScheme(mobileConfig)
	universalLink := getUniversalLink(mobileConfig)

	// Track created tasks for rollback on failure
	var createdTaskIDs []string

	if project.BuildForAndroid {
		appBuildTask, err := c.DB.CreateAppBuildTask(*createdBuild, "Android", "Queued", project.UseAutoDeployForAndroid)
		if err != nil {
			return nil, err
		}
		createdTaskIDs = append(createdTaskIDs, appBuildTask.ID.String())

		if _, err := c.CodemagicService.TriggerAndroidBuild(codemagic.AndroidBuildParams{
			ProjectID:           project.ID.String(),
			BuildID:             createdBuild.ID.String(),
			TaskID:              appBuildTask.ID.String(),
			Version:             createdBuild.BuildNumber,
			Title:               createdBuild.Title,
			URL:                 project.URL,
			IconURL:             createdBuild.Icon,
			SplashColor:         createdBuild.SplashScreenColor,
			OneSignalAppID:      project.OneSignalAppID,
			PackageName:         project.PackageName,
			RemoveBranding:      createdBuild.RemoveWemobBranding,
			Permissions:         permissions,
			URLScheme:           urlScheme,
			UniversalLinkDomain: universalLink,
		}); err != nil {
			c.cancelCreatedTasks(createdTaskIDs)
			return nil, fmt.Errorf("failed to trigger Android build: %w", err)
		}
	}

	if project.BuildForIOS {
		appBuildTask, err := c.DB.CreateAppBuildTask(*createdBuild, "IOS", "Queued", project.UseAutoDeployForIOS)
		if err != nil {
			c.cancelCreatedTasks(createdTaskIDs)
			return nil, err
		}
		createdTaskIDs = append(createdTaskIDs, appBuildTask.ID.String())

		if _, err := c.CodemagicService.TriggerIOSBuild(codemagic.IOSBuildParams{
			ProjectID:           project.ID.String(),
			BuildID:             createdBuild.ID.String(),
			TaskID:              appBuildTask.ID.String(),
			Version:             createdBuild.BuildNumber,
			Title:               createdBuild.Title,
			URL:                 project.URL,
			IconURL:             createdBuild.Icon,
			SplashColor:         createdBuild.SplashScreenColor,
			OneSignalAppID:      project.OneSignalAppID,
			PackageName:         project.PackageName,
			RemoveBranding:      createdBuild.RemoveWemobBranding,
			Permissions:         permissions,
			URLScheme:           urlScheme,
			UniversalLinkDomain: universalLink,
		}); err != nil {
			c.cancelCreatedTasks(createdTaskIDs)
			return nil, fmt.Errorf("failed to trigger iOS build: %w", err)
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

// Config extraction helpers to avoid repeated nil checks.

func getPermissions(cfg *models.MobileAppConfig) []string {
	if cfg != nil {
		return cfg.Permissions
	}
	return nil
}

func getURLScheme(cfg *models.MobileAppConfig) string {
	if cfg != nil {
		return cfg.URLScheme
	}
	return ""
}

func getUniversalLink(cfg *models.MobileAppConfig) string {
	if cfg != nil {
		return cfg.UniversalLinkDomain
	}
	return ""
}
