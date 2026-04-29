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
	"os"
	"os/exec"
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

// EnsureApp creates a Fly app if it doesn't already exist, then allocates
// public IPs (free shared IPv4 + free dedicated IPv6) so the app is reachable
// at <name>.fly.dev. Without IPs, the Machines REST API path leaves apps
// unreachable from the public internet (only flyctl launch auto-allocates).
// Idempotent.
func (s *Service) EnsureApp(appName string) error {
	exists, err := s.AppExists(appName)
	if err != nil {
		return err
	}
	if !exists {
		body := map[string]any{
			"app_name": appName,
			"org_slug": s.OrgSlug,
			"network":  "default",
		}
		if err := s.doRequest("POST", "/v1/apps", body, nil); err != nil {
			// Concurrent creation: another caller won the race. Treat as success.
			if apiErr, ok := err.(*APIError); ok && apiErr.Status != http.StatusUnprocessableEntity {
				return err
			}
		}
	}
	// IP allocation is idempotent on flyctl's side — second call is a no-op
	// when an IP of that type already exists.
	return s.allocateIPs(appName)
}

// allocateIPs assigns a shared IPv4 + dedicated IPv6 to the app. Both are free.
// flyctl shell-out because Fly's Machines REST API doesn't expose IP allocation
// directly — it goes through the GraphQL API, and flyctl wraps it cleanly.
func (s *Service) allocateIPs(appName string) error {
	calls := [][]string{
		{"flyctl", "ips", "allocate-v6", "--app", appName},
		{"flyctl", "ips", "allocate-v4", "--shared", "--app", appName},
	}
	for _, args := range calls {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Env = []string{
			"HOME=" + os.Getenv("HOME"),
			"PATH=" + os.Getenv("PATH"),
			"FLY_API_TOKEN=" + s.APIToken,
			"FLY_NO_UPDATE_CHECK=1",
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			outStr := string(out)
			// Already-allocated errors are expected and fine.
			if strings.Contains(outStr, "already") {
				continue
			}
			return fmt.Errorf("flyctl ips %s: %w (%s)", args[2], err, truncate(outStr, 500))
		}
	}
	return nil
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

// SyncSecrets sets each listed secret on a Fly app via the per-label endpoint.
// Existing secrets not in the map are left alone (additive sync). Idempotent:
// re-setting an unchanged value is a no-op on Fly's side (digest stays the same).
//
// Fly's Machines API requires one POST per secret to /v1/apps/<app>/secrets/<label>;
// there is NO bulk endpoint despite what the URL pattern might suggest. POSTing
// to /v1/apps/<app>/secrets returns the *list* of existing secrets and silently
// ignores the request body — that bug burned us once already, hence this comment.
func (s *Service) SyncSecrets(appName string, kv map[string]string) error {
	if len(kv) == 0 {
		return nil
	}
	for k, v := range kv {
		body := map[string]any{
			"value": v,
			"type":  "shared",
		}
		if err := s.doRequest("POST", "/v1/apps/"+appName+"/secrets/"+k, body, nil); err != nil {
			return fmt.Errorf("set secret %q: %w", k, err)
		}
	}
	return nil
}
