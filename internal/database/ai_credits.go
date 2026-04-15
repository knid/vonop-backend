package database

import (
	"context"
	"database/sql"
	"time"

	"github.com/knid/wemob/internal/models"
	"github.com/uptrace/bun"
)

func (db *Database) GetAICreditsBalance(userID string) (*models.AICreditsBalance, error) {
	var balance models.AICreditsBalance
	err := db.DB.NewSelect().
		Model(&balance).
		Where("user_id = ?", userID).
		Scan(context.Background())
	return &balance, err
}

func (db *Database) CreateAICreditsBalance(balance *models.AICreditsBalance) (*models.AICreditsBalance, error) {
	_, err := db.DB.NewInsert().Model(balance).On("CONFLICT (user_id) DO NOTHING").Exec(context.Background())
	if err != nil {
		return nil, err
	}
	// Re-read to get the actual row (whether we inserted or it already existed)
	return db.GetAICreditsBalance(balance.UserID.String())
}

func (db *Database) UpdateAICreditsBalance(balance *models.AICreditsBalance) (*models.AICreditsBalance, error) {
	balance.UpdatedAt = time.Now()
	_, err := db.DB.NewUpdate().
		Model(balance).
		Where("id = ?", balance.ID).
		Exec(context.Background())
	return balance, err
}

func (db *Database) CreateAICreditTransaction(tx *models.AICreditTransaction) (*models.AICreditTransaction, error) {
	_, err := db.DB.NewInsert().Model(tx).Exec(context.Background())
	return tx, err
}

func (db *Database) GetAICreditTransactions(userID string, limit int) ([]models.AICreditTransaction, error) {
	var txs []models.AICreditTransaction
	err := db.DB.NewSelect().
		Model(&txs).
		Where("user_id = ?", userID).
		OrderExpr("created_at DESC").
		Limit(limit).
		Scan(context.Background())
	return txs, err
}

func (db *Database) GetAICreditTransactionsPaginated(userID string, page, pageSize int) ([]models.AICreditTransaction, int, error) {
	var txs []models.AICreditTransaction
	offset := (page - 1) * pageSize

	total, err := db.DB.NewSelect().
		Model(&txs).
		Where("user_id = ?", userID).
		OrderExpr("created_at DESC").
		Limit(pageSize).
		Offset(offset).
		ScanAndCount(context.Background())
	return txs, total, err
}

// SumRecentTransactionAmounts returns the sum of amounts for the N most recent
// transactions (by created_at DESC). Used to compute running balance_after in paginated history.
func (db *Database) SumRecentTransactionAmounts(userID string, limit int) (int, error) {
	var sum int
	err := db.DB.NewRaw(
		"SELECT COALESCE(SUM(amount), 0) FROM (SELECT amount FROM ai_credit_transactions WHERE user_id = ? ORDER BY created_at DESC LIMIT ?) sub",
		userID, limit,
	).Scan(context.Background(), &sum)
	return sum, err
}

// --- Atomic Credit Operations ---

// AtomicDeductCreditsUnchecked atomically deducts credits, allowing negative balance down to floor.
// Returns sql.ErrNoRows if deduction would push balance below floor.
func (db *Database) AtomicDeductCreditsUnchecked(tx bun.IDB, balanceID string, cost int, floor int) (int, error) {
	var newBalance int
	err := tx.NewRaw(
		"UPDATE ai_credits_balances SET balance = balance - ?, updated_at = NOW() WHERE id = ? AND balance - ? >= ? RETURNING balance",
		cost, balanceID, cost, floor,
	).Scan(context.Background(), &newBalance)
	return newBalance, err
}

// AtomicDeductCreditsToFloor atomically deducts credits, clamping at the floor
// instead of failing. Returns (newBalance, actualDeducted, nil).
// If balance is already at or below floor, returns (0, 0, nil).
func (db *Database) AtomicDeductCreditsToFloor(tx bun.IDB, balanceID string, cost int, floor int) (newBalance int, deducted int, err error) {
	var result struct {
		NewBalance int `bun:"new_balance"`
		Deducted   int `bun:"deducted"`
	}
	err = tx.NewRaw(
		`WITH old AS (
			SELECT id, balance FROM ai_credits_balances WHERE id = ? FOR UPDATE
		)
		UPDATE ai_credits_balances b
		SET balance = GREATEST(?, old.balance - ?), updated_at = NOW()
		FROM old
		WHERE b.id = old.id AND old.balance > ?
		RETURNING b.balance AS new_balance, (old.balance - b.balance) AS deducted`,
		balanceID, floor, cost, floor,
	).Scan(context.Background(), &result)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, 0, nil
		}
		return 0, 0, err
	}
	return result.NewBalance, result.Deducted, nil
}

// AtomicAddCredits atomically adds credits. Returns the new balance.
func (db *Database) AtomicAddCredits(tx bun.IDB, balanceID string, amount int) (int, error) {
	var newBalance int
	err := tx.NewRaw(
		"UPDATE ai_credits_balances SET balance = balance + ?, updated_at = NOW() WHERE id = ? RETURNING balance",
		amount, balanceID,
	).Scan(context.Background(), &newBalance)
	return newBalance, err
}

