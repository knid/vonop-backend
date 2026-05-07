package meta

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHashEmailNormalization(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		// Lowercase + trim before SHA-256 — Meta's normalization spec.
		{"  Foo@Example.com  ", Hash("foo@example.com")},
		{"FOO@EXAMPLE.COM", Hash("foo@example.com")},
		{"foo@example.com", Hash("foo@example.com")},
		{"", ""},
	}
	for _, tc := range cases {
		got := HashEmail(tc.in)
		if got != tc.want {
			t.Errorf("HashEmail(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestHashLowercaseTrim(t *testing.T) {
	if HashLowercase("  John  ") != Hash("john") {
		t.Error("HashLowercase did not lowercase + trim")
	}
	if HashLowercase("") != "" {
		t.Error("HashLowercase empty should return empty")
	}
}

func TestHashLength(t *testing.T) {
	// SHA-256 hex digest must be 64 hex chars.
	got := Hash("foo@example.com")
	if len(got) != 64 {
		t.Errorf("Hash length = %d, want 64", len(got))
	}
}

func TestNewServiceGracefulDegrade(t *testing.T) {
	if s := NewService("", "tok", ""); s != nil {
		t.Error("NewService should return nil with empty pixel ID")
	}
	if s := NewService("123", "", ""); s != nil {
		t.Error("NewService should return nil with empty access token")
	}
	if s := NewService("", "", ""); s != nil {
		t.Error("NewService should return nil with both empty")
	}

	// Nil-safe: methods are safe to call when service is nil.
	var s *Service
	if err := s.Send(context.Background(), Event{}); err != nil {
		t.Errorf("Send on nil should be no-op, got %v", err)
	}
	s.SetTestEndpoint("anything") // must not panic
}

func TestSendPayloadShape(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		// The pixel ID must be in the URL path.
		if !strings.Contains(r.URL.Path, "/PIXEL/events") {
			t.Errorf("URL path = %q, want pixel ID in path", r.URL.Path)
		}
		// Token goes in the Authorization header, never as a query param.
		if got := r.Header.Get("Authorization"); got != "Bearer TOK" {
			t.Errorf("Authorization = %q, want %q", got, "Bearer TOK")
		}
		if r.URL.Query().Get("access_token") != "" {
			t.Errorf("access_token must not appear in URL query")
		}
		_, _ = w.Write([]byte(`{"events_received":1}`))
	}))
	defer srv.Close()

	s := NewService("PIXEL", "TOK", "TEST123")
	s.SetTestEndpoint(srv.URL)

	ev := Event{
		EventName:    "Subscribe",
		EventTime:    1700000000,
		EventID:      "subscribe-sub_abc",
		ActionSource: "website",
		UserData: UserData{
			Em:         []string{HashEmail("user@example.com")},
			ExternalID: []string{Hash("user-123")},
			ClientIP:   "1.2.3.4",
			ClientUA:   "Mozilla/5.0",
			Fbp:        "fb.1.x.y",
		},
		CustomData: CustomData{
			Value:          12.34,
			Currency:       "USD",
			SubscriptionID: "sub_abc",
		},
	}
	if err := s.Send(context.Background(), ev); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if captured["test_event_code"] != "TEST123" {
		t.Errorf("test_event_code missing/incorrect: %v", captured["test_event_code"])
	}
	data, ok := captured["data"].([]any)
	if !ok || len(data) != 1 {
		t.Fatalf("data missing or wrong length: %v", captured["data"])
	}
	first := data[0].(map[string]any)
	if first["event_name"] != "Subscribe" {
		t.Errorf("event_name = %v, want Subscribe", first["event_name"])
	}
	if first["event_id"] != "subscribe-sub_abc" {
		t.Errorf("event_id = %v, want subscribe-sub_abc", first["event_id"])
	}
	if first["action_source"] != "website" {
		t.Errorf("action_source = %v, want website", first["action_source"])
	}
	custom, ok := first["custom_data"].(map[string]any)
	if !ok {
		t.Fatalf("custom_data missing or wrong type: %v", first["custom_data"])
	}
	if custom["value"].(float64) != 12.34 {
		t.Errorf("custom_data.value = %v, want 12.34", custom["value"])
	}
	if custom["currency"] != "USD" {
		t.Errorf("custom_data.currency = %v, want USD", custom["currency"])
	}
	if custom["subscription_id"] != "sub_abc" {
		t.Errorf("custom_data.subscription_id = %v, want sub_abc", custom["subscription_id"])
	}
	user, ok := first["user_data"].(map[string]any)
	if !ok {
		t.Fatalf("user_data missing")
	}
	em := user["em"].([]any)
	if len(em) != 1 || len(em[0].(string)) != 64 {
		t.Errorf("em should be one 64-char hash, got %v", em)
	}
	if user["fbp"] != "fb.1.x.y" {
		t.Errorf("fbp = %v, want fb.1.x.y", user["fbp"])
	}
}

func TestSendNoOpWhenEmpty(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
	}))
	defer srv.Close()
	s := NewService("PIXEL", "TOK", "")
	s.SetTestEndpoint(srv.URL)
	if err := s.Send(context.Background()); err != nil {
		t.Fatalf("Send with no events: %v", err)
	}
	if hit {
		t.Error("HTTP request should not fire when no events given")
	}
}
