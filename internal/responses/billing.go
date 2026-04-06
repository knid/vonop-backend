package responses

type PlanLimitsResponse struct {
	MaxDeployedWebApps   int    `json:"max_deployed_web_apps"`
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
