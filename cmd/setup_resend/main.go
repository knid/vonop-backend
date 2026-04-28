// cmd/setup_resend registers event definitions in Resend's events API for
// every EVENT_* constant in internal/constants/events.go. Idempotent —
// run once after creating the Resend account.
//
// Usage:
//   RESEND_API_KEY=re_xxx go run ./cmd/setup_resend
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/knid/wemob/internal/constants"
)

func main() {
	apiKey := os.Getenv("RESEND_API_KEY")
	if apiKey == "" {
		log.Fatal("RESEND_API_KEY is required")
	}

	events := eventNames()
	log.Printf("Registering %d events with Resend", len(events))

	client := &http.Client{Timeout: 10 * time.Second}
	created, skipped, failed := 0, 0, 0
	for _, name := range events {
		status, err := registerEvent(client, apiKey, name)
		switch {
		case err != nil:
			failed++
			log.Printf("FAIL %s: %v", name, err)
		case status == "created":
			created++
			log.Printf("CREATED %s", name)
		case status == "exists":
			skipped++
		}
	}
	log.Printf("Done. created=%d skipped=%d failed=%d", created, skipped, failed)
}

// eventNames mirrors internal/constants/events.go. Keep in sync when adding
// new EVENT_* constants — the script registers exactly this list.
func eventNames() []string {
	return []string{
		constants.EVENT_NEW_USER,
		constants.EVENT_NEW_LOGIN,
		constants.EVENT_USER_DELETED,
		constants.EVENT_USER_SETUP_FINISHED,
		constants.EVENT_EMAIL_VERIFIED,
		constants.EVENT_PASSWORD_RESET,
		constants.EVENT_PASSWORD_RESET_REQUESTED,
		constants.EVENT_EMAIL_VERIFICATION_REQUESTED,
		constants.EVENT_NEW_TICKET,
		constants.EVENT_NEW_PROJECT,
		constants.EVENT_PROJECT_UPDATED,
		constants.EVENT_PROJECT_DELETED,
		constants.EVENT_APP_DRAFT_CREATED,
		constants.EVENT_APP_GENERATION_STARTED,
		constants.EVENT_APP_EXPORTED_ZIP,
		constants.EVENT_SNAPSHOT_CREATED,
		constants.EVENT_SNAPSHOT_RESTORED,
		constants.EVENT_INTEGRATION_CONNECTED,
		constants.EVENT_INTEGRATION_DISCONNECTED,
		constants.EVENT_APP_GENERATION_COMPLETED,
		constants.EVENT_APP_CHAT_SENT,
		constants.EVENT_APP_CHAT_COMPLETED,
		constants.EVENT_APP_CHAT_FAILED,
		constants.EVENT_BUILD_STARTED_IOS,
		constants.EVENT_BUILD_STARTED_ANDROID,
		constants.EVENT_BUILD_FINISHED_IOS,
		constants.EVENT_BUILD_FINISHED_ANDROID,
		constants.EVENT_BUILD_ERROR_IOS,
		constants.EVENT_BUILD_ERROR_ANDROID,
		constants.EVENT_USER_APPLE_DEPLOYMENT_STARTED,
		constants.EVENT_USER_APPLE_DEPLOYMENT_FINISHED,
		constants.EVENT_USER_APPLE_DEPLOYMENT_FAILED,
		constants.EVENT_USER_ANDROID_DEPLOYMENT_STARTED,
		constants.EVENT_USER_ANDROID_DEPLOYMENT_FINISHED,
		constants.EVENT_USER_ANDROID_DEPLOYMENT_FAILED,
		constants.EVENT_PENDING_CONNECTION_NUDGE,
		constants.EVENT_WEB_DEPLOYMENT_SUCCEEDED,
		constants.EVENT_WEB_DEPLOYMENT_FAILED,
		constants.EVENT_WEB_UNDEPLOYED,
		constants.EVENT_MOBILE_DEPLOYMENT_SUCCEEDED,
		constants.EVENT_MOBILE_DEPLOYMENT_FAILED,
		constants.EVENT_MOBILE_UNDEPLOYED,
		constants.EVENT_DOMAIN_ADDED,
		constants.EVENT_DOMAIN_VERIFIED,
		constants.EVENT_DOMAIN_REMOVED,
		constants.EVENT_GITHUB_EXPORTED,
		constants.EVENT_GITHUB_PUSHED,
		constants.EVENT_SUBSCRIPTION_CREATED,
		constants.EVENT_SUBSCRIPTION_UPDATED,
		constants.EVENT_SUBSCRIPTION_RENEWED,
		constants.EVENT_SUBSCRIPTION_UPGRADED,
		constants.EVENT_SUBSCRIPTION_DOWNGRADED,
		constants.EVENT_SUBSCRIPTION_CANCELLED,
		constants.EVENT_SUBSCRIPTION_QUOTA_REACHED,
		constants.EVENT_SUBSCRIPTION_NEED,
		constants.EVENT_PAYMENT_METHOD_ATTACHED,
		constants.EVENT_PAYMENT_METHOD_DETACHED,
		constants.EVENT_PAYMENT_SUCCEEDED,
		constants.EVENT_PAYMENT_FAILED,
		constants.EVENT_PAYMENT_USER_ACTION_REQUIRED,
		constants.EVENT_INVOICE_UPCOMING,
		constants.EVENT_CHECKOUT_STARTED,
		constants.EVENT_CREDIT_PURCHASE_STARTED,
		constants.EVENT_UNHANDLED_EVENT,
		constants.EVENT_AI_CREDITS_LOW,
		constants.EVENT_AI_CREDITS_DEPLETED,
		constants.EVENT_CREDITS_PURCHASED,
		constants.EVENT_CREDITS_GRANTED,
		constants.EVENT_CREDITS_REFUNDED,
		constants.EVENT_CREDITS_THRESHOLD_CROSSED,
		constants.EVENT_CREDITS_OVERDRAFT_STARTED,
		constants.EVENT_CREDITS_GATEWAY_BLOCKED,
		constants.EVENT_REFERRAL_SIGNUP_BONUS_RECEIVED,
		constants.EVENT_REFERRAL_SUBSCRIPTION_REWARD_RECEIVED,
		constants.EVENT_REFERRAL_CODE_APPLIED,
		constants.EVENT_ORGANIZATION_CREATED,
		constants.EVENT_ORGANIZATION_MEMBER_INVITED,
		constants.EVENT_DASHBOARD_CREATED,
		constants.EVENT_STORE_CHAT_SENT,
	}
}

func registerEvent(client *http.Client, apiKey, name string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"name":   name,
		"schema": "{}", // permissive — payload shape varies per event
	})
	req, err := http.NewRequest(http.MethodPost, "https://api.resend.com/events/create", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 300 {
		return "created", nil
	}
	respBody, _ := io.ReadAll(resp.Body)
	bodyStr := string(respBody)
	// Treat already-exists as a skip.
	if resp.StatusCode == http.StatusConflict || resp.StatusCode == http.StatusUnprocessableEntity ||
		strings.Contains(strings.ToLower(bodyStr), "already exists") {
		return "exists", nil
	}
	return "", fmt.Errorf("%s: %s", resp.Status, bodyStr)
}
