package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) CreateWebhookEndpoint(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	if requireActiveSubscription(w, user) {
		return
	}

	var req requests.CreateWebhookRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.ValidationError(w, err)
		return
	}

	result, err := v.Controller.CreateWebhookEndpoint(user, req)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, result, http.StatusCreated)
}

func (v *HttpViews) ListWebhookEndpoints(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	if rawOrgID := r.URL.Query().Get("org_id"); rawOrgID != "" {
		orgID, oErr := v.Controller.RequireOrgMembership(user, rawOrgID)
		if oErr != nil {
			utils.JSONErrorCode(w, constants.ErrForbidden, http.StatusForbidden, nil)
			return
		}
		endpoints, dErr := v.Controller.DB.GetWebhookEndpointsByOrgID(orgID.String())
		if dErr != nil {
			utils.JSONInternalError(w, dErr)
			return
		}
		var resp []responses.WebhookEndpointResponse
		for _, ep := range endpoints {
			resp = append(resp, responses.WebhookEndpointResponse{
				ID:        ep.ID,
				URL:       ep.URL,
				Events:    ep.Events,
				Active:    ep.Active,
				CreatedAt: ep.CreatedAt,
			})
		}
		utils.JSONResponse(w, resp, http.StatusOK)
		return
	}

	result, err := v.Controller.ListWebhookEndpoints(user)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}

func (v *HttpViews) UpdateWebhookEndpoint(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	webhookID := chi.URLParam(r, "webhookId")
	if webhookID == "" {
		utils.JSONErrorCode(w, constants.ErrValidationRequired, http.StatusBadRequest, map[string]any{"field": "webhook_id"})
		return
	}

	var req requests.UpdateWebhookRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.ValidationError(w, err)
		return
	}

	result, err := v.Controller.UpdateWebhookEndpoint(user, webhookID, req)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}

func (v *HttpViews) DeleteWebhookEndpoint(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	webhookID := chi.URLParam(r, "webhookId")
	if webhookID == "" {
		utils.JSONErrorCode(w, constants.ErrValidationRequired, http.StatusBadRequest, map[string]any{"field": "webhook_id"})
		return
	}

	err = v.Controller.DeleteWebhookEndpoint(user, webhookID)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, nil, http.StatusNoContent)
}
