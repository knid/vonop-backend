package views

import (
	"log/slog"
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

// requireActiveSubscription checks that the user's personal org has an
// active subscription. Returns true if the request should be blocked
// (response already written).
//
// Reads SubscriptionStatus from the user's personal Organization (Plan 5).
// The user is still passed so the lookup can resolve via GetPersonalOrgByUserID.
func requireActiveSubscription(w http.ResponseWriter, c *controllers.Controller, user *models.User) bool {
	org, err := c.DB.GetPersonalOrgByUserID(user.ID.String())
	if err != nil || org == nil {
		slog.Error("requireActiveSubscription: failed to load personal org", "error", err, "user_id", user.ID)
		utils.JSONError(w, constants.ErrSubscriptionRequired, "active subscription required", http.StatusForbidden, nil)
		return true
	}
	switch org.SubscriptionStatus {
	// F3: 'trial' was historically whitelisted but no Trial plan row was
	// seeded and Stripe trials aren't currently configured; treating it as
	// active-equivalent created a state we couldn't actually reach. Removed.
	case "active", "free":
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
	orgID, err := v.Controller.RequireOrgIDForUser(user, nil)
	if err != nil {
		utils.JSONInternalError(w, err)
		return true
	}
	balance, err := v.Controller.GetOrCreateCreditsBalance(user, orgID)
	if err != nil {
		utils.JSONInternalError(w, err)
		return true
	}
	if balance.Balance < 1 {
		var billingURL string
		if orgRec, orgErr := v.Controller.DB.GetOrganizationByID(orgID.String()); orgErr == nil {
			billingURL = v.Controller.BillingURLForOrg(orgRec)
		} else {
			billingURL = v.Controller.BillingURLForUser(user)
		}
		go v.Controller.EventService.SendEvent(user, constants.EVENT_AI_CREDITS_DEPLETED, map[string]any{
			"balance":     balance.Balance,
			"billing_url": billingURL,
		})
		go v.Controller.TrackUserEvent(user, constants.EVENT_AI_CREDITS_DEPLETED, posthog.NewProperties().Set("balance", balance.Balance))

		planName := ""
		monthlyLimit := 0
		if limits, pn, limitsErr := v.Controller.GetUserLimitsWithPlan(user); limitsErr == nil {
			monthlyLimit = limits.MonthlyAICredits
			planName = pn
		}
		// Subscription billing period anchors on the personal org (Plan 5).
		var subsStart time.Time
		if org, orgErr := v.Controller.DB.GetPersonalOrgByUserID(user.ID.String()); orgErr == nil && org != nil {
			subsStart = org.SubscriptionStartAt
		}
		_, renewEnd := utils.BillingPeriod(subsStart, time.Now())

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
