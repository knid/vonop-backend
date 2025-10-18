package views

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) GetProjectNotifications(w http.ResponseWriter, r *http.Request) {
	// Implementation of the view to get notifications for a specific project
	tokenString, err := utils.ExtractTokenFromHeader(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	userInformationRes, err := v.Controller.GetUser(tokenString)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	user, err := v.Controller.GetUserModelById(userInformationRes.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	if !(user.SubscriptionStatus == "active" || user.SubscriptionStatus == "trial") {
		utils.JSONErrorResponse(w, "you do not have access to this resource", http.StatusForbidden)
		return
	}

	projectID := chi.URLParam(r, "projectId")
	if projectID == "" {
		utils.JSONErrorResponse(w, "project_id is required", http.StatusBadRequest)
		return
	}

	projectInformationRes, err := v.Controller.GetProject(user, projectID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	notificationsRes, err := v.Controller.GetNotifications(projectInformationRes.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, notificationsRes, http.StatusOK)
}

func (v *HttpViews) SendProjectNotification(w http.ResponseWriter, r *http.Request) {

	fmt.Println("SendProjectNotification called at " + (string)(time.Now().Format("2006-01-02 15:04:05")))
	tokenString, err := utils.ExtractTokenFromHeader(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	userInformationRes, err := v.Controller.GetUser(tokenString)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	user, err := v.Controller.GetUserModelById(userInformationRes.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	if !(user.SubscriptionStatus == "active" || user.SubscriptionStatus == "trial") {
		utils.JSONErrorResponse(w, "you do not have access to this resource", http.StatusForbidden)
		go v.Controller.CRMService.SendEvent(user, constants.BREVO_EVENT_SUBSCRIPTION_NEED, map[string]any{
			"action": "send_notification",
		})
		return
	}

	var notifications []models.Notification
	projects, err := v.Controller.GetProjects(user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	for _, project := range projects {
		projectNotifications, err := v.Controller.DB.GetNotifications(project.ID.String())
		if err != nil {
			continue
		}

		notifications = append(notifications, projectNotifications...)
	}

	subsStart := user.SubscriptionStartAt
	now := time.Now()

	renewStart := time.Date(now.Year(), now.Month(), subsStart.Day(), 0, 0, 0, 0, now.Location())

	if now.Before(renewStart) {
		renewStart = renewStart.AddDate(0, -1, 0)
	}

	renewEnd := renewStart.AddDate(0, 1, 0)

	count := 0
	for _, notification := range notifications {
		if notification.CreatedAt.After(renewStart) && notification.CreatedAt.Before(renewEnd) {
			count++
		}
	}

	subsInfo, err := v.Controller.GetSubscription(user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	limits, err := v.Controller.DB.GetStandartUserLimitsById(subsInfo.Limits.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	if count >= limits.MontlyNotificationCount {
		utils.JSONErrorResponse(w, "You have reached your monthly notification limit", http.StatusForbidden)
		go v.Controller.CRMService.SendEvent(user, constants.BREVO_EVENT_SUBSCRIPTION_QUOTA_REACHED, map[string]any{
			"action": "send_notification",
			"limit":  limits.MontlyNotificationCount,
		})
		return
	}

	projectId := chi.URLParam(r, "projectId")
	projectUUID, err := uuid.Parse(projectId)
	if err != nil {
		utils.JSONErrorResponse(w, "project_id is required", http.StatusBadRequest)
		return
	}
	// Implementation of the view to send a notification
	var notificationCreateReq requests.NotificationCreateRequest
	if err := utils.ParseJSONBody(r, &notificationCreateReq); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	notificationCreateReq.ProjectID = projectUUID

	fmt.Println("Received notification send request for project ID: " + projectId)
	notificationSendRes, err := v.Controller.SendNotification(user, notificationCreateReq)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, notificationSendRes, http.StatusCreated)
}

func (v *HttpViews) GetProjectNotification(w http.ResponseWriter, r *http.Request) {
	// Implementation of the view to get a specific notification by ID
	tokenString, err := utils.ExtractTokenFromHeader(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	userInformationRes, err := v.Controller.GetUser(tokenString)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	user, err := v.Controller.GetUserModelById(userInformationRes.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	notificationID := chi.URLParam(r, "notificationId")
	if notificationID == "" {
		utils.JSONErrorResponse(w, "notification_id is required", http.StatusBadRequest)
		return
	}

	notificationRes, err := v.Controller.GetNotification(user, notificationID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, notificationRes, http.StatusOK)
}
