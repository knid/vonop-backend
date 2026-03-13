package views

import (
	"net/http"

	"github.com/knid/wemob/internal/static"
)

// GET /sdk/wemob-bridge.js
func (v *HttpViews) ServeBridgeSDK(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(static.WemobBridgeJS))
}
