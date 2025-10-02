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

func (db *Database) GetDeviceById(deviceId string) (models.Device, error) {
	var device models.Device
	err := db.DB.NewSelect().Model(&device).Where("id = ?", deviceId).Scan(context.Background())

	return device, err
}

func (db *Database) GetDeviceByToken(token string) (models.Device, error) {
	var device models.Device
	err := db.DB.NewSelect().Model(&device).Where("token = ?", token).Scan(context.Background())

	return device, err
}

func (db *Database) CreateDevice(device *models.Device) (*models.Device, error) {
	_, err := db.DB.NewInsert().Model(device).Exec(context.Background())

	return device, err
}

func (db *Database) GetDevicesByProject(project *models.Project) ([]models.Device, error) {
	var devices []models.Device
	err := db.DB.NewSelect().Model(&devices).Where("project_id = ?", project.ID.String()).Scan(context.Background())

	return devices, err
}
