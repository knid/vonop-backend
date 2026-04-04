package views

import (
	"net/http"
	"strconv"
	"strings"
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
			savings := (sub.MonthlyPrice * 12) - sub.AnnualPrice
			if savings > 0 {
				annualSavings = savings
			}
		}

		var planLimits responses.PlanLimitsResponse
		limits, limitsErr := v.Controller.DB.GetStandartUserLimitsById(sub.LimitsID.String())
		if limitsErr == nil {
			planLimits = responses.PlanLimitsResponse{
				MaxDeployedWebApps:   limits.MaxDeployedWebApps,
				MonthlyAICredits:     limits.MonthlyAICredits,
				CreditRolloverMonths: limits.CreditRolloverMonths,
				CanBuildMobile:       limits.CanBuildMobile,
				CanUseAnalytics:      limits.CanUseAnalytics,
				CanAccessCode:        limits.CanAccessCode,
				CanBuyDomain:         limits.CanBuyDomain,
				CanUseEmailService:   limits.CanUseEmailService,
				CanUseEcommerce:      limits.CanUseEcommerce,
				CanDeployWeb:         limits.CanDeployWeb,
				MaximumProjectCount:  limits.MaximumProjectCount,
				MonthlyAppCount:      limits.MonthlyAppCount,
				MonthlyDeployCount:   limits.MonthlyDeployCount,
				CanRemoveBrandLogo:   limits.CanRemoveBrandLogo,
				CanExportToGitHub:    limits.CanExportToGitHub,
				DeployMemoryMB:       limits.DeployMemoryMB,
				DeployCPUs:           limits.DeployCPUs,
				DeployDBMaxCU:        limits.DeployDBMaxCU,
				DeployDBStorageMB:    limits.DeployDBStorageMB,
			}
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
			Limits:        planLimits,
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

func (v *HttpViews) GetCreditHistory(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	page := 1
	pageSize := 20
	if p := r.URL.Query().Get("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v > 0 {
			page = v
		}
	}
	if ps := r.URL.Query().Get("page_size"); ps != "" {
		if v, err := strconv.Atoi(ps); err == nil && v > 0 && v <= 100 {
			pageSize = v
		}
	}

	txs, total, err := v.Controller.DB.GetAICreditTransactionsPaginated(user.ID.String(), page, pageSize)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Compute running balance_after for each transaction (newest first).
	// balance_after[i] = current_balance - sum(amounts of transactions newer than i)
	currentBalance := 0
	if bal, balErr := v.Controller.DB.GetAICreditsBalance(user.ID.String()); balErr == nil {
		currentBalance = bal.Balance
	}
	// For pages after 1, subtract the sum of newer transactions not on this page
	offset := (page - 1) * pageSize
	if offset > 0 {
		if newerSum, sumErr := v.Controller.DB.SumRecentTransactionAmounts(user.ID.String(), offset); sumErr == nil {
			currentBalance -= newerSum
		}
	}

	transactions := make([]responses.CreditTransactionResponse, 0, len(txs))
	runningBalance := currentBalance
	for _, tx := range txs {
		transactions = append(transactions, responses.CreditTransactionResponse{
			ID:           tx.ID.String(),
			Type:         categorizeTxType(tx.Action, tx.Amount),
			Action:       tx.Action,
			Amount:       tx.Amount,
			BalanceAfter: runningBalance,
			CreatedAt:    tx.CreatedAt.Format("2006-01-02T15:04:05Z"),
			Description:  describeTxAction(tx.Action),
		})
		runningBalance -= tx.Amount
	}

	utils.JSONResponse(w, responses.CreditHistoryResponse{
		Transactions: transactions,
		Total:        total,
		Page:         page,
		PageSize:     pageSize,
	}, http.StatusOK)
}

func categorizeTxType(action string, amount int) string {
	lower := strings.ToLower(action)
	if strings.Contains(lower, "refund") {
		return "refund"
	}
	if strings.Contains(lower, "grant") || strings.Contains(lower, "allocation") || strings.Contains(lower, "bonus") {
		return "allocation"
	}
	if strings.Contains(lower, "purchase") || strings.Contains(lower, "purchased") {
		return "purchase"
	}
	if amount < 0 {
		return "usage"
	}
	if amount > 0 {
		return "allocation"
	}
	return "usage"
}

func describeTxAction(action string) string {
	descriptions := map[string]string{
		"generate_app":        "App generation",
		"generate":            "App generation",
		"app_chat":            "Chat edit",
		"chat":                "Chat edit",
		"screenshot_to_code":  "Screenshot to code",
		"element_edit":        "Element edit",
		"generate_image":     "Image generation",
		"purchase":            "Credit purchase",
		"credits_purchased":   "Credit purchase",
		"monthly_grant":       "Monthly credit allocation",
		"initial_grant":       "Initial credit allocation",
		"refund":              "Credit refund",
	}
	if desc, ok := descriptions[action]; ok {
		return desc
	}
	words := strings.Split(action, "_")
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
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
