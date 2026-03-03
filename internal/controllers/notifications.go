package controllers

import (
	"errors"

	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils/external"
)

func (c *Controller) GetNotifications(projectId string) ([]responses.NotificationInformationResponse, error) {
	project, err := c.DB.GetProjectById(projectId)
	if err != nil {
		return nil, err
	}

	notifications, err := c.DB.GetNotifications(project.ID.String())
	if err != nil {
		return nil, err
	}

	notificationResponses := make([]responses.NotificationInformationResponse, len(notifications))
	for i, notification := range notifications {
		notificationResponses[i] = responses.NotificationInformationResponse{
			ID:          notification.ID,
			ProjectID:   notification.ProjectID,
			Title:       notification.Title,
			Description: notification.Description,
			URL:         notification.URL,
			Image:       notification.Image,
			CreatedAt:   notification.CreatedAt,
		}
	}

	return notificationResponses, nil
}

func (c *Controller) GetNotification(user *models.User, id string) (*models.Notification, error) {
	notification, err := c.DB.GetNotificationById(id)
	if err != nil {
		return nil, err
	}

	if notification.Project.User.ID.String() != user.ID.String() {
		return nil, errors.New("you do not have access to this resource")
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
	if project.UserID.String() != user.ID.String() {
		return nil, errors.New("you do not have access to this resource")
	}

	notification := models.Notification{
		ProjectID:   notificationCreateReq.ProjectID,
		Title:       notificationCreateReq.Title,
		Description: notificationCreateReq.Description,
		URL:         notificationCreateReq.URL,
		Image:       notificationCreateReq.Image,
		SendAfter:   notificationCreateReq.SendAfter,
	}

	createdNotification, err := c.DB.CreateNotification(&notification)
	if err != nil {
		return nil, err
	}

	// utils.ConvertBase64ToImageSave(notificationCreateReq.Image, "images/"+createdNotification.ID.String())

	err = external.SendOneSignalNotification(createdNotification, project)

	return &responses.NotificationSendResponse{
		SuccessResponse: responses.SuccessResponse{Success: true},
	}, err

}
