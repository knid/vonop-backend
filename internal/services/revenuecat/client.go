// Package revenuecat is a thin client for the RevenueCat REST v2 API used to
// auto-provision a per-tenant project + iOS + Android apps + fetch public SDK
// keys for IAP. The org-level secret API key (Bearer token, scopes
// project_configuration:projects:read_write +
// project_configuration:apps:read_write) lives in Infisical as
// REVENUECAT_API_KEY; this package never persists it.
package revenuecat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Service is the RevenueCat REST v2 client. Construct with NewService; tests
// can redirect HTTP calls to an httptest.Server via SetTestEndpoint.
type Service struct {
	apiKey     string
	apiBaseURL string
	client     *http.Client
}

// NewService constructs a Service with a 30s default HTTP timeout.
func NewService(apiKey string) *Service {
	return &Service{
		apiKey: apiKey,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// SetTestEndpoint overrides the API base URL. Use only from tests.
func (s *Service) SetTestEndpoint(u string) {
	s.apiBaseURL = u
}

func (s *Service) baseURL() string {
	if s.apiBaseURL != "" {
		return s.apiBaseURL
	}
	return "https://api.revenuecat.com"
}

// Project is the subset of a RevenueCat project the controller cares about.
type Project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// CreateProject creates a new RC project. Caller is responsible for picking
// a name that is unique within the org; RC enforces uniqueness and returns
// 4xx on collision (which `do` propagates as a fail-fast error).
func (s *Service) CreateProject(ctx context.Context, name string) (*Project, error) {
	var out Project
	if err := s.do(ctx, "POST", "/v2/projects", map[string]string{"name": name}, &out); err != nil {
		return nil, fmt.Errorf("revenuecat: CreateProject: %w", err)
	}
	return &out, nil
}

// AppType is the discriminator for CreateAppParams.
type AppType string

const (
	AppTypeAppStore  AppType = "app_store"
	AppTypePlayStore AppType = "play_store"
)

// CreateAppParams is the body for POST /v2/projects/{project_id}/apps.
// Exactly one of AppStore or PlayStore must be set, matching Type.
type CreateAppParams struct {
	Name      string           `json:"name"`
	Type      AppType          `json:"type"`
	AppStore  *AppStoreConfig  `json:"app_store,omitempty"`
	PlayStore *PlayStoreConfig `json:"play_store,omitempty"`
}

// AppStoreConfig is the iOS sub-payload for CreateAppParams.
type AppStoreConfig struct {
	BundleID string `json:"bundle_id"`
}

// PlayStoreConfig is the Android sub-payload for CreateAppParams.
type PlayStoreConfig struct {
	PackageName string `json:"package_name"`
}

// App is the subset of a RevenueCat app the controller cares about.
type App struct {
	ID   string  `json:"id"`
	Type AppType `json:"type"`
	Name string  `json:"name"`
}

// CreateApp creates a new RC app inside the given project.
func (s *Service) CreateApp(ctx context.Context, projectID string, params CreateAppParams) (*App, error) {
	var out App
	path := fmt.Sprintf("/v2/projects/%s/apps", projectID)
	if err := s.do(ctx, "POST", path, params, &out); err != nil {
		return nil, fmt.Errorf("revenuecat: CreateApp(%s): %w", projectID, err)
	}
	return &out, nil
}

// PublicAPIKey is one entry from the public_api_keys list endpoint.
type PublicAPIKey struct {
	Key         string `json:"key"`
	Environment string `json:"environment"` // "production" | "sandbox" | "" / "default"
	Label       string `json:"label,omitempty"`
}

type listPublicAPIKeysResponse struct {
	Items []PublicAPIKey `json:"items"`
}

// ListPublicAPIKeys returns all public SDK keys for an app. Caller picks the
// production key (or whichever environment is needed) — see ProductionKeyForApp.
func (s *Service) ListPublicAPIKeys(ctx context.Context, projectID, appID string) ([]PublicAPIKey, error) {
	var out listPublicAPIKeysResponse
	path := fmt.Sprintf("/v2/projects/%s/apps/%s/public_api_keys", projectID, appID)
	if err := s.do(ctx, "GET", path, nil, &out); err != nil {
		return nil, fmt.Errorf("revenuecat: ListPublicAPIKeys(%s/%s): %w", projectID, appID, err)
	}
	return out.Items, nil
}

// ProductionKeyForApp picks the first production-environment key from the
// list, falling back to keys[0] if no key is tagged "production" but at least
// one key exists. Returns "" on empty input.
//
// Falling back rather than failing keeps builds green when RC returns a key
// tagged "" or "default" — the public key is still usable in the binary; the
// environment label is informational.
func ProductionKeyForApp(keys []PublicAPIKey) string {
	for _, k := range keys {
		if k.Environment == "production" {
			return k.Key
		}
	}
	if len(keys) > 0 {
		return keys[0].Key
	}
	return ""
}

// ErrAuth is returned on 401 (bad/missing API key or insufficient scope). The
// caller can match with errors.Is to surface a specific auth-failure message.
var ErrAuth = errors.New("revenuecat: authentication failed (401) — check REVENUECAT_API_KEY scope")

// ErrRateLimit is returned after exhausting the 429 retry budget.
var ErrRateLimit = errors.New("revenuecat: rate limit exceeded after retries")

// do runs one request with the documented retry policy:
//
//	429              → 3 retries with exponential backoff (1s, 2s, 4s)
//	5xx / network    → 1 retry
//	401              → wrap as ErrAuth, no retry
//	other 4xx        → fail fast (no retry)
//
// Worst-case wall time per call: ~7s (initial + 1s + 2s + 4s) under sustained
// 429s, then surfaces ErrRateLimit. The package-level 60-req/min Project
// Configuration rate limit is well within typical signup rates (~5 calls per
// provision: 1 project + 2 apps + 2 key fetches).
func (s *Service) do(ctx context.Context, method, path string, body any, out any) error {
	delays := []time.Duration{0, 1 * time.Second, 2 * time.Second, 4 * time.Second}
	var lastErr error

	for attempt, delay := range delays {
		if delay > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		var bodyReader io.Reader
		if body != nil {
			b, err := json.Marshal(body)
			if err != nil {
				return fmt.Errorf("marshal: %w", err)
			}
			bodyReader = bytes.NewReader(b)
		}

		req, err := http.NewRequestWithContext(ctx, method, s.baseURL()+path, bodyReader)
		if err != nil {
			return fmt.Errorf("new request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+s.apiKey)
		req.Header.Set("Accept", "application/json")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := s.client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("http: %w", err)
			// Network errors retry once (treated like 5xx). attempt 0 = initial,
			// attempt 1 = first retry; bail out at attempt >= 1.
			if attempt >= 1 {
				return lastErr
			}
			continue
		}
		respBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		switch {
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			if out != nil {
				if err := json.Unmarshal(respBody, out); err != nil {
					return fmt.Errorf("unmarshal %d response: %w", resp.StatusCode, err)
				}
			}
			return nil
		case resp.StatusCode == 401:
			return ErrAuth
		case resp.StatusCode == 429:
			lastErr = fmt.Errorf("%w: %s", ErrRateLimit, truncBody(respBody))
			// continue → retry with next delay (up to 3 retries from delays[1..3])
		case resp.StatusCode >= 500:
			lastErr = fmt.Errorf("server %d: %s", resp.StatusCode, truncBody(respBody))
			if attempt >= 1 {
				return lastErr
			}
		default:
			// 4xx other than 401 / 429 — fail fast.
			return fmt.Errorf("client %d: %s", resp.StatusCode, truncBody(respBody))
		}
	}
	return lastErr
}

// truncBody trims a response body for inclusion in error messages; RC
// returns verbose validation errors and we don't want to flood logs.
func truncBody(b []byte) string {
	const max = 512
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "...[truncated]"
}
