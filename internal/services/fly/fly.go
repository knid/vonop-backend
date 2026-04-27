package fly

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const machinesAPIBase = "https://api.machines.dev"

type Service struct {
	APIToken      string
	OrgSlug       string
	Region        string
	Registry      string
	WebhookSecret string
	httpClient    *http.Client
}

func NewService(apiToken, orgSlug, region, webhookSecret string) *Service {
	return &Service{
		APIToken:      apiToken,
		OrgSlug:       orgSlug,
		Region:        region,
		Registry:      "registry.fly.io",
		WebhookSecret: webhookSecret,
		httpClient:    &http.Client{Timeout: 60 * time.Second},
	}
}

// doRequest performs an authenticated request against the Fly Machines API.
// 4xx is logged at warn level (often expected — e.g. polling for a machine
// that doesn't exist yet); 5xx is logged at error level.
func (s *Service) doRequest(method, path string, body any, out any) error {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("fly: marshal: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, machinesAPIBase+path, reqBody)
	if err != nil {
		return fmt.Errorf("fly: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.APIToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("fly: do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("fly: read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		preview := string(respBody)
		if len(preview) > 500 {
			preview = preview[:500]
		}
		if resp.StatusCode < 500 {
			slog.Warn("Fly API client error", "status", resp.StatusCode, "path", path, "body", preview)
		} else {
			slog.Error("Fly API server error", "status", resp.StatusCode, "path", path, "body", preview)
		}
		return &APIError{Status: resp.StatusCode, Body: preview}
	}

	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("fly: unmarshal: %w", err)
		}
	}
	return nil
}

// APIError carries the HTTP status from a Fly API response so callers can
// classify retryable vs non-retryable failures.
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("fly API: status %d: %s", e.Status, e.Body)
}

// VerifyWebhookSignature checks the HMAC-SHA256 signature Fly sends with
// webhook payloads.
func (s *Service) VerifyWebhookSignature(payload []byte, sigHex string) bool {
	if s.WebhookSecret == "" || sigHex == "" {
		return false
	}
	want, err := hex.DecodeString(strings.TrimPrefix(sigHex, "sha256="))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(s.WebhookSecret))
	mac.Write(payload)
	return hmac.Equal(mac.Sum(nil), want)
}

// EnsureApp creates a Fly app if it doesn't already exist. Idempotent.
func (s *Service) EnsureApp(appName string) error {
	exists, err := s.AppExists(appName)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	body := map[string]any{
		"app_name": appName,
		"org_slug": s.OrgSlug,
		"network":  "default",
	}
	return s.doRequest("POST", "/v1/apps", body, nil)
}

// AppExists returns true if the named Fly app exists in the current org.
func (s *Service) AppExists(appName string) (bool, error) {
	err := s.doRequest("GET", "/v1/apps/"+appName, nil, nil)
	if err == nil {
		return true, nil
	}
	if apiErr, ok := err.(*APIError); ok && apiErr.Status == http.StatusNotFound {
		return false, nil
	}
	return false, err
}

// DeleteApp removes a Fly app and all its machines. Idempotent: succeeds
// silently if the app no longer exists.
func (s *Service) DeleteApp(appName string) error {
	err := s.doRequest("DELETE", "/v1/apps/"+appName+"?force=true", nil, nil)
	if apiErr, ok := err.(*APIError); ok && apiErr.Status == http.StatusNotFound {
		return nil
	}
	return err
}

// SyncSecrets sets all the listed secrets on a Fly app in one bulk call.
// Existing secrets not in the map are left alone — this matches Coolify's
// SyncEnvVars semantics. Idempotent.
func (s *Service) SyncSecrets(appName string, kv map[string]string) error {
	if len(kv) == 0 {
		return nil
	}
	body := map[string]any{
		"secrets": kv,
	}
	return s.doRequest("POST", "/v1/apps/"+appName+"/secrets", body, nil)
}
