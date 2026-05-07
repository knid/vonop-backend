package controllers

import (
	"context"
	"strings"

	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
)

func (c *Controller) UpdateDeploymentStatus(reportDeplomentReq requests.ReportDeploymentRequest) error {
	deployment, err := c.DB.GetDeploymentByID(reportDeplomentReq.DeploymentID)
	if err != nil {
		return err
	}

	deployment.Status = reportDeplomentReq.Status
	if reportDeplomentReq.Message != "" {
		deployment.StatusMessage = reportDeplomentReq.Message
	}

	if _, err = c.DB.UpdateDeployment(deployment); err != nil {
		return err
	}

	// Phase 5a: emit event for the status transition.
	switch strings.ToLower(reportDeplomentReq.Status) {
	case "deployed", "live", "published":
		_ = database.AppendDeploymentEvent(context.Background(), c.DB.DB, deployment.ID,
			models.DeploymentEventStoreLive, models.DeploymentEventActorSystem, nil,
			map[string]interface{}{"note": reportDeplomentReq.Message})
	case "reviewing", "submitted":
		_ = database.AppendDeploymentEvent(context.Background(), c.DB.DB, deployment.ID,
			models.DeploymentEventStoreReviewStarted, models.DeploymentEventActorSystem, nil,
			map[string]interface{}{"note": reportDeplomentReq.Message})
	case "rejected":
		_ = database.AppendDeploymentEvent(context.Background(), c.DB.DB, deployment.ID,
			models.DeploymentEventStoreRejected, models.DeploymentEventActorSystem, nil,
			map[string]interface{}{"note": reportDeplomentReq.Message})
	case "error", "failed":
		_ = database.AppendDeploymentEvent(context.Background(), c.DB.DB, deployment.ID,
			models.DeploymentEventFailed, models.DeploymentEventActorSystem, nil,
			map[string]interface{}{"note": reportDeplomentReq.Message})
	case "deploying":
		// "deploying" is when the Codemagic workflow has actually pushed
		// the binary to the store — semantically an "uploaded_to_store"
		// moment. "queued" (the prior state) is before any upload has
		// occurred so we deliberately don't emit for it.
		_ = database.AppendDeploymentEvent(context.Background(), c.DB.DB, deployment.ID,
			models.DeploymentEventUploadedToStore, models.DeploymentEventActorSystem, nil,
			map[string]interface{}{"note": reportDeplomentReq.Message})
	}

	return nil
}

