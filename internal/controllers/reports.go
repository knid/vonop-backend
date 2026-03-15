package controllers

import (
	"log/slog"

	"github.com/knid/wemob/internal/requests"
)

func (c *Controller) UpdateBuildTaskStatus(reportBuildReq requests.ReportBuildRequest) error {
	buildTask, err := c.DB.GetAppBuildTaskById(reportBuildReq.TaskID)
	if err != nil {
		return err
	}

	slog.Info("updating build task status", "task_id", buildTask.ID.String(), "status", reportBuildReq.Status)

	buildTask.StatusText = reportBuildReq.Status
	if reportBuildReq.Status == "success" || reportBuildReq.Status == "completed" ||
		reportBuildReq.Status == "done" || reportBuildReq.Status == "built" || reportBuildReq.Status == "Built" {
		buildTask.Finished = true
	}

	return c.DB.UpdateAppBuildTask(buildTask)
}

func (c *Controller) UpdateDeploymentStatus(reportDeplomentReq requests.ReportDeploymentRequest) error {
	deployment, err := c.DB.GetDeploymentByID(reportDeplomentReq.DeploymentID)
	if err != nil {
		return err
	}

	deployment.Status = reportDeplomentReq.Status

	_, err = c.DB.UpdateDeployment(deployment)
	return err
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
