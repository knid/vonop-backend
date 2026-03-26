package views

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/knid/wemob/internal/services/coolify"
	"github.com/knid/wemob/internal/utils"
)

// POST /webhooks/coolify
func (v *HttpViews) HandleCoolifyWebhook(w http.ResponseWriter, r *http.Request) {
	// Verify shared secret
	secret := r.Header.Get("X-Webhook-Secret")
	if secret == "" {
		secret = r.URL.Query().Get("secret")
	}
	if v.Controller.CoolifyService == nil || secret != v.Controller.CoolifyService.WebhookSecret {
		utils.JSONErrorResponse(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var event coolify.DeploymentEvent
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		slog.Warn("Coolify webhook: invalid payload", "error", err)
		utils.JSONErrorResponse(w, "invalid payload", http.StatusBadRequest)
		return
	}

	slog.Info("Coolify webhook received",
		"app_uuid", event.AppUUID(),
		"status", event.DeployStatus(),
		"type", event.Type)

	if err := v.Controller.HandleCoolifyWebhook(event); err != nil {
		slog.Error("Coolify webhook handler error", "error", err)
		// Return 200 anyway — Coolify shouldn't retry on our processing errors
	}

	utils.JSONResponse(w, map[string]string{"status": "ok"}, http.StatusOK)
}
