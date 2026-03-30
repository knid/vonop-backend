package views

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
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

// POST /v2/apps/generate/from-image
func (v *HttpViews) GenerateAppFromImage(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if requireActiveSubscription(w, user) {
		return
	}
	if v.checkMinimumCredits(w, user) {
		return
	}

	var req requests.ScreenshotToAppRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Token tracking for post-hoc billing
	tracker := utils.NewTokenTracker()
	tracker.UserID = user.ID.String()
	ctx := utils.WithTokenTracker(r.Context(), tracker)

	structure, err := v.Controller.PlanAppFileStructureFromScreenshot(ctx, req.ImageBase64)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	genReq := requests.GenerateAppRequest{Prompt: "Generated from screenshot"}
	project, err := v.Controller.CreateAppProject(r.Context(), user, genReq)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if structure.Name != "" {
		project.Name = structure.Name
	}
	if structure.Slug != "" {
		project.Slug = structure.Slug
	}
	themeData := models.AppTheme{
		ColorScheme: structure.ColorScheme,
		Typography:  structure.Typography,
	}
	themeJSON, _ := json.Marshal(themeData)
	project.Theme = themeJSON
	project.Status = "ready"
	v.Controller.ApplyStructureToProject(project, structure)
	v.Controller.DB.UpdateAppProject(project)

	var theme map[string]any
	json.Unmarshal(project.Theme, &theme)

	utils.JSONResponse(w, responses.AppProjectDetailResponse{
		AppProjectResponse: responses.AppProjectResponse{
			ID:          project.ID,
			Name:        project.Name,
			Slug:        project.Slug,
			Prompt:      project.Prompt,
			ProjectType: project.ProjectType,
			Theme:       theme,
			Settings:    project.Settings,
			Status:      project.Status,
			CreatedAt:   project.CreatedAt,
			UpdatedAt:   project.UpdatedAt,
		},
	}, http.StatusCreated)

	v.Controller.RecordTokenUsage(user, "generate_from_image",
		v.Controller.SecretManager.GetSecret("DEFAULT_LLM_MODEL"),
		v.Controller.LLMProvider().Name, project.ID.String(), "standard", tracker)

	// Post-hoc token-based billing
	prompt, completion, total, _ := tracker.Totals()
	if total > 0 {
		credits := models.TokensToCredits(prompt, completion)
		if err := v.Controller.DeductAICreditsUnchecked(user, credits, "generate_from_image", project.ID.String()); err != nil {
			slog.Warn("failed to deduct AI credits from tokens", "error", err,
				"user_id", user.ID, "credits", credits, "tokens", total)
		}
	}

	v.Controller.TrackUserEvent(user, "app_generated_from_image", posthog.NewProperties().
		Set("app_id", project.ID.String()))
}

