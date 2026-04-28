package controllers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/codemagic"
	"github.com/slack-go/slack"
)

// ResumePendingDeployments attaches the newly-added connection to any
// pending_connection deployments the user has for the matching platform,
// flips their status to Queued, triggers Codemagic, and re-emits a
// submitted event with actor_type = system + metadata.resumed = true.
//
// platform is "ios" or "android". connectionID is the uuid of the
// connection just created. User scoping prevents cross-user pollution
// if a staff member ever adds a connection on another user's behalf.
//
// All errors are logged but do not abort the outer handler — the user
// has their connection and can manually submit if auto-resume hiccups.
func (c *Controller) ResumePendingDeployments(ctx context.Context, user *models.User, platform string, connectionID uuid.UUID) {
	deployments, err := c.DB.GetDeployments(user)
	if err != nil {
		slog.Warn("resume: list deployments failed", "user_id", user.ID, "err", err)
		return
	}

	for _, d := range deployments {
		if d.Status != "pending_connection" {
			continue
		}
		if !slices.Contains(d.Platforms, platform) {
			continue
		}
		// Skip if that platform already has a connection set (happens
		// on multi-platform pending deployments when the second
		// connection is added).
		if platform == "ios" && d.AppleConnectionID != uuid.Nil {
			continue
		}
		if platform == "android" && d.GooglePlayConnectionID != uuid.Nil {
			continue
		}

		if err := c.resumeOne(ctx, user, d, platform, connectionID); err != nil {
			slog.Warn("resume: resume deployment failed",
				"user_id", user.ID,
				"deployment_id", d.ID,
				"platform", platform,
				"err", err)
			continue
		}
		slog.Info("resume: pending_connection resumed",
			"deployment_id", d.ID,
			"platform", platform,
			"connection_id", connectionID)
	}
}

func (c *Controller) resumeOne(ctx context.Context, user *models.User, d models.Deployment, platform string, connectionID uuid.UUID) error {
	// Attach connection.
	if platform == "ios" {
		d.AppleConnectionID = connectionID
	} else {
		d.GooglePlayConnectionID = connectionID
	}

	// Flip status. If it has BOTH platforms pending and only one was just
	// fulfilled, keep it at pending_connection until the other platform's
	// connection arrives.
	stillPending := false
	for _, p := range d.Platforms {
		switch p {
		case "ios":
			if d.AppleConnectionID == uuid.Nil {
				stillPending = true
			}
		case "android":
			if d.GooglePlayConnectionID == uuid.Nil {
				stillPending = true
			}
		}
	}
	if !stillPending {
		d.Status = "Queued"
	}
	if _, err := c.DB.UpdateDeployment(&d); err != nil {
		return fmt.Errorf("update deployment: %w", err)
	}

	// If there are still missing connections, don't trigger Codemagic yet.
	if stillPending {
		return nil
	}

	project, err := c.DB.GetProjectById(d.ProjectID.String())
	if err != nil {
		return fmt.Errorf("get project: %w", err)
	}
	build, err := c.DB.GetProjectBuildById(d.BuildID.String())
	if err != nil {
		return fmt.Errorf("get build: %w", err)
	}

	// Trigger per-platform. Multi-platform deployments fire both.
	if slices.Contains(d.Platforms, "ios") {
		cred, err := c.DB.GetAppleConnectionById(d.AppleConnectionID.String())
		if err != nil {
			return fmt.Errorf("get apple connection: %w", err)
		}
		if _, err := c.CodemagicService.TriggerIOSDeploy(codemagic.IOSDeployParams{
			BuildID:            d.BuildID.String(),
			DeploymentID:       d.ID.String(),
			PackageName:        project.PackageName,
			TeamID:             cred.TeamID,
			KeyID:              cred.KeyID,
			IssuerID:           cred.IssuerID,
			P8KeyS3Path:        cred.P8KeyPath,
			Title:              d.Title,
			Description:        d.Description,
			Screenshots:        d.ScreenshotPaths,
			Icon:               build.Icon,
			Locale:             d.Locale,
			NewApp:             true,
			ChangedBuild:       true,
			TitleChanged:       true,
			DescChanged:        true,
			ScreenshotsChanged: true,
			RolloutTrack:       d.RolloutTrack,
			ReleaseNotes:       d.ReleaseNotes,
		}); err != nil {
			return fmt.Errorf("trigger iOS deploy: %w", err)
		}
		go c.CRMService.SendEvent(user, constants.EVENT_USER_APPLE_DEPLOYMENT_STARTED, map[string]interface{}{
			"project_id":       project.ID.String(),
			"deployment_id":    d.ID.String(),
			"build_id":         build.ID.String(),
			"deployment_title": d.Title,
		})
	}
	if slices.Contains(d.Platforms, "android") {
		cred, err := c.DB.GetGooglePlayConnectionById(d.GooglePlayConnectionID.String())
		if err != nil {
			return fmt.Errorf("get google connection: %w", err)
		}
		if _, err := c.CodemagicService.TriggerAndroidDeploy(codemagic.AndroidDeployParams{
			BuildID:              d.BuildID.String(),
			DeploymentID:         d.ID.String(),
			PackageName:          project.PackageName,
			Version:              build.BuildNumber,
			Title:                d.Title,
			Description:          d.Description,
			Screenshots:          d.ScreenshotPaths,
			IconPath:             d.IconPath,
			Locale:               d.Locale,
			ServiceAccountS3Path: cred.ServiceAccountPath,
			NewApp:               true,
			ChangedBuild:         true,
			TitleChanged:         true,
			DescChanged:          true,
			ScreenshotsChanged:   true,
			IconChanged:          true,
		}); err != nil {
			return fmt.Errorf("trigger Android deploy: %w", err)
		}
		go c.CRMService.SendEvent(user, constants.EVENT_USER_ANDROID_DEPLOYMENT_STARTED, map[string]interface{}{
			"project_id":       project.ID.String(),
			"deployment_id":    d.ID.String(),
			"build_id":         build.ID.String(),
			"deployment_title": d.Title,
		})
	}

	// Re-emit a submitted event for the resume. System actor because the
	// user's action was adding the connection, not re-clicking submit.
	_ = database.AppendDeploymentEvent(ctx, c.DB.DB, d.ID,
		models.DeploymentEventSubmitted, models.DeploymentEventActorSystem, nil,
		map[string]interface{}{"resumed": true, "platform": platform})
	return nil
}

