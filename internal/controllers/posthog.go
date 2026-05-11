package controllers

import (
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/posthog/posthog-go"
)

// billingCacheTTL bounds how stale the auto-injected plan / subscription_status
// can be on events. Short enough that a plan change propagates within a few
// minutes even on tabs that don't trigger a billing webhook reload; long enough
// that the personal-org + subscription lookups don't fire on every hot event.
const billingCacheTTL = 5 * time.Minute

// identifyCacheTTL bounds how often EnsureIdentified re-sends $identify for the
// same user within a single process. Replaces the prior lifetime-of-process
// cache so person properties (subscription_status, trial_used, …) don't go
// stale until the next deploy.
const identifyCacheTTL = 30 * time.Minute

type cachedBilling struct {
	orgID, plan, status string
	expiresAt           time.Time
}

// AppEventProps builds a PostHog properties bundle pre-populated with the
// app_id and app_title for `projectID`. When the project resolves, it also
// pins `org_id` to the project's actual organization (not the caller's
// personal org) so events for multi-tenant apps attribute correctly. Callers
// should pass any event-specific props by chaining `.Set(...)`.
func (c *Controller) AppEventProps(projectID string) posthog.Properties {
	props := posthog.NewProperties().Set("app_id", projectID)
	if projectID == "" {
		return props
	}
	if _, err := uuid.Parse(projectID); err != nil {
		return props
	}
	if proj, err := c.DB.GetAppProjectByID(projectID); err == nil && proj != nil {
		props.Set("app_title", proj.Name)
		if proj.OrganizationID != uuid.Nil {
			props.Set("org_id", proj.OrganizationID.String())
		}
	}
	return props
}

// TrackUserEvent sends a named event to PostHog using an already-loaded user.
// Auto-injects the standard attribution bundle so dashboards can slice every
// event consistently without each call site re-passing the same props:
//   - $email (for person reconciliation)
//   - $ip = "" (suppresses PostHog server-side geo-enrichment that would
//     otherwise pin every backend event to the API container's IP)
//   - org_id, plan, subscription_status (sourced from the user's personal org;
//     overridable by passing them explicitly on `props`, e.g. AppEventProps
//     pins org_id to the project's actual org)
func (c *Controller) TrackUserEvent(user *models.User, event constants.EventName, props posthog.Properties) {
	if c.PostHog == nil || user == nil {
		return
	}

	if user.Email != "" {
		if _, set := props["$email"]; !set {
			props.Set("$email", user.Email)
		}
	}

	// Suppress PostHog server-IP attribution. Without this, every backend
	// event ships the API container's egress IP as $ip and PostHog's geo
	// enrichment pins the whole project to one location.
	if _, set := props["$ip"]; !set {
		props.Set("$ip", "")
	}

	billing := c.cachedUserBilling(user)
	if _, set := props["org_id"]; !set && billing.orgID != "" {
		props.Set("org_id", billing.orgID)
	}
	if _, set := props["plan"]; !set && billing.plan != "" {
		props.Set("plan", billing.plan)
	}
	if _, set := props["subscription_status"]; !set && billing.status != "" {
		props.Set("subscription_status", billing.status)
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
// still queryable by task_type / queue. No org_id / plan auto-injection
// because the distinct_id is opaque (could be user, org, deployment, job).
// $ip is suppressed for the same reason as TrackUserEvent.
func (c *Controller) TrackSystemEvent(distinctID string, event constants.EventName, props posthog.Properties) {
	if c.PostHog == nil {
		return
	}
	if distinctID == "" {
		distinctID = "system"
	}
	if _, set := props["$ip"]; !set {
		props.Set("$ip", "")
	}
	c.PostHog.Enqueue(posthog.Capture{
		DistinctId: distinctID,
		Event:      string(event),
		Properties: props,
	})
}

// cachedUserBilling resolves the user's billing snapshot for PostHog property
// auto-injection. Cached per-process with a short TTL so high-volume event
// paths don't hit the DB every call, but plan changes still propagate without
// requiring a deploy. Returns zero values on lookup failure so a transient DB
// error never silences analytics.
func (c *Controller) cachedUserBilling(user *models.User) cachedBilling {
	if user == nil {
		return cachedBilling{}
	}
	uid := user.ID.String()
	now := time.Now()
	if v, ok := c.personalOrgIDs.Load(uid); ok {
		if cached, ok := v.(cachedBilling); ok && cached.expiresAt.After(now) {
			return cached
		}
	}

	org, err := c.DB.GetPersonalOrgByUserID(uid)
	if err != nil || org == nil {
		empty := cachedBilling{expiresAt: now.Add(billingCacheTTL)}
		c.personalOrgIDs.Store(uid, empty)
		return empty
	}

	out := cachedBilling{
		orgID:     org.ID.String(),
		status:    org.SubscriptionStatus,
		expiresAt: now.Add(billingCacheTTL),
	}
	if org.SubscriptionID != uuid.Nil {
		if sub, err := c.DB.GetSubscriptionById(org.SubscriptionID.String()); err == nil && sub != nil {
			out.plan = sub.Name
		}
	}
	c.personalOrgIDs.Store(uid, out)
	return out
}

// invalidateUserBilling drops the cached billing snapshot for a user. Called
// from IdentifyUser so subscription-change webhooks immediately propagate to
// subsequent events (otherwise the cache could hold stale plan/status for up
// to billingCacheTTL after a real upgrade/downgrade/cancel).
func (c *Controller) invalidateUserBilling(user *models.User) {
	if user == nil {
		return
	}
	c.personalOrgIDs.Delete(user.ID.String())
}

// EnsureIdentified sends $identify if the user hasn't been identified within
// the last identifyCacheTTL. Bounded TTL (not lifetime-of-process) ensures
// person properties refresh on long-running API containers between deploys.
func (c *Controller) EnsureIdentified(user *models.User) {
	if c.PostHog == nil || user == nil {
		return
	}
	uid := user.ID.String()
	now := time.Now()
	if v, ok := c.identifiedUsers.Load(uid); ok {
		if when, ok := v.(time.Time); ok && now.Sub(when) < identifyCacheTTL {
			return
		}
	}
	c.identifiedUsers.Store(uid, now)
	c.IdentifyUser(user)
}

// IdentifyUser sets/updates user properties in PostHog. Called on
// login/register/subscription change. Always sends (bypasses the
// EnsureIdentified TTL) and invalidates the per-user billing cache so the
// fresh plan/status flows into the very next TrackUserEvent.
func (c *Controller) IdentifyUser(user *models.User) {
	if c.PostHog == nil || user == nil {
		return
	}
	c.invalidateUserBilling(user)
	c.identifiedUsers.Store(user.ID.String(), time.Now())

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
