package controllers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/tasks"
	"github.com/knid/wemob/internal/utils/external"
	"github.com/slack-go/slack"
)

func (c *Controller) GetDeployments(user *models.User) ([]responses.DeploymentResponse, error) {
	deployments, err := c.DB.GetDeployments(user)
	if err != nil {
		return nil, err
	}

	var deploymentResponses []responses.DeploymentResponse
	for _, deployment := range deployments {
		deploymentResponses = append(deploymentResponses, responses.DeploymentResponse{
			ID:                      deployment.ID,
			UserID:                  deployment.UserID,
			ProjectID:               deployment.ProjectID,
			BuildID:                 deployment.BuildID,
			Platforms:               deployment.Platforms,
			IconID:                  deployment.IconPath,
			Title:                   deployment.Title,
			Description:             deployment.Description,
			Status:                  deployment.Status,
			ScreenshotIDs:           deployment.ScreenshotPaths,
			Method:                  deployment.Method,
			AppStoreCredentialsID:   deployment.AppStoreCredentialsID,
			GooglePlayCredentialsID: deployment.GooglePlayCredentialsID,
			CreatedAt:               deployment.CreatedAt,
			UpdatedAt:               deployment.UpdatedAt,
		})
	}

	return deploymentResponses, nil
}

func (c *Controller) GetDeploymentByID(deploymentID string) (*responses.DeploymentResponse, error) {
	deployment, err := c.DB.GetDeploymentByID(deploymentID)
	if err != nil {
		return nil, err
	}

	return &responses.DeploymentResponse{
		ID:                      deployment.ID,
		UserID:                  deployment.UserID,
		ProjectID:               deployment.ProjectID,
		BuildID:                 deployment.BuildID,
		Platforms:               deployment.Platforms,
		IconID:                  deployment.IconPath,
		Title:                   deployment.Title,
		Description:             deployment.Description,
		Status:                  deployment.Status,
		ScreenshotIDs:           deployment.ScreenshotPaths,
		Method:                  deployment.Method,
		AppStoreCredentialsID:   deployment.AppStoreCredentialsID,
		GooglePlayCredentialsID: deployment.GooglePlayCredentialsID,
		CreatedAt:               deployment.CreatedAt,
		UpdatedAt:               deployment.UpdatedAt,
	}, nil
}

