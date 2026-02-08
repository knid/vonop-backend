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
