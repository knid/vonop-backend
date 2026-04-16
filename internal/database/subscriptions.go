package database

import (
	"context"

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
	_, err := db.DB.NewUpdate().Model(standartUserSubscription).Where("id = ?", standartUserSubscription.ID).Exec(context.Background())

	return standartUserSubscription, err
}

func (db *Database) GetStandartUserSubscriptionByUser(user *models.User) (*models.StandartUserSubscription, error) {
	var subs models.StandartUserSubscription
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
	err := db.DB.NewSelect().Model(&subs).
		Where("price_id = ? OR annual_price_id = ?", priceID, priceID).
		Scan(context.Background())

	return &subs, err
}

func (db *Database) GetSubscriptionByName(name string) (*models.StandartUserSubscription, error) {
	var subs models.StandartUserSubscription
	err := db.DB.NewSelect().Model(&subs).Where("name = ?", name).Scan(context.Background())
	return &subs, err
}

// GetSubscriptionByNameIncludeDeleted finds a subscription by name, including soft-deleted rows.
func (db *Database) GetSubscriptionByNameIncludeDeleted(name string) (*models.StandartUserSubscription, error) {
	var subs models.StandartUserSubscription
	err := db.DB.NewSelect().Model(&subs).WhereAllWithDeleted().Where("name = ?", name).Scan(context.Background())
	return &subs, err
}

func (db *Database) CreateStandartUserLimits(limits *models.StandartUserLimits) (*models.StandartUserLimits, error) {
	_, err := db.DB.NewInsert().Model(limits).Exec(context.Background())
	return limits, err
}

func (db *Database) UpdateStandartUserLimits(limits *models.StandartUserLimits) (*models.StandartUserLimits, error) {
	_, err := db.DB.NewUpdate().Model(limits).Where("id = ?", limits.ID).Exec(context.Background())
	return limits, err
}

func (db *Database) CreateSubscription(sub *models.StandartUserSubscription) (*models.StandartUserSubscription, error) {
	_, err := db.DB.NewInsert().Model(sub).Exec(context.Background())
	return sub, err
}

func (db *Database) CountDeployedWebAppsByUserID(userID string) (int, error) {
	var count int
	err := db.DB.NewSelect().
		TableExpr("app_deployments").
		ColumnExpr("COUNT(DISTINCT app_project_id)").
		Where("user_id = ?", userID).
		Where("platform = 'web'").
		Where("status = 'deployed'").
		Scan(context.Background(), &count)
	return count, err
}

// DeployedWebAppInfo holds the name and ID of a deployed web app.
type DeployedWebAppInfo struct {
	AppProjectID string `bun:"app_project_id"`
	Name         string `bun:"name"`
}

func (db *Database) GetDeployedWebAppsByUserID(userID string) ([]DeployedWebAppInfo, error) {
	var apps []DeployedWebAppInfo
	err := db.DB.NewSelect().
		TableExpr("app_deployments AS d").
		Join("JOIN app_projects AS p ON p.id = d.app_project_id").
		ColumnExpr("DISTINCT d.app_project_id, p.name").
		Where("d.user_id = ?", userID).
		Where("d.platform = 'web'").
		Where("d.status = 'deployed'").
		Scan(context.Background(), &apps)
	return apps, err
}

func (db *Database) CountDeployedMobileAppsByUserID(userID string) (int, error) {
	var count int
	err := db.DB.NewSelect().
		TableExpr("app_deployments").
		ColumnExpr("COUNT(DISTINCT app_project_id)").
		Where("user_id = ?", userID).
		Where("platform = 'mobile'").
		Where("status = 'deployed'").
		Scan(context.Background(), &count)
	return count, err
}

func (db *Database) GetDeployedMobileAppsByUserID(userID string) ([]DeployedWebAppInfo, error) {
	var apps []DeployedWebAppInfo
	err := db.DB.NewSelect().
		TableExpr("app_deployments AS d").
		Join("JOIN app_projects AS p ON p.id = d.app_project_id").
		ColumnExpr("DISTINCT d.app_project_id, p.name").
		Where("d.user_id = ?", userID).
		Where("d.platform = 'mobile'").
		Where("d.status = 'deployed'").
		Scan(context.Background(), &apps)
	return apps, err
}

func (db *Database) IsAppAlreadyDeployedMobile(appProjectID string) (bool, error) {
	count, err := db.DB.NewSelect().
		TableExpr("app_deployments").
		Where("app_project_id = ?", appProjectID).
		Where("platform = 'mobile'").
		Where("status = 'deployed'").
		Count(context.Background())
	return count > 0, err
}
