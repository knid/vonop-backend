package controllers

import (
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
)

// UserBilling is a read-only snapshot of a user's billing state, sourced from
// their personal organization. Plan A made billing org-scoped — the matching
// columns on User are deprecated. Use this helper anywhere you would have
// reached for user.SubscriptionID / SubscriptionStatus / etc.
type UserBilling struct {
	SubscriptionID      uuid.UUID
	SubscriptionStatus  string
	SubscriptionStartAt time.Time
	TrialStartAt        time.Time
	TrialUsed           bool
	StripeCustomerID    string
	Subscription        *models.StandardUserSubscription
}

// GetUserBilling returns the user's billing snapshot from their personal org.
// Returns zero values if the user has no personal org or the lookup fails —
// callers should treat the zero state as "unknown / not subscribed".
func (c *Controller) GetUserBilling(user *models.User) UserBilling {
	if user == nil {
		return UserBilling{}
	}
	org, err := c.DB.GetPersonalOrgByUserID(user.ID.String())
	if err != nil || org == nil {
		return UserBilling{}
	}
	var sub *models.StandardUserSubscription
	if org.SubscriptionID != uuid.Nil {
		if s, err := c.DB.GetSubscriptionById(org.SubscriptionID.String()); err == nil {
			sub = s
		}
	}
	return UserBilling{
		SubscriptionID:      org.SubscriptionID,
		SubscriptionStatus:  org.SubscriptionStatus,
		SubscriptionStartAt: org.SubscriptionStartAt,
		TrialStartAt:        org.TrialStartAt,
		TrialUsed:           org.TrialUsed,
		StripeCustomerID:    org.StripeCustomerID,
		Subscription:        sub,
	}
}
