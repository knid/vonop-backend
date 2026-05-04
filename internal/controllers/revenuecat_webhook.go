package controllers

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/models"
)

// RCEventV2 is the subset of RevenueCat v2 webhook payload fields we consume.
// See https://www.revenuecat.com/docs/integrations/webhooks/event-flows for
// the full schema. Fields we don't use are left out — JSON unmarshal silently
// drops unknown keys, which is what we want for forward-compat as RC adds
// new fields.
type RCEventV2 struct {
	APIVersion string         `json:"api_version,omitempty"`
	Event      RCEventV2Event `json:"event"`
}

// RCEventV2Event mirrors RC's `event` object.
type RCEventV2Event struct {
	Type             string  `json:"type"`
	ID               string  `json:"id,omitempty"`
	AppUserID        string  `json:"app_user_id"`
	OriginalAppUserID string `json:"original_app_user_id,omitempty"`
	AliasedAppUserID string  `json:"aliases,omitempty"` // RC uses different shapes for SUBSCRIBER_ALIAS / TRANSFER

	// AppID is RC's project id (we mapped via cfg.RevenueCatProjectID at provision time).
	AppID string `json:"app_id,omitempty"`

	// Sandbox: true for app-store / play-store sandbox purchases
	Environment string `json:"environment,omitempty"` // "PRODUCTION" | "SANDBOX"

	ProductID     string `json:"product_id,omitempty"`
	NewProductID  string `json:"new_product_id,omitempty"`
	EntitlementID string `json:"entitlement_id,omitempty"`
	EntitlementIDs []string `json:"entitlement_ids,omitempty"`

	Store string `json:"store,omitempty"` // "APP_STORE" | "PLAY_STORE" | "STRIPE" | ...

	PurchasedAtMs   int64 `json:"purchased_at_ms,omitempty"`
	ExpirationAtMs  int64 `json:"expiration_at_ms,omitempty"`
	CancelReason    string `json:"cancel_reason,omitempty"`
	GracePeriodExpirationAtMs int64 `json:"grace_period_expiration_at_ms,omitempty"`

	CountryCode string  `json:"country_code,omitempty"`
	Currency    string  `json:"currency,omitempty"`
	Price       float64 `json:"price,omitempty"`         // RC sends as a decimal in `price_in_purchased_currency`
	PriceUSD    float64 `json:"price_in_usd,omitempty"`
	PriceInCurrency float64 `json:"price_in_purchased_currency,omitempty"`

	IsTrialPeriod bool `json:"is_trial_period,omitempty"`
	PeriodType    string `json:"period_type,omitempty"` // "TRIAL" | "INTRO" | "NORMAL"

	// SUBSCRIBER_ALIAS / TRANSFER
	TransferredFrom []string `json:"transferred_from,omitempty"`
	TransferredTo   []string `json:"transferred_to,omitempty"`
}

// RC event types we route on. Any value outside this set is logged + 200ed.
const (
	RCEventInitialPurchase     = "INITIAL_PURCHASE"
	RCEventRenewal             = "RENEWAL"
	RCEventProductChange       = "PRODUCT_CHANGE"
	RCEventCancellation        = "CANCELLATION"
	RCEventUncancellation      = "UNCANCELLATION"
	RCEventNonRenewingPurchase = "NON_RENEWING_PURCHASE"
	RCEventExpiration          = "EXPIRATION"
	RCEventBillingIssue        = "BILLING_ISSUE"
	RCEventSubscriberAlias     = "SUBSCRIBER_ALIAS"
	RCEventTransfer            = "TRANSFER"
)

