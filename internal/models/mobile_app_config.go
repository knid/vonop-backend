package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

type MobileAppConfig struct {
	bun.BaseModel `bun:"table:mobile_app_configs,alias:mac"`

	ID        uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	ProjectID uuid.UUID `bun:",notnull,type:uuid,unique" json:"project_id"`

	// Plugin toggles
	PushEnabled      bool `bun:",notnull,default:true" json:"push_enabled"`
	AnalyticsEnabled bool `bun:",notnull,default:true" json:"analytics_enabled"`

	// UI features
	PullToRefreshEnabled bool `bun:",notnull,default:true" json:"pull_to_refresh_enabled"`
	ExternalBrowserLinks bool `bun:",notnull,default:true" json:"external_browser_links"`

	// Bridge
	BridgeEnabled bool     `bun:",notnull,default:true" json:"bridge_enabled"`
	BridgeMethods []string `bun:",array,type:text[]" json:"bridge_methods"`

	// Permissions (build-time — injected into manifests)
	Permissions []string `bun:",array,type:text[]" json:"permissions"`

	// Deep linking (build-time)
	URLScheme           string `bun:",nullzero" json:"url_scheme"`
	UniversalLinkDomain string `bun:",nullzero" json:"universal_link_domain"`

	// Offline (runtime)
	OfflineEnabled  bool   `bun:",notnull,default:false" json:"offline_enabled"`
	CachingStrategy string `bun:",nullzero,default:'network-first'" json:"caching_strategy"`

	// Biometric (runtime)
	BiometricEnabled bool `bun:",notnull,default:false" json:"biometric_enabled"`

	// Dark mode (runtime)
	DarkModeEnabled bool `bun:",notnull,default:true" json:"dark_mode_enabled"`

	// Rating (runtime)
	RatingEnabled  bool `bun:",notnull,default:false" json:"rating_enabled"`
	RatingSessions int  `bun:",notnull,default:5" json:"rating_sessions"`

	// Updates (runtime)
	UpdateCheckEnabled bool `bun:",notnull,default:false" json:"update_check_enabled"`

	// Auth (build + deploy time).
	AuthEnabled   bool     `bun:",notnull,default:false" json:"auth_enabled"`
	AuthProviders []string `bun:",array,type:text[]" json:"auth_providers"`
	AuthScheme    string   `bun:",nullzero" json:"auth_scheme"`

	// IAP / RevenueCat (build-time — public keys baked into the binary).
	IAPEnabled                 bool   `bun:"iap_enabled,notnull,default:false" json:"iap_enabled"`
	RevenueCatProjectID        string `bun:"revenuecat_project_id,nullzero" json:"revenuecat_project_id"`
	RevenueCatIOSAppID         string `bun:"revenuecat_ios_app_id,nullzero" json:"revenuecat_ios_app_id"`
	RevenueCatAndroidAppID     string `bun:"revenuecat_android_app_id,nullzero" json:"revenuecat_android_app_id"`
	RevenueCatPublicKeyIOS     string `bun:"revenuecat_public_key_ios,nullzero" json:"revenuecat_public_key_ios"`
	RevenueCatPublicKeyAndroid string `bun:"revenuecat_public_key_android,nullzero" json:"revenuecat_public_key_android"`

	// Self-hosted OTA (expo-updates) — P2.1.
	OTAEnabled        bool   `bun:",notnull,default:true" json:"ota_enabled"`
	OTAReleaseChannel string `bun:",nullzero,default:'production'" json:"ota_release_channel"`

	// Landing page (P3.1) — store URLs become real CTAs once published.
	AppStoreURL          string `bun:",nullzero" json:"app_store_url"`
	PlayStoreURL         string `bun:",nullzero" json:"play_store_url"`
	LandingPagePublished bool   `bun:",notnull,default:false" json:"landing_page_published"`

	// Force update (P3.2) — per-platform minimum native build number.
	// 0 means "no minimum" (the gate is disabled). The mobile app reads this
	// at cold-start via GET /v1/mobile/{slug}/min-version and shows a hard
	// "update required" screen when its native build < min.
	MinBuildNumberIOS     int64  `bun:",notnull,default:0" json:"min_build_number_ios"`
	MinBuildNumberAndroid int64  `bun:",notnull,default:0" json:"min_build_number_android"`
	MinVersionMessage     string `bun:",nullzero" json:"min_version_message"`

	// OneSignal (P2.2 — push notifications). Per-tenant app id + REST API
	// key. Key stored encrypted via CredRef envelope; id is non-secret.
	// Replaces the v1 shadow Project's OneSignalAppID/OneSignalAppToken.
	OneSignalAppID          string       `bun:"one_signal_app_id,nullzero" json:"one_signal_app_id"`
	OneSignalRESTKeyCredRef *CredRefJSON `bun:"one_signal_rest_key_cred_ref,type:jsonb,nullzero" json:"one_signal_rest_key_cred_ref,omitempty"`

	CreatedAt time.Time `bun:",nullzero,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt time.Time `bun:",nullzero,notnull,default:current_timestamp" json:"updated_at"`
}
