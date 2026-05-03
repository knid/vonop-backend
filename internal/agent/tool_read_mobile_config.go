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
	return "Read the current mobile app configuration: enabled native features (bridge methods), URL scheme for deep links, universal link domain, biometric flag, AND in-app purchase config (whether IAP is enabled and whether RevenueCat is provisioned, so paywalls and entitlement gating can rely on lib/iap.ts). Use this BEFORE writing code that depends on a native feature or IAP, to confirm it's enabled."
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
	IAP                   iapInfo  `json:"iap"`
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

func (t *ReadMobileConfigTool) Execute(ctx context.Context, rc *RunContext, _ json.RawMessage) (string, bool, error) {
	if rc.Deps == nil || rc.Deps.DB == nil {
		return "", false, fmt.Errorf("read_mobile_config: no database available")
	}

	cfg, err := rc.Deps.DB.GetMobileAppConfig(rc.Project.ID.String())
	if err != nil {
		// Missing config is not an error — new projects may not have one yet.
		// Return empty feature list so the agent can proceed without crashing.
		out := readMobileConfigOutput{EnabledNativeFeatures: []string{}}
		b, _ := json.Marshal(out)
		return string(b), false, nil
	}

	out := readMobileConfigOutput{EnabledNativeFeatures: []string{}}
	if cfg != nil {
		if cfg.BridgeMethods != nil {
			out.EnabledNativeFeatures = cfg.BridgeMethods
		}
		out.URLScheme = cfg.URLScheme
		out.UniversalLinkDomain = cfg.UniversalLinkDomain
		out.BiometricEnabled = cfg.BiometricEnabled
		out.IAP.Enabled = cfg.IAPEnabled
		out.IAP.RevenueCatConfigured = cfg.RevenueCatPublicKeyIOS != "" && cfg.RevenueCatPublicKeyAndroid != ""
	}

	b, err := json.Marshal(out)
	if err != nil {
		return "", false, fmt.Errorf("marshal output: %w", err)
	}
	return string(b), false, nil
}
