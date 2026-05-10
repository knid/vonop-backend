package controllers

import (
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/posthog/posthog-go"
)

// TrackUserEvent sends a named event to PostHog using an already-loaded user.
// Auto-injects $email and org_id (the user's personal org) when those props
// aren't explicitly set by the caller. org_id is critical because billing is
// org-scoped — without it dashboards can't slice MAU/ARPU/conversion per org.
func (c *Controller) TrackUserEvent(user *models.User, event constants.EventName, props posthog.Properties) {
	if c.PostHog == nil || user == nil {
		return
	}

	if user.Email != "" {
		if _, set := props["$email"]; !set {
			props.Set("$email", user.Email)
		}
	}

	if _, set := props["org_id"]; !set {
		if orgID := c.personalOrgIDFor(user); orgID != "" {
			props.Set("org_id", orgID)
		}
	}

	c.PostHog.Enqueue(posthog.Capture{
		DistinctId: user.ID.String(),
		Event:      string(event),
		Properties: props,
	})
}

// TrackSystemEvent fires a PostHog event keyed by an opaque distinct ID
// rather than a *models.User. Used by background paths that don't always
// have a user attached — Asynq job lifecycle, cron heartbeats, webhook
// ingestion. distinctID falls back to "system" when empty so events are
// still queryable by task_type / queue. No org_id auto-injection because
// the distinct_id is opaque (could be user, org, deployment, job, or "system").
func (c *Controller) TrackSystemEvent(distinctID string, event constants.EventName, props posthog.Properties) {
	if c.PostHog == nil {
		return
	}
	if distinctID == "" {
		distinctID = "system"
	}
	c.PostHog.Enqueue(posthog.Capture{
		DistinctId: distinctID,
		Event:      string(event),
		Properties: props,
	})
}

// personalOrgIDFor returns the user's personal org ID for PostHog attribution.
// Cached per-process via sync.Map so high-volume event paths don't hit the DB
// every call. Returns "" on lookup failure (event still emits, just without
// org_id) so a transient DB error never silences analytics.
func (c *Controller) personalOrgIDFor(user *models.User) string {
	if user == nil {
		return ""
	}
	uid := user.ID.String()
	if v, ok := c.personalOrgIDs.Load(uid); ok {
		return v.(string)
	}
	org, err := c.DB.GetPersonalOrgByUserID(uid)
	if err != nil || org == nil {
		// Cache the empty result to avoid hammering the DB for users that
		// somehow don't have a personal org yet (signup race, backfill gap).
		c.personalOrgIDs.Store(uid, "")
		return ""
	}
	orgID := org.ID.String()
	c.personalOrgIDs.Store(uid, orgID)
	return orgID
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