// GET /v2/apps/{appId}/logo — public endpoint that redirects to a fresh pre-signed S3 URL.
// No auth required — logos are public assets embedded in generated app code.
func (v *HttpViews) GetAppLogo(w http.ResponseWriter, r *http.Request) {
	appId := chi.URLParam(r, "appId")
	if appId == "" {
		http.NotFound(w, r)
		return
	}

	project, err := v.Controller.DB.GetAppProjectByID(appId)
	if err != nil || project == nil || project.LogoS3Key == "" {
		http.NotFound(w, r)
		return
	}

	presigned, err := v.Controller.AssetsStorage.GetObject(r.Context(), project.LogoS3Key, 3600)
	if err != nil {
		slog.Error("GetAppLogo: failed to presign", "error", err, "app_id", appId)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.Redirect(w, r, presigned.URL, http.StatusFound)
}

// GET /v2/apps/
func (v *HttpViews) GetApps(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	var types []string
	if typeParam := r.URL.Query().Get("type"); typeParam != "" {
		types = strings.Split(typeParam, ",")
	}

	projects, err := v.Controller.GetAppProjectsByUserFiltered(user, types)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Auto-heal: check if any "generating" apps are stale (no active job)
	hasGenerating := false
	for _, p := range projects {
		if p.Status == "generating" {
			hasGenerating = true
			break
		}
	}
	var activeGenJobTypes map[string]bool
	if hasGenerating {
		activeGenJobTypes = make(map[string]bool)
		activeJobs, _ := v.Controller.GetActiveJobs(user.ID)
		for _, j := range activeJobs {
			activeGenJobTypes[j.Type] = true
		}
	}

	var resp []responses.AppProjectResponse
	for _, p := range projects {
		var theme map[string]any
		json.Unmarshal(p.Theme, &theme)

		hasStore := false
		store, err := v.Controller.GetStore(p.ID.String())
		if err == nil && store != nil {
			hasStore = true
		}

		status := p.Status
		if status == "generating" && activeGenJobTypes != nil &&
			!activeGenJobTypes["generate_app"] && !activeGenJobTypes["generate_ecommerce_app"] {
			status = "ready"
			go v.Controller.UpdateAppProjectStatus(&p, "ready")
		}

		var screenshotURL string
		if p.ScreenshotS3Key != "" {
			if presigned, err := v.Controller.AssetsStorage.GetObject(r.Context(), p.ScreenshotS3Key, 3600); err == nil {
				screenshotURL = presigned.URL
			}
		}

		resp = append(resp, responses.AppProjectResponse{
			ID:            p.ID,
			Name:          p.Name,
			Slug:          p.Slug,
			Prompt:        p.Prompt,
			ProjectType:   p.ProjectType,
			Theme:         theme,
			Settings:      p.Settings,
			Status:        status,
			HasStore:      hasStore,
			ScreenshotURL: screenshotURL,
			CreatedAt:     p.CreatedAt,
			UpdatedAt:     p.UpdatedAt,
		})
	}

	utils.JSONResponse(w, resp, http.StatusOK)
}

// GET /v2/apps/{appId}
func (v *HttpViews) GetApp(w http.ResponseWriter, r *http.Request) {
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

	var theme map[string]any
	json.Unmarshal(project.Theme, &theme)

	hasStore := false
	store, storeErr := v.Controller.GetStore(project.ID.String())
	if storeErr == nil && store != nil {
		hasStore = true
	}

	// Auto-heal: if status is "generating" but no active generation job exists, update to "ready"
	if project.Status == "generating" {
		activeJobs, _ := v.Controller.GetActiveJobs(user.ID)
		hasActiveGenJob := false
		for _, j := range activeJobs {
			if j.Type == "generate_app" || j.Type == "generate_ecommerce_app" {
				hasActiveGenJob = true
				break
			}
		}
		if !hasActiveGenJob {
			project.Status = "ready"
			v.Controller.UpdateAppProjectStatus(project, "ready")
		}
	}

	// Get files for the response
	fileMap, _ := v.Controller.GetAppGeneratedFiles(project.ID.String())
	var fileResp []responses.AppFileResponse
	for path, content := range fileMap {
		if path == ".env.local" {
			continue
		}
		fileResp = append(fileResp, responses.AppFileResponse{
			FilePath: path,
			Content:  content,
			FileType: controllers.InferFileType(path),
		})
	}

	var screenshotURL string
	if project.ScreenshotS3Key != "" {
		if presigned, err := v.Controller.AssetsStorage.GetObject(r.Context(), project.ScreenshotS3Key, 3600); err == nil {
			screenshotURL = presigned.URL
		}
	}

	utils.JSONResponse(w, responses.AppProjectDetailResponse{
		AppProjectResponse: responses.AppProjectResponse{
			ID:            project.ID,
			Name:          project.Name,
			Slug:          project.Slug,
			Prompt:        project.Prompt,
			ProjectType:   project.ProjectType,
			Theme:         theme,
			Settings:      project.Settings,
			Status:        project.Status,
			HasStore:      hasStore,
			ScreenshotURL: screenshotURL,
			CreatedAt:     project.CreatedAt,
			UpdatedAt:     project.UpdatedAt,
		},
		Files: fileResp,
	}, http.StatusOK)
}

// PUT /v2/apps/{appId}
func (v *HttpViews) UpdateApp(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	var req requests.UpdateAppProjectRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	project, err := v.Controller.UpdateAppProject(appID, user, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	var theme map[string]any
	json.Unmarshal(project.Theme, &theme)

	v.Controller.TrackUserEvent(user, "app_updated", posthog.NewProperties().
		Set("app_id", project.ID.String()))

	utils.JSONResponse(w, responses.AppProjectResponse{
		ID:          project.ID,
		Name:        project.Name,
		Slug:        project.Slug,
		Prompt:      project.Prompt,
		ProjectType: project.ProjectType,
		Theme:       theme,
		Settings:    project.Settings,
		Status:      project.Status,
		CreatedAt:   project.CreatedAt,
		UpdatedAt:   project.UpdatedAt,
	}, http.StatusOK)
}

// DELETE /v2/apps/{appId}
func (v *HttpViews) DeleteApp(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	if err := v.Controller.DeleteAppProject(appID, user); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	v.Controller.TrackUserEvent(user, "app_deleted", posthog.NewProperties().
		Set("app_id", appID))

	utils.JSONResponse(w, responses.SuccessResponse{Success: true}, http.StatusOK)
}

// GET /v2/apps/{appId}/files/{platform}
func (v *HttpViews) GetAppFiles(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	platform := chi.URLParam(r, "platform")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	fileMap, err := v.Controller.GetAppGeneratedFiles(project.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = platform // platform param kept for API compatibility

	var resp []responses.AppFileResponse
	for path, content := range fileMap {
		if path == ".env.local" {
			continue
		}
		resp = append(resp, responses.AppFileResponse{
			FilePath: path,
			Content:  content,
			FileType: controllers.InferFileType(path),
		})
	}

	utils.JSONResponse(w, resp, http.StatusOK)
}

// POST /v2/apps/{appId}/files — Create a new file
func (v *HttpViews) CreateAppFile(w http.ResponseWriter, r *http.Request) {
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

	var req requests.AppFileCreateRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	validate := validator.New()
	if err := req.Validate(validate); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := v.Controller.CreateAppFile(project, req.FilePath, req.Content, req.FileType); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	v.Controller.TrackUserEvent(user, "app_file_created", posthog.NewProperties().
		Set("app_id", project.ID.String()).
		Set("file_path", req.FilePath))

	utils.JSONResponse(w, responses.AppFileResponse{
		FilePath: req.FilePath,
		Content:  req.Content,
		FileType: req.FileType,
	}, http.StatusCreated)
}

// PUT /v2/apps/{appId}/files/{fid} — Update a file's content
// {fid} is now treated as a URL-encoded file path for backwards compatibility.
func (v *HttpViews) UpdateAppFile(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	fid, _ := url.PathUnescape(chi.URLParam(r, "fid"))
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	var req requests.AppFileUpdateRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	validate := validator.New()
	if err := req.Validate(validate); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	// fid is the file path
	if err := v.Controller.UpdateAppFile(project, fid, req.Content); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	v.Controller.TrackUserEvent(user, "app_file_updated", posthog.NewProperties().
		Set("app_id", project.ID.String()).
		Set("file_path", fid))

	utils.JSONResponse(w, responses.AppFileResponse{
		FilePath: fid,
		Content:  req.Content,
		FileType: controllers.InferFileType(fid),
	}, http.StatusOK)
}

// DELETE /v2/apps/{appId}/files/{fid} — Delete a file
// {fid} is now treated as a URL-encoded file path.
func (v *HttpViews) DeleteAppFile(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	fid, _ := url.PathUnescape(chi.URLParam(r, "fid"))
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	if err := v.Controller.DeleteAppFile(project, fid); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	v.Controller.TrackUserEvent(user, "app_file_deleted", posthog.NewProperties().
		Set("app_id", project.ID.String()))

	utils.JSONResponse(w, responses.SuccessResponse{Success: true}, http.StatusOK)
}

// Snapshots

// GET /v2/apps/{appId}/snapshots
func (v *HttpViews) GetAppSnapshots(w http.ResponseWriter, r *http.Request) {
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

	entries, err := v.Controller.GetAppSnapshots(project.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := make([]responses.AppSnapshotResponse, 0, len(entries))
	for _, e := range entries {
		resp = append(resp, responses.AppSnapshotResponse{
			Hash:        e.Hash,
			Description: e.Description,
			CreatedAt:   e.CreatedAt,
		})
	}

	utils.JSONResponse(w, resp, http.StatusOK)
}

// POST /v2/apps/{appId}/snapshots
func (v *HttpViews) CreateAppSnapshot(w http.ResponseWriter, r *http.Request) {
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

	var req requests.CreateAppSnapshotRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	hash, err := v.Controller.CreateAppSnapshot(project, req.Description, "manual")
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	v.Controller.TrackUserEvent(user, "app_snapshot_created", posthog.NewProperties().
		Set("app_id", project.ID.String()))

	utils.JSONResponse(w, responses.AppSnapshotResponse{
		Hash:        hash,
		Description: req.Description,
		CreatedAt:   time.Now(),
	}, http.StatusCreated)
}

// POST /v2/apps/{appId}/snapshots/{sid}/restore
func (v *HttpViews) RestoreAppSnapshot(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	sid := chi.URLParam(r, "sid")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	_, err = v.Controller.RestoreAppSnapshot(project, sid)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	v.Controller.TrackUserEvent(user, "app_snapshot_restored", posthog.NewProperties().
		Set("app_id", project.ID.String()).
		Set("snapshot_id", sid))

	utils.JSONResponse(w, responses.SuccessResponse{Success: true}, http.StatusOK)
}

// DELETE /v2/apps/{appId}/snapshots/{sid} — no longer supported (git commits are immutable)
func (v *HttpViews) DeleteAppSnapshot(w http.ResponseWriter, r *http.Request) {
	utils.JSONErrorResponse(w, "snapshot deletion is not supported with git-based storage", http.StatusGone)
}

// GET /v2/apps/{appId}/export/{platform}
func (v *HttpViews) ExportApp(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	// Check code access permission
	limits, limitsErr := v.Controller.DB.GetStandartUserLimitsByUser(user)
	if limitsErr != nil || !limits.CanAccessCode {
		go v.Controller.CRMService.SendEvent(user, constants.BREVO_EVENT_SUBSCRIPTION_NEED, map[string]any{"action": "access_code"})
		utils.JSONCodedErrorResponse(w, "FEATURE_NOT_AVAILABLE", "code access requires Starter or Professional plan", http.StatusForbidden, nil)
		return
	}

	appID := chi.URLParam(r, "appId")
	platform := chi.URLParam(r, "platform")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	fileMap, err := v.Controller.ExportAppAsZIP(project.ID.String())
	if err != nil || len(fileMap) == 0 {
		utils.JSONErrorResponse(w, "no files to export", http.StatusNotFound)
		return
	}
	_ = platform // kept for API compatibility

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for path, content := range fileMap {
		fw, err := zw.Create(path)
		if err != nil {
			continue
		}
		fw.Write([]byte(content))
	}
	zw.Close()

	v.Controller.TrackUserEvent(user, "app_exported", posthog.NewProperties().
		Set("app_id", project.ID.String()).
		Set("platform", platform))

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename="+project.Slug+"-"+platform+".zip")
	w.WriteHeader(http.StatusOK)
	w.Write(buf.Bytes())
}

// Integrations

// GET /v2/apps/{appId}/integrations
func (v *HttpViews) GetAppIntegrations(w http.ResponseWriter, r *http.Request) {
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

	integrations, err := v.Controller.GetAppIntegrations(project.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var resp []responses.AppIntegrationResp
	for _, i := range integrations {
		resp = append(resp, responses.AppIntegrationResp{
			ID:        i.ID,
			Provider:  i.Provider,
			Config:    i.Config,
			Status:    i.Status,
			CreatedAt: i.CreatedAt,
		})
	}

	utils.JSONResponse(w, resp, http.StatusOK)
}

// POST /v2/apps/{appId}/integrations
func (v *HttpViews) ConnectAppIntegration(w http.ResponseWriter, r *http.Request) {
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

	var req requests.ConnectIntegrationRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	integ, err := v.Controller.ConnectAppIntegration(project, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Sync env vars to preview so new integration vars are available immediately
	go v.syncEnvVarsToPreview(project)

	v.Controller.TrackUserEvent(user, "integration_connected", posthog.NewProperties().
		Set("app_id", project.ID.String()).
		Set("provider", integ.Provider))

	utils.JSONResponse(w, responses.AppIntegrationResp{
		ID:        integ.ID,
		Provider:  integ.Provider,
		Config:    integ.Config,
		Status:    integ.Status,
		CreatedAt: integ.CreatedAt,
	}, http.StatusCreated)
}

// PUT /v2/apps/{appId}/integrations/{iid}
func (v *HttpViews) UpdateAppIntegration(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	iid := chi.URLParam(r, "iid")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	var req requests.UpdateIntegrationRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	integ, err := v.Controller.UpdateAppIntegration(iid, project, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Sync env vars to preview so updated integration credentials take effect
	go v.syncEnvVarsToPreview(project)

	utils.JSONResponse(w, responses.AppIntegrationResp{
		ID:        integ.ID,
		Provider:  integ.Provider,
		Config:    integ.Config,
		Status:    integ.Status,
		CreatedAt: integ.CreatedAt,
	}, http.StatusOK)
}

// DELETE /v2/apps/{appId}/integrations/{iid}
func (v *HttpViews) DisconnectAppIntegration(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	iid := chi.URLParam(r, "iid")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	if err := v.Controller.DisconnectAppIntegration(iid, project); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Sync env vars to preview so removed integration vars are cleared
	go v.syncEnvVarsToPreview(project)

	v.Controller.TrackUserEvent(user, "integration_disconnected", posthog.NewProperties().
		Set("app_id", project.ID.String()))

	utils.JSONResponse(w, responses.SuccessResponse{Success: true}, http.StatusOK)
}

// POST /v2/apps/{appId}/convert-to-mobile
func (v *HttpViews) ConvertToMobile(w http.ResponseWriter, r *http.Request) {
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
		go v.Controller.CRMService.SendEvent(user, constants.BREVO_EVENT_SUBSCRIPTION_NEED, map[string]any{"action": "convert_to_mobile"})
		utils.JSONCodedErrorResponse(w, "FEATURE_NOT_AVAILABLE", "mobile conversion requires Professional plan", http.StatusForbidden, nil)
		return
	}

	appID := chi.URLParam(r, "appId")
	clone, err := v.Controller.CloneAppProjectAsMobile(user, appID)
	if err != nil {
		status := http.StatusInternalServerError
		if err.Error() == "unauthorized" {
			status = http.StatusForbidden
		} else if err.Error() == "source project not found" {
			status = http.StatusNotFound
		}
		utils.JSONErrorResponse(w, err.Error(), status)
		return
	}

	var theme map[string]any
	json.Unmarshal(clone.Theme, &theme)

	utils.JSONResponse(w, responses.AppProjectResponse{
		ID:          clone.ID,
		Name:        clone.Name,
		Slug:        clone.Slug,
		Prompt:      clone.Prompt,
		ProjectType: clone.ProjectType,
		Theme:       theme,
		Settings:    clone.Settings,
		Status:      clone.Status,
		CreatedAt:   clone.CreatedAt,
		UpdatedAt:   clone.UpdatedAt,
	}, http.StatusOK)
}

// POST /v2/apps/{appId}/mobile-optimize
func (v *HttpViews) MobileOptimize(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if requireActiveSubscription(w, user) {
		return
	}
	if v.checkMinimumCredits(w, user) {
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	if project.ProjectType != "mobile_app" {
		utils.JSONErrorResponse(w, "mobile optimization is only available for mobile app projects", http.StatusBadRequest)
		return
	}
	if project.Status != "ready" {
		utils.JSONErrorResponse(w, "project is not ready for optimization", http.StatusConflict)
		return
	}

	v.handleStreamOrAsync(w, r, user, StreamConfig{
		JobType: "mobile_optimize",
		StreamFunc: func(ctx context.Context, events chan<- responses.SSEEvent, jobID string) {
			v.Controller.StreamMobileOptimize(ctx, project, user, events)
		},
	})
}

// GET /v2/apps/{appId}/conversation
func (v *HttpViews) GetAppConversation(w http.ResponseWriter, r *http.Request) {
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

	conv, err := v.Controller.GetAppConversation(project.ID.String())
	if err != nil {
		utils.JSONResponse(w, responses.AppConversationResponse{
			Messages: []responses.AppConversationMessageResp{},
		}, http.StatusOK)
		return
	}

	var messages []models.AppConversationMessage
	json.Unmarshal(conv.Messages, &messages)

	msgResp := make([]responses.AppConversationMessageResp, 0, len(messages))
	for _, m := range messages {
		var fileOps []responses.AppChatFileOpResp
		for _, op := range m.FileOperations {
			fileOps = append(fileOps, responses.AppChatFileOpResp{
				Action:   op.Action,
				FilePath: op.FilePath,
				FileType: op.FileType,
			})
		}
		var toolCalls []responses.AppChatToolCallResp
		for _, tc := range m.ToolCalls {
			toolCalls = append(toolCalls, responses.AppChatToolCallResp{
				Tool:      tc.Tool,
				Arguments: tc.Arguments,
			})
		}
		msgResp = append(msgResp, responses.AppConversationMessageResp{
			ID:                 m.ID,
			Role:               m.Role,
			Content:            m.Content,
			Timestamp:          m.Timestamp,
			FileOperations:     fileOps,
			ImageURLs:          m.ImageURLs,
			SnapshotID:         m.SnapshotID,
			Suggestions:        m.Suggestions,
			ThinkingText:       m.ThinkingText,
			ThinkingDurationMs: m.ThinkingDurationMs,
			ToolCalls:          toolCalls,
		})
	}

	utils.JSONResponse(w, responses.AppConversationResponse{
		ID:        conv.ID,
		Messages:  msgResp,
		CreatedAt: conv.CreatedAt,
		UpdatedAt: conv.UpdatedAt,
	}, http.StatusOK)
}

// GET /v2/integrations/providers
func (v *HttpViews) GetV2IntegrationProviders(w http.ResponseWriter, r *http.Request) {
	providers := v.Controller.GetIntegrationProviders()
	utils.JSONResponse(w, providers, http.StatusOK)
}

// POST /v2/apps/{appId}/capture-screenshot
func (v *HttpViews) CaptureAppScreenshot(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, "App not found", http.StatusNotFound)
		return
	}

	var body struct {
		URL string `json:"url"`
	}
	if err := utils.ParseJSONBody(r, &body); err != nil || body.URL == "" {
		utils.JSONErrorResponse(w, "url required", http.StatusBadRequest)
		return
	}

	go v.Controller.CaptureAppScreenshot(project, body.URL)

	utils.JSONResponse(w, map[string]bool{"ok": true}, http.StatusAccepted)
}

// GET /v2/apps/{appId}/init — consolidated endpoint returning all data the frontend needs.
func (v *HttpViews) GetAppInit(w http.ResponseWriter, r *http.Request) {
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

	// Auto-heal: if status is "generating" but no active generation job exists, update to "ready"
	if project.Status == "generating" {
		activeJobs, _ := v.Controller.GetActiveJobs(user.ID)
		hasActiveGenJob := false
		for _, j := range activeJobs {
			if j.Type == "generate_app" || j.Type == "generate_ecommerce_app" {
				hasActiveGenJob = true
				break
			}
		}
		if !hasActiveGenJob {
			project.Status = "ready"
			v.Controller.UpdateAppProjectStatus(project, "ready")
		}
	}

	var (
		wg sync.WaitGroup

		conv           *models.AppConversation
		fileMap        map[string]string
		domains        []responses.AppDomainResponse
		envVars        []models.AppEnvVar
		missingEnvVars []controllers.RequiredEnvVar
		integrations   []models.AppIntegration
		deployment     *models.AppDeployment
		hasStore       bool
		screenshotURL  string
	)

	wg.Add(8)

	go func() {
		defer wg.Done()
		conv, _ = v.Controller.GetAppConversation(project.ID.String())
	}()

	go func() {
		defer wg.Done()
		fileMap, _ = v.Controller.GetAppGeneratedFiles(project.ID.String())
	}()

	go func() {
		defer wg.Done()
		domains, _ = v.Controller.GetAppDomains(project.ID.String())
	}()

	go func() {
		defer wg.Done()
		envVars, _ = v.Controller.GetAppEnvVars(project.ID.String())
	}()

	go func() {
		defer wg.Done()
		missingEnvVars, _ = v.Controller.GetMissingEnvVars(project)
	}()

	go func() {
		defer wg.Done()
		integrations, _ = v.Controller.GetAppIntegrations(project.ID.String())
	}()

	go func() {
		defer wg.Done()
		deployment, _ = v.Controller.GetLatestDeploymentStatus(project.ID.String())
	}()

	go func() {
		defer wg.Done()
		store, storeErr := v.Controller.GetStore(project.ID.String())
		if storeErr == nil && store != nil {
			hasStore = true
		}
	}()

	// Screenshot presigned URL (cheap, no goroutine needed)
	if project.ScreenshotS3Key != "" {
		if presigned, err := v.Controller.AssetsStorage.GetObject(r.Context(), project.ScreenshotS3Key, 3600); err == nil {
			screenshotURL = presigned.URL
		}
	}

	wg.Wait()

	// Build project detail response
	var theme map[string]any
	json.Unmarshal(project.Theme, &theme)

	var fileResp []responses.AppFileResponse
	for path, content := range fileMap {
		if path == ".env.local" {
			continue
		}
		fileResp = append(fileResp, responses.AppFileResponse{
			FilePath: path,
			Content:  content,
			FileType: controllers.InferFileType(path),
		})
	}

	projectResp := responses.AppProjectDetailResponse{
		AppProjectResponse: responses.AppProjectResponse{
			ID:            project.ID,
			Name:          project.Name,
			Slug:          project.Slug,
			Prompt:        project.Prompt,
			ProjectType:   project.ProjectType,
			Theme:         theme,
			Settings:      project.Settings,
			Status:        project.Status,
			HasStore:      hasStore,
			ScreenshotURL: screenshotURL,
			CreatedAt:     project.CreatedAt,
			UpdatedAt:     project.UpdatedAt,
		},
		Files: fileResp,
	}

	// Build conversation response
	var convResp *responses.AppConversationResponse
	if conv != nil {
		var messages []models.AppConversationMessage
		json.Unmarshal(conv.Messages, &messages)

		msgResp := make([]responses.AppConversationMessageResp, 0, len(messages))
		for _, m := range messages {
			var fileOps []responses.AppChatFileOpResp
			for _, op := range m.FileOperations {
				fileOps = append(fileOps, responses.AppChatFileOpResp{
					Action:   op.Action,
					FilePath: op.FilePath,
					FileType: op.FileType,
				})
			}
			var toolCalls []responses.AppChatToolCallResp
			for _, tc := range m.ToolCalls {
				toolCalls = append(toolCalls, responses.AppChatToolCallResp{
					Tool:      tc.Tool,
					Arguments: tc.Arguments,
				})
			}
			msgResp = append(msgResp, responses.AppConversationMessageResp{
				ID:                 m.ID,
				Role:               m.Role,
				Content:            m.Content,
				Timestamp:          m.Timestamp,
				FileOperations:     fileOps,
				ImageURLs:          m.ImageURLs,
				SnapshotID:         m.SnapshotID,
				Suggestions:        m.Suggestions,
				ThinkingText:       m.ThinkingText,
				ThinkingDurationMs: m.ThinkingDurationMs,
				ToolCalls:          toolCalls,
			})
		}
		convResp = &responses.AppConversationResponse{
			ID:        conv.ID,
			Messages:  msgResp,
			CreatedAt: conv.CreatedAt,
			UpdatedAt: conv.UpdatedAt,
		}
	}

	// Build domains response (already the right type)
	if domains == nil {
		domains = []responses.AppDomainResponse{}
	}

	// Build env vars response
	envVarResp := make([]responses.EnvVarResponse, 0, len(envVars))
	for _, ev := range envVars {
		envVarResp = append(envVarResp, responses.EnvVarResponse{
			ID:        ev.ID,
			Key:       ev.Key,
			Value:     ev.Value,
			IsPublic:  ev.IsPublic,
			CreatedAt: ev.CreatedAt,
			UpdatedAt: ev.UpdatedAt,
		})
	}

	// Build missing env vars response
	missingResp := make([]responses.MissingEnvVarResponse, 0, len(missingEnvVars))
	for _, mv := range missingEnvVars {
		missingResp = append(missingResp, responses.MissingEnvVarResponse{
			Key:         mv.Key,
			Description: mv.Description,
			IsPublic:    mv.IsPublic,
			Provider:    mv.Provider,
		})
	}

	// Build integrations response
	integrationResp := make([]responses.AppIntegrationResp, 0, len(integrations))
	for _, i := range integrations {
		integrationResp = append(integrationResp, responses.AppIntegrationResp{
			ID:        i.ID,
			Provider:  i.Provider,
			Config:    i.Config,
			Status:    i.Status,
			CreatedAt: i.CreatedAt,
		})
	}

	// Build deployment response
	var deployResp *responses.AppDeploymentResponse
	if deployment != nil {
		deployResp = &responses.AppDeploymentResponse{
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
		}
	}

	utils.JSONResponse(w, responses.AppInitResponse{
		Project:        projectResp,
		Conversation:   convResp,
		Domains:        domains,
		EnvVars:        envVarResp,
		MissingEnvVars: missingResp,
		Integrations:   integrationResp,
		Deployment:     deployResp,
		SandboxID:      project.SandboxID,
	}, http.StatusOK)
}
