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
	_, err := db.DB.NewUpdate().Model(appUsageEvent).Where("id = ?", appUsageEvent.ID).Exec(context.Background())

	return &appUsageEvent, err
}

func (db *Database) GetDeviceById(deviceId string) (models.Device, error) {
	var device models.Device
	err := db.DB.NewSelect().Model(&device).Where("id = ?", deviceId).Scan(context.Background())

	return device, err
}

func (db *Database) GetDeviceByToken(token string) (*models.Device, error) {
	var device models.Device
	err := db.DB.NewSelect().Model(&device).Where("token = ?", token).Scan(context.Background())

	return &device, err
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

func (db *Database) CreateSession(session *models.Session) (*models.Session, error) {
	_, err := db.DB.NewInsert().Model(session).Exec(context.Background())

	return session, err
}

func (db *Database) UpdateSession(session models.Session) (*models.Session, error) {
	_, err := db.DB.NewUpdate().Model(session).Where("id = ?", session.ID).Exec(context.Background())

	return &session, err
}
func (db *Database) GetSessionsByProject(project *models.Project) ([]models.Session, error) {
	var sessions []models.Session
	err := db.DB.NewSelect().Model(&sessions).Where("project_id = ?", project.ID.String()).Scan(context.Background())

	return sessions, err
}

func (db *Database) GetSessionsByDevice(device *models.Device) ([]models.Session, error) {
	var sessions []models.Session
	err := db.DB.NewSelect().Model(&sessions).Where("device_id = ?", device.ID.String()).Scan(context.Background())

	return sessions, err
}

func (db *Database) GetSessionById(sessionId string) (models.Session, error) {
	var session models.Session
	err := db.DB.NewSelect().Model(&session).Where("id = ?", sessionId).Scan(context.Background())

	return session, err
}

func (db *Database) GetAppUsageEventsBySession(session *models.Session) ([]models.AppUsageEvent, error) {
	var appUsageEvents []models.AppUsageEvent
	err := db.DB.NewSelect().Model(&appUsageEvents).Where("session_id = ?", session.ID.String()).Scan(context.Background())

	return appUsageEvents, err

}

func (db *Database) GetAppUsageEventById(eventId string) (models.AppUsageEvent, error) {
	var appUsageEvent models.AppUsageEvent
	err := db.DB.NewSelect().Model(&appUsageEvent).Where("id = ?", eventId).Scan(context.Background())

	return appUsageEvent, err
}
