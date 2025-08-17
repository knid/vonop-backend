package database

import (
	"context"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
)

func (db *Database) GetNotifications(project *models.Project) (*[]models.Notification, error) {
	var notifications []models.Notification
	err := db.DB.NewSelect().Model(&notifications).Where("project_id = ?", project.ID.String()).Scan(context.Background())

	return &notifications, err
}

func (db *Database) GetNotificationById(id string) (*models.Notification, error) {
	var notification models.Notification
	notification.ID = uuid.MustParse(id)

	err := db.DB.NewSelect().Model(&notification).WherePK().Scan(context.Background())

	return &notification, err
}

func (db *Database) CreateNotification(notification *models.Notification) (*models.Notification, error) {
	_, err := db.DB.NewInsert().Model(notification).Exec(context.Background())

	return notification, err
}
