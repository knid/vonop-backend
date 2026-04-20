package controllers

import (
	"context"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
)

func (c *Controller) UpdateBuildTaskStatus(reportBuildReq requests.ReportBuildRequest) error {
	buildTask, err := c.DB.GetAppBuildTaskById(reportBuildReq.TaskID)
	if err != nil {
		return err
	}

	slog.Info("updating build task status", "task_id", buildTask.ID.String(), "status", reportBuildReq.Status)

	buildTask.StatusText = reportBuildReq.Status
	switch reportBuildReq.Status {
	case "success", "completed", "done", "built", "Built", "Builded":
		buildTask.Finished = true
		// Clear any prior error message on a successful terminal state so a
		// re-run after a failure doesn't keep showing the stale failure.
		buildTask.ErrorMessage = ""
	case "error", "failed", "cancelled":
		buildTask.Finished = true
		if reportBuildReq.Message != "" {
			buildTask.ErrorMessage = reportBuildReq.Message
			annotationID, annotErr := database.MatchBuildAnnotation(context.Background(), c.DB.DB, reportBuildReq.Message)
			if annotErr == nil && annotationID != uuid.Nil {
				buildTask.AnnotationID = &annotationID
			} else if annotErr != nil {
				slog.Warn("annotation match failed", "task_id", buildTask.ID, "err", annotErr)
			}
		}
	}

	if err := c.DB.UpdateAppBuildTask(buildTask); err != nil {
		return err
	}

	if strings.EqualFold(reportBuildReq.Status, "Built") {
		signed, err := c.AutoSignIOSBuildIfPossible(context.Background(), buildTask)
		if err != nil {
			slog.Warn("auto-sign enqueue failed", "task_id", buildTask.ID, "err", err)
		} else if signed {
			slog.Info("auto-sign enqueued", "task_id", buildTask.ID)
		}
	}

	return nil
}

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

func (c *Controller) UpdateSignIPATaskStatus(reportSignReq requests.ReportSignIPARequest) error {
	signTask, err := c.DB.GetIOSSignTaskById(reportSignReq.TaskID)
	if err != nil {
		return err
	}

	signTask.StatusMessage = reportSignReq.StatusMessage
	if reportSignReq.Status == "success" || reportSignReq.Status == "completed" ||
		reportSignReq.Status == "done" {
		signTask.Finished = true
	}

	return c.DB.UpdateIOSSignTask(signTask)
}
