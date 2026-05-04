package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/constants"
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

