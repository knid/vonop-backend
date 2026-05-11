package controllers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/knid/wemob/internal/models"
)

// autoDeployBackendForAgent queues a Fly deploy for the project's backend/
// subtree. Called from the agent runner via RunDependencies.AutoDeployBackend
// AFTER a chat turn whose diff touched any backend/* path commits cleanly.
// Closes the "user described an AI feature but nothing works because they
// didn't manually click Deploy" UX gap.
//
// Guards:
//   - Only mobile_app_native projects auto-deploy (web projects keep the
//     manual deploy button — auto-deploying every chat turn there would
//     surprise the user with a backend they didn't ask for).
//   - The owning user is resolved from project.UserID (the user passed
//     into AgentDeps may be a collaborator on a shared org, but billing
//     and quotas should land on the project owner — same as the manual
//     view-layer Deploy flow).
//   - ErrDeployInProgress is swallowed: a deploy is already running for
//     this project, our redundant request would be rejected anyway, and
//     the in-flight one will pick up the just-committed code.
//   - All other errors log warn + return nil. The chat turn must not
//     fail because a background deploy hiccupped.
//
// Plan-tier routing: free / trial users go to deploy-web-free, everyone
// else to deploy-web-pro. Same logic as the manual Deploy view (see
// app_deploy_views.go) but without the queue-saturation 503 — auto-deploy
// is best-effort, so we don't block a chat turn on a saturated queue.
func (c *Controller) autoDeployBackendForAgent(ctx context.Context, project *models.AppProject, agentUser *models.User) error {
	if project == nil {
		return fmt.Errorf("autoDeployBackend: nil project")
	}
	if project.ProjectType != models.ProjectTypeMobileAppNative {
		// web/ecommerce projects deploy on user click — auto-deploy would
		// surprise them by spinning up infra for changes they may not
		// have intended to ship yet.
		return nil
	}

	// Resolve the project owner. The agent run might have been initiated
	// by a collaborator, but the deploy should bill against the project's
	// owning user (mirrors the manual deploy flow).
	owner := agentUser
	if project.UserID != owner.ID {
		ownerLookup, err := c.DB.GetUserById(project.UserID.String())
		if err != nil {
			slog.Warn("autoDeployBackend: failed to load project owner",
				"error", err, "project_id", project.ID, "owner_id", project.UserID)
			return nil
		}
		owner = ownerLookup
	}

	deployment, err := c.CreateQueuedWebDeployment(project, owner)
	if err != nil {
		if errors.Is(err, ErrDeployInProgress) {
			// Manual or earlier-turn deploy still running — that one will
			// ship the latest commit. Nothing to do.
			return nil
		}
		slog.Warn("autoDeployBackend: failed to create deployment",
			"error", err, "project_id", project.ID)
		return nil
	}

	_, planName, _ := c.GetUserLimitsWithPlan(owner)
	isPaid := planName != "" && planName != "free" && planName != "trial"

	if _, err := c.EnqueueWebDeploy(deployment.ID, isPaid); err != nil {
		// Mark the deployment failed so the UI doesn't show a perpetual
		// "queued" state.
		c.MarkDeploymentFailed(deployment.ID.String(), "auto-enqueue: "+err.Error())
		slog.Warn("autoDeployBackend: enqueue failed",
			"error", err, "project_id", project.ID, "deployment_id", deployment.ID)
		return nil
	}

	slog.Info("autoDeployBackend: queued",
		"project_id", project.ID,
		"deployment_id", deployment.ID,
		"plan", planName,
		"paid", isPaid)
	return nil
}

// triggerWebDeployForAgent queues a Fly web deploy for the project on behalf
// of the chat agent's trigger_web_deploy tool. Works for any project type
// with a web deploy pipeline (web, ecommerce, mobile_app_native).
//
// Guards:
//   - The owning user is resolved from project.UserID so billing lands on the
//     project owner even when a collaborator initiated the chat.
//   - ErrDeployInProgress is returned as a normal error so the agent can tell
//     the user a deploy is already running.
//   - All other errors bubble up so the agent knows the deploy didn't start.
func (c *Controller) triggerWebDeployForAgent(ctx context.Context, project *models.AppProject, agentUser *models.User) (string, error) {
	if project == nil {
		return "", fmt.Errorf("triggerWebDeploy: nil project")
	}

	// Resolve the project owner.
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
