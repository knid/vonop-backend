package fly

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// LogLine is one log entry from a deployed user-app on Fly.
type LogLine struct {
	Timestamp string `json:"timestamp"` // RFC3339
	Level     string `json:"level"`     // "info" | "warn" | "error" | "" (unknown)
	Message   string `json:"message"`
}

// GetLogs returns recent log lines for the given Fly app. Source is Fly's
// GraphQL `recentLogs` (~last 5 minutes of buffered output). The `since`
// and `level` filters are applied client-side on the returned buffer.
//
// `since`: zero value disables the filter; otherwise lines with a parseable
// Timestamp older than since are dropped. The boundary is inclusive — a line
// timestamped exactly at `since` is kept. Lines whose Timestamp does not parse
// as RFC3339 (empty, malformed, or non-conforming) are always included
// regardless of the filter.
// `level`: empty string disables the filter; otherwise an exact match.
func (s *Service) GetLogs(appName string, since time.Time, level string) ([]LogLine, error) {
	payload := map[string]any{
		"query":     `query($name: String!) { app(name: $name) { recentLogs { timestamp level message } } }`,
		"variables": map[string]any{"name": appName},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("fly: marshal logs query: %w", err)
	}

	req, err := http.NewRequest("POST", s.GraphQLBase, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("fly: create logs request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.APIToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fly: do logs request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("fly: read logs response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("fly logs: status %d: %s", resp.StatusCode, string(raw))
	}

	var parsed struct {
		Data struct {
			App struct {
				RecentLogs []LogLine `json:"recentLogs"`
			} `json:"app"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("fly: parse logs response: %w", err)
	}
	if len(parsed.Errors) > 0 {
		return nil, fmt.Errorf("fly logs graphql: %s", parsed.Errors[0].Message)
	}

	out := make([]LogLine, 0, len(parsed.Data.App.RecentLogs))
	for _, ln := range parsed.Data.App.RecentLogs {
		if level != "" && ln.Level != level {
			continue
		}
		if !since.IsZero() {
			t, err := time.Parse(time.RFC3339, ln.Timestamp)
			if err == nil && t.Before(since) {
				continue
			}
		}
		out = append(out, ln)
	}
	return out, nil
}
