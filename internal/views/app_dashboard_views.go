package views

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

// POST /v2/apps/{appId}/dashboard
func (v *HttpViews) CreateDashboard(w http.ResponseWriter, r *http.Request) {
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

	var req requests.CreateDashboardRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	validate := validator.New()
	if err := req.Validate(validate); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	v.handleStreamOrAsync(w, r, user, StreamConfig{
		JobType: "dashboard_create",
		StreamFunc: func(ctx context.Context, events chan<- responses.SSEEvent, jobID string) {
			v.Controller.CreateDashboard(ctx, project, user, req.Prompt, events)
		},
	})
}

// GET /v2/apps/{appId}/dashboard
func (v *HttpViews) GetDashboard(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	_, err = v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	dashboard, panels, err := v.Controller.GetDashboard(appID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	resp := buildDashboardViewResponse(dashboard, panels)
	utils.JSONResponse(w, resp, http.StatusOK)
}

// PUT /v2/apps/{appId}/dashboard
func (v *HttpViews) UpdateDashboard(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	_, err = v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	var req requests.UpdateDashboardRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	validate := validator.New()
	if err := req.Validate(validate); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	dashboard, err := v.Controller.UpdateDashboard(appID, req.Name, req.Description, req.Settings)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	panels, _ := v.Controller.DB.GetAppDashboardPanelsByDashboardID(dashboard.ID.String())
	resp := buildDashboardViewResponse(dashboard, panels)
	utils.JSONResponse(w, resp, http.StatusOK)
}

// DELETE /v2/apps/{appId}/dashboard
func (v *HttpViews) DeleteDashboard(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	_, err = v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	if err := v.Controller.DeleteDashboard(appID); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, responses.SuccessResponse{Success: true}, http.StatusOK)
}

// GET /v2/apps/{appId}/dashboard/panels
func (v *HttpViews) GetDashboardPanels(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	_, err = v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	dashboard, panels, err := v.Controller.GetDashboard(appID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}
	_ = dashboard

	var resp []responses.DashboardPanelResponse
	for _, p := range panels {
		var query any
		json.Unmarshal(p.Query, &query)
		resp = append(resp, responses.DashboardPanelResponse{
			ID:        p.ID,
			Title:     p.Title,
			PanelType: p.PanelType,
			Query:     query,
			Config:    p.Config,
			SortOrder: p.SortOrder,
			GridWidth: p.GridWidth,
		})
	}

	utils.JSONResponse(w, resp, http.StatusOK)
}

// PUT /v2/apps/{appId}/dashboard/panels/{pid}
func (v *HttpViews) UpdateDashboardPanel(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	_, err = v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	panelID := chi.URLParam(r, "pid")

	dashboard, _, err := v.Controller.GetDashboard(appID)
	if err != nil {
		utils.JSONErrorResponse(w, "dashboard not found", http.StatusNotFound)
		return
	}

	var req requests.UpdateDashboardPanelRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	validate := validator.New()
	if err := req.Validate(validate); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	panel, err := v.Controller.UpdateDashboardPanel(panelID, dashboard.ID, req.Title, req.PanelType, req.Config, req.SortOrder, req.GridWidth)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	var query any
	json.Unmarshal(panel.Query, &query)
	utils.JSONResponse(w, responses.DashboardPanelResponse{
		ID:        panel.ID,
		Title:     panel.Title,
		PanelType: panel.PanelType,
		Query:     query,
		Config:    panel.Config,
		SortOrder: panel.SortOrder,
		GridWidth: panel.GridWidth,
	}, http.StatusOK)
}

// DELETE /v2/apps/{appId}/dashboard/panels/{pid}
func (v *HttpViews) DeleteDashboardPanel(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	_, err = v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	panelID := chi.URLParam(r, "pid")

	dashboard, _, err := v.Controller.GetDashboard(appID)
	if err != nil {
		utils.JSONErrorResponse(w, "dashboard not found", http.StatusNotFound)
		return
	}

	if err := v.Controller.DeleteDashboardPanel(panelID, dashboard.ID); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, responses.SuccessResponse{Success: true}, http.StatusOK)
}

