package views

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
	"github.com/posthog/posthog-go"
)

// POST /v2/apps/{appId}/deploy/web
func (v *HttpViews) DeployAppWeb(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	if requireActiveSubscription(w, v.Controller, user) {
		return
	}

	appID := chi.URLParam(r, "appId")

	// Check web deploy permission and limits.
	// A5/A7/A8: limit lookup failures now fail closed. §B Phase 4: deploy
	// quota counts are org-scoped, so we look up the actor's org once.
	limits, planName, limitsErr := v.Controller.GetUserLimitsWithPlan(user)
	if limitsErr != nil {
		utils.JSONError(w, constants.ErrSubscriptionRequired, "active subscription required", http.StatusForbidden, nil)
		return
	}
	if !limits.CanDeployWeb {
		go v.Controller.EventService.SendEvent(user, constants.EVENT_SUBSCRIPTION_NEED, map[string]any{"action": "deploy_web"})
		utils.JSONError(w, constants.ErrFeatureNotAvailable, "web deployments are not available on your plan", http.StatusForbidden, nil)
		return
	}

	// E3 Phase 7: bandwidth quota gate. Reads the daily-cron-populated
	// app_bandwidth_usages row for the org's current billing period; blocks
	// new deploys when bytes_used >= MonthlyBandwidthGB. Existing deploys
	// keep serving traffic — auto-suspend on overage is a follow-up.
	if blocked := v.checkBandwidthQuota(w, r, user, limits, planName); blocked {
		return
	}

	if limits.MaxDeployedWebApps > 0 {
		orgIDForDeploy, orgErr := v.Controller.RequireOrgIDForUser(user, nil)
		if orgErr != nil {
			utils.JSONInternalError(w, orgErr)
			return
		}
		count, _ := v.Controller.DB.CountDeployedWebAppsByOrgID(orgIDForDeploy.String())
		// Allow re-deploys: if this app is already deployed, it doesn't count as a new slot
		alreadyDeployed, _ := v.Controller.DB.IsAppAlreadyDeployed(appID)
		if !alreadyDeployed && count >= limits.MaxDeployedWebApps {
			go v.Controller.EventService.SendEvent(user, constants.EVENT_SUBSCRIPTION_QUOTA_REACHED, map[string]any{"action": "deploy_web", "limit": limits.MaxDeployedWebApps})
			go v.Controller.TrackUserEvent(user, constants.EVENT_QUOTA_REACHED,
				v.Controller.AppEventProps(appID).
					Set("action", "deploy_web").
					Set("limit", limits.MaxDeployedWebApps))

			// Include deployed app names so the user knows which to undeploy
			var deployedNames []string
			if apps, err := v.Controller.DB.GetDeployedWebAppsByOrgID(orgIDForDeploy.String()); err == nil {
				for _, a := range apps {
					deployedNames = append(deployedNames, a.Name)
				}
			}

			utils.JSONError(w, constants.ErrDeployLimitReached,
				fmt.Sprintf("you have reached the maximum of %d deployed web apps for your plan", limits.MaxDeployedWebApps),
				http.StatusForbidden, map[string]any{
					"deployed_apps": deployedNames,
					"used":          count,
					"limit":         limits.MaxDeployedWebApps,
					"plan_name":     planName,
				})
			return
		}
	}

	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	// Phase B: web deploys also gated under build.trigger (cost + action)
	if project.OrganizationID != uuid.Nil {
		if _, err := v.Controller.RequirePermission(user, project.OrganizationID.String(), constants.PermBuildTrigger); err != nil {
			utils.JSONErrorCode(w, constants.ErrForbidden, http.StatusForbidden, nil)
			return
		}
	}

	if existing, err := v.Controller.DB.GetBuildingAppDeployment(project.ID.String()); err == nil && existing != nil && existing.ID.String() != "" {
		utils.JSONErrorCode(w, constants.ErrDeployInProgress, http.StatusConflict, nil)
		return
	}

	// Admission control — push back rather than queue forever.
	// Per-tier queue so free-tier saturation never pushes back on paid users.
	queueName := "deploy-web-free"
	// Plan-name-based routing is more robust than threshold-checking limit fields,
	// which can change as new plans are introduced.
	isPaid := planName != "" && planName != "free" && planName != "trial"
	if isPaid {
		queueName = "deploy-web-pro"
	}
	if v.Controller.AsynqInspector != nil {
		qi, _ := v.Controller.AsynqInspector.GetQueueInfo(queueName)
		if qi != nil && qi.Pending+qi.Active > 200 {
			utils.JSONError(w, constants.ErrSystemBusy,
				"deploy queue is currently saturated, please try again in a minute",
				http.StatusServiceUnavailable, map[string]any{
					"queue":       queueName,
					"queue_depth": qi.Pending,
				})
			return
		}
	}

	deployment, err := v.Controller.CreateQueuedWebDeployment(project, user)
	if err != nil {
		if errors.Is(err, controllers.ErrDeployInProgress) {
			utils.JSONErrorCode(w, constants.ErrDeployInProgress, http.StatusConflict, nil)
			return
		}
		utils.JSONInternalError(w, err)
		return
	}

	queue, err := v.Controller.EnqueueWebDeploy(deployment.ID, isPaid)
	if err != nil {
		v.Controller.MarkDeploymentFailed(deployment.ID.String(), "enqueue: "+err.Error())
		utils.JSONInternalError(w, err)
		return
	}

	go v.Controller.TrackUserEvent(user, constants.EVENT_APP_DEPLOY_ENQUEUED,
		v.Controller.AppEventProps(project.ID.String()).
			Set("platform", "web").
			Set("queue", queue))

	utils.JSONResponseStatus(w, responses.AppDeploymentResponse{
		ID:           deployment.ID,
		AppProjectID: deployment.AppProjectID,
		Platform:     deployment.Platform,
		Provider:     deployment.Provider,
		Status:       deployment.Status,
		Version:      deployment.Version,
	}, http.StatusAccepted)
}

