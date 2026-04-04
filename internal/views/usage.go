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
	var notificationLimit, buildLimit, appLimit, deployLimit, aiCreditsLimit int
	limits, limitsErr := v.Controller.GetUserLimits(user)
	if limitsErr == nil {
		notificationLimit = limits.MontlyNotificationCount
		buildLimit = limits.MontlyBuildCount
		appLimit = limits.MonthlyAppCount
		deployLimit = limits.MonthlyDeployCount
		aiCreditsLimit = limits.MonthlyAICredits
	}

	// App projects created this period
	appProjectsUsed, _ := v.Controller.DB.CountAppProjectsByUserIDSince(user.ID.String(), renewStart)

	// Web deploys this period
	webDeploysUsed, _ := v.Controller.DB.CountWebDeploysSince(user.ID.String(), renewStart)

	// AI credits balance
	var aiCreditsBalance int
	balance, balErr := v.Controller.GetOrCreateCreditsBalance(user)
	if balErr == nil {
		aiCreditsBalance = balance.Balance
	}

	output := map[string]any{
		"notifications_used":    notificationCount,
		"app_builds_used":       appBuildTaskCount,
		"notification_limit":    notificationLimit,
		"app_builds_limit":      buildLimit,
		"app_projects_used":     appProjectsUsed,
		"app_projects_limit":    appLimit,
		"web_deploys_used":      webDeploysUsed,
		"web_deploys_limit":     deployLimit,
		"ai_credits_balance":    aiCreditsBalance,
		"ai_credits_limit":      aiCreditsLimit,
		"subscription_status":   user.SubscriptionStatus,
		"subscription_plan_id":  user.SubscriptionID,
		"subscription_start_at": user.SubscriptionStartAt,
		"renewal_period_start":  renewStart,
		"renewal_period_end":    renewEnd,
	}

	utils.JSONResponse(w, output, http.StatusOK)
}
