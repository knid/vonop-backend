package views

import (
	"net/http"

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
	utils.JSONCodedErrorResponse(w, "SUBSCRIPTION_REQUIRED", "active subscription required", http.StatusForbidden, nil)
	return true
}

// checkMinimumCredits verifies the user has at least 1 AI credit.
// Returns true if blocked (response already written).
func (v *HttpViews) checkMinimumCredits(w http.ResponseWriter, user *models.User) bool {
	balance, err := v.Controller.GetOrCreateCreditsBalance(user)
	if err != nil {
		utils.JSONErrorResponse(w, "failed to check credits", http.StatusInternalServerError)
		return true
	}
	if balance.Balance < 1 {
		go v.Controller.CRMService.SendEvent(user, constants.BREVO_EVENT_AI_CREDITS_DEPLETED, map[string]any{"balance": balance.Balance})
		go v.Controller.TrackUserEvent(user, "ai_credits_depleted", posthog.NewProperties().Set("balance", balance.Balance))
		utils.JSONCodedErrorResponse(w, "AI_CREDITS_DEPLETED",
			"insufficient AI credits: have 0, need at least 1",
			http.StatusPaymentRequired, map[string]any{
				"credits_required": 1,
				"action":           "ai_generation",
			})
		return true
	}
	return false
}

