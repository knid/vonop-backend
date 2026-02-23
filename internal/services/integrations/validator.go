package integrations

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ValidateIntegrationConfig validates integration-specific configuration
// beyond just credential fields. This checks provider-specific rules
// such as URL format, key prefix patterns, etc.
func ValidateIntegrationConfig(provider string, credentials map[string]any) error {
	// First run standard credential validation
	if err := ValidateCredentials(provider, credentials); err != nil {
		return err
	}

	// Provider-specific validation
	switch provider {
	case "supabase":
		if url, ok := credentials["url"].(string); ok {
			if !strings.HasPrefix(url, "https://") {
				return fmt.Errorf("supabase URL must start with https://")
			}
			if !strings.Contains(url, ".supabase.co") {
				return fmt.Errorf("supabase URL must be a valid Supabase project URL")
			}
		}

	case "stripe":
		if pk, ok := credentials["publishable_key"].(string); ok {
			if !strings.HasPrefix(pk, "pk_") {
				return fmt.Errorf("stripe publishable key must start with pk_")
			}
		}

	case "firebase":
		if config, ok := credentials["config"].(string); ok {
			if !strings.Contains(config, "apiKey") {
				return fmt.Errorf("firebase config must contain apiKey")
			}
		}

	case "resend":
		if key, ok := credentials["api_key"].(string); ok {
			if !strings.HasPrefix(key, "re_") {
				return fmt.Errorf("resend API key must start with re_")
			}
		}
	}

	return nil
}

var testHTTPClient = &http.Client{Timeout: 10 * time.Second}

// TestConnection tests if integration credentials work by making a lightweight
// API call to the provider. Returns nil on success, an error describing the
// failure otherwise.
func TestConnection(provider string, credentials map[string]any) error {
	if err := ValidateCredentials(provider, credentials); err != nil {
		return err
	}

	switch provider {
	case "supabase":
		return testSupabase(credentials)
	case "neon":
		return testNeon(credentials)
	case "stripe":
		return testStripe(credentials)
	case "iyzico":
		return testIyzico(credentials)
	case "fal":
		return testFal(credentials)
	case "upstash":
		return testUpstash(credentials)
	case "resend":
		return testResend(credentials)
	default:
		return nil
	}
}

// testSupabase hits the Supabase REST health endpoint.
func testSupabase(creds map[string]any) error {
	url := creds["url"].(string)
	anonKey := creds["anon_key"].(string)

	req, err := http.NewRequest("GET", strings.TrimRight(url, "/")+"/rest/v1/", nil)
	if err != nil {
		return fmt.Errorf("supabase: failed to create request: %w", err)
	}
	req.Header.Set("apikey", anonKey)
	req.Header.Set("Authorization", "Bearer "+anonKey)

	resp, err := testHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("supabase: connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("supabase: invalid anon key (status %d)", resp.StatusCode)
	}
	return nil
}

// testNeon validates the connection string format.
func testNeon(creds map[string]any) error {
	connStr := creds["connection_string"].(string)
	if !strings.HasPrefix(connStr, "postgres://") && !strings.HasPrefix(connStr, "postgresql://") {
		return fmt.Errorf("neon: connection string must start with postgres:// or postgresql://")
	}
	if !strings.Contains(connStr, "@") {
		return fmt.Errorf("neon: connection string must include credentials (user@host)")
	}
	return nil
}

// testStripe calls GET /v1/balance to verify the secret key.
func testStripe(creds map[string]any) error {
	secretKey := creds["secret_key"].(string)

	req, err := http.NewRequest("GET", "https://api.stripe.com/v1/balance", nil)
	if err != nil {
		return fmt.Errorf("stripe: failed to create request: %w", err)
	}
	req.SetBasicAuth(secretKey, "")

	resp, err := testHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("stripe: connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("stripe: invalid secret key")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("stripe: unexpected status %d", resp.StatusCode)
	}
	return nil
}

// testIyzico calls the Iyzico test endpoint to verify API credentials.
func testIyzico(creds map[string]any) error {
	apiKey := creds["api_key"].(string)
	secretKey := creds["secret_key"].(string)
	if apiKey == "" || secretKey == "" {
		return fmt.Errorf("iyzico: api_key and secret_key are required")
	}

	req, err := http.NewRequest("GET", "https://api.iyzipay.com/payment/test", nil)
	if err != nil {
		return fmt.Errorf("iyzico: failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "IYZWS "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := testHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("iyzico: connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("iyzico: invalid credentials (status %d)", resp.StatusCode)
	}
	return nil
}

// testFal calls the Fal.ai API to verify the API key.
func testFal(creds map[string]any) error {
	apiKey := creds["api_key"].(string)

	req, err := http.NewRequest("GET", "https://queue.fal.run/fal-ai/fast-sdxl", nil)
	if err != nil {
		return fmt.Errorf("fal: failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Key "+apiKey)

	resp, err := testHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("fal: connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("fal: invalid API key (status %d)", resp.StatusCode)
	}
	return nil
}

// testUpstash PINGs the Upstash Redis REST endpoint.
func testUpstash(creds map[string]any) error {
	restURL := creds["rest_url"].(string)
	restToken := creds["rest_token"].(string)

	req, err := http.NewRequest("GET", strings.TrimRight(restURL, "/")+"/PING", nil)
	if err != nil {
		return fmt.Errorf("upstash: failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+restToken)

	resp, err := testHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("upstash: connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("upstash: invalid credentials (status %d)", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(body, &result); err == nil {
		if result.Result != "PONG" {
			return fmt.Errorf("upstash: unexpected PING response: %s", result.Result)
		}
	}
	return nil
}

// testResend sends a validation request to the Resend API.
func testResend(creds map[string]any) error {
	apiKey := creds["api_key"].(string)

	req, err := http.NewRequest("GET", "https://api.resend.com/domains", nil)
	if err != nil {
		return fmt.Errorf("resend: failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := testHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("resend: connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("resend: invalid API key (status %d)", resp.StatusCode)
	}
	return nil
}
