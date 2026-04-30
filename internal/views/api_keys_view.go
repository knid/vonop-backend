package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	if requireActiveSubscription(w, user) {
		return
	}

	var req requests.CreateAPIKeyRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.ValidationError(w, err)
		return
	}

	result, err := v.Controller.CreateAPIKey(user, req)
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

	if rawOrgID := r.URL.Query().Get("org_id"); rawOrgID != "" {
		orgID, oErr := v.Controller.RequireOrgMembership(user, rawOrgID)
		if oErr != nil {
			utils.JSONErrorCode(w, constants.ErrForbidden, http.StatusForbidden, nil)
			return
		}
		keys, dErr := v.Controller.DB.GetAPIKeysByOrgID(orgID.String())
		if dErr != nil {
			utils.JSONInternalError(w, dErr)
			return
		}
		var resp []responses.APIKeyResponse
		for _, k := range keys {
			resp = append(resp, responses.APIKeyResponse{
				ID:        k.ID,
				Name:      k.Name,
				KeyPrefix: k.KeyPrefix,
				Scopes:    k.Scopes,
				LastUsed:  k.LastUsed,
				ExpiresAt: k.ExpiresAt,
				CreatedAt: k.CreatedAt,
			})
		}
		utils.JSONResponse(w, resp, http.StatusOK)
		return
	}

	result, err := v.Controller.ListAPIKeys(user)
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
