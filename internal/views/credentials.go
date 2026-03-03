package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) ListAppStoreCredentials(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	creds, err := v.Controller.GetAppStoreCredentials(user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, creds, http.StatusOK)
}

func (v *HttpViews) CreateAppStoreCredential(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	if !(user.SubscriptionStatus == "active" || user.SubscriptionStatus == "trial") {
		utils.JSONErrorResponse(w, "you do not have access to this resource", http.StatusForbidden)
		go v.Controller.CRMService.SendEvent(user, constants.BREVO_EVENT_SUBSCRIPTION_NEED, map[string]any{
			"action": "create_app_store_credential",
		})
		return
	}

	var req requests.AppStoreCredentialsCreateRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := utils.ValidateFunc[requests.AppStoreCredentialsCreateRequest](req, validate); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	cred, err := v.Controller.CreateAppStoreCredential(user, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, cred, http.StatusCreated)
}

func (v *HttpViews) GetAppStoreCredential(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	credID := chi.URLParam(r, "credentialId")
	if credID == "" {
		utils.JSONErrorResponse(w, "credential_id is required", http.StatusBadRequest)
		return
	}

	cred, err := v.Controller.GetAppStoreCredentialById(user, credID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, cred, http.StatusOK)
}

func (v *HttpViews) UpdateAppStoreCredential(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	if !(user.SubscriptionStatus == "active" || user.SubscriptionStatus == "trial") {
		utils.JSONErrorResponse(w, "you do not have access to this resource", http.StatusForbidden)
		go v.Controller.CRMService.SendEvent(user, constants.BREVO_EVENT_SUBSCRIPTION_NEED, map[string]any{
			"action": "create_google_play_credential",
		})
		return
	}

	credID := chi.URLParam(r, "credentialId")
	if credID == "" {
		utils.JSONErrorResponse(w, "credential_id is required", http.StatusBadRequest)
		return
	}

	var req requests.AppStoreCredentialsCreateRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := utils.ValidateFunc[requests.AppStoreCredentialsCreateRequest](req, validate); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	cred, err := v.Controller.UpdateAppStoreCredential(user, credID, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, cred, http.StatusOK)
}

func (v *HttpViews) DeleteAppStoreCredential(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	credID := chi.URLParam(r, "credentialId")
	if credID == "" {
		utils.JSONErrorResponse(w, "credential_id is required", http.StatusBadRequest)
		return
	}

	err = v.Controller.DeleteAppStoreCredential(user, credID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, "", http.StatusNoContent)
}

func (v *HttpViews) ListGooglePlayCredentials(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	creds, err := v.Controller.GetGooglePlayCredentials(user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, creds, http.StatusOK)

}

func (v *HttpViews) CreateGooglePlayCredential(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	if !(user.SubscriptionStatus == "active" || user.SubscriptionStatus == "trial") {
		utils.JSONErrorResponse(w, "you do not have access to this resource", http.StatusForbidden)
		go v.Controller.CRMService.SendEvent(user, constants.BREVO_EVENT_SUBSCRIPTION_NEED, map[string]any{
			"action": "create_google_play_credential",
		})
		return
	}

	var req requests.GooglePlayCredentialsCreateRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := utils.ValidateFunc[requests.GooglePlayCredentialsCreateRequest](req, validate); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	cred, err := v.Controller.CreateGooglePlayCredential(user, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, cred, http.StatusCreated)
}

func (v *HttpViews) GetGooglePlayCredential(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	credID := chi.URLParam(r, "credentialId")
	if credID == "" {
		utils.JSONErrorResponse(w, "credential_id is required", http.StatusBadRequest)
		return
	}

	cred, err := v.Controller.GetGooglePlayCredentialById(user, credID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, cred, http.StatusOK)
}

func (v *HttpViews) UpdateGooglePlayCredential(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	if !(user.SubscriptionStatus == "active" || user.SubscriptionStatus == "trial") {
		utils.JSONErrorResponse(w, "you do not have access to this resource", http.StatusForbidden)
		go v.Controller.CRMService.SendEvent(user, constants.BREVO_EVENT_SUBSCRIPTION_NEED, map[string]any{
			"action": "create_google_play_credential",
		})
		return
	}

	credID := chi.URLParam(r, "credentialId")
	if credID == "" {
		utils.JSONErrorResponse(w, "credential_id is required", http.StatusBadRequest)
		return
	}

	var req requests.GooglePlayCredentialsCreateRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := utils.ValidateFunc[requests.GooglePlayCredentialsCreateRequest](req, validate); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	cred, err := v.Controller.UpdateGooglePlayCredential(user, credID, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, cred, http.StatusOK)
}

func (v *HttpViews) DeleteGooglePlayCredential(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	credID := chi.URLParam(r, "credentialId")
	if credID == "" {
		utils.JSONErrorResponse(w, "credential_id is required", http.StatusBadRequest)
		return
	}

	err = v.Controller.DeleteGooglePlayCredential(user, credID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, "", http.StatusNoContent)
}