// GET /v2/apps/{appId}/dashboard/data
func (v *HttpViews) GetDashboardData(w http.ResponseWriter, r *http.Request) {
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

	dashboard, panels, err := v.Controller.GetDashboard(appID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	var integration *models.AppIntegration
	if dashboard.Provider != "platform" {
		integration, err = v.getDBIntegration(project, dashboard.Provider)
		if err != nil {
			utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	results := v.Controller.QueryAllPanels(dashboard, panels, integration)
	utils.JSONResponse(w, responses.DashboardDataResponse{Panels: results}, http.StatusOK)
}

// GET /v2/apps/{appId}/dashboard/panels/{pid}/data
func (v *HttpViews) GetPanelData(w http.ResponseWriter, r *http.Request) {
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

	panelID := chi.URLParam(r, "pid")

	dashboard, _, err := v.Controller.GetDashboard(appID)
	if err != nil {
		utils.JSONErrorResponse(w, "dashboard not found", http.StatusNotFound)
		return
	}

	panel, err := v.Controller.DB.GetAppDashboardPanelByID(panelID)
	if err != nil {
		utils.JSONErrorResponse(w, "panel not found", http.StatusNotFound)
		return
	}
	if panel.DashboardID != dashboard.ID {
		utils.JSONErrorResponse(w, "panel does not belong to this dashboard", http.StatusBadRequest)
		return
	}

	var integration *models.AppIntegration
	if dashboard.Provider != "platform" {
		integration, err = v.getDBIntegration(project, dashboard.Provider)
		if err != nil {
			utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	result := v.Controller.QueryPanel(dashboard, panel, integration)
	utils.JSONResponse(w, result, http.StatusOK)
}

// POST /v2/apps/{appId}/dashboard/chat
func (v *HttpViews) ChatDashboard(w http.ResponseWriter, r *http.Request) {
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
	_, err = v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	dashboard, _, err := v.Controller.GetDashboard(appID)
	if err != nil {
		utils.JSONErrorResponse(w, "dashboard not found. Create one first.", http.StatusNotFound)
		return
	}

	var req requests.DashboardChatRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	validate := validator.New()
	if err := req.Validate(validate); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	v.handleStreamOrAsync(w, r, user, StreamConfig{
		JobType: "dashboard_chat",
		StreamFunc: func(ctx context.Context, events chan<- responses.SSEEvent, jobID string) {
			v.Controller.StreamDashboardChat(ctx, dashboard, user, req.Message, events)
		},
	})
}

// GET /v2/apps/{appId}/dashboard/schema
func (v *HttpViews) GetDashboardSchema(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	_, err = v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	dashboard, _, err := v.Controller.GetDashboard(appID)
	if err != nil {
		utils.JSONErrorResponse(w, "dashboard not found", http.StatusNotFound)
		return
	}

	var schema any
	json.Unmarshal(dashboard.Schema, &schema)
	utils.JSONResponse(w, map[string]any{"schema": schema}, http.StatusOK)
}

// POST /v2/apps/{appId}/dashboard/schema/refresh
func (v *HttpViews) RefreshDashboardSchema(w http.ResponseWriter, r *http.Request) {
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
	_, err = v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	schema, err := v.Controller.RefreshDashboardSchema(r.Context(), appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var parsed any
	json.Unmarshal(schema, &parsed)
	utils.JSONResponse(w, map[string]any{"schema": parsed}, http.StatusOK)
}

// PATCH /v2/apps/{appId}/dashboard/panels/{pid}/data
func (v *HttpViews) UpdatePanelCell(w http.ResponseWriter, r *http.Request) {
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

	panelID := chi.URLParam(r, "pid")

	dashboard, _, err := v.Controller.GetDashboard(appID)
	if err != nil {
		utils.JSONErrorResponse(w, "dashboard not found", http.StatusNotFound)
		return
	}

	panel, err := v.Controller.DB.GetAppDashboardPanelByID(panelID)
	if err != nil {
		utils.JSONErrorResponse(w, "panel not found", http.StatusNotFound)
		return
	}
	if panel.DashboardID != dashboard.ID {
		utils.JSONErrorResponse(w, "panel does not belong to this dashboard", http.StatusBadRequest)
		return
	}
	if panel.PanelType != "table" {
		utils.JSONErrorResponse(w, "cell editing is only supported for table panels", http.StatusBadRequest)
		return
	}

	var req requests.UpdateCellRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	validate := validator.New()
	if err := req.Validate(validate); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	var integration *models.AppIntegration
	if dashboard.Provider != "platform" {
		integration, err = v.getDBIntegration(project, dashboard.Provider)
		if err != nil {
			utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	// Resolve column aliases from the panel's SQL query so the UPDATE targets the real column
	resolvedTable := req.Table
	resolvedColumn := req.Column
	var raw map[string]any
	if err := json.Unmarshal(panel.Query, &raw); err == nil {
		if panelSQL, ok := raw["sql"].(string); ok && panelSQL != "" {
			realCol, tableAlias := controllers.ResolveColumnFromSQL(panelSQL, req.Column)
			resolvedColumn = realCol
			if tableAlias != "" {
				refs := controllers.ParseTableReferences(panelSQL)
				if realTable := controllers.ResolveTableFromRef(refs, tableAlias); realTable != "" {
					resolvedTable = realTable
				}
			}
		}
	}

	if err := v.Controller.UpdateCell(dashboard, integration, resolvedTable, resolvedColumn, req.Value, req.PrimaryKey); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, responses.CellUpdateResponse{
		Success: true,
		Table:   req.Table,
		Column:  req.Column,
		Value:   req.Value,
	}, http.StatusOK)
}

// getDBIntegration finds the database integration for a project.
func (v *HttpViews) getDBIntegration(project *models.AppProject, provider string) (*models.AppIntegration, error) {
	integration, err := v.Controller.DB.GetAppIntegrationByProvider(project.ID.String(), provider)
	if err != nil {
		return nil, fmt.Errorf("database integration not found")
	}
	return integration, nil
}

func buildDashboardViewResponse(d *models.AppDashboard, panels []models.AppDashboardPanel) responses.DashboardResponse {
	resp := responses.DashboardResponse{
		ID:          d.ID,
		Name:        d.Name,
		Description: d.Description,
		Provider:    d.Provider,
		Status:      d.Status,
		Settings:    d.Settings,
		CreatedAt:   d.CreatedAt.Unix(),
		UpdatedAt:   d.UpdatedAt.Unix(),
	}
	if len(d.Schema) > 0 {
		var schema any
		json.Unmarshal(d.Schema, &schema)
		resp.Schema = schema
	}

	var panelResps []responses.DashboardPanelResponse
	for _, p := range panels {
		var query any
		json.Unmarshal(p.Query, &query)
		panelResps = append(panelResps, responses.DashboardPanelResponse{
			ID:        p.ID,
			Title:     p.Title,
			PanelType: p.PanelType,
			Query:     query,
			Config:    p.Config,
			SortOrder: p.SortOrder,
			GridWidth: p.GridWidth,
		})
	}
	resp.Panels = panelResps
	return resp
}
