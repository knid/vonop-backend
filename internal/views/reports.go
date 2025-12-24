package views

import (
	"net/http"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) UpdateBuildTaskStatus(w http.ResponseWriter, r *http.Request) {
	// check secret
	var reportBuildReq requests.ReportBuildRequest
	err := utils.ParseJSONBody(r, &reportBuildReq)
	if err != nil {
		utils.JSONErrorResponse(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	err = v.Controller.UpdateBuildTaskStatus(reportBuildReq)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	task, err := v.Controller.DB.GetAppBuildTaskById(reportBuildReq.TaskID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
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

	if reportBuildReq.Status == "Building" && task.Platform == "ios" {
		go v.Controller.CRMService.SendEvent(task.Project.User, constants.BREVO_EVENT_BUILD_STARTED_IOS, payload)
	} else if reportBuildReq.Status == "Building" && task.Platform == "android" {
		go v.Controller.CRMService.SendEvent(task.Project.User, constants.BREVO_EVENT_BUILD_STARTED_ANDROID, payload)
	} else if reportBuildReq.Status == "Builded" && task.Platform == "ios" {
		go v.Controller.CRMService.SendEvent(task.Project.User, constants.BREVO_EVENT_BUILD_FINISHED_IOS, payload)
	} else if reportBuildReq.Status == "Builded" && task.Platform == "android" {
		go v.Controller.CRMService.SendEvent(task.Project.User, constants.BREVO_EVENT_BUILD_FINISHED_ANDROID, payload)
	} else if reportBuildReq.Status == "error" && task.Platform == "ios" {
		payload["error_message"] = reportBuildReq.Message
		go v.Controller.CRMService.SendEvent(task.Project.User, constants.BREVO_EVENT_BUILD_ERROR_IOS, payload)
	} else if reportBuildReq.Status == "error" && task.Platform == "android" {
		payload["error_message"] = reportBuildReq.Message
		go v.Controller.CRMService.SendEvent(task.Project.User, constants.BREVO_EVENT_BUILD_ERROR_ANDROID, payload)
	}

	utils.JSONResponse(w, map[string]string{"status": "success"}, http.StatusOK)
}

func (v *HttpViews) UpdateDeploymentStatus(w http.ResponseWriter, r *http.Request) {
	// check secret
	var reportDeploymentReq requests.ReportDeploymentRequest
	err := utils.ParseJSONBody(r, &reportDeploymentReq)
	if err != nil {
		utils.JSONErrorResponse(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	err = v.Controller.UpdateDeploymentStatus(reportDeploymentReq)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	deployment, err := v.Controller.DB.GetDeploymentByID(reportDeploymentReq.DeploymentID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
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
			go v.Controller.CRMService.SendEvent(deployment.User, constants.BREVO_EVENT_USER_APPLE_DEPLOYMENT_STARTED, payload)
		case "Deployed":
			go v.Controller.CRMService.SendEvent(deployment.User, constants.BREVO_EVENT_USER_APPLE_DEPLOYMENT_FINISHED, payload)
		case "error":
			payload["error_message"] = reportDeploymentReq.Message
			go v.Controller.CRMService.SendEvent(deployment.User, constants.BREVO_EVENT_USER_APPLE_DEPLOYMENT_FAILED, payload)
		}
	case "android":
		switch reportDeploymentReq.Status {
		case "Deploying":
			go v.Controller.CRMService.SendEvent(deployment.User, constants.BREVO_EVENT_USER_ANDROID_DEPLOYMENT_STARTED, payload)
		case "Deployed":
			go v.Controller.CRMService.SendEvent(deployment.User, constants.BREVO_EVENT_USER_ANDROID_DEPLOYMENT_FINISHED, payload)
		case "error":
			payload["error_message"] = reportDeploymentReq.Message
			go v.Controller.CRMService.SendEvent(deployment.User, constants.BREVO_EVENT_USER_ANDROID_DEPLOYMENT_FAILED, payload)
		}
	}

	utils.JSONResponse(w, map[string]string{"status": "success"}, http.StatusOK)
}

func (v *HttpViews) UpdateSignIPATaskStatus(w http.ResponseWriter, r *http.Request) {
	// check secret
	var reportSignReq requests.ReportSignIPARequest
	err := utils.ParseJSONBody(r, &reportSignReq)
	if err != nil {
		utils.JSONErrorResponse(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	err = v.Controller.UpdateSignIPATaskStatus(reportSignReq)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, map[string]string{"status": "success"}, http.StatusOK)
}
