package views

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
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
		injection.SortOrder = *body.Order
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
// Classifies the user's mobile studio prompt. If url_wrapper, auto-creates the
// project and returns project_id + proxy_url so the frontend goes straight to
// preview + injection chat.
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
		URL    string `json:"url"` // When provided, skip classification and treat as url_wrapper directly
	}
	if err := utils.ParseJSONBody(r, &body); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	if body.Prompt == "" && body.URL == "" {
		utils.JSONErrorResponse(w, "prompt or url is required", http.StatusBadRequest)
		return
	}

	var intent string
	var extractedURL string

	if body.URL != "" {
		// Direct URL conversion (e.g. "Convert to Mobile App" button) — skip LLM classification
		intent = "url_wrapper"
		extractedURL = body.URL
	} else {
		intent, extractedURL = v.Controller.ClassifyMobileIntent(r.Context(), body.Prompt)
	}

	if intent == "url_wrapper" && extractedURL != "" {
		// Auto-create the project so the frontend can go straight to preview
		project, err := v.Controller.QuickCreateMobileProject(user, extractedURL)
		if err != nil {
			status := http.StatusInternalServerError
			if err.Error() == "you have reached your project limit" {
				status = http.StatusBadRequest
			}
			utils.JSONErrorResponse(w, err.Error(), status)
			return
		}

		proxyURL := fmt.Sprintf("https://proxy.wemob.io/?url=%s&project_id=%s",
			url.QueryEscape(extractedURL), project.ID.String())

		// Fetch site metadata (title, description, favicon) — best effort
		var siteMeta *utils.SiteMetadata
		htmlContent, fetchErr := utils.FetchHTML(
			extractedURL,
			v.Controller.SecretManager.GetSecret("PROXY_HOST"),
			v.Controller.SecretManager.GetSecret("PROXY_AUTH"),
		)
		if fetchErr == nil && htmlContent != "" {
			meta := utils.ExtractSiteMetadata(htmlContent, extractedURL)
			siteMeta = &meta
		} else if fetchErr != nil {
			slog.Warn("Failed to fetch site metadata", "url", extractedURL, "error", fetchErr)
		}

		// Generate a conversational welcome message using the fast model
		// The LLM matches the user's language automatically
		welcomeMsg := ""
		if siteMeta != nil {
			welcomeMsg = v.Controller.GenerateURLWrapperWelcome(r.Context(), extractedURL, *siteMeta, body.Prompt)
		}

		utils.JSONResponse(w, map[string]any{
			"intent":          "url_wrapper",
			"url":             extractedURL,
			"project_id":      project.ID.String(),
			"proxy_url":       proxyURL,
			"welcome_message": welcomeMsg,
		}, http.StatusOK)
		return
	}

	utils.JSONResponse(w, map[string]any{
		"intent": intent,
		"url":    "",
	}, http.StatusOK)
}

// GET /v2/mobile/{projectId}/config (authenticated)
// projectId can be either a Project ID (URL-wrapper) or an AppProject ID (converted app)
func (v *HttpViews) GetMobileAppConfig(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	projectID := chi.URLParam(r, "projectId")

	// Try Project (URL-wrapper) first, then fall back to AppProject (converted app)
	var ownerID uuid.UUID
	var oneSignalAppID string
	project, err := v.Controller.GetProjectModelById(projectID)
	if err != nil {
		appProject, appErr := v.Controller.DB.GetAppProjectByID(projectID)
		if appErr != nil {
			utils.JSONErrorResponse(w, "project not found", http.StatusNotFound)
			return
		}
		ownerID = appProject.UserID
	} else {
		ownerID = project.UserID
		oneSignalAppID = project.OneSignalAppID
	}
	if ownerID != user.ID {
		utils.JSONErrorResponse(w, "unauthorized", http.StatusForbidden)
		return
	}

	config, err := v.Controller.DB.GetMobileAppConfig(projectID)
	if err != nil {
		// Return defaults if no config exists
		utils.JSONResponse(w, defaultMobileConfigResponse(oneSignalAppID), http.StatusOK)
		return
	}

	utils.JSONResponse(w, buildMobileConfigResponse(config, oneSignalAppID), http.StatusOK)
}

