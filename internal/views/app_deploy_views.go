package views

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
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
			go v.Controller.TrackUserEvent(user, constants.EVENT_QUOTA_REACHED, posthog.NewProperties().Set("action", "deploy_web").Set("limit", limits.MaxDeployedWebApps))

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

	go v.Controller.TrackUserEvent(user, constants.EVENT_APP_DEPLOY_ENQUEUED, posthog.NewProperties().
		Set("app_id", project.ID.String()).
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

	v.Controller.TrackUserEvent(user, constants.EVENT_WEB_UNDEPLOYED, posthog.NewProperties().
		Set("app_id", project.ID.String()))

	go v.Controller.EventService.SendEvent(user, constants.EVENT_WEB_UNDEPLOYED, map[string]any{
		"app_id": project.ID.String(),
	})

	utils.JSONResponse(w, map[string]string{"status": "undeployed"}, http.StatusOK)
}

// POST /v2/apps/{appId}/deploy/mobile
//
// Only mobile_app_native projects are accepted — they publish via the
// self-hosted expo-updates pipeline. Other project types are rejected with
// HTTP 400 (the legacy WebView wrap-the-web-URL flow has been removed).
func (v *HttpViews) DeployAppMobile(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	if requireActiveSubscription(w, v.Controller, user) {
		return
	}

	appID := chi.URLParam(r, "appId")

	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}
	if project.ProjectType != models.ProjectTypeMobileAppNative {
		utils.JSONError(w, constants.ErrValidationBody,
			"this endpoint only accepts mobile_app_native projects",
			http.StatusBadRequest, nil)
		return
	}

	// Check mobile deploy permission and limits.
	// A5/A7/A8: limit lookup failures now fail closed. §B Phase 4: deploy
	// quota counts are org-scoped.
	limits, planName, limitsErr := v.Controller.GetUserLimitsWithPlan(user)
	if limitsErr != nil {
		utils.JSONError(w, constants.ErrSubscriptionRequired, "active subscription required", http.StatusForbidden, nil)
		return
	}
	if !limits.CanDeployMobile {
		go v.Controller.EventService.SendEvent(user, constants.EVENT_SUBSCRIPTION_NEED, map[string]any{"action": "deploy_mobile"})
		utils.JSONError(w, constants.ErrFeatureNotAvailable, "mobile deployments are not available on your plan", http.StatusForbidden, nil)
		return
	}

	// E3 Phase 7: bandwidth quota gate. OTA payloads count against the same
	// MonthlyBandwidthGB tally as web egress (Fly billing rolls them up
	// together via runtime_apps).
	if blocked := v.checkBandwidthQuota(w, r, user, limits, planName); blocked {
		return
	}

	if limits.MaxDeployedMobileApps > 0 {
		orgIDForDeploy, orgErr := v.Controller.RequireOrgIDForUser(user, nil)
		if orgErr != nil {
			utils.JSONInternalError(w, orgErr)
			return
		}
		count, _ := v.Controller.DB.CountDeployedMobileAppsByOrgID(orgIDForDeploy.String())
		alreadyDeployed, _ := v.Controller.DB.IsAppAlreadyDeployedMobile(appID)
		if !alreadyDeployed && count >= limits.MaxDeployedMobileApps {
			go v.Controller.EventService.SendEvent(user, constants.EVENT_SUBSCRIPTION_QUOTA_REACHED, map[string]any{"action": "deploy_mobile", "limit": limits.MaxDeployedMobileApps})
			go v.Controller.TrackUserEvent(user, constants.EVENT_QUOTA_REACHED, posthog.NewProperties().Set("action", "deploy_mobile").Set("limit", limits.MaxDeployedMobileApps))

			var deployedNames []string
			if apps, err := v.Controller.DB.GetDeployedMobileAppsByOrgID(orgIDForDeploy.String()); err == nil {
				for _, a := range apps {
					deployedNames = append(deployedNames, a.Name)
				}
			}

			utils.JSONError(w, constants.ErrDeployLimitReached,
				fmt.Sprintf("you have reached the maximum of %d deployed mobile apps for your plan", limits.MaxDeployedMobileApps),
				http.StatusForbidden, map[string]any{
					"deployed_apps": deployedNames,
					"used":          count,
					"limit":         limits.MaxDeployedMobileApps,
					"plan_name":     planName,
				})
			return
		}
	}

	// Native Expo apps publish via Codemagic's expo-ota-publish workflow
	// (~3 min vs 25 min for the legacy native rebuild). Installed clients
	// pick up the new JS bundle on next launch (no app store review).
	build, task, err := v.Controller.TriggerCodemagicOTAPublish(r.Context(), project, user)
	if err != nil {
		if errors.Is(err, controllers.ErrQuickPublishWrongType) {
			utils.JSONError(w, "wrong_project_type", err.Error(), http.StatusConflict, nil)
			return
		}
		utils.JSONInternalError(w, err)
		return
	}

	v.Controller.TrackUserEvent(user, constants.EVENT_APP_DEPLOYED_EXPO_OTA, posthog.NewProperties().
		Set("app_id", project.ID.String()).
		Set("build_id", build.ID.String()).
		Set("task_id", task.ID.String()))

	// Return the synthesized AppDeploymentResponse shape so the frontend's
	// existing deploy-status polling reads the same shape from the trigger
	// response and the subsequent /deploy/status calls.
	utils.JSONResponseStatus(w, synthesizeMobileDeployResponse(build, task), http.StatusAccepted)
}

