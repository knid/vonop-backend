package views

import (
	"net/http"
	"time"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) GetMontlyUsage(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	buildTasks, err := v.Controller.DB.GetAppBuildTasksByUser(user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	projects, err := v.Controller.GetProjects(user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	var notifications []models.Notification
	for _, project := range projects {
		projectNotifications, err := v.Controller.DB.GetNotifications(project.ID.String())
		if err != nil {
			continue
		}

		notifications = append(notifications, projectNotifications...)
	}

	renewStart, renewEnd := utils.BillingPeriod(user.SubscriptionStartAt, time.Now())

	notificationCount := 0
	for _, notification := range notifications {
		if notification.CreatedAt.After(renewStart) && notification.CreatedAt.Before(renewEnd) {
			notificationCount++
		}
	}

	appBuildTaskCount := 0
	for _, appBuildTask := range buildTasks {
		if appBuildTask.CreatedAt.After(renewStart) && appBuildTask.CreatedAt.Before(renewEnd) {
			appBuildTaskCount++
		}
	}

	// Fetch plan limits to include in response
	var notificationLimit, buildLimit int
	limits, limitsErr := v.Controller.DB.GetStandartUserLimitsByUser(user)
	if limitsErr == nil {
		notificationLimit = limits.MontlyNotificationCount
		buildLimit = limits.MontlyBuildCount
	}

	output := map[string]any{
		"notifications_used":    notificationCount,
		"app_builds_used":       appBuildTaskCount,
		"notification_limit":    notificationLimit,
		"app_builds_limit":      buildLimit,
		"subscription_status":   user.SubscriptionStatus,
		"subscription_plan_id":  user.SubscriptionID,
		"subscription_start_at": user.SubscriptionStartAt,
		"renewal_period_start":  renewStart,
		"renewal_period_end":    renewEnd,
	}

	utils.JSONResponse(w, output, http.StatusOK)
}
