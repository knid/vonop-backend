package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) ListAppleConnections(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	if rawOrgID := r.URL.Query().Get("org_id"); rawOrgID != "" {
		creds, oErr := v.Controller.ListAppleConnectionsForOrg(r.Context(), user, rawOrgID)
		if oErr != nil {
			utils.JSONErrorCode(w, constants.ErrForbidden, http.StatusForbidden, nil)
			return
		}
		utils.JSONResponse(w, creds, http.StatusOK)
		return
	}

	creds, err := v.Controller.ListAppleConnections(user)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, creds, http.StatusOK)
}

func (v *HttpViews) CreateAppleConnection(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	if requireActiveSubscription(w, v.Controller, user) {
		go v.Controller.EventService.SendEvent(user, constants.EVENT_SUBSCRIPTION_NEED, map[string]any{
			"action": "create_app_store_credential",
		})
		return
	}

	var req requests.AppleConnectionCreateRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.ValidationError(w, err)
		return
	}

	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := utils.ValidateFunc[requests.AppleConnectionCreateRequest](req, validate); err != nil {
		utils.ValidationError(w, err)
		return
	}

	orgID, err := v.Controller.RequireOrgIDForUser(user, &req.OrganizationID)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	cred, err := v.Controller.CreateAppleConnection(user, orgID, req)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, cred, http.StatusCreated)
}

func (v *HttpViews) GetAppleConnection(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	credID := chi.URLParam(r, "connectionId")
	if credID == "" {
		credID = chi.URLParam(r, "credentialId")
	}
	if credID == "" {
		utils.JSONErrorCode(w, constants.ErrValidationRequired, http.StatusBadRequest, map[string]any{"field": "connection_id"})
		return
	}

	orgID := chi.URLParam(r, "org_id")
	if orgID == "" {
		orgID = r.URL.Query().Get("org_id")
	}
	if orgID != "" {
		cred, oErr := v.Controller.GetAppleConnectionForOrg(r.Context(), user, orgID, credID)
		if oErr != nil {
			utils.JSONInternalError(w, oErr)
			return
		}
		utils.JSONResponse(w, cred, http.StatusOK)
		return
	}

	cred, err := v.Controller.GetAppleConnectionById(user, credID)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, cred, http.StatusOK)
}

func (v *HttpViews) UpdateAppleConnection(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	if requireActiveSubscription(w, v.Controller, user) {
		go v.Controller.EventService.SendEvent(user, constants.EVENT_SUBSCRIPTION_NEED, map[string]any{
			"action": "create_google_play_credential",
		})
		return
	}

	credID := chi.URLParam(r, "connectionId")
	if credID == "" {
		credID = chi.URLParam(r, "credentialId")
	}
	if credID == "" {
		utils.JSONErrorCode(w, constants.ErrValidationRequired, http.StatusBadRequest, map[string]any{"field": "connection_id"})
		return
	}

	var req requests.AppleConnectionUpdateRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.ValidationError(w, err)
		return
	}

	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := utils.ValidateFunc[requests.AppleConnectionUpdateRequest](req, validate); err != nil {
		utils.ValidationError(w, err)
		return
	}

	orgID := chi.URLParam(r, "org_id")
	if orgID == "" {
		orgID = r.URL.Query().Get("org_id")
	}
	if orgID != "" {
		cred, oErr := v.Controller.UpdateAppleConnectionForOrg(r.Context(), user, orgID, credID, req)
		if oErr != nil {
			utils.JSONInternalError(w, oErr)
			return
		}
		utils.JSONResponse(w, cred, http.StatusOK)
		return
	}

	cred, err := v.Controller.UpdateAppleConnection(user, credID, req)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, cred, http.StatusOK)
}

func (v *HttpViews) DeleteAppleConnection(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	credID := chi.URLParam(r, "connectionId")
	if credID == "" {
		credID = chi.URLParam(r, "credentialId")
	}
	if credID == "" {
		utils.JSONErrorCode(w, constants.ErrValidationRequired, http.StatusBadRequest, map[string]any{"field": "connection_id"})
		return
	}

	orgID := chi.URLParam(r, "org_id")
	if orgID == "" {
		orgID = r.URL.Query().Get("org_id")
	}
	if orgID != "" {
		if oErr := v.Controller.DeleteAppleConnectionForOrg(r.Context(), user, orgID, credID); oErr != nil {
			utils.JSONInternalError(w, oErr)
			return
		}
		utils.JSONResponse(w, "", http.StatusNoContent)
		return
	}

	err = v.Controller.DeleteAppleConnection(user, credID)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, "", http.StatusNoContent)
}