// HandleRCEvent dispatches a parsed RC v2 webhook event. Always returns
// nil for "unknown type" — the webhook view 200s those so RC stops
// retrying. Returns a non-nil error only when the database write fails
// AND we want RC to retry (= actual infra failure, not a missing project).
func (c *Controller) HandleRCEvent(ctx context.Context, evt RCEventV2) error {
	t := evt.Event.Type
	switch t {
	case RCEventInitialPurchase, RCEventRenewal, RCEventProductChange, RCEventUncancellation:
		return c.upsertMobileSubscriptionFromEvent(ctx, evt, models.MobileSubscriptionStatusActive)

	case RCEventNonRenewingPurchase:
		return c.upsertMobileSubscriptionFromEvent(ctx, evt, models.MobileSubscriptionStatusNonRenewing)

	case RCEventCancellation:
		return c.markRCSubscriptionStatus(ctx, evt, models.MobileSubscriptionStatusCanceled, true)

	case RCEventExpiration:
		return c.markRCSubscriptionStatus(ctx, evt, models.MobileSubscriptionStatusExpired, true)

	case RCEventBillingIssue:
		return c.markRCSubscriptionStatus(ctx, evt, models.MobileSubscriptionStatusBillingIssue, false)

	case RCEventSubscriberAlias, RCEventTransfer:
		return c.handleRCAliasOrTransfer(ctx, evt)

	default:
		slog.Info("revenuecat: unhandled event type (acked)",
			"type", t,
			"app_id", evt.Event.AppID,
			"app_user_id", evt.Event.AppUserID,
		)
		return nil
	}
}

// UpsertMobileSubscription is the public dispatcher used by the webhook view
// when it just wants "do whatever this event says" without picking the
// internal status itself. Mostly equivalent to HandleRCEvent — exposed
// separately because the spec named both.
func (c *Controller) UpsertMobileSubscription(ctx context.Context, evt RCEventV2) error {
	return c.HandleRCEvent(ctx, evt)
}

// upsertMobileSubscriptionFromEvent is the common write path for events that
// produce/update an active row.
func (c *Controller) upsertMobileSubscriptionFromEvent(ctx context.Context, evt RCEventV2, status string) error {
	cfg, project, err := c.resolveRCProject(ctx, evt.Event.AppID)
	if err != nil {
		return err
	}
	if cfg == nil || project == nil {
		// We don't recognise this RC project — ack and move on. Could be a
		// dev tester's project, an admin-deleted app, or a stale webhook
		// pointed at an old environment.
		slog.Info("revenuecat: event for unknown project (acked)",
			"app_id", evt.Event.AppID,
			"type", evt.Event.Type,
		)
		return nil
	}

	row := mobileSubscriptionFromEvent(evt, cfg.ProjectID, project.OrganizationID, status)
	if row == nil {
		// Event lacked the bare-minimum fields (app_user_id, product_id) — log + ack.
		slog.Warn("revenuecat: event missing required fields (acked)",
			"type", evt.Event.Type,
			"app_id", evt.Event.AppID,
			"app_user_id", evt.Event.AppUserID,
			"product_id", evt.Event.ProductID,
		)
		return nil
	}

	if _, err := c.DB.UpsertMobileSubscription(ctx, row); err != nil {
		return fmt.Errorf("revenuecat: upsert subscription: %w", err)
	}
	slog.Info("revenuecat: subscription upserted",
		"type", evt.Event.Type,
		"project_id", cfg.ProjectID,
		"rc_app_user_id", row.RCAppUserID,
		"product_id", row.ProductID,
		"status", status,
	)
	return nil
}

// markRCSubscriptionStatus is the path for events that flip an existing row's
// status without changing the period numbers. setCanceledAt = true stamps the
// canceled_at column (used by churn-30d math).
func (c *Controller) markRCSubscriptionStatus(ctx context.Context, evt RCEventV2, status string, setCanceledAt bool) error {
	cfg, _, err := c.resolveRCProject(ctx, evt.Event.AppID)
	if err != nil {
		return err
	}
	if cfg == nil {
		slog.Info("revenuecat: status event for unknown project (acked)",
			"app_id", evt.Event.AppID, "type", evt.Event.Type)
		return nil
	}

	productID := normaliseRCProductID(evt)
	if evt.Event.AppUserID == "" || productID == "" {
		slog.Warn("revenuecat: status event missing keys (acked)",
			"type", evt.Event.Type, "app_user_id", evt.Event.AppUserID, "product_id", productID)
		return nil
	}

	var canceledAt *time.Time
	if setCanceledAt {
		now := time.Now().UTC()
		canceledAt = &now
	}

	n, err := c.DB.MarkMobileSubscriptionStatus(ctx,
		cfg.ProjectID.String(), evt.Event.AppUserID, productID, status, canceledAt)
	if err != nil {
		return fmt.Errorf("revenuecat: mark status: %w", err)
	}
	if n == 0 {
		// Nothing to update — RC sent a CANCELLATION for a row we never
		// saw the INITIAL_PURCHASE for (out-of-order delivery). Log so we
		// can spot patterns in BetterStack; don't 500.
		slog.Info("revenuecat: status event with no matching row",
			"type", evt.Event.Type, "project_id", cfg.ProjectID,
			"rc_app_user_id", evt.Event.AppUserID, "product_id", productID)
	}
	return nil
}

