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
	BridgeEnabled bool     `bun:",notnull,default:false" json:"bridge_enabled"`
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

	CreatedAt time.Time `bun:",nullzero,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt time.Time `bun:",nullzero,notnull,default:current_timestamp" json:"updated_at"`
}
