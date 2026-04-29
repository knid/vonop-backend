package events

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

// fieldSpec describes one custom field to ensure exists on Twenty's Person object.
type fieldSpec struct {
	ObjectMetadataId string `json:"objectMetadataId"`
	Name             string `json:"name"`
	Label            string `json:"label"`
	Type             string `json:"type"` // TEXT, BOOLEAN, NUMBER, UUID, DATE_TIME
	Description      string `json:"description,omitempty"`
}

var personCustomFieldDefs = []struct {
	Name        string
	Label       string
	Type        string
	Description string
}{
	{"wemobUserId", "Wemob User ID", "UUID", "Canonical FK to Wemob users.id"},
	{"verified", "Verified", "BOOLEAN", ""},
	{"provider", "Auth Provider", "TEXT", ""},
	{"subscriptionStatus", "Subscription Status", "TEXT", ""},
	{"subscriptionId", "Subscription ID", "TEXT", "Stripe subscription ID"},
	{"trialUsed", "Trial Used", "BOOLEAN", ""},
	{"lastEventName", "Last Event Name", "TEXT", ""},
	{"lastEventAt", "Last Event At", "DATE_TIME", ""},
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

	personID, existing, err := s.fetchPersonObject(ctx)
	if err != nil {
		slog.Error("twenty: fetch person object failed", "err", err)
		return
	}
	if personID == "" {
		slog.Error("twenty: person object not found in metadata")
		return
	}
	s.personObjectID = personID

	for _, f := range personCustomFieldDefs {
		if _, ok := existing[f.Name]; ok {
			continue
		}
		spec := fieldSpec{
			ObjectMetadataId: personID,
			Name:             f.Name,
			Label:            f.Label,
			Type:             f.Type,
			Description:      f.Description,
		}
		if err := s.createField(ctx, spec); err != nil {
			slog.Error("twenty: create field failed", "field", f.Name, "err", err)
			continue
		}
		slog.Info("twenty: created custom field", "field", f.Name)
	}
}

// fetchPersonObject returns the Person object's metadata UUID and the set of
// existing field names on it. Twenty's metadata API requires UUID lookups for
// any specific-object endpoint, so we list all objects and find the one with
// nameSingular == "person".
func (s *TwentyService) fetchPersonObject(ctx context.Context) (string, map[string]struct{}, error) {
	endpoint := s.baseURL + "/rest/metadata/objects"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	resp, err := s.client.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", nil, fmt.Errorf("twenty: %s %s", resp.Status, string(body))
	}
	var out struct {
		Data struct {
			Objects []struct {
				ID            string `json:"id"`
				NameSingular  string `json:"nameSingular"`
				Fields        []struct {
					Name string `json:"name"`
				} `json:"fields"`
			} `json:"objects"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", nil, err
	}
	for _, o := range out.Data.Objects {
		if o.NameSingular == "person" {
			names := make(map[string]struct{}, len(o.Fields))
			for _, f := range o.Fields {
				names[f.Name] = struct{}{}
			}
			return o.ID, names, nil
		}
	}
	return "", nil, nil
}

func (s *TwentyService) createField(ctx context.Context, f fieldSpec) error {
	body, err := json.Marshal(f)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/rest/metadata/fields", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
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
