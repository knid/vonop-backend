package utils

import (
	"context"
	"log/slog"
	"time"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	crmServices "github.com/knid/wemob/internal/services/crm"
)

// CheckUsageAlertsJob runs every hour and sends CRM events for usage approaching limits,
// trial lifecycle, and AI credit depletion.
func CheckUsageAlertsJob(db *database.Database, crmService crmServices.CRMService) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		checkUsageAlerts(db, crmService)
	}
}

func checkUsageAlerts(db *database.Database, crmService crmServices.CRMService) {
	var users []models.User
	err := db.DB.NewSelect().
		Model(&users).
		Where("deleted_at IS NULL").
		Where("subscription_status IN (?)", "active", "trial").
		Scan(context.Background())
	if err != nil {
		slog.Error("usage alerts: failed to fetch users", "error", err)
		return
	}

	now := time.Now()

	for _, user := range users {
		// Trial lifecycle alerts
		if user.SubscriptionStatus == "trial" && !user.TrialStartAt.IsZero() {
			trialDuration := 14 * 24 * time.Hour // 14 day trial
			trialEnd := user.TrialStartAt.Add(trialDuration)
			elapsed := now.Sub(user.TrialStartAt)

			// Trial halfway (around day 7)
			if elapsed >= 7*24*time.Hour && elapsed < 8*24*time.Hour {
				go crmService.SendEvent(&user, constants.BREVO_EVENT_TRIAL_HALFWAY, map[string]any{
					"days_remaining": int(trialEnd.Sub(now).Hours() / 24),
				})
			}

			// Trial ending soon (3 days left)
			daysLeft := trialEnd.Sub(now).Hours() / 24
			if daysLeft <= 3 && daysLeft > 2 {
				go crmService.SendEvent(&user, constants.BREVO_EVENT_TRIAL_ENDING_SOON, map[string]any{
					"days_remaining": int(daysLeft),
				})
			}
		}

		// AI credits alerts
		balance, err := db.GetAICreditsBalance(user.ID.String())
		if err != nil {
			continue
		}

		limits, err := db.GetStandartUserLimitsByUser(&user)
		if err != nil || limits.MonthlyAICredits == 0 {
			continue
		}

		usagePercent := float64(limits.MonthlyAICredits-balance.Balance) / float64(limits.MonthlyAICredits) * 100

		if usagePercent >= 80 && usagePercent < 90 {
			go crmService.SendEvent(&user, constants.BREVO_EVENT_AI_CREDITS_LOW, map[string]any{
				"credits_remaining": balance.Balance,
				"credits_total":     limits.MonthlyAICredits,
				"usage_percent":     int(usagePercent),
			})
		}

		if balance.Balance <= 0 {
			go crmService.SendEvent(&user, constants.BREVO_EVENT_AI_CREDITS_DEPLETED, map[string]any{
				"credits_total": limits.MonthlyAICredits,
			})
		}
	}
}
