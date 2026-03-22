package views

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/utils"
	"github.com/stripe/stripe-go/v82/webhook"
)

// POST /webhooks/store/stripe/{storeId}
func (v *HttpViews) HandleStoreStripeWebhook(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeId")
	if storeID == "" {
		utils.JSONErrorResponse(w, "store id is required", http.StatusBadRequest)
		return
	}

	// Verify the store exists
	store, err := v.Controller.DB.GetStoreByID(storeID)
	if err != nil || store == nil {
		utils.JSONErrorResponse(w, "store not found", http.StatusNotFound)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		utils.JSONErrorResponse(w, "failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Verify Stripe webhook signature — reject if no webhook_secret is configured
	integrations, _ := v.Controller.DB.GetAppIntegrationsByProjectID(store.AppProjectID.String())
	signatureVerified := false
	for _, integ := range integrations {
		if integ.Provider == "stripe" && integ.Status == "connected" {
			var creds map[string]any
			if err := json.Unmarshal(integ.Credentials, &creds); err == nil {
				if webhookSecret, ok := creds["webhook_secret"].(string); ok && webhookSecret != "" {
					sigHeader := r.Header.Get("Stripe-Signature")
					if _, err := webhook.ConstructEvent(body, sigHeader, webhookSecret); err != nil {
						slog.Error("Stripe webhook signature verification failed", "error", err, "storeId", storeID)
						utils.JSONErrorResponse(w, "invalid signature", http.StatusBadRequest)
						return
					}
					signatureVerified = true
					slog.Info("Stripe webhook signature verified", "storeId", storeID)
				}
			}
			break
		}
	}
	if !signatureVerified {
		slog.Warn("Stripe webhook rejected: no webhook_secret configured", "storeId", storeID)
		utils.JSONErrorResponse(w, "webhook signature verification not configured", http.StatusForbidden)
		return
	}

	var payload struct {
		Type string `json:"type"`
		Data struct {
			Object struct {
				ID            string `json:"id"`
				PaymentIntent string `json:"payment_intent"`
				Status        string `json:"status"`
				Metadata      struct {
					OrderID     string `json:"order_id"`
					OrderNumber string `json:"order_number"`
				} `json:"metadata"`
			} `json:"object"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &payload); err != nil {
		slog.Error("Failed to parse Stripe store webhook", "error", err, "storeId", storeID)
		utils.JSONErrorResponse(w, "invalid payload", http.StatusBadRequest)
		return
	}

	slog.Info("Store Stripe webhook received", "type", payload.Type, "storeId", storeID)

	switch payload.Type {
	case "checkout.session.completed", "payment_intent.succeeded":
		orderID := payload.Data.Object.Metadata.OrderID
		if orderID == "" {
			// Try order number
			orderNumber := payload.Data.Object.Metadata.OrderNumber
			if orderNumber != "" {
				order, err := v.Controller.DB.GetOrderByOrderNumber(orderNumber)
				if err == nil {
					orderID = order.ID.String()
				}
			}
		}

		if orderID != "" {
			order, err := v.Controller.DB.GetOrderByID(orderID)
			if err == nil {
				if order.StoreID != store.ID {
					slog.Warn("Stripe webhook order does not belong to store", "orderId", orderID, "orderStoreId", order.StoreID, "webhookStoreId", store.ID)
					break
				}
				order.PaymentStatus = "paid"
				order.Status = "confirmed"
				order.PaymentProvider = "stripe"
				order.PaymentID = payload.Data.Object.ID
				order.UpdatedAt = time.Now()
				v.Controller.DB.UpdateOrder(order)

				// Finalize order post-payment (inventory, cart, email)
				orderItems, _ := v.Controller.DB.GetOrderItemsByOrderID(order.ID.String())
				sessionID, _ := order.Metadata["session_id"].(string)
				v.Controller.FinalizeOrderPostPayment(store, order, orderItems, sessionID)

				slog.Info("Store order payment confirmed via Stripe", "orderId", orderID, "storeId", storeID)
			}
		}

	case "charge.refunded":
		orderID := payload.Data.Object.Metadata.OrderID
		if orderID != "" {
			order, err := v.Controller.DB.GetOrderByID(orderID)
			if err == nil {
				if order.StoreID != store.ID {
					slog.Warn("Stripe webhook refund order does not belong to store", "orderId", orderID, "orderStoreId", order.StoreID, "webhookStoreId", store.ID)
					break
				}
				order.PaymentStatus = "refunded"
				order.Status = "refunded"
				order.UpdatedAt = time.Now()
				v.Controller.DB.UpdateOrder(order)
				slog.Info("Store order refunded via Stripe", "orderId", orderID, "storeId", storeID)
			}
		}
	}

	// Always return 200 to acknowledge receipt
	w.WriteHeader(http.StatusOK)
}

// POST /webhooks/store/iyzico/{storeId}
func (v *HttpViews) HandleStoreIyzicoWebhook(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeId")
	if storeID == "" {
		utils.JSONErrorResponse(w, "store id is required", http.StatusBadRequest)
		return
	}

	// Verify the store exists
	store, err := v.Controller.DB.GetStoreByID(storeID)
	if err != nil || store == nil {
		utils.JSONErrorResponse(w, "store not found", http.StatusNotFound)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		utils.JSONErrorResponse(w, "failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Verify Iyzico webhook signature — reject if no webhook_secret is configured
	integrations, _ := v.Controller.DB.GetAppIntegrationsByProjectID(store.AppProjectID.String())
	signatureVerified := false
	for _, integ := range integrations {
		if integ.Provider == "iyzico" && integ.Status == "connected" {
			var creds map[string]any
			if err := json.Unmarshal(integ.Credentials, &creds); err == nil {
				if webhookSecret, ok := creds["webhook_secret"].(string); ok && webhookSecret != "" {
					sigHeader := r.Header.Get("X-IYZ-Signature")
					if sigHeader == "" {
						slog.Error("Iyzico webhook missing signature header", "storeId", storeID)
						utils.JSONErrorResponse(w, "missing signature", http.StatusBadRequest)
						return
					}
					mac := hmac.New(sha256.New, []byte(webhookSecret))
					mac.Write(body)
					expectedSig := hex.EncodeToString(mac.Sum(nil))
					if !hmac.Equal([]byte(sigHeader), []byte(expectedSig)) {
						slog.Error("Iyzico webhook signature verification failed", "storeId", storeID)
						utils.JSONErrorResponse(w, "invalid signature", http.StatusBadRequest)
						return
					}
					signatureVerified = true
					slog.Info("Iyzico webhook signature verified", "storeId", storeID)
				}
			}
			break
		}
	}
	if !signatureVerified {
		slog.Warn("Iyzico webhook rejected: no webhook_secret configured", "storeId", storeID)
		utils.JSONErrorResponse(w, "webhook signature verification not configured", http.StatusForbidden)
		return
	}

	// Iyzico webhook payload uses paymentConversationId (maps to our order ID set as conversationId)
	var payload struct {
		Status                string `json:"status"`
		PaymentID             string `json:"paymentId"`
		PaymentConversationID string `json:"paymentConversationId"`
		IyziReferenceCode     string `json:"iyziReferenceCode"`
		IyziEventType         string `json:"iyziEventType"`
		IyziPaymentID         string `json:"iyziPaymentId"`
		Token                 string `json:"token"`
	}

	if err := json.Unmarshal(body, &payload); err != nil {
		slog.Error("Failed to parse Iyzico store webhook", "error", err, "storeId", storeID)
		utils.JSONErrorResponse(w, "invalid payload", http.StatusBadRequest)
		return
	}

	slog.Info("Store Iyzico webhook received",
		"status", payload.Status, "storeId", storeID,
		"paymentConversationId", payload.PaymentConversationID,
		"eventType", payload.IyziEventType,
		"paymentId", payload.PaymentID)

	// paymentConversationId maps to order.ID (set as conversationId during checkout)
	orderLookupID := payload.PaymentConversationID
	if orderLookupID != "" {
		order, err := v.Controller.DB.GetOrderByID(orderLookupID)
		if err != nil {
			// Fallback: try as order number
			order, err = v.Controller.DB.GetOrderByOrderNumber(orderLookupID)
		}

		if err == nil && order != nil {
			if order.StoreID != store.ID {
				slog.Warn("Iyzico webhook order does not belong to store", "orderId", order.ID.String(), "orderStoreId", order.StoreID, "webhookStoreId", store.ID)
			} else if order.PaymentStatus == "paid" {
				slog.Info("Iyzico webhook: order already paid, skipping", "orderId", order.ID.String(), "storeId", storeID)
			} else {
				paymentID := payload.PaymentID
				if paymentID == "" {
					paymentID = payload.IyziPaymentID
				}

				switch payload.Status {
				case "SUCCESS":
					order.PaymentStatus = "paid"
					order.Status = "confirmed"
					order.PaymentProvider = "iyzico"
					order.PaymentID = paymentID
					order.UpdatedAt = time.Now()
					v.Controller.DB.UpdateOrder(order)

					// Finalize order post-payment (inventory, cart, email)
					orderItems, _ := v.Controller.DB.GetOrderItemsByOrderID(order.ID.String())
					sessionID, _ := order.Metadata["session_id"].(string)
					v.Controller.FinalizeOrderPostPayment(store, order, orderItems, sessionID)

					slog.Info("Store order payment confirmed via Iyzico webhook", "orderId", order.ID.String(), "storeId", storeID)

				case "FAILURE":
					order.PaymentStatus = "failed"
					order.UpdatedAt = time.Now()
					v.Controller.DB.UpdateOrder(order)
					slog.Info("Store order payment failed via Iyzico webhook", "orderId", order.ID.String(), "storeId", storeID)
				}
			}
		} else {
			slog.Warn("Iyzico webhook: order not found", "lookupId", orderLookupID, "storeId", storeID)
		}
	}

	// Always return 200 to acknowledge receipt
	w.WriteHeader(http.StatusOK)
}