// handleRCAliasOrTransfer joins or reassigns subscription rows when RC tells
// us two app_user_ids should be considered the same person, or that all of
// person A's subscriptions should now belong to person B.
//
// Conservative implementation: we just rewrite rc_app_user_id on the matching
// rows. Identity collapse / merge is one-directional — we don't dedupe rows
// that may now collide on the unique index; the upsert path will resolve any
// duplicates the next time RC sends an event for the canonical id.
func (c *Controller) handleRCAliasOrTransfer(ctx context.Context, evt RCEventV2) error {
	cfg, _, err := c.resolveRCProject(ctx, evt.Event.AppID)
	if err != nil {
		return err
	}
	if cfg == nil {
		return nil
	}
	from := evt.Event.TransferredFrom
	to := evt.Event.TransferredTo

	// SUBSCRIBER_ALIAS uses original_app_user_id + app_user_id pair.
	if len(from) == 0 && evt.Event.OriginalAppUserID != "" {
		from = []string{evt.Event.OriginalAppUserID}
	}
	if len(to) == 0 && evt.Event.AppUserID != "" {
		to = []string{evt.Event.AppUserID}
	}
	if len(from) == 0 || len(to) == 0 {
		slog.Warn("revenuecat: alias/transfer missing from/to (acked)",
			"type", evt.Event.Type, "app_id", evt.Event.AppID)
		return nil
	}
	target := to[0]
	for _, src := range from {
		if src == target {
			continue
		}
		_, uerr := c.DB.DB.NewUpdate().
			Model((*models.MobileSubscription)(nil)).
			Set("rc_app_user_id = ?", target).
			Set("updated_at = now()").
			Where("app_project_id = ?", cfg.ProjectID).
			Where("rc_app_user_id = ?", src).
			Exec(ctx)
		if uerr != nil {
			// Treat as best-effort; ack so RC doesn't loop.
			slog.Warn("revenuecat: alias/transfer rewrite failed (acked)",
				"err", uerr, "from", src, "to", target, "project_id", cfg.ProjectID)
		}
	}
	slog.Info("revenuecat: alias/transfer applied",
		"type", evt.Event.Type, "from_count", len(from), "to", target, "project_id", cfg.ProjectID)
	return nil
}

