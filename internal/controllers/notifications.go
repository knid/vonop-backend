package controllers

import (
	"errors"
	"fmt"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
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
		return nil, errors.New("you do not have access this resources")
	}

	return notification, nil
}

func (c *Controller) SendNotification(user *models.User, notificationCreateReq requests.NotificationCreateRequest) (*responses.NotificationSendResponse, error) {
	fmt.Println("SendNotification called at " + (string)(time.Now().Format("2006-01-02 15:04:05")))
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
		return nil, errors.New("you do not have access this resources")
	}

	notification := models.Notification{
		ProjectID:   notificationCreateReq.ProjectID,
		Title:       notificationCreateReq.Title,
		Description: notificationCreateReq.Description,
	}

	fmt.Println("Creating notification in DB")
	createdNotification, err := c.DB.CreateNotification(&notification)
	if err != nil {
		return nil, err
	}

	fmt.Println("Saving image to: " + "storage/images/" + createdNotification.ID.String() + ".png")

	utils.ConvertBase64ToImageSave(notificationCreateReq.Image, "storage/images/"+createdNotification.ID.String()+".png")

	err = external.SendOneSignalNotification(&notification, project.OneSignalAppID)

	return &responses.NotificationSendResponse{
		SuccessResponse: responses.SuccessResponse{Success: true},
	}, err

}
