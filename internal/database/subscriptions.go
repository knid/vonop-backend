package database

import (
	"context"
	"log"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
)

func (db *Database) GetSubscriptions() (*[]models.StandartUserSubscription, error) {
	var standartUserSubscriptions []models.StandartUserSubscription
	err := db.DB.NewSelect().Model(&standartUserSubscriptions).Scan(context.Background())

	return &standartUserSubscriptions, err
}

func (db *Database) GetSubscriptionById(id string) (*models.StandartUserSubscription, error) {
	var standartUserSubscription models.StandartUserSubscription
	err := db.DB.NewSelect().Model(&standartUserSubscription).Where("id = ?", id).Scan(context.Background())

	return &standartUserSubscription, err
}

func (db *Database) UpdateSubscription(standartUserSubscription *models.StandartUserSubscription) (*models.StandartUserSubscription, error) {
	_, err := db.DB.NewUpdate().Model(standartUserSubscription).WherePK().Exec(context.Background())

	return standartUserSubscription, err
}

func (db *Database) DeleteSubscription(standartUserSubscription *models.StandartUserSubscription) (*models.StandartUserSubscription, error) {
	_, err := db.DB.NewDelete().Model(standartUserSubscription).WherePK().Exec(context.Background())

	return standartUserSubscription, err
}

func (db *Database) GetStandartUserSubscriptionByUser(user *models.User) (*models.StandartUserSubscription, error) {
	var subs models.StandartUserSubscription
	log.Println(user.SubscriptionID)
	err := db.DB.NewSelect().Model(&subs).Where("id = ?", user.SubscriptionID).Scan(context.Background())

	return &subs, err
}

func (db *Database) SetStandartUserSubscription(user *models.User, subs *models.StandartUserSubscription) (*models.User, error) {
	user.SubscriptionID = subs.ID
	_, err := db.DB.NewUpdate().Model(user).Where("id = ?", user.ID.String()).Exec(context.Background())
	if err != nil {
		return nil, err
	}

	return user, nil
}

func (db *Database) DeleteStandartUserSubscription(user *models.User) (*models.User, error) {
	user.SubscriptionID = uuid.Nil
	_, err := db.DB.NewUpdate().Model(user).Where("id = ?", user.ID.String()).Exec(context.Background())
	if err != nil {
		return nil, err
	}

	return user, nil
}

func (db *Database) GetStandartUserLimitsByUser(user *models.User) (*models.StandartUserLimits, error) {
	subs, err := db.GetStandartUserSubscriptionByUser(user)
	if err != nil {
		return nil, err
	}

	var limits models.StandartUserLimits
	err = db.DB.NewSelect().Model(&limits).Where("id = ?", subs.LimitsID).Scan(context.Background())

	return &limits, err
}

func (db *Database) GetStandartUserLimitsById(id string) (*models.StandartUserLimits, error) {
	var limits models.StandartUserLimits
	err := db.DB.NewSelect().Model(&limits).Where("id = ?", id).Scan(context.Background())

	return &limits, err
}

func (db *Database) GetSubscriptionByPriceID(priceID string) (*models.StandartUserSubscription, error) {
	var subs models.StandartUserSubscription
	err := db.DB.NewSelect().Model(&subs).Where("price_id = ?", priceID).Scan(context.Background())

	return &subs, err
}
