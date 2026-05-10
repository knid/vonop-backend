package views

import (
	"fmt"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

// toGitHubExportResponse converts a model to a response struct.
func toGitHubExportResponse(export *models.AppGitHubExport) responses.GitHubExportResponse {
	resp := responses.GitHubExportResponse{
		ID:           export.ID.String(),
		RepoFullName: export.RepoFullName,
		RepoURL:      export.RepoURL,
		Private:      export.Private,
		CreatedAt:    export.CreatedAt,
	}
	if !export.LastPushedAt.IsZero() {
		resp.LastPushedAt = &export.LastPushedAt
	}
	return resp
}

// GET /v2/auth/github — redirects to GitHub OAuth authorization page.
func (v *HttpViews) GitHubAuthRedirect(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	clientID := v.Controller.SecretManager.GetSecret("GITHUB_CLIENT_ID")
	clientSecret := v.Controller.SecretManager.GetSecret("GITHUB_CLIENT_SECRET")
	redirectURI := v.Controller.SecretManager.GetSecret("GITHUB_REDIRECT_URI")

	state := utils.GenerateOAuthState(user.ID.String(), clientSecret)
	authURL := fmt.Sprintf(
		"https://github.com/login/oauth/authorize?client_id=%s&redirect_uri=%s&scope=repo&state=%s",
		url.QueryEscape(clientID), url.QueryEscape(redirectURI), url.QueryEscape(state),
	)
	http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
}

// GET /v2/auth/github/callback — handles the OAuth callback from GitHub.
func (v *HttpViews) GitHubAuthCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	stateStr := r.URL.Query().Get("state")

	if code == "" || stateStr == "" {
		utils.JSONError(w, constants.ErrValidationRequired, "Missing code or state parameter.", http.StatusBadRequest, nil)
		return
	}

	clientSecret := v.Controller.SecretManager.GetSecret("GITHUB_CLIENT_SECRET")
	userID, err := utils.VerifyOAuthState(stateStr, clientSecret)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthOAuthStateInvalid, http.StatusBadRequest, nil)
		return
	}

	user, err := v.Controller.GetUserById(userID)
	if err != nil {
		utils.JSONError(w, constants.ErrNotFound, "User not found.", http.StatusNotFound, nil)
		return
	}

	if err := v.Controller.ConnectGitHub(user, code); err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	// Redirect to frontend with success
	frontendURL := v.Controller.SecretManager.GetSecret("FRONTEND_URL")
	http.Redirect(w, r, frontendURL+"/settings?github=connected", http.StatusTemporaryRedirect)
}

// DELETE /v2/auth/github — disconnects GitHub account.
func (v *HttpViews) DisconnectGitHub(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	if err := v.Controller.DisconnectGitHub(user); err != nil {
		utils.JSONInternalError(w, err)
		return
	}
	utils.JSONResponse(w, responses.SuccessResponse{Success: true}, http.StatusOK)
}

// GET /v2/auth/github/status — returns GitHub connection status.
func (v *HttpViews) GetGitHubConnectionStatus(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	resp := responses.GitHubConnectionResponse{
		Connected: user.GitHubUsername != "",
		Username:  user.GitHubUsername,
	}
	utils.JSONResponse(w, resp, http.StatusOK)
}

