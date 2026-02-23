package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) CreateAppDomain(d *models.AppDomain) (*models.AppDomain, error) {
	_, err := db.DB.NewInsert().Model(d).Exec(context.Background())
	return d, err
}

func (db *Database) GetAppDomainsByProjectID(projectID string) ([]models.AppDomain, error) {
	var domains []models.AppDomain
	err := db.DB.NewSelect().Model(&domains).Where("app_project_id = ?", projectID).Order("created_at DESC").Scan(context.Background())
	return domains, err
}

func (db *Database) GetAppDomainByID(id string) (*models.AppDomain, error) {
	var domain models.AppDomain
	err := db.DB.NewSelect().Model(&domain).Where("id = ?", id).Scan(context.Background())
	return &domain, err
}

func (db *Database) GetAppDomainByName(domainName string) (*models.AppDomain, error) {
	var domain models.AppDomain
	err := db.DB.NewSelect().Model(&domain).Where("domain = ?", domainName).Scan(context.Background())
	return &domain, err
}

func (db *Database) UpdateAppDomain(d *models.AppDomain) (*models.AppDomain, error) {
	_, err := db.DB.NewUpdate().Model(d).Where("id = ?", d.ID).Exec(context.Background())
	return d, err
}

func (db *Database) DeleteAppDomain(d *models.AppDomain) error {
	_, err := db.DB.NewDelete().Model(d).Where("id = ?", d.ID).Exec(context.Background())
	return err
}
