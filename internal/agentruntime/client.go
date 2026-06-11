package agentruntime

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	BaseURL           string
	RuntimeAPIURL     string
	AssistantID       string
	InternalToken     string
	HMACSecret        string
	AccessTokenSecret string
	HTTPClient        *http.Client
	Now               func() time.Time
}

func NewClient(baseURL, internalToken, accessTokenSecret string) *Client {
	baseURL = strings.TrimRight(baseURL, "/")
	return &Client{
		BaseURL:           baseURL,
		RuntimeAPIURL:     baseURL,
		AssistantID:       DefaultAssistantID,
		InternalToken:     internalToken,
		HMACSecret:        internalToken,
		AccessTokenSecret: accessTokenSecret,
		HTTPClient:        &http.Client{Timeout: 5 * time.Minute},
		Now:               time.Now,
	}
}

func (c *Client) SetRuntimeAPIURL(runtimeAPIURL string) {
	c.RuntimeAPIURL = strings.TrimRight(runtimeAPIURL, "/")
}

func (c *Client) StartRun(ctx context.Context, req StartRunRequest) (*RuntimeRun, error) {
	var out RuntimeRun
	if err := c.doJSON(ctx, http.MethodPost, "/internal/agent-runs", req, &out); err != nil {
		return nil, err
	}
	if out.AssistantID == "" {
		out.AssistantID = c.assistantID()
	}
	if out.RuntimeAPIURL == "" {
		out.RuntimeAPIURL = c.runtimeAPIURL()
	}
	return &out, nil
}

func (c *Client) GetRun(ctx context.Context, runID string) (*RuntimeRun, error) {
	var out RuntimeRun
	if err := c.doJSON(ctx, http.MethodGet, "/internal/agent-runs/"+runID, nil, &out); err != nil {
		return nil, err
	}
	if out.AssistantID == "" {
		out.AssistantID = c.assistantID()
	}
	if out.RuntimeAPIURL == "" {
		out.RuntimeAPIURL = c.runtimeAPIURL()
	}
	return &out, nil
}

func (c *Client) CancelRun(ctx context.Context, runID string) error {
	return c.doJSON(ctx, http.MethodPost, "/internal/agent-runs/"+runID+"/cancel", map[string]any{}, nil)
}

func (c *Client) ResumeRun(ctx context.Context, runID string, req ResumeRunRequest) (*RuntimeRun, error) {
	var out RuntimeRun
	if err := c.doJSON(ctx, http.MethodPost, "/internal/agent-runs/"+runID+"/resume", req, &out); err != nil {
		return nil, err
	}
	if out.AssistantID == "" {
		out.AssistantID = c.assistantID()
	}
	if out.RuntimeAPIURL == "" {
		out.RuntimeAPIURL = c.runtimeAPIURL()
	}
	return &out, nil
}

func (c *Client) IssueRuntimeAccessToken(req RuntimeAccessRequest) (*RuntimeAccessInfo, error) {
	if req.AssistantID == "" {
		req.AssistantID = c.assistantID()
	}
	runtimeAPIURL := strings.TrimRight(strings.TrimSpace(req.RuntimeAPIURL), "/")
	if runtimeAPIURL == "" {
		runtimeAPIURL = c.runtimeAPIURL()
	}
	if req.ExpiresIn <= 0 {
		req.ExpiresIn = 30 * time.Minute
	}
	now := c.now()
	expiresAt := now.Add(req.ExpiresIn).Unix()
	payload := map[string]any{
		"sub":          req.UserID,
		"org_id":       req.OrgID,
		"project_id":   req.ProjectID,
		"thread_id":    req.ThreadID,
		"assistant_id": req.AssistantID,
		"iat":          now.Unix(),
		"exp":          expiresAt,
		"nonce":        randomTokenNonce(),
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("agentruntime: marshal runtime access token: %w", err)
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(rawPayload)
	sig := signBytes([]byte(c.accessSecret()), []byte(encodedPayload))
	token := encodedPayload + "." + base64.RawURLEncoding.EncodeToString(sig)
	return &RuntimeAccessInfo{
		RuntimeAPIURL:      runtimeAPIURL,
		AssistantID:        req.AssistantID,
		ThreadID:           req.ThreadID,
		RuntimeAccessToken: token,
		ExpiresAtUnix:      expiresAt,
	}, nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, body any, out any) error {
	if c.BaseURL == "" {
		return fmt.Errorf("agentruntime: base URL is required")
	}
	var bodyBytes []byte
	var err error
	if body != nil {
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("agentruntime: marshal request: %w", err)
		}
	} else {
		bodyBytes = []byte{}
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("agentruntime: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.InternalToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.InternalToken)
	}
	c.signRequest(httpReq, bodyBytes)

	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("agentruntime: request failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("agentruntime: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{StatusCode: resp.StatusCode, Body: string(respBody)}
	}
	if out == nil || len(bytes.TrimSpace(respBody)) == 0 {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("agentruntime: decode response: %w", err)
	}
	return nil
}

func (c *Client) signRequest(req *http.Request, body []byte) {
	secret := c.hmacSecret()
	if secret == "" {
		return
	}
	ts := fmt.Sprintf("%d", c.now().Unix())
	req.Header.Set("X-Wemob-Timestamp", ts)
	macInput := append([]byte(ts+"."), body...)
	sig := signBytes([]byte(secret), macInput)
	req.Header.Set("X-Wemob-Signature", "sha256="+hex.EncodeToString(sig))
}

func (c *Client) assistantID() string {
	if c.AssistantID != "" {
		return c.AssistantID
	}
	return DefaultAssistantID
}

func (c *Client) runtimeAPIURL() string {
	if c.RuntimeAPIURL != "" {
		return strings.TrimRight(c.RuntimeAPIURL, "/")
	}
	return strings.TrimRight(c.BaseURL, "/")
}

func (c *Client) hmacSecret() string {
	if c.HMACSecret != "" {
		return c.HMACSecret
	}
	return c.InternalToken
}

func (c *Client) accessSecret() string {
	if c.AccessTokenSecret != "" {
		return c.AccessTokenSecret
	}
	return c.hmacSecret()
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func signBytes(secret []byte, payload []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}

func randomTokenNonce() string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}
