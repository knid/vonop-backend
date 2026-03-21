package views

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

// syncEnvVarsToPreview regenerates .env.local in the git repo so future
// preview restarts pick up the latest env vars.
func (v *HttpViews) syncEnvVarsToPreview(project *models.AppProject) {
	envVars := v.Controller.CollectAllEnvVars(project)

	repo, err := v.Controller.GitRepoService.OpenOrInit(project.ID.String())
	if err != nil {
		slog.Warn("syncEnvVarsToPreview: failed to open repo", "error", err, "project_id", project.ID)
		return
	}

	dotEnv := controllers.BuildDotEnvLocal(envVars)
	if dotEnv != "" {
		if err := repo.WriteFile(".env.local", []byte(dotEnv)); err != nil {
			slog.Warn("syncEnvVarsToPreview: failed to write .env.local", "error", err, "project_id", project.ID)
		}
	} else {
		// All env vars removed — delete the stale .env.local file
		if err := repo.DeleteFile(".env.local"); err != nil {
			// Not an error if the file doesn't exist
			slog.Debug("syncEnvVarsToPreview: .env.local not found to delete", "project_id", project.ID)
		}
	}
}

// GET /v2/apps/{appId}/env-vars/missing
func (v *HttpViews) GetMissingEnvVars(w http.ResponseWriter, r *http.Request) {
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

	missing, err := v.Controller.GetMissingEnvVars(project)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, missing, http.StatusOK)
}

// GET /v2/apps/{appId}/env-vars
func (v *HttpViews) GetAppEnvVars(w http.ResponseWriter, r *http.Request) {
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

	envVars, err := v.Controller.GetAppEnvVars(project.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var resp []responses.EnvVarResponse
	for _, ev := range envVars {
		resp = append(resp, responses.EnvVarResponse{
			ID:        ev.ID,
			Key:       ev.Key,
			Value:     ev.Value,
			IsPublic:  ev.IsPublic,
			CreatedAt: ev.CreatedAt,
			UpdatedAt: ev.UpdatedAt,
		})
	}

	if resp == nil {
		resp = []responses.EnvVarResponse{}
	}

	utils.JSONResponse(w, resp, http.StatusOK)
}

// POST /v2/apps/{appId}/env-vars
func (v *HttpViews) SetAppEnvVar(w http.ResponseWriter, r *http.Request) {
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

	var req requests.SetEnvVarRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	envVar, err := v.Controller.SetAppEnvVar(project, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	go v.syncEnvVarsToPreview(project)

	allEnvVars := v.Controller.CollectAllEnvVars(project)
	envLocalContent := controllers.BuildDotEnvLocal(allEnvVars)

	utils.JSONResponse(w, responses.EnvVarResponse{
		ID:              envVar.ID,
		Key:             envVar.Key,
		Value:           envVar.Value,
		IsPublic:        envVar.IsPublic,
		EnvLocalContent: envLocalContent,
		CreatedAt:       envVar.CreatedAt,
		UpdatedAt:       envVar.UpdatedAt,
	}, http.StatusCreated)
}

// PUT /v2/apps/{appId}/env-vars/{evid}
func (v *HttpViews) UpdateAppEnvVar(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	evID := chi.URLParam(r, "evid")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	var req requests.UpdateEnvVarRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	envVar, err := v.Controller.UpdateAppEnvVar(evID, project, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	go v.syncEnvVarsToPreview(project)

	allEnvVars := v.Controller.CollectAllEnvVars(project)
	envLocalContent := controllers.BuildDotEnvLocal(allEnvVars)

	utils.JSONResponse(w, responses.EnvVarResponse{
		ID:              envVar.ID,
		Key:             envVar.Key,
		Value:           envVar.Value,
		IsPublic:        envVar.IsPublic,
		EnvLocalContent: envLocalContent,
		CreatedAt:       envVar.CreatedAt,
		UpdatedAt:       envVar.UpdatedAt,
	}, http.StatusOK)
}

// DELETE /v2/apps/{appId}/env-vars/{evid}
func (v *HttpViews) DeleteAppEnvVar(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	evID := chi.URLParam(r, "evid")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	if err := v.Controller.DeleteAppEnvVar(evID, project); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	go v.syncEnvVarsToPreview(project)

	allEnvVars := v.Controller.CollectAllEnvVars(project)
	envLocalContent := controllers.BuildDotEnvLocal(allEnvVars)

	utils.JSONResponse(w, map[string]any{
		"success":           true,
		"env_local_content": envLocalContent,
	}, http.StatusOK)
}
