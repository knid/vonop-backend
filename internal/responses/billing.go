package responses

import "github.com/knid/wemob/internal/constants"

// PlansListResponse is the wrapper returned by GET /v1/billing/plans. It
// carries the plan list plus platform-wide context the frontend needs to
// render the comparison table (available Fly regions, the default region a
// user lands on if their plan can't pick).
type PlansListResponse struct {
	Plans            []PlanResponse        `json:"plans"`
	AvailableRegions []constants.FlyRegion `json:"available_regions"`
	DefaultRegion    string                `json:"default_region"`
}

type PlanLimitsResponse struct {
	// Credits
	MonthlyAICredits     int            `json:"monthly_ai_credits"`
	CreditRolloverMonths int            `json:"credit_rollover_months"`
	BuildCreditCosts     map[string]int `json:"build_credit_costs"`
	// Org / plan caps
	MaxSeats               int `json:"max_seats"`
	MaximumCredentialCount int `json:"maximum_credential_count"`
	// App project caps
	MaxWebApps            int `json:"max_web_apps"`
	MaxMobileApps         int `json:"max_mobile_apps"`
	MaxDeployedWebApps    int `json:"max_deployed_web_apps"`
	MaxDeployedMobileApps int `json:"max_deployed_mobile_apps"`
	// Build / notification quotas (legacy — credits gate now, but values still reported)
	MontlyBuildCount        int `json:"montly_build_count"`
	MontlyNotificationCount int `json:"montly_notification_count"`
	MonthlyOTAPublishCount  int `json:"monthly_ota_publish_count"`
	// Capability flags
	CanBuildToAndroid  bool `json:"can_build_to_android"`
	CanBuildToIOS      bool `json:"can_build_to_ios"`
	CanBuildMobile     bool `json:"can_build_mobile"`
	CanDeployMobile    bool `json:"can_deploy_mobile"`
	CanDeployWeb       bool `json:"can_deploy_web"`
	CanUseAnalytics    bool `json:"can_use_analytics"`
	CanAccessCode      bool `json:"can_access_code"`
	CanBuyDomain       bool `json:"can_buy_domain"`
	CanUseEmailService bool `json:"can_use_email_service"`
	CanUseEcommerce    bool `json:"can_use_ecommerce"`
	CanExportToGitHub  bool `json:"can_export_to_github"`
	CanRemoveBrandLogo bool `json:"can_remove_brand_logo"`
	CanChooseRegion    bool `json:"can_choose_region"`
	// Fly machine spec
	DeployMemoryMB     int    `json:"deploy_memory_mb"`
	DeployCPUs         string `json:"deploy_cpus"`
	MinMachinesRunning int    `json:"min_machines_running"`
	MonthlyBandwidthGB int    `json:"monthly_bandwidth_gb"`
	// Neon database quotas
	DeployDBMaxCU             string `json:"deploy_db_max_cu"`
	DeployDBStorageMB         int    `json:"deploy_db_storage_mb"`
	DeployDBActiveTimeSec     int    `json:"deploy_db_active_time_sec"`
	DeployDBDataTransferBytes int64  `json:"deploy_db_data_transfer_bytes"`
	DeployDBComputeTimeSec    int    `json:"deploy_db_compute_time_sec"`
	DeployDBSuspendTimeoutSec int    `json:"deploy_db_suspend_timeout_sec"`
	DeployDBOrgTier           string `json:"deploy_db_org_tier"`
}

type PlanResponse struct {
	ID            string             `json:"id"`
	Name          string             `json:"name"`
	Description   string             `json:"description"`
	MonthlyPrice  int                `json:"monthly_price"`
	AnnualPrice   int                `json:"annual_price"`
	PriceID       string             `json:"price_id"`
	AnnualPriceID string             `json:"annual_price_id,omitempty"`
	AnnualSavings    int                `json:"annual_savings"`
	IntroPrice       int                `json:"intro_price,omitempty"`
	IntroPriceMonths int                `json:"intro_price_months,omitempty"`
	IntroEligible    bool               `json:"intro_eligible,omitempty"`
	Limits           PlanLimitsResponse `json:"limits"`
}

type AICreditsBalanceResponse struct {
	Balance     int    `json:"balance"`
	PeriodStart string `json:"period_start"`
	PeriodEnd   string `json:"period_end"`
}

// CreditBreakdownResponse is the derived sub-vs-purchased split served by
// GET /v1/billing/credits/breakdown. The backend stores a single undifferentiated
// balance column; this response is the logical projection the UI wants so it
// can render "X of Y sub credits used" next to "N extra credits remaining"
// instead of the broken total-minus-balance formula.
type CreditBreakdownResponse struct {
	Balance             int    `json:"balance"`
	SubBalance          int    `json:"sub_balance"`
	SubLimit            int    `json:"sub_limit"`
	SubUsedThisPeriod   int    `json:"sub_used_this_period"`
	PurchasedBalance    int    `json:"purchased_balance"`
	PeriodStart         string `json:"period_start"`
	PeriodEnd           string `json:"period_end"`
}

type TokenUsageEntry struct {
	Action           string `json:"action"`
	Model            string `json:"model"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	TotalTokens      int    `json:"total_tokens"`
	CallCount        int    `json:"call_count"`
	Reference        string `json:"reference,omitempty"`
	CreatedAt        string `json:"created_at"`
}

type TokenUsageSummary struct {
	TotalPromptTokens     int `json:"total_prompt_tokens"`
	TotalCompletionTokens int `json:"total_completion_tokens"`
	TotalTokens           int `json:"total_tokens"`
	TotalCallCount        int `json:"total_call_count,omitempty"`
}

type TokenUsageResponse struct {
	Entries []TokenUsageEntry `json:"entries"`
	Summary TokenUsageSummary `json:"summary"`
}

type CreditTransactionResponse struct {
	ID           string `json:"id"`
	Type         string `json:"type"`
	Action       string `json:"action"`
	Amount       int    `json:"amount"`
	BalanceAfter int    `json:"balance_after"`
	CreatedAt    string `json:"created_at"`
	Description  string `json:"description,omitempty"`
	// Reference and ProjectID added in 2026-05 cost attribution work.
	// Agencies can now see which project/app a cost belongs to.
	Reference string `json:"reference,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
}

type CreditHistoryResponse struct {
	Transactions []CreditTransactionResponse `json:"transactions"`
	Total        int                         `json:"total"`
	Page         int                         `json:"page"`
	PageSize     int                         `json:"page_size"`
}
