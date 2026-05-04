package views

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/utils"
)

const expoReportsTimestampSkew = 5 * time.Minute

type expoBuildReport struct {
	TaskID  string `json:"task_id"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	// Phase distinguishes build status events from submit status events.
	// "build" (default — used when absent) | "submit". Stored verbatim in
	// the event payload jsonb so downstream queries can filter via
	// `payload @> '{"phase":"submit"}'`. P1.2 publishing-blocks (2026-05-03).
	Phase     string `json:"phase,omitempty"`
	Timestamp int64  `json:"timestamp"`
}

// ExpoReportsHandler holds the small dependency set the webhook handler needs.
// Decoupled from the larger HttpViews so it's straightforward to test.
type ExpoReportsHandler struct {
	Store      database.BuildEventStore
	HMACMaster string
}

// NewExpoReportsHandler constructs an ExpoReportsHandler.
// hmacMaster is the WEMOB_WORKER_HMAC_KEY secret (loaded from SecretManager
// in cmd/main.go) used to derive per-build signing keys.
func NewExpoReportsHandler(store database.BuildEventStore, hmacMaster string) *ExpoReportsHandler {
	return &ExpoReportsHandler{Store: store, HMACMaster: hmacMaster}
}

// UpdateExpoBuildStatus is the HTTP handler for /v2/reports/expo-build.
//
// Auth: HMAC-SHA256 over "<ts>.<body>" using a purpose-bound key derived as
//
//	buildKey = HKDF-SHA256(HMACMaster, salt="wemob-worker-hmac-v1",
//	                       info="build-webhook:<task_id>")
//
// The request must have:
//
//	X-Wemob-Signature: sha256=<hex>
//	X-Wemob-Timestamp: <unix-seconds>
//
// Stale timestamps (>5 min old) are rejected (replay protection).
//
// Idempotency: events deduped by SHA-256 of the raw body. A retry of the
// exact same body produces no second row.
//
// P0 #8b (mobile audit, 2026-05-04): migrated from
// HMAC(master, task_id) to HKDF with a "build-webhook" purpose so a leaked
// build secret can't be used to sign keystore or OTA webhooks.
func (h *ExpoReportsHandler) UpdateExpoBuildStatus(w http.ResponseWriter, r *http.Request) {
	// Bound the body to prevent memory exhaustion from oversized payloads.
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		utils.JSONError(w, "bad_request", "read body", http.StatusBadRequest, nil)
		return
	}

	sigHeader := strings.TrimSpace(r.Header.Get("X-Wemob-Signature"))
	tsHeader := strings.TrimSpace(r.Header.Get("X-Wemob-Timestamp"))
	if sigHeader == "" || tsHeader == "" {
		utils.JSONError(w, "unauthorized", "missing signature or timestamp", http.StatusUnauthorized, nil)
		return
	}

	ts, err := strconv.ParseInt(tsHeader, 10, 64)
	tsTime := time.Unix(ts, 0)
	// Reject both stale (too old) and future-dated timestamps to prevent replays and
	// forward-timestamp attacks. Allow ±5 min clock skew in either direction.
	if err != nil || time.Since(tsTime) > expoReportsTimestampSkew || time.Until(tsTime) > expoReportsTimestampSkew {
		utils.JSONError(w, "unauthorized", "stale timestamp", http.StatusUnauthorized, nil)
		return
	}

	var report expoBuildReport
	if err := json.Unmarshal(body, &report); err != nil {
		utils.JSONError(w, "bad_request", "invalid json", http.StatusBadRequest, nil)
		return
	}

	// Derive per-build, per-purpose key via HKDF — a leak of this key
	// only compromises the build webhook for this task_id (not other
	// builds, not other webhook types). Hex-encode the 32-byte derived
	// key so the shell-side derivation in codemagic.yaml matches.
	derived := utils.DeriveHMACKey([]byte(h.HMACMaster), "build-webhook", report.TaskID)
	buildSecret := hex.EncodeToString(derived)

	// Compute expected signature over "<timestamp>.<body>" so the timestamp is
	// bound to the signature and cannot be substituted independently.
	h2 := hmac.New(sha256.New, []byte(buildSecret))
	h2.Write([]byte(tsHeader + "." + string(body)))
	expected := "sha256=" + hex.EncodeToString(h2.Sum(nil))

	// Constant-time compare
	if !hmac.Equal([]byte(sigHeader), []byte(expected)) {
		utils.JSONError(w, "unauthorized", "bad signature", http.StatusUnauthorized, nil)
		return
	}

	taskID, err := uuid.Parse(report.TaskID)
	if err != nil {
		utils.JSONError(w, "bad_request", "invalid task_id", http.StatusBadRequest, nil)
		return
	}

	sum := sha256.Sum256(body)
	event := &models.BuildEvent{
		TaskID:      taskID,
		EventType:   report.Status,
		Payload:     body,
		MessageHash: hex.EncodeToString(sum[:]),
		OccurredAt:  time.Unix(report.Timestamp, 0),
	}

	if err := h.Store.AppendBuildEvent(r.Context(), event); err != nil {
		utils.JSONError(w, "internal", "record event", http.StatusInternalServerError, nil)
		return
	}

	utils.JSONResponse(w, map[string]string{"status": "ok"}, http.StatusOK)
}
