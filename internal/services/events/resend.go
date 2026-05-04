package events

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/knid/wemob/internal/models"
)

// ResendService keeps Resend's view of each user in sync via two calls per event:
//
//  1. POST /contacts — upserts the contact with first/last name, segment
//     membership, and custom properties (wemob_user_id, verified, provider,
//     subscription_status, trial_used). Resend's POST /contacts is upsert by
//     email — duplicates update in place.
//  2. POST /events — fires the event so Resend automations can match it and
//     trigger emails. Event payload also carries the same attributes inline so
//     automation conditions can read them without a contact join.
//
// Both calls are made on every SendEvent so contact state stays continuously
// fresh. Errors are logged and swallowed — never propagated.
type ResendService struct {
	apiKey     string
	client     *http.Client
	baseURL    string   // overridable for tests/staging; defaults to https://api.resend.com
	segmentIDs []string // segments every upserted contact joins; empty = no segment assignment
}

// NewResendService constructs the service. segmentIDs is the list of Resend
// segment UUIDs every contact should be added to (e.g. the "General" segment).
// Empty list = contacts created without segment membership.
func NewResendService(apiKey string, segmentIDs ...string) *ResendService {
	return &ResendService{
		apiKey:     apiKey,
		client:     &http.Client{Timeout: 8 * time.Second},
		baseURL:    "https://api.resend.com",
		segmentIDs: segmentIDs,
	}
}

type resendEventPayload struct {
	Event   string         `json:"event"`
	Email   string         `json:"email"`
	Payload map[string]any `json:"payload,omitempty"`
}

type resendContactPayload struct {
	Email      string            `json:"email"`
	FirstName  string            `json:"first_name,omitempty"`
	LastName   string            `json:"last_name,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
	SegmentIDs []string          `json:"segment_ids,omitempty"`
}

func (s *ResendService) SendEvent(user *models.User, event string, attributes map[string]any) error {
	if s.apiKey == "" {
		return errors.New("resend events: not initialized")
	}
	if user == nil || user.Email == "" {
		slog.Warn("resend events: missing user or email", "event", event)
		return nil
	}
	if event == "" {
		return errors.New("resend events: empty event name")
	}

	// Sync contact state (first/last name, segment, custom properties). Done
	// before the event POST so any automation that reads contact properties
	// sees the freshest values. Failures are logged and swallowed — the event
	// itself still goes through.
	s.upsertContact(user)

	// Build event payload: contact attrs first, caller-supplied attrs override.
	payload := contactPayload(user)
	for k, v := range attributes {
		payload[k] = v
	}

	body, err := json.Marshal(resendEventPayload{
		Event:   event,
		Email:   user.Email,
		Payload: payload,
	})
	if err != nil {
		slog.Error("resend events: marshal failed", "event", event, "err", err)
		return nil
	}

	s.postWithRetry(s.baseURL+"/events", body, "resend events", event, user.ID.String())
	return nil
}

func (s *ResendService) upsertContact(user *models.User) {
	firstName, lastName := splitName(user.Name)
	body, err := json.Marshal(resendContactPayload{
		Email:      strings.ToLower(strings.TrimSpace(user.Email)),
		FirstName:  firstName,
		LastName:   lastName,
		Properties: contactProperties(user),
		SegmentIDs: s.segmentIDs,
	})
	if err != nil {
		slog.Error("resend contact: marshal failed", "user_id", user.ID.String(), "err", err)
		return
	}
	s.postWithRetry(s.baseURL+"/contacts", body, "resend contact", "", user.ID.String())
}

// postWithRetry POSTs body to url with bearer auth and 2x retry on 5xx/timeout.
// All failures log and return — never propagate. context is a short label used
// in log messages ("resend events" / "resend contact"); event is optional.
func (s *ResendService) postWithRetry(url string, body []byte, ctx, event, userID string) {
	maxRetries := 2
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * time.Second)
		}
		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("Authorization", "Bearer "+s.apiKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := s.client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode < 300 {
			resp.Body.Close()
			return
		}
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("%s: %s %s", ctx, resp.Status, string(respBody))
			continue
		}
		// 4xx — no retry; log once.
		slog.Warn(ctx+": 4xx response",
			"event", event, "user_id", userID,
			"status", resp.Status, "body", string(respBody))
		return
	}
	if lastErr != nil {
		slog.Warn(ctx+": send failed after retries",
			"event", event, "user_id", userID, "err", lastErr)
	}
}

// contactProperties returns the user attribute set as Resend property values.
// Resend properties accept string or number only — booleans are stringified
// to "true"/"false" to match the values the backfill seeded.
func contactProperties(user *models.User) map[string]string {
	if user == nil {
		return nil
	}
	language := user.Language
	if language == "" {
		language = "en"
	}
	return map[string]string{
		"wemob_user_id":       user.ID.String(),
		"verified":            strconv.FormatBool(user.Verified),
		"provider":            user.Provider,
		"subscription_status": user.SubscriptionStatus,
		"trial_used":          strconv.FormatBool(user.TrialUsed),
		"language":            language,
	}
}

// splitName splits "First Middle Last" into ("First", "Middle Last"). Empty
// input returns two empty strings. Single-token input returns it as firstName
// with empty lastName.
func splitName(full string) (string, string) {
	full = strings.TrimSpace(full)
	if full == "" {
		return "", ""
	}
	if i := strings.IndexAny(full, " \t"); i >= 0 {
		return full[:i], strings.TrimSpace(full[i+1:])
	}
	return full, ""
}