func (c *Controller) CreateDeploymentTask(user *models.User, deploymentReq requests.CreateDeploymentRequest) (*responses.DeploymentResponse, error) {
	// TODO:
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := validate.Struct(deploymentReq); err != nil {
		return nil, err
	}

	deployments, err := c.GetDeployments(user)
	if err != nil {
		return nil, err
	}

	for _, d := range deployments {
		if d.ProjectID.String() == deploymentReq.ProjectID && d.Method == "wemob" {
			return nil, errors.New("deployment for this project and platform already exists")
		}
		if d.ProjectID.String() == deploymentReq.ProjectID && d.Method == "user" {
			for _, p := range d.Platforms {
				if slices.Contains(deploymentReq.Platforms, p) {
					return nil, errors.New("deployment for this project and platform already exists")
				}
			}
		}
	}

	project, err := c.DB.GetProjectById(deploymentReq.ProjectID)
	if err != nil {
		return nil, err
	}

	if project.UserID.String() != user.ID.String() {
		return nil, errors.New("you do not have access this resources")
	}

	build, err := c.DB.GetProjectBuildById(deploymentReq.BuildID)
	if err != nil {
		return nil, err
	}

	appStoreCredId, err := uuid.Parse(deploymentReq.AppStoreCredentialsID)
	if err != nil {
		if deploymentReq.Method == "wemob" {
			appStoreCredId = uuid.Nil
		} else {
			return nil, errors.New("invalid app store credentials id")
		}
	}
	googlePlayCredId, err := uuid.Parse(deploymentReq.GooglePlayCredentialsID)
	if err != nil {
		if deploymentReq.Method == "wemob" {
			googlePlayCredId = uuid.Nil
		} else {
			return nil, errors.New("invalid google play credentials id")
		}
	}

	deployment, err := c.DB.CreateDeployment(&models.Deployment{
		UserID:                  user.ID,
		ProjectID:               project.ID,
		BuildID:                 build.ID,
		Platforms:               deploymentReq.Platforms,
		IconPath:                deploymentReq.IconID,
		Title:                   deploymentReq.Title,
		Description:             deploymentReq.Description,
		Locale:                  deploymentReq.Locale,
		AppStoreCredentialsID:   appStoreCredId,
		GooglePlayCredentialsID: googlePlayCredId,
		Status:                  "Queued",
		Method:                  deploymentReq.Method,
		ScreenshotPaths:         deploymentReq.ScreenshotPaths,
	})
	if err != nil {
		return nil, err
	}

	var taskPayload []byte
	var task *asynq.Task
	var taskType string

	if deploymentReq.Method == "user" {
		if slices.Contains(deploymentReq.Platforms, "ios") {
			cred, err := c.DB.GetAppStoreCredentialById(appStoreCredId.String())
			if err != nil {
				return nil, err
			}

			// Create and enqueue task to deploy iOS build
			iosDeployPayload := tasks.IOSDeployPayload{
				BuildID:            deploymentReq.BuildID,
				PackageName:        project.PackageName,
				TeamID:             cred.TeamID,
				KeyID:              cred.KeyID,
				IssuerID:           cred.IssuerID,
				P8KeyPath:          cred.P8KeyPath,
				Title:              deploymentReq.Title,
				Description:        deploymentReq.Description,
				Screenshots:        deploymentReq.ScreenshotPaths,
				Icon:               build.Icon,
				Locale:             deploymentReq.Locale,
				NewApp:             true,
				ChangedStoreInfo:   true,
				ChangedBuild:       true,
				TitleChanged:       true,
				DescriptionChanged: true,
				ScreenshotsChanged: true,
				DeploymentID:       deployment.ID.String(),
			}

			taskType = tasks.TypeDeployIOS
			taskPayload, err = json.Marshal(iosDeployPayload)
			if err != nil {
				return nil, err
			}

			// Get APNS P8 Key content from s3
			apnsP8KeyBytes, err := c.UserApiKeysStorage.ReadObjectBytes(context.TODO(), cred.APNSP8KeyPath)
			if err != nil {
				return nil, err
			}
			apnsP8Key := base64.StdEncoding.EncodeToString(apnsP8KeyBytes)

			// Upload APNS P8 Key to OneSignal
			err = external.UploadAPNSP8KeyToOneSignal(*project, apnsP8Key, cred.APNSP8KeyID, cred.TeamID, project.PackageName, c.SecretManager.GetSecret("ONESIGNAL_ORG_ID"), c.SecretManager.GetSecret("ONESIGNAL_ORG_API_KEY"))
			if err != nil {
				return nil, err
			}

			go c.CRMService.SendEvent(user, constants.BREVO_EVENT_USER_APPLE_DEPLOYMENT_STARTED, map[string]interface{}{
				"project_id":       project.ID.String(),
				"deployment_id":    deployment.ID.String(),
				"build_id":         build.ID.String(),
				"deployment_title": deploymentReq.Title,
			})
		}
		if slices.Contains(deploymentReq.Platforms, "android") {
			cred, err := c.DB.GetGooglePlayCredentialById(googlePlayCredId.String())
			if err != nil {
				return nil, err
			}
			androidDeployPayload := tasks.AndroidDeployPayload{
				ServiceAccountPath: cred.ServiceAccountPath,
				BuildID:            deploymentReq.BuildID,
				PackageName:        project.PackageName,
				Title:              deploymentReq.Title,
				Description:        deploymentReq.Description,
				IconPath:           deploymentReq.IconID,
				Screenshots:        deploymentReq.ScreenshotPaths,
				Version:            build.BuildNumber,
				Locale:             deploymentReq.Locale,
				ChangedStoreInfo:   true,
				ChangedBuild:       true,
				TitleChanged:       true,
				DescriptionChanged: true,
				ScreenshotsChanged: true,
				Secret:             deployment.ID.String(),
				DeploymentID:       deployment.ID.String(),
			}

			taskType = tasks.TypeDeployAndroid //
			taskPayload, err = json.Marshal(androidDeployPayload)
			if err != nil {
				return nil, err
			}
			go c.CRMService.SendEvent(user, constants.BREVO_EVENT_USER_ANDROID_DEPLOYMENT_STARTED, map[string]interface{}{
				"project_id":       project.ID.String(),
				"deployment_id":    deployment.ID.String(),
				"build_id":         build.ID.String(),
				"deployment_title": deploymentReq.Title,
			})
		} else {
			return nil, errors.New("unsupported platform")
		}

		task = asynq.NewTask(taskType, taskPayload)
		_, err = c.TaskService.Enqueue(task, asynq.MaxRetry(2), asynq.Timeout(time.Minute*3))
		if err != nil {
			return nil, err
		}

	} else if deploymentReq.Method == "wemob" {
		go c.SlackService.Client.PostMessage(
			constants.SLACK_CHANNEL_ID_NEW_ANDROID_DEPLOY_REQUEST,
			slack.MsgOptionText("New android deploy request: ("+project.URL+") "+deployment.ID.String(), false),
		)
	}

	return &responses.DeploymentResponse{
		ID:                      deployment.ID,
		UserID:                  deployment.UserID,
		ProjectID:               deployment.ProjectID,
		BuildID:                 deployment.BuildID,
		Platforms:               deployment.Platforms,
		IconID:                  deployment.IconPath,
		Title:                   deployment.Title,
		Description:             deployment.Description,
		Status:                  deployment.Status,
		ScreenshotIDs:           deployment.ScreenshotPaths,
		Method:                  deployment.Method,
		Locale:                  deployment.Locale,
		AppStoreCredentialsID:   deployment.AppStoreCredentialsID,
		GooglePlayCredentialsID: deployment.GooglePlayCredentialsID,
		CreatedAt:               deployment.CreatedAt,
		UpdatedAt:               deployment.UpdatedAt,
	}, nil
}

