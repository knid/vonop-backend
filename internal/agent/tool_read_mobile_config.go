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
	return "Read the current mobile app configuration: enabled native features (bridge methods), URL scheme for deep links, universal link domain, biometric flag, App Store + Play Store URLs (for the marketing landing at backend/app/page.tsx), and the landing_page_published flag. Use this BEFORE writing code that depends on a native feature, and BEFORE writing the marketing landing page so you know whether to render real store-badge links or 'Coming soon' placeholders."
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

	// Landing-page metadata (P3.1) — empty until the user publishes to the
	// stores. The agent uses these to decide between real store-badge CTAs
	// and "Coming soon" placeholders on backend/app/page.tsx.
	AppStoreURL          string `json:"app_store_url"`
	PlayStoreURL         string `json:"play_store_url"`
	LandingPagePublished bool   `json:"landing_page_published"`
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
		out.AppStoreURL = cfg.AppStoreURL
		out.PlayStoreURL = cfg.PlayStoreURL
		out.LandingPagePublished = cfg.LandingPagePublished
	}

	b, err := json.Marshal(out)
	if err != nil {
		return "", false, fmt.Errorf("marshal output: %w", err)
	}
	return string(b), false, nil
}
