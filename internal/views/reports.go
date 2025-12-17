package views

import (
	"net/http"

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
