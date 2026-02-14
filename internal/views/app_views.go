package views

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
	"github.com/posthog/posthog-go"
)

// POST /v2/apps/generate — non-streaming generation (redirects to stream)
func (v *HttpViews) GenerateApp(w http.ResponseWriter, r *http.Request) {
	// For non-streaming, just create the project and return; frontend should use /generate/stream
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

	var req requests.GenerateAppRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	project, err := v.Controller.CreateAppProject(user, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Token tracking for post-hoc billing
	tracker := utils.NewTokenTracker()
	ctx := utils.WithTokenTracker(r.Context(), tracker)

	// Plan structure and generate files synchronously (code-first)
	structure, err := v.Controller.PlanAppFileStructure(ctx, req.Prompt, req.ProjectType)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Update project with generated info
	if structure.Name != "" {
		project.Name = structure.Name
	}
	if structure.Slug != "" {
		project.Slug = structure.Slug
	}
	themeData := models.AppTheme{
		ColorScheme:     structure.ColorScheme,
		DarkColorScheme: structure.DarkColorScheme,
		Typography:      structure.Typography,
	}
	themeJSON, _ := json.Marshal(themeData)
	project.Theme = themeJSON
	project.Status = "ready"
	v.Controller.DB.UpdateAppProject(project)

	// If ecommerce, create store
	if req.ProjectType == "ecommerce" {
		v.Controller.CreateStore(project, user, project.Name+" Store")
	}

	var theme map[string]any
	json.Unmarshal(project.Theme, &theme)

	hasStore := req.ProjectType == "ecommerce"

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
			HasStore:    hasStore,
			CreatedAt:   project.CreatedAt,
			UpdatedAt:   project.UpdatedAt,
		},
	}, http.StatusCreated)

	// Post-hoc token-based billing
	_, _, total, _ := tracker.Totals()
	if total > 0 {
		credits := models.TokensToCredits(total)
		if err := v.Controller.DeductAICreditsUnchecked(user, credits, "generate_app", project.ID.String()); err != nil {
			slog.Warn("failed to deduct AI credits from tokens", "error", err,
				"user_id", user.ID, "credits", credits, "tokens", total)
		}
	}

	v.Controller.TrackUserEvent(user, "app_created", posthog.NewProperties().
		Set("project_type", req.ProjectType).
		Set("app_id", project.ID.String()))
}

// POST /v2/apps/generate/ecommerce
func (v *HttpViews) GenerateEcommerceApp(w http.ResponseWriter, r *http.Request) {
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

	var req requests.GenerateAppRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	req.ProjectType = "ecommerce"

	project, err := v.Controller.CreateAppProject(user, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Token tracking for post-hoc billing
	tracker := utils.NewTokenTracker()
	ctx := utils.WithTokenTracker(r.Context(), tracker)

	structure, err := v.Controller.PlanAppFileStructure(ctx, req.Prompt, "ecommerce")
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
		ColorScheme:     structure.ColorScheme,
		DarkColorScheme: structure.DarkColorScheme,
		Typography:      structure.Typography,
	}
	themeJSON, _ := json.Marshal(themeData)
	project.Theme = themeJSON
	project.Status = "ready"
	v.Controller.DB.UpdateAppProject(project)
	v.Controller.CreateStore(project, user, project.Name+" Store")

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
			HasStore:    true,
			CreatedAt:   project.CreatedAt,
			UpdatedAt:   project.UpdatedAt,
		},
	}, http.StatusCreated)

	// Post-hoc token-based billing
	_, _, total, _ := tracker.Totals()
	if total > 0 {
		credits := models.TokensToCredits(total)
		if err := v.Controller.DeductAICreditsUnchecked(user, credits, "generate_ecommerce_app", project.ID.String()); err != nil {
			slog.Warn("failed to deduct AI credits from tokens", "error", err,
				"user_id", user.ID, "credits", credits, "tokens", total)
		}
	}

	v.Controller.TrackUserEvent(user, "app_created", posthog.NewProperties().
		Set("project_type", "ecommerce").
		Set("app_id", project.ID.String()))
}

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
	ctx := utils.WithTokenTracker(r.Context(), tracker)

	structure, err := v.Controller.PlanAppFileStructureFromScreenshot(ctx, req.ImageBase64)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	genReq := requests.GenerateAppRequest{Prompt: "Generated from screenshot"}
	project, err := v.Controller.CreateAppProject(user, genReq)
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
		ColorScheme:     structure.ColorScheme,
		DarkColorScheme: structure.DarkColorScheme,
		Typography:      structure.Typography,
	}
	themeJSON, _ := json.Marshal(themeData)
	project.Theme = themeJSON
	project.Status = "ready"
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

	// Post-hoc token-based billing
	_, _, total, _ := tracker.Totals()
	if total > 0 {
		credits := models.TokensToCredits(total)
		if err := v.Controller.DeductAICreditsUnchecked(user, credits, "generate_from_image", project.ID.String()); err != nil {
			slog.Warn("failed to deduct AI credits from tokens", "error", err,
				"user_id", user.ID, "credits", credits, "tokens", total)
		}
	}

	v.Controller.TrackUserEvent(user, "app_generated_from_image", posthog.NewProperties().
		Set("app_id", project.ID.String()))
}

