package controllers

import (
	"fmt"

	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
)

// AdminListCreditTransactions returns a paginated list of credit transactions.
func (c *Controller) AdminListCreditTransactions(params requests.AdminListParams) (*responses.PaginatedResponse, error) {
	params.Defaults()

	dbParams := database.AdminCreditListParams{
		Page:     params.Page,
		PageSize: params.PageSize,
		Search:   params.Search,
		Status:   params.Status,
		SortDir:  params.SortDir,
	}

	txns, total, err := c.DB.AdminListCreditTransactions(dbParams)
	if err != nil {
		return nil, fmt.Errorf("failed to list credit transactions: %w", err)
	}

	items := make([]responses.AdminCreditTransactionResponse, len(txns))
	for i, t := range txns {
		userEmail := ""
		if t.User != nil {
			userEmail = t.User.Email
		}
		items[i] = responses.AdminCreditTransactionResponse{
			ID:        t.ID,
			UserID:    t.UserID,
			UserEmail: userEmail,
			Amount:    t.Amount,
			Action:    t.Action,
			Reference: t.Reference,
			CreatedAt: t.CreatedAt,
		}
	}

	totalPages := total / params.PageSize
	if total%params.PageSize != 0 {
		totalPages++
	}

	return &responses.PaginatedResponse{
		Data:       items,
		Total:      total,
		Page:       params.Page,
		PageSize:   params.PageSize,
		TotalPages: totalPages,
	}, nil
}

// AdminListCreditBalances returns a paginated list of credit balances sorted by balance ASC.
func (c *Controller) AdminListCreditBalances(params requests.AdminListParams) (*responses.PaginatedResponse, error) {
	params.Defaults()

	dbParams := database.AdminCreditListParams{
		Page:     params.Page,
		PageSize: params.PageSize,
		Search:   params.Search,
		SortDir:  params.SortDir,
	}

	balances, total, err := c.DB.AdminListCreditBalances(dbParams)
	if err != nil {
		return nil, fmt.Errorf("failed to list credit balances: %w", err)
	}

	items := make([]responses.AdminCreditBalanceResponse, len(balances))
	for i, b := range balances {
		userEmail := ""
		if b.User != nil {
			userEmail = b.User.Email
		}
		items[i] = responses.AdminCreditBalanceResponse{
			UserID:    b.UserID,
			UserEmail: userEmail,
			Balance:   b.Balance,
		}
	}

	totalPages := total / params.PageSize
	if total%params.PageSize != 0 {
		totalPages++
	}

	return &responses.PaginatedResponse{
		Data:       items,
		Total:      total,
		Page:       params.Page,
		PageSize:   params.PageSize,
		TotalPages: totalPages,
	}, nil
}

// AdminGetTokenUsage returns aggregated token usage grouped by model and action.
func (c *Controller) AdminGetTokenUsage() ([]responses.AdminTokenUsageResponse, error) {
	rows, err := c.DB.AdminGetTokenUsage()
	if err != nil {
		return nil, fmt.Errorf("failed to get token usage: %w", err)
	}

	items := make([]responses.AdminTokenUsageResponse, len(rows))
	for i, r := range rows {
		items[i] = responses.AdminTokenUsageResponse{
			Model:           r.Model,
			Action:          r.Action,
			TotalPrompt:     r.TotalPrompt,
			TotalCompletion: r.TotalCompletion,
			TotalTokens:     r.TotalTokens,
			TotalCalls:      r.TotalCalls,
		}
	}

	return items, nil
}
