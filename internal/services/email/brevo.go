package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	brevo "github.com/getbrevo/brevo-go/lib"
	"github.com/knid/wemob/internal/models"
)

type BrevoEmailService struct {
	apiKey string
	client *brevo.APIClient
}

func NewBrevoEmailService(apiKey string) *BrevoEmailService {
	cfg := brevo.NewConfiguration()
	cfg.HTTPClient = &http.Client{Timeout: 10 * time.Second}

	return &BrevoEmailService{
		apiKey: apiKey,
		client: brevo.NewAPIClient(cfg),
	}
}

func (s *BrevoEmailService) SendEmail(user *models.User, templateID int, params map[string]any) (string, error) {
	if s.apiKey == "" || s.client == nil {
		return "", errors.New("brevo email service not initialized")
	}

	if user == nil {
		return "", errors.New("user is required")
	}

	if templateID == 0 {
		return "", errors.New("template id is required")
	}

	send := brevo.SendSmtpEmail{
		TemplateId: int64(templateID),
		To: []brevo.SendSmtpEmailTo{
			{Email: user.Email, Name: user.Name},
		},
		Params: params,
	}

	ctx := context.WithValue(context.Background(), brevo.ContextAPIKey, brevo.APIKey{Key: s.apiKey})

	var result brevo.CreateSmtpEmail
	var resp *http.Response
	var err error
	maxRetries := 2
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * time.Second)
			slog.Warn("Retrying brevo email send", "attempt", attempt+1)
		}
		result, resp, err = s.client.TransactionalEmailsApi.SendTransacEmail(ctx, send)
		if err == nil && (resp == nil || resp.StatusCode < 500) {
			break
		}
		if attempt == maxRetries {
			break
		}
	}
	if err != nil {
		var apiErr brevo.GenericSwaggerError
		if errors.As(err, &apiErr) && len(apiErr.Body()) > 0 {
			return "", fmt.Errorf("brevo send transactional email failed: %s: %s", apiErr.Error(), string(apiErr.Body()))
		}
		return "", fmt.Errorf("brevo send transactional email failed: %w", err)
	}

	if resp != nil && resp.StatusCode >= 300 {
		return "", fmt.Errorf("brevo send transactional email failed: %s", resp.Status)
	}

	if result.MessageId != "" {
		return result.MessageId, nil
	}

	if len(result.MessageIds) > 0 {
		return result.MessageIds[0], nil
	}

	return "", fmt.Errorf("email send returned no message ID")
}
