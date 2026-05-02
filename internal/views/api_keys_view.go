package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	if requireActiveSubscription(w, v.Controller, user) {
		return
	}

	var req requests.CreateAPIKeyRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.ValidationError(w, err)
		return
	}

	orgID, err := v.Controller.RequireOrgIDForUser(user, &req.OrganizationID)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	result, err := v.Controller.CreateAPIKey(user, orgID, req)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, result, http.StatusCreated)
}

func (v *HttpViews) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	var orgIDPtr *uuid.UUID
	if rawOrgID := r.URL.Query().Get("org_id"); rawOrgID != "" {
		parsed, pErr := uuid.Parse(rawOrgID)
		if pErr != nil {
			utils.JSONErrorCode(w, constants.ErrValidationRequired, http.StatusBadRequest, map[string]any{"field": "org_id"})
			return
		}
		orgIDPtr = &parsed
	}

	result, err := v.Controller.ListAPIKeys(user, orgIDPtr)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}

func (v *HttpViews) DeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	keyID := chi.URLParam(r, "keyId")
	if keyID == "" {
		utils.JSONErrorCode(w, constants.ErrValidationRequired, http.StatusBadRequest, map[string]any{"field": "key_id"})
		return
	}

	err = v.Controller.DeleteAPIKey(user, keyID)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, nil, http.StatusNoContent)
}
