package events

import "github.com/knid/wemob/internal/models"

// contactPayload builds the standard attribute set sent to Resend with
// every event so automations can read user state without a contact join.
// Carries the same keys as contactProperties (the contact upsert), plus
// the contact display name. Pass the cached billingInfo so we don't
// requery the personal org row twice in one SendEvent.
func contactPayload(user *models.User, info billingInfo) map[string]any {
	if user == nil {
		return map[string]any{}
	}
	language := user.Language
	if language == "" {
		language = "en"
	}
	return map[string]any{
		// `wemob_user_id` mirrors the Resend contact property of the same name
		// so automation conditions can filter the same key on either surface.
		// `user_id` is kept as a legacy alias — drop in a future cleanup once
		// no automation references it.
		"wemob_user_id":       user.ID.String(),
		"user_id":             user.ID.String(),
		"name":                user.Name,
		"verified":            user.Verified,
		"provider":            user.Provider,
		"subscription_status": info.Status,
		"subscription_id":     info.SubID,
		"trial_used":          info.TrialUsed,
		"language":            language,
	}
}
