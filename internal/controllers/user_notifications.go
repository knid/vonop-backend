package controllers

import (
	"errors"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
)

func (c *Controller) GetUserNotifications(user *models.User) ([]responses.UserNotificationResponse, error) {
	userNotifications, err := c.DB.GetUserNotifications(user)
	if err != nil {
		return nil, err
	}

	var userNotificationsResponse []responses.UserNotificationResponse
	for _, userNotification := range userNotifications {
		userNotificationsResponse = append(userNotificationsResponse, responses.UserNotificationResponse{
			ID:        userNotification.ID,
			Content:   userNotification.Content,
			Readed:    userNotification.Readed,
			CreatedAt: userNotification.CreatedAt,
		})
	}

	return userNotificationsResponse, nil
}

func (c *Controller) CreateUserNotification(user *models.User, content string) (*models.UserNotifications, error) {
	userNotification, err := c.DB.CreateUserNotification(&models.UserNotifications{
		UserID:  user.ID,
		Content: content,
	})

	if err != nil {
		return nil, err
	}

	return userNotification, nil
}

func (c *Controller) GetUserNotification(user *models.User, userNotificationId string) (*responses.UserNotificationResponse, error) {
	userNotification, err := c.DB.GetUserNotification(userNotificationId)
	if err != nil {
		return nil, err
	}

	if user.ID.String() != userNotification.UserID.String() {
		return nil, errors.New("you do not have access this resource")
	}

	userNotification.Readed = true
	c.DB.UpdateUserNotification(userNotification)

	userNotificationRes := responses.UserNotificationResponse{
		ID:        userNotification.ID,
		Content:   userNotification.Content,
		Readed:    userNotification.Readed,
		CreatedAt: userNotification.CreatedAt,
	}
	return &userNotificationRes, nil
}
