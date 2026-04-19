package controllers

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
)

// ValidateAppleConnection downloads the .p8 key from S3, signs a short-lived
// App Store Connect JWT, and GETs /v1/apps to verify the credentials are valid.
func (c *Controller) ValidateAppleConnection(ctx context.Context, req requests.ValidateAppleConnectionRequest) responses.ValidationResult {
	// --- early-exit on missing fields ---
	if req.KeyID == "" || req.IssuerID == "" || req.TeamID == "" || req.P8KeyID == "" {
		missing := ""
		switch {
		case req.KeyID == "":
			missing = "Key ID"
		case req.IssuerID == "":
			missing = "Issuer ID"
		case req.TeamID == "":
			missing = "Team ID"
		case req.P8KeyID == "":
			missing = "P8 key"
		}
		return responses.ValidationResult{
			Valid:   false,
			Message: fmt.Sprintf("%s is required to validate your App Store Connect connection.", missing),
		}
	}

	// --- download .p8 bytes from S3 ---
	s3Key := "p8_keys/" + req.P8KeyID + ".p8"
	p8Bytes, err := c.UserApiKeysStorage.ReadObjectBytes(ctx, s3Key)
	if err != nil {
		return responses.ValidationResult{
			Valid:   false,
			Message: "Could not retrieve your .p8 key from storage. Please re-upload it and try again.",
		}
	}

	// --- parse the EC private key from PEM ---
	privateKey, err := parseECPrivateKeyFromP8(p8Bytes)
	if err != nil {
		return responses.ValidationResult{
			Valid:   false,
			Message: "The .p8 file could not be parsed as an EC private key. Make sure you uploaded the correct file from App Store Connect.",
		}
	}

	// --- sign a 10-minute App Store Connect JWT ---
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"iss": req.IssuerID,
		"iat": now.Unix(),
		"exp": now.Add(10 * time.Minute).Unix(),
		"aud": "appstoreconnect-v1",
	})
	token.Header["kid"] = req.KeyID
	token.Header["typ"] = "JWT"

	signed, err := token.SignedString(privateKey)
	if err != nil {
		return responses.ValidationResult{
			Valid:   false,
			Message: "Failed to sign the App Store Connect JWT. Please contact support if this persists.",
		}
	}

	// --- call App Store Connect API ---
	apiReq, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.appstoreconnect.apple.com/v1/apps?limit=20", nil)
	if err != nil {
		return responses.ValidationResult{
			Valid:   false,
			Message: "Failed to build the App Store Connect request.",
		}
	}
	apiReq.Header.Set("Authorization", "Bearer "+signed)

	resp, err := http.DefaultClient.Do(apiReq)
	if err != nil {
		return responses.ValidationResult{
			Valid:   false,
			Message: "Could not reach the App Store Connect API. Check your network connectivity and try again.",
		}
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

	switch resp.StatusCode {
	case http.StatusOK:
		// parse app names from response
		var asc struct {
			Data []struct {
				Attributes struct {
					Name string `json:"name"`
				} `json:"attributes"`
			} `json:"data"`
		}
		apps := []string{}
		if json.Unmarshal(bodyBytes, &asc) == nil {
			for _, d := range asc.Data {
				if d.Attributes.Name != "" {
					apps = append(apps, d.Attributes.Name)
				}
			}
		}
		var msg string
		switch len(apps) {
		case 0:
			msg = "Connection verified! Your key is valid, but no apps are visible yet (it may take a moment for new keys to propagate)."
		case 1:
			msg = fmt.Sprintf("Connection verified! We can see 1 app in your App Store Connect account: %s.", apps[0])
		default:
			msg = fmt.Sprintf("Connection verified! We can see %d apps in your App Store Connect account.", len(apps))
		}
		return responses.ValidationResult{Valid: true, Message: msg, Apps: apps}

	case http.StatusUnauthorized, http.StatusForbidden:
		return responses.ValidationResult{
			Valid:   false,
			Message: "Apple rejected the key. Double-check the Key ID, Issuer ID, and that the .p8 file matches the Key ID you entered.",
		}

	default:
		truncated := string(bodyBytes)
		if len(truncated) > 200 {
			truncated = truncated[:200] + "…"
		}
		return responses.ValidationResult{
			Valid:   false,
			Message: fmt.Sprintf("App Store Connect returned an unexpected status %d. Body: %s", resp.StatusCode, truncated),
		}
	}
}

