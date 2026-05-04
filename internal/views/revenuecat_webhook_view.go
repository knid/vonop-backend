package views

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/knid/wemob/internal/controllers"
)

// HandleRevenueCatWebhook accepts a RevenueCat v2 webhook event and forwards
// it to the controller's HandleRCEvent dispatcher.
//
// Auth: shared bearer token. RC sends `Authorization: Bearer <secret>` where
// <secret> is the value configured in the RC dashboard's webhook integration.
// Backend reads the expected secret from REVENUECAT_WEBHOOK_AUTH_TOKEN via
// the SecretManager and constant-time compares.
//
// Path: POST /v2/webhooks/revenuecat
//
// Response policy:
//   - 401 when the bearer token is missing/wrong (RC will retry per its
//     standard backoff — fine, since a 401 also surfaces in the RC UI).
//   - 200 on every other outcome (including unknown event types, missing
//     project, malformed body) so RC's retry loop doesn't hammer us for
//     events we'll never be able to process.
//   - 500 only when the database write itself fails AND we want RC to
//     retry (= actual infra failure).
func (v *HttpViews) HandleRevenueCatWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 256*1024))
	if err != nil {
		slog.Warn("revenuecat webhook: read body failed", "err", err)
		w.WriteHeader(http.StatusOK)
		return
	}
	defer r.Body.Close()

	if authErr := v.Controller.VerifyRCWebhookAuth(r.Header.Get("Authorization")); authErr != nil {
		slog.Warn("revenuecat webhook: auth failed", "err", authErr)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	var evt controllers.RCEventV2
	if err := json.Unmarshal(body, &evt); err != nil {
		slog.Warn("revenuecat webhook: unmarshal failed (acked)", "err", err)
		w.WriteHeader(http.StatusOK)
		return
	}
	if evt.Event.Type == "" {
		slog.Warn("revenuecat webhook: missing event.type (acked)")
		w.WriteHeader(http.StatusOK)
		return
	}

	if err := v.Controller.HandleRCEvent(r.Context(), evt); err != nil {
		// Distinguish infra failures from "we don't know this project".
		// HandleRCEvent already returns nil for the latter, so reaching
		// here means a write or lookup actually failed — let RC retry.
		slog.Error("revenuecat webhook: handler error",
			"err", err, "type", evt.Event.Type, "app_id", evt.Event.AppID)
		// Surface as 500 unless the caller passed a sentinel we want to ack.
		if errors.Is(err, controllers.ErrRCWebhookUnauthenticated) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}