// synthesizeMobileDeployResponse builds an AppDeploymentResponse from a
// Build + AppBuildTask pair. Used by GetDeployStatus / GetDeployHistory for
// mobile_app_native projects, which now publish via Codemagic and write to
// Build/AppBuildTask rather than AppDeployment. Frontend polling reads the
// same response shape regardless of project type.
func synthesizeMobileDeployResponse(build *models.Build, task *models.AppBuildTask) responses.AppDeploymentResponse {
	return responses.AppDeploymentResponse{
		ID:           task.ID,
		AppProjectID: build.AppProjectID,
		Platform:     "ota",
		Provider:     "codemagic",
		Status:       normalizeMobileDeployStatus(task.StatusText),
		Version:      build.BuildNumber,
		ErrorMessage: task.ErrorMessage,
		CreatedAt:    task.CreatedAt,
		UpdatedAt:    task.UpdatedAt,
	}
}

// normalizeMobileDeployStatus maps AppBuildTask.StatusText vocabulary
// ("Queued", "Building", "Built", "Failed", "Cancelled") into the
// AppDeployment status vocabulary the frontend already understands
// ("queued", "building", "deployed", "failed", "cancelled").
func normalizeMobileDeployStatus(statusText string) string {
	switch statusText {
	case "Built":
		return "deployed"
	case "Building":
		return "building"
	case "Queued":
		return "queued"
	case "Failed":
		return "failed"
	case "Cancelled":
		return "cancelled"
	}
	return strings.ToLower(statusText)
}

// DELETE /v2/apps/{appId}/deploy/mobile
//
// Only mobile_app_native projects are accepted; other project types are
// rejected with HTTP 400.
func (v *HttpViews) UndeployAppMobile(w http.ResponseWriter, r *http.Request) {
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
	if project.ProjectType != models.ProjectTypeMobileAppNative {
		utils.JSONError(w, constants.ErrValidationBody,
			"this endpoint only accepts mobile_app_native projects",
			http.StatusBadRequest, nil)
		return
	}

	if err := v.Controller.UndeployApp(project.ID.String()); err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	v.Controller.TrackUserEvent(user, constants.EVENT_MOBILE_UNDEPLOYED, posthog.NewProperties().
		Set("app_id", project.ID.String()))

	go v.Controller.EventService.SendEvent(user, constants.EVENT_MOBILE_UNDEPLOYED, map[string]any{
		"app_id": project.ID.String(),
	})

	utils.JSONResponse(w, map[string]string{"status": "undeployed"}, http.StatusOK)
}

