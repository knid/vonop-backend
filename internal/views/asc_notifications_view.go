package views

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
)

// HandleASCNotification accepts an App Store Connect webhook (preview as
// of 2024-2025). Apple's notification system signs payloads with HMAC-SHA256
// using a shared secret configured per-account. We verify by recomputing the
// signature against the body using ASC_NOTIFICATIONS_HMAC_SECRET.
//
// When the secret is unset (typical in dev / staging) we accept without
// verification but log a warning — payload-level validation still happens
// inside the controller.
//
// Path: POST /v2/webhooks/asc-notifications
//
// Always returns 200 OK on processing errors so Apple doesn't retry the same
// notification forever; processing failures are logged at warn level.
func (v *HttpViews) HandleASCNotification(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 256*1024))
	if err != nil {
		slog.Warn("asc notifications: read body failed", "err", err)
		w.WriteHeader(http.StatusOK)
		return
	}
	defer r.Body.Close()

	secret := v.Controller.SecretManager.GetSecret("ASC_NOTIFICATIONS_HMAC_SECRET")
	if secret != "" {
		got := r.Header.Get("X-Apple-Signature")
		if got == "" {
			got = r.Header.Get("X-Hub-Signature-256")
		}
		if !verifyHMACSHA256(body, secret, got) {
			slog.Warn("asc notifications: invalid signature")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
	} else {
		slog.Warn("asc notifications: ASC_NOTIFICATIONS_HMAC_SECRET unset, accepting without signature verification")
	}

	if err := v.Controller.HandleASCNotification(r.Context(), body); err != nil {
		slog.Warn("asc notifications: handler error (acked anyway)", "err", err)
	}
	w.WriteHeader(http.StatusOK)
}

// verifyHMACSHA256 returns true if got matches HMAC-SHA256(body, secret) in
// hex. Accepts a "sha256=" prefix for compatibility with hub-style headers.
func verifyHMACSHA256(body []byte, secret, got string) bool {
	if got == "" || secret == "" {
		return false
	}
	if len(got) > 7 && got[:7] == "sha256=" {
		got = got[7:]
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(want), []byte(got))
}
