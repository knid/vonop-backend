package storeauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const playAPIBase = "https://androidpublisher.googleapis.com/androidpublisher/v3"

// playHTTPClient is the package-level HTTP client used for Play API calls.
// Has a 15-second timeout (avoids http.DefaultClient per T5 audit).
var playHTTPClient = &http.Client{Timeout: 15 * time.Second}

// CheckPlayAppSigningEnrolment returns (true, nil) if the service account
// can see the app in Play Console (a strong signal that signing is set up
// and the SA has Release Manager access). Returns (false, nil) on 404 (app
// not registered) or 403 (SA lacks access). Returns (false, err) on any
// other error.
//
// Used by the first-publish gate (P0.3 T10) to block Android deploys if
// Play App Signing isn't enrolled.
func CheckPlayAppSigningEnrolment(ctx context.Context, sa GoogleServiceAccount, packageName string) (bool, error) {
	token, err := GoogleToken(ctx, sa)
	if err != nil {
		return false, fmt.Errorf("mint google token: %w", err)
	}
	u := fmt.Sprintf("%s/applications/%s", playAPIBase, url.PathEscape(packageName))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := playHTTPClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("play http: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 32*1024))
	switch resp.StatusCode {
	case http.StatusOK:
		return parsePlayApplicationResponse(body)
	case http.StatusNotFound:
		// App not registered in Play Console yet (or wrong package name)
		return false, nil
	case http.StatusForbidden:
		// SA exists but lacks Release Manager / Admin access on this app
		return false, nil
	default:
		return false, fmt.Errorf("play /applications returned %d: %s", resp.StatusCode, string(body))
	}
}

func parsePlayApplicationResponse(body []byte) (bool, error) {
	// We don't currently inspect the body — HTTP 200 is the signal.
	// But we do verify the response is well-formed JSON to catch upstream issues.
	var probe map[string]any
	if err := json.Unmarshal(body, &probe); err != nil {
		return false, fmt.Errorf("parse play app: %w", err)
	}
	return true, nil
}
