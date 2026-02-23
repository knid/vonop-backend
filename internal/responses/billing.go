package responses

type PlanResponse struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	MonthlyPrice  int    `json:"monthly_price"`
	AnnualPrice   int    `json:"annual_price"`
	PriceID       string `json:"price_id"`
	AnnualPriceID string `json:"annual_price_id,omitempty"`
	AnnualSavings int    `json:"annual_savings"`
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
