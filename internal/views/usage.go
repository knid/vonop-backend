package views

import (
	"net/http"
	"time"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) GetMontlyUsage(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	projects, err := v.Controller.GetProjects(user)
	if err != nil {
		utils.JSONInternalError(w, err)
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

	// Anchor billing window on the personal org's SubscriptionStartAt
	// (Plan A made billing org-scoped). Zero time falls through to
	// BillingPeriod's "first of month" fallback.
	var subsStart time.Time
	if org, orgErr := v.Controller.DB.GetPersonalOrgByUserID(user.ID.String()); orgErr == nil && org != nil {
		subsStart = org.SubscriptionStartAt
	}
	renewStart, renewEnd := utils.BillingPeriod(subsStart, time.Now())

	notificationCount := 0
	for _, notification := range notifications {
		if notification.CreatedAt.After(renewStart) && notification.CreatedAt.Before(renewEnd) {
			notificationCount++
		}
	}

	// Fetch plan limits to include in response
	var notificationLimit, webAppLimit, mobileAppLimit, aiCreditsLimit int
	limits, limitsErr := v.Controller.GetUserLimits(user)
	if limitsErr == nil {
		notificationLimit = limits.MontlyNotificationCount
		webAppLimit = limits.MaxWebApps
		mobileAppLimit = limits.MaxMobileApps
		aiCreditsLimit = limits.MonthlyAICredits
	}

	// App project counts by type (total, not period-based). §B Phase 4:
	// org-scoped — limits are billed at the org, so usage is too. Falls back
	// to user-scoped on org lookup failure so the view stays renderable.
	var webAppsUsed, mobileAppsUsed int
	if org, orgErr := v.Controller.DB.GetPersonalOrgByUserID(user.ID.String()); orgErr == nil && org != nil {
		webAppsUsed, _ = v.Controller.DB.CountAppProjectsByOrgIDAndType(org.ID.String(), "")
		mobileAppsUsed, _ = v.Controller.DB.CountAppProjectsByOrgIDAndType(org.ID.String(), "mobile")
	} else {
		webAppsUsed, _ = v.Controller.DB.CountAppProjectsByUserIDAndType(user.ID.String(), "")
		mobileAppsUsed, _ = v.Controller.DB.CountAppProjectsByUserIDAndType(user.ID.String(), "mobile")
	}

	// AI credits balance — read against the user's personal org.
	var aiCreditsBalance int
	if orgID, orgErr := v.Controller.RequireOrgIDForUser(user, nil); orgErr == nil {
		if balance, balErr := v.Controller.GetOrCreateCreditsBalance(user, orgID); balErr == nil {
			aiCreditsBalance = balance.Balance
		}
	}

	billing := v.Controller.GetUserBilling(user)
	output := map[string]any{
		"notifications_used":    notificationCount,
		"notification_limit":    notificationLimit,
		"web_apps_used":         webAppsUsed,
		"web_apps_limit":        webAppLimit,
		"mobile_apps_used":      mobileAppsUsed,
		"mobile_apps_limit":     mobileAppLimit,
		"ai_credits_balance":    aiCreditsBalance,
		"ai_credits_limit":      aiCreditsLimit,
		"subscription_status":   billing.SubscriptionStatus,
		"subscription_plan_id":  billing.SubscriptionID,
		"subscription_start_at": billing.SubscriptionStartAt,
		"renewal_period_start":  renewStart,
		"renewal_period_end":    renewEnd,
	}

	utils.JSONResponse(w, output, http.StatusOK)
}
