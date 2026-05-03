package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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
	return "Read the current mobile app configuration: enabled native features (bridge methods), URL scheme for deep links, universal link domain, biometric flag, authentication config (whether Better-Auth is enabled, which providers are wired, OAuth redirect scheme), in-app purchase config (whether IAP is enabled and whether RevenueCat is provisioned, so paywalls and entitlement gating can rely on lib/iap.ts), AND push notification status (whether OneSignal is provisioned, so opt-in UI patterns and lib/push.ts helpers are safe to call). Use this BEFORE writing code that depends on a native feature, auth, IAP, or push, to confirm it's enabled."
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
}

// authInfo summarises the project's authentication configuration. Sourced
// from MobileAppConfig.AuthEnabled / AuthProviders / AuthScheme. The
// per-customer BETTER_AUTH_SECRET is intentionally NOT exposed here — it's
// a deploy-time concern, encrypted via CredStore, and never returned to
// the agent.
type authInfo struct {
	Enabled   bool     `json:"enabled"`
	Providers []string `json:"providers"`
	Scheme    string   `json:"scheme"`
}

// iapInfo summarises in-app purchase configuration. RevenueCatConfigured is
// true when both platform public keys are populated (RC provisioning
// succeeded). The actual public SDK keys are NOT exposed — they're build-
// time-baked into the binary by the Codemagic pipeline and the agent
// doesn't need them to write paywall code.
type iapInfo struct {
	Enabled              bool `json:"enabled"`
	RevenueCatConfigured bool `json:"revenuecat_configured"`
}

// pushInfo summarises push-notification configuration. Configured is true
// when the per-tenant OneSignal app has been provisioned (the v1 shadow row's
// app ID is no longer the "expo-shadow-{projectID}" placeholder). The actual
// app ID is NOT exposed — it's build-time-baked into the binary by Codemagic
// and the agent doesn't need it to write opt-in UI.
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
		// Return empty feature list so the agent can proceed without crashing.
		out := readMobileConfigOutput{
			EnabledNativeFeatures: []string{},
			Auth:                  authInfo{Providers: []string{}},
		}
		b, _ := json.Marshal(out)
		return string(b), false, nil
	}

	out := readMobileConfigOutput{EnabledNativeFeatures: []string{}}
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

		out.Push.Enabled = cfg.PushEnabled
		// Configured = there's a real OneSignal app ID on the v1 shadow row
		// (not the "expo-shadow-{projectID}" placeholder). Resolve lazily
		// to keep the tool cheap when push isn't enabled.
		if out.Push.Enabled {
			shadowURL := "expo://" + rc.Project.Slug
			if v1, _ := rc.Deps.DB.GetProjectByUserAndURL(rc.Project.UserID.String(), shadowURL); v1 != nil && v1.OneSignalAppID != "" && !strings.HasPrefix(v1.OneSignalAppID, "expo-shadow-") {
				out.Push.Configured = true
			}
		}
	}

	b, err := json.Marshal(out)
	if err != nil {
		return "", false, fmt.Errorf("marshal output: %w", err)
	}
	return string(b), false, nil
}
