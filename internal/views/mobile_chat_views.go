package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/utils"
)

// GET /v2/mobile/{projectId}/config (authenticated)
// projectId can be either a Project ID (URL-wrapper) or an AppProject ID (converted app)
func (v *HttpViews) GetMobileAppConfig(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	projectID := chi.URLParam(r, "projectId")

	// Try Project (URL-wrapper) first, then fall back to AppProject (converted app)
	var oneSignalAppID string
	authorized := false
	project, err := v.Controller.GetProjectModelById(projectID)
	if err != nil {
		appProject, appErr := v.Controller.DB.GetAppProjectByID(projectID)
		if appErr != nil {
			utils.JSONErrorCode(w, constants.ErrProjectNotFound, http.StatusNotFound, nil)
			return
		}
		if err := v.Controller.AuthorizeProjectAccess(user, appProject); err == nil {
			authorized = true
		}
	} else {
		oneSignalAppID = project.OneSignalAppID
		if project.UserID == user.ID {
			authorized = true
		}
	}
	if !authorized {
		utils.JSONErrorCode(w, constants.ErrForbidden, http.StatusForbidden, nil)
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
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	projectID := chi.URLParam(r, "projectId")

	// Try Project (URL-wrapper) first, then fall back to AppProject (converted app)
	var oneSignalAppID string
	authorized := false
	project, err := v.Controller.GetProjectModelById(projectID)
	if err != nil {
		appProject, appErr := v.Controller.DB.GetAppProjectByID(projectID)
		if appErr != nil {
			utils.JSONErrorCode(w, constants.ErrProjectNotFound, http.StatusNotFound, nil)
			return
		}
		if err := v.Controller.AuthorizeProjectAccess(user, appProject); err == nil {
			authorized = true
		}
	} else {
		oneSignalAppID = project.OneSignalAppID
		if project.UserID == user.ID {
			authorized = true
		}
	}
	if !authorized {
		utils.JSONErrorCode(w, constants.ErrForbidden, http.StatusForbidden, nil)
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
		OTAEnabled           *bool     `json:"ota_enabled"`
		OTAReleaseChannel    *string   `json:"ota_release_channel"`
	}
	if err := utils.ParseJSONBody(r, &body); err != nil {
		utils.ValidationError(w, err)
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
	if body.OTAEnabled != nil {
		config.OTAEnabled = *body.OTAEnabled
	}
	if body.OTAReleaseChannel != nil {
		config.OTAReleaseChannel = *body.OTAReleaseChannel
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
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, buildMobileConfigResponse(updated, oneSignalAppID), http.StatusOK)
}

// GET /v1/mobile/{projectId}/config (public, no auth)
func (v *HttpViews) GetPublicMobileConfig(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectId")

	project, err := v.Controller.GetProjectModelById(projectID)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrProjectNotFound, http.StatusNotFound, nil)
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
		DarkModeEnabled:      false,
		RatingSessions:       5,
		CachingStrategy:      "network-first",
		OTAEnabled:           true,
		OTAReleaseChannel:    "production",
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
		"pull_to_refresh":        config.PullToRefreshEnabled,
		"external_browser_links": config.ExternalBrowserLinks,
		"bridge": map[string]any{
			"enabled": config.BridgeEnabled,
			"methods": bridgeMethods,
		},
		"biometric":    map[string]any{"enabled": config.BiometricEnabled},
		"dark_mode":    map[string]any{"enabled": config.DarkModeEnabled},
		"rating":       map[string]any{"enabled": config.RatingEnabled, "sessions": config.RatingSessions},
		"update_check": map[string]any{"enabled": config.UpdateCheckEnabled},
		"ota": map[string]any{
			"enabled":         config.OTAEnabled,
			"release_channel": config.OTAReleaseChannel,
		},
		"offline":      map[string]any{"enabled": config.OfflineEnabled, "caching_strategy": config.CachingStrategy},
		"permissions":  permissions,
		"deep_linking": map[string]any{"url_scheme": config.URLScheme, "universal_link_domain": config.UniversalLinkDomain},
	}
}