// POST /v2/apps/{appId}/github/export — creates a new GitHub repo and pushes.
func (v *HttpViews) ExportToGitHub(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	if requireActiveSubscription(w, v.Controller, user) {
		return
	}

	// Check plan limit
	limits, limitsErr := v.Controller.GetUserLimits(user)
	if limitsErr == nil && !limits.CanExportToGitHub {
		go v.Controller.EventService.SendEvent(user, constants.EVENT_SUBSCRIPTION_NEED, map[string]any{"action": "export_to_github"})
		utils.JSONError(w, constants.ErrGitHubExportNotAvailable,
			"GitHub export is not available on your plan", http.StatusForbidden, nil)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	var req requests.ExportToGitHubRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.ValidationError(w, err)
		return
	}
	validate := validator.New()
	if err := utils.ValidateFunc[requests.ExportToGitHubRequest](req, validate); err != nil {
		utils.ValidationError(w, err)
		return
	}

	export, err := v.Controller.ExportToGitHub(user, project.ID, req)
	if err != nil {
		errMsg := err.Error()
		if errMsg == "GITHUB_REPO_EXISTS" {
			utils.JSONError(w, constants.ErrGitHubRepoExists,
				"A repository with this name already exists on your GitHub account.", http.StatusConflict, nil)
			return
		}
		utils.JSONInternalError(w, err)
		return
	}

	go v.Controller.TrackUserEvent(user, constants.EVENT_GITHUB_EXPORTED,
		v.Controller.AppEventProps(project.ID.String()).
			Set("repo_name", req.RepoName))

	go v.Controller.EventService.SendEvent(user, constants.EVENT_GITHUB_EXPORTED, map[string]any{
		"app_id":    project.ID.String(),
		"repo_name": req.RepoName,
	})

	utils.JSONResponse(w, toGitHubExportResponse(export), http.StatusCreated)
}

// POST /v2/apps/{appId}/github/push — pushes latest changes to existing GitHub repo.
func (v *HttpViews) PushToGitHub(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	if requireActiveSubscription(w, v.Controller, user) {
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	export, err := v.Controller.PushToGitHub(user, project.ID)
	if err != nil {
		errMsg := err.Error()
		switch errMsg {
		case "GITHUB_AUTH_EXPIRED":
			utils.JSONError(w, constants.ErrGitHubAuthExpired,
				"your GitHub token has expired — please reconnect", http.StatusUnauthorized, nil)
		case "GITHUB_REPO_NOT_FOUND":
			utils.JSONError(w, constants.ErrGitHubRepoNotFound,
				"the GitHub repository was deleted — export again to create a new one", http.StatusNotFound, nil)
		case "GITHUB_PUSH_REJECTED":
			utils.JSONError(w, constants.ErrGitHubPushRejected,
				"push rejected by GitHub — the remote history has diverged. Remove the export and re-export.", http.StatusConflict, nil)
		case "GITHUB_PUSH_TIMEOUT":
			utils.JSONError(w, constants.ErrGitHubPushTimeout,
				"push timed out — try again later", http.StatusGatewayTimeout, nil)
		default:
			utils.JSONInternalError(w, err)
		}
		return
	}

	go v.Controller.TrackUserEvent(user, constants.EVENT_GITHUB_PUSHED,
		v.Controller.AppEventProps(project.ID.String()))

	go v.Controller.EventService.SendEvent(user, constants.EVENT_GITHUB_PUSHED, map[string]any{
		"app_id": project.ID.String(),
	})

	utils.JSONResponse(w, toGitHubExportResponse(export), http.StatusOK)
}

// GET /v2/apps/{appId}/github — returns the export record for a project.
func (v *HttpViews) GetGitHubExport(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	appID := chi.URLParam(r, "appId")
	_, err = v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	parsedID, _ := uuid.Parse(appID)
	export, err := v.Controller.GetGitHubExport(parsedID)
	if err != nil {
		utils.JSONError(w, constants.ErrNotFound, "No GitHub export for this project.", http.StatusNotFound, nil)
		return
	}

	utils.JSONResponse(w, toGitHubExportResponse(export), http.StatusOK)
}

// DELETE /v2/apps/{appId}/github — removes the GitHub export for a project.
func (v *HttpViews) RemoveGitHubExport(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	if err := v.Controller.RemoveGitHubExport(user, project.ID); err != nil {
		utils.JSONInternalError(w, err)
		return
	}
	utils.JSONResponse(w, responses.SuccessResponse{Success: true}, http.StatusOK)
}