// DELETE /v2/apps/{appId}/deploy/web
func (v *HttpViews) UndeployAppWeb(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	if err := v.Controller.UndeployApp(project.ID.String()); err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	v.Controller.TrackUserEvent(user, constants.EVENT_WEB_UNDEPLOYED,
		v.Controller.AppEventProps(project.ID.String()))

	go v.Controller.EventService.SendEvent(user, constants.EVENT_WEB_UNDEPLOYED, map[string]any{
		"app_id": project.ID.String(),
	})

	utils.JSONResponse(w, map[string]string{"status": "undeployed"}, http.StatusOK)
}

// GET /v2/apps/{appId}/deploy/status
func (v *HttpViews) GetDeployStatus(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	deployment, err := v.Controller.GetLatestDeploymentStatus(project.ID.String())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			utils.JSONResponse(w, responses.AppDeploymentResponse{
				AppProjectID: project.ID,
				Platform:     "web",
				Status:       "not_deployed",
			}, http.StatusOK)
			return
		}
		utils.JSONErrorCode(w, constants.ErrInternal, http.StatusInternalServerError, nil)
		return
	}

	utils.JSONResponse(w, responses.AppDeploymentResponse{
		ID:           deployment.ID,
		AppProjectID: deployment.AppProjectID,
		Platform:     deployment.Platform,
		Provider:     deployment.Provider,
		Status:       deployment.Status,
		DeployedURL:  deployment.DeployedURL,
		Version:      deployment.Version,
		ErrorMessage: deployment.ErrorMessage,
		ProviderData: deployment.ProviderData,
		CreatedAt:    deployment.CreatedAt,
		UpdatedAt:    deployment.UpdatedAt,
	}, http.StatusOK)
}

// GET /v2/apps/{appId}/deploy/history
func (v *HttpViews) GetDeployHistory(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	deployments, err := v.Controller.GetAppDeployments(project.ID.String())
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	var resp []responses.AppDeploymentResponse
	for _, d := range deployments {
		resp = append(resp, responses.AppDeploymentResponse{
			ID:           d.ID,
			AppProjectID: d.AppProjectID,
			Platform:     d.Platform,
			Provider:     d.Provider,
			Status:       d.Status,
			DeployedURL:  d.DeployedURL,
			Version:      d.Version,
			ErrorMessage: d.ErrorMessage,
			ProviderData: d.ProviderData,
			CreatedAt:    d.CreatedAt,
			UpdatedAt:    d.UpdatedAt,
		})
	}

	utils.JSONResponse(w, resp, http.StatusOK)
}

