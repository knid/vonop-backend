package external

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/knid/wemob/internal/models"
)

var (
	onesignalHTTPClient = &http.Client{Timeout: 30 * time.Second}
	onesignalAPIBaseURL = "https://api.onesignal.com"
)

// SetOneSignalTestEndpoint redirects all OneSignal calls (CreateApplication,
// CreateAppToken, UploadAPNSP8Key, SendOneSignalNotification, SendNotificationViaSDK)
// to a test server. Returns a restore function. Test-only; do not call from
// production code.
func SetOneSignalTestEndpoint(u string) (restore func()) {
	prev := onesignalAPIBaseURL
	onesignalAPIBaseURL = u
	return func() { onesignalAPIBaseURL = prev }
}

func CreateOneSignalApplication(project models.Project, orgId, orgApiKey, fcmB64 string) (string, error) {
	url := onesignalAPIBaseURL + "/apps"

	payloadStr := map[string]string{
		"name":                        project.PackageName,
		"organization_id":             orgId,
		"fcm_v1_service_account_json": fcmB64,
	}
	marshalled, _ := json.Marshal(payloadStr)

	req, _ := http.NewRequest("POST", url, bytes.NewReader(marshalled))

	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", "Basic "+orgApiKey)

	res, err := onesignalHTTPClient.Do(req)
	if err != nil {
		return "", err
	}

	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return "", err
	}

	var response map[string]interface{}
	json.Unmarshal(body, &response)

	onesignal_app_id, ok := response["id"]
	if !ok {
		slog.Error("Error creating OneSignal Application", "response", response, "project_id", project.ID)
		return "", errors.New("error creating application")
	}

	return onesignal_app_id.(string), nil
}

func CreateOneSignalAppToken(project models.Project, orgApiKey string) (string, error) {
	url := fmt.Sprintf("%s/apps/%s/auth/tokens", onesignalAPIBaseURL, project.OneSignalAppID)

	payloadStr := map[string]string{
		"name": "wemob-api",
	}

	marshalled, _ := json.Marshal(payloadStr)

	req, _ := http.NewRequest("POST", url, bytes.NewReader(marshalled))

	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", "Basic "+orgApiKey)

	res, err := onesignalHTTPClient.Do(req)
	if err != nil {
		return "", err
	}

	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)

	var response map[string]interface{}
	json.Unmarshal(body, &response)

	onesignal_app_api_key, ok := response["formatted_token"]
	if !ok {
		slog.Error("Error creating OneSignal App Token", "response", response, "project_id", project.ID)
		return "", errors.New("error creating app token")
	}

	return onesignal_app_api_key.(string), nil
}

func UploadAPNSP8KeyToOneSignal(project models.Project, apnsP8Key string, apnsKeyID string, apnsTeamID string, apnsBundleID string, orgId, orgApiKey string) error {
	url := fmt.Sprintf("%s/apps/%s", onesignalAPIBaseURL, project.OneSignalAppID)
	payloadStr := map[string]string{
		"organization_id": orgId,
		"apns_p8":         apnsP8Key,
		"apns_key_id":     apnsKeyID,
		"apns_team_id":    apnsTeamID,
		"apns_bundle_id":  apnsBundleID,
	}
	marshalled, _ := json.Marshal(payloadStr)

	req, _ := http.NewRequest("PUT", url, bytes.NewReader(marshalled))

	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", "Basic "+orgApiKey)

	res, err := onesignalHTTPClient.Do(req)
	if err != nil {
		return err
	}

	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}

	var response map[string]interface{}
	json.Unmarshal(body, &response)

	if res.StatusCode != 200 {
		slog.Error("Error uploading APNS P8 Key to OneSignal", "response", response, "project_id", project.ID)
		return errors.New("error uploading APNS P8 Key to OneSignal")
	}

	return nil
}

func SendOneSignalNotification(notification *models.Notification, project *models.Project) error {
	url := onesignalAPIBaseURL + "/notifications?c=push"

	payloadStr := map[string]any{
		"app_id": project.OneSignalAppID,
		"contents": map[string]string{
			"en": notification.Description,
		},
		"headings": map[string]string{
			"en": notification.Title,
		},
		"data": map[string]string{
			"notification_id": notification.ID.String(),
			"url":             notification.URL,
		},
		"included_segments": []string{
			"Total Subscriptions",
		},
	}
	if notification.Image != "" {
		payloadStr["ios_attachments"] = map[string]string{
			"id": notification.Image,
		}
		payloadStr["big_picture"] = notification.Image
	}

	if notification.SendAfter != "" {
		payloadStr["send_after"] = notification.SendAfter
	}

	marshalled, _ := json.Marshal(payloadStr)

	req, _ := http.NewRequest("POST", url, bytes.NewReader(marshalled))

	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", "Basic "+project.OneSignalAppToken)

	res, err := onesignalHTTPClient.Do(req)
	if err != nil {
		return err
	}

	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)

	var response map[string]interface{}
	json.Unmarshal(body, &response)

	return nil
}

