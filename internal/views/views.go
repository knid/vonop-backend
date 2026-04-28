package views

import (
	"net/http"
	"time"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/utils"
	"github.com/posthog/posthog-go"
)

type HttpViews struct {
	Controller *controllers.Controller
}

// requireActiveSubscription checks that the user has an active subscription.
// Returns true if the request should be blocked (response already written).
func requireActiveSubscription(w http.ResponseWriter, user *models.User) bool {
	switch user.SubscriptionStatus {
	case "active", "free", "trial":
		return false
	}
	utils.JSONError(w, constants.ErrSubscriptionRequired, "active subscription required", http.StatusForbidden, nil)
	return true
}

// checkAnalyticsAccess verifies the user's plan includes analytics.
// Returns true if blocked (response already written).
func (v *HttpViews) checkAnalyticsAccess(w http.ResponseWriter, user *models.User) bool {
	limits, err := v.Controller.GetUserLimits(user)
	if err != nil || !limits.CanUseAnalytics {
		utils.JSONError(w, constants.ErrFeatureNotAvailable,
			"analytics is not available on your plan",
			http.StatusForbidden, nil)
		return true
	}
	return false
}

// checkMinimumCredits verifies the user has at least 1 AI credit.
// Returns true if blocked (response already written).
func (v *HttpViews) checkMinimumCredits(w http.ResponseWriter, user *models.User) bool {
	balance, err := v.Controller.GetOrCreateCreditsBalance(user)
	if err != nil {
		utils.JSONInternalError(w, err)
		return true
	}
	if balance.Balance < 1 {
		go v.Controller.CRMService.SendEvent(user, constants.EVENT_AI_CREDITS_DEPLETED, map[string]any{"balance": balance.Balance})
		go v.Controller.TrackUserEvent(user, "ai_credits_depleted", posthog.NewProperties().Set("balance", balance.Balance))

		planName := ""
		monthlyLimit := 0
		if limits, pn, limitsErr := v.Controller.GetUserLimitsWithPlan(user); limitsErr == nil {
			monthlyLimit = limits.MonthlyAICredits
			planName = pn
		}
		_, renewEnd := utils.BillingPeriod(user.SubscriptionStartAt, time.Now())

		utils.JSONError(w, constants.ErrAICreditsDepleted,
			"insufficient AI credits: have 0, need at least 1",
			http.StatusPaymentRequired, map[string]any{
				"credits_required": 1,
				"action":           "ai_generation",
				"balance":          balance.Balance,
				"used":             monthlyLimit - balance.Balance,
				"limit":            monthlyLimit,
				"renewal_date":     renewEnd.Format(time.RFC3339),
				"plan_name":        planName,
			})
		return true
	}
	return false
}
