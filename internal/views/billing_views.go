package views

import (
	"net/http"

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
