package views

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/controllers"
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

	// Check web deploy permission and limits
	limits, planName, limitsErr := v.Controller.GetUserLimitsWithPlan(user)
	if limitsErr == nil {
		if !limits.CanDeployWeb {
			go v.Controller.EventService.SendEvent(user, constants.EVENT_SUBSCRIPTION_NEED, map[string]any{"action": "deploy_web"})
			utils.JSONError(w, constants.ErrFeatureNotAvailable, "web deployments are not available on your plan", http.StatusForbidden, nil)
			return
		}

		if limits.MaxDeployedWebApps > 0 {
			count, _ := v.Controller.DB.CountDeployedWebAppsByUserID(user.ID.String())
			// Allow re-deploys: if this app is already deployed, it doesn't count as a new slot
			alreadyDeployed, _ := v.Controller.DB.IsAppAlreadyDeployed(appID)
			if !alreadyDeployed && count >= limits.MaxDeployedWebApps {
				go v.Controller.EventService.SendEvent(user, constants.EVENT_SUBSCRIPTION_QUOTA_REACHED, map[string]any{"action": "deploy_web", "limit": limits.MaxDeployedWebApps})
				go v.Controller.TrackUserEvent(user, "quota_reached", posthog.NewProperties().Set("action", "deploy_web").Set("limit", limits.MaxDeployedWebApps))

				// Include deployed app names so the user knows which to undeploy
				var deployedNames []string
				if apps, err := v.Controller.DB.GetDeployedWebAppsByUserID(user.ID.String()); err == nil {
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

	go v.Controller.TrackUserEvent(user, "app_deploy_enqueued", posthog.NewProperties().
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

	v.Controller.TrackUserEvent(user, "app_undeployed_web", posthog.NewProperties().
		Set("app_id", project.ID.String()))

	go v.Controller.EventService.SendEvent(user, constants.EVENT_WEB_UNDEPLOYED, map[string]any{
		"app_id": project.ID.String(),
	})

	utils.JSONResponse(w, map[string]string{"status": "undeployed"}, http.StatusOK)
}

// POST /v2/apps/{appId}/deploy/mobile
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

	// Check mobile deploy permission and limits
	limits, planName, limitsErr := v.Controller.GetUserLimitsWithPlan(user)
	if limitsErr == nil {
		if !limits.CanDeployMobile {
			go v.Controller.EventService.SendEvent(user, constants.EVENT_SUBSCRIPTION_NEED, map[string]any{"action": "deploy_mobile"})
			utils.JSONError(w, constants.ErrFeatureNotAvailable, "mobile deployments are not available on your plan", http.StatusForbidden, nil)
			return
		}

		if limits.MaxDeployedMobileApps > 0 {
			count, _ := v.Controller.DB.CountDeployedMobileAppsByUserID(user.ID.String())
			alreadyDeployed, _ := v.Controller.DB.IsAppAlreadyDeployedMobile(appID)
			if !alreadyDeployed && count >= limits.MaxDeployedMobileApps {
				go v.Controller.EventService.SendEvent(user, constants.EVENT_SUBSCRIPTION_QUOTA_REACHED, map[string]any{"action": "deploy_mobile", "limit": limits.MaxDeployedMobileApps})
				go v.Controller.TrackUserEvent(user, "quota_reached", posthog.NewProperties().Set("action", "deploy_mobile").Set("limit", limits.MaxDeployedMobileApps))

				var deployedNames []string
				if apps, err := v.Controller.DB.GetDeployedMobileAppsByUserID(user.ID.String()); err == nil {
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

	// Mobile builds wrap a deployed web URL — require the web app to already
	// be deployed. The user-facing flow on the frontend pushes them through
	// the web deploy first, so this is mainly a safety check.
	deployment, err := v.Controller.DB.GetLatestAppDeployment(project.ID.String(), "web")
	if err != nil || deployment == nil || deployment.Status != "deployed" || deployment.DeployedURL == "" {
		utils.JSONError(w, constants.ErrDeployInProgress,
			"deploy this app to the web first; mobile builds wrap the web URL",
			http.StatusConflict, nil)
		return
	}

	v.Controller.TrackUserEvent(user, "app_deployed_mobile", posthog.NewProperties().
		Set("app_id", project.ID.String()).
		Set("project_type", project.ProjectType).
		Set("deploy_version", deployment.Version))

	go v.Controller.EventService.SendEvent(user, constants.EVENT_MOBILE_DEPLOYMENT_SUCCEEDED, map[string]any{
		"app_id":       project.ID.String(),
		"project_type": project.ProjectType,
		"version":      deployment.Version,
		"deployed_url": deployment.DeployedURL,
	})

	utils.JSONResponse(w, responses.AppDeploymentResponse{
		ID:           deployment.ID,
		AppProjectID: deployment.AppProjectID,
		Platform:     deployment.Platform,
		Provider:     deployment.Provider,
		Status:       deployment.Status,
		DeployedURL:  deployment.DeployedURL,
		Version:      deployment.Version,
		CreatedAt:    deployment.CreatedAt,
		UpdatedAt:    deployment.UpdatedAt,
	}, http.StatusCreated)
}

// DELETE /v2/apps/{appId}/deploy/mobile
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

	if err := v.Controller.UndeployApp(project.ID.String()); err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	v.Controller.TrackUserEvent(user, "app_undeployed_mobile", posthog.NewProperties().
		Set("app_id", project.ID.String()))

	go v.Controller.EventService.SendEvent(user, constants.EVENT_MOBILE_UNDEPLOYED, map[string]any{
		"app_id": project.ID.String(),
	})

	utils.JSONResponse(w, map[string]string{"status": "undeployed"}, http.StatusOK)
}

// POST /v2/apps/{appId}/build/android — single-platform, kept for back-compat
// with existing clients. New UI should use BuildAppMobileMulti.
func (v *HttpViews) BuildAppAndroid(w http.ResponseWriter, r *http.Request) {
	v.buildAppMobile(w, r, "android")
}

// POST /v2/apps/{appId}/build/ios — single-platform, kept for back-compat.
func (v *HttpViews) BuildAppIOS(w http.ResponseWriter, r *http.Request) {
	v.buildAppMobile(w, r, "ios")
}

// POST /v2/apps/{appId}/build — combined endpoint that enqueues Android
// and/or iOS in a single call. Returns per-platform task status so partial
// trigger failures are visible to the client.
func (v *HttpViews) BuildAppMobileMulti(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	if requireActiveSubscription(w, v.Controller, user) {
		return
	}

	limits, limitsErr := v.Controller.GetUserLimits(user)
	if limitsErr != nil || !limits.CanBuildMobile {
		go v.Controller.EventService.SendEvent(user, constants.EVENT_SUBSCRIPTION_NEED, map[string]any{"action": "build_mobile"})
		utils.JSONError(w, constants.ErrFeatureNotAvailable, "mobile builds require Professional plan", http.StatusForbidden, nil)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	var req requests.MultiPlatformMobileBuildRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.ValidationError(w, err)
		return
	}
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := req.Validate(validate); err != nil {
		utils.ValidationError(w, err)
		return
	}

	build, tasks, triggerErr := v.Controller.BuildAppMobile(project, user, req.Platforms, req.AppName, req.Icon, req.SplashScreenColor)

	// Partial failures (one platform succeeded, another failed) return
	// triggerErr=nil with per-task Failed status. Total failure returns a
	// non-nil triggerErr AND the tasks that were persisted with Failed
	// status — we still surface those to the client so the UI can show
	// the per-platform error messages instead of a bare 500.
	if triggerErr != nil && (build == nil || len(tasks) == 0) {
		utils.JSONInternalError(w, triggerErr)
		return
	}

	// Per-platform task envelopes so the client can show per-platform
	// status (including "Failed" for triggers that errored out).
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
		v.Controller.TrackUserEvent(user, "app_deployed_"+platform, posthog.NewProperties().
			Set("app_id", project.ID.String()).
			Set("project_type", project.ProjectType))
	}

	// 207 Multi-Status when every platform's trigger failed but we still
	// have tasks to show. 201 Created for the normal (including partial
	// success) path. The client treats both as "inspect tasks".
	statusCode := http.StatusCreated
	allFailed := len(tasks) > 0 && triggerErr != nil
	if allFailed {
		statusCode = http.StatusMultiStatus
	}

	utils.JSONResponse(w, map[string]any{
		"build_id":   build.ID,
		"project_id": build.ProjectID,
		"version":    build.BuildNumber,
		"tasks":      taskResponses,
	}, statusCode)
}

func (v *HttpViews) buildAppMobile(w http.ResponseWriter, r *http.Request, platform string) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	if requireActiveSubscription(w, v.Controller, user) {
		return
	}

	limits, limitsErr := v.Controller.GetUserLimits(user)
	if limitsErr != nil || !limits.CanBuildMobile {
		go v.Controller.EventService.SendEvent(user, constants.EVENT_SUBSCRIPTION_NEED, map[string]any{"action": "build_mobile", "platform": platform})
		utils.JSONError(w, constants.ErrFeatureNotAvailable, "mobile builds require Professional plan", http.StatusForbidden, nil)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	var req requests.MobileBuildRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.ValidationError(w, err)
		return
	}

	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := req.Validate(validate); err != nil {
		utils.ValidationError(w, err)
		return
	}

	build, tasks, err := v.Controller.BuildAppMobile(project, user, []string{platform}, req.AppName, req.Icon, req.SplashScreenColor)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}
	if len(tasks) == 0 {
		utils.JSONErrorCode(w, constants.ErrInternal, http.StatusInternalServerError, nil)
		return
	}
	task := tasks[0]

	v.Controller.TrackUserEvent(user, "app_deployed_"+platform, posthog.NewProperties().
		Set("app_id", project.ID.String()).
		Set("project_type", project.ProjectType))

	utils.JSONResponse(w, map[string]any{
		"build_id":   build.ID,
		"task_id":    task.ID,
		"project_id": build.ProjectID,
		"platform":   platform,
		"status":     task.StatusText,
		"version":    build.BuildNumber,
	}, http.StatusCreated)
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

	result, err := v.Controller.ValidateAppFiles(project.ID.String())
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}
