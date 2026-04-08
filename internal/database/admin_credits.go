package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
	"github.com/uptrace/bun"
)

// AdminCreditListParams holds query parameters for listing credit-related data in the admin panel.
type AdminCreditListParams struct {
	Page     int
	PageSize int
	Search   string
	Status   string // reused for action filter on transactions
	SortDir  string
}

// Offset returns the pagination offset.
func (p AdminCreditListParams) Offset() int {
	return (p.Page - 1) * p.PageSize
}

// AdminListCreditTransactions returns a paginated list of credit transactions with user relations.
func (db *Database) AdminListCreditTransactions(params AdminCreditListParams) ([]models.AICreditTransaction, int, error) {
	var txns []models.AICreditTransaction

	q := db.DB.NewSelect().
		Model(&txns).
		Relation("User")

	// Filter by action (using status param)
	if params.Status != "" {
		q = q.Where("\"ai_credit_transaction\".\"action\" = ?", params.Status)
	}

	// Search by user email
	if params.Search != "" {
		search := "%" + params.Search + "%"
		q = q.WhereGroup(" AND ", func(sq *bun.SelectQuery) *bun.SelectQuery {
			return sq.Where("\"user\".\"email\" ILIKE ?", search)
		})
	}

	// Sorting
	q = q.OrderExpr("\"ai_credit_transaction\".\"created_at\" " + params.SortDir)

	// Pagination
	q = q.Limit(params.PageSize).Offset(params.Offset())

	total, err := q.ScanAndCount(context.Background())
	return txns, total, err
}

// AdminListCreditBalances returns a paginated list of credit balances sorted by balance ASC.
func (db *Database) AdminListCreditBalances(params AdminCreditListParams) ([]models.AICreditsBalance, int, error) {
	var balances []models.AICreditsBalance

	q := db.DB.NewSelect().
		Model(&balances).
		Relation("User")

	// Search by user email
	if params.Search != "" {
		search := "%" + params.Search + "%"
		q = q.WhereGroup(" AND ", func(sq *bun.SelectQuery) *bun.SelectQuery {
			return sq.Where("\"user\".\"email\" ILIKE ?", search)
		})
	}

	// Sort by balance ASC
	q = q.OrderExpr("\"ai_credits_balance\".\"balance\" ASC")

	// Pagination
	q = q.Limit(params.PageSize).Offset(params.Offset())

	total, err := q.ScanAndCount(context.Background())
	return balances, total, err
}

// AdminTokenUsageRow holds one row from the aggregated token usage query.
type AdminTokenUsageRow struct {
	Model           string `bun:"model"`
	Action          string `bun:"action"`
	TotalPrompt     int    `bun:"total_prompt"`
	TotalCompletion int    `bun:"total_completion"`
	TotalTokens     int    `bun:"total_tokens"`
	TotalCalls      int    `bun:"total_calls"`
}

// AdminGetTokenUsage returns aggregated token usage grouped by model and action.
func (db *Database) AdminGetTokenUsage() ([]AdminTokenUsageRow, error) {
	var rows []AdminTokenUsageRow

	err := db.DB.NewSelect().
		TableExpr("llm_token_usages").
		ColumnExpr("model, action").
		ColumnExpr("SUM(prompt_tokens) AS total_prompt").
		ColumnExpr("SUM(completion_tokens) AS total_completion").
		ColumnExpr("SUM(total_tokens) AS total_tokens").
		ColumnExpr("SUM(call_count) AS total_calls").
		GroupExpr("model, action").
		OrderExpr("total_tokens DESC").
		Scan(context.Background(), &rows)

	return rows, err
}
