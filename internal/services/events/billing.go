package events

import (
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
)

// billingInfo is the resolved per-user billing state used to populate both
// the Resend contact properties and the event payload. Cached per SendEvent
// call so a single hot-path doesn't query GetPersonalOrgByUserID twice.
type billingInfo struct {
	Status    string
	SubID     string
	TrialUsed bool
}

// resolveBillingInfo loads the user's billing snapshot from their personal
// organization (Plan A — org-billed). Empty values are returned when the
// user/db are nil or the org cannot be resolved.
func resolveBillingInfo(db *database.Database, user *models.User) billingInfo {
	if db == nil || user == nil {
		return billingInfo{SubID: uuid.Nil.String()}
	}
	org, err := db.GetPersonalOrgByUserID(user.ID.String())
	if err != nil || org == nil {
		return billingInfo{SubID: uuid.Nil.String()}
	}
	return billingInfo{
		Status:    org.SubscriptionStatus,
		SubID:     org.SubscriptionID.String(),
		TrialUsed: org.TrialUsed,
	}
}
