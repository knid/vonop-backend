package controllers

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/posthog/posthog-go"
	"github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/webhook"
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

	// Deduplication: skip events we have already processed
	processed, err := c.DB.IsWebhookEventProcessed(event.ID)
	if err != nil {
		slog.Error("failed to check webhook event dedup", "error", err, "event_id", event.ID)
	}
	if processed {
		slog.Info("webhook event already processed, skipping", "event_id", event.ID)
		w.WriteHeader(http.StatusOK)
		return
	}

	// Process the event. On failure, return 500 so Stripe retries.
	// The event is only marked as processed after successful handling.
	processingErr := c.processWebhookEvent(event)
	if processingErr != nil {
		slog.Error("webhook processing failed", "error", processingErr, "event_id", event.ID, "event_type", event.Type)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Mark event as processed only after successful handling
	if err := c.DB.MarkWebhookEventProcessed(event.ID, string(event.Type)); err != nil {
		slog.Error("failed to mark webhook event as processed", "error", err, "event_id", event.ID)
	}

	w.WriteHeader(http.StatusOK)
}

// processWebhookEvent handles a verified Stripe event. Returns an error if
// the event should be retried (e.g. user not found, DB failure).
func (c *Controller) processWebhookEvent(event stripe.Event) error {
	switch event.Type {

	case "customer.subscription.created":
		var sub stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
			slog.Error("webhook: failed to unmarshal subscription", "error", err, "event_id", event.ID)
			return nil // permanent parse error — don't retry
		}
		slog.Info("subscription created", "customer_id", sub.Customer.ID, "status", sub.Status)

		org, err := c.DB.GetOrganizationByStripeCustomerID(sub.Customer.ID)
		if err != nil {
			return fmt.Errorf("organization not found for stripe customer %s: %w", sub.Customer.ID, err)
		}

		// Bounds check: ensure subscription has items
		if len(sub.Items.Data) == 0 {
			return fmt.Errorf("subscription %s has no items", sub.ID)
		}

		subs, err := c.DB.GetSubscriptionByPriceID(sub.Items.Data[0].Price.ID)
		if err != nil {
			return fmt.Errorf("plan not found for price ID %s: %w", sub.Items.Data[0].Price.ID, err)
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

		// Load org owner for user-keyed notifications, CRM events, and posthog identity.
		owner, err := c.DB.GetUserById(org.OwnerID.String())
		if err != nil {
			return fmt.Errorf("owner not found for org %s: %w", org.ID, err)
		}

		setupJustFinished := false
		if !owner.SetupFinished {
			owner.SetupFinished = true
			setupJustFinished = true
			go c.EventService.SendEvent(owner, constants.EVENT_USER_SETUP_FINISHED, map[string]any{})
		}

		go c.EventService.SendEvent(owner, constants.EVENT_SUBSCRIPTION_CREATED, map[string]any{
			"subscription_status": org.SubscriptionStatus,
			"subscription_id":     org.SubscriptionID.String(),
			"plan_name":           subs.Name,
			"price_id":            sub.Items.Data[0].Price.ID,
		})

		if _, err := c.DB.UpdateOrganization(org); err != nil {
			return fmt.Errorf("failed to update organization %s after subscription created: %w", org.ID, err)
		}

		// SetupFinished is still tracked on the user object — persist it.
		if setupJustFinished {
			if _, err := c.DB.UpdateUser(owner); err != nil {
				slog.Error("failed to update user setup_finished flag", "error", err, "user_id", owner.ID)
			}
		}

		// Grant plan's monthly credits (e.g. 100 for Starter).
		// Must run after UpdateOrganization so the org has the correct SubscriptionID.
		// Subscriptions are billed at the org level (Plan A) — credits land on the org balance.
		c.GrantSubscriptionCredits(owner, org.ID)

		// Grant referral subscription reward (50 credits to both parties) if user was referred
		// Must be synchronous — if this runs as a goroutine the event is marked processed
		// before credits are granted, and Stripe won't retry on failure.
		c.GrantReferralSubscriptionReward(owner)

		c.TrackUserEvent(owner, "subscription_created", posthog.NewProperties().
			Set("status", org.SubscriptionStatus).
			Set("stripe_status", string(sub.Status)).
			Set("plan_name", subs.Name).
			Set("price_id", sub.Items.Data[0].Price.ID))
		c.IdentifyUser(owner)

	case "customer.subscription.updated":
		var sub stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
			slog.Error("webhook: failed to unmarshal subscription", "error", err, "event_id", event.ID)
			return nil
		}
		slog.Info("subscription updated", "customer_id", sub.Customer.ID, "status", sub.Status)

		org, err := c.DB.GetOrganizationByStripeCustomerID(sub.Customer.ID)
		if err != nil {
			return fmt.Errorf("organization not found for stripe customer %s: %w", sub.Customer.ID, err)
		}

		if len(sub.Items.Data) == 0 {
			return fmt.Errorf("subscription %s has no items", sub.ID)
		}

		subs, err := c.DB.GetSubscriptionByPriceID(sub.Items.Data[0].Price.ID)
		if err != nil {
			return fmt.Errorf("plan not found for price ID %s: %w", sub.Items.Data[0].Price.ID, err)
		}

		// Capture plan change detection BEFORE mutating org.SubscriptionID
		oldSubID := org.SubscriptionID
		planChanged := oldSubID != uuid.Nil && oldSubID != subs.ID
		var oldMonthlyPrice int
		if planChanged {
			if oldSub, err := c.DB.GetSubscriptionById(oldSubID.String()); err == nil {
				oldMonthlyPrice = oldSub.MonthlyPrice
			}
		}

		// Load org owner for user-keyed notifications and posthog identity.
		owner, err := c.DB.GetUserById(org.OwnerID.String())
		if err != nil {
			return fmt.Errorf("owner not found for org %s: %w", org.ID, err)
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
				go c.EventService.SendEvent(owner, constants.EVENT_USER_SETUP_FINISHED, map[string]any{})
			}

		case "trialing":
			org.SubscriptionID = subs.ID
			org.SubscriptionStatus = newStatus
			if org.TrialStartAt.IsZero() {
				org.TrialStartAt = time.Now()
			}

		case "past_due":
			// Keep SubscriptionID — Stripe is still retrying payment, plan limits stay valid
			org.SubscriptionID = subs.ID
			org.SubscriptionStatus = newStatus

		case "canceled":
			org.SubscriptionStatus = newStatus
			org.SubscriptionID = uuid.Nil

		case "unpaid":
			// Keep SubscriptionID — subscription still exists in Stripe, just unpaid
			org.SubscriptionID = subs.ID
			org.SubscriptionStatus = newStatus

		default:
			// incomplete, incomplete_expired, paused
			org.SubscriptionStatus = newStatus
			org.SubscriptionID = uuid.Nil
		}

		go c.EventService.SendEvent(owner, constants.EVENT_SUBSCRIPTION_UPDATED, map[string]any{
			"subscription_status": org.SubscriptionStatus,
			"subscription_id":     org.SubscriptionID.String(),
			"plan_name":           subs.Name,
		})

		if planChanged && org.SubscriptionID != uuid.Nil {
			changeEvent := constants.EVENT_SUBSCRIPTION_UPGRADED
			if subs.MonthlyPrice < oldMonthlyPrice {
				changeEvent = constants.EVENT_SUBSCRIPTION_DOWNGRADED
			}
			go c.EventService.SendEvent(owner, changeEvent, map[string]any{
				"old_plan_id":       oldSubID.String(),
				"new_plan_id":       subs.ID.String(),
				"new_plan_name":     subs.Name,
				"old_monthly_price": oldMonthlyPrice,
				"new_monthly_price": subs.MonthlyPrice,
			})
		}

		if _, err := c.DB.UpdateOrganization(org); err != nil {
			return fmt.Errorf("failed to update organization %s after subscription change: %w", org.ID, err)
		}

		// Persist owner's SetupFinished flag if it was just flipped.
		if setupJustFinished {
			if _, err := c.DB.UpdateUser(owner); err != nil {
				slog.Error("failed to update user setup_finished flag", "error", err, "user_id", owner.ID)
			}
		}

		c.TrackUserEvent(owner, "subscription_updated", posthog.NewProperties().
			Set("status", org.SubscriptionStatus).
			Set("stripe_status", string(sub.Status)).
			Set("plan_name", subs.Name).
			Set("price_id", sub.Items.Data[0].Price.ID))
		c.IdentifyUser(owner)

	case "invoice.payment_succeeded":
		var invoice stripe.Invoice
		if err := json.Unmarshal(event.Data.Raw, &invoice); err != nil {
			slog.Error("webhook: failed to unmarshal invoice", "error", err, "event_id", event.ID)
			return nil
		}
		slog.Info("invoice payment succeeded", "customer_id", invoice.Customer.ID)

		org, err := c.DB.GetOrganizationByStripeCustomerID(invoice.Customer.ID)
		if err != nil {
			return fmt.Errorf("organization not found for stripe customer %s: %w", invoice.Customer.ID, err)
		}

		org.SubscriptionStatus = "active"

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
			return fmt.Errorf("owner not found for org %s: %w", org.ID, err)
		}

		setupJustFinished := false
		if !owner.SetupFinished {
			owner.SetupFinished = true
			setupJustFinished = true
			go c.EventService.SendEvent(owner, constants.EVENT_USER_SETUP_FINISHED, map[string]any{})
		}

		go c.EventService.SendEvent(owner, constants.EVENT_SUBSCRIPTION_UPDATED, map[string]any{
			"subscription_status": org.SubscriptionStatus,
			"subscription_id":     org.SubscriptionID.String(),
		})

		go c.EventService.SendEvent(owner, constants.EVENT_PAYMENT_SUCCEEDED, map[string]any{
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
			go c.EventService.SendEvent(owner, constants.EVENT_SUBSCRIPTION_RENEWED, map[string]any{
				"subscription_id": org.SubscriptionID.String(),
				"amount_paid":     invoice.AmountPaid,
				"currency":        string(invoice.Currency),
				"invoice_id":      invoice.ID,
			})
		}

		if _, err := c.DB.UpdateOrganization(org); err != nil {
			return fmt.Errorf("failed to update organization %s after payment succeeded: %w", org.ID, err)
		}

		if setupJustFinished {
			if _, err := c.DB.UpdateUser(owner); err != nil {
				slog.Error("failed to update user setup_finished flag", "error", err, "user_id", owner.ID)
			}
		}

		c.TrackUserEvent(owner, "payment_succeeded", posthog.NewProperties().
			Set("amount_paid", invoice.AmountPaid).
			Set("currency", string(invoice.Currency)).
			Set("invoice_id", invoice.ID).
			Set("billing_reason", string(invoice.BillingReason)))
		c.IdentifyUser(owner)

	case "invoice.payment_failed":
		var invoice stripe.Invoice
		if err := json.Unmarshal(event.Data.Raw, &invoice); err != nil {
			slog.Error("webhook: failed to unmarshal invoice", "error", err, "event_id", event.ID)
			return nil
		}
		slog.Info("invoice payment failed", "customer_id", invoice.Customer.ID,
			"attempt_count", invoice.AttemptCount, "next_attempt", invoice.NextPaymentAttempt)

		org, err := c.DB.GetOrganizationByStripeCustomerID(invoice.Customer.ID)
		if err != nil {
			return fmt.Errorf("organization not found for stripe customer %s: %w", invoice.Customer.ID, err)
		}

		owner, err := c.DB.GetUserById(org.OwnerID.String())
		if err != nil {
			return fmt.Errorf("owner not found for org %s: %w", org.ID, err)
		}

		// Don't change subscription status here — let customer.subscription.updated
		// handle the status transition. Stripe sends both events, and the subscription
		// event has the authoritative status (past_due on first failure, unpaid after
		// exhausting retries). Changing status here would be premature.

		go c.EventService.SendEvent(owner, constants.EVENT_PAYMENT_FAILED, map[string]any{
			"subscription_status": org.SubscriptionStatus,
			"subscription_id":     org.SubscriptionID.String(),
			"pay_invoice_url":     invoice.HostedInvoiceURL,
			"attempt_count":       invoice.AttemptCount,
		})

		c.TrackUserEvent(owner, "payment_failed", posthog.NewProperties().
			Set("invoice_url", invoice.HostedInvoiceURL).
			Set("amount_due", invoice.AmountDue).
			Set("attempt_count", invoice.AttemptCount))
		c.IdentifyUser(owner)

	case "invoice.payment_action_required":
		var invoice stripe.Invoice
		if err := json.Unmarshal(event.Data.Raw, &invoice); err != nil {
			slog.Error("webhook: failed to unmarshal invoice", "error", err, "event_id", event.ID)
			return nil
		}
		slog.Info("invoice payment action required", "customer_id", invoice.Customer.ID)

		org, err := c.DB.GetOrganizationByStripeCustomerID(invoice.Customer.ID)
		if err != nil {
			return fmt.Errorf("organization not found for stripe customer %s: %w", invoice.Customer.ID, err)
		}

		owner, err := c.DB.GetUserById(org.OwnerID.String())
		if err != nil {
			return fmt.Errorf("owner not found for org %s: %w", org.ID, err)
		}

		go c.EventService.SendEvent(owner, constants.EVENT_PAYMENT_USER_ACTION_REQUIRED, map[string]any{
			"subscription_status": org.SubscriptionStatus,
			"subscription_id":     org.SubscriptionID.String(),
			"pay_invoice_url":     invoice.HostedInvoiceURL,
		})

		c.TrackUserEvent(owner, "payment_action_required", posthog.NewProperties().
			Set("invoice_url", invoice.HostedInvoiceURL))
		c.IdentifyUser(owner)

	case "invoice.upcoming":
		var invoice stripe.Invoice
		if err := json.Unmarshal(event.Data.Raw, &invoice); err != nil {
			slog.Error("webhook: failed to unmarshal invoice", "error", err, "event_id", event.ID)
			return nil
		}
		if invoice.Customer == nil {
			return nil
		}
		org, err := c.DB.GetOrganizationByStripeCustomerID(invoice.Customer.ID)
		if err != nil {
			return fmt.Errorf("organization not found for stripe customer %s: %w", invoice.Customer.ID, err)
		}

		owner, err := c.DB.GetUserById(org.OwnerID.String())
		if err != nil {
			return fmt.Errorf("owner not found for org %s: %w", org.ID, err)
		}

		go c.EventService.SendEvent(owner, constants.EVENT_INVOICE_UPCOMING, map[string]any{
			"amount_due":   invoice.AmountDue,
			"currency":     string(invoice.Currency),
			"period_start": invoice.PeriodStart,
			"period_end":   invoice.PeriodEnd,
			"invoice_url":  invoice.HostedInvoiceURL,
		})

		c.TrackUserEvent(owner, "invoice_upcoming", posthog.NewProperties().
			Set("amount_due", invoice.AmountDue).
			Set("currency", string(invoice.Currency)))

	case "customer.subscription.deleted":
		var sub stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
			slog.Error("webhook: failed to unmarshal subscription", "error", err, "event_id", event.ID)
			return nil
		}
		slog.Info("subscription canceled", "customer_id", sub.Customer.ID)

		org, err := c.DB.GetOrganizationByStripeCustomerID(sub.Customer.ID)
		if err != nil {
			return fmt.Errorf("organization not found for stripe customer %s: %w", sub.Customer.ID, err)
		}

		owner, err := c.DB.GetUserById(org.OwnerID.String())
		if err != nil {
			return fmt.Errorf("owner not found for org %s: %w", org.ID, err)
		}

		org.SubscriptionStatus = "canceled"
		org.SubscriptionID = uuid.Nil

		go c.EventService.SendEvent(owner, constants.EVENT_SUBSCRIPTION_CANCELLED, map[string]any{})

		if _, err := c.DB.UpdateOrganization(org); err != nil {
			return fmt.Errorf("failed to update organization %s after subscription deleted: %w", org.ID, err)
		}

		c.TrackUserEvent(owner, "subscription_canceled", posthog.NewProperties().
			Set("subscription_id", sub.ID))
		c.IdentifyUser(owner)

	case "payment_method.attached":
		var pm stripe.PaymentMethod
		if err := json.Unmarshal(event.Data.Raw, &pm); err != nil {
			slog.Error("webhook: failed to unmarshal payment method", "error", err, "event_id", event.ID)
			return nil
		}

		org, err := c.DB.GetOrganizationByStripeCustomerID(pm.Customer.ID)
		if err != nil {
			return fmt.Errorf("organization not found for stripe customer %s: %w", pm.Customer.ID, err)
		}

		owner, err := c.DB.GetUserById(org.OwnerID.String())
		if err != nil {
			return fmt.Errorf("owner not found for org %s: %w", org.ID, err)
		}

		go c.EventService.SendEvent(owner, constants.EVENT_PAYMENT_METHOD_ATTACHED, map[string]any{})

		c.TrackUserEvent(owner, "payment_method_attached", posthog.NewProperties().
			Set("payment_method_type", string(pm.Type)))

	case "payment_method.detached":
		var pm stripe.PaymentMethod
		if err := json.Unmarshal(event.Data.Raw, &pm); err != nil {
			slog.Error("webhook: failed to unmarshal payment method", "error", err, "event_id", event.ID)
			return nil
		}

		org, err := c.DB.GetOrganizationByStripeCustomerID(pm.Customer.ID)
		if err != nil {
			return fmt.Errorf("organization not found for stripe customer %s: %w", pm.Customer.ID, err)
		}

		owner, err := c.DB.GetUserById(org.OwnerID.String())
		if err != nil {
			return fmt.Errorf("owner not found for org %s: %w", org.ID, err)
		}

		go c.EventService.SendEvent(owner, constants.EVENT_PAYMENT_METHOD_DETACHED, map[string]any{})

		c.TrackUserEvent(owner, "payment_method_detached", posthog.NewProperties().
			Set("payment_method_type", string(pm.Type)))

	case "checkout.session.completed":
		var cs stripe.CheckoutSession
		if err := json.Unmarshal(event.Data.Raw, &cs); err != nil {
			slog.Error("webhook: failed to unmarshal checkout session", "error", err, "event_id", event.ID)
			return nil
		}

		// Only handle credit purchase sessions (identified by metadata)
		if cs.Metadata["type"] != "credit_purchase" {
			break
		}

		userID := cs.Metadata["user_id"]
		packSizeStr := cs.Metadata["pack_size"]
		if userID == "" || packSizeStr == "" {
			slog.Error("webhook: credit purchase missing metadata", "event_id", event.ID, "metadata", cs.Metadata)
			return nil
		}

		packSize, err := strconv.Atoi(packSizeStr)
		if err != nil || packSize <= 0 {
			slog.Error("webhook: invalid pack_size in metadata", "pack_size", packSizeStr, "event_id", event.ID)
			return nil
		}

		user, err := c.DB.GetUserById(userID)
		if err != nil {
			return fmt.Errorf("user %s not found for credit purchase: %w", userID, err)
		}

		// Credit purchases are tied to the buyer's personal org (Plan A).
		personalOrg, orgErr := c.EnsurePersonalOrg(user)
		if orgErr != nil {
			return fmt.Errorf("failed to ensure personal org for credit purchase: %w", orgErr)
		}
		if err := c.AddAICredits(user, personalOrg.ID, packSize, "purchase", cs.ID); err != nil {
			return fmt.Errorf("failed to add %d credits for user %s: %w", packSize, userID, err)
		}

		slog.Info("credits purchased", "user_id", userID, "pack_size", packSize, "session_id", cs.ID)

		go c.EventService.SendEvent(user, constants.EVENT_CREDITS_PURCHASED, map[string]any{
			"pack_size":    packSize,
			"session_id":   cs.ID,
			"amount_total": cs.AmountTotal,
			"currency":     string(cs.Currency),
		})

		c.TrackUserEvent(user, "credits_purchased", posthog.NewProperties().
			Set("pack_size", packSize).
			Set("session_id", cs.ID).
			Set("amount_total", cs.AmountTotal))
		c.IdentifyUser(user)

	default:
		slog.Warn("unhandled webhook event type", "event_type", event.Type, "event_id", event.ID)

		var unmarshalledData map[string]any
		if err := json.Unmarshal(event.Data.Raw, &unmarshalledData); err != nil {
			slog.Error("webhook: failed to unmarshal unhandled event data", "error", err, "event_id", event.ID)
			return nil
		}

		customerID, ok := unmarshalledData["customer"].(string)
		if ok && customerID != "" {
			if org, err := c.DB.GetOrganizationByStripeCustomerID(customerID); err == nil {
				if owner, err := c.DB.GetUserById(org.OwnerID.String()); err == nil {
					go c.EventService.SendEvent(owner, constants.EVENT_UNHANDLED_EVENT, map[string]any{
						"from":       "stripe-webhook",
						"event_type": event.Type,
					})
					return nil
				}
			}
		}

		go c.EventService.SendEvent(&models.User{}, constants.EVENT_UNHANDLED_EVENT, map[string]any{
			"from":       "stripe-webhook",
			"event_type": event.Type,
		})
	}

	return nil
}