// GET /v2/apps/
func (v *HttpViews) GetApps(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	projects, err := v.Controller.GetAppProjectsByUser(user)
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

		resp = append(resp, responses.AppProjectResponse{
			ID:          p.ID,
			Name:        p.Name,
			Slug:        p.Slug,
			Prompt:      p.Prompt,
			ProjectType: p.ProjectType,
			Theme:       theme,
			Settings:    p.Settings,
			Status:      status,
			HasStore:    hasStore,
			CreatedAt:   p.CreatedAt,
			UpdatedAt:   p.UpdatedAt,
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
	files, _ := v.Controller.GetAppGeneratedFiles(project.ID.String(), "nextjs")
	var fileResp []responses.AppFileResponse
	for _, f := range files {
		fileResp = append(fileResp, responses.AppFileResponse{
			ID:        f.ID,
			Platform:  f.Platform,
			FilePath:  f.FilePath,
			Content:   f.Content,
			FileType:  f.FileType,
			CreatedAt: f.CreatedAt,
			UpdatedAt: f.UpdatedAt,
		})
	}

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
			HasStore:    hasStore,
			CreatedAt:   project.CreatedAt,
			UpdatedAt:   project.UpdatedAt,
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

	files, err := v.Controller.GetAppGeneratedFiles(project.ID.String(), platform)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var resp []responses.AppFileResponse
	for _, f := range files {
		resp = append(resp, responses.AppFileResponse{
			ID:        f.ID,
			Platform:  f.Platform,
			FilePath:  f.FilePath,
			Content:   f.Content,
			FileType:  f.FileType,
			CreatedAt: f.CreatedAt,
			UpdatedAt: f.UpdatedAt,
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

	file, err := v.Controller.CreateAppFile(project, req.FilePath, req.Content, req.FileType)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	v.Controller.TrackUserEvent(user, "app_file_created", posthog.NewProperties().
		Set("app_id", project.ID.String()).
		Set("file_path", req.FilePath))

	utils.JSONResponse(w, responses.AppFileResponse{
		ID:        file.ID,
		Platform:  file.Platform,
		FilePath:  file.FilePath,
		Content:   file.Content,
		FileType:  file.FileType,
		CreatedAt: file.CreatedAt,
		UpdatedAt: file.UpdatedAt,
	}, http.StatusCreated)
}

// PUT /v2/apps/{appId}/files/{fid} — Update a file's content
func (v *HttpViews) UpdateAppFile(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	fid := chi.URLParam(r, "fid")
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

	file, err := v.Controller.UpdateAppFile(project, fid, req.Content)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	v.Controller.TrackUserEvent(user, "app_file_updated", posthog.NewProperties().
		Set("app_id", project.ID.String()).
		Set("file_path", file.FilePath))

	utils.JSONResponse(w, responses.AppFileResponse{
		ID:        file.ID,
		Platform:  file.Platform,
		FilePath:  file.FilePath,
		Content:   file.Content,
		FileType:  file.FileType,
		CreatedAt: file.CreatedAt,
		UpdatedAt: file.UpdatedAt,
	}, http.StatusOK)
}

// DELETE /v2/apps/{appId}/files/{fid} — Delete a file
func (v *HttpViews) DeleteAppFile(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	fid := chi.URLParam(r, "fid")
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

	snapshots, err := v.Controller.GetAppSnapshots(project.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var resp []responses.AppSnapshotResponse
	for _, s := range snapshots {
		resp = append(resp, responses.AppSnapshotResponse{
			ID:          s.ID,
			Description: s.Description,
			Source:      s.Source,
			CreatedAt:   s.CreatedAt,
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

	snapshot, err := v.Controller.CreateAppSnapshot(project, req.Description, "manual")
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	v.Controller.TrackUserEvent(user, "app_snapshot_created", posthog.NewProperties().
		Set("app_id", project.ID.String()))

	utils.JSONResponse(w, responses.AppSnapshotResponse{
		ID:          snapshot.ID,
		Description: snapshot.Description,
		Source:      snapshot.Source,
		CreatedAt:   snapshot.CreatedAt,
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

// GET /v2/apps/{appId}/export/{platform}
func (v *HttpViews) ExportApp(w http.ResponseWriter, r *http.Request) {
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

	files, err := v.Controller.ExportAppAsZIP(project.ID.String(), platform)
	if err != nil || len(files) == 0 {
		utils.JSONErrorResponse(w, "no files to export", http.StatusNotFound)
		return
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range files {
		fw, err := zw.Create(f.FilePath)
		if err != nil {
			continue
		}
		fw.Write([]byte(f.Content))
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

	v.Controller.TrackUserEvent(user, "integration_disconnected", posthog.NewProperties().
		Set("app_id", project.ID.String()))

	utils.JSONResponse(w, responses.SuccessResponse{Success: true}, http.StatusOK)
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

	var msgResp []responses.AppConversationMessageResp
	for _, m := range messages {
		msgResp = append(msgResp, responses.AppConversationMessageResp{
			Role:      m.Role,
			Content:   m.Content,
			Timestamp: m.Timestamp,
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