func (c *Controller) UpdateDeploymentTask(user *models.User, deploymentID string, deploymentReq requests.CreateDeploymentRequest) (*responses.DeploymentResponse, error) {
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := validate.Struct(deploymentReq); err != nil {
		return nil, err
	}

	project, err := c.DB.GetProjectById(deploymentReq.ProjectID)
	if err != nil {
		return nil, err
	}

	if project.UserID.String() != user.ID.String() {
		return nil, errors.New("you do not have access this resources")
	}

	build, err := c.DB.GetProjectBuildById(deploymentReq.BuildID)
	if err != nil {
		return nil, err
	}

	deployment, err := c.DB.GetDeploymentByID(deploymentID)
	if err != nil {
		return nil, err
	}

	if deployment.UserID.String() != user.ID.String() {
		return nil, errors.New("you do not have access this resources")
	}

	var titleChanged bool
	var descriptionChanged bool
	var screenshotsChanged bool
	var buildChanged bool
	var changedStoreInfo bool

	if deployment.Title != deploymentReq.Title {
		titleChanged = true
		changedStoreInfo = true
	}

	if deployment.Description != deploymentReq.Description {
		descriptionChanged = true
		changedStoreInfo = true
	}

	if !slices.Equal(deployment.ScreenshotPaths, deploymentReq.ScreenshotPaths) {
		screenshotsChanged = true
		changedStoreInfo = true
	}

	if deployment.BuildID.String() != deploymentReq.BuildID {
		buildChanged = true
	}

	if deployment.IconPath != deploymentReq.IconID {
		changedStoreInfo = true
	}

	deployment.Title = deploymentReq.Title
	deployment.Description = deploymentReq.Description
	deployment.IconPath = deploymentReq.IconID
	deployment.ScreenshotPaths = deploymentReq.ScreenshotPaths

	deployment.Status = "Queued"

	updatedDeployment, err := c.DB.UpdateDeployment(deployment)
	if err != nil {
		return nil, err
	}

	appStoreCredId, err := uuid.Parse(deploymentReq.AppStoreCredentialsID)
	if err != nil {
		if deploymentReq.Method == "wemob" {
			appStoreCredId = uuid.Nil
		} else {
			return nil, errors.New("invalid app store credentials id")
		}
	}
	googlePlayCredId, err := uuid.Parse(deploymentReq.GooglePlayCredentialsID)
	if err != nil {
		if deploymentReq.Method == "wemob" {
			googlePlayCredId = uuid.Nil
		} else {
			return nil, errors.New("invalid google play credentials id")
		}
	}

	var taskPayload []byte
	var task *asynq.Task
	var taskType string

	if slices.Contains(deployment.Platforms, "ios") {
		cred, err := c.DB.GetAppStoreCredentialById(appStoreCredId.String())
		if err != nil {
			return nil, err
		}

		// Create and enqueue task to deploy iOS build
		iosDeployPayload := tasks.IOSDeployPayload{
			BuildID:            deploymentReq.BuildID,
			PackageName:        project.PackageName,
			KeyID:              cred.KeyID,
			IssuerID:           cred.IssuerID,
			P8KeyPath:          cred.P8KeyPath,
			Title:              deploymentReq.Title,
			Description:        deploymentReq.Description,
			Screenshots:        deploymentReq.ScreenshotPaths,
			Icon:               build.Icon,
			NewApp:             false,
			ChangedStoreInfo:   changedStoreInfo,
			ChangedBuild:       buildChanged,
			TitleChanged:       titleChanged,
			DescriptionChanged: descriptionChanged,
			ScreenshotsChanged: screenshotsChanged,
			DeploymentID:       deployment.ID.String(),
		}

		taskType = tasks.TypeDeployIOS
		taskPayload, err = json.Marshal(iosDeployPayload)
		if err != nil {
			return nil, err
		}

	} else if slices.Contains(deploymentReq.Platforms, "android") {
		cred, err := c.DB.GetGooglePlayCredentialById(googlePlayCredId.String())
		if err != nil {
			return nil, err
		}
		androidDeployPayload := tasks.AndroidDeployPayload{
			ServiceAccountPath: cred.ServiceAccountPath,
			BuildID:            deploymentReq.BuildID,
			PackageName:        project.PackageName,
			Title:              deploymentReq.Title,
			Description:        deploymentReq.Description,
			IconPath:           deploymentReq.IconID,
			Screenshots:        deploymentReq.ScreenshotPaths,
			Version:            build.BuildNumber,
			ChangedStoreInfo:   changedStoreInfo,
			ChangedBuild:       buildChanged,
			TitleChanged:       titleChanged,
			DescriptionChanged: descriptionChanged,
			ScreenshotsChanged: screenshotsChanged,
			Secret:             deployment.ID.String(),
			DeploymentID:       deployment.ID.String(),
		}

		taskType = tasks.TypeDeployAndroid //
		taskPayload, err = json.Marshal(androidDeployPayload)
		if err != nil {
			return nil, err
		}
	} else {
		return nil, errors.New("unsupported platform")
	}

	task = asynq.NewTask(taskType, taskPayload)
	_, err = c.TaskService.Enqueue(task, asynq.MaxRetry(2), asynq.Timeout(time.Minute*3))
	if err != nil {
		return nil, err
	}

	return &responses.DeploymentResponse{
		ID:                      updatedDeployment.ID,
		UserID:                  updatedDeployment.UserID,
		ProjectID:               updatedDeployment.ProjectID,
		BuildID:                 updatedDeployment.BuildID,
		Platforms:               updatedDeployment.Platforms,
		IconID:                  updatedDeployment.IconPath,
		Title:                   updatedDeployment.Title,
		Description:             updatedDeployment.Description,
		Status:                  updatedDeployment.Status,
		ScreenshotIDs:           updatedDeployment.ScreenshotPaths,
		Method:                  updatedDeployment.Method,
		AppStoreCredentialsID:   updatedDeployment.AppStoreCredentialsID,
		GooglePlayCredentialsID: updatedDeployment.GooglePlayCredentialsID,
		CreatedAt:               updatedDeployment.CreatedAt,
		UpdatedAt:               updatedDeployment.UpdatedAt,
	}, nil
}

