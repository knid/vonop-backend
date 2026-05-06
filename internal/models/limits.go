package models

import (
	"time"

	"github.com/google/uuid"
)

type StandardUserLimits struct {
	ID                      uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	MaximumCredentialCount  int       `bun:",notnull"`
	MontlyNotificationCount int       `bun:",notnull"`
	MontlyBuildCount        int       `bun:",notnull"`
	CanBuildToAndroid       bool      `bun:",notnull"`
	CanBuildToIOS           bool      `bun:",notnull"`
	CanUseAnalytics         bool      `bun:",notnull"`
	CanRemoveBrandLogo      bool      `bun:",notnull"`
	// AI limits
	MonthlyAICredits int `bun:",notnull,default:0"`
	// App builder limits
	MaxWebApps            int    `bun:"monthly_app_count,notnull,default:0"`
	MaxMobileApps         int    `bun:",notnull,default:0"`
	CanUseEcommerce       bool   `bun:",notnull,default:false"`
	CanDeployWeb          bool   `bun:"can_deploy_web,notnull,default:false"`
	MaxDeployedWebApps    int    `bun:",notnull,default:0"`
	MaxDeployedMobileApps int    `bun:",notnull,default:0"`
	CanDeployMobile       bool   `bun:",notnull,default:false"`
	DeployMemoryMB        int    `bun:",notnull,default:256"`
	DeployCPUs            string `bun:"deploy_cpus,notnull,default:'0.25'"`
	DeployDBMaxCU         string `bun:"deploy_db_max_cu,notnull,default:'0.25'"`
	DeployDBStorageMB     int    `bun:"deploy_db_storage_mb,notnull,default:512"`
	// Neon agent-plan quotas. ActiveTime + DataTransfer are the levers Neon
	// actually meters on the Free org; storage alone won't hold a free user
	// in the sponsored org. OrgTier picks which Neon org (Free vs Paid) the
	// user's project lives in — transferred via personal API key on plan
	// change. SuspendTimeout shortens cold-suspend on free tiers to cap
	// active_time consumption.
	DeployDBActiveTimeSec     int    `bun:"deploy_db_active_time_sec,notnull,default:360000"`
	DeployDBDataTransferBytes int64  `bun:"deploy_db_data_transfer_bytes,notnull,default:5368709120"`
	DeployDBComputeTimeSec    int    `bun:"deploy_db_compute_time_sec,notnull,default:0"`
	DeployDBSuspendTimeoutSec int    `bun:"deploy_db_suspend_timeout_sec,notnull,default:300"`
	DeployDBOrgTier           string `bun:"deploy_db_org_tier,notnull,default:'free'"`
	// Fly machine warmth. 0 = scale-to-zero (cheap, cold starts). >=1 keeps
	// at least N machines running so the first request after idle skips the
	// 100ms-2s wake. Each warm machine adds steady-state Fly compute cost.
	MinMachinesRunning int `bun:"min_machines_running,notnull,default:0"`
	// Maximum number of organization members allowed on this plan.
	// Enforcement (blocking the Nth invite) is handled by the org-agency
	// roadmap; this field exists so that work can read it.
	MaxSeats int `bun:"max_seats,notnull,default:1"`
	// Fly egress quota per billing period. Metered daily via Fly GraphQL.
	// 0 = unlimited (avoid; pre-A1 default).
	MonthlyBandwidthGB int `bun:"monthly_bandwidth_gb,notnull,default:0"`
	// Self-hosted Expo OTA publish quota per billing period. 0 = unlimited.
	MonthlyOTAPublishCount int `bun:"monthly_ota_publish_count,notnull,default:0"`
	// Per-app Fly region selection (Pro+ feature). When false, every app
	// pins to FLY_PRIMARY_REGION.
	CanChooseRegion      bool `bun:"can_choose_region,notnull,default:false"`
	CreditRolloverMonths int  `bun:",notnull,default:0"`
	CanAccessCode        bool `bun:",notnull,default:false"`
	CanBuyDomain         bool `bun:",notnull,default:false"`
	CanExportToGitHub    bool `bun:"can_export_to_github,notnull,default:false"`
	CanUseEmailService   bool `bun:",notnull,default:false"`
	CanBuildMobile       bool `bun:",notnull,default:false"`
	// Deprecated: removed from product surface in commit ab7c5f0 ("Remove
	// CanSeeActiveUsers from usage"). The DB column still exists with NOT
	// NULL and *no* DB-level default (it predates Bun's auto-default era).
	//
	// We deliberately omit the `default:false` tag so Bun emits the literal
	// FALSE value into the INSERT instead of the SQL keyword DEFAULT.
	// DEFAULT only works when the column has a DB-level default — and on
	// this legacy column it doesn't, so emitting DEFAULT becomes NULL and
	// trips the NOT NULL constraint. Verified against prod via
	// information_schema.columns.column_default = NULL.
	//
	// A separate cleanup spec can DROP COLUMN once we're sure no admin
	// tooling reads it.
	CanSeeActiveUsers bool `bun:",notnull"`
	CreatedAt         time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt            time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt            time.Time `bun:",soft_delete,nullzero"`
}
