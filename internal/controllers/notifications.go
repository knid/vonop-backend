package controllers

import (
	"errors"

	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
)

func (c *Controller) GetNotifications(projectId string) ([]models.Notification, error) {
	project, err := c.DB.GetProjectById(projectId)
	if err != nil {
		return nil, err
	}

	return c.DB.GetNotifications(project)
}

func (c *Controller) GetNotification(user *models.User, id string) (*models.Notification, error) {
	notification, err := c.DB.GetNotificationById(id)
	if err != nil {
		return nil, err
	}

	if notification.Project.User.ID.String() != user.ID.String() {
		return nil, errors.New("you do not have access this resources")
	}

	return notification, nil
}

func (c *Controller) SendNotification(user *models.User, notificationCreateReq requests.NotificationCreateRequest) (*responses.NotificationSendResponse, error) {
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := validate.Struct(notificationCreateReq); err != nil {
		return nil, err
	}

	// verify user has access to the project
	project, err := c.DB.GetProjectById(notificationCreateReq.ProjectID.String())
	if err != nil {
		return nil, err
	}
	if project.User.ID.String() != user.ID.String() {
		return nil, errors.New("you do not have access this resources")
	}

	// TODO:
	//
	// Create onesignal notification send request

	return nil, nil

}
