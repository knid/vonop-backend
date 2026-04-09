package views

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

// --- Public endpoints ---

// GET /v2/templates
func (v *HttpViews) ListPublishedTemplates(w http.ResponseWriter, r *http.Request) {
	tag := r.URL.Query().Get("tag")
	templateType := r.URL.Query().Get("type")

	templates, err := v.Controller.ListPublishedTemplates(tag, templateType)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	items := make([]any, len(templates))
	for i := range templates {
		resp := controllers.ToTemplateResponse(&templates[i])
		if templates[i].PreviewImageKey != "" {
			if presigned, err := v.Controller.AssetsStorage.GetObject(r.Context(), templates[i].PreviewImageKey, 3600); err == nil {
				resp.PreviewImageKey = presigned.URL
			}
		}
		items[i] = resp
	}

	utils.JSONResponse(w, map[string]any{"templates": items}, http.StatusOK)
}

// GET /v2/templates/{slug}
func (v *HttpViews) GetPublishedTemplate(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")

	template, err := v.Controller.GetPublishedTemplate(slug)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	resp := controllers.ToTemplateDetailResponse(template)
	if template.PreviewImageKey != "" {
		if presigned, err := v.Controller.AssetsStorage.GetObject(r.Context(), template.PreviewImageKey, 3600); err == nil {
			resp.PreviewImageKey = presigned.URL
		}
	}
	utils.JSONResponse(w, resp, http.StatusOK)
}

// --- Authenticated user endpoints ---

// POST /v2/templates/{slug}/copy
func (v *HttpViews) CopyTemplate(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if requireActiveSubscription(w, user) {
		return
	}

	slug := chi.URLParam(r, "slug")

	project, err := v.Controller.CopyTemplate(user, slug)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	theme := map[string]any{}
	if len(project.Theme) > 0 {
		json.Unmarshal(project.Theme, &theme)
	}

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
	}, http.StatusCreated)
}

// --- Admin endpoints ---

// POST /v2/admin/templates
func (v *HttpViews) CreateAdminTemplate(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	var req requests.CreateAppTemplateRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	template, err := v.Controller.CreateAppTemplate(user, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	resp := controllers.ToTemplateDetailResponse(template)
	utils.JSONResponse(w, resp, http.StatusCreated)
}

// POST /v2/admin/templates/from-app/{appId}
func (v *HttpViews) CreateTemplateFromApp(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	var req requests.CreateTemplateFromAppRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	template, err := v.Controller.CreateTemplateFromApp(user, appID, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	resp := controllers.ToTemplateDetailResponse(template)
	utils.JSONResponse(w, resp, http.StatusCreated)
}

// GET /v2/admin/templates
func (v *HttpViews) ListAdminTemplates(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	templates, err := v.Controller.ListAllTemplates(user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusForbidden)
		return
	}

	items := make([]any, len(templates))
	for i := range templates {
		resp := controllers.ToTemplateResponse(&templates[i])
		items[i] = resp
	}

	utils.JSONResponse(w, map[string]any{"templates": items}, http.StatusOK)
}

// GET /v2/admin/templates/{id}
func (v *HttpViews) GetAdminTemplate(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	id := chi.URLParam(r, "id")
	template, err := v.Controller.GetAppTemplate(id, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	resp := controllers.ToTemplateDetailResponse(template)
	utils.JSONResponse(w, resp, http.StatusOK)
}

// PUT /v2/admin/templates/{id}
func (v *HttpViews) UpdateAdminTemplate(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	id := chi.URLParam(r, "id")
	var req requests.UpdateAppTemplateRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	template, err := v.Controller.UpdateAppTemplate(id, user, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	resp := controllers.ToTemplateDetailResponse(template)
	utils.JSONResponse(w, resp, http.StatusOK)
}

// DELETE /v2/admin/templates/{id}
func (v *HttpViews) DeleteAdminTemplate(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	id := chi.URLParam(r, "id")
	if err := v.Controller.DeleteAppTemplate(id, user); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, map[string]string{"status": "deleted"}, http.StatusOK)
}

// PUT /v2/admin/templates/{id}/publish
func (v *HttpViews) PublishAdminTemplate(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	id := chi.URLParam(r, "id")
	var req requests.PublishAppTemplateRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	template, err := v.Controller.PublishAppTemplate(id, user, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	resp := controllers.ToTemplateDetailResponse(template)
	utils.JSONResponse(w, resp, http.StatusOK)
}

// PUT /v2/admin/templates/{id}/sort
func (v *HttpViews) SortAdminTemplate(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	id := chi.URLParam(r, "id")
	var req requests.SortAppTemplateRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	template, err := v.Controller.SortAppTemplate(id, user, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	resp := controllers.ToTemplateDetailResponse(template)
	utils.JSONResponse(w, resp, http.StatusOK)
}