// SendNotificationParams is the structured payload for SendNotificationViaSDK.
// Use Headings + Contents maps keyed by IETF language tag (e.g. "en", "tr") so
// the user app can show the right locale automatically when OneSignal picks
// based on the device language.
type SendNotificationParams struct {
	Headings map[string]string // {"en": "Title", "tr": "Başlık"}
	Contents map[string]string // {"en": "Body", "tr": "Gövde"}

	// Audience — exactly one of IncludedSegments / IncludeAliases must be non-empty.
	// IncludedSegments is the OneSignal segment-name list (e.g. ["Subscribed Users"]).
	// IncludeAliases.external_id targets specific Better-Auth user IDs.
	IncludedSegments []string
	IncludeAliases   map[string][]string // {"external_id": ["user-uuid-1", "user-uuid-2"]}

	// Optional metadata for client-side handling (deep-link, custom data).
	Data map[string]string

	// <P2#25> rich notification fields.
	// SendAfter is the ISO-8601 / RFC 3339 string OneSignal accepts for
	// scheduled delivery (e.g. "2026-05-04T18:30:00Z"). Empty = send now.
	SendAfter string
	// BigPicture is an absolute https URL for the Android big-picture style.
	BigPicture string
	// IOSAttachments is the iOS rich-media map (id → URL); only one key
	// matters in practice. Pass {"id": "https://..."} to attach a media
	// payload to the alert.
	IOSAttachments map[string]string
	// WebURL / AppURL → OneSignal's `web_url` / `app_url` for click-through
	// behaviour. Caller is expected to pre-validate these against the
	// project's allow-list (http(s) + project URL scheme).
	WebURL string
	AppURL string
}

// SendNotificationResult is the OneSignal response we surface to callers.
// The OneSignal v1 shape returns id + recipients (int).
type SendNotificationResult struct {
	NotificationID  string `json:"id"`
	RecipientsCount int    `json:"recipients"`
}

// ErrOneSignalAuth is returned on 401 (bad/missing REST API key for the app).
var ErrOneSignalAuth = errors.New("onesignal: authentication failed (401) — check rest API key")

// ErrOneSignalRateLimit is returned after exhausting the 429 retry budget.
var ErrOneSignalRateLimit = errors.New("onesignal: rate limit exceeded after retries")

// SendNotificationViaSDK posts a structured notification to OneSignal's v1
// notifications endpoint using the per-tenant REST API key.
//
// Retry policy:
//
//	429 → 3 attempts with exponential backoff (1s, 2s, 4s)
//	5xx → 1 retry
//	401 → wrap as ErrOneSignalAuth, no retry
//	other 4xx → fail fast
//
// Total worst-case ~7s.
func SendNotificationViaSDK(ctx context.Context, appID, restAPIKey string, params SendNotificationParams) (*SendNotificationResult, error) {
	if appID == "" {
		return nil, errors.New("onesignal: appID is required")
	}
	if restAPIKey == "" {
		return nil, errors.New("onesignal: restAPIKey is required")
	}
	if len(params.Headings) == 0 || len(params.Contents) == 0 {
		return nil, errors.New("onesignal: headings and contents are required")
	}
	if len(params.IncludedSegments) == 0 && len(params.IncludeAliases) == 0 {
		return nil, errors.New("onesignal: must specify either IncludedSegments or IncludeAliases")
	}

	body := map[string]any{
		"app_id":   appID,
		"headings": params.Headings,
		"contents": params.Contents,
	}
	if len(params.IncludedSegments) > 0 {
		body["included_segments"] = params.IncludedSegments
	}
	if len(params.IncludeAliases) > 0 {
		body["include_aliases"] = params.IncludeAliases
		body["target_channel"] = "push"
	}
	if len(params.Data) > 0 {
		body["data"] = params.Data
	}
	// <P2#25> rich notification + scheduled-send fields. All optional;
	// OneSignal silently ignores empty/missing keys, so we only emit what
	// the caller actually populated.
	if params.SendAfter != "" {
		body["send_after"] = params.SendAfter
	}
	if params.BigPicture != "" {
		body["big_picture"] = params.BigPicture
	}
	if len(params.IOSAttachments) > 0 {
		body["ios_attachments"] = params.IOSAttachments
	}
	if params.WebURL != "" {
		body["web_url"] = params.WebURL
	}
	if params.AppURL != "" {
		body["app_url"] = params.AppURL
	}

	delays := []time.Duration{0, 1 * time.Second, 2 * time.Second, 4 * time.Second}
	var lastErr error

	for attempt, delay := range delays {
		if delay > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		marshalled, _ := json.Marshal(body)
		req, err := http.NewRequestWithContext(ctx, "POST", onesignalAPIBaseURL+"/notifications", bytes.NewReader(marshalled))
		if err != nil {
			return nil, fmt.Errorf("onesignal: new request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Basic "+restAPIKey)

		resp, err := onesignalHTTPClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("onesignal: http: %w", err)
			if attempt >= 1 {
				return nil, lastErr
			}
			continue
		}
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		switch {
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			var out SendNotificationResult
			if err := json.Unmarshal(respBody, &out); err != nil {
				return nil, fmt.Errorf("onesignal: unmarshal %d: %w", resp.StatusCode, err)
			}
			return &out, nil
		case resp.StatusCode == 401:
			return nil, ErrOneSignalAuth
		case resp.StatusCode == 429:
			lastErr = fmt.Errorf("%w: %s", ErrOneSignalRateLimit, string(respBody))
		case resp.StatusCode >= 500:
			lastErr = fmt.Errorf("onesignal: server %d: %s", resp.StatusCode, string(respBody))
			if attempt >= 1 {
				return nil, lastErr
			}
		default:
			return nil, fmt.Errorf("onesignal: client %d: %s", resp.StatusCode, string(respBody))
		}
	}
	return nil, lastErr
}
