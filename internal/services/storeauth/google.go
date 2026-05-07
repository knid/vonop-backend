package storeauth

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const playAPIBase = "https://androidpublisher.googleapis.com/androidpublisher/v3"

// playHTTPClient is the package-level HTTP client used for Play API calls.
// 15-second timeout (avoids http.DefaultClient per T5 audit).
var playHTTPClient = &http.Client{Timeout: 15 * time.Second}

// GoogleServiceAccount is the subset of fields we read from a Google
// service-account JSON key.
type GoogleServiceAccount struct {
	Type         string `json:"type"`
	ProjectID    string `json:"project_id"`
	PrivateKeyID string `json:"private_key_id"`
	PrivateKey   string `json:"private_key"`
	ClientEmail  string `json:"client_email"`
	TokenURI     string `json:"token_uri"`
}

// ParseServiceAccount unmarshals a service-account JSON blob.
func ParseServiceAccount(saJSON []byte) (GoogleServiceAccount, error) {
	var sa GoogleServiceAccount
	if err := json.Unmarshal(saJSON, &sa); err != nil {
		return sa, fmt.Errorf("parse service account json: %w", err)
	}
	if sa.Type != "service_account" || sa.ClientEmail == "" || sa.PrivateKey == "" {
		return sa, fmt.Errorf("service account missing required fields")
	}
	if sa.TokenURI == "" {
		sa.TokenURI = "https://oauth2.googleapis.com/token"
	}
	return sa, nil
}

// PlayTrackRelease is the subset of Play `Release` we use for status polling
// (P1#16). One Release per (track, version-code) tuple.
type PlayTrackRelease struct {
	Name            string  // optional human-readable
	VersionCodes    []int64 // version codes contained in this release
	Status          string  // "draft" | "inProgress" | "halted" | "completed"
	UserFraction    float64 // 0..1, only meaningful for inProgress
	ReleaseNotes    string  // empty when not present
	StoreReleaseRef string  // synthesized as "{track}#{joinedVersionCodes}"
}

// PlayEdit is the minimum we read back from the edits.insert call.
type PlayEdit struct {
	ID            string `json:"id"`
	ExpiryTimeSec string `json:"expiryTimeSeconds"`
}

