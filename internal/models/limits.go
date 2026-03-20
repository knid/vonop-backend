package models

import (
	"time"

	"github.com/google/uuid"
)

type StandartUserLimits struct {
	ID                      uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	MaximumProjectCount     int       `bun:",notnull"`
	MaximumCredentialCount  int       `bun:",notnull"`
	MontlyNotificationCount int       `bun:",notnull"`
	MontlyBuildCount        int       `bun:",notnull"`
	CanBuildToAndroid       bool      `bun:",notnull"`
	CanBuildToIOS           bool      `bun:",notnull"`
	CanUseAnalytics         bool      `bun:",notnull"`
	CanRemoveBrandLogo      bool      `bun:",notnull"`
	CanSeeActiveUsers       bool      `bun:",notnull"`
	// AI limits
	MonthlyAICredits       int  `bun:",notnull,default:0"`
	// App builder limits
	MonthlyAppCount    int  `bun:",notnull,default:0"`
	MonthlyDeployCount int  `bun:",notnull,default:0"`
	CanUseEcommerce    bool `bun:",notnull,default:false"`
	CanDeployWeb       bool `bun:"can_deploy_to_vercel,notnull,default:false"`
	MaxDeployedWebApps   int    `bun:",notnull,default:0"`
	DeployMemoryMB       int    `bun:",notnull,default:256"`
	DeployCPUs           string `bun:"deploy_cpus,notnull,default:'0.25'"`
	DeployDBMaxCU        string `bun:"deploy_db_max_cu,notnull,default:'0.25'"`
	DeployDBStorageMB    int    `bun:"deploy_db_storage_mb,notnull,default:512"`
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
