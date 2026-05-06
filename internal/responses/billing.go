package responses

type PlanLimitsResponse struct {
	MaxDeployedWebApps    int    `json:"max_deployed_web_apps"`
	MaxDeployedMobileApps int    `json:"max_deployed_mobile_apps"`
	CanDeployMobile       bool   `json:"can_deploy_mobile"`
	MonthlyAICredits     int    `json:"monthly_ai_credits"`
	CreditRolloverMonths int    `json:"credit_rollover_months"`
	MontlyBuildCount     int    `json:"montly_build_count"`
	CanBuildMobile       bool   `json:"can_build_mobile"`
	CanUseAnalytics      bool   `json:"can_use_analytics"`
	CanAccessCode        bool   `json:"can_access_code"`
	CanBuyDomain         bool   `json:"can_buy_domain"`
	CanUseEmailService   bool   `json:"can_use_email_service"`
	CanUseEcommerce      bool   `json:"can_use_ecommerce"`
	CanDeployWeb         bool   `json:"can_deploy_web"`
	MaxWebApps           int    `json:"max_web_apps"`
	MaxMobileApps        int    `json:"max_mobile_apps"`
	CanRemoveBrandLogo   bool   `json:"can_remove_brand_logo"`
	CanExportToGitHub    bool   `json:"can_export_to_github"`
	DeployMemoryMB       int    `json:"deploy_memory_mb"`
	DeployCPUs           string `json:"deploy_cpus"`
	DeployDBMaxCU        string `json:"deploy_db_max_cu"`
	DeployDBStorageMB    int    `json:"deploy_db_storage_mb"`
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
	AnnualSavings int                `json:"annual_savings"`
	Limits        PlanLimitsResponse `json:"limits"`
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
	ID          string `json:"id"`
	Type        string `json:"type"`
	Action      string `json:"action"`
	Amount      int    `json:"amount"`
	BalanceAfter int   `json:"balance_after"`
	CreatedAt   string `json:"created_at"`
	Description string `json:"description,omitempty"`
}

type CreditHistoryResponse struct {
	Transactions []CreditTransactionResponse `json:"transactions"`
	Total        int                         `json:"total"`
	Page         int                         `json:"page"`
	PageSize     int                         `json:"page_size"`
}
