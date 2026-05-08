package events

import (
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
)

// billingSnapshot returns the user's billing status/subscriptionID/trialUsed
// from their personal organization (Plan A — org-billed). Empty values are
// returned when the org or its subscription cannot be resolved; callers
// surface them as empty fields in CRM/event payloads.
func billingSnapshot(db *database.Database, user *models.User) (status, subID string, trialUsed bool) {
	if db == nil || user == nil {
		return "", uuid.Nil.String(), false
	}
	org, err := db.GetPersonalOrgByUserID(user.ID.String())
	if err != nil || org == nil {
		return "", uuid.Nil.String(), false
	}
	return org.SubscriptionStatus, org.SubscriptionID.String(), org.TrialUsed
}
