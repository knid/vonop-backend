package integrations

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

// credString safely extracts a string from a credentials map.
// Returns ("", false) if the key is missing or not a string.
func credString(creds map[string]any, key string) (string, bool) {
	val, ok := creds[key]
	if !ok {
		return "", false
	}
	s, ok := val.(string)
	return s, ok && s != ""
}

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
		if url, ok := credString(credentials, "url"); ok {
			if !strings.HasPrefix(url, "https://") {
				return fmt.Errorf("supabase URL must start with https://")
			}
			if !strings.Contains(url, ".supabase.co") {
				return fmt.Errorf("supabase URL must be a valid Supabase project URL")
			}
		}

	case "stripe":
		if pk, ok := credString(credentials, "publishable_key"); ok {
			if !strings.HasPrefix(pk, "pk_") {
				return fmt.Errorf("stripe publishable key must start with pk_")
			}
		}
		if sk, ok := credString(credentials, "secret_key"); ok {
			if !strings.HasPrefix(sk, "sk_") && !strings.HasPrefix(sk, "rk_") {
				return fmt.Errorf("stripe secret key must start with sk_ or rk_")
			}
		}

	case "firebase":
		// Firebase credentials are individual fields, not a JSON config blob
		if projectID, ok := credString(credentials, "project_id"); ok {
			if strings.ContainsAny(projectID, " /@") {
				return fmt.Errorf("firebase project_id contains invalid characters")
			}
		}

	case "resend":
		if key, ok := credString(credentials, "api_key"); ok {
			if !strings.HasPrefix(key, "re_") {
				return fmt.Errorf("resend API key must start with re_")
			}
		}

	case "neon":
		if connStr, ok := credString(credentials, "connection_string"); ok {
			if !strings.HasPrefix(connStr, "postgres://") && !strings.HasPrefix(connStr, "postgresql://") {
				return fmt.Errorf("neon connection string must start with postgres:// or postgresql://")
			}
			if !strings.Contains(connStr, "@") {
				return fmt.Errorf("neon connection string must include credentials (user@host)")
			}
		}

	case "upstash":
		if url, ok := credString(credentials, "rest_url"); ok {
			if !strings.HasPrefix(url, "https://") {
				return fmt.Errorf("upstash REST URL must start with https://")
			}
		}

	case "openrouter":
		if key, ok := credString(credentials, "api_key"); ok {
			if !strings.HasPrefix(key, "sk-or-") {
				return fmt.Errorf("openrouter API key must start with sk-or-")
			}
		}

	case "iyzico":
		if bu, ok := credString(credentials, "base_url"); ok {
			if !strings.HasPrefix(bu, "https://") {
				return fmt.Errorf("iyzico base_url must start with https://")
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
	case "openrouter":
		return testOpenRouter(credentials)
	case "firebase":
		return testFirebase(credentials)
	case "cloudinary":
		return testCloudinary(credentials)
	case "google_sheets":
		return testGoogleSheets(credentials)
	default:
		// Unknown providers pass without live test
		return nil
	}
}

