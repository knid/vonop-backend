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

		user, err := c.DB.GetUserByStripeCustomerID(sub.Customer.ID)
		if err != nil {
			return fmt.Errorf("user not found for stripe customer %s: %w", sub.Customer.ID, err)
		}

		// Bounds check: ensure subscription has items
		if len(sub.Items.Data) == 0 {
			return fmt.Errorf("subscription %s has no items", sub.ID)
		}

		subs, err := c.DB.GetSubscriptionByPriceID(sub.Items.Data[0].Price.ID)
		if err != nil {
			return fmt.Errorf("plan not found for price ID %s: %w", sub.Items.Data[0].Price.ID, err)
		}
		user.SubscriptionID = subs.ID

		// Map Stripe status to our internal status
		user.SubscriptionStatus = mapStripeStatus(sub.Status)

		// Set timing based on actual Stripe status
		if sub.Status == "trialing" {
			user.TrialStartAt = time.Now()
		} else {
			user.SubscriptionStartAt = time.Now()
		}

		// Mark trial as used if subscription has/had a trial
		if sub.TrialEnd > 0 {
			user.TrialUsed = true
		}

		if !user.SetupFinished {
			user.SetupFinished = true
			go c.CRMService.SendEvent(user, constants.EVENT_USER_SETUP_FINISHED, map[string]any{})
		}

		go c.CRMService.SendEvent(user, constants.EVENT_SUBSCRIPTION_CREATED, map[string]any{
			"subscription_status": user.SubscriptionStatus,
			"subscription_id":     user.SubscriptionID.String(),
			"plan_name":           subs.Name,
			"price_id":            sub.Items.Data[0].Price.ID,
		})

		if _, err := c.DB.UpdateUser(user); err != nil {
			return fmt.Errorf("failed to update user %s after subscription created: %w", user.ID, err)
		}

		// Grant plan's monthly credits (e.g. 100 for Starter).
		// Must run after UpdateUser so the user has the correct SubscriptionID.
		c.GrantSubscriptionCredits(user)

		// Grant referral subscription reward (50 credits to both parties) if user was referred
		// Must be synchronous — if this runs as a goroutine the event is marked processed
		// before credits are granted, and Stripe won't retry on failure.
		c.GrantReferralSubscriptionReward(user)

		c.TrackUserEvent(user, "subscription_created", posthog.NewProperties().
			Set("status", user.SubscriptionStatus).
			Set("stripe_status", string(sub.Status)).
			Set("plan_name", subs.Name).
			Set("price_id", sub.Items.Data[0].Price.ID))
		c.IdentifyUser(user)

	case "customer.subscription.updated":
		var sub stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
			slog.Error("webhook: failed to unmarshal subscription", "error", err, "event_id", event.ID)
			return nil
		}
		slog.Info("subscription updated", "customer_id", sub.Customer.ID, "status", sub.Status)

		user, err := c.DB.GetUserByStripeCustomerID(sub.Customer.ID)
		if err != nil {
			return fmt.Errorf("user not found for stripe customer %s: %w", sub.Customer.ID, err)
		}

		if len(sub.Items.Data) == 0 {
			return fmt.Errorf("subscription %s has no items", sub.ID)
		}

		subs, err := c.DB.GetSubscriptionByPriceID(sub.Items.Data[0].Price.ID)
		if err != nil {
			return fmt.Errorf("plan not found for price ID %s: %w", sub.Items.Data[0].Price.ID, err)
		}

		// Capture plan change detection BEFORE mutating user.SubscriptionID
		oldSubID := user.SubscriptionID
		planChanged := oldSubID != uuid.Nil && oldSubID != subs.ID
		var oldMonthlyPrice int
		if planChanged {
			if oldSub, err := c.DB.GetSubscriptionById(oldSubID.String()); err == nil {
				oldMonthlyPrice = oldSub.MonthlyPrice
			}
		}

		// Map Stripe status to our internal status
		newStatus := mapStripeStatus(sub.Status)

		switch sub.Status {
		case "active":
			user.SubscriptionID = subs.ID
			user.SubscriptionStatus = newStatus
			// Only set SubscriptionStartAt on initial activation (not renewals)
			if user.SubscriptionStartAt.IsZero() {
				user.SubscriptionStartAt = time.Now()
			}
			if !user.SetupFinished {
				user.SetupFinished = true
				go c.CRMService.SendEvent(user, constants.EVENT_USER_SETUP_FINISHED, map[string]any{})
			}

		case "trialing":
			user.SubscriptionID = subs.ID
			user.SubscriptionStatus = newStatus
			if user.TrialStartAt.IsZero() {
				user.TrialStartAt = time.Now()
			}

		case "past_due":
			// Keep SubscriptionID — Stripe is still retrying payment, plan limits stay valid
			user.SubscriptionID = subs.ID
			user.SubscriptionStatus = newStatus

		case "canceled":
			user.SubscriptionStatus = newStatus
			user.SubscriptionID = uuid.Nil

		case "unpaid":
			// Keep SubscriptionID — subscription still exists in Stripe, just unpaid
			user.SubscriptionID = subs.ID
			user.SubscriptionStatus = newStatus

		default:
			// incomplete, incomplete_expired, paused
			user.SubscriptionStatus = newStatus
			user.SubscriptionID = uuid.Nil
		}

		go c.CRMService.SendEvent(user, constants.EVENT_SUBSCRIPTION_UPDATED, map[string]any{
			"subscription_status": user.SubscriptionStatus,
			"subscription_id":     user.SubscriptionID.String(),
			"plan_name":           subs.Name,
		})

		if planChanged && user.SubscriptionID != uuid.Nil {
			changeEvent := constants.EVENT_SUBSCRIPTION_UPGRADED
			if subs.MonthlyPrice < oldMonthlyPrice {
				changeEvent = constants.EVENT_SUBSCRIPTION_DOWNGRADED
			}
			go c.CRMService.SendEvent(user, changeEvent, map[string]any{
				"old_plan_id":       oldSubID.String(),
				"new_plan_id":       subs.ID.String(),
				"new_plan_name":     subs.Name,
				"old_monthly_price": oldMonthlyPrice,
				"new_monthly_price": subs.MonthlyPrice,
			})
		}

		if _, err := c.DB.UpdateUser(user); err != nil {
			return fmt.Errorf("failed to update user %s after subscription change: %w", user.ID, err)
		}

		c.TrackUserEvent(user, "subscription_updated", posthog.NewProperties().
			Set("status", user.SubscriptionStatus).
			Set("stripe_status", string(sub.Status)).
			Set("plan_name", subs.Name).
			Set("price_id", sub.Items.Data[0].Price.ID))
		c.IdentifyUser(user)

	case "invoice.payment_succeeded":
		var invoice stripe.Invoice
		if err := json.Unmarshal(event.Data.Raw, &invoice); err != nil {
			slog.Error("webhook: failed to unmarshal invoice", "error", err, "event_id", event.ID)
			return nil
		}
		slog.Info("invoice payment succeeded", "customer_id", invoice.Customer.ID)

		user, err := c.DB.GetUserByStripeCustomerID(invoice.Customer.ID)
		if err != nil {
			return fmt.Errorf("user not found for stripe customer %s: %w", invoice.Customer.ID, err)
		}

		user.SubscriptionStatus = "active"

		// Restore SubscriptionID if it was cleared by a prior canceled/inactive event.
		// Look up plan from invoice line items (v82: Pricing.PriceDetails.Price).
		if user.SubscriptionID == uuid.Nil && invoice.Lines != nil && len(invoice.Lines.Data) > 0 {
			lineItem := invoice.Lines.Data[0]
			if lineItem.Pricing != nil && lineItem.Pricing.PriceDetails != nil && lineItem.Pricing.PriceDetails.Price != "" {
				if subs, err := c.DB.GetSubscriptionByPriceID(lineItem.Pricing.PriceDetails.Price); err == nil {
					user.SubscriptionID = subs.ID
				}
			}
		}

		if !user.SetupFinished {
			user.SetupFinished = true
			go c.CRMService.SendEvent(user, constants.EVENT_USER_SETUP_FINISHED, map[string]any{})
		}

		go c.CRMService.SendEvent(user, constants.EVENT_SUBSCRIPTION_UPDATED, map[string]any{
			"subscription_status": user.SubscriptionStatus,
			"subscription_id":     user.SubscriptionID.String(),
		})

		go c.CRMService.SendEvent(user, constants.EVENT_PAYMENT_SUCCEEDED, map[string]any{
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
			go c.CRMService.SendEvent(user, constants.EVENT_SUBSCRIPTION_RENEWED, map[string]any{
				"subscription_id": user.SubscriptionID.String(),
				"amount_paid":     invoice.AmountPaid,
				"currency":        string(invoice.Currency),
				"invoice_id":      invoice.ID,
			})
		}

		if _, err := c.DB.UpdateUser(user); err != nil {
			return fmt.Errorf("failed to update user %s after payment succeeded: %w", user.ID, err)
		}

		c.TrackUserEvent(user, "payment_succeeded", posthog.NewProperties().
			Set("amount_paid", invoice.AmountPaid).
			Set("currency", string(invoice.Currency)).
			Set("invoice_id", invoice.ID).
			Set("billing_reason", string(invoice.BillingReason)))
		c.IdentifyUser(user)

	case "invoice.payment_failed":
		var invoice stripe.Invoice
		if err := json.Unmarshal(event.Data.Raw, &invoice); err != nil {
			slog.Error("webhook: failed to unmarshal invoice", "error", err, "event_id", event.ID)
			return nil
		}
		slog.Info("invoice payment failed", "customer_id", invoice.Customer.ID,
			"attempt_count", invoice.AttemptCount, "next_attempt", invoice.NextPaymentAttempt)

		user, err := c.DB.GetUserByStripeCustomerID(invoice.Customer.ID)
		if err != nil {
			return fmt.Errorf("user not found for stripe customer %s: %w", invoice.Customer.ID, err)
		}

		// Don't change subscription status here — let customer.subscription.updated
		// handle the status transition. Stripe sends both events, and the subscription
		// event has the authoritative status (past_due on first failure, unpaid after
		// exhausting retries). Changing status here would be premature.

		go c.CRMService.SendEvent(user, constants.EVENT_PAYMENT_FAILED, map[string]any{
			"subscription_status": user.SubscriptionStatus,
			"subscription_id":     user.SubscriptionID.String(),
			"pay_invoice_url":     invoice.HostedInvoiceURL,
			"attempt_count":       invoice.AttemptCount,
		})

		c.TrackUserEvent(user, "payment_failed", posthog.NewProperties().
			Set("invoice_url", invoice.HostedInvoiceURL).
			Set("amount_due", invoice.AmountDue).
			Set("attempt_count", invoice.AttemptCount))
		c.IdentifyUser(user)

	case "invoice.payment_action_required":
		var invoice stripe.Invoice
		if err := json.Unmarshal(event.Data.Raw, &invoice); err != nil {
			slog.Error("webhook: failed to unmarshal invoice", "error", err, "event_id", event.ID)
			return nil
		}
		slog.Info("invoice payment action required", "customer_id", invoice.Customer.ID)

		user, err := c.DB.GetUserByStripeCustomerID(invoice.Customer.ID)
		if err != nil {
			return fmt.Errorf("user not found for stripe customer %s: %w", invoice.Customer.ID, err)
		}

		go c.CRMService.SendEvent(user, constants.EVENT_PAYMENT_USER_ACTION_REQUIRED, map[string]any{
			"subscription_status": user.SubscriptionStatus,
			"subscription_id":     user.SubscriptionID.String(),
			"pay_invoice_url":     invoice.HostedInvoiceURL,
		})

		c.TrackUserEvent(user, "payment_action_required", posthog.NewProperties().
			Set("invoice_url", invoice.HostedInvoiceURL))
		c.IdentifyUser(user)

	case "invoice.upcoming":
		var invoice stripe.Invoice
		if err := json.Unmarshal(event.Data.Raw, &invoice); err != nil {
			slog.Error("webhook: failed to unmarshal invoice", "error", err, "event_id", event.ID)
			return nil
		}
		if invoice.Customer == nil {
			return nil
		}
		user, err := c.DB.GetUserByStripeCustomerID(invoice.Customer.ID)
		if err != nil {
			return fmt.Errorf("user not found for stripe customer %s: %w", invoice.Customer.ID, err)
		}

		go c.CRMService.SendEvent(user, constants.EVENT_INVOICE_UPCOMING, map[string]any{
			"amount_due":   invoice.AmountDue,
			"currency":     string(invoice.Currency),
			"period_start": invoice.PeriodStart,
			"period_end":   invoice.PeriodEnd,
			"invoice_url":  invoice.HostedInvoiceURL,
		})

		c.TrackUserEvent(user, "invoice_upcoming", posthog.NewProperties().
			Set("amount_due", invoice.AmountDue).
			Set("currency", string(invoice.Currency)))

	case "customer.subscription.deleted":
		var sub stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
			slog.Error("webhook: failed to unmarshal subscription", "error", err, "event_id", event.ID)
			return nil
		}
		slog.Info("subscription canceled", "customer_id", sub.Customer.ID)

		user, err := c.DB.GetUserByStripeCustomerID(sub.Customer.ID)
		if err != nil {
			return fmt.Errorf("user not found for stripe customer %s: %w", sub.Customer.ID, err)
		}

		user.SubscriptionStatus = "canceled"
		user.SubscriptionID = uuid.Nil

		go c.CRMService.SendEvent(user, constants.EVENT_SUBSCRIPTION_CANCELLED, map[string]any{})

		if _, err := c.DB.UpdateUser(user); err != nil {
			return fmt.Errorf("failed to update user %s after subscription deleted: %w", user.ID, err)
		}

		c.TrackUserEvent(user, "subscription_canceled", posthog.NewProperties().
			Set("subscription_id", sub.ID))
		c.IdentifyUser(user)

	case "payment_method.attached":
		var pm stripe.PaymentMethod
		if err := json.Unmarshal(event.Data.Raw, &pm); err != nil {
			slog.Error("webhook: failed to unmarshal payment method", "error", err, "event_id", event.ID)
			return nil
		}

		user, err := c.DB.GetUserByStripeCustomerID(pm.Customer.ID)
		if err != nil {
			return fmt.Errorf("user not found for stripe customer %s: %w", pm.Customer.ID, err)
		}

		go c.CRMService.SendEvent(user, constants.EVENT_PAYMENT_METHOD_ATTACHED, map[string]any{})

		c.TrackUserEvent(user, "payment_method_attached", posthog.NewProperties().
			Set("payment_method_type", string(pm.Type)))

	case "payment_method.detached":
		var pm stripe.PaymentMethod
		if err := json.Unmarshal(event.Data.Raw, &pm); err != nil {
			slog.Error("webhook: failed to unmarshal payment method", "error", err, "event_id", event.ID)
			return nil
		}

		user, err := c.DB.GetUserByStripeCustomerID(pm.Customer.ID)
		if err != nil {
			return fmt.Errorf("user not found for stripe customer %s: %w", pm.Customer.ID, err)
		}

		go c.CRMService.SendEvent(user, constants.EVENT_PAYMENT_METHOD_DETACHED, map[string]any{})

		c.TrackUserEvent(user, "payment_method_detached", posthog.NewProperties().
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

		if err := c.AddAICredits(user, packSize, "purchase", cs.ID); err != nil {
			return fmt.Errorf("failed to add %d credits for user %s: %w", packSize, userID, err)
		}

		slog.Info("credits purchased", "user_id", userID, "pack_size", packSize, "session_id", cs.ID)

		go c.CRMService.SendEvent(user, constants.EVENT_CREDITS_PURCHASED, map[string]any{
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
			user, err := c.DB.GetUserByStripeCustomerID(customerID)
			if err == nil {
				go c.CRMService.SendEvent(user, constants.EVENT_UNHANDLED_EVENT, map[string]any{
					"from":       "stripe-webhook",
					"event_type": event.Type,
				})
				return nil
			}
		}

		go c.CRMService.SendEvent(&models.User{}, constants.EVENT_UNHANDLED_EVENT, map[string]any{
			"from":       "stripe-webhook",
			"event_type": event.Type,
		})
	}

	return nil
}
