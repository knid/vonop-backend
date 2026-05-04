package codemagic

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

var httpClient = &http.Client{Timeout: 30 * time.Second}


type Service struct {
	APIToken string
	AppID    string
	Branch   string
	// apiBaseURL and httpClient may be overridden in tests; zero values fall
	// back to the production defaults (https://api.codemagic.io and the
	// package-level httpClient with 30s timeout).
	apiBaseURL string
	client     *http.Client
}

func NewService(apiToken, appID string) *Service {
	return &Service{
		APIToken: apiToken,
		AppID:    appID,
		Branch:   "main",
	}
}

// baseURL returns the Codemagic API base URL, falling back to production.
func (s *Service) baseURL() string {
	if s.apiBaseURL != "" {
		return s.apiBaseURL
	}
	return "https://api.codemagic.io"
}

// SetTestEndpoint overrides the API base URL for testing. Use only from
// integration tests; production callers should construct via NewService.
//
// Exists so integration tests in other packages (e.g. controllers) can
// redirect Codemagic API calls to an httptest.Server without exposing the
// unexported apiBaseURL field directly.
func (s *Service) SetTestEndpoint(baseURL string) {
	s.apiBaseURL = baseURL
}

// httpClientOrDefault returns s.client if set, else the package-level httpClient.
func (s *Service) httpClientOrDefault() *http.Client {
	if s.client != nil {
		return s.client
	}
	return httpClient
}

type triggerRequest struct {
	AppID      string              `json:"appId"`
	WorkflowID string             `json:"workflowId"`
	Branch     string              `json:"branch"`
	Env        *triggerEnvironment `json:"environment,omitempty"`
}

type triggerEnvironment struct {
	Variables map[string]string `json:"variables,omitempty"`
}

type triggerResponse struct {
	BuildID string `json:"buildId"`
}

// TriggerBuild starts a Codemagic build and returns the build ID.
// Retries up to 3 times on 5xx and network errors with ~2s linear backoff.
func (s *Service) TriggerBuild(workflowID string, variables map[string]string) (string, error) {
	body := triggerRequest{
		AppID:      s.AppID,
		WorkflowID: workflowID,
		Branch:     s.Branch,
		Env: &triggerEnvironment{
			Variables: variables,
		},
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("codemagic: marshal: %w", err)
	}

	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		req, err := http.NewRequest("POST", s.baseURL()+"/builds", bytes.NewReader(jsonBody))
		if err != nil {
			return "", fmt.Errorf("codemagic: create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-auth-token", s.APIToken)

		resp, err := s.httpClientOrDefault().Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(2 * time.Second)
			continue
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return "", fmt.Errorf("codemagic: read response: %w", err)
		}

		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("codemagic: API returned %d: %s", resp.StatusCode, string(respBody))
			time.Sleep(2 * time.Second)
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			slog.Error("codemagic: trigger failed",
				"status", resp.StatusCode,
				"body", string(respBody),
				"workflow", workflowID,
			)
			return "", fmt.Errorf("codemagic: API returned %d: %s", resp.StatusCode, string(respBody))
		}

		var result triggerResponse
		if err := json.Unmarshal(respBody, &result); err != nil {
			return "", fmt.Errorf("codemagic: unmarshal response: %w", err)
		}

		slog.Info("codemagic: build triggered",
			"build_id", result.BuildID,
			"workflow", workflowID,
		)

		return result.BuildID, nil
	}
	return "", fmt.Errorf("codemagic: trigger failed after retries: %w", lastErr)
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
