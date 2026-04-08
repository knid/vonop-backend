package views

import (
	"net/http"

	"github.com/knid/wemob/internal/utils"
)

// AdminListCreditTransactions handles GET /v1/admin/credits/transactions
func (v *HttpViews) AdminListCreditTransactions(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if user.Type != "admin" {
		utils.JSONErrorResponse(w, "admin access required", http.StatusForbidden)
		return
	}

	params := adminListParamsFromQuery(r)
	result, err := v.Controller.AdminListCreditTransactions(params)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}

// AdminListCreditBalances handles GET /v1/admin/credits/balances
func (v *HttpViews) AdminListCreditBalances(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if user.Type != "admin" {
		utils.JSONErrorResponse(w, "admin access required", http.StatusForbidden)
		return
	}

	params := adminListParamsFromQuery(r)
	result, err := v.Controller.AdminListCreditBalances(params)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}

// AdminGetTokenUsage handles GET /v1/admin/credits/token-usage
func (v *HttpViews) AdminGetTokenUsage(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if user.Type != "admin" {
		utils.JSONErrorResponse(w, "admin access required", http.StatusForbidden)
		return
	}

	result, err := v.Controller.AdminGetTokenUsage()
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}