func (v *HttpViews) ListGooglePlayConnections(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	if rawOrgID := r.URL.Query().Get("org_id"); rawOrgID != "" {
		creds, oErr := v.Controller.ListGooglePlayConnectionsForOrg(r.Context(), user, rawOrgID)
		if oErr != nil {
			utils.JSONErrorCode(w, constants.ErrForbidden, http.StatusForbidden, nil)
			return
		}
		utils.JSONResponse(w, creds, http.StatusOK)
		return
	}

	creds, err := v.Controller.ListGooglePlayConnections(user)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, creds, http.StatusOK)

}

func (v *HttpViews) CreateGooglePlayConnection(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	if requireActiveSubscription(w, v.Controller, user) {
		go v.Controller.EventService.SendEvent(user, constants.EVENT_SUBSCRIPTION_NEED, map[string]any{
			"action": "create_google_play_credential",
		})
		return
	}

	var req requests.GooglePlayConnectionCreateRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.ValidationError(w, err)
		return
	}

	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := utils.ValidateFunc[requests.GooglePlayConnectionCreateRequest](req, validate); err != nil {
		utils.ValidationError(w, err)
		return
	}

	orgID, err := v.Controller.RequireOrgIDForUser(user, &req.OrganizationID)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	cred, err := v.Controller.CreateGooglePlayConnection(user, orgID, req)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, cred, http.StatusCreated)
}

func (v *HttpViews) GetGooglePlayConnection(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	credID := chi.URLParam(r, "connectionId")
	if credID == "" {
		credID = chi.URLParam(r, "credentialId")
	}
	if credID == "" {
		utils.JSONErrorCode(w, constants.ErrValidationRequired, http.StatusBadRequest, map[string]any{"field": "connection_id"})
		return
	}

	orgID := chi.URLParam(r, "org_id")
	if orgID == "" {
		orgID = r.URL.Query().Get("org_id")
	}
	if orgID != "" {
		cred, oErr := v.Controller.GetGooglePlayConnectionForOrg(r.Context(), user, orgID, credID)
		if oErr != nil {
			utils.JSONInternalError(w, oErr)
			return
		}
		utils.JSONResponse(w, cred, http.StatusOK)
		return
	}

	cred, err := v.Controller.GetGooglePlayConnectionById(user, credID)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, cred, http.StatusOK)
}

func (v *HttpViews) UpdateGooglePlayConnection(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	if requireActiveSubscription(w, v.Controller, user) {
		go v.Controller.EventService.SendEvent(user, constants.EVENT_SUBSCRIPTION_NEED, map[string]any{
			"action": "create_google_play_credential",
		})
		return
	}

	credID := chi.URLParam(r, "connectionId")
	if credID == "" {
		credID = chi.URLParam(r, "credentialId")
	}
	if credID == "" {
		utils.JSONErrorCode(w, constants.ErrValidationRequired, http.StatusBadRequest, map[string]any{"field": "connection_id"})
		return
	}

	var req requests.GooglePlayConnectionUpdateRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.ValidationError(w, err)
		return
	}

	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := utils.ValidateFunc[requests.GooglePlayConnectionUpdateRequest](req, validate); err != nil {
		utils.ValidationError(w, err)
		return
	}

	orgID := chi.URLParam(r, "org_id")
	if orgID == "" {
		orgID = r.URL.Query().Get("org_id")
	}
	if orgID != "" {
		cred, oErr := v.Controller.UpdateGooglePlayConnectionForOrg(r.Context(), user, orgID, credID, req)
		if oErr != nil {
			utils.JSONInternalError(w, oErr)
			return
		}
		utils.JSONResponse(w, cred, http.StatusOK)
		return
	}

	cred, err := v.Controller.UpdateGooglePlayConnection(user, credID, req)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, cred, http.StatusOK)
}

func (v *HttpViews) DeleteGooglePlayConnection(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	credID := chi.URLParam(r, "connectionId")
	if credID == "" {
		credID = chi.URLParam(r, "credentialId")
	}
	if credID == "" {
		utils.JSONErrorCode(w, constants.ErrValidationRequired, http.StatusBadRequest, map[string]any{"field": "connection_id"})
		return
	}

	orgID := chi.URLParam(r, "org_id")
	if orgID == "" {
		orgID = r.URL.Query().Get("org_id")
	}
	if orgID != "" {
		if oErr := v.Controller.DeleteGooglePlayConnectionForOrg(r.Context(), user, orgID, credID); oErr != nil {
			utils.JSONInternalError(w, oErr)
			return
		}
		utils.JSONResponse(w, "", http.StatusNoContent)
		return
	}

	err = v.Controller.DeleteGooglePlayConnection(user, credID)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, "", http.StatusNoContent)
}
