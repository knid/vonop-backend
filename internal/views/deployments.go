package views

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) GetDeployments(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	if requireActiveSubscription(w, v.Controller, user) {
		return
	}

	if rawOrgID := r.URL.Query().Get("org_id"); rawOrgID != "" {
		orgID, oErr := v.Controller.RequireOrgMembership(user, rawOrgID)
		if oErr != nil {
			utils.JSONErrorCode(w, constants.ErrForbidden, http.StatusForbidden, nil)
			return
		}
		deployments, dErr := v.Controller.DB.GetDeploymentsByOrgID(orgID.String())
		if dErr != nil {
			utils.JSONInternalError(w, dErr)
			return
		}
		var resp []responses.DeploymentResponse
		for _, deployment := range deployments {
			resp = append(resp, responses.DeploymentResponse{
				ID:                     deployment.ID,
				UserID:                 deployment.UserID,
				ProjectID:              deployment.ProjectID,
				BuildID:                deployment.BuildID,
				Platforms:              deployment.Platforms,
				IconID:                 deployment.IconPath,
				Title:                  deployment.Title,
				Description:            deployment.Description,
				Status:                 deployment.Status,
				ScreenshotIDs:          deployment.ScreenshotPaths,
				Method:                 deployment.Method,
				AppleConnectionID:      deployment.AppleConnectionID,
				GooglePlayConnectionID: deployment.GooglePlayConnectionID,
				CreatedAt:              deployment.CreatedAt,
				UpdatedAt:              deployment.UpdatedAt,
			})
		}
		utils.JSONResponse(w, resp, http.StatusOK)
		return
	}

	deployments, err := v.Controller.GetDeployments(user)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, deployments, http.StatusOK)
}

func (v *HttpViews) GetDeployment(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	if requireActiveSubscription(w, v.Controller, user) {
		return
	}

	deploymentID := chi.URLParam(r, "deploymentId")
	if deploymentID == "" {
		utils.JSONErrorCode(w, constants.ErrValidationRequired, http.StatusBadRequest, map[string]any{"field": "deployment_id"})
		return
	}
	deployment, err := v.Controller.GetDeploymentByID(deploymentID)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	if deployment.UserID.String() != user.ID.String() {
		utils.JSONErrorCode(w, constants.ErrForbidden, http.StatusForbidden, nil)
		return
	}

	utils.JSONResponse(w, deployment, http.StatusOK)
}

func (v *HttpViews) CreateDeployment(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	if requireActiveSubscription(w, v.Controller, user) {
		return
	}

	var deploymentCreateReq requests.CreateDeploymentRequest
	if err := utils.ParseJSONBody(r, &deploymentCreateReq); err != nil {
		utils.ValidationError(w, err)
		return
	}

	orgID, err := v.Controller.RequireOrgIDForUser(user, &deploymentCreateReq.OrganizationID)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	deployment, err := v.Controller.CreateDeploymentTask(user, orgID, deploymentCreateReq)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, deployment, http.StatusOK)
}

func (v *HttpViews) UpdateDeployment(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	if requireActiveSubscription(w, v.Controller, user) {
		return
	}

	var deploymentCreateReq requests.UpdateDeploymentRequest
	if err := utils.ParseJSONBody(r, &deploymentCreateReq); err != nil {
		utils.ValidationError(w, err)
		return
	}

	deploymentID := chi.URLParam(r, "deploymentId")
	if deploymentID == "" {
		utils.JSONErrorCode(w, constants.ErrValidationRequired, http.StatusBadRequest, map[string]any{"field": "deployment_id"})
		return
	}

	deployment, err := v.Controller.UpdateDeploymentTask(user, deploymentID, deploymentCreateReq)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, deployment, http.StatusOK)
}

// SwitchDeploymentToManaged handles POST /v1/deployments/:id/switch-to-managed.
// Flips a pending_connection deployment onto the Wemob-managed track.
func (v *HttpViews) SwitchDeploymentToManaged(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	if requireActiveSubscription(w, v.Controller, user) {
		return
	}

	deploymentID := chi.URLParam(r, "deploymentId")
	if deploymentID == "" {
		utils.JSONErrorCode(w, constants.ErrValidationRequired, http.StatusBadRequest, map[string]any{"field": "deployment_id"})
		return
	}
	updated, err := v.Controller.SwitchToManaged(r.Context(), user, deploymentID)
	if err != nil {
		utils.JSONInternalError(w, err)
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
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	if requireActiveSubscription(w, v.Controller, user) {
		return
	}

	var signIPAReq requests.SignIPARequest
	if err := utils.ParseJSONBody(r, &signIPAReq); err != nil {
		utils.ValidationError(w, err)
		return
	}

	task, err := v.Controller.SignUnsignedIPATask(user, signIPAReq)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, map[string]string{"task_id": task.ID.String()}, http.StatusOK)
}

func (v *HttpViews) CheckSignIPATaskStatus(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	if requireActiveSubscription(w, v.Controller, user) {
		return
	}

	signTaskID := chi.URLParam(r, "signTaskId")
	if signTaskID == "" {
		utils.JSONErrorCode(w, constants.ErrValidationRequired, http.StatusBadRequest, map[string]any{"field": "sign_task_id"})
		return
	}

	signTask, err := v.Controller.DB.GetIOSSignTaskById(signTaskID)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	project, err := v.Controller.DB.GetProjectById(signTask.ProjectID.String())
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}
	if project.UserID.String() != user.ID.String() {
		utils.JSONErrorCode(w, constants.ErrForbidden, http.StatusForbidden, nil)
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
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	if requireActiveSubscription(w, v.Controller, user) {
		return
	}

	buildID := chi.URLParam(r, "buildId")
	if buildID == "" {
		utils.JSONErrorCode(w, constants.ErrValidationRequired, http.StatusBadRequest, map[string]any{"field": "build_id"})
		return
	}

	signTask, err := v.Controller.GetLatestSignStatusForBuild(user, buildID)
	if err != nil {
		// sql.ErrNoRows is expected — surface as 404 so the client can
		// distinguish "never signed" from a real server error.
		if errors.Is(err, sql.ErrNoRows) {
			utils.JSONError(w, constants.ErrNotFound, "No sign task for this build.", http.StatusNotFound, nil)
			return
		}
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, map[string]any{
		"task_id":    signTask.ID.String(),
		"status":     signTask.Status,
		"message":    signTask.StatusMessage,
		"finished":   signTask.Finished,
		"build_id":   signTask.BuildID.String(),
		"created_at": signTask.CreatedAt,
	}, http.StatusOK)
}
