package controllers

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/posthog/posthog-go"
	"github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/webhook"
)

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
		slog.Error(fmt.Sprintf("⚠️  Webhook error while parsing basic request. %v\n", err))
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	endpointSecret := c.SecretManager.GetSecret("STRIPE_WEBHOOK_SECRET")
	signatureHeader := req.Header.Get("Stripe-Signature")
	event, err = webhook.ConstructEvent(payload, signatureHeader, endpointSecret)
	if err != nil {
		slog.Error(fmt.Sprintf("⚠️  Webhook signature verification failed. %v\n", err))
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	switch event.Type {

	case "customer.subscription.created":
		var sub stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &sub); err == nil {
			slog.Info("subscription created", "customer_id", sub.Customer.ID, "status", sub.Status)

			user, err := c.DB.GetUserByStripeCustomerID(sub.Customer.ID)
			if err != nil {
				break
			}

			if !user.TrialUsed {
				user.SubscriptionStatus = "trial"
				user.TrialStartAt = time.Now()
				go c.CRMService.SendEvent(user, constants.BREVO_EVENT_TRIAL_STARTED, map[string]any{
					"subscription_status": user.SubscriptionStatus,
					"subscription_id":     user.SubscriptionID.String(),
				})
			} else {
				user.SubscriptionStatus = "active"
				user.SubscriptionStartAt = time.Now()
			}

			go c.CRMService.SendEvent(user, constants.BREVO_EVENT_SUBSCRIPTION_UPDATED, map[string]any{
				"subscription_status": user.SubscriptionStatus,
				"subscription_id":     user.SubscriptionID.String(),
			})

			subs, err := c.DB.GetSubscriptionByPriceID(sub.Items.Data[0].Price.ID)
			if err != nil {
				break
			}
			user.SubscriptionID = subs.ID

			user.TrialUsed = true

			if !user.SetupFinished {
				user.SetupFinished = true
				go c.CRMService.SendEvent(user, constants.BREVO_EVENT_USER_SETUP_FINISHED, map[string]any{})
			}

			if _, err := c.DB.UpdateUser(user); err != nil {
				break
			}

			c.TrackUserEvent(user, "subscription_created", posthog.NewProperties().
				Set("status", user.SubscriptionStatus).
				Set("plan_name", subs.Name).
				Set("price_id", sub.Items.Data[0].Price.ID))
			c.IdentifyUser(user)
		}

	case "customer.subscription.updated":
		var sub stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &sub); err == nil {
			slog.Info("subscription updated", "customer_id", sub.Customer.ID, "status", sub.Status)

			user, err := c.DB.GetUserByStripeCustomerID(sub.Customer.ID)
			if err != nil {
				break
			}

			subs, err := c.DB.GetSubscriptionByPriceID(sub.Items.Data[0].Price.ID)
			if err != nil {
				break
			}
			user.SubscriptionID = subs.ID

			switch sub.Status {
			case "active":
				user.SubscriptionStatus = "active"
				user.SubscriptionStartAt = time.Now()
				if !user.SetupFinished {
					user.SetupFinished = true
					go c.CRMService.SendEvent(user, constants.BREVO_EVENT_USER_SETUP_FINISHED, map[string]any{})
					if user.TrialUsed {
						go c.CRMService.SendEvent(user, constants.BREVO_EVENT_TRIAL_ENDED, map[string]any{})
					}
					if _, err := c.DB.UpdateUser(user); err != nil {
						slog.Error("failed to persist setup_finished flag", "user_id", user.ID, "error", err)
					}
				}
			case "past_due":
				user.SubscriptionStatus = "past_due"
				user.SubscriptionID = uuid.Nil // Remove subscription association

			case "canceled":
				user.SubscriptionStatus = "canceled"
				user.SubscriptionID = uuid.Nil // Remove subscription association

			case "unpaid":
				user.SubscriptionStatus = "unpaid"
				user.SubscriptionID = uuid.Nil // Remove subscription association

			default:
				user.SubscriptionStatus = "inactive"
				user.SubscriptionID = uuid.Nil // Remove subscription association

			}

			go c.CRMService.SendEvent(user, constants.BREVO_EVENT_SUBSCRIPTION_UPDATED, map[string]any{
				"subscription_status": user.SubscriptionStatus,
				"subscription_id":     user.SubscriptionID.String(),
			})

			if _, err := c.DB.UpdateUser(user); err != nil {
				break
			}

			c.TrackUserEvent(user, "subscription_updated", posthog.NewProperties().
				Set("status", user.SubscriptionStatus).
				Set("plan_name", subs.Name).
				Set("price_id", sub.Items.Data[0].Price.ID))
			c.IdentifyUser(user)
		}

	case "invoice.payment_succeeded":
		var invoice stripe.Invoice
		if err := json.Unmarshal(event.Data.Raw, &invoice); err == nil {
			slog.Info("invoice payment succeeded", "customer_id", invoice.Customer.ID)

			user, err := c.DB.GetUserByStripeCustomerID(invoice.Customer.ID)
			if err != nil {
				break
			}

			user.SubscriptionStatus = "active"
			if !user.SetupFinished {
				user.SetupFinished = true
				go c.CRMService.SendEvent(user, constants.BREVO_EVENT_USER_SETUP_FINISHED, map[string]any{})
				if _, err := c.DB.UpdateUser(user); err != nil {
					slog.Error("failed to persist setup_finished flag", "user_id", user.ID, "error", err)
				}
			}

			go c.CRMService.SendEvent(user, constants.BREVO_EVENT_SUBSCRIPTION_UPDATED, map[string]any{
				"subscription_status": user.SubscriptionStatus,
				"subscription_id":     user.SubscriptionID.String(),
			})

			if _, err := c.DB.UpdateUser(user); err != nil {
				break
			}

			c.TrackUserEvent(user, "payment_succeeded", posthog.NewProperties().
				Set("amount_paid", invoice.AmountPaid).
				Set("currency", string(invoice.Currency)).
				Set("invoice_id", invoice.ID))
			c.IdentifyUser(user)
		}

	case "invoice.payment_failed":
		var invoice stripe.Invoice
		if err := json.Unmarshal(event.Data.Raw, &invoice); err == nil {
			slog.Info("invoice payment failed", "customer_id", invoice.Customer.ID)

			user, err := c.DB.GetUserByStripeCustomerID(invoice.Customer.ID)
			if err != nil {
				break
			}

			user.SubscriptionStatus = "unpaid"

			go c.CRMService.SendEvent(user, constants.BREVO_EVENT_PAYMENT_FAILED, map[string]any{
				"subscription_status": user.SubscriptionStatus,
				"subscription_id":     user.SubscriptionID.String(),
				"pay_invoice_url":     invoice.HostedInvoiceURL,
			})

			if _, err := c.DB.UpdateUser(user); err != nil {
				break
			}
			c.TrackUserEvent(user, "payment_failed", posthog.NewProperties().
				Set("invoice_url", invoice.HostedInvoiceURL).
				Set("amount_due", invoice.AmountDue).
				Set("attempt_count", invoice.AttemptCount))
			c.IdentifyUser(user)
		}

	case "invoice.payment_action_required":
		var invoice stripe.Invoice
		if err := json.Unmarshal(event.Data.Raw, &invoice); err == nil {
			slog.Info("invoice payment action required", "customer_id", invoice.Customer.ID)

			user, err := c.DB.GetUserByStripeCustomerID(invoice.Customer.ID)
			if err != nil {
				break
			}

			go c.CRMService.SendEvent(user, constants.BREVO_EVENT_PAYMENT_USER_ACTION_REQUIRED, map[string]any{
				"subscription_status": user.SubscriptionStatus,
				"subscription_id":     user.SubscriptionID.String(),
				"pay_invoice_url":     invoice.HostedInvoiceURL,
			})

			c.TrackUserEvent(user, "payment_action_required", posthog.NewProperties().
				Set("invoice_url", invoice.HostedInvoiceURL))
		}

	case "customer.subscription.deleted":
		var sub stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &sub); err == nil {
			slog.Info("subscription canceled", "customer_id", sub.Customer.ID)

			user, err := c.DB.GetUserByStripeCustomerID(sub.Customer.ID)
			if err != nil {
				break
			}

			// Downgrade to free plan instead of fully canceling
			freePlan, freeErr := c.DB.GetSubscriptionByName("Free")
			if freeErr == nil {
				user.SubscriptionID = freePlan.ID
				user.SubscriptionStatus = "free"
			} else {
				user.SubscriptionStatus = "canceled"
				user.SubscriptionID = uuid.Nil
			}

			go c.CRMService.SendEvent(user, constants.BREVO_EVENT_SUBSCRIPTION_CANCELLED, map[string]any{})

			if _, err := c.DB.UpdateUser(user); err != nil {
				break
			}

			c.TrackUserEvent(user, "subscription_canceled", posthog.NewProperties().
				Set("subscription_id", sub.ID))
			c.IdentifyUser(user)
		}

	case "payment_method.attached":
		var pm stripe.PaymentMethod
		if err := json.Unmarshal(event.Data.Raw, &pm); err == nil {
			user, err := c.DB.GetUserByStripeCustomerID(pm.Customer.ID)
			if err != nil {
				slog.Error(fmt.Sprintf("Error finding user by customer ID: %v\n", err))
				break
			}

			go c.CRMService.SendEvent(user, constants.BREVO_EVENT_PAYMENT_METHOD_ATTACHED, map[string]any{})

			c.TrackUserEvent(user, "payment_method_attached", posthog.NewProperties().
				Set("payment_method_type", string(pm.Type)))
		}
	case "payment_method.detached":
		var pm stripe.PaymentMethod
		if err := json.Unmarshal(event.Data.Raw, &pm); err == nil {
			user, err := c.DB.GetUserByStripeCustomerID(pm.Customer.ID)
			if err != nil {
				slog.Error(fmt.Sprintf("Error finding user by customer ID: %v\n", err))
				break
			}

			go c.CRMService.SendEvent(user, constants.BREVO_EVENT_PAYMENT_METHOD_DETTACHED, map[string]any{})

			c.TrackUserEvent(user, "payment_method_detached", posthog.NewProperties().
				Set("payment_method_type", string(pm.Type)))
		}

	default:
		slog.Warn(fmt.Sprintf("Unhandled event type: %s\n", event.Type))

		var unmarshalledData map[string]any
		if err := json.Unmarshal(event.Data.Raw, &unmarshalledData); err != nil {
			slog.Error(fmt.Sprintf("Error unmarshalling event data: %v\n", err))
			break
		}

		customerID, ok := unmarshalledData["customer"].(string)
		if ok && customerID != "" {
			user, err := c.DB.GetUserByStripeCustomerID(customerID)
			if err == nil {
				go c.CRMService.SendEvent(user, constants.BREVO_EVENT_UNHANDLED_EVENT, map[string]any{
					"from":       "stripe-webhook",
					"event_type": event.Type,
				})
				break
			}
		}

		go c.CRMService.SendEvent(&models.User{}, constants.BREVO_EVENT_UNHANDLED_EVENT, map[string]any{
			"from":       "stripe-webhook",
			"event_type": event.Type,
		})
	}

	w.WriteHeader(http.StatusOK)
}