// BuildAppExpo triggers an Expo (mobile_app_native) build.
//
// Rejects non-mobile_app_native projects with HTTP 400.
func (v *HttpViews) BuildAppExpo(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	if requireActiveSubscription(w, v.Controller, user) {
		return
	}

	limits, limitsErr := v.Controller.GetUserLimits(user)
	// A9: any paid plan unlocks CanBuildMobile (Starter + Pro), not just Pro.
	// Old message lied. A5/A7/A8: limitsErr now fails closed.
	if limitsErr != nil || !limits.CanBuildMobile {
		go v.Controller.EventService.SendEvent(user, constants.EVENT_SUBSCRIPTION_NEED, map[string]any{"action": "build_expo"})
		utils.JSONError(w, constants.ErrFeatureNotAvailable, "mobile builds are not available on your plan", http.StatusForbidden, nil)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}
	if project.ProjectType != models.ProjectTypeMobileAppNative {
		utils.JSONError(w, constants.ErrValidationBody,
			"this endpoint only accepts mobile_app_native projects",
			http.StatusBadRequest, nil)
		return
	}

	var req requests.ExpoBuildRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.ValidationError(w, err)
		return
	}
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := req.Validate(validate); err != nil {
		utils.ValidationError(w, err)
		return
	}

	build, tasks, triggerErr := v.Controller.BuildExpoMobile(r.Context(), project, user, controllers.ExpoBuildOptions{
		Platforms:        req.Platforms,
		AppName:          req.AppName,
		Icon:             req.Icon,
		SplashColor:      req.SplashColor,
		MarketingVersion: req.MarketingVersion,
		BundleIDOverride: req.BundleIDOverride,
	})

	// Partial-failure handling: surface per-task Failed status to the client
	// when at least one platform produced a task row, even if triggerErr is
	// non-nil.
	if triggerErr != nil && (build == nil || len(tasks) == 0) {
		var platformErr *controllers.BuildPlatformNotAllowedError
		var capErr *controllers.MemberCreditCapError
		switch {
		case errors.As(triggerErr, &platformErr):
			utils.JSONError(w, constants.ErrFeatureNotAvailable, platformErr.Error(), http.StatusForbidden, map[string]any{
				"platform": platformErr.Platform,
			})
		case errors.Is(triggerErr, controllers.ErrBuildAlreadyInProgress):
			utils.JSONErrorCode(w, constants.ErrBuildInProgress, http.StatusConflict, nil)
		case errors.As(triggerErr, &capErr):
			utils.JSONError(w, constants.ErrAICreditsDepleted, capErr.Error(), http.StatusPaymentRequired, map[string]any{
				"cap":       capErr.Cap,
				"spent":     capErr.Spent,
				"requested": capErr.Requested,
				"reason":    "member_credit_cap",
			})
		case errors.Is(triggerErr, controllers.ErrAICreditsFloorReached):
			utils.JSONError(w, constants.ErrAICreditsDepleted, triggerErr.Error(), http.StatusPaymentRequired, map[string]any{
				"reason": "overdraft_floor",
			})
		case errors.Is(triggerErr, controllers.ErrOrgInDunning):
			utils.JSONErrorCode(w, constants.ErrOrgInDunning, http.StatusPaymentRequired, nil)
		default:
			utils.JSONInternalError(w, triggerErr)
		}
		return
	}

	taskResponses := make([]map[string]any, 0, len(tasks))
	for _, t := range tasks {
		taskResponses = append(taskResponses, map[string]any{
			"task_id":       t.ID,
			"platform":      strings.ToLower(t.Platform),
			"status":        t.StatusText,
			"finished":      t.Finished,
			"error_message": t.ErrorMessage,
		})
	}

	for _, platform := range req.Platforms {
		v.Controller.TrackUserEvent(user, constants.EVENT_PREFIX_APP_BUILT_EXPO+constants.EventName(platform), posthog.NewProperties().
			Set("app_id", project.ID.String()).
			Set("project_type", project.ProjectType))
	}

	statusCode := http.StatusCreated
	allFailed := len(tasks) > 0 && triggerErr != nil
	if allFailed {
		statusCode = http.StatusMultiStatus
	}

	utils.JSONResponse(w, map[string]any{
		"build_id":   build.ID,
		"project_id": build.AppProjectID,
		"version":    build.BuildNumber,
		"tasks":      taskResponses,
	}, statusCode)
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

	// Mobile projects publish via Codemagic and write Build + AppBuildTask
	// rows, not AppDeployment. Synthesize an AppDeploymentResponse from the
	// latest build's task so the frontend's existing polling JustWorks.
	if project.ProjectType == models.ProjectTypeMobileAppNative {
		build, err := v.Controller.DB.GetLastBuildByAppProject(project.ID.String())
		if err != nil || build == nil {
			utils.JSONResponse(w, responses.AppDeploymentResponse{
				AppProjectID: project.ID,
				Platform:     "ota",
				Status:       "not_deployed",
			}, http.StatusOK)
			return
		}
		task, err := v.Controller.DB.GetLatestAppBuildTaskByBuildID(build.ID.String())
		if err != nil || task == nil {
			utils.JSONResponse(w, responses.AppDeploymentResponse{
				AppProjectID: project.ID,
				Platform:     "ota",
				Status:       "not_deployed",
			}, http.StatusOK)
			return
		}
		utils.JSONResponse(w, synthesizeMobileDeployResponse(build, task), http.StatusOK)
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

	// Mobile projects: synthesize from Build + AppBuildTask history.
	if project.ProjectType == models.ProjectTypeMobileAppNative {
		builds, err := v.Controller.DB.GetBuildsByAppProject(project.ID.String())
		if err != nil {
			utils.JSONInternalError(w, err)
			return
		}
		resp := make([]responses.AppDeploymentResponse, 0, len(builds))
		for i := range builds {
			b := builds[i]
			task, err := v.Controller.DB.GetLatestAppBuildTaskByBuildID(b.ID.String())
			if err != nil || task == nil {
				continue
			}
			resp = append(resp, synthesizeMobileDeployResponse(&b, task))
		}
		utils.JSONResponse(w, resp, http.StatusOK)
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

	token, err := utils.ExtractTokenFromHeader(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	result, err := v.Controller.E2BService.StartSandbox(token, appID)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrInternalUpstream, http.StatusBadGateway, nil)
		return
	}

	// Save sandbox ID if we got one
	if result.SandboxID != "" {
		_ = v.Controller.DB.UpdateAppProjectSandboxID(project.ID.String(), result.SandboxID)
	}

	if result.Status == "error" {
		utils.JSONResponse(w, result, http.StatusBadGateway)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
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

	if project.SandboxID == "" {
		utils.JSONResponse(w, map[string]string{
			"status":      "stopped",
			"preview_url": "",
			"sandbox_id":  "",
		}, http.StatusOK)
		return
	}

	utils.JSONResponse(w, map[string]string{
		"status":      "ready",
		"preview_url": v.Controller.E2BService.PreviewURL(project.SandboxID),
		"sandbox_id":  project.SandboxID,
	}, http.StatusOK)
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
