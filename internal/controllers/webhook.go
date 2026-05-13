package controllers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/posthog/posthog-go"
	"github.com/stripe/stripe-go/v82"
	checkoutsession "github.com/stripe/stripe-go/v82/checkout/session"
	"github.com/stripe/stripe-go/v82/webhook"
	"github.com/uptrace/bun"
)

// mapStripeStatus maps a Stripe subscription status to our internal status.
// Stripe statuses: active, trialing, past_due, canceled, unpaid, incomplete, incomplete_expired, paused.
func mapStripeStatus(status stripe.SubscriptionStatus) string {
	switch status {
	case "active":
		return "active"
	case "trialing":
		return "trial"
	case "past_due":
		return "past_due"
	case "canceled":
		return "canceled"
	case "unpaid":
		return "unpaid"
	default:
		// incomplete, incomplete_expired, paused, or unknown
		return "inactive"
	}
}

func (c *Controller) HandleWebhook(w http.ResponseWriter, req *http.Request) {
	const MaxBodyBytes = int64(65536)
	req.Body = http.MaxBytesReader(w, req.Body, MaxBodyBytes)
	payload, err := io.ReadAll(req.Body)
	if err != nil {
		slog.Error(fmt.Sprintf("Error reading request body: %v\n", err))
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	event := stripe.Event{}

	if err := json.Unmarshal(payload, &event); err != nil {
		slog.Error(fmt.Sprintf("Webhook error while parsing basic request. %v\n", err))
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	endpointSecret := c.SecretManager.GetSecret("STRIPE_WEBHOOK_SECRET")
	if endpointSecret == "" {
		slog.Error("STRIPE_WEBHOOK_SECRET is not configured")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	signatureHeader := req.Header.Get("Stripe-Signature")
	event, err = webhook.ConstructEvent(payload, signatureHeader, endpointSecret)
	if err != nil {
		slog.Error(fmt.Sprintf("Webhook signature verification failed. %v\n", err))
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Atomic dedup + side-effects: the webhook_events insert and the money-touching
	// writes (AddAICredits, GrantSubscriptionCredits) share one tx so a process
	// crash between dedup and money-write can't cause Stripe retries to double-fire.
	var (
		alreadyProcessed bool
		postCommitHooks  []func()
	)
	txErr := c.DB.RunInTx(func(tx bun.Tx) error {
		inserted, err := c.DB.InsertWebhookEventTx(tx, event.ID, string(event.Type))
		if err != nil {
			return fmt.Errorf("dedup insert: %w", err)
		}
		if !inserted {
			alreadyProcessed = true
			return nil
		}
		hooks, perr := c.processWebhookEventTx(tx, event)
		postCommitHooks = hooks
		return perr
	})
	if txErr != nil {
		slog.Error("webhook processing failed", "error", txErr, "event_id", event.ID, "event_type", event.Type)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if alreadyProcessed {
		slog.Info("webhook event already processed, skipping", "event_id", event.ID)
		w.WriteHeader(http.StatusOK)
		return
	}

	// Async event-sender goroutines deliberately fire only after the dedup tx
	// has committed, so a Stripe retry that arrives mid-tx doesn't double-fire
	// Resend events.
	for _, hook := range postCommitHooks {
		go hook()
	}

	w.WriteHeader(http.StatusOK)
}

// processWebhookEventTx handles a verified Stripe event inside a transaction
// owned by the caller (HandleWebhook). Returns the post-commit hooks to fire
// after the tx commits — these are the goroutines that publish to the event
// bus, deliberately deferred so a Stripe retry that arrives mid-tx can't
// double-fire Resend events. Returns an error if the event should be retried.
func (c *Controller) processWebhookEventTx(tx bun.Tx, event stripe.Event) ([]func(), error) {
	var hooks []func()
	addHook := func(user *models.User, name constants.EventName, props map[string]any) {
		u := user
		hooks = append(hooks, func() { c.EventService.SendEvent(u, name, props) })
	}

	switch event.Type {

	case "customer.subscription.created":
		var sub stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
			slog.Error("webhook: failed to unmarshal subscription", "error", err, "event_id", event.ID)
			return nil, nil // permanent parse error — don't retry
		}
		slog.Info("subscription created", "customer_id", sub.Customer.ID, "status", sub.Status)

		org, err := c.DB.GetOrganizationByStripeCustomerID(sub.Customer.ID)
		if err != nil {
			return nil, fmt.Errorf("organization not found for stripe customer %s: %w", sub.Customer.ID, err)
		}

		// Bounds check: ensure subscription has items
		if len(sub.Items.Data) == 0 {
			return nil, fmt.Errorf("subscription %s has no items", sub.ID)
		}

		subs, err := c.DB.GetSubscriptionByPriceID(sub.Items.Data[0].Price.ID)
		if err != nil {
			return nil, fmt.Errorf("plan not found for price ID %s: %w", sub.Items.Data[0].Price.ID, err)
		}
		org.SubscriptionID = subs.ID

		// Map Stripe status to our internal status
		org.SubscriptionStatus = mapStripeStatus(sub.Status)

		// Set timing based on actual Stripe status
		if sub.Status == "trialing" {
			org.TrialStartAt = time.Now()
		} else {
			org.SubscriptionStartAt = time.Now()
		}

		// Mark trial as used if subscription has/had a trial
		if sub.TrialEnd > 0 {
			org.TrialUsed = true
		}

		// Load org owner for user-keyed notifications, Resend events, and posthog identity.
		owner, err := c.DB.GetUserById(org.OwnerID.String())
		if err != nil {
			return nil, fmt.Errorf("owner not found for org %s: %w", org.ID, err)
		}

		setupJustFinished := false
		if !owner.SetupFinished {
			owner.SetupFinished = true
			setupJustFinished = true
			addHook(owner, constants.EVENT_USER_SETUP_FINISHED, map[string]any{})
		}

		addHook(owner, constants.EVENT_SUBSCRIPTION_CREATED, map[string]any{
			"subscription_status": org.SubscriptionStatus,
			"subscription_id":     org.SubscriptionID.String(),
			"plan_name":           subs.Name,
			"price_id":            sub.Items.Data[0].Price.ID,
		})

		if _, err := c.DB.UpdateOrganizationTx(tx, org); err != nil {
			return nil, fmt.Errorf("failed to update organization %s after subscription created: %w", org.ID, err)
		}

		// SetupFinished is still tracked on the user object — persist it.
		if setupJustFinished {
			if _, err := c.DB.UpdateUserTx(tx, owner); err != nil {
				return nil, fmt.Errorf("update user setup_finished flag: %w", err)
			}
		}

		// Grant plan's monthly credits inside the dedup tx. Must run after
		// UpdateOrganization so the org has the correct SubscriptionID.
		// Pass the already-resolved org and credit count so the grant sees the
		// new plan instead of reading stale committed state via GetUserLimits.
		monthlyCredits := c.PlanMonthlyAICredits(subs)
		hook, gerr := c.GrantSubscriptionCreditsInTxWithResolved(tx, owner, org.ID, org, monthlyCredits)
		if gerr != nil {
			return nil, fmt.Errorf("grant subscription credits: %w", gerr)
		}
		if hook != nil {
			hooks = append(hooks, hook)
		}

		// Grant referral subscription reward (50 credits to both parties) if user was referred.
		// Already idempotent via ClaimSubscriptionReward — runs outside the tx.
		c.GrantReferralSubscriptionReward(owner)

		// subscription_status (the mapped internal value, formerly `status`) is
		// auto-injected by TrackUserEvent off the user's billing snapshot —
		// dropping the explicit Set so we don't carry two near-identical keys
		// on the same event. stripe_status keeps the raw Stripe state for
		// debugging mismatches between the two.
		c.IdentifyUser(owner)
		c.TrackUserEvent(owner, constants.EVENT_SUBSCRIPTION_CREATED, posthog.NewProperties().
			Set("stripe_status", string(sub.Status)).
			Set("plan_name", subs.Name).
			Set("price_id", sub.Items.Data[0].Price.ID))

		// Meta CAPI: Subscribe fires once on initial subscription create. Skip
		// trial-only states ("trialing") since no money has moved yet — wait
		// for invoice.payment_succeeded to fire Purchase. We currently never
		// open subscriptions in trialing mode, so this branch is forward-safe.
		if sub.Status == "active" {
			price := sub.Items.Data[0].Price
			var value float64
			if price != nil && price.UnitAmount > 0 {
				value = float64(price.UnitAmount) / 100.0
			}
			currency := "USD"
			if price != nil && price.Currency != "" {
				currency = string(price.Currency)
			}
			ownerSnap := owner
			subID := sub.ID
			planName := subs.Name
			hooks = append(hooks, func() {
				c.FireMetaSubscribe(ownerSnap, subID, planName, value, currency)
			})
		}

	case "customer.subscription.updated":
		var sub stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
			slog.Error("webhook: failed to unmarshal subscription", "error", err, "event_id", event.ID)
			return nil, nil
		}
		slog.Info("subscription updated", "customer_id", sub.Customer.ID, "status", sub.Status)

		org, err := c.DB.GetOrganizationByStripeCustomerID(sub.Customer.ID)
		if err != nil {
			return nil, fmt.Errorf("organization not found for stripe customer %s: %w", sub.Customer.ID, err)
		}

		if len(sub.Items.Data) == 0 {
			return nil, fmt.Errorf("subscription %s has no items", sub.ID)
		}

		newPriceID := sub.Items.Data[0].Price.ID
		subs, err := c.DB.GetSubscriptionByPriceID(newPriceID)
		if err != nil {
			return nil, fmt.Errorf("plan not found for price ID %s: %w", newPriceID, err)
		}

		// Capture plan change detection BEFORE mutating org.SubscriptionID
		oldSubID := org.SubscriptionID
		planChanged := oldSubID != uuid.Nil && oldSubID != subs.ID
		var oldMonthlyPrice int
		var oldSub *models.StandardUserSubscription
		if planChanged {
			if loaded, err := c.DB.GetSubscriptionById(oldSubID.String()); err == nil {
				oldSub = loaded
				oldMonthlyPrice = loaded.MonthlyPrice
			}
		}

		// Stripe fires customer.subscription.updated for many reasons (payment
		// method change, cancel-at-period-end, schedule edits). Only treat it
		// as a plan change when previous_attributes.items is present.
		_, itemsChanged := event.Data.PreviousAttributes["items"]

		// Load org owner for user-keyed notifications and posthog identity.
		owner, err := c.DB.GetUserById(org.OwnerID.String())
		if err != nil {
			return nil, fmt.Errorf("owner not found for org %s: %w", org.ID, err)
		}

		// Map Stripe status to our internal status
		newStatus := mapStripeStatus(sub.Status)

		setupJustFinished := false

		switch sub.Status {
		case "active":
			org.SubscriptionID = subs.ID
			org.SubscriptionStatus = newStatus
			// Only set SubscriptionStartAt on initial activation (not renewals)
			if org.SubscriptionStartAt.IsZero() {
				org.SubscriptionStartAt = time.Now()
			}
			if !owner.SetupFinished {
				owner.SetupFinished = true
				setupJustFinished = true
				addHook(owner, constants.EVENT_USER_SETUP_FINISHED, map[string]any{})
			}
			// A6: re-subscribed within the grace window → kill any pending
			// teardown so the org's apps + DBs aren't destroyed mid-flight.
			if err := c.CancelOrgGraceTeardown(org.ID); err != nil {
				slog.Warn("active: failed to cancel grace teardown", "error", err, "org_id", org.ID)
			}

		case "trialing":
			org.SubscriptionID = subs.ID
			org.SubscriptionStatus = newStatus
			if org.TrialStartAt.IsZero() {
				org.TrialStartAt = time.Now()
			}
			if err := c.CancelOrgGraceTeardown(org.ID); err != nil {
				slog.Warn("trialing: failed to cancel grace teardown", "error", err, "org_id", org.ID)
			}

		case "past_due":
			// Keep SubscriptionID — Stripe is still retrying payment, plan limits stay valid
			org.SubscriptionID = subs.ID
			org.SubscriptionStatus = newStatus

		case "canceled":
			// Free tier removed 2026-05-13: cancelled orgs go to no plan.
			// SubscriptionID = uuid.Nil causes GetStandardUserSubscriptionByOrg to
			// return sql.ErrNoRows, which propagates up through most limit lookups
			// and blocks the operation — fail-closed.
			org.SubscriptionStatus = newStatus
			org.SubscriptionID = uuid.Nil
			// A6: schedule the 7-day grace teardown.
			if err := c.EnqueueOrgGraceTeardown(org.ID); err != nil {
				slog.Warn("canceled: failed to schedule grace teardown", "error", err, "org_id", org.ID)
			}

		case "unpaid":
			// Keep SubscriptionID — subscription still exists in Stripe, just unpaid
			org.SubscriptionID = subs.ID
			org.SubscriptionStatus = newStatus

		default:
			// incomplete, incomplete_expired, paused — null the plan for the
			// same reason as "canceled" above (no more free tier).
			org.SubscriptionStatus = newStatus
			org.SubscriptionID = uuid.Nil
		}

		addHook(owner, constants.EVENT_SUBSCRIPTION_UPDATED, map[string]any{
			"subscription_status": org.SubscriptionStatus,
			"subscription_id":     org.SubscriptionID.String(),
			"plan_name":           subs.Name,
		})

		if planChanged && org.SubscriptionID != uuid.Nil {
			changeEvent := constants.EVENT_SUBSCRIPTION_UPGRADED
			if subs.MonthlyPrice < oldMonthlyPrice {
				changeEvent = constants.EVENT_SUBSCRIPTION_DOWNGRADED
			}
			addHook(owner, changeEvent, map[string]any{
				"old_plan_id":       oldSubID.String(),
				"new_plan_id":       subs.ID.String(),
				"new_plan_name":     subs.Name,
				"old_monthly_price": oldMonthlyPrice,
				"new_monthly_price": subs.MonthlyPrice,
			})
		}

		if _, err := c.DB.UpdateOrganizationTx(tx, org); err != nil {
			return nil, fmt.Errorf("failed to update organization %s after subscription change: %w", org.ID, err)
		}

		// Persist owner's SetupFinished flag if it was just flipped.
		if setupJustFinished {
			if _, err := c.DB.UpdateUserTx(tx, owner); err != nil {
				return nil, fmt.Errorf("update user setup_finished flag: %w", err)
			}
		}

		// Reference scopes by (subID, currentPeriodEnd, newPriceID) so the same
		// upgrade dedups across Stripe retries while a future-period or
		// further-upgrade event gets a fresh ledger row.
		if planChanged && itemsChanged && org.SubscriptionID != uuid.Nil && oldSub != nil {
			newGrant := c.PlanMonthlyAICredits(subs)
			oldGrant := c.PlanMonthlyAICredits(oldSub)
			delta := newGrant - oldGrant
			if delta > 0 {
				balance, balErr := c.GetOrCreateCreditsBalance(owner, org.ID)
				if balErr != nil {
					return nil, fmt.Errorf("get/create credits balance for upgrade grant: %w", balErr)
				}
				periodEnd := sub.Items.Data[0].CurrentPeriodEnd
				reference := fmt.Sprintf("%s:%d:upgrade-%s", sub.ID, periodEnd, newPriceID)
				if err := c.AddAICreditsInTx(tx, owner, org.ID, balance.ID, delta, "subscription_upgrade_grant", reference); err != nil {
					return nil, fmt.Errorf("grant upgrade credits: %w", err)
				}
				hooks = append(hooks, func() {
					c.EventService.SendEvent(owner, constants.EVENT_CREDITS_GRANTED, map[string]any{
						"credits":    delta,
						"grant_type": "subscription_upgrade",
						"reference":  reference,
					})
				})
			}
		}

		// subscription_status is auto-injected via the billing snapshot (see
		// posthog.go TrackUserEvent). Identify first so the cached snapshot
		// picks up the just-written org.SubscriptionStatus.
		c.IdentifyUser(owner)
		c.TrackUserEvent(owner, constants.EVENT_SUBSCRIPTION_UPDATED, posthog.NewProperties().
			Set("stripe_status", string(sub.Status)).
			Set("plan_name", subs.Name).
			Set("price_id", sub.Items.Data[0].Price.ID))

		// Plan change → fire a separate upgrade/downgrade event so funnels can
		// distinguish a real plan transition from cancel-at-period-end toggles
		// or payment-method updates (all of which also raise subscription_updated).
		if planChanged && org.SubscriptionID != uuid.Nil {
			changeEventName := constants.EVENT_SUBSCRIPTION_UPGRADED
			if subs.MonthlyPrice < oldMonthlyPrice {
				changeEventName = constants.EVENT_SUBSCRIPTION_DOWNGRADED
			}
			c.TrackUserEvent(owner, changeEventName, posthog.NewProperties().
				Set("old_plan_id", oldSubID.String()).
				Set("new_plan_id", subs.ID.String()).
				Set("new_plan_name", subs.Name).
				Set("old_monthly_price", oldMonthlyPrice).
				Set("new_monthly_price", subs.MonthlyPrice))
		}

		if setupJustFinished {
			c.TrackUserEvent(owner, constants.EVENT_USER_SETUP_FINISHED, posthog.NewProperties().
				Set("trigger", "subscription_active"))
		}

		// Sync Neon projects to the new plan tier: transfer free↔paid org
		// (using personal API key) and PATCH per-project quotas. Done as a
		// post-commit hook so the webhook doesn't block on Neon API; we
		// snapshot the org ID + new limits inside the closure to avoid a
		// race with later state mutations.
		if planChanged && org.SubscriptionID != uuid.Nil {
			newLimits, limitsErr := c.DB.GetStandardUserLimitsById(subs.LimitsID.String())
			if limitsErr != nil {
				slog.Warn("subscription_updated: failed to load new limits for Neon sync",
					"error", limitsErr, "subscription_id", subs.ID)
			} else {
				orgID := org.ID
				hooks = append(hooks, func() {
					ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
					defer cancel()
					c.SyncNeonProjectsForOrgPlan(ctx, orgID, newLimits)
				})
			}
		}

	case "invoice.payment_succeeded":
		var invoice stripe.Invoice
		if err := json.Unmarshal(event.Data.Raw, &invoice); err != nil {
			slog.Error("webhook: failed to unmarshal invoice", "error", err, "event_id", event.ID)
			return nil, nil
		}
		slog.Info("invoice payment succeeded", "customer_id", invoice.Customer.ID)

		org, err := c.DB.GetOrganizationByStripeCustomerID(invoice.Customer.ID)
		if err != nil {
			return nil, fmt.Errorf("organization not found for stripe customer %s: %w", invoice.Customer.ID, err)
		}

		org.SubscriptionStatus = "active"

		// A6: payment-succeeded after a cancellation/dunning event implies
		// re-subscribe. Cancel any pending grace teardown so the org's apps
		// stay intact.
		if err := c.CancelOrgGraceTeardown(org.ID); err != nil {
			slog.Warn("invoice.payment_succeeded: failed to cancel grace teardown", "error", err, "org_id", org.ID)
		}

		// Restore SubscriptionID if it was cleared by a prior canceled/inactive event.
		// Look up plan from invoice line items (v82: Pricing.PriceDetails.Price).
		if org.SubscriptionID == uuid.Nil && invoice.Lines != nil && len(invoice.Lines.Data) > 0 {
			lineItem := invoice.Lines.Data[0]
			if lineItem.Pricing != nil && lineItem.Pricing.PriceDetails != nil && lineItem.Pricing.PriceDetails.Price != "" {
				if subs, err := c.DB.GetSubscriptionByPriceID(lineItem.Pricing.PriceDetails.Price); err == nil {
					org.SubscriptionID = subs.ID
				}
			}
		}

		// Load org owner for user-keyed notifications and posthog identity.
		owner, err := c.DB.GetUserById(org.OwnerID.String())
		if err != nil {
			return nil, fmt.Errorf("owner not found for org %s: %w", org.ID, err)
		}

		setupJustFinished := false
		if !owner.SetupFinished {
			owner.SetupFinished = true
			setupJustFinished = true
			addHook(owner, constants.EVENT_USER_SETUP_FINISHED, map[string]any{})
		}

		addHook(owner, constants.EVENT_SUBSCRIPTION_UPDATED, map[string]any{
			"subscription_status": org.SubscriptionStatus,
			"subscription_id":     org.SubscriptionID.String(),
		})

		addHook(owner, constants.EVENT_PAYMENT_SUCCEEDED, map[string]any{
			"amount_paid":    invoice.AmountPaid,
			"currency":       string(invoice.Currency),
			"invoice_id":     invoice.ID,
			"invoice_url":    invoice.HostedInvoiceURL,
			"billing_reason": string(invoice.BillingReason),
		})

		// Fire subscription_renewed specifically on recurring cycle invoices.
		// Stripe sets BillingReason = "subscription_cycle" on renewal, "subscription_create"
		// on first charge (which is already covered by customer.subscription.created).
		if invoice.BillingReason == stripe.InvoiceBillingReasonSubscriptionCycle {
			addHook(owner, constants.EVENT_SUBSCRIPTION_RENEWED, map[string]any{
				"subscription_id": org.SubscriptionID.String(),
				"amount_paid":     invoice.AmountPaid,
				"currency":        string(invoice.Currency),
				"invoice_id":      invoice.ID,
			})
		}

		if _, err := c.DB.UpdateOrganizationTx(tx, org); err != nil {
			return nil, fmt.Errorf("failed to update organization %s after payment succeeded: %w", org.ID, err)
		}

		if setupJustFinished {
			if _, err := c.DB.UpdateUserTx(tx, owner); err != nil {
				return nil, fmt.Errorf("update user setup_finished flag: %w", err)
			}
		}

		c.TrackUserEvent(owner, constants.EVENT_PAYMENT_SUCCEEDED, posthog.NewProperties().
			Set("amount_paid", invoice.AmountPaid).
			Set("currency", string(invoice.Currency)).
			Set("invoice_id", invoice.ID).
			Set("billing_reason", string(invoice.BillingReason)))

		// Recurring-cycle invoice → fire subscription_renewed too, so retention
		// funnels can separate first-charge from rebill without mining
		// billing_reason on every payment_succeeded event.
		if invoice.BillingReason == stripe.InvoiceBillingReasonSubscriptionCycle {
			c.TrackUserEvent(owner, constants.EVENT_SUBSCRIPTION_RENEWED, posthog.NewProperties().
				Set("subscription_id", org.SubscriptionID.String()).
				Set("amount_paid", invoice.AmountPaid).
				Set("currency", string(invoice.Currency)).
				Set("invoice_id", invoice.ID))
		}

		if setupJustFinished {
			c.TrackUserEvent(owner, constants.EVENT_USER_SETUP_FINISHED, posthog.NewProperties().
				Set("trigger", "invoice_paid"))
		}

		c.IdentifyUser(owner)

		// Meta CAPI: Purchase fires on every paid subscription invoice (first
		// charge + renewals). EventID derives from invoice.ID so dedup is
		// automatic across webhook retries.
		if invoice.AmountPaid > 0 {
			value := float64(invoice.AmountPaid) / 100.0
			currency := "USD"
			if invoice.Currency != "" {
				currency = string(invoice.Currency)
			}
			ownerSnap := owner
			invoiceID := invoice.ID
			hooks = append(hooks, func() {
				c.FireMetaPurchase(ownerSnap, invoiceID, "Subscription", []string{invoiceID}, value, currency, "subscription")
			})
		}

	case "invoice.payment_failed":
		var invoice stripe.Invoice
		if err := json.Unmarshal(event.Data.Raw, &invoice); err != nil {
			slog.Error("webhook: failed to unmarshal invoice", "error", err, "event_id", event.ID)
			return nil, nil
		}
		slog.Info("invoice payment failed", "customer_id", invoice.Customer.ID,
			"attempt_count", invoice.AttemptCount, "next_attempt", invoice.NextPaymentAttempt)

		org, err := c.DB.GetOrganizationByStripeCustomerID(invoice.Customer.ID)
		if err != nil {
			return nil, fmt.Errorf("organization not found for stripe customer %s: %w", invoice.Customer.ID, err)
		}

		owner, err := c.DB.GetUserById(org.OwnerID.String())
		if err != nil {
			return nil, fmt.Errorf("owner not found for org %s: %w", org.ID, err)
		}

		// Don't change subscription status here — let customer.subscription.updated
		// handle the status transition. Stripe sends both events, and the subscription
		// event has the authoritative status (past_due on first failure, unpaid after
		// exhausting retries). Changing status here would be premature.

		addHook(owner, constants.EVENT_PAYMENT_FAILED, map[string]any{
			"subscription_status": org.SubscriptionStatus,
			"subscription_id":     org.SubscriptionID.String(),
			"pay_invoice_url":     invoice.HostedInvoiceURL,
			"attempt_count":       invoice.AttemptCount,
			"billing_url":         c.BillingURLForOrg(org),
		})

		c.TrackUserEvent(owner, constants.EVENT_PAYMENT_FAILED, posthog.NewProperties().
			Set("invoice_url", invoice.HostedInvoiceURL).
			Set("amount_due", invoice.AmountDue).
			Set("attempt_count", invoice.AttemptCount))
		c.IdentifyUser(owner)

	case "invoice.payment_action_required":
		var invoice stripe.Invoice
		if err := json.Unmarshal(event.Data.Raw, &invoice); err != nil {
			slog.Error("webhook: failed to unmarshal invoice", "error", err, "event_id", event.ID)
			return nil, nil
		}
		slog.Info("invoice payment action required", "customer_id", invoice.Customer.ID)

		org, err := c.DB.GetOrganizationByStripeCustomerID(invoice.Customer.ID)
		if err != nil {
			return nil, fmt.Errorf("organization not found for stripe customer %s: %w", invoice.Customer.ID, err)
		}

		owner, err := c.DB.GetUserById(org.OwnerID.String())
		if err != nil {
			return nil, fmt.Errorf("owner not found for org %s: %w", org.ID, err)
		}

		addHook(owner, constants.EVENT_PAYMENT_USER_ACTION_REQUIRED, map[string]any{
			"subscription_status": org.SubscriptionStatus,
			"subscription_id":     org.SubscriptionID.String(),
			"pay_invoice_url":     invoice.HostedInvoiceURL,
			"billing_url":         c.BillingURLForOrg(org),
		})

		c.TrackUserEvent(owner, constants.EVENT_PAYMENT_USER_ACTION_REQUIRED, posthog.NewProperties().
			Set("invoice_url", invoice.HostedInvoiceURL))
		c.IdentifyUser(owner)

	case "invoice.upcoming":
		var invoice stripe.Invoice
		if err := json.Unmarshal(event.Data.Raw, &invoice); err != nil {
			slog.Error("webhook: failed to unmarshal invoice", "error", err, "event_id", event.ID)
			return nil, nil
		}
		if invoice.Customer == nil {
			return nil, nil
		}
		org, err := c.DB.GetOrganizationByStripeCustomerID(invoice.Customer.ID)
		if err != nil {
			return nil, fmt.Errorf("organization not found for stripe customer %s: %w", invoice.Customer.ID, err)
		}

		owner, err := c.DB.GetUserById(org.OwnerID.String())
		if err != nil {
			return nil, fmt.Errorf("owner not found for org %s: %w", org.ID, err)
		}

		addHook(owner, constants.EVENT_INVOICE_UPCOMING, map[string]any{
			"amount_due":   invoice.AmountDue,
			"currency":     string(invoice.Currency),
			"period_start": invoice.PeriodStart,
			"period_end":   invoice.PeriodEnd,
			"invoice_url":  invoice.HostedInvoiceURL,
		})

		c.TrackUserEvent(owner, constants.EVENT_INVOICE_UPCOMING, posthog.NewProperties().
			Set("amount_due", invoice.AmountDue).
			Set("currency", string(invoice.Currency)))

	case "customer.subscription.deleted":
		var sub stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
			slog.Error("webhook: failed to unmarshal subscription", "error", err, "event_id", event.ID)
			return nil, nil
		}
		slog.Info("subscription canceled", "customer_id", sub.Customer.ID)

		org, err := c.DB.GetOrganizationByStripeCustomerID(sub.Customer.ID)
		if err != nil {
			return nil, fmt.Errorf("organization not found for stripe customer %s: %w", sub.Customer.ID, err)
		}

		owner, err := c.DB.GetUserById(org.OwnerID.String())
		if err != nil {
			return nil, fmt.Errorf("owner not found for org %s: %w", org.ID, err)
		}

		// Resolve the plan name BEFORE nulling SubscriptionID so the email
		// payload can name the plan the user is losing.
		var planName string
		if org.SubscriptionID != uuid.Nil {
			if subs, perr := c.DB.GetSubscriptionById(org.SubscriptionID.String()); perr == nil && subs != nil {
				planName = subs.Name
			}
		}

		// Free tier removed 2026-05-13: cancelled orgs go to no plan.
		// SubscriptionID = uuid.Nil causes GetStandardUserSubscriptionByOrg to
		// return sql.ErrNoRows, which propagates up through most limit lookups
		// and blocks the operation — fail-closed.
		org.SubscriptionStatus = "canceled"
		org.SubscriptionID = uuid.Nil

		// A6: schedule a 7-day-out teardown of the org's Fly apps + Neon DBs.
		// Cancelled gracefully if the user re-subscribes within the window.
		if err := c.EnqueueOrgGraceTeardown(org.ID); err != nil {
			slog.Warn("subscription_canceled: failed to schedule grace teardown",
				"error", err, "org_id", org.ID)
		}

		// Stripe fires customer.subscription.deleted for both voluntary cancels
		// and dunning-driven cancels (final retry exhausted / dispute). Branch
		// on CancellationDetails.Reason so the user gets the right email.
		cancelEvent := constants.EVENT_SUBSCRIPTION_CANCELED_DUNNING
		if sub.CancellationDetails != nil &&
			sub.CancellationDetails.Reason == stripe.SubscriptionCancellationDetailsReasonCancellationRequested {
			cancelEvent = constants.EVENT_SUBSCRIPTION_CANCELED_VOLUNTARY
		}
		cancelReason := ""
		if sub.CancellationDetails != nil {
			cancelReason = string(sub.CancellationDetails.Reason)
		}
		eventProps := map[string]any{
			"subscription_id":     sub.ID,
			"plan_name":           planName,
			"cancellation_reason": cancelReason,
			"billing_url":         c.BillingURLForOrg(org),
		}
		addHook(owner, cancelEvent, eventProps)
		// Keep firing the legacy event for backward-compat with existing consumers.
		addHook(owner, constants.EVENT_SUBSCRIPTION_CANCELED, eventProps)

		if _, err := c.DB.UpdateOrganizationTx(tx, org); err != nil {
			return nil, fmt.Errorf("failed to update organization %s after subscription deleted: %w", org.ID, err)
		}

		c.TrackUserEvent(owner, constants.EVENT_SUBSCRIPTION_CANCELED, posthog.NewProperties().
			Set("subscription_id", sub.ID).
			Set("cancellation_reason", cancelReason).
			Set("category", cancelEvent))
		c.IdentifyUser(owner)

	case "payment_method.attached":
		var pm stripe.PaymentMethod
		if err := json.Unmarshal(event.Data.Raw, &pm); err != nil {
			slog.Error("webhook: failed to unmarshal payment method", "error", err, "event_id", event.ID)
			return nil, nil
		}

		org, err := c.DB.GetOrganizationByStripeCustomerID(pm.Customer.ID)
		if err != nil {
			return nil, fmt.Errorf("organization not found for stripe customer %s: %w", pm.Customer.ID, err)
		}

		owner, err := c.DB.GetUserById(org.OwnerID.String())
		if err != nil {
			return nil, fmt.Errorf("owner not found for org %s: %w", org.ID, err)
		}

		addHook(owner, constants.EVENT_PAYMENT_METHOD_ATTACHED, map[string]any{})

		c.TrackUserEvent(owner, constants.EVENT_PAYMENT_METHOD_ATTACHED, posthog.NewProperties().
			Set("payment_method_type", string(pm.Type)))

	case "payment_method.detached":
		var pm stripe.PaymentMethod
		if err := json.Unmarshal(event.Data.Raw, &pm); err != nil {
			slog.Error("webhook: failed to unmarshal payment method", "error", err, "event_id", event.ID)
			return nil, nil
		}

		org, err := c.DB.GetOrganizationByStripeCustomerID(pm.Customer.ID)
		if err != nil {
			return nil, fmt.Errorf("organization not found for stripe customer %s: %w", pm.Customer.ID, err)
		}

		owner, err := c.DB.GetUserById(org.OwnerID.String())
		if err != nil {
			return nil, fmt.Errorf("owner not found for org %s: %w", org.ID, err)
		}

		addHook(owner, constants.EVENT_PAYMENT_METHOD_DETACHED, map[string]any{})

		c.TrackUserEvent(owner, constants.EVENT_PAYMENT_METHOD_DETACHED, posthog.NewProperties().
			Set("payment_method_type", string(pm.Type)))

	case "checkout.session.completed":
		var cs stripe.CheckoutSession
		if err := json.Unmarshal(event.Data.Raw, &cs); err != nil {
			slog.Error("webhook: failed to unmarshal checkout session", "error", err, "event_id", event.ID)
			return nil, nil
		}

		// Only handle credit purchase sessions (identified by metadata)
		if cs.Metadata["type"] != "credit_purchase" {
			break
		}

		userID := cs.Metadata["user_id"]
		packSizeStr := cs.Metadata["pack_size"]
		if userID == "" || packSizeStr == "" {
			slog.Error("webhook: credit purchase missing metadata", "event_id", event.ID, "metadata", cs.Metadata)
			return nil, nil
		}

		packSize, err := strconv.Atoi(packSizeStr)
		if err != nil || packSize <= 0 {
			slog.Error("webhook: invalid pack_size in metadata", "pack_size", packSizeStr, "event_id", event.ID)
			return nil, nil
		}

		user, err := c.DB.GetUserById(userID)
		if err != nil {
			return nil, fmt.Errorf("user %s not found for credit purchase: %w", userID, err)
		}

		// Credit purchases are tied to the buyer's personal org (Plan A).
		personalOrg, orgErr := c.EnsurePersonalOrg(user)
		if orgErr != nil {
			return nil, fmt.Errorf("failed to ensure personal org for credit purchase: %w", orgErr)
		}
		// Resolve balance outside the tx (period-reset logic can't share an
		// outer tx), then do the money-write inside the dedup tx.
		balance, err := c.GetOrCreateCreditsBalance(user, personalOrg.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get credits balance for user %s: %w", userID, err)
		}
		if err := c.AddAICreditsInTx(tx, user, personalOrg.ID, balance.ID, packSize, "purchase", cs.ID); err != nil {
			return nil, fmt.Errorf("failed to add %d credits for user %s: %w", packSize, userID, err)
		}

		slog.Info("credits purchased", "user_id", userID, "pack_size", packSize, "session_id", cs.ID)

		addHook(user, constants.EVENT_CREDITS_PURCHASED, map[string]any{
			"pack_size":    packSize,
			"session_id":   cs.ID,
			"amount_total": cs.AmountTotal,
			"currency":     string(cs.Currency),
		})

		c.TrackUserEvent(user, constants.EVENT_CREDITS_PURCHASED, posthog.NewProperties().
			Set("pack_size", packSize).
			Set("session_id", cs.ID).
			Set("amount_total", cs.AmountTotal))
		c.IdentifyUser(user)

		// Meta CAPI: Purchase for the one-shot credit-pack flow. EventID
		// derives from cs.ID so retries dedup automatically.
		if cs.AmountTotal > 0 {
			value := float64(cs.AmountTotal) / 100.0
			currency := "USD"
			if cs.Currency != "" {
				currency = string(cs.Currency)
			}
			userSnap := user
			sessionID := cs.ID
			packLabel := fmt.Sprintf("%d AI Credits", packSize)
			hooks = append(hooks, func() {
				c.FireMetaPurchase(userSnap, sessionID, packLabel, []string{sessionID}, value, currency, "credit_pack")
			})
		}

	case "charge.refunded":
		var charge stripe.Charge
		if err := json.Unmarshal(event.Data.Raw, &charge); err != nil {
			slog.Error("webhook: failed to unmarshal charge", "error", err, "event_id", event.ID)
			return nil, nil
		}

		if charge.Amount <= 0 || charge.AmountRefunded <= 0 {
			slog.Warn("charge.refunded with non-positive amounts; skipping",
				"charge_id", charge.ID, "amount", charge.Amount, "amount_refunded", charge.AmountRefunded)
			break
		}

		// Resolve candidate references for the original purchase row. Existing
		// purchases store cs.ID (see checkout.session.completed); fall back to
		// payment_intent and charge IDs for resilience against future changes.
		candidates := []string{}
		if charge.PaymentIntent != nil && charge.PaymentIntent.ID != "" {
			listParams := &stripe.CheckoutSessionListParams{PaymentIntent: stripe.String(charge.PaymentIntent.ID)}
			iter := checkoutsession.List(listParams)
			for iter.Next() {
				if s := iter.CheckoutSession(); s != nil && s.ID != "" {
					candidates = append(candidates, s.ID)
				}
			}
			if iterErr := iter.Err(); iterErr != nil {
				slog.Warn("charge.refunded: failed to list checkout sessions for payment_intent",
					"error", iterErr, "charge_id", charge.ID, "payment_intent", charge.PaymentIntent.ID)
			}
			candidates = append(candidates, charge.PaymentIntent.ID)
		}
		candidates = append(candidates, charge.ID)

		original, err := c.DB.GetAICreditTransactionByActionRefsTx(tx, "purchase", candidates)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				slog.Warn("charge.refunded: no matching purchase row; skipping",
					"charge_id", charge.ID, "candidates", candidates)
				break
			}
			return nil, fmt.Errorf("lookup purchase tx for charge %s: %w", charge.ID, err)
		}

		var latestRefund *stripe.Refund
		if charge.Refunds != nil {
			for _, r := range charge.Refunds.Data {
				if r == nil {
					continue
				}
				if latestRefund == nil || r.Created > latestRefund.Created {
					latestRefund = r
				}
			}
		}
		if latestRefund == nil || latestRefund.ID == "" {
			slog.Warn("charge.refunded: event has no refund records; skipping",
				"charge_id", charge.ID)
			break
		}

		// Stripe re-fires charge.refunded for each refund; AmountRefunded is
		// cumulative across all refunds on the charge. targetDebit is the total
		// debit that should exist; priorDebit is what we've already written on
		// earlier refund rows for this charge. Only write the delta.
		refundFraction := float64(charge.AmountRefunded) / float64(charge.Amount)
		creditsToDebit := int(math.Round(float64(original.Amount) * refundFraction))
		if charge.AmountRefunded == charge.Amount {
			creditsToDebit = original.Amount
		}
		targetDebit := -creditsToDebit

		priorDebit, err := c.DB.SumAICreditAmountsByActionRefPrefixTx(tx, "refund", charge.ID+":%")
		if err != nil {
			return nil, fmt.Errorf("sum prior refund debits for charge %s: %w", charge.ID, err)
		}
		thisDelta := targetDebit - priorDebit
		if thisDelta == 0 {
			slog.Info("charge.refunded: no delta to apply", "charge_id", charge.ID,
				"target_debit", targetDebit, "prior_debit", priorDebit)
			break
		}

		balance, err := c.DB.GetAICreditsBalanceByOrg(original.OrganizationID.String())
		if err != nil {
			return nil, fmt.Errorf("get balance for org %s: %w", original.OrganizationID, err)
		}

		owner, err := c.DB.GetUserById(original.UserID.String())
		if err != nil {
			return nil, fmt.Errorf("user %s not found for refund: %w", original.UserID, err)
		}

		refundRef := charge.ID + ":" + latestRefund.ID
		if err := c.AddAICreditsInTx(tx, owner, original.OrganizationID, balance.ID, thisDelta, "refund", refundRef); err != nil {
			return nil, fmt.Errorf("debit %d credits for refund %s: %w", thisDelta, refundRef, err)
		}

		slog.Info("credits refunded", "user_id", owner.ID, "org_id", original.OrganizationID,
			"charge_id", charge.ID, "refund_id", latestRefund.ID,
			"delta", thisDelta, "target_debit", targetDebit, "prior_debit", priorDebit,
			"amount_refunded", charge.AmountRefunded, "amount", charge.Amount)

		addHook(owner, constants.EVENT_CREDITS_REFUNDED, map[string]any{
			"charge_id":       charge.ID,
			"refund_id":       latestRefund.ID,
			"delta":           thisDelta,
			"target_debit":    targetDebit,
			"prior_debit":     priorDebit,
			"amount_refunded": charge.AmountRefunded,
			"amount":          charge.Amount,
		})

		c.TrackUserEvent(owner, constants.EVENT_CREDITS_REFUNDED, posthog.NewProperties().
			Set("charge_id", charge.ID).
			Set("refund_id", latestRefund.ID).
			Set("delta", thisDelta).
			Set("amount_refunded", charge.AmountRefunded).
			Set("amount", charge.Amount))
		c.IdentifyUser(owner)

	default:
		slog.Warn("unhandled webhook event type", "event_type", event.Type, "event_id", event.ID)

		var unmarshalledData map[string]any
		if err := json.Unmarshal(event.Data.Raw, &unmarshalledData); err != nil {
			slog.Error("webhook: failed to unmarshal unhandled event data", "error", err, "event_id", event.ID)
			return hooks, nil
		}

		customerID, ok := unmarshalledData["customer"].(string)
		if ok && customerID != "" {
			if org, err := c.DB.GetOrganizationByStripeCustomerID(customerID); err == nil {
				if owner, err := c.DB.GetUserById(org.OwnerID.String()); err == nil {
					addHook(owner, constants.EVENT_UNHANDLED_EVENT, map[string]any{
						"from":       "stripe-webhook",
						"event_type": event.Type,
					})
					return hooks, nil
				}
			}
		}

		addHook(&models.User{}, constants.EVENT_UNHANDLED_EVENT, map[string]any{
			"from":       "stripe-webhook",
			"event_type": event.Type,
		})
	}

	return hooks, nil
}