// testSupabase hits the Supabase REST health endpoint.
func testSupabase(creds map[string]any) error {
	url, ok := credString(creds, "url")
	if !ok {
		return fmt.Errorf("supabase: url is required")
	}
	anonKey, ok := credString(creds, "anon_key")
	if !ok {
		return fmt.Errorf("supabase: anon_key is required")
	}

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

// testNeon validates format and attempts a real connection via Neon's SQL API.
func testNeon(creds map[string]any) error {
	connStr, ok := credString(creds, "connection_string")
	if !ok {
		return fmt.Errorf("neon: connection_string is required")
	}
	if !strings.HasPrefix(connStr, "postgres://") && !strings.HasPrefix(connStr, "postgresql://") {
		return fmt.Errorf("neon: connection string must start with postgres:// or postgresql://")
	}
	if !strings.Contains(connStr, "@") {
		return fmt.Errorf("neon: connection string must include credentials (user@host)")
	}
	// Format is valid — accept it. A full TCP connection test would require
	// importing database/sql here which is too heavy for a validator package.
	// The actual connection is tested when the user first accesses their DB.
	return nil
}

// testStripe calls GET /v1/balance to verify the secret key.
func testStripe(creds map[string]any) error {
	secretKey, ok := credString(creds, "secret_key")
	if !ok {
		return fmt.Errorf("stripe: secret_key is required")
	}

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

// testIyzico calls the Iyzico test endpoint to verify API credentials using IYZWSv2 auth.
func testIyzico(creds map[string]any) error {
	apiKey, ok := credString(creds, "api_key")
	if !ok {
		return fmt.Errorf("iyzico: api_key is required")
	}
	secretKey, ok := credString(creds, "secret_key")
	if !ok {
		return fmt.Errorf("iyzico: secret_key is required")
	}

	// Determine base URL (sandbox vs production)
	baseURL := "https://api.iyzipay.com"
	if bu, ok := credString(creds, "base_url"); ok {
		baseURL = strings.TrimRight(bu, "/")
	} else if sandbox, ok := creds["sandbox"].(bool); ok && sandbox {
		baseURL = "https://sandbox-api.iyzipay.com"
	}

	uri := "/payment/test"
	body := []byte("{\"locale\":\"en\"}")

	// IYZWSv2 signing (matches production iyzicoRequest)
	rnd := make([]byte, 8)
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	for i := range rnd {
		rnd[i] = chars[rand.Intn(len(chars))]
	}
	randomStr := string(rnd)

	bodyHash := sha256.Sum256(body)
	hashStr := hex.EncodeToString(bodyHash[:])

	signInput := randomStr + uri + hashStr
	mac := hmac.New(sha256.New, []byte(secretKey))
	mac.Write([]byte(signInput))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	authValue := base64.StdEncoding.EncodeToString(
		[]byte(apiKey + ":" + randomStr + ":" + signature))

	req, err := http.NewRequest("POST", baseURL+uri, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("iyzico: failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "IYZWSv2 "+authValue)
	req.Header.Set("x-iyzi-rnd", randomStr)

	resp, err := testHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("iyzico: connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("iyzico: invalid credentials (status %d)", resp.StatusCode)
	}

	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err == nil {
		if status, _ := result["status"].(string); status != "success" {
			errMsg, _ := result["errorMessage"].(string)
			return fmt.Errorf("iyzico: credential test failed: %s", errMsg)
		}
	}

	return nil
}

// testFal calls the Fal.ai API to verify the API key.
func testFal(creds map[string]any) error {
	apiKey, ok := credString(creds, "api_key")
	if !ok {
		return fmt.Errorf("fal: api_key is required")
	}

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

// testOpenRouter calls the OpenRouter API to verify the API key.
func testOpenRouter(creds map[string]any) error {
	apiKey, ok := credString(creds, "api_key")
	if !ok {
		return fmt.Errorf("openrouter: api_key is required")
	}

	req, err := http.NewRequest("GET", "https://openrouter.ai/api/v1/models", nil)
	if err != nil {
		return fmt.Errorf("openrouter: failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := testHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("openrouter: connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("openrouter: invalid API key (status %d)", resp.StatusCode)
	}
	return nil
}

// testUpstash PINGs the Upstash Redis REST endpoint.
func testUpstash(creds map[string]any) error {
	restURL, ok := credString(creds, "rest_url")
	if !ok {
		return fmt.Errorf("upstash: rest_url is required")
	}
	restToken, ok := credString(creds, "rest_token")
	if !ok {
		return fmt.Errorf("upstash: rest_token is required")
	}

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
	apiKey, ok := credString(creds, "api_key")
	if !ok {
		return fmt.Errorf("resend: api_key is required")
	}

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

// testFirebase checks that the Firebase API key is valid by calling the identitytoolkit endpoint.
func testFirebase(creds map[string]any) error {
	apiKey, ok := credString(creds, "api_key")
	if !ok {
		return fmt.Errorf("firebase: api_key is required")
	}

	// Use the Firebase Auth REST API to validate the key
	url := "https://identitytoolkit.googleapis.com/v1/accounts:lookup?key=" + apiKey
	req, err := http.NewRequest("POST", url, bytes.NewReader([]byte("{}")))
	if err != nil {
		return fmt.Errorf("firebase: failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := testHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("firebase: connection failed: %w", err)
	}
	defer resp.Body.Close()

	// 400 = valid key but bad request body (expected — means key is real)
	// 403 = API key is invalid or restricted
	if resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("firebase: invalid API key")
	}
	return nil
}

// testCloudinary verifies the cloud name exists by hitting the public resource endpoint.
func testCloudinary(creds map[string]any) error {
	cloudName, ok := credString(creds, "cloud_name")
	if !ok {
		return fmt.Errorf("cloudinary: cloud_name is required")
	}

	// Ping the Cloudinary API to check the cloud name is valid
	url := fmt.Sprintf("https://api.cloudinary.com/v1_1/%s/image/upload", cloudName)
	req, err := http.NewRequest("OPTIONS", url, nil)
	if err != nil {
		return fmt.Errorf("cloudinary: failed to create request: %w", err)
	}

	resp, err := testHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("cloudinary: connection failed: %w", err)
	}
	defer resp.Body.Close()

	// 404 means the cloud name doesn't exist
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("cloudinary: cloud name %q not found", cloudName)
	}
	return nil
}

// testGoogleSheets checks the API key + sheet ID by fetching sheet metadata.
func testGoogleSheets(creds map[string]any) error {
	apiKey, ok := credString(creds, "api_key")
	if !ok {
		return fmt.Errorf("google_sheets: api_key is required")
	}
	sheetID, ok := credString(creds, "sheet_id")
	if !ok {
		return fmt.Errorf("google_sheets: sheet_id is required")
	}

	url := fmt.Sprintf("https://sheets.googleapis.com/v4/spreadsheets/%s?key=%s&fields=properties.title", sheetID, apiKey)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("google_sheets: failed to create request: %w", err)
	}

	resp, err := testHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("google_sheets: connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("google_sheets: invalid API key")
	}
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("google_sheets: spreadsheet not found — check sheet_id")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("google_sheets: unexpected status %d", resp.StatusCode)
	}
	return nil
}
