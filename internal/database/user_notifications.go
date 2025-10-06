package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) GetUserNotification(userNotificationId string) (*models.UserNotifications, error) {
	var userNotification models.UserNotifications
	err := db.DB.NewSelect().Model(&userNotification).Where("id = ?", userNotificationId).Scan(context.Background())

	return &userNotification, err
}

func (db *Database) GetUserNotifications(user *models.User) ([]models.UserNotifications, error) {
	var userNotifications []models.UserNotifications
	err := db.DB.NewSelect().Model(&userNotifications).Where("user_id = ?", user.ID).Scan(context.Background())

	return userNotifications, err
}

func (db *Database) CreateUserNotification(userNotification *models.UserNotifications) (*models.UserNotifications, error) {
	_, err := db.DB.NewInsert().Model(userNotification).Exec(context.Background())

	return userNotification, err
}

func (db *Database) UpdateUserNotification(userNotification *models.UserNotifications) (*models.UserNotifications, error) {
	_, err := db.DB.NewUpdate().Model(userNotification).Where("id = ?", userNotification.ID).Exec(context.Background())

	return userNotification, err
}
