package database

import (
	"context"
	"time"

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

// GetAppDomainsPendingDNSCheck returns domains whose DNS records have not yet
// been verified to point at our edge. Used by the DNS-check cron job.
func (db *Database) GetAppDomainsPendingDNSCheck() ([]models.AppDomain, error) {
	var rows []models.AppDomain
	err := db.DB.NewSelect().Model(&rows).
		Where("dns_check_status = ?", "pending").
		Scan(context.Background())
	return rows, err
}

// MarkAppDomainDNSStatus updates the DNS verification status for a domain.
func (db *Database) MarkAppDomainDNSStatus(domainID string, status string) error {
	_, err := db.DB.NewUpdate().Model((*models.AppDomain)(nil)).
		Set("dns_check_status = ?", status).
		Set("updated_at = ?", time.Now()).
		Where("id = ?", domainID).
		Exec(context.Background())
	return err
}
