package agent

import (
	"context"
	"encoding/json"
	"fmt"
)

// ReadMobileConfigTool exposes the project's MobileAppConfig to the AI agent
// so it knows which native features are enabled, what the deep-link scheme is,
// etc. Used by mobile_app_native projects.
//
// No input parameters — pure read.
type ReadMobileConfigTool struct{}

var _ ToolExecutor = (*ReadMobileConfigTool)(nil)

func (t *ReadMobileConfigTool) Name() string {
	return "read_mobile_config"
}

func (t *ReadMobileConfigTool) Description() string {
	return "Read the current mobile app configuration: enabled native features (bridge methods), URL scheme for deep links, universal link domain, biometric flag, authentication config (Better-Auth providers/scheme), in-app purchase config (IAP + RevenueCat provisioning), push notification status (OneSignal provisioning), OTA flag, App Store + Play Store URLs (for the marketing landing at backend/app/page.tsx), and the landing_page_published flag. Use this BEFORE writing code that depends on any of these features."
}

func (t *ReadMobileConfigTool) Schema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"required":             []string{},
		"additionalProperties": false,
	}
}

type readMobileConfigOutput struct {
	EnabledNativeFeatures []string `json:"enabled_native_features"`
	URLScheme             string   `json:"url_scheme"`
	UniversalLinkDomain   string   `json:"universal_link_domain"`
	BiometricEnabled      bool     `json:"biometric_enabled"`
	Auth                  authInfo `json:"auth"`
	IAP                   iapInfo  `json:"iap"`
	Push                  pushInfo `json:"push"`
	// OTAEnabled — true when the project ships JS-only updates via
	// self-hosted expo-updates.
	OTAEnabled bool `json:"ota_enabled"`

	// Landing-page metadata (P3.1).
	AppStoreURL          string `json:"app_store_url"`
	PlayStoreURL         string `json:"play_store_url"`
	LandingPagePublished bool   `json:"landing_page_published"`

	// Force update (P3.2) — read-only. The agent should NOT modify these via
	// configure_mobile_bridge; they're operator-controlled (admin/owner only)
	// because bumping the floor locks out users on older builds.
	MinBuildNumberIOS     int64  `json:"min_build_number_ios"`
	MinBuildNumberAndroid int64  `json:"min_build_number_android"`
	MinVersionMessage     string `json:"min_version_message"`
}

// authInfo summarises the project's authentication configuration.
type authInfo struct {
	Enabled   bool     `json:"enabled"`
	Providers []string `json:"providers"`
	Scheme    string   `json:"scheme"`
}

// iapInfo summarises in-app purchase configuration.
type iapInfo struct {
	Enabled              bool `json:"enabled"`
	RevenueCatConfigured bool `json:"revenuecat_configured"`
}

// pushInfo summarises push-notification configuration.
type pushInfo struct {
	Enabled    bool `json:"enabled"`
	Configured bool `json:"configured"`
}

func (t *ReadMobileConfigTool) Execute(ctx context.Context, rc *RunContext, _ json.RawMessage) (string, bool, error) {
	if rc.Deps == nil || rc.Deps.DB == nil {
		return "", false, fmt.Errorf("read_mobile_config: no database available")
	}

	cfg, err := rc.Deps.DB.GetMobileAppConfig(rc.Project.ID.String())
	if err != nil {
		// Missing config is not an error — new projects may not have one yet.
		// Return empty feature list + OTA-on default (matches the column's
		// NOT NULL DEFAULT true) so the agent can proceed without crashing.
		out := readMobileConfigOutput{
			EnabledNativeFeatures: []string{},
			Auth:                  authInfo{Providers: []string{}},
			OTAEnabled:            true,
		}
		b, _ := json.Marshal(out)
		return string(b), false, nil
	}

	out := readMobileConfigOutput{EnabledNativeFeatures: []string{}, OTAEnabled: true}
	out.Auth.Providers = []string{}
	if cfg != nil {
		if cfg.BridgeMethods != nil {
			out.EnabledNativeFeatures = cfg.BridgeMethods
		}
		out.URLScheme = cfg.URLScheme
		out.UniversalLinkDomain = cfg.UniversalLinkDomain
		out.BiometricEnabled = cfg.BiometricEnabled
		out.Auth.Enabled = cfg.AuthEnabled
		if cfg.AuthProviders != nil {
			out.Auth.Providers = cfg.AuthProviders
		}
		out.Auth.Scheme = cfg.AuthScheme
		out.IAP.Enabled = cfg.IAPEnabled
		out.IAP.RevenueCatConfigured = cfg.RevenueCatPublicKeyIOS != "" && cfg.RevenueCatPublicKeyAndroid != ""
		out.OTAEnabled = cfg.OTAEnabled
		out.AppStoreURL = cfg.AppStoreURL
		out.PlayStoreURL = cfg.PlayStoreURL
		out.LandingPagePublished = cfg.LandingPagePublished
		out.MinBuildNumberIOS = cfg.MinBuildNumberIOS
		out.MinBuildNumberAndroid = cfg.MinBuildNumberAndroid
		out.MinVersionMessage = cfg.MinVersionMessage

		out.Push.Enabled = cfg.PushEnabled
		// Configured = MobileAppConfig has a real OneSignal app id persisted by
		// provisionOneSignalApp. Pre-provisioning, the column is empty.
		out.Push.Configured = cfg.OneSignalAppID != ""
	}

	b, err := json.Marshal(out)
	if err != nil {
		return "", false, fmt.Errorf("marshal output: %w", err)
	}
	return string(b), false, nil
}