// GET /v2/apps/{appId}/preview-bundle
func (v *HttpViews) GetPreviewBundle(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	bundle, err := v.Controller.GetPreviewBundle(project)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, bundle, http.StatusOK)
}

// GET /v2/apps/{appId}/sandbox
func (v *HttpViews) GetAppSandbox(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	utils.JSONResponse(w, map[string]string{"sandbox_id": project.SandboxID}, http.StatusOK)
}

// PUT /v2/apps/{appId}/sandbox
func (v *HttpViews) SetAppSandbox(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	var body struct {
		SandboxID string `json:"sandbox_id"`
	}
	if err := utils.ParseJSONBody(r, &body); err != nil {
		utils.ValidationError(w, err)
		return
	}

	if err := v.Controller.DB.UpdateAppProjectSandboxID(project.ID.String(), body.SandboxID); err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, map[string]string{"sandbox_id": body.SandboxID}, http.StatusOK)
}

// POST /v2/apps/{appId}/sandbox/start
func (v *HttpViews) StartAppSandbox(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	status, err := v.Controller.StartSandbox(r.Context(), project)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, status, http.StatusOK)
}

// GET /v2/apps/{appId}/sandbox/status
func (v *HttpViews) GetAppSandboxStatus(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	status, err := v.Controller.SandboxStatus(r.Context(), project)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}
	utils.JSONResponse(w, status, http.StatusOK)
}

// POST /v2/apps/{appId}/deploy/validate
func (v *HttpViews) ValidateAppFiles(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	result, err := v.Controller.ValidateAppFiles(r.Context(), project.ID.String(), user)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}

// checkBandwidthQuota gates a deploy on the org's MonthlyBandwidthGB. Returns
// true when the deploy must be blocked (response already written). Bandwidth
// usage comes from app_bandwidth_usages, populated by the daily Fly GraphQL
// sync cron (E3 Phase 7); a missing row means "no usage tracked yet, allow".
func (v *HttpViews) checkBandwidthQuota(w http.ResponseWriter, r *http.Request, user *models.User, limits *models.StandardUserLimits, planName string) bool {
	if limits == nil || limits.MonthlyBandwidthGB <= 0 {
		return false
	}
	org, orgErr := v.Controller.DB.GetPersonalOrgByUserID(user.ID.String())
	if orgErr != nil || org == nil {
		// Best-effort: don't lock users out of deploying because the org
		// lookup hiccupped. The cron will catch up next pass.
		return false
	}
	periodStart, periodEnd := utils.BillingPeriod(org.SubscriptionStartAt, time.Now())
	usage, _ := v.Controller.DB.GetBandwidthUsageForOrgPeriod(r.Context(), org.ID, periodStart)
	const bytesPerGB = int64(1024 * 1024 * 1024)
	limitBytes := int64(limits.MonthlyBandwidthGB) * bytesPerGB
	if usage == nil || usage.BytesUsed < limitBytes {
		return false
	}
	go v.Controller.EventService.SendEvent(user, constants.EVENT_SUBSCRIPTION_QUOTA_REACHED, map[string]any{
		"action":     "bandwidth",
		"limit_gb":   limits.MonthlyBandwidthGB,
		"bytes_used": usage.BytesUsed,
	})
	go v.Controller.TrackUserEvent(user, constants.EVENT_QUOTA_REACHED, posthog.NewProperties().
		Set("action", "bandwidth").
		Set("limit_gb", limits.MonthlyBandwidthGB).
		Set("bytes_used", usage.BytesUsed))
	utils.JSONError(w, constants.ErrDeployLimitReached,
		fmt.Sprintf("you have used %d GB of %d GB bandwidth for this billing period",
			usage.BytesUsed/bytesPerGB, limits.MonthlyBandwidthGB),
		http.StatusForbidden, map[string]any{
			"bytes_used":   usage.BytesUsed,
			"limit_gb":     limits.MonthlyBandwidthGB,
			"renewal_date": periodEnd.Format(time.RFC3339),
			"plan_name":    planName,
		})
	return true
}
