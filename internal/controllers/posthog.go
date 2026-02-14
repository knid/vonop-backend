package controllers

import (
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/posthog/posthog-go"
)

// TrackEvent sends a named event to PostHog. No-op if PostHog is nil.
func (c *Controller) TrackEvent(userID uuid.UUID, event string, props posthog.Properties) {
	if c.PostHog == nil {
		return
	}
	c.PostHog.Enqueue(posthog.Capture{
		DistinctId: userID.String(),
		Event:      event,
		Properties: props,
	})
}

// IdentifyUser sets/updates user properties in PostHog. Called on login/register/subscription change.
func (c *Controller) IdentifyUser(user *models.User) {
	if c.PostHog == nil || user == nil {
		return
	}
	c.PostHog.Enqueue(posthog.Identify{
		DistinctId: user.ID.String(),
		Properties: posthog.NewProperties().
			Set("$email", user.Email).
			Set("$name", user.Name).
			Set("company", user.Company).
			Set("provider", user.Provider).
			Set("subscription_status", user.SubscriptionStatus).
			Set("subscription_id", user.SubscriptionID.String()).
			Set("trial_used", user.TrialUsed).
			Set("setup_finished", user.SetupFinished).
			Set("verified", user.Verified).
			Set("user_type", user.Type).
			Set("created_at", user.CreatedAt),
	})
}
