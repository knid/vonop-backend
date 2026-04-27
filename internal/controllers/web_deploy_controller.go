package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/tasks"
)

// CreateQueuedWebDeployment writes a deployment row in 'queued' state. The
// worker will flip it through 'building' → 'deploying' → 'succeeded'/'failed'.
func (c *Controller) CreateQueuedWebDeployment(project *models.AppProject, user *models.User) (*models.AppDeployment, error) {
	deployment := &models.AppDeployment{
		AppProjectID: project.ID,
		UserID:       user.ID,
		Platform:     "web",
		Provider:     "fly",
		Status:       "queued",
		Version:      1,
	}
	if latest, err := c.DB.GetLatestAppDeployment(project.ID.String(), "web"); err == nil && latest != nil {
		deployment.Version = latest.Version + 1
	}
	deployment, err := c.DB.CreateAppDeployment(deployment)
	if err != nil {
		return nil, err
	}

	actorID := user.ID
	if err := database.AppendDeploymentEvent(context.Background(), c.DB.DB, deployment.ID,
		models.DeploymentEventSubmitted, models.DeploymentEventActorUser, &actorID,
		map[string]any{"method": "fly"}); err != nil {
		slog.Warn("append deployment_events.submitted failed", "err", err)
	}
	return deployment, nil
}

// EnqueueWebDeploy submits a deploy:web task to the Asynq queue tier matching
// the user's plan. Returns the queue name so the caller can include it in the
// 202 response body for client-side observability.
func (c *Controller) EnqueueWebDeploy(deploymentID uuid.UUID, isPaidPlan bool) (queue string, err error) {
	queue = "deploy-web-free"
	if isPaidPlan {
		queue = "deploy-web-pro"
	}
	payload, err := json.Marshal(tasks.WebDeployPayload{DeploymentID: deploymentID.String()})
	if err != nil {
		return "", fmt.Errorf("marshal payload: %w", err)
	}
	task := asynq.NewTask(tasks.TypeDeployWeb, payload)
	_, err = c.TaskService.Enqueue(task,
		asynq.Queue(queue),
		asynq.MaxRetry(4),
		asynq.Timeout(15*time.Minute),
		asynq.TaskID(deploymentID.String()),
	)
	if err != nil {
		return queue, fmt.Errorf("enqueue: %w", err)
	}
	return queue, nil
}

// MarkDeploymentBuilding flips a deployment row from 'queued' to 'building'.
func (c *Controller) MarkDeploymentBuilding(deploymentID string) error {
	_, err := c.DB.DB.NewUpdate().
		Model((*models.AppDeployment)(nil)).
		Set("status = ?", "building").
		Set("updated_at = ?", time.Now()).
		Where("id = ?", deploymentID).
		Exec(context.Background())
	return err
}

// MarkDeploymentDeploying flips status to 'deploying' once the image is built
// and Machines are being launched.
func (c *Controller) MarkDeploymentDeploying(deploymentID string) error {
	_, err := c.DB.DB.NewUpdate().
		Model((*models.AppDeployment)(nil)).
		Set("status = ?", "deploying").
		Set("updated_at = ?", time.Now()).
		Where("id = ?", deploymentID).
		Exec(context.Background())
	return err
}

// MarkDeploymentSucceeded is called when the Fly webhook reports a healthy
// machine. Also writes deployed_url for the client's response.
func (c *Controller) MarkDeploymentSucceeded(deploymentID string, deployedURL string) error {
	_, err := c.DB.DB.NewUpdate().
		Model((*models.AppDeployment)(nil)).
		Set("status = ?", "succeeded").
		Set("deployed_url = ?", deployedURL).
		Set("updated_at = ?", time.Now()).
		Where("id = ?", deploymentID).
		Exec(context.Background())
	return err
}

// MarkDeploymentFailed records the reason in the deployment row so the UI can
// show a useful error.
func (c *Controller) MarkDeploymentFailed(deploymentID string, reason string) error {
	_, err := c.DB.DB.NewUpdate().
		Model((*models.AppDeployment)(nil)).
		Set("status = ?", "failed").
		Set("error_message = ?", reason).
		Set("updated_at = ?", time.Now()).
		Where("id = ?", deploymentID).
		Exec(context.Background())
	return err
}
