package models

import (
	"time"

	"github.com/google/uuid"
)

type StandardUserLimits struct {
	ID                      uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	MaximumProjectCount     int       `bun:",notnull"`
	MaximumCredentialCount  int       `bun:",notnull"`
	MontlyNotificationCount int       `bun:",notnull"`
	MontlyBuildCount        int       `bun:",notnull"`
	CanBuildToAndroid       bool      `bun:",notnull"`
	CanBuildToIOS           bool      `bun:",notnull"`
	CanUseAnalytics         bool      `bun:",notnull"`
	CanRemoveBrandLogo      bool      `bun:",notnull"`
	// AI limits
	MonthlyAICredits       int  `bun:",notnull,default:0"`
	// App builder limits
	MaxWebApps         int  `bun:"monthly_app_count,notnull,default:0"`
	MaxMobileApps      int  `bun:",notnull,default:0"`
	CanUseEcommerce    bool `bun:",notnull,default:false"`
	CanDeployWeb       bool `bun:"can_deploy_web,notnull,default:false"`
	MaxDeployedWebApps    int    `bun:",notnull,default:0"`
	MaxDeployedMobileApps int    `bun:",notnull,default:0"`
	CanDeployMobile       bool   `bun:",notnull,default:false"`
	DeployMemoryMB        int    `bun:",notnull,default:256"`
	DeployCPUs           string `bun:"deploy_cpus,notnull,default:'0.25'"`
	DeployDBMaxCU        string `bun:"deploy_db_max_cu,notnull,default:'0.25'"`
	DeployDBStorageMB    int    `bun:"deploy_db_storage_mb,notnull,default:512"`
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
	CreditRolloverMonths int    `bun:",notnull,default:0"`
	CanAccessCode        bool `bun:",notnull,default:false"`
	CanBuyDomain         bool `bun:",notnull,default:false"`
	CanExportToGitHub    bool `bun:"can_export_to_github,notnull,default:false"`
	CanUseEmailService   bool `bun:",notnull,default:false"`
	CanBuildMobile       bool `bun:",notnull,default:false"`
	CreatedAt               time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt               time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt               time.Time `bun:",soft_delete,nullzero"`
}
