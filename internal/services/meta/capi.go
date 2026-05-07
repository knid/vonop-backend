// Package meta is a thin client for Meta's Conversions API (CAPI). Callers
// fire-and-forget server-side conversion events (CompleteRegistration,
// InitiateCheckout, Subscribe, Purchase) so attribution survives ad blockers,
// iOS ATT, and ITP — losses that pure client-side Pixel can't recover from.
//
// Browser-side fbp/fbc cookies and the click ID (fbclid) are still captured
// by the front-end Pixel and round-tripped to the backend on the API calls
// that trigger conversions. The backend persists them on the user so the
// async Stripe webhook (which has no browser context) can include them in
// CAPI events for high match quality.
//
// Pixel ID + access token live in Infisical as META_PIXEL_ID and
// META_CAPI_ACCESS_TOKEN. Set META_CAPI_TEST_EVENT_CODE during integration
// testing to route events to Meta Events Manager's Test Events tab without
// affecting production attribution.
package meta

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Service is the Meta CAPI client. Construct with NewService; nil-safe — every
// public method is a no-op when s == nil so callers don't need to guard.
type Service struct {
	pixelID       string
	accessToken   string
	testEventCode string
	apiBaseURL    string
	client        *http.Client
}

// NewService constructs a Service. Returns nil when pixelID or accessToken is
// empty so callers can graceful-degrade (matches the RevenueCat service
// pattern). testEventCode is optional; when set, events are tagged for Meta's
// Test Events tab.
func NewService(pixelID, accessToken, testEventCode string) *Service {
	if pixelID == "" || accessToken == "" {
		return nil
	}
	return &Service{
		pixelID:       pixelID,
		accessToken:   accessToken,
		testEventCode: testEventCode,
		client:        &http.Client{Timeout: 10 * time.Second},
	}
}

// SetTestEndpoint overrides the API base URL. Use only from tests.
func (s *Service) SetTestEndpoint(u string) {
	if s == nil {
		return
	}
	s.apiBaseURL = u
}

func (s *Service) baseURL() string {
	if s.apiBaseURL != "" {
		return s.apiBaseURL
	}
	return "https://graph.facebook.com/v21.0"
}

// UserData is the matching payload for a CAPI event. Hashed PII fields
// (Em, Ph, Fn, Ln, ExternalID) MUST be SHA-256 hashed before assignment —
// use HashEmail / HashLowercase / Hash. Cookies (Fbp, Fbc), IP, and UA are
// passed in raw.
type UserData struct {
	Em         []string `json:"em,omitempty"`          // SHA-256 of normalized email
	Ph         []string `json:"ph,omitempty"`          // SHA-256 of E.164 phone
	Fn         []string `json:"fn,omitempty"`          // SHA-256 of first name lowercased
	Ln         []string `json:"ln,omitempty"`          // SHA-256 of last name lowercased
	ExternalID []string `json:"external_id,omitempty"` // SHA-256 of stable user ID
	ClientIP   string   `json:"client_ip_address,omitempty"`
	ClientUA   string   `json:"client_user_agent,omitempty"`
	Fbp        string   `json:"fbp,omitempty"`
	Fbc        string   `json:"fbc,omitempty"`
}

// CustomData carries event-specific payload (value, currency, IDs).
type CustomData struct {
	Value          float64        `json:"value,omitempty"`
	Currency       string         `json:"currency,omitempty"`
	ContentName    string         `json:"content_name,omitempty"`
	ContentIDs     []string       `json:"content_ids,omitempty"`
	ContentType    string         `json:"content_type,omitempty"`
	OrderID        string         `json:"order_id,omitempty"`
	SubscriptionID string         `json:"subscription_id,omitempty"`
	Extra          map[string]any `json:"-"`
}

// Event is one CAPI event. EventID is required for dedup with browser-side
// Pixel events; even when no Pixel mirror exists, a stable EventID makes
// Meta's webhook-retry dedup correct.
type Event struct {
	EventName       string     `json:"event_name"`
	EventTime       int64      `json:"event_time"` // Unix seconds
	EventID         string     `json:"event_id,omitempty"`
	EventSourceURL  string     `json:"event_source_url,omitempty"`
	ActionSource    string     `json:"action_source"` // "website" | "app" | "system_generated"
	UserData        UserData   `json:"user_data"`
	CustomData      CustomData `json:"custom_data,omitempty"`
}

