package fly

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGetLogs_ReturnsParsedEntries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		// Fly returns a GraphQL response shape; we mock it.
		resp := map[string]any{
			"data": map[string]any{
				"app": map[string]any{
					"recentLogs": []map[string]any{
						{"timestamp": "2026-05-04T12:00:00Z", "level": "info", "message": "GET /api/todos 200 12ms"},
						{"timestamp": "2026-05-04T12:00:01Z", "level": "error", "message": "TypeError: Cannot read property 'foo' of undefined"},
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	s := &Service{
		APIToken:    "test-token",
		APIBase:     server.URL,
		GraphQLBase: server.URL, // see Step 3 — new field for GraphQL endpoint override
		httpClient:  server.Client(),
	}

	got, err := s.GetLogs("wemob-acme", time.Time{}, "")
	if err != nil {
		t.Fatalf("GetLogs: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[1].Level != "error" {
		t.Errorf("got[1].Level = %q, want error", got[1].Level)
	}
}

func TestGetLogs_FiltersByLevel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"data": map[string]any{
				"app": map[string]any{
					"recentLogs": []map[string]any{
						{"timestamp": "2026-05-04T12:00:00Z", "level": "info", "message": "ok"},
						{"timestamp": "2026-05-04T12:00:01Z", "level": "error", "message": "boom"},
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	s := &Service{APIToken: "t", APIBase: server.URL, GraphQLBase: server.URL, httpClient: server.Client()}

	got, err := s.GetLogs("wemob-acme", time.Time{}, "error")
	if err != nil {
		t.Fatalf("GetLogs: %v", err)
	}
	if len(got) != 1 || got[0].Level != "error" {
		t.Errorf("unexpected filter result: %+v", got)
	}
}

func TestGetLogs_FiltersBySince(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"data": map[string]any{
				"app": map[string]any{
					"recentLogs": []map[string]any{
						{"timestamp": "2026-05-04T11:00:00Z", "level": "info", "message": "old"},
						{"timestamp": "2026-05-04T12:00:01Z", "level": "info", "message": "new"},
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	s := &Service{APIToken: "t", APIBase: server.URL, GraphQLBase: server.URL, httpClient: server.Client()}

	since, _ := time.Parse(time.RFC3339, "2026-05-04T12:00:00Z")
	got, err := s.GetLogs("wemob-acme", since, "")
	if err != nil {
		t.Fatalf("GetLogs: %v", err)
	}
	if len(got) != 1 || got[0].Message != "new" {
		t.Errorf("unexpected since filter: %+v", got)
	}
}
