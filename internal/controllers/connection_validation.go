package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/services/storeauth"
)

// ValidateAppleConnection downloads the .p8 key from S3, signs a short-lived
// App Store Connect JWT, and GETs /v1/apps to verify the credentials are valid.
// If req.ConnectionID is non-nil the outcome is persisted to connection_validations.
func (c *Controller) ValidateAppleConnection(ctx context.Context, req requests.ValidateAppleConnectionRequest) responses.ValidationResult {
	result := c.validateAppleConnectionCore(ctx, req)
	if req.ConnectionID != nil {
		status := "invalid"
		if result.Valid {
			status = "valid"
		}
		persistConnectionValidation(ctx, c.DB, "apple", *req.ConnectionID, status, result.Message, len(result.Apps))
	}
	return result
}

func (c *Controller) validateAppleConnectionCore(ctx context.Context, req requests.ValidateAppleConnectionRequest) responses.ValidationResult {
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

	// --- sign a 10-minute App Store Connect JWT via storeauth helper ---
	signed, err := storeauth.AppleToken(storeauth.AppleConnectionInput{
		KeyID:    req.KeyID,
		IssuerID: req.IssuerID,
		P8PEM:    p8Bytes,
	})
	if err != nil {
		// Preserve original user-visible messages: parse errors vs. sign errors.
		msg := err.Error()
		if strings.Contains(msg, "PEM block") || strings.Contains(msg, "PKCS8") || strings.Contains(msg, "EC private key") {
			return responses.ValidationResult{
				Valid:   false,
				Message: "The .p8 file could not be parsed as an EC private key. Make sure you uploaded the correct file from App Store Connect.",
			}
		}
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
// If req.ConnectionID is non-nil the outcome is persisted to connection_validations.
//
// NOTE (Phase 3a): Full service-account OAuth2 token exchange is implemented below
// using the standard JWT-signed grant flow (no external SDK required).
func (c *Controller) ValidateGooglePlayConnection(ctx context.Context, req requests.ValidateGooglePlayConnectionRequest) responses.ValidationResult {
	result := c.validateGooglePlayConnectionCore(ctx, req)
	if req.ConnectionID != nil {
		status := "invalid"
		if result.Valid {
			status = "valid"
		}
		persistConnectionValidation(ctx, c.DB, "google_play", *req.ConnectionID, status, result.Message, 0)
	}
	return result
}

func (c *Controller) validateGooglePlayConnectionCore(ctx context.Context, req requests.ValidateGooglePlayConnectionRequest) responses.ValidationResult {
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

	// --- parse the service account JSON via storeauth helper ---
	sa, err := storeauth.ParseServiceAccount(saBytes)
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "missing required fields") {
			return responses.ValidationResult{
				Valid:   false,
				Message: "The uploaded file does not look like a Google service account key. Please download a Service Account JSON key from Google Cloud Console.",
			}
		}
		return responses.ValidationResult{
			Valid:   false,
			Message: "The service account file could not be parsed as valid JSON. Make sure you downloaded the correct file from Google Cloud Console.",
		}
	}

	// --- exchange for access token via storeauth helper ---
	accessToken, err := storeauth.GoogleToken(ctx, sa)
	if err != nil {
		msg := err.Error()
		switch {
		case strings.Contains(msg, "PEM block"):
			return responses.ValidationResult{
				Valid:   false,
				Message: "Could not decode the private key PEM block from the service account file.",
			}
		case strings.Contains(msg, "parse service account private key"):
			return responses.ValidationResult{
				Valid:   false,
				Message: "Could not parse the service account private key. The key may be malformed.",
			}
		case strings.Contains(msg, "sign grant jwt"):
			return responses.ValidationResult{
				Valid:   false,
				Message: "Failed to sign the Google OAuth2 grant JWT.",
			}
		case strings.Contains(msg, "oauth2 token exchange"):
			return responses.ValidationResult{
				Valid:   false,
				Message: "Could not reach the Google OAuth2 token endpoint. Check your network connectivity.",
			}
		case strings.Contains(msg, "google oauth2 status"):
			// Try to surface the Google error_description if present in err body.
			friendly := "Google rejected the service account credentials."
			if idx := strings.Index(msg, ": "); idx > 0 {
				body := msg[idx+2:]
				var errResp struct {
					Error            string `json:"error"`
					ErrorDescription string `json:"error_description"`
				}
				_ = json.Unmarshal([]byte(body), &errResp)
				if errResp.ErrorDescription != "" {
					friendly += " Details: " + errResp.ErrorDescription
				}
			}
			return responses.ValidationResult{Valid: false, Message: friendly}
		case strings.Contains(msg, "parse oauth2 response"):
			return responses.ValidationResult{
				Valid:   false,
				Message: "Google returned a token response but it could not be parsed.",
			}
		default:
			return responses.ValidationResult{
				Valid:   false,
				Message: "Google rejected the service account credentials.",
			}
		}
	}

	// --- call the Android Publisher API ---
	// We use the "inappproducts" endpoint on a dummy package to trigger the auth
	// check without needing a specific package name. A 404 (app not found) still
	// confirms the credentials are valid; a 401/403 means bad key.
	pubReq, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://androidpublisher.googleapis.com/androidpublisher/v3/applications/com.android.example/inappproducts",
		nil)
	pubReq.Header.Set("Authorization", "Bearer "+accessToken)

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

// connectionValidationAppender is the minimal write surface needed by
// persistConnectionValidation. *database.Database satisfies it via
// AppendConnectionValidation (added in P0.3 T4).
type connectionValidationAppender interface {
	AppendConnectionValidation(ctx context.Context, v *models.ConnectionValidation) error
}

// persistConnectionValidation writes a validation outcome row. Best-effort:
// errors are logged at warn level but never returned, so a failed persist
// doesn't break the validation flow itself.
func persistConnectionValidation(ctx context.Context, store connectionValidationAppender, kind string, connID uuid.UUID, status, message string, appCount int) {
	if store == nil {
		return
	}
	err := store.AppendConnectionValidation(ctx, &models.ConnectionValidation{
		ConnectionKind: kind,
		ConnectionID:   connID,
		Status:         status,
		Message:        message,
		AppCount:       appCount,
		ValidatedAt:    time.Now().UTC(),
	})
	if err != nil {
		slog.Warn("persist connection validation failed", "err", err, "kind", kind, "conn", connID)
	}
}
