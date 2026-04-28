package events

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
)

// TwentyService upserts a Person record on each SendEvent. The event name
// and per-event attributes are NOT persisted to Twenty (Twenty is for
// relationship data, not an event log). Only `lastEventName` and
// `lastEventAt` reflect the most recent event.
//
// Person resolution: the Twenty person ID is cached on models.User.TwentyPersonID
// so subsequent calls skip the lookup-by-wemobUserId query.
type TwentyService struct {
	apiKey  string
	baseURL string
	client  *http.Client
	db      *database.Database
}

func NewTwentyService(db *database.Database, apiKey, baseURL string) *TwentyService {
	return &TwentyService{
		apiKey:  apiKey,
		baseURL: baseURL,
		client:  &http.Client{Timeout: 8 * time.Second},
		db:      db,
	}
}

type twentyPerson struct {
	ID                 string       `json:"id,omitempty"`
	Name               twentyName   `json:"name,omitempty"`
	Emails             twentyEmails `json:"emails,omitempty"`
	WemobUserId        string       `json:"wemobUserId,omitempty"`
	Verified           bool         `json:"verified"`
	Provider           string       `json:"provider,omitempty"`
	SubscriptionStatus string       `json:"subscriptionStatus,omitempty"`
	SubscriptionId     string       `json:"subscriptionId,omitempty"`
	TrialUsed          bool         `json:"trialUsed"`
	LastEventName      string       `json:"lastEventName,omitempty"`
	LastEventAt        string       `json:"lastEventAt,omitempty"` // ISO 8601
}

type twentyName struct {
	FirstName string `json:"firstName,omitempty"`
	LastName  string `json:"lastName,omitempty"`
}

type twentyEmails struct {
	PrimaryEmail string `json:"primaryEmail,omitempty"`
}

func (s *TwentyService) SendEvent(user *models.User, event string, _ map[string]any) error {
	if s.apiKey == "" {
		return errors.New("twenty: not initialized")
	}
	if user == nil || user.ID.String() == "" {
		return nil
	}

	person := twentyPerson{
		Name:               twentyName{FirstName: user.Name},
		Emails:             twentyEmails{PrimaryEmail: user.Email},
		WemobUserId:        user.ID.String(),
		Verified:           user.Verified,
		Provider:           user.Provider,
		SubscriptionStatus: user.SubscriptionStatus,
		SubscriptionId:     user.SubscriptionID.String(),
		TrialUsed:          user.TrialUsed,
		LastEventName:      event,
		LastEventAt:        time.Now().UTC().Format(time.RFC3339),
	}

	personID := user.TwentyPersonID
	if personID == "" {
		// Try to find existing by wemobUserId.
		var err error
		personID, err = s.findPersonIDByWemobUserID(user.ID.String())
		if err != nil {
			slog.Warn("twenty: lookup failed", "err", err, "user_id", user.ID.String())
			// Fall through — try to create anyway.
		}
	}

	if personID != "" {
		if err := s.patchPerson(personID, person); err != nil {
			slog.Warn("twenty: patch failed", "err", err, "user_id", user.ID.String())
			return nil
		}
		return nil
	}

	createdID, err := s.createPerson(person)
	if err != nil {
		slog.Warn("twenty: create failed", "err", err, "user_id", user.ID.String())
		return nil
	}

	// Persist the new Person ID for future calls.
	if s.db != nil {
		if dbErr := s.db.SetUserTwentyPersonID(user.ID.String(), createdID); dbErr != nil {
			slog.Warn("twenty: persist person id failed", "err", dbErr, "user_id", user.ID.String())
		} else {
			user.TwentyPersonID = createdID
		}
	}
	return nil
}

func (s *TwentyService) findPersonIDByWemobUserID(wemobUserID string) (string, error) {
	q := url.Values{}
	q.Set("filter", fmt.Sprintf("wemobUserId[eq]:%s", wemobUserID))
	q.Set("limit", "1")
	endpoint := s.baseURL + "/rest/people?" + q.Encode()

	resp, err := s.do(http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("twenty: lookup %s: %s", resp.Status, string(body))
	}

	var out struct {
		Data struct {
			People []struct {
				ID string `json:"id"`
			} `json:"people"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if len(out.Data.People) == 0 {
		return "", nil
	}
	return out.Data.People[0].ID, nil
}

func (s *TwentyService) createPerson(person twentyPerson) (string, error) {
	body, err := json.Marshal(person)
	if err != nil {
		return "", err
	}
	resp, err := s.do(http.MethodPost, s.baseURL+"/rest/people", body)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("twenty: create %s: %s", resp.Status, string(respBody))
	}

	var out struct {
		Data struct {
			CreatePerson struct {
				ID string `json:"id"`
			} `json:"createPerson"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.Data.CreatePerson.ID, nil
}

func (s *TwentyService) patchPerson(id string, person twentyPerson) error {
	body, err := json.Marshal(person)
	if err != nil {
		return err
	}
	resp, err := s.do(http.MethodPatch, s.baseURL+"/rest/people/"+id, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("twenty: patch %s: %s", resp.Status, string(respBody))
	}
	return nil
}

// do issues an authenticated HTTP request with 2x exponential retry on 5xx/timeout.
func (s *TwentyService) do(method, requestURL string, body []byte) (*http.Response, error) {
	maxRetries := 2
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * time.Second)
		}
		var reader io.Reader
		if body != nil {
			reader = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(context.Background(), method, requestURL, reader)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+s.apiKey)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := s.client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode < 500 {
			return resp, nil
		}
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		lastErr = fmt.Errorf("twenty: %s %s", resp.Status, string(respBody))
	}
	if lastErr == nil {
		lastErr = errors.New("twenty: unknown error after retries")
	}
	return nil, lastErr
}
