package fly

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestStartStoppedMachines_StartsOnlyStopped(t *testing.T) {
	var mu sync.Mutex
	started := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("missing or wrong auth header")
		}
		switch {
		case r.Method == "GET" && r.URL.Path == "/v1/apps/wemob-acme/machines":
			machines := []map[string]any{
				{"id": "m1", "state": "stopped", "name": "x-1"},
				{"id": "m2", "state": "started", "name": "x-2"},
				{"id": "m3", "state": "stopped", "name": "x-3"},
			}
			_ = json.NewEncoder(w).Encode(machines)
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/start"):
			parts := strings.Split(r.URL.Path, "/")
			machineID := parts[len(parts)-2]
			mu.Lock()
			started = append(started, machineID)
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", http.StatusNotFound)
		}
	}))
	defer server.Close()

	s := &Service{APIToken: "test-token", APIBase: server.URL, httpClient: server.Client()}
	if err := s.StartStoppedMachines("wemob-acme"); err != nil {
		t.Fatalf("StartStoppedMachines: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(started) != 2 {
		t.Fatalf("expected 2 start calls, got %d: %v", len(started), started)
	}
	found := map[string]bool{}
	for _, id := range started {
		found[id] = true
	}
	if !found["m1"] || !found["m3"] {
		t.Errorf("expected m1 and m3 to be started, got %v", started)
	}
	if found["m2"] {
		t.Errorf("m2 was already started, should not have been started again")
	}
}

func TestStartStoppedMachines_AllStartedNoop(t *testing.T) {
	postCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			machines := []map[string]any{
				{"id": "m1", "state": "started"},
				{"id": "m2", "state": "started"},
			}
			_ = json.NewEncoder(w).Encode(machines)
			return
		}
		postCount++
		t.Errorf("unexpected POST: %s", r.URL.Path)
	}))
	defer server.Close()

	s := &Service{APIToken: "test-token", APIBase: server.URL, httpClient: server.Client()}
	if err := s.StartStoppedMachines("wemob-acme"); err != nil {
		t.Fatalf("StartStoppedMachines: %v", err)
	}
	if postCount > 0 {
		t.Errorf("expected no POST calls, got %d", postCount)
	}
}

func TestStartStoppedMachines_PropagatesStartError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			machines := []map[string]any{{"id": "m1", "state": "stopped"}}
			_ = json.NewEncoder(w).Encode(machines)
			return
		}
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	s := &Service{APIToken: "test-token", APIBase: server.URL, httpClient: server.Client()}
	err := s.StartStoppedMachines("wemob-acme")
	if err == nil || !strings.Contains(err.Error(), "m1") {
		t.Errorf("expected error mentioning m1, got %v", err)
	}
}

func TestStartStoppedMachines_PropagatesListError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	s := &Service{APIToken: "test-token", APIBase: server.URL, httpClient: server.Client()}
	err := s.StartStoppedMachines("wemob-acme")
	if err == nil || !strings.Contains(err.Error(), "list machines") {
		t.Errorf("expected list-machines error, got %v", err)
	}
}
