package views

import (
	"net/http"
	"time"

	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) GetBillingPlans(w http.ResponseWriter, r *http.Request) {
	subs, err := v.Controller.DB.GetSubscriptions()
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var plans []responses.PlanResponse
	for _, sub := range *subs {
		annualSavings := 0
		if sub.AnnualPrice > 0 && sub.MonthlyPrice > 0 {
			annualSavings = (sub.MonthlyPrice * 12) - sub.AnnualPrice
		}

		plans = append(plans, responses.PlanResponse{
			ID:            sub.ID.String(),
			Name:          sub.Name,
			Description:   sub.Description,
			MonthlyPrice:  sub.MonthlyPrice,
			AnnualPrice:   sub.AnnualPrice,
			PriceID:       sub.PriceID,
			AnnualPriceID: sub.AnnualPriceID,
			AnnualSavings: annualSavings,
		})
	}

	utils.JSONResponse(w, plans, http.StatusOK)
}

func (v *HttpViews) GetAICreditsBalance(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	balance, err := v.Controller.GetOrCreateCreditsBalance(user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, responses.AICreditsBalanceResponse{
		Balance:     balance.Balance,
		PeriodStart: balance.PeriodStart.Format("2006-01-02T15:04:05Z"),
		PeriodEnd:   balance.PeriodEnd.Format("2006-01-02T15:04:05Z"),
	}, http.StatusOK)
}

func (v *HttpViews) GetAICreditTransactions(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	txs, err := v.Controller.DB.GetAICreditTransactions(user.ID.String(), 50)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, txs, http.StatusOK)
}

func (v *HttpViews) GetTokenUsage(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	entries, err := v.Controller.DB.GetLLMTokenUsageByUser(user.ID.String(), 100)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	since := time.Now().AddDate(0, -1, 0)
	promptTotal, completionTotal, totalTokens, err := v.Controller.DB.GetLLMTokenUsageSummary(user.ID.String(), since)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var responseEntries []responses.TokenUsageEntry
	for _, e := range entries {
		responseEntries = append(responseEntries, responses.TokenUsageEntry{
			Action:           e.Action,
			Model:            e.Model,
			PromptTokens:     e.PromptTokens,
			CompletionTokens: e.CompletionTokens,
			TotalTokens:      e.TotalTokens,
			CallCount:        e.CallCount,
			Reference:        e.Reference,
			CreatedAt:        e.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}

	utils.JSONResponse(w, responses.TokenUsageResponse{
		Entries: responseEntries,
		Summary: responses.TokenUsageSummary{
			TotalPromptTokens:     promptTotal,
			TotalCompletionTokens: completionTotal,
			TotalTokens:           totalTokens,
		},
	}, http.StatusOK)
}
