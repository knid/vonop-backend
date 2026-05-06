package responses

import "github.com/google/uuid"

type LimitsResponse struct {
	ID                      uuid.UUID `json:"id"`
	MaximumAccessIdCount    int       `json:"maximum_access_id_count"`
	MontlyNotificationCount int       `json:"montly_notification_count"`
	MontlyBuildCount        int       `json:"montly_build_count"`
	CanBuildToAndroid       bool      `json:"can_build_to_android"`
	CanBuildToIOS           bool      `json:"can_build_to_ios"`
	CanUseAnalytics         bool      `json:"can_use_analytics"`
	CanRemoveBrandLogo      bool      `json:"can_remove_brand_logo"`
	MaxDeployedWebApps      int       `json:"max_deployed_web_apps"`
	MaxDeployedMobileApps   int       `json:"max_deployed_mobile_apps"`
	CanDeployMobile         bool      `json:"can_deploy_mobile"`
	CreditRolloverMonths    int       `json:"credit_rollover_months"`
	CanAccessCode           bool      `json:"can_access_code"`
	CanBuyDomain            bool      `json:"can_buy_domain"`
	CanUseEmailService      bool      `json:"can_use_email_service"`
	CanBuildMobile          bool      `json:"can_build_mobile"`
	MonthlyAICredits        int       `json:"monthly_ai_credits"`
	MaxWebApps              int       `json:"max_web_apps"`
	MaxMobileApps           int       `json:"max_mobile_apps"`
	CanUseEcommerce         bool      `json:"can_use_ecommerce"`
	CanDeployWeb            bool      `json:"can_deploy_web"`
	CanExportToGitHub       bool      `json:"can_export_to_github"`
	DeployMemoryMB          int       `json:"deploy_memory_mb"`
	DeployCPUs              string    `json:"deploy_cpus"`
	DeployDBMaxCU           string    `json:"deploy_db_max_cu"`
	DeployDBStorageMB       int       `json:"deploy_db_storage_mb"`
	DeployDBActiveTimeSec     int    `json:"deploy_db_active_time_sec"`
	DeployDBDataTransferBytes int64  `json:"deploy_db_data_transfer_bytes"`
	DeployDBComputeTimeSec    int    `json:"deploy_db_compute_time_sec"`
	DeployDBSuspendTimeoutSec int    `json:"deploy_db_suspend_timeout_sec"`
	DeployDBOrgTier           string `json:"deploy_db_org_tier"`
}

// Note: LimitsResponse used to omit Deploy* fields entirely — the
// /v1/billing/subscription handler in controllers/subscriptions.go now
// populates them from the user's resolved limits row.
