package controllers

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/services/codemagic"
	"github.com/knid/wemob/internal/services/storeauth"
	"github.com/knid/wemob/internal/utils/external"
	"github.com/slack-go/slack"
)

// ErrPlayAppSigningRequired is returned when an Android first-publish is
// attempted but Play App Signing is not yet enrolled for the package name.
var ErrPlayAppSigningRequired = errors.New("Play App Signing must be enabled before first publish — see https://play.google.com/console (Setup → App integrity → Play App Signing)")

var localeRegexp = regexp.MustCompile(`^[a-zA-Z]{2}(-[a-zA-Z]{2,4})?$`)

// isValidLocale checks that a locale string matches common BCP 47 / POSIX formats (e.g. en-US, tr_TR, pt).
func isValidLocale(locale string) bool {
	return localeRegexp.MatchString(locale)
}

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
			AppleConnectionID:       deployment.AppleConnectionID,
			GooglePlayConnectionID:  deployment.GooglePlayConnectionID,
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
		AppleConnectionID:       deployment.AppleConnectionID,
		GooglePlayConnectionID:  deployment.GooglePlayConnectionID,
		CreatedAt:               deployment.CreatedAt,
		UpdatedAt:               deployment.UpdatedAt,
	}, nil
}

func (c *Controller) CreateDeploymentTask(user *models.User, orgID uuid.UUID, deploymentReq requests.CreateDeploymentRequest) (*responses.DeploymentResponse, error) {
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := validate.Struct(deploymentReq); err != nil {
		return nil, err
	}

	deployments, err := c.GetDeployments(user)
	if err != nil {
		return nil, err
	}

	for _, d := range deployments {
		if d.ProjectID.String() != deploymentReq.ProjectID {
			continue
		}
		for _, p := range d.Platforms {
			if slices.Contains(deploymentReq.Platforms, p) {
				return nil, fmt.Errorf("deployment for this project on %s already exists", p)
			}
		}
	}

	project, err := c.DB.GetProjectById(deploymentReq.ProjectID)
	if err != nil {
		return nil, err
	}

	if project.UserID.String() != user.ID.String() {
		return nil, errors.New("you do not have access to this resource")
	}

	build, err := c.DB.GetProjectBuildById(deploymentReq.BuildID)
	if err != nil {
		return nil, err
	}

	if deploymentReq.Locale != "" && !isValidLocale(deploymentReq.Locale) {
		return nil, errors.New("invalid locale format, expected format like en-US or en_US")
	}

	appStoreCredId, err := uuid.Parse(deploymentReq.AppleConnectionID)
	if err != nil {
		if deploymentReq.Method == "wemob" || deploymentReq.DeferConnection {
			appStoreCredId = uuid.Nil
		} else {
			if slices.Contains(deploymentReq.Platforms, "ios") {
				return nil, errors.New("invalid app store credentials id")
			}
		}
	}
	googlePlayCredId, err := uuid.Parse(deploymentReq.GooglePlayConnectionID)
	if err != nil {
		if deploymentReq.Method == "wemob" || deploymentReq.DeferConnection {
			googlePlayCredId = uuid.Nil
		} else {
			if slices.Contains(deploymentReq.Platforms, "android") {
				return nil, errors.New("invalid google play credentials id")
			}
		}
	}

	initialStatus := "Queued"
	if deploymentReq.DeferConnection && deploymentReq.Method == "user" {
		initialStatus = "pending_connection"
	}

	deployment, err := c.DB.CreateDeployment(&models.Deployment{
		UserID:                 user.ID,
		OrganizationID:         orgID,
		ProjectID:              project.ID,
		BuildID:                build.ID,
		Platforms:              deploymentReq.Platforms,
		IconPath:               deploymentReq.IconID,
		Title:                  deploymentReq.Title,
		Description:            deploymentReq.Description,
		Locale:                 deploymentReq.Locale,
		AppleConnectionID:      appStoreCredId,
		GooglePlayConnectionID: googlePlayCredId,
		Status:                 initialStatus,
		Method:                 deploymentReq.Method,
		ScreenshotPaths:        deploymentReq.ScreenshotPaths,
		ReleaseNotes:           deploymentReq.ReleaseNotes,
		RolloutTrack:           deploymentReq.RolloutTrack,
	})
	if err != nil {
		return nil, err
	}

	// Phase 5a: emit submitted event.
	actorID := user.ID
	submittedMeta := map[string]interface{}{"method": deployment.Method}
	if initialStatus == "pending_connection" {
		submittedMeta["pending_connection"] = true
	}
	if err := database.AppendDeploymentEvent(context.Background(), c.DB.DB, deployment.ID,
		models.DeploymentEventSubmitted, models.DeploymentEventActorUser, &actorID,
		submittedMeta); err != nil {
		slog.Warn("append deployment_events.submitted failed", "err", err)
	}

	if deploymentReq.Method == "user" && !deploymentReq.DeferConnection {
		hasIOS := slices.Contains(deploymentReq.Platforms, "ios")
		hasAndroid := slices.Contains(deploymentReq.Platforms, "android")

		if !hasIOS && !hasAndroid {
			return nil, errors.New("unsupported platform")
		}

		if hasIOS {
			cred, err := c.DB.GetAppleConnectionById(appStoreCredId.String())
			if err != nil {
				return nil, err
			}

			if _, err := c.CodemagicService.TriggerIOSDeploy(codemagic.IOSDeployParams{
				BuildID:            deploymentReq.BuildID,
				DeploymentID:       deployment.ID.String(),
				PackageName:        project.PackageName,
				TeamID:             cred.TeamID,
				KeyID:              cred.KeyID,
				IssuerID:           cred.IssuerID,
				P8KeyS3Path:        cred.P8KeyPath,
				Title:              deploymentReq.Title,
				Description:        deploymentReq.Description,
				Screenshots:        deploymentReq.ScreenshotPaths,
				Icon:               build.Icon,
				Locale:             deploymentReq.Locale,
				NewApp:             true,
				ChangedBuild:       true,
				TitleChanged:       true,
				DescChanged:        true,
				ScreenshotsChanged: true,
				RolloutTrack:       deploymentReq.RolloutTrack,
				ReleaseNotes:       deploymentReq.ReleaseNotes,
			}); err != nil {
				return nil, fmt.Errorf("failed to trigger iOS deploy: %w", err)
			}

			go c.EventService.SendEvent(user, constants.EVENT_USER_APPLE_DEPLOYMENT_STARTED, map[string]interface{}{
				"project_id":       project.ID.String(),
				"deployment_id":    deployment.ID.String(),
				"build_id":         build.ID.String(),
				"deployment_title": deploymentReq.Title,
			})
		}

		if hasAndroid {
			cred, err := c.DB.GetGooglePlayConnectionById(googlePlayCredId.String())
			if err != nil {
				return nil, err
			}

			// First-publish gate: verify Play App Signing is enrolled before
			// sending to Codemagic. A missing enrolment causes silent failures
			// mid-pipeline; surfacing it here gives the user a clear error.
			if err := c.RequirePlayAppSigning(context.Background(), cred, project.PackageName); err != nil {
				return nil, err
			}

			if _, err := c.CodemagicService.TriggerAndroidDeploy(codemagic.AndroidDeployParams{
				BuildID:              deploymentReq.BuildID,
				DeploymentID:         deployment.ID.String(),
				PackageName:          project.PackageName,
				Version:              build.BuildNumber,
				Title:                deploymentReq.Title,
				Description:          deploymentReq.Description,
				Screenshots:          deploymentReq.ScreenshotPaths,
				IconPath:             deploymentReq.IconID,
				Locale:               deploymentReq.Locale,
				ServiceAccountS3Path: cred.ServiceAccountPath,
				NewApp:               true,
				ChangedBuild:         true,
				TitleChanged:         true,
				DescChanged:          true,
				ScreenshotsChanged:   true,
				IconChanged:          true,
			}); err != nil {
				return nil, fmt.Errorf("failed to trigger Android deploy: %w", err)
			}

			go c.EventService.SendEvent(user, constants.EVENT_USER_ANDROID_DEPLOYMENT_STARTED, map[string]interface{}{
				"project_id":       project.ID.String(),
				"deployment_id":    deployment.ID.String(),
				"build_id":         build.ID.String(),
				"deployment_title": deploymentReq.Title,
			})
		}

	} else if deploymentReq.Method == "wemob" {
		go c.SlackService.Client.PostMessage(
			constants.SLACK_CHANNEL_ID_NEW_ANDROID_DEPLOY_REQUEST,
			slack.MsgOptionText("New deploy request: ("+project.URL+") "+deployment.ID.String()+" platforms: "+fmt.Sprintf("%v", deploymentReq.Platforms), false),
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
		AppleConnectionID:       deployment.AppleConnectionID,
		GooglePlayConnectionID:  deployment.GooglePlayConnectionID,
		CreatedAt:               deployment.CreatedAt,
		UpdatedAt:               deployment.UpdatedAt,
	}, nil
}

func (c *Controller) UpdateDeploymentTask(user *models.User, deploymentID string, deploymentReq requests.UpdateDeploymentRequest) (*responses.DeploymentResponse, error) {
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := validate.Struct(deploymentReq); err != nil {
		return nil, err
	}

	project, err := c.DB.GetProjectById(deploymentReq.ProjectID)
	if err != nil {
		return nil, err
	}

	if project.UserID.String() != user.ID.String() {
		return nil, errors.New("you do not have access to this resource")
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
		return nil, errors.New("you do not have access to this resource")
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
	deployment.RolloutTrack = deploymentReq.RolloutTrack
	deployment.ReleaseNotes = deploymentReq.ReleaseNotes
	deployment.IconPath = deploymentReq.IconID
	deployment.ScreenshotPaths = deploymentReq.ScreenshotPaths
	if deploymentReq.BuildID != "" {
		deployment.BuildID = uuid.MustParse(deploymentReq.BuildID)
	}
	if deploymentReq.Locale != "" {
		deployment.Locale = deploymentReq.Locale
	}

	deployment.Status = "Queued"

	updatedDeployment, err := c.DB.UpdateDeployment(deployment)
	if err != nil {
		return nil, err
	}

	appStoreCredId, err := uuid.Parse(deploymentReq.AppleConnectionID)
	if err != nil {
		if deploymentReq.Method == "wemob" {
			appStoreCredId = uuid.Nil
		} else {
			return nil, errors.New("invalid app store credentials id")
		}
	}
	googlePlayCredId, err := uuid.Parse(deploymentReq.GooglePlayConnectionID)
	if err != nil {
		if deploymentReq.Method == "wemob" {
			googlePlayCredId = uuid.Nil
		} else {
			return nil, errors.New("invalid google play credentials id")
		}
	}

	hasIOS := slices.Contains(deployment.Platforms, "ios")
	hasAndroid := slices.Contains(deployment.Platforms, "android")

	if !hasIOS && !hasAndroid {
		return nil, errors.New("unsupported platform")
	}

	if hasIOS {
		cred, err := c.DB.GetAppleConnectionById(appStoreCredId.String())
		if err != nil {
			return nil, err
		}

		if _, err := c.CodemagicService.TriggerIOSDeploy(codemagic.IOSDeployParams{
			BuildID:            deploymentReq.BuildID,
			DeploymentID:       deployment.ID.String(),
			PackageName:        project.PackageName,
			TeamID:             cred.TeamID,
			KeyID:              cred.KeyID,
			IssuerID:           cred.IssuerID,
			P8KeyS3Path:        cred.P8KeyPath,
			Title:              deploymentReq.Title,
			Description:        deploymentReq.Description,
			Screenshots:        deploymentReq.ScreenshotPaths,
			Icon:               build.Icon,
			Locale:             deployment.Locale,
			NewApp:             false,
			ChangedBuild:       buildChanged,
			TitleChanged:       titleChanged,
			DescChanged:        descriptionChanged,
			ScreenshotsChanged: screenshotsChanged,
			RolloutTrack:       deploymentReq.RolloutTrack,
			ReleaseNotes:       deploymentReq.ReleaseNotes,
		}); err != nil {
			return nil, fmt.Errorf("failed to trigger iOS deploy: %w", err)
		}
	}

	if hasAndroid {
		cred, err := c.DB.GetGooglePlayConnectionById(googlePlayCredId.String())
		if err != nil {
			return nil, err
		}

		if _, err := c.CodemagicService.TriggerAndroidDeploy(codemagic.AndroidDeployParams{
			BuildID:              deploymentReq.BuildID,
			DeploymentID:         deployment.ID.String(),
			PackageName:          project.PackageName,
			Version:              build.BuildNumber,
			Title:                deploymentReq.Title,
			Description:          deploymentReq.Description,
			Screenshots:          deploymentReq.ScreenshotPaths,
			IconPath:             deploymentReq.IconID,
			Locale:               deployment.Locale,
			ServiceAccountS3Path: cred.ServiceAccountPath,
			NewApp:               false,
			ChangedBuild:         buildChanged,
			TitleChanged:         titleChanged,
			DescChanged:          descriptionChanged,
			ScreenshotsChanged:   screenshotsChanged,
			IconChanged:          changedStoreInfo,
		}); err != nil {
			return nil, fmt.Errorf("failed to trigger Android deploy: %w", err)
		}
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
		AppleConnectionID:       updatedDeployment.AppleConnectionID,
		GooglePlayConnectionID:  updatedDeployment.GooglePlayConnectionID,
		CreatedAt:               updatedDeployment.CreatedAt,
		UpdatedAt:               updatedDeployment.UpdatedAt,
	}, nil
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
		return nil, errors.New("you do not have access to this resource")
	}

	build, err := c.DB.GetProjectBuildById(signReq.BuildID)
	if err != nil {
		return nil, err
	}

	cred, err := c.DB.GetAppleConnectionById(signReq.CredentialsID)
	if err != nil {
		return nil, err
	}

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

	iosSignTask, err := c.DB.CreateIOSSignTask(models.SignIPATask{
		ProjectID: project.ID,
		BuildID:   build.ID,
		Status:    "signing",
		Finished:  false,
	})
	if err != nil {
		return nil, err
	}

	if _, err := c.CodemagicService.TriggerIOSSign(codemagic.IOSSignParams{
		BuildID:     build.ID.String(),
		TaskID:      iosSignTask.ID.String(),
		PackageName: project.PackageName,
		TeamID:      cred.TeamID,
		KeyID:       cred.KeyID,
		IssuerID:    cred.IssuerID,
		P8KeyS3Path: cred.P8KeyPath,
	}); err != nil {
		return nil, fmt.Errorf("failed to trigger iOS sign: %w", err)
	}

	return iosSignTask, nil
}

// AutoSignIOSBuildIfPossible is called from the build-completed webhook.
// If the completed build is iOS and the project has a DefaultAppleConnectionID
// set, it enqueues a sign task using that connection. Returns (false, nil) when
// no connection is configured — the UI will show 'Built (unsigned)' and prompt
// the user to configure one in project settings.
//
// Returns (true, nil) when a sign task was enqueued.
func (c *Controller) AutoSignIOSBuildIfPossible(ctx context.Context, task *models.AppBuildTask) (bool, error) {
	if task.Platform != "ios" {
		return false, nil
	}
	// Load the AppProject to check for a configured default connection.
	appProject, err := c.DB.GetAppProjectByID(task.ProjectID.String())
	if err != nil {
		return false, err
	}
	if appProject.DefaultAppleConnectionID == nil {
		// No connection configured — graceful exit.
		return false, nil
	}

	user, err := c.DB.GetUserById(appProject.UserID.String())
	if err != nil {
		return false, err
	}

	// Verify the connection belongs to the project's org before using it.
	conn, err := c.DB.GetAppleConnectionByOrg(ctx, appProject.OrganizationID, *appProject.DefaultAppleConnectionID)
	if err != nil {
		// Connection was deleted or moved to a different org — treat as not configured.
		return false, nil
	}

	// Idempotency guard: if a non-failed sign task already exists for this
	// build (e.g., the Codemagic webhook fired twice), don't create a duplicate.
	existing, existsErr := c.DB.GetLatestIOSSignTaskByBuild(task.BuildID.String())
	if existsErr == nil && existing != nil && !existing.Finished {
		// An active sign task is already in progress for this build.
		return false, nil
	}
	if existsErr == nil && existing != nil && existing.Finished && !strings.EqualFold(existing.Status, "error") {
		// A completed (non-error) sign task exists — no need to retry.
		return false, nil
	}
	// If existsErr != nil, no existing task was found (sql.ErrNoRows) — proceed.
	// If existing is a failed sign task (Finished=true, Status="error"), fall
	// through to allow a fresh attempt.

	signReq := requests.SignIPARequest{
		ProjectID:     task.ProjectID.String(),
		BuildID:       task.BuildID.String(),
		CredentialsID: conn.ID.String(),
	}
	if _, err := c.SignUnsignedIPATask(user, signReq); err != nil {
		return false, err
	}
	return true, nil
}

// GetLatestSignStatusForBuild returns the most recent sign task for a build,
// scoped to the caller. Used by the build card UI to display "signed" /
// "signing" / "not signed" across refreshes without relying on local state.
func (c *Controller) GetLatestSignStatusForBuild(user *models.User, buildID string) (*models.SignIPATask, error) {
	build, err := c.DB.GetProjectBuildById(buildID)
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

	signTask, err := c.DB.GetLatestIOSSignTaskByBuild(buildID)
	if err != nil {
		return nil, err
	}
	return signTask, nil
}

// RequirePlayAppSigning checks that Play App Signing is enrolled for the
// given package name before triggering a first Android publish. Returns
// ErrPlayAppSigningRequired if not enrolled, nil if enrolled or if the check
// cannot be performed (in which case we log and allow through, so a temporary
// SA permission issue doesn't permanently block the user).
//
// Callers on the update path (re-deploy) may skip this check since the app is
// already live and signing must already be configured.
func (c *Controller) RequirePlayAppSigning(ctx context.Context, cred *models.GooglePlayConnection, packageName string) error {
	saBytes, err := c.UserApiKeysStorage.ReadObjectBytes(ctx, cred.ServiceAccountPath)
	if err != nil {
		// Can't read the SA — let the deploy proceed and fail at Codemagic with
		// a more actionable error than "SA unreadable".
		slog.Warn("RequirePlayAppSigning: read service account failed, skipping gate",
			"conn", cred.ID, "err", err)
		return nil
	}
	sa, err := storeauth.ParseServiceAccount(saBytes)
	if err != nil {
		slog.Warn("RequirePlayAppSigning: parse service account failed, skipping gate",
			"conn", cred.ID, "err", err)
		return nil
	}
	enrolled, err := storeauth.CheckPlayAppSigningEnrolment(ctx, sa, packageName)
	if err != nil {
		// Network or unexpected API error — don't block the user.
		slog.Warn("RequirePlayAppSigning: enrolment check error, skipping gate",
			"conn", cred.ID, "package", packageName, "err", err)
		return nil
	}
	if !enrolled {
		return ErrPlayAppSigningRequired
	}
	return nil
}
