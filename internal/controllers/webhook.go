package controllers

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
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

	endpointSecret := os.Getenv("STRIPE_WEBHOOK_SECRET")
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
			log.Printf("Subscription created for customer %s. Status: %s", sub.Customer.ID, sub.Status)

			user, err := c.DB.GetUserByStripeCustomerID(sub.Customer.ID)
			if err != nil {
				break
			}

			if !user.TrialUsed {
				user.SubscriptionStatus = "trial"
				go c.CRMService.SendEvent(user, constants.BREVO_EVENT_SUBSCRIPTION_UPDATED, map[string]any{
					"subscription_status": user.SubscriptionStatus,
					"subscription_id":     user.SubscriptionID.String(),
				})
			} else {
				user.SubscriptionStatus = "active"
			}

			subs, err := c.DB.GetSubscriptionByPriceID(sub.Items.Data[0].Price.ID)
			if err != nil {
				break
			}
			user.SubscriptionID = subs.ID

			user.TrialUsed = true

			if _, err := c.DB.UpdateUser(user); err != nil {
				break
			}
		}

	case "customer.subscription.updated":
		var sub stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &sub); err == nil {
			log.Printf("Subscription updated for customer %s. Status: %s", sub.Customer.ID, sub.Status)

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
		}

	case "invoice.payment_succeeded":
		var invoice stripe.Invoice
		if err := json.Unmarshal(event.Data.Raw, &invoice); err == nil {
			log.Printf("Invoice payment succeeded for customer %s", invoice.Customer.ID)

			user, err := c.DB.GetUserByStripeCustomerID(invoice.Customer.ID)
			if err != nil {
				break
			}

			user.SubscriptionStatus = "active"

			go c.CRMService.SendEvent(user, constants.BREVO_EVENT_SUBSCRIPTION_UPDATED, map[string]any{
				"subscription_status": user.SubscriptionStatus,
				"subscription_id":     user.SubscriptionID.String(),
			})

			if _, err := c.DB.UpdateUser(user); err != nil {
				break
			}
			// TODO: DB'de invoice'u ödenmiş olarak işaretle, kullanıcının erişimini aktif tut
		}

	case "invoice.payment_failed":
		var invoice stripe.Invoice
		if err := json.Unmarshal(event.Data.Raw, &invoice); err == nil {
			log.Printf("Invoice payment failed for customer %s", invoice.Customer.ID)

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
			// TODO: DB'de başarısız ödeme kaydı tut, kullanıcıya uyarı gönder veya erişimi kısıtla
		}

	case "invoice.payment_action_required":
		var invoice stripe.Invoice
		if err := json.Unmarshal(event.Data.Raw, &invoice); err == nil {
			log.Printf("Invoice payment failed for customer %s", invoice.Customer.ID)

			user, err := c.DB.GetUserByStripeCustomerID(invoice.Customer.ID)
			if err != nil {
				break
			}

			go c.CRMService.SendEvent(user, constants.BREVO_EVENT_PAYMENT_USER_ACTION_REQUIRED, map[string]any{
				"subscription_status": user.SubscriptionStatus,
				"subscription_id":     user.SubscriptionID.String(),
				"pay_invoice_url":     invoice.HostedInvoiceURL,
			})
		}

	case "customer.subscription.deleted":
		var sub stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &sub); err == nil {
			log.Printf("Subscription canceled for customer %s", sub.Customer.ID)

			user, err := c.DB.GetUserByStripeCustomerID(sub.Customer.ID)
			if err != nil {
				break
			}

			user.SubscriptionStatus = "canceled"
			user.SubscriptionID = uuid.Nil // Remove subscription association

			go c.CRMService.SendEvent(user, constants.BREVO_EVENT_SUBSCRIPTION_CANCELLED, map[string]any{})

			if _, err := c.DB.UpdateUser(user); err != nil {
				break
			}
			// TODO: DB'de subscription'ı iptal et, kullanıcının erişimini kapat
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
					"event_type": event.Type,
				})
				break
			}
		}

		go c.CRMService.SendEvent(&models.User{}, constants.BREVO_EVENT_UNHANDLED_EVENT, map[string]any{
			"event_type": event.Type,
		})
	}

	w.WriteHeader(http.StatusOK)
}
