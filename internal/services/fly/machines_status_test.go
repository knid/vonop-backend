package fly

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetMachineStatus_SingleMachineHappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/apps/wemob-acme/machines" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("missing or wrong auth header")
		}
		machines := []map[string]any{
			{
				"id":        "m1",
				"name":      "wemob-acme-app",
				"state":     "started",
				"region":    "ams",
				"image_ref": map[string]any{"registry": "registry.fly.io", "repository": "wemob-acme", "tag": "v1"},
				"updated_at": "2026-05-04T12:00:00Z",
			},
		}
		_ = json.NewEncoder(w).Encode(machines)
	}))
	defer server.Close()

	s := &Service{APIToken: "test-token", APIBase: server.URL, httpClient: server.Client()}

	got, err := s.GetMachineStatus("wemob-acme")
	if err != nil {
		t.Fatalf("GetMachineStatus: %v", err)
	}
	if got.State != "started" {
		t.Errorf("state = %q, want started", got.State)
	}
	if got.Region != "ams" {
		t.Errorf("region = %q, want ams", got.Region)
	}
	if got.Image != "registry.fly.io/wemob-acme:v1" {
		t.Errorf("image = %q, want %q", got.Image, "registry.fly.io/wemob-acme:v1")
	}
}

func TestGetMachineStatus_NoMachines(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]any{})
	}))
	defer server.Close()

	s := &Service{APIToken: "test-token", APIBase: server.URL, httpClient: server.Client()}

	_, err := s.GetMachineStatus("wemob-acme")
	if err == nil || !strings.Contains(err.Error(), "no machines") {
		t.Errorf("expected 'no machines' error, got %v", err)
	}
}

func TestGetMachineStatus_EmptyImageRefProducesEmptyImage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		machines := []map[string]any{
			{
				"id":         "m1",
				"name":       "wemob-acme-app",
				"state":      "created",
				"region":     "ams",
				// image_ref intentionally omitted
				"updated_at": "2026-05-04T12:00:00Z",
			},
		}
		_ = json.NewEncoder(w).Encode(machines)
	}))
	defer server.Close()

	s := &Service{APIToken: "test-token", APIBase: server.URL, httpClient: server.Client()}
	got, err := s.GetMachineStatus("wemob-acme")
	if err != nil {
		t.Fatalf("GetMachineStatus: %v", err)
	}
	if got.Image != "" {
		t.Errorf("image = %q, want empty when image_ref is absent", got.Image)
	}
}
