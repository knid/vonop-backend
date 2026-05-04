package storeauth

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
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

// ascAPIBase is the App Store Connect API base URL. Declared as a var so
// SetASCAPIBaseForTest can override it for integration testing.
var ascAPIBase = "https://api.appstoreconnect.apple.com"

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
		exp, err := time.Parse(time.RFC3339, d.Attributes.ExpirationDate)
		if err != nil {
			slog.Warn("apple cert: invalid expiration date, skipping",
				"id", d.ID,
				"raw", d.Attributes.ExpirationDate,
				"err", err)
			continue
		}
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
		exp, err := time.Parse(time.RFC3339, d.Attributes.ExpirationDate)
		if err != nil {
			slog.Warn("apple profile: invalid expiration date, skipping",
				"id", d.ID,
				"raw", d.Attributes.ExpirationDate,
				"err", err)
			continue
		}
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

// AppleAppStoreVersion is the parsed shape we surface to callers from
// /v1/appStoreVersions/{id}. Used by the release status polling worker
// (P1#16).
type AppleAppStoreVersion struct {
	ID            string
	AppStoreState string // "WAITING_FOR_REVIEW" | "IN_REVIEW" | "READY_FOR_SALE" | …
	VersionString string
	Platform      string // "IOS" | "MAC_OS" | "TV_OS" | …
}

