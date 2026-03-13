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

	CreatedAt time.Time `bun:",nullzero,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt time.Time `bun:",nullzero,notnull,default:current_timestamp" json:"updated_at"`
}
