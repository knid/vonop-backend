package views

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

// POST /v2/apps/migrate/{websiteId}/stream
func (v *HttpViews) MigrateWebsiteStream(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if requireActiveSubscription(w, user) {
		return
	}
	if v.deductAICredits(w, user, models.AICreditCostGenerateApp, "migrate_website_stream", "") {
		return
	}

	websiteID := chi.URLParam(r, "websiteId")
	if websiteID == "" {
		utils.JSONErrorResponse(w, "website id is required", http.StatusBadRequest)
		return
	}

	v.handleStreamOrAsync(w, r, user, StreamConfig{
		JobType: "migrate_website",
		StreamFunc: func(ctx context.Context, events chan<- responses.SSEEvent) {
			v.Controller.MigrateWebsiteToAppStream(ctx, user, websiteID, events)
		},
	})
}

// POST /v2/apps/migrate/{websiteId}
func (v *HttpViews) MigrateWebsite(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if requireActiveSubscription(w, user) {
		return
	}
	if v.deductAICredits(w, user, models.AICreditCostGenerateApp, "migrate_website", "") {
		return
	}

	websiteID := chi.URLParam(r, "websiteId")
	if websiteID == "" {
		utils.JSONErrorResponse(w, "website id is required", http.StatusBadRequest)
		return
	}

	project, err := v.Controller.MigrateWebsiteToApp(user, websiteID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

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
}
