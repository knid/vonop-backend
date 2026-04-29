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

// fieldSpec is the body of POST /rest/metadata/fields.
type fieldSpec struct {
	ObjectMetadataId        string                  `json:"objectMetadataId"`
	Name                    string                  `json:"name"`
	Label                   string                  `json:"label"`
	Type                    string                  `json:"type"`
	Description             string                  `json:"description,omitempty"`
	Icon                    string                  `json:"icon,omitempty"`
	RelationCreationPayload *relationCreationPayload `json:"relationCreationPayload,omitempty"`
}

type relationCreationPayload struct {
	Type                   string `json:"type"` // MANY_TO_ONE | ONE_TO_MANY
	TargetObjectMetadataId string `json:"targetObjectMetadataId"`
	TargetFieldLabel       string `json:"targetFieldLabel"`
	TargetFieldIcon        string `json:"targetFieldIcon"`
}

// objectSpec is the body of POST /rest/metadata/objects.
type objectSpec struct {
	NameSingular  string `json:"nameSingular"`
	NamePlural    string `json:"namePlural"`
	LabelSingular string `json:"labelSingular"`
	LabelPlural   string `json:"labelPlural"`
	Description   string `json:"description,omitempty"`
	Icon          string `json:"icon,omitempty"`
}

var personCustomFieldDefs = []struct {
	Name        string
	Label       string
	Type        string
	Icon        string
	Description string
}{
	{"wemobUserId", "Wemob User ID", "UUID", "IconHash", "Canonical FK to Wemob users.id"},
	{"verified", "Verified", "BOOLEAN", "IconCheck", ""},
	{"provider", "Auth Provider", "TEXT", "IconLogin", ""},
	{"subscriptionStatus", "Subscription Status", "TEXT", "IconStatusChange", ""},
	{"subscriptionId", "Subscription ID", "TEXT", "IconCreditCard", "Stripe subscription ID"},
	{"trialUsed", "Trial Used", "BOOLEAN", "IconClock", ""},
	{"lastEventName", "Last Event Name", "TEXT", "IconBolt", ""},
	{"lastEventAt", "Last Event At", "DATE_TIME", "IconClockHour4", ""},
}

const (
	userEventObjectName = "userEvent"
)

// userEventCustomFieldDefs are added to the userEvent object after it exists.
// The "person" relation is added last so the reverse field "eventsCustom" lands
// on Person at the end (visible in Person's Timeline).
var userEventCustomFieldDefs = []fieldSpec{
	{Name: "eventName", Label: "Event Name", Type: "TEXT", Icon: "IconBolt"},
	{Name: "eventAt", Label: "Event At", Type: "DATE_TIME", Icon: "IconClock"},
	{Name: "payload", Label: "Payload", Type: "RAW_JSON", Icon: "IconBraces"},
	{Name: "wemobEventId", Label: "Wemob Event ID", Type: "TEXT", Icon: "IconHash"},
	// person RELATION is filled in dynamically (needs Person UUID).
}

