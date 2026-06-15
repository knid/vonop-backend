package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	ScoreAgentRunSuccess = "agent_run_success"
	ScoreAgentRunTurns   = "agent_run_turns"
	ScoreAgentToolErrors = "agent_tool_errors"
)

type ScoreClient struct {
	BaseURL    string
	PublicKey  string
	SecretKey  string
	HTTPClient *http.Client
}

type ScoreRequest struct {
	TraceID string `json:"traceId"`
	Name    string `json:"name"`
	Value   any    `json:"value"`
	Comment string `json:"comment,omitempty"`
}

func ScoreClientFromEnvAndSecrets(secrets SecretProvider) ScoreClient {
	cfg := LangfuseConfigFromEnvAndSecrets(secrets)
	publicKey := envOrSecret("LANGFUSE_PUBLIC_KEY", secrets)
	secretKey := envOrSecret("LANGFUSE_SECRET_KEY", secrets)
	baseURL := strings.TrimRight(envOrSecret("LANGFUSE_BASE_URL", secrets), "/")
	if baseURL == "" {
		baseURL = baseURLFromOTELTracesEndpoint(envOrSecret("LANGFUSE_OTEL_TRACES_ENDPOINT", secrets))
	}
	if baseURL == "" {
		baseURL = defaultLangfuseBaseURL
	}
	if cfg.Enabled {
		return ScoreClient{BaseURL: baseURL, PublicKey: publicKey, SecretKey: secretKey}
	}
	return ScoreClient{}
}

func (c ScoreClient) CreateScore(ctx context.Context, req ScoreRequest) error {
	if c.BaseURL == "" || c.PublicKey == "" || c.SecretKey == "" || req.TraceID == "" || req.Name == "" {
		return nil
	}
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/api/public/scores", bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.SetBasicAuth(c.PublicKey, c.SecretKey)
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("langfuse score status %d", resp.StatusCode)
	}
	return nil
}

func baseURLFromOTELTracesEndpoint(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return ""
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	path := strings.TrimRight(parsed.Path, "/")
	for _, suffix := range []string{"/api/public/otel/v1/traces", "/api/public/otel"} {
		if strings.HasSuffix(path, suffix) {
			path = strings.TrimSuffix(path, suffix)
			break
		}
	}
	parsed.Path = path
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/")
}