// AtomicResetPeriod atomically resets the billing period if it has expired.
// Returns (newBalance, true, nil) if the reset happened, or (0, false, nil) if
// the period was already current (another request won the race).
func (db *Database) AtomicResetPeriod(balanceID string, newBalance int, periodStart, periodEnd time.Time) (int, bool, error) {
	var bal int
	err := db.DB.NewRaw(
		"UPDATE ai_credits_balances SET balance = ?, period_start = ?, period_end = ?, updated_at = NOW() WHERE id = ? AND period_end < NOW() RETURNING balance",
		newBalance, periodStart, periodEnd, balanceID,
	).Scan(context.Background(), &bal)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, false, nil // already reset by another request
		}
		return 0, false, err
	}
	return bal, true, nil
}

// CreateAICreditTransactionTx inserts a credit transaction within an existing DB transaction.
func (db *Database) CreateAICreditTransactionTx(tx bun.IDB, t *models.AICreditTransaction) (*models.AICreditTransaction, error) {
	_, err := tx.NewInsert().Model(t).Exec(context.Background())
	return t, err
}

// RunInTx executes fn inside a database transaction. If fn returns an error, the transaction is rolled back.
func (db *Database) RunInTx(fn func(tx bun.Tx) error) error {
	return db.DB.RunInTx(context.Background(), &sql.TxOptions{}, func(ctx context.Context, tx bun.Tx) error {
		return fn(tx)
	})
}

// HasUsageTransactions returns true if the user has any credit transactions
// other than system grants (initial_grant, monthly_grant, subscription_grant).
// Used to determine if the user has ever actively used their credits.
func (db *Database) HasUsageTransactions(userID string) (bool, error) {
	var count int
	err := db.DB.NewRaw(
		"SELECT COUNT(*) FROM ai_credit_transactions WHERE user_id = ? AND action NOT IN ('initial_grant', 'monthly_grant', 'subscription_grant')",
		userID,
	).Scan(context.Background(), &count)
	return count > 0, err
}

// HasInitialGrant returns true if the user has already received their
// one-time initial credit grant.
func (db *Database) HasInitialGrant(userID string) (bool, error) {
	var count int
	err := db.DB.NewRaw(
		"SELECT COUNT(*) FROM ai_credit_transactions WHERE user_id = ? AND action = 'initial_grant'",
		userID,
	).Scan(context.Background(), &count)
	return count > 0, err
}

// --- LLM Token Usage ---

func (db *Database) CreateLLMTokenUsage(usage *models.LLMTokenUsage) error {
	_, err := db.DB.NewInsert().Model(usage).Exec(context.Background())
	return err
}

func (db *Database) GetLLMTokenUsageByUser(userID string, limit int) ([]models.LLMTokenUsage, error) {
	var entries []models.LLMTokenUsage
	err := db.DB.NewSelect().
		Model(&entries).
		Where("user_id = ?", userID).
		OrderExpr("created_at DESC").
		Limit(limit).
		Scan(context.Background())
	return entries, err
}

func (db *Database) GetLLMTokenUsageSummary(userID string, since time.Time) (prompt, completion, total int, err error) {
	var result struct {
		Prompt     int `bun:"prompt"`
		Completion int `bun:"completion"`
		Total      int `bun:"total"`
	}
	err = db.DB.NewSelect().
		TableExpr("llm_token_usages").
		ColumnExpr("COALESCE(SUM(prompt_tokens), 0) AS prompt").
		ColumnExpr("COALESCE(SUM(completion_tokens), 0) AS completion").
		ColumnExpr("COALESCE(SUM(total_tokens), 0) AS total").
		Where("user_id = ?", userID).
		Where("created_at >= ?", since).
		Scan(context.Background(), &result)
	return result.Prompt, result.Completion, result.Total, err
}

// GetLLMTokenUsageByProject returns the most recent LLM token usage
// entries for a given project (scoped by the Reference column which
// the gateway and dashboard controllers populate with the project ID).
func (db *Database) GetLLMTokenUsageByProject(projectID string, limit int) ([]models.LLMTokenUsage, error) {
	var entries []models.LLMTokenUsage
	err := db.DB.NewSelect().
		Model(&entries).
		Where("reference = ?", projectID).
		OrderExpr("created_at DESC").
		Limit(limit).
		Scan(context.Background())
	return entries, err
}

// GetLLMTokenUsageSummaryByProject returns aggregate token totals for a
// project since the given time. Mirrors GetLLMTokenUsageSummary but
// scopes by project reference instead of user.
func (db *Database) GetLLMTokenUsageSummaryByProject(projectID string, since time.Time) (prompt, completion, total, callCount int, err error) {
	var result struct {
		Prompt     int `bun:"prompt"`
		Completion int `bun:"completion"`
		Total      int `bun:"total"`
		Calls      int `bun:"calls"`
	}
	err = db.DB.NewSelect().
		TableExpr("llm_token_usages").
		ColumnExpr("COALESCE(SUM(prompt_tokens), 0) AS prompt").
		ColumnExpr("COALESCE(SUM(completion_tokens), 0) AS completion").
		ColumnExpr("COALESCE(SUM(total_tokens), 0) AS total").
		ColumnExpr("COALESCE(SUM(call_count), 0) AS calls").
		Where("reference = ?", projectID).
		Where("created_at >= ?", since).
		Scan(context.Background(), &result)
	return result.Prompt, result.Completion, result.Total, result.Calls, err
}