// SwitchToManaged flips a pending_connection deployment onto the
// Wemob-managed track. Used by the "Set up later?" nudge email's
// escape link + the Launchpad store tile's secondary CTA. Clears the
// user connection IDs (since the deployment is now managed by us),
// sets Status = "Queued", posts a Slack notification (mirrors the
// managed-deploy codepath in CreateDeploymentTask), and emits
// submitted with metadata.switched_to_managed = true for timeline
// attribution.
//
// Idempotent: calling on a non-pending deployment is a no-op.
func (c *Controller) SwitchToManaged(ctx context.Context, user *models.User, deploymentID string) (*models.Deployment, error) {
	deployment, err := c.DB.GetDeploymentByID(deploymentID)
	if err != nil {
		return nil, err
	}
	if deployment.UserID.String() != user.ID.String() {
		return nil, errors.New("you do not have access to this resource")
	}
	if deployment.Status != "pending_connection" {
		return deployment, nil // nothing to do
	}

	deployment.Method = "wemob"
	deployment.AppleConnectionID = uuid.Nil
	deployment.GooglePlayConnectionID = uuid.Nil
	deployment.Status = "Queued"

	updated, err := c.DB.UpdateDeployment(deployment)
	if err != nil {
		return nil, fmt.Errorf("update deployment: %w", err)
	}

	// Emit submitted with attribution metadata.
	actorID := user.ID
	_ = database.AppendDeploymentEvent(ctx, c.DB.DB, updated.ID,
		models.DeploymentEventSubmitted, models.DeploymentEventActorUser, &actorID,
		map[string]interface{}{"switched_to_managed": true})

	// Mirror the Slack notification from CreateDeploymentTask's managed branch.
	go c.SlackService.Client.PostMessage(
		constants.SLACK_CHANNEL_ID_NEW_ANDROID_DEPLOY_REQUEST,
		slack.MsgOptionText(
			"Switched to managed: ("+deployment.ProjectID.String()+") "+
				updated.ID.String()+" platforms: "+fmt.Sprintf("%v", updated.Platforms),
			false,
		),
	)
	return updated, nil
}
