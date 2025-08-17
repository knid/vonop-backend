package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) GetSubscriptions() (*[]models.StandartUserSubscription, error) {
	var standartUserSubscriptions []models.StandartUserSubscription
	err := db.DB.NewSelect().Model(&standartUserSubscriptions).Scan(context.Background())

	return &standartUserSubscriptions, err
}

func (db *Database) UpdateSubscription(standartUserSubscription *models.StandartUserSubscription) (*models.StandartUserSubscription, error) {
	_, err := db.DB.NewUpdate().Model(standartUserSubscription).WherePK().Exec(context.Background())

	return standartUserSubscription, err
}

func (db *Database) DeleteSubscription(standartUserSubscription *models.StandartUserSubscription) (*models.StandartUserSubscription, error) {
	_, err := db.DB.NewDelete().Model(standartUserSubscription).WherePK().Exec(context.Background())

	return standartUserSubscription, err
}
