package e2b

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAgentControlSendsTokenAndParsesResponse(t *testing.T) {
	var gotToken string
	var gotReq AgentControlRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("X-Wemob-Agent-Token")
		if r.URL.Path != "/api/e2b/agent/status" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ready", "sandbox_id": "sbx"})
	}))
	defer srv.Close()

	service := NewService(srv.URL, "preview.wemob.app", "agent-token")
	out, err := service.AgentControl(context.Background(), AgentControlRequest{AppID: "app-1", Action: "status"})
	if err != nil {
		t.Fatalf("AgentControl returned error: %v", err)
	}
	if gotToken != "agent-token" {
		t.Fatalf("token = %q", gotToken)
	}
	if gotReq.AppID != "app-1" || gotReq.Action != "status" {
		t.Fatalf("request = %+v", gotReq)
	}
	if out["status"] != "ready" || out["sandbox_id"] != "sbx" {
		t.Fatalf("response = %+v", out)
	}
}

func TestAgentControlNon200ReturnsResponseAndError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]any{"message": "sandbox unavailable"})
	}))
	defer srv.Close()

	service := NewService(srv.URL, "preview.wemob.app", "agent-token")
	out, err := service.AgentControl(context.Background(), AgentControlRequest{AppID: "app-1", Action: "exec"})
	if err == nil {
		t.Fatal("expected error")
	}
	if out["message"] != "sandbox unavailable" {
		t.Fatalf("response = %+v", out)
	}
}
