package database

import (
	"context"
	"time"

	"github.com/knid/wemob/internal/models"
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
	_, err := db.DB.NewInsert().Model(balance).Exec(context.Background())
	return balance, err
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
