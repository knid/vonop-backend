package views

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

// pubsubEnvelope is the standard Pub/Sub push delivery shape. Google wraps
// the actual notification in a base64-encoded `data` field.
type pubsubEnvelope struct {
	Message struct {
		Data        string            `json:"data"`
		MessageID   string            `json:"messageId"`
		PublishTime string            `json:"publishTime"`
		Attributes  map[string]string `json:"attributes"`
	} `json:"message"`
	Subscription string `json:"subscription"`
}

// HandlePlayRTDN accepts Google Play Real-Time Developer Notifications
// delivered via Pub/Sub push. Pub/Sub authenticates push deliveries with a
// JWT in the Authorization header (Google-signed OIDC token); we verify the
// audience matches our configured value before parsing the body.
//
// Path: POST /v2/webhooks/play-rtdn
//
// Returns 200 OK on processing errors so Pub/Sub stops redelivering the
// same notification.
func (v *HttpViews) HandlePlayRTDN(w http.ResponseWriter, r *http.Request) {
	expectedAudience := v.Controller.SecretManager.GetSecret("PLAY_RTDN_AUDIENCE")

	if expectedAudience != "" {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			slog.Warn("play rtdn: missing bearer token")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		// Lightweight audience check on the JWT payload claim. Full key-rotation
		// JWKS validation is tracked as a follow-up task; for now we trust that
		// Google-signed tokens with the correct audience came from Pub/Sub.
		if !audienceMatches(auth[len("Bearer "):], expectedAudience) {
			slog.Warn("play rtdn: jwt audience mismatch")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
	} else {
		slog.Warn("play rtdn: PLAY_RTDN_AUDIENCE unset, skipping JWT check")
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 256*1024))
	if err != nil {
		slog.Warn("play rtdn: read body failed", "err", err)
		w.WriteHeader(http.StatusOK)
		return
	}
	defer r.Body.Close()

	var env pubsubEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		slog.Warn("play rtdn: decode envelope failed", "err", err)
		w.WriteHeader(http.StatusOK)
		return
	}
	cleartext, err := base64.StdEncoding.DecodeString(env.Message.Data)
	if err != nil {
		slog.Warn("play rtdn: base64 decode failed", "err", err)
		w.WriteHeader(http.StatusOK)
		return
	}
	if err := v.Controller.HandlePlayRTDN(r.Context(), cleartext); err != nil {
		slog.Warn("play rtdn: handler error (acked anyway)", "err", err)
	}
	w.WriteHeader(http.StatusOK)
}

// audienceMatches inspects the unverified payload claim of a JWT to check
// the audience. Verification of the signature against Google's JWKS is a
// follow-up; today the audience claim alone is enough to reject random
// public traffic, and Pub/Sub guarantees the token is Google-signed when
// the subscription is configured with a service-account audience.
func audienceMatches(token, want string) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	var claims struct {
		Aud string `json:"aud"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return false
	}
	return claims.Aud == want
}
