package utils

import (
	"context"
	"log/slog"
	"time"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	crmServices "github.com/knid/wemob/internal/services/crm"
	"github.com/uptrace/bun"
)

// CheckUsageAlertsJob runs every hour and sends CRM events for usage approaching limits
// and AI credit depletion.
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
		Where("subscription_status IN (?)", bun.In([]string{"active", "free", "trial"})).
		Scan(context.Background())
	if err != nil {
		slog.Error("usage alerts: failed to fetch users", "error", err)
		return
	}

	for _, user := range users {
		// AI credits alerts
		balance, err := db.GetAICreditsBalance(user.ID.String())
		if err != nil {
			slog.Warn("usage alerts: failed to fetch AI credits balance", "error", err, "user_id", user.ID.String())
			continue
		}

		limits, err := db.GetStandartUserLimitsByUser(&user)
		if err != nil || limits.MonthlyAICredits == 0 {
			continue
		}

		usagePercent := float64(limits.MonthlyAICredits-balance.Balance) / float64(limits.MonthlyAICredits) * 100

		// CRM events are fire-and-forget; errors are handled internally by the CRM service.
		if usagePercent >= 90 || balance.Balance <= 0 {
			go crmService.SendEvent(&user, constants.BREVO_EVENT_AI_CREDITS_DEPLETED, map[string]any{
				"credits_total": limits.MonthlyAICredits,
			})
		} else if usagePercent >= 80 {
			go crmService.SendEvent(&user, constants.BREVO_EVENT_AI_CREDITS_LOW, map[string]any{
				"credits_remaining": balance.Balance,
				"credits_total":     limits.MonthlyAICredits,
				"usage_percent":     int(usagePercent),
			})
		}
	}
}