// FetchAppStoreVersion calls /v1/appStoreVersions/{id} and returns the parsed
// AppStoreVersion. Used by the release status polling worker (P1#16) to
// detect transitions on in-flight uploads.
func FetchAppStoreVersion(ctx context.Context, in AppleConnectionInput, versionID string) (*AppleAppStoreVersion, error) {
	if versionID == "" {
		return nil, fmt.Errorf("versionID required")
	}
	body, err := ascGetJSON(ctx, in, "/v1/appStoreVersions/"+versionID)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Data struct {
			ID         string `json:"id"`
			Attributes struct {
				AppStoreState string `json:"appStoreState"`
				VersionString string `json:"versionString"`
				Platform      string `json:"platform"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse app store version: %w", err)
	}
	return &AppleAppStoreVersion{
		ID:            resp.Data.ID,
		AppStoreState: resp.Data.Attributes.AppStoreState,
		VersionString: resp.Data.Attributes.VersionString,
		Platform:      resp.Data.Attributes.Platform,
	}, nil
}

// PatchAppStoreVersionPhasedReleaseState updates a phased release's state via
// PATCH /v1/appStoreVersionPhasedReleases/{id}. State must be one of:
// "ACTIVE" | "PAUSED" | "COMPLETE". Apple uses this to halt or resume staged
// rollouts (P1#16 HaltPhasedRollout).
func PatchAppStoreVersionPhasedReleaseState(ctx context.Context, in AppleConnectionInput, phasedReleaseID, state string) error {
	if phasedReleaseID == "" {
		return fmt.Errorf("phasedReleaseID required")
	}
	switch state {
	case "ACTIVE", "PAUSED", "COMPLETE":
	default:
		return fmt.Errorf("invalid phasedReleaseState %q", state)
	}
	token, err := AppleToken(in)
	if err != nil {
		return fmt.Errorf("mint asc jwt: %w", err)
	}
	payload := map[string]any{
		"data": map[string]any{
			"type": "appStoreVersionPhasedReleases",
			"id":   phasedReleaseID,
			"attributes": map[string]any{
				"phasedReleaseState": state,
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode patch payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch,
		ascAPIBase+"/v1/appStoreVersionPhasedReleases/"+phasedReleaseID,
		strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := ascHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("asc patch: %w", err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return fmt.Errorf("asc PATCH /v1/appStoreVersionPhasedReleases/%s returned %d: %s",
			phasedReleaseID, resp.StatusCode, string(got))
	}
	return nil
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

// ascPostJSON performs an authenticated POST with a JSON body against the ASC API base.
// Used by certificate / bundleId / profile creation endpoints (P1 #14).
func ascPostJSON(ctx context.Context, in AppleConnectionInput, path string, payload []byte) ([]byte, error) {
	token, err := AppleToken(in)
	if err != nil {
		return nil, fmt.Errorf("mint asc jwt: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ascAPIBase+path, strings.NewReader(string(payload)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := ascHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("asc http: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20)) // 4 MB cap (profiles can be ~10 KB but mobileprovision payloads vary)
	if err != nil {
		return nil, fmt.Errorf("read asc body: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("asc POST %s returned %d: %s", path, resp.StatusCode, string(body))
	}
	return body, nil
}

// CreatedCertificate is the result of POST /v1/certificates. CertificateContent
// is the DER-encoded cert (the field is named the same as Apple's response
// — opaque base64 payload).
type CreatedCertificate struct {
	ID                 string
	CertificateType    string
	SerialNumber       string
	DisplayName        string
	ExpiresAt          time.Time
	CertificateContent string // base64-encoded DER
}

// CreateCertificate calls POST /v1/certificates to mint a fresh distribution
// (or development) certificate from a CSR.
//
// csrContent is the PEM-formatted certificate signing request bytes — Apple's
// API wants the *base64 of the entire PEM document* (including the
// "-----BEGIN CERTIFICATE REQUEST-----" header). Caller is responsible for
// providing the PEM bytes; we base64-encode here.
//
// certType is one of "IOS_DISTRIBUTION", "IOS_DEVELOPMENT",
// "MAC_APP_DISTRIBUTION", etc. P1 #14 only uses IOS_DISTRIBUTION.
//
// API ref: https://developer.apple.com/documentation/appstoreconnectapi/create_a_certificate
func CreateCertificate(ctx context.Context, in AppleConnectionInput, csrPEM []byte, certType string) (*CreatedCertificate, error) {
	csrB64 := base64.StdEncoding.EncodeToString(csrPEM)
	payload := map[string]any{
		"data": map[string]any{
			"type": "certificates",
			"attributes": map[string]any{
				"csrContent":      csrB64,
				"certificateType": certType,
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal cert payload: %w", err)
	}
	respBody, err := ascPostJSON(ctx, in, "/v1/certificates", body)
	if err != nil {
		return nil, fmt.Errorf("create certificate: %w", err)
	}
	var resp struct {
		Data struct {
			ID         string `json:"id"`
			Attributes struct {
				CertificateContent string `json:"certificateContent"`
				CertificateType    string `json:"certificateType"`
				SerialNumber       string `json:"serialNumber"`
				DisplayName        string `json:"displayName"`
				ExpirationDate     string `json:"expirationDate"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("parse cert create response: %w", err)
	}
	out := &CreatedCertificate{
		ID:                 resp.Data.ID,
		CertificateType:    resp.Data.Attributes.CertificateType,
		SerialNumber:       resp.Data.Attributes.SerialNumber,
		DisplayName:        resp.Data.Attributes.DisplayName,
		CertificateContent: resp.Data.Attributes.CertificateContent,
	}
	if resp.Data.Attributes.ExpirationDate != "" {
		if t, err := time.Parse(time.RFC3339, resp.Data.Attributes.ExpirationDate); err == nil {
			out.ExpiresAt = t
		}
	}
	return out, nil
}

// AppleBundleID represents one row from /v1/bundleIds.
type AppleBundleID struct {
	ID         string
	Identifier string
	Name       string
}

// FetchBundleIDByIdentifier looks up a bundle id by its reverse-DNS identifier
// (e.g. "com.wemob.myapp"). Returns (nil, nil) when not found so the caller
// can decide to create one.
//
// API ref: https://developer.apple.com/documentation/appstoreconnectapi/list_bundle_ids
func FetchBundleIDByIdentifier(ctx context.Context, in AppleConnectionInput, identifier string) (*AppleBundleID, error) {
	// filter[identifier] is a documented ASC filter; Apple returns at most
	// one match per identifier (they're globally unique per team).
	body, err := ascGetJSON(ctx, in, "/v1/bundleIds?filter[identifier]="+url.QueryEscape(identifier)+"&limit=1")
	if err != nil {
		return nil, fmt.Errorf("fetch bundle id: %w", err)
	}
	var resp struct {
		Data []struct {
			ID         string `json:"id"`
			Attributes struct {
				Identifier string `json:"identifier"`
				Name       string `json:"name"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse bundle ids: %w", err)
	}
	if len(resp.Data) == 0 {
		return nil, nil
	}
	return &AppleBundleID{
		ID:         resp.Data[0].ID,
		Identifier: resp.Data[0].Attributes.Identifier,
		Name:       resp.Data[0].Attributes.Name,
	}, nil
}

// CreateBundleID registers a new bundle id (reverse-DNS identifier) under
// the team. iOS platform is hard-coded since P1 #14 only targets iOS.
//
// API ref: https://developer.apple.com/documentation/appstoreconnectapi/register_a_new_bundle_id
func CreateBundleID(ctx context.Context, in AppleConnectionInput, identifier, name string) (*AppleBundleID, error) {
	if name == "" {
		// Apple requires a "name"; default to a sanitized identifier.
		name = identifier
	}
	payload := map[string]any{
		"data": map[string]any{
			"type": "bundleIds",
			"attributes": map[string]any{
				"identifier": identifier,
				"name":       name,
				"platform":   "IOS",
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal bundleid payload: %w", err)
	}
	respBody, err := ascPostJSON(ctx, in, "/v1/bundleIds", body)
	if err != nil {
		return nil, fmt.Errorf("create bundle id: %w", err)
	}
	var resp struct {
		Data struct {
			ID         string `json:"id"`
			Attributes struct {
				Identifier string `json:"identifier"`
				Name       string `json:"name"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("parse bundleid response: %w", err)
	}
	return &AppleBundleID{
		ID:         resp.Data.ID,
		Identifier: resp.Data.Attributes.Identifier,
		Name:       resp.Data.Attributes.Name,
	}, nil
}

// CreatedProfile is the response of POST /v1/profiles.
type CreatedProfile struct {
	ID             string
	UUID           string
	Name           string
	ProfileType    string
	ProfileContent string // base64-encoded .mobileprovision bytes
	ExpiresAt      time.Time
}

// CreateProfile calls POST /v1/profiles to mint a provisioning profile that
// ties bundleID + certificateID together for a given profile type
// ("IOS_APP_STORE" for distribution, "IOS_APP_DEVELOPMENT" for dev).
//
// API ref: https://developer.apple.com/documentation/appstoreconnectapi/create_a_profile
func CreateProfile(ctx context.Context, in AppleConnectionInput, name, profileType, bundleIDRowID, certificateRowID string, deviceRowIDs []string) (*CreatedProfile, error) {
	relationships := map[string]any{
		"bundleId": map[string]any{
			"data": map[string]any{"type": "bundleIds", "id": bundleIDRowID},
		},
		"certificates": map[string]any{
			"data": []map[string]any{{"type": "certificates", "id": certificateRowID}},
		},
	}
	if profileType == "IOS_APP_DEVELOPMENT" || profileType == "IOS_APP_ADHOC" {
		// Only ad-hoc / dev profiles need devices. Distribution profiles for
		// the App Store don't reference devices.
		devs := make([]map[string]any, 0, len(deviceRowIDs))
		for _, d := range deviceRowIDs {
			devs = append(devs, map[string]any{"type": "devices", "id": d})
		}
		relationships["devices"] = map[string]any{"data": devs}
	}
	payload := map[string]any{
		"data": map[string]any{
			"type": "profiles",
			"attributes": map[string]any{
				"name":        name,
				"profileType": profileType,
			},
			"relationships": relationships,
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal profile payload: %w", err)
	}
	respBody, err := ascPostJSON(ctx, in, "/v1/profiles", body)
	if err != nil {
		return nil, fmt.Errorf("create profile: %w", err)
	}
	var resp struct {
		Data struct {
			ID         string `json:"id"`
			Attributes struct {
				Name           string `json:"name"`
				UUID           string `json:"uuid"`
				ProfileType    string `json:"profileType"`
				ProfileContent string `json:"profileContent"`
				ExpirationDate string `json:"expirationDate"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("parse profile response: %w", err)
	}
	out := &CreatedProfile{
		ID:             resp.Data.ID,
		UUID:           resp.Data.Attributes.UUID,
		Name:           resp.Data.Attributes.Name,
		ProfileType:    resp.Data.Attributes.ProfileType,
		ProfileContent: resp.Data.Attributes.ProfileContent,
	}
	if resp.Data.Attributes.ExpirationDate != "" {
		if t, err := time.Parse(time.RFC3339, resp.Data.Attributes.ExpirationDate); err == nil {
			out.ExpiresAt = t
		}
	}
	return out, nil
}

// SetASCAPIBaseForTest overrides the ASC API base URL. Test-only; never call
// from production code.
func SetASCAPIBaseForTest(base string) (restore func()) {
	prev := ascAPIBase
	ascAPIBase = base
	return func() { ascAPIBase = prev }
}
