package external

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestSendNotificationViaSDK_HappyPath(t *testing.T) {
	var gotAuth, gotMethod, gotPath, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotMethod = r.Method
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "notif-abc", "recipients": 42})
	}))
	defer server.Close()
	restore := SetOneSignalTestEndpoint(server.URL)
	defer restore()

	out, err := SendNotificationViaSDK(context.Background(), "app-xyz", "rest-key-1", SendNotificationParams{
		Headings:         map[string]string{"en": "Hello"},
		Contents:         map[string]string{"en": "Hi there"},
		IncludedSegments: []string{"Subscribed Users"},
	})
	if err != nil {
		t.Fatalf("SendNotificationViaSDK: %v", err)
	}
	if out.NotificationID != "notif-abc" {
		t.Errorf("notif id: got %q", out.NotificationID)
	}
	if out.RecipientsCount != 42 {
		t.Errorf("recipients: got %d", out.RecipientsCount)
	}
	if gotAuth != "Basic rest-key-1" {
		t.Errorf("auth: got %q", gotAuth)
	}
	if gotMethod != "POST" || gotPath != "/notifications" {
		t.Errorf("path: got %s %s", gotMethod, gotPath)
	}
	if !strings.Contains(gotBody, `"app_id":"app-xyz"`) {
		t.Errorf("body missing app_id: %q", gotBody)
	}
	if !strings.Contains(gotBody, `"included_segments":["Subscribed Users"]`) {
		t.Errorf("body missing segments: %q", gotBody)
	}
}

func TestSendNotificationViaSDK_AliasesPayload(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(200)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "n", "recipients": 2})
	}))
	defer server.Close()
	restore := SetOneSignalTestEndpoint(server.URL)
	defer restore()

	_, err := SendNotificationViaSDK(context.Background(), "app", "k", SendNotificationParams{
		Headings:       map[string]string{"en": "Hi"},
		Contents:       map[string]string{"en": "Hi body"},
		IncludeAliases: map[string][]string{"external_id": {"u1", "u2"}},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(gotBody, `"include_aliases":{"external_id":["u1","u2"]}`) {
		t.Errorf("body missing aliases: %q", gotBody)
	}
	if !strings.Contains(gotBody, `"target_channel":"push"`) {
		t.Errorf("body missing target_channel: %q", gotBody)
	}
}

func TestSendNotificationViaSDK_Auth401_NoRetry(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(401)
	}))
	defer server.Close()
	restore := SetOneSignalTestEndpoint(server.URL)
	defer restore()

	_, err := SendNotificationViaSDK(context.Background(), "app", "bad", SendNotificationParams{
		Headings:         map[string]string{"en": "x"},
		Contents:         map[string]string{"en": "y"},
		IncludedSegments: []string{"Subscribed Users"},
	})
	if !errors.Is(err, ErrOneSignalAuth) {
		t.Fatalf("expected ErrOneSignalAuth, got %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("expected 1 attempt on 401, got %d", calls.Load())
	}
}

func TestSendNotificationViaSDK_429RetriesThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n < 3 {
			w.WriteHeader(429)
			return
		}
		w.WriteHeader(200)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "n", "recipients": 1})
	}))
	defer server.Close()
	restore := SetOneSignalTestEndpoint(server.URL)
	defer restore()

	out, err := SendNotificationViaSDK(context.Background(), "app", "k", SendNotificationParams{
		Headings:         map[string]string{"en": "x"},
		Contents:         map[string]string{"en": "y"},
		IncludedSegments: []string{"Subscribed Users"},
	})
	if err != nil {
		t.Fatalf("expected success after retries: %v", err)
	}
	if out.NotificationID != "n" {
		t.Errorf("id: %q", out.NotificationID)
	}
	if calls.Load() != 3 {
		t.Errorf("expected 3 attempts, got %d", calls.Load())
	}
}

func TestSendNotificationViaSDK_5xxRetriesOnce(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(500)
	}))
	defer server.Close()
	restore := SetOneSignalTestEndpoint(server.URL)
	defer restore()

	_, err := SendNotificationViaSDK(context.Background(), "app", "k", SendNotificationParams{
		Headings:         map[string]string{"en": "x"},
		Contents:         map[string]string{"en": "y"},
		IncludedSegments: []string{"Subscribed Users"},
	})
	if err == nil {
		t.Fatalf("expected error on persistent 5xx")
	}
	if calls.Load() != 2 {
		t.Errorf("expected 2 attempts (initial + 1 retry), got %d", calls.Load())
	}
}

func TestSendNotificationViaSDK_RequiredFields(t *testing.T) {
	cases := []struct {
		name   string
		params SendNotificationParams
		appID  string
		key    string
	}{
		{name: "missing app_id", params: SendNotificationParams{Headings: map[string]string{"en": "h"}, Contents: map[string]string{"en": "b"}, IncludedSegments: []string{"x"}}},
		{name: "missing key", appID: "a", params: SendNotificationParams{Headings: map[string]string{"en": "h"}, Contents: map[string]string{"en": "b"}, IncludedSegments: []string{"x"}}},
		{name: "missing headings", appID: "a", key: "k", params: SendNotificationParams{Contents: map[string]string{"en": "b"}, IncludedSegments: []string{"x"}}},
		{name: "missing audience", appID: "a", key: "k", params: SendNotificationParams{Headings: map[string]string{"en": "h"}, Contents: map[string]string{"en": "b"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := SendNotificationViaSDK(context.Background(), tc.appID, tc.key, tc.params)
			if err == nil {
				t.Errorf("expected validation error for %s", tc.name)
			}
		})
	}
}
