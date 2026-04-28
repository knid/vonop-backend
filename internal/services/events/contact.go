package events

import "github.com/knid/wemob/internal/models"

// contactPayload builds the standard attribute set sent both to Twenty
// (as Person custom fields) and to Resend (as event payload). Keeping
// this in one place ensures both destinations see the same view of the
// user.
func contactPayload(user *models.User) map[string]any {
	if user == nil {
		return map[string]any{}
	}
	return map[string]any{
		"user_id":             user.ID.String(),
		"name":                user.Name,
		"verified":            user.Verified,
		"provider":            user.Provider,
		"subscription_status": user.SubscriptionStatus,
		"subscription_id":     user.SubscriptionID.String(),
		"trial_used":          user.TrialUsed,
	}
}
