package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) GetNotifications(projectId string) ([]models.Notification, error) {
	var notifications []models.Notification
	err := db.DB.NewSelect().Model(&notifications).Where("project_id = ?", projectId).Scan(context.Background())

	return notifications, err
}

func (db *Database) GetNotificationById(id string) (*models.Notification, error) {
	var notification models.Notification

	err := db.DB.NewSelect().Model(&notification).Where("id = ?", id).Scan(context.Background())

	return &notification, err
}

func (db *Database) CreateNotification(notification *models.Notification) (*models.Notification, error) {
	_, err := db.DB.NewInsert().Model(notification).Exec(context.Background())

	return notification, err
}
