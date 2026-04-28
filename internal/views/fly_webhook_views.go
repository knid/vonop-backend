package views

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/knid/wemob/internal/services/fly"
)

// HandleFlyWebhook is invoked by Fly on machine state changes. We only care
// about "started" (deploy succeeded) and "destroyed"/"failed" (deploy failed).
//
// Path: POST /webhooks/fly
func (v *HttpViews) HandleFlyWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusOK) // ack to prevent retries
		return
	}
	defer r.Body.Close()

	sig := r.Header.Get("Fly-Webhook-Signature")
	if !v.Controller.FlyService.VerifyWebhookSignature(body, sig) {
		slog.Warn("Fly webhook signature invalid")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	var event fly.MachineEvent
	if err := json.Unmarshal(body, &event); err != nil {
		slog.Warn("Fly webhook payload invalid", "err", err)
		w.WriteHeader(http.StatusOK)
		return
	}

	if err := v.Controller.HandleFlyMachineEvent(event); err != nil {
		slog.Error("Fly webhook handler error", "err", err, "app", event.AppName)
	}
	// Always 200 — Fly retries non-2xx, we don't want retries on processing errors.
	w.WriteHeader(http.StatusOK)
}
