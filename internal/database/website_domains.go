package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) CreateDomain(d *models.WebsiteDomain) (*models.WebsiteDomain, error) {
	_, err := db.DB.NewInsert().Model(d).Exec(context.Background())
	return d, err
}

func (db *Database) GetDomainsByWebsiteId(websiteId string) ([]models.WebsiteDomain, error) {
	var domains []models.WebsiteDomain
	err := db.DB.NewSelect().Model(&domains).Where("website_id = ?", websiteId).Scan(context.Background())
	return domains, err
}

func (db *Database) GetDomainById(id string) (*models.WebsiteDomain, error) {
	var domain models.WebsiteDomain
	err := db.DB.NewSelect().Model(&domain).Where("id = ?", id).Scan(context.Background())
	return &domain, err
}

func (db *Database) GetDomainByName(domainName string) (*models.WebsiteDomain, error) {
	var domain models.WebsiteDomain
	err := db.DB.NewSelect().Model(&domain).Where("domain = ?", domainName).Scan(context.Background())
	return &domain, err
}

func (db *Database) UpdateDomain(d *models.WebsiteDomain) (*models.WebsiteDomain, error) {
	_, err := db.DB.NewUpdate().Model(d).Where("id = ?", d.ID).Exec(context.Background())
	return d, err
}

func (db *Database) DeleteDomain(d *models.WebsiteDomain) error {
	_, err := db.DB.NewDelete().Model(d).Where("id = ?", d.ID).Exec(context.Background())
	return err
}
