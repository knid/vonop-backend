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
	TaskID    string `json:"task_id"`
	Status    string `json:"status"`
	Message   string `json:"message,omitempty"`
	Timestamp int64  `json:"timestamp"`
}

// ExpoReportsHandler holds the small dependency set the webhook handler needs.
// Decoupled from the larger HttpViews so it's straightforward to test.
type ExpoReportsHandler struct {
	Store      database.BuildEventStore
	HMACMaster string
}

// NewExpoReportsHandler constructs an ExpoReportsHandler.
// hmacMaster is the WEMOB_WORKER_HMAC_KEY secret used to derive per-build
// signing keys. TODO(P1.1): wire through SecretManager instead of os.Getenv.
func NewExpoReportsHandler(store database.BuildEventStore, hmacMaster string) *ExpoReportsHandler {
	return &ExpoReportsHandler{Store: store, HMACMaster: hmacMaster}
}

// UpdateExpoBuildStatus is the HTTP handler for /v2/reports/expo-build.
//
// Auth: HMAC-SHA256 over the raw body using a per-build secret derived as
//
//	buildSecret = HMAC-SHA256(HMACMaster, task_id) (hex)
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

	// Derive per-build secret: HMAC-SHA256(master, task_id) hex
	h1 := hmac.New(sha256.New, []byte(h.HMACMaster))
	h1.Write([]byte(report.TaskID))
	buildSecret := hex.EncodeToString(h1.Sum(nil))

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
