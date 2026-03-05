package views

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
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
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	var req requests.DeployWebRequest
	utils.ParseJSONBody(r, &req)

	target := "production"
	if req.Target != "" {
		target = req.Target
	}

	deployStart := time.Now()
	deployment, err := v.Controller.DeployToVercel(project, user, target)
	if err != nil {
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

// POST /v2/apps/{appId}/deploy/android
func (v *HttpViews) DeployAppAndroid(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if requireActiveSubscription(w, user) {
		return
	}
	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	var req requests.DeployFlutterRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	build, err := v.Controller.BuildFlutter(project, user, "android", req.PackageName, req.AppName)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	v.Controller.TrackUserEvent(user, "app_deployed_android", posthog.NewProperties().
		Set("app_id", project.ID.String()).
		Set("project_type", project.ProjectType))

	utils.JSONResponse(w, map[string]any{
		"build_id": build.ID,
		"platform": build.Platform,
		"status":   build.Status,
		"version":  build.Version,
	}, http.StatusCreated)
}

// POST /v2/apps/{appId}/deploy/ios
func (v *HttpViews) DeployAppIOS(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if requireActiveSubscription(w, user) {
		return
	}
	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	var req requests.DeployFlutterRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	build, err := v.Controller.BuildFlutter(project, user, "ios", req.PackageName, req.AppName)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	v.Controller.TrackUserEvent(user, "app_deployed_ios", posthog.NewProperties().
		Set("app_id", project.ID.String()).
		Set("project_type", project.ProjectType))

	utils.JSONResponse(w, map[string]any{
		"build_id": build.ID,
		"platform": build.Platform,
		"status":   build.Status,
		"version":  build.Version,
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
