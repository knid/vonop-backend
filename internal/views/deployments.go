package views

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) GetDeployments(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	if requireActiveSubscription(w, user) {
		return
	}

	deployments, err := v.Controller.GetDeployments(user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, deployments, http.StatusOK)
}

func (v *HttpViews) GetDeployment(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	if requireActiveSubscription(w, user) {
		return
	}

	deploymentID := chi.URLParam(r, "deploymentId")
	if deploymentID == "" {
		utils.JSONErrorResponse(w, "deployment ID is required", http.StatusBadRequest)
		return
	}
	deployment, err := v.Controller.GetDeploymentByID(deploymentID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	if deployment.UserID.String() != user.ID.String() {
		utils.JSONErrorResponse(w, "you do not have access to this resource", http.StatusForbidden)
		return
	}

	utils.JSONResponse(w, deployment, http.StatusOK)
}

func (v *HttpViews) CreateDeployment(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	if requireActiveSubscription(w, user) {
		return
	}

	var deploymentCreateReq requests.CreateDeploymentRequest
	if err := utils.ParseJSONBody(r, &deploymentCreateReq); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	deployment, err := v.Controller.CreateDeploymentTask(user, deploymentCreateReq)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, deployment, http.StatusOK)
}

func (v *HttpViews) UpdateDeployment(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	if requireActiveSubscription(w, user) {
		return
	}

	var deploymentCreateReq requests.CreateDeploymentRequest
	if err := utils.ParseJSONBody(r, &deploymentCreateReq); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	deploymentID := chi.URLParam(r, "deploymentId")
	if deploymentID == "" {
		utils.JSONErrorResponse(w, "deployment ID is required", http.StatusBadRequest)
		return
	}

	deployment, err := v.Controller.UpdateDeploymentTask(user, deploymentID, deploymentCreateReq)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, deployment, http.StatusOK)
}

// SwitchDeploymentToManaged handles POST /v1/deployments/:id/switch-to-managed.
// Flips a pending_connection deployment onto the Wemob-managed track.
func (v *HttpViews) SwitchDeploymentToManaged(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	if requireActiveSubscription(w, user) {
		return
	}

	deploymentID := chi.URLParam(r, "deploymentId")
	if deploymentID == "" {
		utils.JSONErrorResponse(w, "deployment id required", http.StatusBadRequest)
		return
	}
	updated, err := v.Controller.SwitchToManaged(r.Context(), user, deploymentID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	utils.JSONResponse(w, map[string]interface{}{
		"id":     updated.ID.String(),
		"status": updated.Status,
		"method": updated.Method,
	}, http.StatusOK)
}

func (v *HttpViews) SignUnsignedIPA(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	if requireActiveSubscription(w, user) {
		return
	}

	var signIPAReq requests.SignIPARequest
	if err := utils.ParseJSONBody(r, &signIPAReq); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	task, err := v.Controller.SignUnsignedIPATask(user, signIPAReq)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, map[string]string{"task_id": task.ID.String()}, http.StatusOK)
}

func (v *HttpViews) CheckSignIPATaskStatus(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	if requireActiveSubscription(w, user) {
		return
	}

	signTaskID := chi.URLParam(r, "signTaskId")
	if signTaskID == "" {
		utils.JSONErrorResponse(w, "sign task ID is required", http.StatusBadRequest)
		return
	}

	signTask, err := v.Controller.DB.GetIOSSignTaskById(signTaskID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	project, err := v.Controller.DB.GetProjectById(signTask.ProjectID.String())
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	if project.UserID.String() != user.ID.String() {
		utils.JSONErrorResponse(w, "you do not have access to this resource", http.StatusForbidden)
		return
	}

	utils.JSONResponse(w, map[string]any{
		"status":   signTask.Status,
		"message":  signTask.StatusMessage,
		"finished": signTask.Finished,
	}, http.StatusOK)
}

// GetBuildSignStatus returns the latest sign task for a given build so the
// build card UI can reflect signing state after a page refresh. Responds
// with 404 when no sign task has been run yet — the UI treats that as
// "unsigned, show Sign button".
func (v *HttpViews) GetBuildSignStatus(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if requireActiveSubscription(w, user) {
		return
	}

	buildID := chi.URLParam(r, "buildId")
	if buildID == "" {
		utils.JSONErrorResponse(w, "build ID is required", http.StatusBadRequest)
		return
	}

	signTask, err := v.Controller.GetLatestSignStatusForBuild(user, buildID)
	if err != nil {
		// sql.ErrNoRows is expected — surface as 404 so the client can
		// distinguish "never signed" from a real server error.
		if errors.Is(err, sql.ErrNoRows) {
			utils.JSONErrorResponse(w, "no sign task for this build", http.StatusNotFound)
			return
		}
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, map[string]any{
		"task_id":   signTask.ID.String(),
		"status":    signTask.Status,
		"message":   signTask.StatusMessage,
		"finished":  signTask.Finished,
		"build_id":  signTask.BuildID.String(),
		"created_at": signTask.CreatedAt,
	}, http.StatusOK)
}
