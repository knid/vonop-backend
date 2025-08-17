package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) GetAppUsageEvents(project *models.Project) (*[]models.AppUsageEvent, error) {
	var appUsageEvents []models.AppUsageEvent
	err := db.DB.NewSelect().Model(&appUsageEvents).Where("project_id = ?", project.ID.String()).Scan(context.Background())

	return &appUsageEvents, err
}

func (db *Database) CreateAppUsageEvent(appUsageEvent *models.AppUsageEvent) (*models.AppUsageEvent, error) {
	_, err := db.DB.NewInsert().Model(appUsageEvent).Exec(context.Background())

	return appUsageEvent, err
}

func (db *Database) UpdateAppUsageEvent(appUsageEvent models.AppUsageEvent) (*models.AppUsageEvent, error) {
	_, err := db.DB.NewUpdate().Model(appUsageEvent).WherePK().Exec(context.Background())

	return &appUsageEvent, err
}
