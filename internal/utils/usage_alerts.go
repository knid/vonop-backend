package utils

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/events"
	"github.com/uptrace/bun"
)

// alertRecord tracks the last alert sent to a user so the hourly job
// doesn't re-send the same event every tick.
type alertRecord struct {
	level   string // "depleted" or "low"
	sentAt  time.Time
}

var (
	alertHistory   = make(map[string]alertRecord) // key: user ID
	alertHistoryMu sync.Mutex
)

const alertCooldown = 24 * time.Hour

// shouldAlert returns true if the user hasn't been alerted at this level
// within the cooldown window, or if the level worsened.
func shouldAlert(userID, level string) bool {
	alertHistoryMu.Lock()
	defer alertHistoryMu.Unlock()

	prev, ok := alertHistory[userID]
	if !ok || prev.level != level || time.Since(prev.sentAt) >= alertCooldown {
		alertHistory[userID] = alertRecord{level: level, sentAt: time.Now()}
		return true
	}
	return false
}

// clearAlert removes a user from the alert history when their balance
// recovers, so they'll be alerted again if they deplete again later.
func clearAlert(userID string) {
	alertHistoryMu.Lock()
	delete(alertHistory, userID)
	alertHistoryMu.Unlock()
}

// CheckUsageAlertsJob runs every hour and fires events for usage approaching limits
// and AI credit depletion.
func CheckUsageAlertsJob(db *database.Database, eventService events.EventService) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		checkUsageAlerts(db, eventService)
	}
}

func checkUsageAlerts(db *database.Database, eventService events.EventService) {
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
		balance, err := db.GetAICreditsBalance(user.ID.String())
		if err != nil {
			// Users without a balance row yet have nothing to alert on; skip quietly.
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			slog.Warn("usage alerts: failed to fetch AI credits balance", "error", err, "user_id", user.ID.String())
			continue
		}

		limits, err := db.GetStandardUserLimitsByUser(&user)
		if err != nil || limits.MonthlyAICredits == 0 {
			continue
		}

		remainingPercent := float64(balance.Balance) / float64(limits.MonthlyAICredits) * 100

		if balance.Balance <= 0 || remainingPercent <= 10 {
			if shouldAlert(user.ID.String(), "depleted") {
				go eventService.SendEvent(&user, constants.EVENT_AI_CREDITS_DEPLETED, map[string]any{
					"credits_total":     limits.MonthlyAICredits,
					"credits_remaining": balance.Balance,
				})
			}
		} else if remainingPercent <= 20 {
			if shouldAlert(user.ID.String(), "low") {
				go eventService.SendEvent(&user, constants.EVENT_AI_CREDITS_LOW, map[string]any{
					"credits_remaining": balance.Balance,
					"credits_total":     limits.MonthlyAICredits,
					"usage_percent":     int(100 - remainingPercent),
				})
			}
		} else {
			// Balance recovered — clear history so future depletion triggers a fresh alert.
			clearAlert(user.ID.String())
		}
	}
}
