package codemagic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// TriggerExpoAndroidBuild triggers the Codemagic expo-android-build workflow.
// Retries up to 3 times on 5xx and network errors with linear backoff.
func (s *Service) TriggerExpoAndroidBuild(ctx context.Context, p ExpoAndroidBuildParams) (string, error) {
	return s.triggerExpo(ctx, WorkflowExpoAndroidBuild, p.toEnvVars())
}

// TriggerExpoIOSBuild triggers the Codemagic expo-ios-build workflow.
// Retries up to 3 times on 5xx and network errors with linear backoff.
func (s *Service) TriggerExpoIOSBuild(ctx context.Context, p ExpoIOSBuildParams) (string, error) {
	return s.triggerExpo(ctx, WorkflowExpoIOSBuild, p.toEnvVars())
}

type expoTriggerEnvironment struct {
	Variables map[string]string `json:"variables,omitempty"`
}

type expoTriggerBody struct {
	AppID       string                 `json:"appId"`
	WorkflowID  string                 `json:"workflowId"`
	Branch      string                 `json:"branch"`
	Environment expoTriggerEnvironment `json:"environment"`
}

type expoTriggerResponse struct {
	BuildID string `json:"build_id"`
}

func (s *Service) triggerExpo(ctx context.Context, workflowID string, vars map[string]string) (string, error) {
	body := expoTriggerBody{
		AppID:       s.AppID,
		WorkflowID:  workflowID,
		Branch:      s.Branch,
		Environment: expoTriggerEnvironment{Variables: vars},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("expo trigger: marshal body: %w", err)
	}

	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL()+"/builds", bytes.NewReader(payload))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-auth-token", s.APIToken)

		resp, err := s.httpClientOrDefault().Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(time.Duration(attempt) * time.Second)
			continue
		}
		if resp.StatusCode >= 500 {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			lastErr = fmt.Errorf("codemagic returned %d", resp.StatusCode)
			time.Sleep(time.Duration(attempt) * time.Second)
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			b, _ := io.ReadAll(resp.Body)
			return "", fmt.Errorf("codemagic %d: %s", resp.StatusCode, string(b))
		}
		var tr expoTriggerResponse
		if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
			return "", fmt.Errorf("expo trigger: decode response: %w", err)
		}
		return tr.BuildID, nil
	}
	return "", fmt.Errorf("expo trigger failed after retries: %w", lastErr)
}
