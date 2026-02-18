package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) GetAppDashboardByProjectID(projectID string) (*models.AppDashboard, error) {
	var dashboard models.AppDashboard
	err := db.DB.NewSelect().Model(&dashboard).Where("app_project_id = ?", projectID).Scan(context.Background())
	return &dashboard, err
}

func (db *Database) GetAppDashboardByID(id string) (*models.AppDashboard, error) {
	var dashboard models.AppDashboard
	err := db.DB.NewSelect().Model(&dashboard).Where("id = ?", id).Scan(context.Background())
	return &dashboard, err
}

func (db *Database) CreateAppDashboard(d *models.AppDashboard) (*models.AppDashboard, error) {
	_, err := db.DB.NewInsert().Model(d).Exec(context.Background())
	return d, err
}

func (db *Database) UpdateAppDashboard(d *models.AppDashboard) (*models.AppDashboard, error) {
	_, err := db.DB.NewUpdate().Model(d).Where("id = ?", d.ID).Exec(context.Background())
	return d, err
}

func (db *Database) DeleteAppDashboard(d *models.AppDashboard) error {
	_, err := db.DB.NewDelete().Model(d).Where("id = ?", d.ID).Exec(context.Background())
	return err
}

// Panels

func (db *Database) GetAppDashboardPanelsByDashboardID(dashboardID string) ([]models.AppDashboardPanel, error) {
	var panels []models.AppDashboardPanel
	err := db.DB.NewSelect().Model(&panels).Where("dashboard_id = ?", dashboardID).Order("sort_order ASC").Scan(context.Background())
	return panels, err
}

func (db *Database) GetAppDashboardPanelByID(id string) (*models.AppDashboardPanel, error) {
	var panel models.AppDashboardPanel
	err := db.DB.NewSelect().Model(&panel).Where("id = ?", id).Scan(context.Background())
	return &panel, err
}

func (db *Database) CreateAppDashboardPanel(p *models.AppDashboardPanel) (*models.AppDashboardPanel, error) {
	_, err := db.DB.NewInsert().Model(p).Exec(context.Background())
	return p, err
}

func (db *Database) BulkCreateAppDashboardPanels(panels []models.AppDashboardPanel) error {
	if len(panels) == 0 {
		return nil
	}
	_, err := db.DB.NewInsert().Model(&panels).Exec(context.Background())
	return err
}

func (db *Database) UpdateAppDashboardPanel(p *models.AppDashboardPanel) (*models.AppDashboardPanel, error) {
	_, err := db.DB.NewUpdate().Model(p).Where("id = ?", p.ID).Exec(context.Background())
	return p, err
}

func (db *Database) DeleteAppDashboardPanel(p *models.AppDashboardPanel) error {
	_, err := db.DB.NewDelete().Model(p).Where("id = ?", p.ID).Exec(context.Background())
	return err
}

func (db *Database) DeleteAppDashboardPanelsByDashboardID(dashboardID string) error {
	_, err := db.DB.NewDelete().Model((*models.AppDashboardPanel)(nil)).Where("dashboard_id = ?", dashboardID).Exec(context.Background())
	return err
}
