package views

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

// POST /v2/mobile/{projectId}/chat
func (v *HttpViews) MobileChatStream(w http.ResponseWriter, r *http.Request) {
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

	projectID := chi.URLParam(r, "projectId")
	project, err := v.Controller.GetProjectModelById(projectID)
	if err != nil {
		utils.JSONErrorResponse(w, "project not found", http.StatusNotFound)
		return
	}
	if project.UserID != user.ID {
		utils.JSONErrorResponse(w, "unauthorized", http.StatusForbidden)
		return
	}

	var req requests.AppChatRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	v.handleStreamOrAsync(w, r, user, StreamConfig{
		JobType: "mobile_chat",
		StreamFunc: func(ctx context.Context, events chan<- responses.SSEEvent, jobID string) {
			v.Controller.StreamMobileInjectionChat(ctx, project, user, req.Message, req.Images, events)
		},
	})
}

// GET /v2/mobile/{projectId}/injections
func (v *HttpViews) ListMobileInjections(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	projectID := chi.URLParam(r, "projectId")
	project, err := v.Controller.GetProjectModelById(projectID)
	if err != nil {
		utils.JSONErrorResponse(w, "project not found", http.StatusNotFound)
		return
	}
	if project.UserID != user.ID {
		utils.JSONErrorResponse(w, "unauthorized", http.StatusForbidden)
		return
	}

	injections, err := v.Controller.DB.GetMobileInjectionsByProject(projectID)
	if err != nil {
		utils.JSONErrorResponse(w, "failed to get injections", http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, injections, http.StatusOK)
}

// PUT /v2/mobile/{projectId}/injections/{id}
func (v *HttpViews) UpdateMobileInjection(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	projectID := chi.URLParam(r, "projectId")
	project, err := v.Controller.GetProjectModelById(projectID)
	if err != nil {
		utils.JSONErrorResponse(w, "project not found", http.StatusNotFound)
		return
	}
	if project.UserID != user.ID {
		utils.JSONErrorResponse(w, "unauthorized", http.StatusForbidden)
		return
	}

	injectionID := chi.URLParam(r, "id")
	injection, err := v.Controller.DB.GetMobileInjection(injectionID)
	if err != nil {
		utils.JSONErrorResponse(w, "injection not found", http.StatusNotFound)
		return
	}
	if injection.ProjectID != project.ID {
		utils.JSONErrorResponse(w, "injection does not belong to this project", http.StatusForbidden)
		return
	}

	var body struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
		CSS         *string `json:"css"`
		JS          *string `json:"js"`
		HTML        *string `json:"html"`
		IsEnabled   *bool   `json:"is_enabled"`
		Order       *int    `json:"order"`
	}
	if err := utils.ParseJSONBody(r, &body); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	if body.Name != nil {
		injection.Name = *body.Name
	}
	if body.Description != nil {
		injection.Description = *body.Description
	}
	if body.CSS != nil {
		injection.CSS = *body.CSS
	}
	if body.JS != nil {
		injection.JS = *body.JS
	}
	if body.HTML != nil {
		injection.HTML = *body.HTML
	}
	if body.IsEnabled != nil {
		injection.IsEnabled = *body.IsEnabled
	}
	if body.Order != nil {
		injection.Order = *body.Order
	}

	updated, err := v.Controller.DB.UpdateMobileInjection(injection)
	if err != nil {
		utils.JSONErrorResponse(w, "failed to update injection", http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, updated, http.StatusOK)
}

// DELETE /v2/mobile/{projectId}/injections/{id}
func (v *HttpViews) DeleteMobileInjection(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	projectID := chi.URLParam(r, "projectId")
	project, err := v.Controller.GetProjectModelById(projectID)
	if err != nil {
		utils.JSONErrorResponse(w, "project not found", http.StatusNotFound)
		return
	}
	if project.UserID != user.ID {
		utils.JSONErrorResponse(w, "unauthorized", http.StatusForbidden)
		return
	}

	injectionID := chi.URLParam(r, "id")
	injection, err := v.Controller.DB.GetMobileInjection(injectionID)
	if err != nil {
		utils.JSONErrorResponse(w, "injection not found", http.StatusNotFound)
		return
	}
	if injection.ProjectID != project.ID {
		utils.JSONErrorResponse(w, "injection does not belong to this project", http.StatusForbidden)
		return
	}

	if err := v.Controller.DB.DeleteMobileInjection(injectionID); err != nil {
		utils.JSONErrorResponse(w, "failed to delete injection", http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, map[string]any{"success": true}, http.StatusOK)
}

// DELETE /v2/mobile/{projectId}/conversation
func (v *HttpViews) ClearMobileConversation(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	projectID := chi.URLParam(r, "projectId")
	project, err := v.Controller.GetProjectModelById(projectID)
	if err != nil {
		utils.JSONErrorResponse(w, "project not found", http.StatusNotFound)
		return
	}
	if project.UserID != user.ID {
		utils.JSONErrorResponse(w, "unauthorized", http.StatusForbidden)
		return
	}

	if err := v.Controller.ClearMobileConversation(projectID); err != nil {
		utils.JSONErrorResponse(w, "failed to clear conversation", http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, map[string]any{"success": true}, http.StatusOK)
}

// POST /v2/mobile/classify-intent
func (v *HttpViews) ClassifyMobileIntent(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if requireActiveSubscription(w, user) {
		return
	}

	var body struct {
		Prompt string `json:"prompt"`
	}
	if err := utils.ParseJSONBody(r, &body); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	if body.Prompt == "" {
		utils.JSONErrorResponse(w, "prompt is required", http.StatusBadRequest)
		return
	}

	intent, extractedURL := v.Controller.ClassifyMobileIntent(r.Context(), body.Prompt)

	utils.JSONResponse(w, map[string]any{
		"intent": intent,
		"url":    extractedURL,
	}, http.StatusOK)
}

// GET /v1/mobile/{projectId}/injections (public, no auth)
func (v *HttpViews) GetPublicMobileInjections(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectId")

	injections, err := v.Controller.DB.GetEnabledMobileInjections(projectID)
	if err != nil {
		utils.JSONErrorResponse(w, "failed to get injections", http.StatusInternalServerError)
		return
	}

	// Return only the fields needed for injection
	type publicInjection struct {
		Name string `json:"name"`
		CSS  string `json:"css,omitempty"`
		JS   string `json:"js,omitempty"`
		HTML string `json:"html,omitempty"`
	}

	result := make([]publicInjection, 0, len(injections))
	for _, inj := range injections {
		result = append(result, publicInjection{
			Name: inj.Name,
			CSS:  inj.CSS,
			JS:   inj.JS,
			HTML: inj.HTML,
		})
	}

	utils.JSONResponse(w, result, http.StatusOK)
}