type sendRequest struct {
	Data          []Event `json:"data"`
	TestEventCode string  `json:"test_event_code,omitempty"`
}

type sendResponse struct {
	EventsReceived int    `json:"events_received,omitempty"`
	FbtraceID      string `json:"fbtrace_id,omitempty"`
	Error          *struct {
		Message      string `json:"message"`
		Type         string `json:"type"`
		Code         int    `json:"code"`
		ErrorSubcode int    `json:"error_subcode"`
		FbtraceID    string `json:"fbtrace_id"`
	} `json:"error,omitempty"`
}

// Send POSTs events to the Conversions API. Errors are logged and returned
// (callers typically fire-and-forget in a goroutine).
func (s *Service) Send(ctx context.Context, events ...Event) error {
	if s == nil || len(events) == 0 {
		return nil
	}
	body := sendRequest{Data: events, TestEventCode: s.testEventCode}
	buf, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("meta: marshal events: %w", err)
	}

	url := fmt.Sprintf("%s/%s/events", s.baseURL(), s.pixelID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return fmt.Errorf("meta: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// Bearer header keeps the token out of access logs / proxy traces. Meta's
	// Graph API accepts both query-param and Authorization header forms.
	req.Header.Set("Authorization", "Bearer "+s.accessToken)

	resp, err := s.client.Do(req)
	if err != nil {
		slog.Warn("meta CAPI send failed", "error", err, "events", eventNames(events))
		return fmt.Errorf("meta: do: %w", err)
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		slog.Warn("meta CAPI non-2xx",
			"status", resp.StatusCode,
			"body", string(respBytes),
			"events", eventNames(events))
		return fmt.Errorf("meta: status %d: %s", resp.StatusCode, string(respBytes))
	}

	var parsed sendResponse
	if err := json.Unmarshal(respBytes, &parsed); err == nil && parsed.Error != nil {
		slog.Warn("meta CAPI returned error",
			"code", parsed.Error.Code,
			"subcode", parsed.Error.ErrorSubcode,
			"message", parsed.Error.Message,
			"fbtrace_id", parsed.Error.FbtraceID)
		return fmt.Errorf("meta: api error: %s", parsed.Error.Message)
	}

	slog.Info("meta CAPI sent",
		"events_received", parsed.EventsReceived,
		"fbtrace_id", parsed.FbtraceID,
		"events", eventNames(events))
	return nil
}

func eventNames(events []Event) []string {
	out := make([]string, 0, len(events))
	for _, e := range events {
		out = append(out, e.EventName)
	}
	return out
}

// HashEmail normalizes an email (trim + lowercase) and returns its lowercase
// SHA-256 hex digest. Returns "" for an empty input.
func HashEmail(email string) string {
	e := strings.TrimSpace(strings.ToLower(email))
	if e == "" {
		return ""
	}
	return Hash(e)
}

// HashLowercase trims and lowercases the input before SHA-256 hashing.
// Returns "" for an empty input. Use for first/last name and other free-text
// PII per Meta's normalization spec.
func HashLowercase(s string) string {
	v := strings.TrimSpace(strings.ToLower(s))
	if v == "" {
		return ""
	}
	return Hash(v)
}

// Hash returns the lowercase SHA-256 hex digest. Use for already-normalized
// values (E.164 phone numbers, stable user IDs).
func Hash(s string) string {
	if s == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// MarshalJSON merges CustomData.Extra into the top level so callers can pass
// arbitrary fields (e.g. num_items) without extending the struct.
func (c CustomData) MarshalJSON() ([]byte, error) {
	out := map[string]any{}
	if c.Value != 0 {
		out["value"] = c.Value
	}
	if c.Currency != "" {
		out["currency"] = c.Currency
	}
	if c.ContentName != "" {
		out["content_name"] = c.ContentName
	}
	if len(c.ContentIDs) > 0 {
		out["content_ids"] = c.ContentIDs
	}
	if c.ContentType != "" {
		out["content_type"] = c.ContentType
	}
	if c.OrderID != "" {
		out["order_id"] = c.OrderID
	}
	if c.SubscriptionID != "" {
		out["subscription_id"] = c.SubscriptionID
	}
	for k, v := range c.Extra {
		out[k] = v
	}
	return json.Marshal(out)
}
