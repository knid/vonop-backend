package views

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
	"github.com/posthog/posthog-go"
)

// POST /v2/apps/{appId}/deploy/web
func (v *HttpViews) DeployAppWeb(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if requireActiveSubscription(w, user) {
		return
	}

	appID := chi.URLParam(r, "appId")

	// Check web deploy permission and limits
	limits, limitsErr := v.Controller.DB.GetStandartUserLimitsByUser(user)
	if limitsErr == nil {
		if !limits.CanDeployWeb {
			go v.Controller.CRMService.SendEvent(user, constants.BREVO_EVENT_SUBSCRIPTION_NEED, map[string]any{"action": "deploy_web"})
			utils.JSONCodedErrorResponse(w, "FEATURE_NOT_AVAILABLE", "web deployments are not available on your plan", http.StatusForbidden, nil)
			return
		}

		if limits.MaxDeployedWebApps > 0 {
			count, _ := v.Controller.DB.CountDeployedWebAppsByUserID(user.ID.String())
			// Allow re-deploys: if this app is already deployed, it doesn't count as a new slot
			alreadyDeployed, _ := v.Controller.DB.IsAppAlreadyDeployed(appID)
			if !alreadyDeployed && count >= limits.MaxDeployedWebApps {
				go v.Controller.CRMService.SendEvent(user, constants.BREVO_EVENT_SUBSCRIPTION_QUOTA_REACHED, map[string]any{"action": "deploy_web", "limit": limits.MaxDeployedWebApps})
				go v.Controller.TrackUserEvent(user, "quota_reached", posthog.NewProperties().Set("action", "deploy_web").Set("limit", limits.MaxDeployedWebApps))

				// Include deployed app names so the user knows which to undeploy
				var deployedNames []string
				if apps, err := v.Controller.DB.GetDeployedWebAppsByUserID(user.ID.String()); err == nil {
					for _, a := range apps {
						deployedNames = append(deployedNames, a.Name)
					}
				}

				utils.JSONCodedErrorResponse(w, "DEPLOY_LIMIT_REACHED",
					fmt.Sprintf("you have reached the maximum of %d deployed web apps for your plan", limits.MaxDeployedWebApps),
					http.StatusForbidden, map[string]any{
						"deployed_apps": deployedNames,
						"limit":         limits.MaxDeployedWebApps,
					})
				return
			}
		}

		if limits.MonthlyDeployCount > 0 {
			periodStart, _ := utils.BillingPeriod(user.SubscriptionStartAt, time.Now())
			deployCount, _ := v.Controller.DB.CountWebDeploysSince(user.ID.String(), periodStart)
			if deployCount >= limits.MonthlyDeployCount {
				go v.Controller.CRMService.SendEvent(user, constants.BREVO_EVENT_SUBSCRIPTION_QUOTA_REACHED, map[string]any{"action": "deploy_web_monthly", "limit": limits.MonthlyDeployCount})
				go v.Controller.TrackUserEvent(user, "quota_reached", posthog.NewProperties().Set("action", "deploy_web_monthly").Set("limit", limits.MonthlyDeployCount))
				utils.JSONCodedErrorResponse(w, "DEPLOY_LIMIT_REACHED",
					fmt.Sprintf("you have reached your monthly deploy limit (%d)", limits.MonthlyDeployCount),
					http.StatusForbidden, nil)
				return
			}
		}
	}

	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	deployStart := time.Now()
	deployment, err := v.Controller.DeployToCoolify(project, user)
	if err != nil {
		go v.Controller.TrackUserEvent(user, "app_deploy_failed", posthog.NewProperties().
			Set("app_id", project.ID.String()).
			Set("platform", "web").
			Set("error", err.Error()))
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}
	deployDuration := time.Since(deployStart).Seconds()

	v.Controller.TrackUserEvent(user, "app_deployed_web", posthog.NewProperties().
		Set("app_id", project.ID.String()).
		Set("project_type", project.ProjectType).
		Set("deploy_duration_seconds", deployDuration).
		Set("deploy_version", deployment.Version))

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

// DELETE /v2/apps/{appId}/deploy/web
func (v *HttpViews) UndeployAppWeb(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	if err := v.Controller.UndeployApp(project.ID.String()); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	v.Controller.TrackUserEvent(user, "app_undeployed_web", posthog.NewProperties().
		Set("app_id", project.ID.String()))

	utils.JSONResponse(w, map[string]string{"status": "undeployed"}, http.StatusOK)
}

// POST /v2/apps/{appId}/build/android
func (v *HttpViews) BuildAppAndroid(w http.ResponseWriter, r *http.Request) {
	v.buildAppMobile(w, r, "android")
}

// POST /v2/apps/{appId}/build/ios
func (v *HttpViews) BuildAppIOS(w http.ResponseWriter, r *http.Request) {
	v.buildAppMobile(w, r, "ios")
}

func (v *HttpViews) buildAppMobile(w http.ResponseWriter, r *http.Request, platform string) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if requireActiveSubscription(w, user) {
		return
	}

	limits, limitsErr := v.Controller.DB.GetStandartUserLimitsByUser(user)
	if limitsErr != nil || !limits.CanBuildMobile {
		go v.Controller.CRMService.SendEvent(user, constants.BREVO_EVENT_SUBSCRIPTION_NEED, map[string]any{"action": "build_mobile", "platform": platform})
		utils.JSONCodedErrorResponse(w, "FEATURE_NOT_AVAILABLE", "mobile builds require Professional plan", http.StatusForbidden, nil)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	var req requests.MobileBuildRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := req.Validate(validate); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	build, task, err := v.Controller.BuildAppMobile(project, user, platform, req.AppName, req.Icon, req.SplashScreenColor)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

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
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	deployment, err := v.Controller.GetLatestDeploymentStatus(project.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, "no deployments found", http.StatusNotFound)
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
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	deployments, err := v.Controller.GetAppDeployments(project.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
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
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	bundle, err := v.Controller.GetPreviewBundle(project)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, bundle, http.StatusOK)
}

// GET /v2/apps/{appId}/sandbox
func (v *HttpViews) GetAppSandbox(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	utils.JSONResponse(w, map[string]string{"sandbox_id": project.SandboxID}, http.StatusOK)
}

// PUT /v2/apps/{appId}/sandbox
func (v *HttpViews) SetAppSandbox(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	var body struct {
		SandboxID string `json:"sandbox_id"`
	}
	if err := utils.ParseJSONBody(r, &body); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := v.Controller.DB.UpdateAppProjectSandboxID(project.ID.String(), body.SandboxID); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, map[string]string{"sandbox_id": body.SandboxID}, http.StatusOK)
}

// POST /v2/apps/{appId}/deploy/validate
func (v *HttpViews) ValidateAppFiles(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	result, err := v.Controller.ValidateAppFiles(project.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}
