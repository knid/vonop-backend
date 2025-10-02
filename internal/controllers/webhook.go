package controllers

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/webhook"
)

func (c *Controller) HandleWebhook(w http.ResponseWriter, req *http.Request) {
	const MaxBodyBytes = int64(65536)
	req.Body = http.MaxBytesReader(w, req.Body, MaxBodyBytes)
	payload, err := io.ReadAll(req.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading request body: %v\n", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	event := stripe.Event{}

	if err := json.Unmarshal(payload, &event); err != nil {
		fmt.Fprintf(os.Stderr, "⚠️  Webhook error while parsing basic request. %v\n", err.Error())
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Replace this endpoint secret with your endpoint's unique secret
	// If you are testing with the CLI, find the secret by running 'stripe listen'
	// If you are using an endpoint defined with the API or dashboard, look in your webhook settings
	// at https://dashboard.stripe.com/webhooks
	endpointSecret := "whsec_8f4158137d1e9de182194075c23756f4cd4ca9a4e9509983cfc78bae53c3a249"
	signatureHeader := req.Header.Get("Stripe-Signature")
	event, err = webhook.ConstructEvent(payload, signatureHeader, endpointSecret)
	if err != nil {
		fmt.Fprintf(os.Stderr, "⚠️  Webhook signature verification failed. %v\n", err)
		w.WriteHeader(http.StatusBadRequest) // Return a 400 error on a bad signature
		return
	}
	// Unmarshal the event data into an appropriate struct depending on its Type
	switch event.Type {
	/* case "payment_intent.succeeded":
	var paymentIntent stripe.PaymentIntent
	if err := json.Unmarshal(event.Data.Raw, &paymentIntent); err == nil {
		log.Printf("Successful payment for %d.", paymentIntent.Amount)
		// TODO: DB'de başarılı ödeme işlemini kaydet
	} */

	/* case "payment_method.attached":
	var paymentMethod stripe.PaymentMethod
	if err := json.Unmarshal(event.Data.Raw, &paymentMethod); err == nil {
		// TODO: DB'ye yeni eklenen payment method bilgisini kaydet
	} */

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

			switch sub.Status {
			case "active":
				user.SubscriptionStatus = "active"
			case "past_due":
				user.SubscriptionStatus = "past_due"
			case "canceled":
				user.SubscriptionStatus = "canceled"
			case "unpaid":
				user.SubscriptionStatus = "unpaid"
			default:
				user.SubscriptionStatus = "inactive"
			}

			subs, err := c.DB.GetSubscriptionByPriceID(sub.Items.Data[0].Price.ID)
			if err != nil {
				break
			}
			user.SubscriptionID = subs.ID

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

			if _, err := c.DB.UpdateUser(user); err != nil {
				break
			}
			// TODO: DB'de başarısız ödeme kaydı tut, kullanıcıya uyarı gönder veya erişimi kısıtla
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

			if _, err := c.DB.UpdateUser(user); err != nil {
				break
			}
			// TODO: DB'de subscription'ı iptal et, kullanıcının erişimini kapat
		}

	default:
		fmt.Fprintf(os.Stderr, "Unhandled event type: %s\n", event.Type)
	}

	w.WriteHeader(http.StatusOK)
}
