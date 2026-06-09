package deepagents

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// DeepAgentsRunRequest is the JSON body sent to the Python bridge.
type DeepAgentsRunRequest struct {
	SchemaVersion     string           `json:"schema_version"`
	JobID             string           `json:"job_id"`
	RunID             string           `json:"run_id"`
	TraceID           string           `json:"trace_id"`
	UserID            string           `json:"user_id"`
	OrganizationID    string           `json:"organization_id"`
	ProjectID         string           `json:"project_id"`
	ProjectType       string           `json:"project_type"`
	FrontendRuntime   string           `json:"frontend_runtime"`
	IsFirstGeneration bool             `json:"is_first_generation"`
	ReadOnly          bool             `json:"read_only"`
	Message           string           `json:"message"`
	History           []map[string]any `json:"history"`
	Attachments       []Attachment     `json:"attachments"`
	Repo              RepoSpec         `json:"repo"`
	Model             ModelSpec        `json:"model"`
	Limits            Limits           `json:"limits"`
	Tools             []ToolSpec       `json:"tools"`
	Skills            SkillsSpec       `json:"skills"`
	Metadata          map[string]any   `json:"metadata"`
}

type RepoSpec struct {
	MountBase   string `json:"mount_base"`
	ProjectID   string `json:"project_id"`
	VirtualRoot string `json:"virtual_root"`
}

type ModelSpec struct {
	Provider       string `json:"provider"`
	Model          string `json:"model"`
	LangchainModel string `json:"langchain_model"`
}

type Limits struct {
	MaxWallclockMs         int `json:"max_wallclock_ms"`
	MaxModelCalls          int `json:"max_model_calls"`
	MaxToolCalls           int `json:"max_tool_calls"`
	MaxOutputTokensPerCall int `json:"max_output_tokens_per_call"`
}

type ToolSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Schema      map[string]any `json:"schema"`
	RiskLevel   string         `json:"risk_level"`
}

type SkillsSpec struct {
	Paths []string `json:"paths"`
	IDs   []string `json:"ids"`
}

type Attachment struct {
	AssetID     string `json:"asset_id,omitempty"`
	FileName    string `json:"file_name,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	URL         string `json:"url,omitempty"`
	IsImage     bool   `json:"is_image,omitempty"`
}

type DeepAgentsEvent struct {
	Type       string          `json:"type"`
	Seq        int             `json:"seq"`
	RunID      string          `json:"run_id"`
	JobID      string          `json:"job_id"`
	TimeUnixMs int64           `json:"time_unix_ms"`
	Data       json.RawMessage `json:"data"`
}

// Client communicates with the scaffolded deepagents Python runtime.
type Client struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

var defaultHTTPClient = &http.Client{Timeout: 2 * time.Minute}

func NewClient(baseURL, token string) *Client {
	return &Client{BaseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"), Token: strings.TrimSpace(token)}
}

func (c *Client) httpClient() *http.Client {
	if c == nil || c.HTTPClient == nil {
		return defaultHTTPClient
	}
	return c.HTTPClient
}

func (c *Client) StreamRun(ctx context.Context, req DeepAgentsRunRequest, onEvent func(DeepAgentsEvent) error) error {
	if c == nil {
		return fmt.Errorf("deepagents: nil client")
	}
	if onEvent == nil {
		return fmt.Errorf("deepagents: onEvent is required")
	}
	if strings.TrimSpace(req.RunID) == "" {
		return fmt.Errorf("deepagents: run_id is required")
	}
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("deepagents: marshal request: %w", err)
	}

	endpoint := strings.TrimRight(c.BaseURL, "/") + "/internal/v1/runs/stream"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("deepagents: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/x-ndjson")
	if strings.TrimSpace(c.Token) != "" {
		httpReq.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.Token))
	}

	resp, err := c.httpClient().Do(httpReq)
	if err != nil {
		return fmt.Errorf("deepagents: stream request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		msg := strings.TrimSpace(string(b))
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return fmt.Errorf("deepagents: stream failed (%d): %s", resp.StatusCode, msg)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 32*1024), 4*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var event DeepAgentsEvent
		if err := json.Unmarshal(line, &event); err != nil {
			slog.Warn("deepagents: skipping malformed event line", "error", err)
			continue
		}
		if err := onEvent(event); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("deepagents: read event stream: %w", err)
	}

	return nil
}

func (c *Client) CancelRun(ctx context.Context, runID string) error {
	if c == nil {
		return fmt.Errorf("deepagents: nil client")
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return fmt.Errorf("deepagents: run_id is required")
	}
	endpoint := strings.TrimRight(c.BaseURL, "/") + "/internal/v1/runs/" + runID + "/cancel"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return fmt.Errorf("deepagents: create cancel request: %w", err)
	}
	if strings.TrimSpace(c.Token) != "" {
		httpReq.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.Token))
	}
	resp, err := c.httpClient().Do(httpReq)
	if err != nil {
		return fmt.Errorf("deepagents: cancel request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		msg := strings.TrimSpace(string(b))
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return fmt.Errorf("deepagents: cancel failed (%d): %s", resp.StatusCode, msg)
	}

	return nil
}

func (c *Client) Heartbeat(ctx context.Context, runID string) error {
	if c == nil {
		return fmt.Errorf("deepagents: nil client")
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return fmt.Errorf("deepagents: run_id is required")
	}
	endpoint := strings.TrimRight(c.BaseURL, "/") + "/internal/v1/runs/" + runID + "/heartbeat"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return err
	}
	if strings.TrimSpace(c.Token) != "" {
		httpReq.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.Token))
	}
	resp, err := c.httpClient().Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("deepagents: heartbeat failed (%d)", resp.StatusCode)
	}
	return nil
}