func (c *Controller) DeleteDeploymentTask(deploymentID string) error {
	return nil
}

func (c *Controller) SignUnsignedIPATask(user *models.User, signReq requests.SignIPARequest) (*models.SignIPATask, error) {
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := validate.Struct(signReq); err != nil {
		return nil, err
	}

	project, err := c.DB.GetProjectById(signReq.ProjectID)
	if err != nil {
		return nil, err
	}

	if project.UserID.String() != user.ID.String() {
		return nil, errors.New("you do not have access this resources")
	}

	build, err := c.DB.GetProjectBuildById(signReq.BuildID)
	if err != nil {
		return nil, err
	}

	cred, err := c.DB.GetAppStoreCredentialById(signReq.CredentialsID)
	if err != nil {
		return nil, err
	}

	iosSignTask, err := c.DB.CreateIOSSignTask(models.SignIPATask{
		ProjectID: project.ID,
		BuildID:   build.ID,
		Status:    "signing",
		Finished:  false,
	})
	if err != nil {
		return nil, err
	}

	iosSignPayload := tasks.IOSSignPayload{
		BuildID:     build.ID.String(),
		PackageName: project.PackageName,
		KeyID:       cred.KeyID,
		TeamID:      cred.TeamID,
		IssuerID:    cred.IssuerID,
		P8KeyPath:   cred.P8KeyPath,
		Secret:      iosSignTask.ID.String(),
		TaskID:      iosSignTask.ID.String(),
	}

	signTaskType := tasks.TypeSignIOS
	signTaskPayload, err := json.Marshal(iosSignPayload)
	if err != nil {
		return nil, err
	}

	signTask := asynq.NewTask(signTaskType, signTaskPayload)
	_, err = c.TaskService.Enqueue(signTask, asynq.MaxRetry(1), asynq.Timeout(time.Minute*3))
	if err != nil {
		return nil, err
	}

	return iosSignTask, nil

}
