package e2b

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

var httpClient = &http.Client{Timeout: 5 * time.Minute}

// StartResult holds the outcome of a sandbox start request.
type StartResult struct {
	Status     string `json:"status"` // "ready" or "error"
	PreviewURL string `json:"preview_url"`
	SandboxID  string `json:"sandbox_id"`
	Error      string `json:"error,omitempty"`
}

// Service proxies sandbox operations to the dashboard's E2B API routes.
type Service struct {
	DashboardURL  string // e.g. https://app.wemob.io
	PreviewDomain string // e.g. preview.wemob.app
	AgentToken    string // shared backend↔dashboard token for worker-side sandbox control
}

func NewService(dashboardURL, previewDomain string, agentToken ...string) *Service {
	token := ""
	if len(agentToken) > 0 {
		token = strings.TrimSpace(agentToken[0])
	}
	return &Service{
		DashboardURL:  strings.TrimRight(dashboardURL, "/"),
		PreviewDomain: previewDomain,
		AgentToken:    token,
	}
}

// PreviewURL constructs the public preview URL for a sandbox.
// Format: https://3000-{sandboxId}.{previewDomain}
func (s *Service) PreviewURL(sandboxID string) string {
	if sandboxID == "" {
		return ""
	}
	return fmt.Sprintf("https://3000-%s.%s", sandboxID, s.PreviewDomain)
}

// StartSandbox proxies a sandbox start request to the dashboard and blocks
// until the sandbox is ready or an error occurs. It reads the dashboard's
// SSE stream and returns the terminal event.
func (s *Service) StartSandbox(authToken, appID string) (*StartResult, error) {
	body := fmt.Sprintf(`{"appId":"%s"}`, appID)
	req, err := http.NewRequest("POST", s.DashboardURL+"/api/e2b/start", strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("e2b: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+authToken)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("e2b: dashboard request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("e2b: dashboard returned status %d", resp.StatusCode)
	}

	// Parse SSE stream: each event is "data: {json}\n\n"
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")

		var event struct {
			Type       string `json:"type"`
			Status     string `json:"status"`
			PreviewURL string `json:"previewUrl"`
			SandboxID  string `json:"sandboxId"`
			Message    string `json:"message"`
		}
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			slog.Warn("e2b: failed to parse SSE event", "payload", payload, "error", err)
			continue
		}

		switch event.Type {
		case "ready":
			previewURL := event.PreviewURL
			if previewURL == "" {
				previewURL = s.PreviewURL(event.SandboxID)
			}
			return &StartResult{
				Status:     "ready",
				PreviewURL: previewURL,
				SandboxID:  event.SandboxID,
			}, nil

		case "error":
			return &StartResult{
				Status:     "error",
				PreviewURL: event.PreviewURL,
				SandboxID:  event.SandboxID,
				Error:      event.Message,
			}, nil

		case "status":
			slog.Info("e2b: sandbox start progress", "status", event.Status, "app_id", appID)
			continue
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("e2b: stream read error: %w", err)
	}

	return nil, fmt.Errorf("e2b: stream ended without ready or error event")
}

// AgentControlRequest is the backend-to-dashboard control-plane request used by
// agent tools. The dashboard owns the E2B SDK and validates the project sandbox.
type AgentControlRequest struct {
	AppID     string `json:"appId"`
	SandboxID string `json:"sandboxId,omitempty"`
	Action    string `json:"action"`
	Command   string `json:"command,omitempty"`
	Cwd       string `json:"cwd,omitempty"`
	Path      string `json:"path,omitempty"`
	Content   string `json:"content,omitempty"`
	Target    string `json:"target,omitempty"`
	URL       string `json:"url,omitempty"`
	Label     string `json:"label,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Route     string `json:"route,omitempty"`
	TimeoutMs int    `json:"timeoutMs,omitempty"`
}

// AgentControl proxies one sandbox control-plane action to wemob-app. The
// response body is intentionally generic because each action has a slightly
// different shape and agent tools render it back to the model as JSON.
func (s *Service) AgentControl(ctx context.Context, req AgentControlRequest) (map[string]any, error) {
	if s == nil || s.DashboardURL == "" {
		return nil, fmt.Errorf("e2b: dashboard URL not configured")
	}
	if s.AgentToken == "" {
		return nil, fmt.Errorf("e2b: agent control token not configured")
	}
	if req.AppID == "" {
		return nil, fmt.Errorf("e2b: appId is required")
	}
	if req.Action == "" {
		return nil, fmt.Errorf("e2b: action is required")
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("e2b: marshal agent request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", s.DashboardURL+"/api/e2b/agent/"+req.Action, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("e2b: create agent request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Wemob-Agent-Token", s.AgentToken)

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("e2b: dashboard agent request failed: %w", err)
	}
	defer resp.Body.Close()

	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("e2b: decode agent response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if msg, _ := out["message"].(string); msg != "" {
			return out, fmt.Errorf("e2b: dashboard agent status %d: %s", resp.StatusCode, msg)
		}
		return out, fmt.Errorf("e2b: dashboard agent status %d", resp.StatusCode)
	}
	return out, nil
}
