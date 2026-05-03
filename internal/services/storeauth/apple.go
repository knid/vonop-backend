package storeauth

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
)

// AppleConnectionInput is the minimum set of fields needed to mint an
// App Store Connect JWT. Kept decoupled from the DB model so callers
// (validation, polling) can both use it without circular imports.
type AppleConnectionInput struct {
	KeyID    string
	IssuerID string
	// P8PEM is the raw bytes of the .p8 file as downloaded from App
	// Store Connect.
	P8PEM []byte
}

// AppleToken signs a 10-minute App Store Connect JWT.
func AppleToken(in AppleConnectionInput) (string, error) {
	privateKey, err := parseECPrivateKeyFromP8(in.P8PEM)
	if err != nil {
		return "", err
	}
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"iss": in.IssuerID,
		"iat": now.Unix(),
		"exp": now.Add(10 * time.Minute).Unix(),
		"aud": "appstoreconnect-v1",
	})
	token.Header["kid"] = in.KeyID
	token.Header["typ"] = "JWT"
	return token.SignedString(privateKey)
}

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

// AppleCertificate represents one row from /v1/certificates.
type AppleCertificate struct {
	ID              string
	SerialNumber    string
	DisplayName     string
	CertificateType string // "DISTRIBUTION" | "DEVELOPMENT" | "DEVELOPER_ID_APPLICATION" | etc.
	ExpiresAt       time.Time
}

// AppleProvisioningProfile represents one row from /v1/profiles.
type AppleProvisioningProfile struct {
	ID          string
	UUID        string
	Name        string
	ProfileType string // "IOS_APP_STORE" | "IOS_APP_DEVELOPMENT" | etc.
	ExpiresAt   time.Time
}

const ascAPIBase = "https://api.appstoreconnect.apple.com"

// ascHTTPClient is the package-level HTTP client used for ASC calls.
// Has a 15-second timeout (T5 audit fix: don't use http.DefaultClient).
var ascHTTPClient = &http.Client{Timeout: 15 * time.Second}

// FetchCertificates calls /v1/certificates and returns the parsed list.
// Used by the credential health cron (P0.3 T8) to track cert expiry.
func FetchCertificates(ctx context.Context, in AppleConnectionInput) ([]AppleCertificate, error) {
	body, err := ascGetJSON(ctx, in, "/v1/certificates")
	if err != nil {
		return nil, fmt.Errorf("fetch certificates: %w", err)
	}
	return parseCertificatesResponse(body)
}

// FetchProvisioningProfiles calls /v1/profiles and returns the parsed list.
func FetchProvisioningProfiles(ctx context.Context, in AppleConnectionInput) ([]AppleProvisioningProfile, error) {
	body, err := ascGetJSON(ctx, in, "/v1/profiles")
	if err != nil {
		return nil, fmt.Errorf("fetch profiles: %w", err)
	}
	return parseProfilesResponse(body)
}

func parseCertificatesResponse(body []byte) ([]AppleCertificate, error) {
	var resp struct {
		Data []struct {
			ID         string `json:"id"`
			Attributes struct {
				SerialNumber    string `json:"serialNumber"`
				DisplayName     string `json:"displayName"`
				CertificateType string `json:"certificateType"`
				ExpirationDate  string `json:"expirationDate"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse certs: %w", err)
	}
	out := make([]AppleCertificate, 0, len(resp.Data))
	for _, d := range resp.Data {
		exp, _ := time.Parse(time.RFC3339, d.Attributes.ExpirationDate)
		out = append(out, AppleCertificate{
			ID:              d.ID,
			SerialNumber:    d.Attributes.SerialNumber,
			DisplayName:     d.Attributes.DisplayName,
			CertificateType: d.Attributes.CertificateType,
			ExpiresAt:       exp,
		})
	}
	return out, nil
}

func parseProfilesResponse(body []byte) ([]AppleProvisioningProfile, error) {
	var resp struct {
		Data []struct {
			ID         string `json:"id"`
			Attributes struct {
				Name           string `json:"name"`
				UUID           string `json:"uuid"`
				ProfileType    string `json:"profileType"`
				ExpirationDate string `json:"expirationDate"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse profiles: %w", err)
	}
	out := make([]AppleProvisioningProfile, 0, len(resp.Data))
	for _, d := range resp.Data {
		exp, _ := time.Parse(time.RFC3339, d.Attributes.ExpirationDate)
		out = append(out, AppleProvisioningProfile{
			ID:          d.ID,
			UUID:        d.Attributes.UUID,
			Name:        d.Attributes.Name,
			ProfileType: d.Attributes.ProfileType,
			ExpiresAt:   exp,
		})
	}
	return out, nil
}

// ascGetJSON performs an authenticated GET against the ASC API base.
func ascGetJSON(ctx context.Context, in AppleConnectionInput, path string) ([]byte, error) {
	token, err := AppleToken(in)
	if err != nil {
		return nil, fmt.Errorf("mint asc jwt: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ascAPIBase+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := ascHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("asc http: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MB cap
	if err != nil {
		return nil, fmt.Errorf("read asc body: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("asc %s returned %d: %s", path, resp.StatusCode, string(body))
	}
	return body, nil
}
