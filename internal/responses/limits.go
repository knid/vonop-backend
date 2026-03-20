package responses

import "github.com/google/uuid"

type LimitsResponse struct {
	ID                      uuid.UUID `json:"id"`
	MaximumProjectCount     int       `json:"maximum_project_count"`
	MaximumAccessIdCount    int       `json:"maximum_access_id_count"`
	MontlyNotificationCount int       `json:"montly_notification_count"`
	MontlyBuildCount        int       `json:"montly_build_count"`
	CanBuildToAndroid       bool      `json:"can_build_to_android"`
	CanBuildToIOS           bool      `json:"can_build_to_ios"`
	CanUseAnalytics         bool      `json:"can_use_analytics"`
	CanRemoveBrandLogo      bool      `json:"can_remove_brand_logo"`
	CanSeeActiveUsers       bool      `json:"can_see_active_users"`
	MaxDeployedWebApps      int       `json:"max_deployed_web_apps"`
	CreditRolloverMonths    int       `json:"credit_rollover_months"`
	CanAccessCode           bool      `json:"can_access_code"`
	CanBuyDomain            bool      `json:"can_buy_domain"`
	CanUseEmailService      bool      `json:"can_use_email_service"`
	CanBuildMobile          bool      `json:"can_build_mobile"`
	MonthlyAICredits        int       `json:"monthly_ai_credits"`
	MonthlyAppCount         int       `json:"monthly_app_count"`
	MonthlyDeployCount      int       `json:"monthly_deploy_count"`
	CanUseEcommerce         bool      `json:"can_use_ecommerce"`
	CanExportToGitHub       bool      `json:"can_export_to_github"`
	DeployMemoryMB          int       `json:"deploy_memory_mb"`
	DeployCPUs              string    `json:"deploy_cpus"`
	DeployDBMaxCU           string    `json:"deploy_db_max_cu"`
	DeployDBStorageMB       int       `json:"deploy_db_storage_mb"`
}
