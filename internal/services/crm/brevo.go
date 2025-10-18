package crm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	brevo "github.com/getbrevo/brevo-go/lib"
	"github.com/knid/wemob/internal/models"
)

type BrevoCRMService struct {
	apiKey string
	client *brevo.APIClient
}

func NewBrevoCRMService(apiKey string) *BrevoCRMService {
	cfg := brevo.NewConfiguration()
	cfg.HTTPClient = &http.Client{Timeout: 5 * time.Second}

	s := BrevoCRMService{}

	s.apiKey = apiKey
	s.client = brevo.NewAPIClient(cfg)

	return &s
}

func (s *BrevoCRMService) SendEvent(user *models.User, event string, attributes map[string]any) error {
	if s.apiKey == "" || s.client == nil {
		return errors.New("brevo service not initialized")
	}

	if user == nil {
		return errors.New("user is required")
	}

	if event == "" {
		return errors.New("event is required")
	}

	props := make(map[string]any, len(attributes)+1)
	for k, v := range attributes {
		props[k] = v
	}

	props["user_id"] = user.ID.String()

	eventPayload := brevo.Event{
		EventName: event,
		Identifiers: &brevo.EventIdentifiers{
			EmailId: user.Email,
			ExtId:   user.ID.String(),
		},
		EventProperties: props,
	}

	eventPayload.ContactProperties = map[string]interface{}{
		"FIRSTNAME":           user.Name,
		"VERIFIED":            user.Verified,
		"PROVIDER":            user.Provider,
		"SUBSCRIPTION_ID":     user.SubscriptionID,
		"SUBSCRIPTION_STATUS": user.SubscriptionStatus,
		"TRIAL_USED":          user.TrialUsed,
	}

	ctx := context.WithValue(context.Background(), brevo.ContextAPIKey, brevo.APIKey{Key: s.apiKey})

	resp, err := s.client.EventsApi.CreateEvent(ctx, eventPayload)
	if err != nil {
		var apiErr brevo.GenericSwaggerError
		if errors.As(err, &apiErr) && len(apiErr.Body()) > 0 {
			return fmt.Errorf("brevo create event failed: %s: %s", apiErr.Error(), string(apiErr.Body()))
		}
		return fmt.Errorf("brevo create event failed: %w", err)
	}

	if resp != nil && resp.StatusCode >= 300 {
		return fmt.Errorf("brevo create event failed: %s", resp.Status)
	}

	return nil
}
