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
