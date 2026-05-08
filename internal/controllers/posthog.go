package controllers

import (
	"github.com/knid/wemob/internal/models"
	"github.com/posthog/posthog-go"
)

// TrackUserEvent sends a named event to PostHog using an already-loaded user.
// Avoids the extra DB lookup that TrackEvent performs.
func (c *Controller) TrackUserEvent(user *models.User, event string, props posthog.Properties) {
	if c.PostHog == nil || user == nil {
		return
	}

	if user.Email != "" {
		props.Set("$email", user.Email)
	}

	c.PostHog.Enqueue(posthog.Capture{
		DistinctId: user.ID.String(),
		Event:      event,
		Properties: props,
	})
}

// TrackSystemEvent fires a PostHog event keyed by an opaque distinct ID
// rather than a *models.User. Used by background paths that don't always
// have a user attached — Asynq job lifecycle, cron heartbeats, webhook
// ingestion. distinctID falls back to "system" when empty so events are
// still queryable by task_type / queue.
func (c *Controller) TrackSystemEvent(distinctID, event string, props posthog.Properties) {
	if c.PostHog == nil {
		return
	}
	if distinctID == "" {
		distinctID = "system"
	}
	c.PostHog.Enqueue(posthog.Capture{
		DistinctId: distinctID,
		Event:      event,
		Properties: props,
	})
}

// EnsureIdentified lazily identifies a user in PostHog at most once per process lifetime.
func (c *Controller) EnsureIdentified(user *models.User) {
	if c.PostHog == nil || user == nil {
		return
	}
	uid := user.ID.String()
	if _, loaded := c.identifiedUsers.LoadOrStore(uid, struct{}{}); loaded {
		return // already identified this process
	}
	c.IdentifyUser(user)
}

// IdentifyUser sets/updates user properties in PostHog. Called on login/register/subscription change.
func (c *Controller) IdentifyUser(user *models.User) {
	if c.PostHog == nil || user == nil {
		return
	}
	billing := c.GetUserBilling(user)
	c.PostHog.Enqueue(posthog.Identify{
		DistinctId: user.ID.String(),
		Properties: posthog.NewProperties().
			Set("$email", user.Email).
			Set("$name", user.Name).
			Set("company", user.Company).
			Set("provider", user.Provider).
			Set("subscription_status", billing.SubscriptionStatus).
			Set("subscription_id", billing.SubscriptionID.String()).
			Set("trial_used", billing.TrialUsed).
			Set("setup_finished", user.SetupFinished).
			Set("verified", user.Verified).
			Set("user_type", user.Type).
			Set("created_at", user.CreatedAt),
	})
}
