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
	"time"

	"github.com/knid/wemob/internal/models"
)

// ResendService POSTs events to Resend's events API so Resend automations
// can match and trigger email sends. The same contact-attribute set we
// store on Twenty's Person rides in every event payload, so automations
// can branch on plan / subscription_status / etc.
type ResendService struct {
	apiKey  string
	client  *http.Client
	baseURL string // overridable for tests/staging; defaults to https://api.resend.com
}

func NewResendService(apiKey string) *ResendService {
	return &ResendService{
		apiKey:  apiKey,
		client:  &http.Client{Timeout: 8 * time.Second},
		baseURL: "https://api.resend.com",
	}
}

type resendEventPayload struct {
	Event   string         `json:"event"`
	Email   string         `json:"email"`
	Payload map[string]any `json:"payload,omitempty"`
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

	// Build payload: contact attrs first, caller-supplied attrs override.
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

	url := s.baseURL + "/events"
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
		defer resp.Body.Close()

		if resp.StatusCode < 300 {
			return nil
		}
		respBody, _ := io.ReadAll(resp.Body)
		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("resend events: %s %s", resp.Status, string(respBody))
			continue
		}
		// 4xx — no retry; log once.
		slog.Warn("resend events: 4xx response",
			"event", event, "user_id", user.ID.String(),
			"status", resp.Status, "body", string(respBody))
		return nil
	}
	if lastErr != nil {
		slog.Warn("resend events: send failed after retries",
			"event", event, "user_id", user.ID.String(), "err", lastErr)
	}
	return nil
}