// EnsureSchema makes the Twenty workspace match the shape this service writes to:
//
//   - Person gets the 8 custom fields (wemobUserId, verified, …).
//   - The "userEvent" custom object exists with eventName, eventAt, payload,
//     wemobEventId, and a many-to-one relation to Person (which auto-creates
//     the reverse "eventsCustom" field on Person, surfacing user events in the
//     Person Timeline).
//
// Idempotent: every step skips when the target already exists. Failures log
// at slog.Error but do not block app startup; CRM data may be stale until the
// next successful boot.
func (s *TwentyService) EnsureSchema(ctx context.Context) {
	if s.apiKey == "" {
		slog.Warn("twenty: EnsureSchema skipped — not initialized")
		return
	}

	personID, personFields, err := s.fetchObjectFields(ctx, "person")
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
		if _, ok := personFields[f.Name]; ok {
			continue
		}
		spec := fieldSpec{
			ObjectMetadataId: personID,
			Name:             f.Name,
			Label:            f.Label,
			Type:             f.Type,
			Icon:             f.Icon,
			Description:      f.Description,
		}
		if err := s.createField(ctx, spec); err != nil {
			slog.Error("twenty: create person field failed", "field", f.Name, "err", err)
			continue
		}
		slog.Info("twenty: created custom field", "object", "person", "field", f.Name)
	}

	// Ensure the userEvent object + fields exist.
	userEventID, userEventFields, err := s.fetchObjectFields(ctx, userEventObjectName)
	if err != nil {
		slog.Error("twenty: fetch userEvent object failed", "err", err)
		return
	}
	if userEventID == "" {
		userEventID, err = s.createObject(ctx, objectSpec{
			NameSingular:  userEventObjectName,
			NamePlural:    "userEvents",
			LabelSingular: "User Event",
			LabelPlural:   "User Events",
			Description:   "Lifecycle events fired from the Wemob backend (signup, login, deploys, payments, etc).",
			Icon:          "IconActivity",
		})
		if err != nil {
			slog.Error("twenty: create userEvent object failed", "err", err)
			return
		}
		slog.Info("twenty: created custom object", "object", userEventObjectName, "id", userEventID)
		userEventFields = map[string]struct{}{} // freshly created, no fields yet
	}
	s.userEventObjectID = userEventID

	for _, f := range userEventCustomFieldDefs {
		if _, ok := userEventFields[f.Name]; ok {
			continue
		}
		spec := f
		spec.ObjectMetadataId = userEventID
		if err := s.createField(ctx, spec); err != nil {
			slog.Error("twenty: create userEvent field failed", "field", f.Name, "err", err)
			continue
		}
		slog.Info("twenty: created custom field", "object", userEventObjectName, "field", f.Name)
	}

	// Person relation: many-to-one userEvent → Person. Twenty creates the
	// reverse one-to-many field on Person automatically (named "eventsCustom").
	if _, ok := userEventFields["person"]; !ok {
		spec := fieldSpec{
			ObjectMetadataId: userEventID,
			Name:             "person",
			Label:            "Person",
			Type:             "RELATION",
			Icon:             "IconUser",
			RelationCreationPayload: &relationCreationPayload{
				Type:                   "MANY_TO_ONE",
				TargetObjectMetadataId: personID,
				TargetFieldLabel:       "Events",
				TargetFieldIcon:        "IconActivity",
			},
		}
		if err := s.createField(ctx, spec); err != nil {
			slog.Error("twenty: create userEvent person relation failed", "err", err)
		} else {
			slog.Info("twenty: created relation", "from", userEventObjectName, "to", "person")
		}
	}
}

// fetchObjectFields returns the UUID of the named object plus the set of
// existing field names on it. Returns "" / nil if the object doesn't exist.
func (s *TwentyService) fetchObjectFields(ctx context.Context, nameSingular string) (string, map[string]struct{}, error) {
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
				ID           string `json:"id"`
				NameSingular string `json:"nameSingular"`
				Fields       []struct {
					Name string `json:"name"`
				} `json:"fields"`
			} `json:"objects"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", nil, err
	}
	for _, o := range out.Data.Objects {
		if o.NameSingular == nameSingular {
			names := make(map[string]struct{}, len(o.Fields))
			for _, f := range o.Fields {
				names[f.Name] = struct{}{}
			}
			return o.ID, names, nil
		}
	}
	return "", nil, nil
}

// createObject creates a new custom object. Returns the new object's UUID by
// re-listing metadata afterwards (the create endpoint's response shape is
// inconsistent on Twenty's side).
func (s *TwentyService) createObject(ctx context.Context, spec objectSpec) (string, error) {
	body, err := json.Marshal(spec)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/rest/metadata/objects", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("twenty: %s %s", resp.Status, string(respBody))
	}
	// Twenty's createObject response wraps under data.create<NameSingular> but
	// with empty fields in some versions. Look it up by name to be safe.
	id, _, err := s.fetchObjectFields(ctx, spec.NameSingular)
	if err != nil {
		return "", err
	}
	return id, nil
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
