package controllers

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

func (c *Controller) CreateWebhookEndpoint(user *models.User, req requests.CreateWebhookRequest) (*responses.WebhookEndpointCreatedResponse, error) {
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := utils.ValidateFunc[requests.CreateWebhookRequest](req, validate); err != nil {
		return nil, err
	}

	// Validate events
	validEvents := map[string]bool{
		models.WebhookEventFormSubmitted:  true,
		models.WebhookEventBuildCompleted: true,
		models.WebhookEventBuildFailed:    true,
	}
	for _, event := range strings.Split(req.Events, ",") {
		if !validEvents[strings.TrimSpace(event)] {
			return nil, errors.New("invalid event: " + event)
		}
	}

	// Generate signing secret
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		return nil, errors.New("failed to generate secret")
	}
	secret := "whsec_" + hex.EncodeToString(secretBytes)

	endpoint := models.WebhookEndpoint{
		UserID: user.ID,
		URL:    req.URL,
		Secret: secret,
		Events: req.Events,
		Active: true,
	}

	created, err := c.DB.CreateWebhookEndpoint(&endpoint)
	if err != nil {
		return nil, err
	}

	return &responses.WebhookEndpointCreatedResponse{
		WebhookEndpointResponse: responses.WebhookEndpointResponse{
			ID:        created.ID,
			URL:       created.URL,
			Events:    created.Events,
			Active:    created.Active,
			CreatedAt: created.CreatedAt,
		},
		Secret: secret,
	}, nil
}

func (c *Controller) ListWebhookEndpoints(user *models.User) ([]responses.WebhookEndpointResponse, error) {
	endpoints, err := c.DB.GetWebhookEndpointsByUserID(user.ID.String())
	if err != nil {
		return nil, err
	}

	var result []responses.WebhookEndpointResponse
	for _, ep := range endpoints {
		result = append(result, responses.WebhookEndpointResponse{
			ID:        ep.ID,
			URL:       ep.URL,
			Events:    ep.Events,
			Active:    ep.Active,
			CreatedAt: ep.CreatedAt,
		})
	}
	return result, nil
}

func (c *Controller) UpdateWebhookEndpoint(user *models.User, webhookID string, req requests.UpdateWebhookRequest) (*responses.WebhookEndpointResponse, error) {
	ep, err := c.DB.GetWebhookEndpointByID(webhookID)
	if err != nil {
		return nil, errors.New("webhook endpoint not found")
	}
	if ep.UserID != user.ID {
		return nil, errors.New("webhook endpoint not found")
	}

	if req.URL != "" {
		ep.URL = req.URL
	}
	if req.Events != "" {
		ep.Events = req.Events
	}
	if req.Active != nil {
		ep.Active = *req.Active
	}

	updated, err := c.DB.UpdateWebhookEndpoint(ep)
	if err != nil {
		return nil, err
	}

	return &responses.WebhookEndpointResponse{
		ID:        updated.ID,
		URL:       updated.URL,
		Events:    updated.Events,
		Active:    updated.Active,
		CreatedAt: updated.CreatedAt,
	}, nil
}

func (c *Controller) DeleteWebhookEndpoint(user *models.User, webhookID string) error {
	ep, err := c.DB.GetWebhookEndpointByID(webhookID)
	if err != nil {
		return errors.New("webhook endpoint not found")
	}
	if ep.UserID != user.ID {
		return errors.New("webhook endpoint not found")
	}
	return c.DB.DeleteWebhookEndpoint(ep)
}

// FireWebhookEvent sends a webhook event to all matching endpoints for a user.
// This should be called asynchronously (go c.FireWebhookEvent(...)).
func (c *Controller) FireWebhookEvent(userID string, event string, payload any) {
	endpoints, err := c.DB.GetWebhookEndpointsForEvent(userID, event)
	if err != nil || len(endpoints) == 0 {
		return
	}

	body := map[string]any{
		"event":     event,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"data":      payload,
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return
	}

	for _, ep := range endpoints {
		go deliverWebhook(ep, jsonBody)
	}
}

// isPrivateIP returns true if the IP is in a private/reserved range.
func isPrivateIP(ip net.IP) bool {
	privateRanges := []string{
		"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
		"127.0.0.0/8", "169.254.0.0/16", "::1/128", "fc00::/7",
	}
	for _, cidr := range privateRanges {
		_, network, _ := net.ParseCIDR(cidr)
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func deliverWebhook(ep models.WebhookEndpoint, body []byte) {
	// Validate target URL to prevent SSRF
	parsed, err := url.Parse(ep.URL)
	if err != nil {
		slog.Error("webhook delivery invalid URL", "url", ep.URL, "error", err)
		return
	}
	if parsed.Scheme != "https" {
		slog.Error("webhook delivery rejected non-HTTPS URL", "url", ep.URL)
		return
	}
	host := parsed.Hostname()
	if host == "localhost" || host == "" {
		slog.Error("webhook delivery rejected localhost", "url", ep.URL)
		return
	}
	ips, err := net.LookupIP(host)
	if err == nil {
		for _, ip := range ips {
			if isPrivateIP(ip) {
				slog.Error("webhook delivery rejected private IP", "url", ep.URL, "ip", ip.String())
				return
			}
		}
	}

	// Compute HMAC signature
	mac := hmac.New(sha256.New, []byte(ep.Secret))
	mac.Write(body)
	signature := hex.EncodeToString(mac.Sum(nil))

	req, err := http.NewRequest("POST", ep.URL, bytes.NewReader(body))
	if err != nil {
		slog.Error("webhook delivery failed to create request", "url", ep.URL, "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Wemob-Signature", signature)
	req.Header.Set("X-Wemob-Event", ep.Events)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		slog.Error("webhook delivery failed", "url", ep.URL, "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		slog.Warn("webhook endpoint returned error", "url", ep.URL, "status", resp.StatusCode)
	}
}