// PUT /v2/mobile/{projectId}/config (authenticated)
// projectId can be either a Project ID (URL-wrapper) or an AppProject ID (converted app)
func (v *HttpViews) UpdateMobileAppConfig(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	projectID := chi.URLParam(r, "projectId")

	// Try Project (URL-wrapper) first, then fall back to AppProject (converted app)
	var ownerID uuid.UUID
	var oneSignalAppID string
	project, err := v.Controller.GetProjectModelById(projectID)
	if err != nil {
		appProject, appErr := v.Controller.DB.GetAppProjectByID(projectID)
		if appErr != nil {
			utils.JSONErrorResponse(w, "project not found", http.StatusNotFound)
			return
		}
		ownerID = appProject.UserID
	} else {
		ownerID = project.UserID
		oneSignalAppID = project.OneSignalAppID
	}
	if ownerID != user.ID {
		utils.JSONErrorResponse(w, "unauthorized", http.StatusForbidden)
		return
	}

	var body struct {
		PushEnabled          *bool     `json:"push_enabled"`
		AnalyticsEnabled     *bool     `json:"analytics_enabled"`
		PullToRefreshEnabled *bool     `json:"pull_to_refresh_enabled"`
		ExternalBrowserLinks *bool     `json:"external_browser_links"`
		BridgeEnabled        *bool     `json:"bridge_enabled"`
		BridgeMethods        *[]string `json:"bridge_methods"`
		Permissions          *[]string `json:"permissions"`
		URLScheme            *string   `json:"url_scheme"`
		UniversalLinkDomain  *string   `json:"universal_link_domain"`
		OfflineEnabled       *bool     `json:"offline_enabled"`
		CachingStrategy      *string   `json:"caching_strategy"`
		BiometricEnabled     *bool     `json:"biometric_enabled"`
		DarkModeEnabled      *bool     `json:"dark_mode_enabled"`
		RatingEnabled        *bool     `json:"rating_enabled"`
		RatingSessions       *int      `json:"rating_sessions"`
		UpdateCheckEnabled   *bool     `json:"update_check_enabled"`
	}
	if err := utils.ParseJSONBody(r, &body); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Get existing config or create defaults
	config, err := v.Controller.DB.GetMobileAppConfig(projectID)
	if err != nil {
		parsedID, _ := uuid.Parse(projectID)
		config = &models.MobileAppConfig{
			ProjectID:            parsedID,
			PushEnabled:          true,
			AnalyticsEnabled:     true,
			PullToRefreshEnabled: true,
			ExternalBrowserLinks: true,
			BridgeEnabled:        true,
		}
	}

	if body.AnalyticsEnabled != nil {
		config.AnalyticsEnabled = *body.AnalyticsEnabled
	}
	if body.PullToRefreshEnabled != nil {
		config.PullToRefreshEnabled = *body.PullToRefreshEnabled
	}
	if body.ExternalBrowserLinks != nil {
		config.ExternalBrowserLinks = *body.ExternalBrowserLinks
	}
	if body.BridgeMethods != nil {
		config.BridgeMethods = *body.BridgeMethods
	}
	if body.Permissions != nil {
		config.Permissions = *body.Permissions
	}
	if body.URLScheme != nil {
		config.URLScheme = *body.URLScheme
	}
	if body.UniversalLinkDomain != nil {
		config.UniversalLinkDomain = *body.UniversalLinkDomain
	}
	if body.OfflineEnabled != nil {
		config.OfflineEnabled = *body.OfflineEnabled
	}
	if body.CachingStrategy != nil {
		config.CachingStrategy = *body.CachingStrategy
	}
	if body.BiometricEnabled != nil {
		config.BiometricEnabled = *body.BiometricEnabled
	}
	if body.DarkModeEnabled != nil {
		config.DarkModeEnabled = *body.DarkModeEnabled
	}
	if body.RatingEnabled != nil {
		config.RatingEnabled = *body.RatingEnabled
	}
	if body.RatingSessions != nil {
		config.RatingSessions = *body.RatingSessions
	}
	if body.UpdateCheckEnabled != nil {
		config.UpdateCheckEnabled = *body.UpdateCheckEnabled
	}

	// Bridge and push are always on — ignore any request to disable them
	config.BridgeEnabled = true
	config.PushEnabled = true

	// Ensure notifications permission is always present
	hasNotifications := false
	for _, p := range config.Permissions {
		if p == "notifications" {
			hasNotifications = true
			break
		}
	}
	if !hasNotifications {
		config.Permissions = append(config.Permissions, "notifications")
	}

	updated, err := v.Controller.DB.UpsertMobileAppConfig(config)
	if err != nil {
		utils.JSONErrorResponse(w, "failed to update config", http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, buildMobileConfigResponse(updated, oneSignalAppID), http.StatusOK)
}

// GET /v1/mobile/{projectId}/config (public, no auth)
func (v *HttpViews) GetPublicMobileConfig(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectId")

	project, err := v.Controller.GetProjectModelById(projectID)
	if err != nil {
		utils.JSONErrorResponse(w, "project not found", http.StatusNotFound)
		return
	}

	config, err := v.Controller.DB.GetMobileAppConfig(projectID)
	if err != nil {
		utils.JSONResponse(w, defaultMobileConfigResponse(project.OneSignalAppID), http.StatusOK)
		return
	}

	utils.JSONResponse(w, buildMobileConfigResponse(config, project.OneSignalAppID), http.StatusOK)
}

func defaultMobileConfigResponse(oneSignalAppID string) map[string]any {
	return buildMobileConfigResponse(&models.MobileAppConfig{
		PushEnabled:          true,
		AnalyticsEnabled:     true,
		PullToRefreshEnabled: true,
		ExternalBrowserLinks: true,
		BridgeEnabled:        true,
		DarkModeEnabled:      true,
		RatingSessions:       5,
		CachingStrategy:      "network-first",
	}, oneSignalAppID)
}

func buildMobileConfigResponse(config *models.MobileAppConfig, oneSignalAppID string) map[string]any {
	bridgeMethods := config.BridgeMethods
	if bridgeMethods == nil {
		bridgeMethods = []string{}
	}

	permissions := config.Permissions
	if permissions == nil {
		permissions = []string{"notifications"}
	} else {
		hasNotif := false
		for _, p := range permissions {
			if p == "notifications" {
				hasNotif = true
				break
			}
		}
		if !hasNotif {
			permissions = append(permissions, "notifications")
		}
	}

	return map[string]any{
		"push": map[string]any{
			"enabled":  config.PushEnabled,
			"provider": "onesignal",
			"app_id":   oneSignalAppID,
		},
		"analytics": map[string]any{
			"enabled":  config.AnalyticsEnabled,
			"endpoint": "https://api.wemob.io",
		},
		"pull_to_refresh":       config.PullToRefreshEnabled,
		"external_browser_links": config.ExternalBrowserLinks,
		"bridge": map[string]any{
			"enabled": config.BridgeEnabled,
			"methods": bridgeMethods,
		},
		"biometric":    map[string]any{"enabled": config.BiometricEnabled},
		"dark_mode":    map[string]any{"enabled": config.DarkModeEnabled},
		"rating":       map[string]any{"enabled": config.RatingEnabled, "sessions": config.RatingSessions},
		"update_check": map[string]any{"enabled": config.UpdateCheckEnabled},
		"offline":      map[string]any{"enabled": config.OfflineEnabled, "caching_strategy": config.CachingStrategy},
		"permissions":  permissions,
		"deep_linking": map[string]any{"url_scheme": config.URLScheme, "universal_link_domain": config.UniversalLinkDomain},
	}
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
