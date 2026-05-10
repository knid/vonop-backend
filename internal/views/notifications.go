package views

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
	"github.com/posthog/posthog-go"
)

func (v *HttpViews) GetProjectNotifications(w http.ResponseWriter, r *http.Request) {
	// Implementation of the view to get notifications for a specific project
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	if requireActiveSubscription(w, v.Controller, user) {
		return
	}

	projectID := chi.URLParam(r, "projectId")
	if projectID == "" {
		utils.JSONErrorCode(w, constants.ErrValidationRequired, http.StatusBadRequest, map[string]any{"field": "project_id"})
		return
	}

	projectInformationRes, err := v.Controller.GetProject(user, projectID)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	notificationsRes, err := v.Controller.GetNotifications(projectInformationRes.ID.String())
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, notificationsRes, http.StatusOK)
}

func (v *HttpViews) SendProjectNotification(w http.ResponseWriter, r *http.Request) {

	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	if requireActiveSubscription(w, v.Controller, user) {
		go v.Controller.EventService.SendEvent(user, constants.EVENT_SUBSCRIPTION_NEED, map[string]any{
			"action": "send_notification",
		})
		return
	}

	var notifications []models.Notification
	projects, err := v.Controller.GetProjects(user)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	for _, project := range projects {
		projectNotifications, err := v.Controller.DB.GetNotifications(project.ID.String())
		if err != nil {
			continue
		}

		notifications = append(notifications, projectNotifications...)
	}

	// Subscription billing period anchors on the personal org (Plan 5).
	org, err := v.Controller.DB.GetPersonalOrgByUserID(user.ID.String())
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}
	subsStart := org.SubscriptionStartAt
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

	limits, planName, err := v.Controller.GetUserLimitsWithPlan(user)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	if count >= limits.MontlyNotificationCount {
		_, renewEnd := utils.BillingPeriod(org.SubscriptionStartAt, time.Now())
		utils.JSONError(w, constants.ErrNotificationLimitReached,
			"You have reached your monthly notification limit",
			http.StatusForbidden, map[string]any{
				"used":         count,
				"limit":        limits.MontlyNotificationCount,
				"renewal_date": renewEnd.Format(time.RFC3339),
				"plan_name":    planName,
			})
		go v.Controller.EventService.SendEvent(user, constants.EVENT_SUBSCRIPTION_QUOTA_REACHED, map[string]any{
			"action": "send_notification",
			"limit":  limits.MontlyNotificationCount,
		})
		go v.Controller.TrackUserEvent(user, constants.EVENT_QUOTA_REACHED, posthog.NewProperties().Set("action", "send_notification").Set("limit", limits.MontlyNotificationCount))
		return
	}

	projectId := chi.URLParam(r, "projectId")
	projectUUID, err := uuid.Parse(projectId)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrValidationRequired, http.StatusBadRequest, map[string]any{"field": "project_id"})
		return
	}
	// Implementation of the view to send a notification
	var notificationCreateReq requests.NotificationCreateRequest
	if err := utils.ParseJSONBody(r, &notificationCreateReq); err != nil {
		utils.ValidationError(w, err)
		return
	}

	notificationCreateReq.ProjectID = projectUUID

	notificationSendRes, err := v.Controller.SendNotification(user, notificationCreateReq)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, notificationSendRes, http.StatusCreated)
}

func (v *HttpViews) GetProjectNotification(w http.ResponseWriter, r *http.Request) {
	// Implementation of the view to get a specific notification by ID
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	notificationID := chi.URLParam(r, "notificationId")
	if notificationID == "" {
		utils.JSONErrorCode(w, constants.ErrValidationRequired, http.StatusBadRequest, map[string]any{"field": "notification_id"})
		return
	}

	notificationRes, err := v.Controller.GetNotification(user, notificationID)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, notificationRes, http.StatusOK)
}