// GetPlayTrackRelease opens a fresh edit, reads the named track, and returns
// the most recent release on it. Used by the release status polling worker
// (P1#16). Caller supplies a context — the edit is always deleted before
// returning to avoid mutating user state.
func GetPlayTrackRelease(ctx context.Context, sa GoogleServiceAccount, packageName, track string) (*PlayTrackRelease, error) {
	if packageName == "" || track == "" {
		return nil, fmt.Errorf("packageName and track required")
	}
	token, err := GoogleToken(ctx, sa)
	if err != nil {
		return nil, fmt.Errorf("mint google token: %w", err)
	}
	pkg := url.PathEscape(packageName)
	editURL := fmt.Sprintf("%s/applications/%s/edits", playAPIBase, pkg)

	editReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, editURL, nil)
	editReq.Header.Set("Authorization", "Bearer "+token)
	editReq.Header.Set("Content-Type", "application/json")
	editResp, err := playHTTPClient.Do(editReq)
	if err != nil {
		return nil, fmt.Errorf("open edit: %w", err)
	}
	defer editResp.Body.Close()
	editBody, _ := io.ReadAll(io.LimitReader(editResp.Body, 4096))
	if editResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("open edit status %d: %s", editResp.StatusCode, string(editBody))
	}
	var edit PlayEdit
	if err := json.Unmarshal(editBody, &edit); err != nil || edit.ID == "" {
		return nil, fmt.Errorf("parse edit response: %w", err)
	}

	// Best-effort cleanup; failure to delete the edit is non-fatal.
	defer func() {
		delURL := editURL + "/" + edit.ID
		delReq, _ := http.NewRequestWithContext(context.Background(), http.MethodDelete, delURL, nil)
		delReq.Header.Set("Authorization", "Bearer "+token)
		if delResp, err := playHTTPClient.Do(delReq); err == nil {
			_, _ = io.ReadAll(delResp.Body)
			delResp.Body.Close()
		}
	}()

	trackURL := fmt.Sprintf("%s/%s/tracks/%s", editURL, edit.ID, url.PathEscape(track))
	trackReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, trackURL, nil)
	trackReq.Header.Set("Authorization", "Bearer "+token)
	trackResp, err := playHTTPClient.Do(trackReq)
	if err != nil {
		return nil, fmt.Errorf("read track: %w", err)
	}
	defer trackResp.Body.Close()
	trackBody, _ := io.ReadAll(io.LimitReader(trackResp.Body, 64*1024))
	if trackResp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if trackResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("read track status %d: %s", trackResp.StatusCode, string(trackBody))
	}
	var t struct {
		Track    string `json:"track"`
		Releases []struct {
			Name         string   `json:"name"`
			VersionCodes []string `json:"versionCodes"`
			Status       string   `json:"status"`
			UserFraction float64  `json:"userFraction"`
			ReleaseNotes []struct {
				Language string `json:"language"`
				Text     string `json:"text"`
			} `json:"releaseNotes"`
		} `json:"releases"`
	}
	if err := json.Unmarshal(trackBody, &t); err != nil {
		return nil, fmt.Errorf("parse track: %w", err)
	}
	if len(t.Releases) == 0 {
		return nil, nil
	}
	rel := t.Releases[0]
	out := &PlayTrackRelease{
		Name:         rel.Name,
		Status:       rel.Status,
		UserFraction: rel.UserFraction,
	}
	for _, vc := range rel.VersionCodes {
		// Play returns version codes as decimal strings.
		var n int64
		_, _ = fmt.Sscanf(vc, "%d", &n)
		out.VersionCodes = append(out.VersionCodes, n)
	}
	if len(rel.ReleaseNotes) > 0 {
		out.ReleaseNotes = rel.ReleaseNotes[0].Text
	}
	out.StoreReleaseRef = fmt.Sprintf("%s#%v", track, out.VersionCodes)
	return out, nil
}

// GetPlayLiveVersionCode returns the highest version code currently on the
// given track, or 0 if the track has no releases.
func GetPlayLiveVersionCode(ctx context.Context, sa GoogleServiceAccount, packageName, track string) (int64, error) {
	rel, err := GetPlayTrackRelease(ctx, sa, packageName, track)
	if err != nil {
		return 0, err
	}
	if rel == nil || len(rel.VersionCodes) == 0 {
		return 0, nil
	}
	var max int64
	for _, vc := range rel.VersionCodes {
		if vc > max {
			max = vc
		}
	}
	return max, nil
}

// GoogleToken mints a short-lived OAuth2 access token scoped to the
// androidpublisher API via the JWT-bearer grant.
func GoogleToken(ctx context.Context, sa GoogleServiceAccount) (string, error) {
	block, _ := pem.Decode([]byte(sa.PrivateKey))
	if block == nil {
		return "", fmt.Errorf("no PEM block in service account private key")
	}
	rsaKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("parse service account private key: %w", err)
	}
	now := time.Now()
	grant := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss":   sa.ClientEmail,
		"sub":   sa.ClientEmail,
		"scope": "https://www.googleapis.com/auth/androidpublisher",
		"aud":   sa.TokenURI,
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	})
	signed, err := grant.SignedString(rsaKey)
	if err != nil {
		return "", fmt.Errorf("sign grant jwt: %w", err)
	}
	vals := url.Values{}
	vals.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
	vals.Set("assertion", signed)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sa.TokenURI, strings.NewReader(vals.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("oauth2 token exchange: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("google oauth2 status %d: %s", resp.StatusCode, string(body))
	}
	var parsed struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil || parsed.AccessToken == "" {
		return "", fmt.Errorf("parse oauth2 response: %w", err)
	}
	return parsed.AccessToken, nil
}
