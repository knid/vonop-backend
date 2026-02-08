package views

import (
	"net/http"

	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/utils"
)

type HttpViews struct {
	Controller *controllers.Controller
}

// requireActiveSubscription checks that the user has an active or trial subscription.
// Returns true if the request should be blocked (response already written).
func requireActiveSubscription(w http.ResponseWriter, user *models.User) bool {
	if user.SubscriptionStatus == "active" || user.SubscriptionStatus == "trial" {
		return false
	}
	utils.JSONCodedErrorResponse(w, "SUBSCRIPTION_REQUIRED", "active subscription required", http.StatusForbidden, nil)
	return true
}

// deductAICredits attempts to deduct AI credits for the given action.
// Returns true if the request should be blocked (insufficient credits, response already written).
func (v *HttpViews) deductAICredits(w http.ResponseWriter, user *models.User, cost int, action string, reference string) bool {
	err := v.Controller.DeductAICredits(user, cost, action, reference)
	if err != nil {
		utils.JSONCodedErrorResponse(w, "AI_CREDITS_DEPLETED", err.Error(), http.StatusPaymentRequired, map[string]any{
			"credits_required": cost,
			"action":           action,
		})
		return true
	}
	return false
}
