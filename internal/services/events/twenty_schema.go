package events

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

// fieldSpec describes one custom field to ensure exists on Twenty's Person object.
type fieldSpec struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Type        string `json:"type"` // TEXT, BOOLEAN, NUMBER, UUID, DATE_TIME
	Description string `json:"description,omitempty"`
}

var personCustomFields = []fieldSpec{
	{Name: "wemobUserId", Label: "Wemob User ID", Type: "UUID", Description: "Canonical FK to Wemob users.id"},
	{Name: "verified", Label: "Verified", Type: "BOOLEAN"},
	{Name: "provider", Label: "Auth Provider", Type: "TEXT"},
	{Name: "subscriptionStatus", Label: "Subscription Status", Type: "TEXT"},
	{Name: "subscriptionId", Label: "Subscription ID", Type: "TEXT", Description: "Stripe subscription ID"},
	{Name: "trialUsed", Label: "Trial Used", Type: "BOOLEAN"},
	{Name: "lastEventName", Label: "Last Event Name", Type: "TEXT"},
	{Name: "lastEventAt", Label: "Last Event At", Type: "DATE_TIME"},
}

// EnsureSchema creates any missing custom fields on Twenty's Person object.
// Idempotent: 4xx "already exists" responses are silently skipped.
// Failure is logged but does not block app startup — CRM data may be stale
// until the next successful boot.
func (s *TwentyService) EnsureSchema(ctx context.Context) {
	if s.apiKey == "" {
		slog.Warn("twenty: EnsureSchema skipped — not initialized")
		return
	}

	existing, err := s.listPersonFieldNames(ctx)
	if err != nil {
		slog.Error("twenty: list fields failed", "err", err)
		return
	}

	for _, f := range personCustomFields {
		if _, ok := existing[f.Name]; ok {
			continue
		}
		if err := s.createPersonField(ctx, f); err != nil {
			slog.Error("twenty: create field failed", "field", f.Name, "err", err)
			continue
		}
		slog.Info("twenty: created custom field", "field", f.Name)
	}
}

func (s *TwentyService) listPersonFieldNames(ctx context.Context) (map[string]struct{}, error) {
	endpoint := s.baseURL + "/rest/metadata/objects/person"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("twenty: %s %s", resp.Status, string(body))
	}
	var out struct {
		Data struct {
			Object struct {
				Fields []struct {
					Name string `json:"name"`
				} `json:"fields"`
			} `json:"object"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	names := make(map[string]struct{}, len(out.Data.Object.Fields))
	for _, f := range out.Data.Object.Fields {
		names[f.Name] = struct{}{}
	}
	return names, nil
}

func (s *TwentyService) createPersonField(ctx context.Context, f fieldSpec) error {
	body, err := json.Marshal(f)
	if err != nil {
		return err
	}
	resp, err := s.do(http.MethodPost, s.baseURL+"/rest/metadata/objects/person/fields", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		// Treat 409 / 422 as already-exists.
		if resp.StatusCode == http.StatusConflict || resp.StatusCode == http.StatusUnprocessableEntity {
			return nil
		}
		return fmt.Errorf("twenty: %s %s", resp.Status, string(respBody))
	}
	return nil
}
