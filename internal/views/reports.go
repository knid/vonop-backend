package views

import (
	"crypto/subtle"
	"log/slog"
	"net/http"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
)

// sendCRMEvent sends a CRM event in a goroutine and logs errors.
func (v *HttpViews) sendCRMEvent(user *models.User, event string, payload map[string]any) {
	go func() {
		if err := v.Controller.CRMService.SendEvent(user, event, payload); err != nil {
			slog.Error("failed to send CRM event", "event", event, "error", err)
		}
	}()
}

// verifyWorkerSecret checks the Authorization header or body secret against the WORKER_REPORT_SECRET.
func (v *HttpViews) verifyWorkerSecret(w http.ResponseWriter, secret string) bool {
	expected := v.Controller.SecretManager.GetSecret("WORKER_REPORT_SECRET")
	if expected == "" {
		utils.JSONErrorCode(w, constants.ErrInternal, http.StatusInternalServerError, nil)
		return false
	}
	if subtle.ConstantTimeCompare([]byte(secret), []byte(expected)) != 1 {
		utils.JSONErrorCode(w, constants.ErrAuthRequired, http.StatusUnauthorized, nil)
		return false
	}
	return true
}

func (v *HttpViews) UpdateBuildTaskStatus(w http.ResponseWriter, r *http.Request) {
	var reportBuildReq requests.ReportBuildRequest
	err := utils.ParseJSONBody(r, &reportBuildReq)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrValidationBody, http.StatusBadRequest, nil)
		return
	}

	if !v.verifyWorkerSecret(w, reportBuildReq.Secret) {
		return
	}

	err = v.Controller.UpdateBuildTaskStatus(reportBuildReq)
	if err != nil {
		utils.ValidationError(w, err)
		return
	}

	task, err := v.Controller.DB.GetAppBuildTaskWithRelations(reportBuildReq.TaskID)
	if err != nil {
		utils.ValidationError(w, err)
		return
	}

	payload := map[string]any{
		"build_id":     task.BuildID.String(),
		"task_id":      task.ID.String(),
		"project_id":   task.ProjectID.String(),
		"build_title":  task.Build.Title,
		"build_number": task.Build.BuildNumber,
		"package_name": task.Build.Project.PackageName,
	}

	// Fire the "build started" CRM event once per build. The yaml now emits
	// a stage ladder (Preparing → Building → Uploading), so gate on the
	// first stage only to avoid double-firing.
	if reportBuildReq.Status == "Preparing" && task.Platform == "ios" {
		v.sendCRMEvent(task.Project.User, constants.BREVO_EVENT_BUILD_STARTED_IOS, payload)
	} else if reportBuildReq.Status == "Preparing" && task.Platform == "android" {
		v.sendCRMEvent(task.Project.User, constants.BREVO_EVENT_BUILD_STARTED_ANDROID, payload)
	} else if reportBuildReq.Status == "Built" && task.Platform == "ios" {
		v.sendCRMEvent(task.Project.User, constants.BREVO_EVENT_BUILD_FINISHED_IOS, payload)
	} else if reportBuildReq.Status == "Built" && task.Platform == "android" {
		v.sendCRMEvent(task.Project.User, constants.BREVO_EVENT_BUILD_FINISHED_ANDROID, payload)
	} else if reportBuildReq.Status == "error" && task.Platform == "ios" {
		payload["error_message"] = reportBuildReq.Message
		v.sendCRMEvent(task.Project.User, constants.BREVO_EVENT_BUILD_ERROR_IOS, payload)
	} else if reportBuildReq.Status == "error" && task.Platform == "android" {
		payload["error_message"] = reportBuildReq.Message
		v.sendCRMEvent(task.Project.User, constants.BREVO_EVENT_BUILD_ERROR_ANDROID, payload)
	}

	utils.JSONResponse(w, map[string]string{"status": "success"}, http.StatusOK)
}

func (v *HttpViews) UpdateDeploymentStatus(w http.ResponseWriter, r *http.Request) {
	var reportDeploymentReq requests.ReportDeploymentRequest
	err := utils.ParseJSONBody(r, &reportDeploymentReq)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrValidationBody, http.StatusBadRequest, nil)
		return
	}

	if !v.verifyWorkerSecret(w, reportDeploymentReq.Secret) {
		return
	}

	err = v.Controller.UpdateDeploymentStatus(reportDeploymentReq)
	if err != nil {
		utils.ValidationError(w, err)
		return
	}

	deployment, err := v.Controller.DB.GetDeploymentWithRelations(reportDeploymentReq.DeploymentID)
	if err != nil {
		utils.ValidationError(w, err)
		return
	}

	payload := map[string]any{
		"project_id":       deployment.ProjectID.String(),
		"deployment_id":    deployment.ID.String(),
		"build_id":         deployment.BuildID.String(),
		"deployment_title": deployment.Title,
	}

	switch reportDeploymentReq.Platform {
	case "ios":
		switch reportDeploymentReq.Status {
		case "Deploying":
			v.sendCRMEvent(deployment.User, constants.BREVO_EVENT_USER_APPLE_DEPLOYMENT_STARTED, payload)
		case "Deployed":
			v.sendCRMEvent(deployment.User, constants.BREVO_EVENT_USER_APPLE_DEPLOYMENT_FINISHED, payload)
		case "error":
			payload["error_message"] = reportDeploymentReq.Message
			v.sendCRMEvent(deployment.User, constants.BREVO_EVENT_USER_APPLE_DEPLOYMENT_FAILED, payload)
		}
	case "android":
		switch reportDeploymentReq.Status {
		case "Deploying":
			v.sendCRMEvent(deployment.User, constants.BREVO_EVENT_USER_ANDROID_DEPLOYMENT_STARTED, payload)
		case "Deployed":
			v.sendCRMEvent(deployment.User, constants.BREVO_EVENT_USER_ANDROID_DEPLOYMENT_FINISHED, payload)
		case "error":
			payload["error_message"] = reportDeploymentReq.Message
			v.sendCRMEvent(deployment.User, constants.BREVO_EVENT_USER_ANDROID_DEPLOYMENT_FAILED, payload)
		}
	}

	utils.JSONResponse(w, map[string]string{"status": "success"}, http.StatusOK)
}

func (v *HttpViews) UpdateSignIPATaskStatus(w http.ResponseWriter, r *http.Request) {
	var reportSignReq requests.ReportSignIPARequest
	err := utils.ParseJSONBody(r, &reportSignReq)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrValidationBody, http.StatusBadRequest, nil)
		return
	}

	if !v.verifyWorkerSecret(w, reportSignReq.Secret) {
		return
	}

	err = v.Controller.UpdateSignIPATaskStatus(reportSignReq)
	if err != nil {
		utils.ValidationError(w, err)
		return
	}

	utils.JSONResponse(w, map[string]string{"status": "success"}, http.StatusOK)
}
