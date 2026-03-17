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

// TrackStorefrontEvent sends an anonymous storefront event to PostHog keyed by store ID.
// Used for public storefront actions (cart, checkout) where there is no authenticated user.
func (c *Controller) TrackStorefrontEvent(storeID string, event string, props posthog.Properties) {
	if c.PostHog == nil {
		return
	}

	// Resolve store owner for attribution (skip user lookup — store_owner_id is sufficient)
	store, err := c.DB.GetStoreByID(storeID)
	if err == nil && store != nil {
		props.Set("store_owner_id", store.UserID.String())
	}

	c.PostHog.Enqueue(posthog.Capture{
		DistinctId: "store:" + storeID,
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
