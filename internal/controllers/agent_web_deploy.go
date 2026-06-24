package controllers

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/knid/wemob/internal/models"
)

func (c *Controller) triggerWebDeployForAgent(ctx context.Context, project *models.AppProject, agentUser *models.User) (string, error) {
	_ = ctx
	if project == nil {
		return "", fmt.Errorf("triggerWebDeploy: nil project")
	}

	owner := agentUser
	if project.UserID != owner.ID {
		ownerLookup, err := c.DB.GetUserById(project.UserID.String())
		if err != nil {
			return "", fmt.Errorf("failed to load project owner: %w", err)
		}
		owner = ownerLookup
	}

	deployment, err := c.CreateQueuedWebDeployment(project, owner)
	if err != nil {
		return "", err
	}

	_, planName, _ := c.GetUserLimitsWithPlan(owner)
	isPaid := planName != "" && planName != "free" && planName != "trial"

	if _, err := c.EnqueueWebDeploy(deployment.ID, isPaid); err != nil {
		c.MarkDeploymentFailed(deployment.ID.String(), "agent-enqueue: "+err.Error())
		return "", err
	}

	slog.Info("triggerWebDeploy: queued",
		"project_id", project.ID,
		"deployment_id", deployment.ID,
		"plan", planName,
		"paid", isPaid)
	return deployment.ID.String(), nil
}
