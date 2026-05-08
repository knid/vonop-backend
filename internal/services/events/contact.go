package events

import (
	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
)

// contactPayload builds the standard attribute set sent both to Twenty
// (as Person custom fields) and to Resend (as event payload). Keeping
// this in one place ensures both destinations see the same view of the
// user. Billing fields are sourced from the user's personal org
// (Plan A — org-billed; the matching User columns are deprecated).
func contactPayload(db *database.Database, user *models.User) map[string]any {
	if user == nil {
		return map[string]any{}
	}
	status, subID, trial := billingSnapshot(db, user)
	return map[string]any{
		"user_id":             user.ID.String(),
		"name":                user.Name,
		"verified":            user.Verified,
		"provider":            user.Provider,
		"subscription_status": status,
		"subscription_id":     subID,
		"trial_used":          trial,
	}
}
