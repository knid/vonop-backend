package database

import (
	"context"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
)

func (db *Database) GetSubscriptions() (*[]models.StandardUserSubscription, error) {
	var standardUserSubscriptions []models.StandardUserSubscription
	err := db.DB.NewSelect().
		Model(&standardUserSubscriptions).
		Order("monthly_price ASC").
		Scan(context.Background())

	return &standardUserSubscriptions, err
}

func (db *Database) GetSubscriptionById(id string) (*models.StandardUserSubscription, error) {
	var standardUserSubscription models.StandardUserSubscription
	err := db.DB.NewSelect().Model(&standardUserSubscription).Where("id = ?", id).Scan(context.Background())

	return &standardUserSubscription, err
}

func (db *Database) UpdateSubscription(standardUserSubscription *models.StandardUserSubscription) (*models.StandardUserSubscription, error) {
	_, err := db.DB.NewUpdate().Model(standardUserSubscription).Where("id = ?", standardUserSubscription.ID).Exec(context.Background())

	return standardUserSubscription, err
}

func (db *Database) GetStandardUserSubscriptionByOrg(org *models.Organization) (*models.StandardUserSubscription, error) {
	var subs models.StandardUserSubscription
	err := db.DB.NewSelect().Model(&subs).Where("id = ?", org.SubscriptionID).Scan(context.Background())

	return &subs, err
}

func (db *Database) SetStandardUserSubscription(org *models.Organization, subs *models.StandardUserSubscription) (*models.Organization, error) {
	org.SubscriptionID = subs.ID
	_, err := db.DB.NewUpdate().Model(org).Where("id = ?", org.ID.String()).Exec(context.Background())
	if err != nil {
		return nil, err
	}

	return org, nil
}

func (db *Database) DeleteStandardUserSubscription(org *models.Organization) (*models.Organization, error) {
	org.SubscriptionID = uuid.Nil
	_, err := db.DB.NewUpdate().Model(org).Where("id = ?", org.ID.String()).Exec(context.Background())
	if err != nil {
		return nil, err
	}

	return org, nil
}

// GetStandardUserLimitsByUser keeps a User-keyed signature because many
// callers still pass *User. Internally it resolves the user's personal
// Organization (Plan 5) and delegates to the org-keyed lookup.
func (db *Database) GetStandardUserLimitsByUser(user *models.User) (*models.StandardUserLimits, error) {
	org, err := db.GetPersonalOrgByUserID(user.ID.String())
	if err != nil {
		return nil, err
	}

	subs, err := db.GetStandardUserSubscriptionByOrg(org)
	if err != nil {
		return nil, err
	}

	var limits models.StandardUserLimits
	err = db.DB.NewSelect().Model(&limits).Where("id = ?", subs.LimitsID).Scan(context.Background())

	return &limits, err
}

// GetLimitsForOrg resolves limits via the given organization's subscription.
// Used by background workers that must gate against the billing actor (org)
// rather than the API actor (user) — Plan A made limits/credits org-scoped.
func (db *Database) GetLimitsForOrg(ctx context.Context, orgID uuid.UUID) (*models.StandardUserLimits, error) {
	var org models.Organization
	if err := db.DB.NewSelect().Model(&org).Where("id = ?", orgID).Scan(ctx); err != nil {
		return nil, err
	}
	subs, err := db.GetStandardUserSubscriptionByOrg(&org)
	if err != nil {
		return nil, err
	}
	var limits models.StandardUserLimits
	if err := db.DB.NewSelect().Model(&limits).Where("id = ?", subs.LimitsID).Scan(ctx); err != nil {
		return nil, err
	}
	return &limits, nil
}

func (db *Database) GetStandardUserLimitsById(id string) (*models.StandardUserLimits, error) {
	var limits models.StandardUserLimits
	err := db.DB.NewSelect().Model(&limits).Where("id = ?", id).Scan(context.Background())

	return &limits, err
}

func (db *Database) GetSubscriptionByPriceID(priceID string) (*models.StandardUserSubscription, error) {
	var subs models.StandardUserSubscription
	err := db.DB.NewSelect().Model(&subs).
		Where("price_id = ? OR annual_price_id = ?", priceID, priceID).
		Scan(context.Background())

	return &subs, err
}

func (db *Database) GetSubscriptionByName(name string) (*models.StandardUserSubscription, error) {
	var subs models.StandardUserSubscription
	err := db.DB.NewSelect().Model(&subs).Where("name = ?", name).Scan(context.Background())
	return &subs, err
}

// GetSubscriptionByNameIncludeDeleted finds a subscription by name, including soft-deleted rows.
func (db *Database) GetSubscriptionByNameIncludeDeleted(name string) (*models.StandardUserSubscription, error) {
	var subs models.StandardUserSubscription
	err := db.DB.NewSelect().Model(&subs).WhereAllWithDeleted().Where("name = ?", name).Scan(context.Background())
	return &subs, err
}

func (db *Database) CreateStandardUserLimits(limits *models.StandardUserLimits) (*models.StandardUserLimits, error) {
	_, err := db.DB.NewInsert().Model(limits).Exec(context.Background())
	return limits, err
}

func (db *Database) UpdateStandardUserLimits(limits *models.StandardUserLimits) (*models.StandardUserLimits, error) {
	_, err := db.DB.NewUpdate().Model(limits).Where("id = ?", limits.ID).Exec(context.Background())
	return limits, err
}

func (db *Database) CreateSubscription(sub *models.StandardUserSubscription) (*models.StandardUserSubscription, error) {
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

// CountDeployedWebAppsByOrgID is the org-scoped variant used for plan limit
// enforcement. Plan/limits Phase 4 (§B): limits live on the org, so the count
// must too. Joins via app_projects.organization_id since app_deployments
// itself doesn't carry org_id.
func (db *Database) CountDeployedWebAppsByOrgID(orgID string) (int, error) {
	var count int
	err := db.DB.NewSelect().
		TableExpr("app_deployments AS d").
		Join("JOIN app_projects AS p ON p.id = d.app_project_id").
		ColumnExpr("COUNT(DISTINCT d.app_project_id)").
		Where("p.organization_id = ?", orgID).
		Where("d.platform = 'web'").
		Where("d.status = 'deployed'").
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

// GetDeployedWebAppsByOrgID — org-scoped sibling used by the over-quota error
// payload so the message can list every deployed app the user could clean up.
func (db *Database) GetDeployedWebAppsByOrgID(orgID string) ([]DeployedWebAppInfo, error) {
	var apps []DeployedWebAppInfo
	err := db.DB.NewSelect().
		TableExpr("app_deployments AS d").
		Join("JOIN app_projects AS p ON p.id = d.app_project_id").
		ColumnExpr("DISTINCT d.app_project_id, p.name").
		Where("p.organization_id = ?", orgID).
		Where("d.platform = 'web'").
		Where("d.status = 'deployed'").
		Scan(context.Background(), &apps)
	return apps, err
}