// ValidateGooglePlayConnection validates a Google Play service account JSON by
// exchanging it for an OAuth2 access token and pinging the Play Publishing API.
//
// NOTE (Phase 3a): Full service-account OAuth2 token exchange is implemented below
// using the standard JWT-signed grant flow (no external SDK required).
func (c *Controller) ValidateGooglePlayConnection(ctx context.Context, req requests.ValidateGooglePlayConnectionRequest) responses.ValidationResult {
	if req.ServiceAccountID == "" {
		return responses.ValidationResult{
			Valid:   false,
			Message: "Service account key is required to validate your Google Play connection.",
		}
	}

	// --- download service account JSON from S3 ---
	s3Key := "service_accounts/" + req.ServiceAccountID + ".json"
	saBytes, err := c.UserApiKeysStorage.ReadObjectBytes(ctx, s3Key)
	if err != nil {
		return responses.ValidationResult{
			Valid:   false,
			Message: "Could not retrieve your service account key from storage. Please re-upload it and try again.",
		}
	}

	// --- parse the service account JSON ---
	var sa struct {
		Type                    string `json:"type"`
		ProjectID               string `json:"project_id"`
		PrivateKeyID            string `json:"private_key_id"`
		PrivateKey              string `json:"private_key"`
		ClientEmail             string `json:"client_email"`
		TokenURI                string `json:"token_uri"`
	}
	if err := json.Unmarshal(saBytes, &sa); err != nil {
		return responses.ValidationResult{
			Valid:   false,
			Message: "The service account file could not be parsed as valid JSON. Make sure you downloaded the correct file from Google Cloud Console.",
		}
	}
	if sa.Type != "service_account" || sa.ClientEmail == "" || sa.PrivateKey == "" {
		return responses.ValidationResult{
			Valid:   false,
			Message: "The uploaded file does not look like a Google service account key. Please download a Service Account JSON key from Google Cloud Console.",
		}
	}
	tokenURI := sa.TokenURI
	if tokenURI == "" {
		tokenURI = "https://oauth2.googleapis.com/token"
	}

	// --- sign a JWT for the service-account grant ---
	block, _ := pem.Decode([]byte(sa.PrivateKey))
	if block == nil {
		return responses.ValidationResult{
			Valid:   false,
			Message: "Could not decode the private key PEM block from the service account file.",
		}
	}
	rsaKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return responses.ValidationResult{
			Valid:   false,
			Message: "Could not parse the service account private key. The key may be malformed.",
		}
	}

	now := time.Now()
	grantToken := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss":   sa.ClientEmail,
		"sub":   sa.ClientEmail,
		"scope": "https://www.googleapis.com/auth/androidpublisher",
		"aud":   tokenURI,
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	})
	signedGrant, err := grantToken.SignedString(rsaKey)
	if err != nil {
		return responses.ValidationResult{
			Valid:   false,
			Message: "Failed to sign the Google OAuth2 grant JWT.",
		}
	}

	// --- exchange for access token ---
	tokenReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, tokenURI, nil)
	q := tokenReq.URL.Query()
	q.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
	q.Set("assertion", signedGrant)
	tokenReq.URL.RawQuery = q.Encode()
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	tokenResp, err := http.DefaultClient.Do(tokenReq)
	if err != nil {
		return responses.ValidationResult{
			Valid:   false,
			Message: "Could not reach the Google OAuth2 token endpoint. Check your network connectivity.",
		}
	}
	defer tokenResp.Body.Close()
	tokenBodyBytes, _ := io.ReadAll(io.LimitReader(tokenResp.Body, 4096))

	if tokenResp.StatusCode != http.StatusOK {
		var errResp struct {
			Error            string `json:"error"`
			ErrorDescription string `json:"error_description"`
		}
		_ = json.Unmarshal(tokenBodyBytes, &errResp)
		msg := "Google rejected the service account credentials."
		if errResp.ErrorDescription != "" {
			msg += " Details: " + errResp.ErrorDescription
		}
		return responses.ValidationResult{Valid: false, Message: msg}
	}

	var tokenJSON struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(tokenBodyBytes, &tokenJSON); err != nil || tokenJSON.AccessToken == "" {
		return responses.ValidationResult{
			Valid:   false,
			Message: "Google returned a token response but it could not be parsed.",
		}
	}

	// --- call the Android Publisher API ---
	// We use the "inappproducts" endpoint on a dummy package to trigger the auth
	// check without needing a specific package name. A 404 (app not found) still
	// confirms the credentials are valid; a 401/403 means bad key.
	pubReq, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://androidpublisher.googleapis.com/androidpublisher/v3/applications/com.android.example/inappproducts",
		nil)
	pubReq.Header.Set("Authorization", "Bearer "+tokenJSON.AccessToken)

	pubResp, err := http.DefaultClient.Do(pubReq)
	if err != nil {
		return responses.ValidationResult{
			Valid:   false,
			Message: "Could not reach the Google Play API. Check your network connectivity.",
		}
	}
	defer pubResp.Body.Close()
	_, _ = io.ReadAll(io.LimitReader(pubResp.Body, 512)) // drain

	switch pubResp.StatusCode {
	case http.StatusOK, http.StatusNotFound:
		// 404 = auth succeeded, package just doesn't exist — that's fine for validation
		return responses.ValidationResult{
			Valid:   true,
			Message: fmt.Sprintf("Connection verified! The service account %s has valid credentials for Google Play.", sa.ClientEmail),
		}
	case http.StatusUnauthorized, http.StatusForbidden:
		return responses.ValidationResult{
			Valid:   false,
			Message: "Google Play rejected the service account. Make sure the account has the 'Release manager' or 'Admin' role in Google Play Console.",
		}
	default:
		return responses.ValidationResult{
			Valid:   false,
			Message: fmt.Sprintf("Google Play returned an unexpected status %d when validating the service account.", pubResp.StatusCode),
		}
	}
}

// parseECPrivateKeyFromP8 parses a PKCS#8 EC private key from a .p8 PEM file
// as downloaded from App Store Connect.
func parseECPrivateKeyFromP8(pemBytes []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found in .p8 file")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse PKCS8 private key: %w", err)
	}
	ecKey, ok := key.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("key is not an EC private key (got %T)", key)
	}
	return ecKey, nil
}
