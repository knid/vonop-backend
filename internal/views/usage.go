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

	var notifications []models.Notification
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

	output := map[string]any{
		"notifications_used":    notificationCount,
		"app_builds_used":       appBuildTaskCount,
		"notification_limit":    user.Subscription.Limits.MontlyNotificationCount,
		"app_builds_limit":      user.Subscription.Limits.MontlyBuildCount,
		"subscription_status":   user.SubscriptionStatus,
		"subscription_plan_id":  user.SubscriptionID,
		"subscription_start_at": user.SubscriptionStartAt,
		"renewal_period_start":  renewStart,
		"renewal_period_end":    renewEnd,
	}

	utils.JSONResponse(w, output, http.StatusOK)
}
