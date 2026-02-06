package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) GetIntegrationProviders(w http.ResponseWriter, r *http.Request) {
	providers := v.Controller.GetIntegrationProviders()
	utils.JSONResponse(w, providers, http.StatusOK)
}

func (v *HttpViews) GetWebsiteIntegrations(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	websiteID := chi.URLParam(r, "id")
	if websiteID == "" {
		utils.JSONErrorResponse(w, "website id is required", http.StatusBadRequest)
		return
	}

	integrations, err := v.Controller.GetWebsiteIntegrations(user, websiteID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, integrations, http.StatusOK)
}

func (v *HttpViews) ConnectIntegration(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	websiteID := chi.URLParam(r, "id")
	if websiteID == "" {
		utils.JSONErrorResponse(w, "website id is required", http.StatusBadRequest)
		return
	}

	var req requests.IntegrationConnectRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	result, err := v.Controller.ConnectIntegration(user, websiteID, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, result, http.StatusCreated)
}

func (v *HttpViews) UpdateIntegration(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	websiteID := chi.URLParam(r, "id")
	integrationID := chi.URLParam(r, "iid")
	if websiteID == "" || integrationID == "" {
		utils.JSONErrorResponse(w, "website id and integration id are required", http.StatusBadRequest)
		return
	}

	var req requests.IntegrationUpdateRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	result, err := v.Controller.UpdateIntegration(user, websiteID, integrationID, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}

func (v *HttpViews) DisconnectIntegration(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	websiteID := chi.URLParam(r, "id")
	integrationID := chi.URLParam(r, "iid")
	if websiteID == "" || integrationID == "" {
		utils.JSONErrorResponse(w, "website id and integration id are required", http.StatusBadRequest)
		return
	}

	err = v.Controller.DisconnectIntegration(user, websiteID, integrationID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, nil, http.StatusNoContent)
}