// resolveRCProject finds the MobileAppConfig and parent AppProject from an
// RC project id. Returns (nil, nil, nil) for unrecognised ids — caller is
// expected to ack-and-move-on.
func (c *Controller) resolveRCProject(ctx context.Context, rcProjectID string) (*models.MobileAppConfig, *models.AppProject, error) {
	if rcProjectID == "" {
		return nil, nil, nil
	}
	cfg, err := c.DB.GetMobileAppConfigByRCProject(ctx, rcProjectID)
	if err != nil {
		// "no rows" arrives as sql.ErrNoRows from Bun; treat as absent.
		if isNoRowsErr(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	if cfg == nil {
		return nil, nil, nil
	}
	project, err := c.DB.GetAppProjectByID(cfg.ProjectID.String())
	if err != nil || project == nil {
		// Config exists but project deleted — log + ack.
		slog.Warn("revenuecat: config orphan (project missing)",
			"project_id", cfg.ProjectID, "rc_project_id", rcProjectID)
		return cfg, nil, nil
	}
	return cfg, project, nil
}

func isNoRowsErr(err error) bool {
	if err == nil {
		return false
	}
	// Bun surfaces no-rows as the underlying sql.ErrNoRows.
	return strings.Contains(err.Error(), "no rows")
}

// normaliseRCProductID picks ProductID, falling back to NewProductID for
// PRODUCT_CHANGE events where the canonical id moved.
func normaliseRCProductID(evt RCEventV2) string {
	if evt.Event.ProductID != "" {
		return evt.Event.ProductID
	}
	return evt.Event.NewProductID
}

// mobileSubscriptionFromEvent maps an RC v2 event to a MobileSubscription
// row. Returns nil when the event lacks required keys.
func mobileSubscriptionFromEvent(evt RCEventV2, projectID, organizationID uuid.UUID, status string) *models.MobileSubscription {
	productID := normaliseRCProductID(evt)
	if evt.Event.AppUserID == "" || productID == "" {
		return nil
	}

	row := &models.MobileSubscription{
		AppProjectID:   projectID,
		OrganizationID: organizationID,
		RCAppUserID:    evt.Event.AppUserID,
		ProductID:      productID,
		EntitlementID:  pickEntitlementID(evt),
		Status:         status,
		Store:          normaliseRCStore(evt.Event.Store),
		CountryCode:    evt.Event.CountryCode,
		Currency:       evt.Event.Currency,
		PriceCents:     priceCentsFromEvent(evt),
		IsTrial:        evt.Event.IsTrialPeriod || strings.EqualFold(evt.Event.PeriodType, "TRIAL"),
	}

	if evt.Event.PurchasedAtMs > 0 {
		row.PeriodStart = time.UnixMilli(evt.Event.PurchasedAtMs).UTC()
	}
	if evt.Event.ExpirationAtMs > 0 {
		row.PeriodEnd = time.UnixMilli(evt.Event.ExpirationAtMs).UTC()
	}
	if evt.Event.Type == RCEventNonRenewingPurchase {
		row.IsLifetime = true
	}
	return row
}

func pickEntitlementID(evt RCEventV2) string {
	if evt.Event.EntitlementID != "" {
		return evt.Event.EntitlementID
	}
	if len(evt.Event.EntitlementIDs) > 0 {
		return evt.Event.EntitlementIDs[0]
	}
	return ""
}

// priceCentsFromEvent prefers price_in_purchased_currency, falls back to
// price_in_usd. Both are decimals in RC's payload; we round to cents.
func priceCentsFromEvent(evt RCEventV2) int64 {
	d := evt.Event.PriceInCurrency
	if d == 0 {
		d = evt.Event.PriceUSD
	}
	if d == 0 {
		d = evt.Event.Price
	}
	if d <= 0 {
		return 0
	}
	return int64(d*100 + 0.5)
}

// normaliseRCStore maps RC's UPPER_SNAKE_CASE store identifiers to our
// lower_snake_case enum.
func normaliseRCStore(s string) string {
	switch strings.ToUpper(s) {
	case "APP_STORE":
		return "app_store"
	case "PLAY_STORE", "MAC_APP_STORE":
		return "play_store"
	case "STRIPE":
		return "stripe"
	case "AMAZON":
		return "amazon"
	case "PROMOTIONAL":
		return "promotional"
	case "":
		return ""
	default:
		return strings.ToLower(s)
	}
}

// ErrRCWebhookUnauthenticated is returned by VerifyRCWebhookAuth when the
// shared-secret check fails. The webhook view maps this to a 401.
var ErrRCWebhookUnauthenticated = errors.New("revenuecat: webhook auth failed")

// VerifyRCWebhookAuth performs constant-time comparison of the bearer token
// against REVENUECAT_WEBHOOK_AUTH_TOKEN from the secret manager. Returns
// ErrRCWebhookUnauthenticated on mismatch / unset secret.
func (c *Controller) VerifyRCWebhookAuth(provided string) error {
	expected := strings.TrimSpace(c.SecretManager.GetSecret("REVENUECAT_WEBHOOK_AUTH_TOKEN"))
	if expected == "" {
		// Refuse to verify when the secret isn't set — better to log + 401
		// than to accept everything in dev.
		return ErrRCWebhookUnauthenticated
	}
	got := strings.TrimSpace(strings.TrimPrefix(provided, "Bearer "))
	if got == "" {
		return ErrRCWebhookUnauthenticated
	}
	if !constantTimeStringsEqual(got, expected) {
		return ErrRCWebhookUnauthenticated
	}
	return nil
}

// constantTimeStringsEqual is a tiny wrapper around subtle.ConstantTimeCompare
// for two strings; returns false unconditionally on length mismatch.
func constantTimeStringsEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
