package views

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

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

// expoReportsTaskStore captures the subset of *database.Database operations
// the handler needs. Defined locally so tests can inject a fake without
// pulling in the full Database surface.
//
// *database.Database satisfies this interface — see ApplyExpoBuildStatusUpdate
// in internal/database/tasks.go and the bun.IDB returned by Database.DB.
type expoReportsTaskStore interface {
	database.BuildEventStore
	ApplyExpoBuildStatusUpdate(
		ctx context.Context,
		taskID uuid.UUID,
		statusText string,
		finished bool,
		errorMessage string,
		annotationID *uuid.UUID,
	) (int64, error)
}

// expoReportsReleaseStore captures the subset of *controllers.Controller
// operations needed to advance Release.Status when a submit-phase terminal
// event arrives. Optional — when nil the handler skips the writeback (the
// task projection still happens). Defined here so tests can inject a fake.
//
// *controllers.Controller satisfies this interface (see GetReleaseByCodemagicTaskID,
// MarkReleaseInReview, MarkReleaseRejected on the controller).
type expoReportsReleaseStore interface {
	GetReleaseByCodemagicTaskID(ctx context.Context, taskID uuid.UUID) (*models.Release, error)
	MarkReleaseInReview(ctx context.Context, id uuid.UUID, message string, occurredAt time.Time) (*models.Release, error)
	MarkReleaseRejected(ctx context.Context, id uuid.UUID, message string, occurredAt time.Time) (*models.Release, error)
}

// ExpoReportsHandler holds the small dependency set the webhook handler needs.
// Decoupled from the larger HttpViews so it's straightforward to test.
type ExpoReportsHandler struct {
	Store      expoReportsTaskStore
	HMACMaster string
	// AnnotationDB is the *bun.DB used to look up build annotations on
	// terminal failure events. Optional — if nil, annotation lookup is
	// skipped (the failure is still recorded). Production wires this from
	// controller.DB.DB; tests can leave it nil.
	AnnotationDB *bun.DB
	// ReleaseStore advances the Release row when a submit-phase terminal
	// event arrives. Optional — when nil, submit-phase events still update
	// the task projection but the Release row stays at "uploaded" forever.
	// Wire via WithReleaseStore in production.
	ReleaseStore expoReportsReleaseStore
}

// NewExpoReportsHandler constructs an ExpoReportsHandler.
// hmacMaster is the WEMOB_WORKER_HMAC_KEY secret (loaded from SecretManager
// in cmd/main.go) used to derive per-build signing keys.
//
// If db is a *database.Database, its underlying *bun.DB is wired up as the
// annotation lookup source. Other implementations (test fakes) skip that
// lookup, which is fine — the failure is still persisted.
func NewExpoReportsHandler(store expoReportsTaskStore, hmacMaster string) *ExpoReportsHandler {
	h := &ExpoReportsHandler{Store: store, HMACMaster: hmacMaster}
	if d, ok := store.(*database.Database); ok {
		h.AnnotationDB = d.DB
	}
	return h
}

// WithReleaseStore wires the controller-side hook used to advance
// Release.Status when a submit-phase terminal event arrives. Returns the
// receiver so production wiring can chain: NewExpoReportsHandler(...).WithReleaseStore(c).
func (h *ExpoReportsHandler) WithReleaseStore(rs expoReportsReleaseStore) *ExpoReportsHandler {
	h.ReleaseStore = rs
	return h
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

	// P0 #1 (mobile audit, 2026-05-04): also project the event onto the
	// app_build_task row so cron / UI / latest-successful queries see it.
	// Without this, every Expo build sits at status_text="Queued" forever
	// and MarkOrphanedBuildTasksFailed marks successful builds as stuck
	// 35 min after they actually finished.
	//
	// Vocabulary mirrors the legacy v1 path in controllers/reports.go:
	//   built/submitted/published → "Built", finished=true
	//   error                     → "Failed", finished=true
	//   preparing/building/uploading → "Preparing"/"Building"/"Uploading"
	//
	// Submit-phase terminal events ("submitted") share the same task row as
	// the parent build, so we treat them the same as a "built" — the row
	// reflects the final outcome of whatever workflow is in flight. Status
	// vocabulary unification is P0 #2, intentionally not tackled here.
	if err := h.applyTaskStatusUpdate(r.Context(), taskID, &report); err != nil {
		// Status update failed but the event was already recorded — log
		// and continue so the worker doesn't retry-spam (the event row
		// is the source of truth; the projection is best-effort).
		slog.Warn("expo build status update failed",
			"task_id", taskID.String(), "status", report.Status, "err", err)
	}

	// On submit-phase terminal events, also advance the Release row.
	// Without this the row stays at "uploaded" forever and the studio UI
	// shows "Uploading…" even after Codemagic has finished pushing to
	// TestFlight / Play Internal. P0 from the 2026-05-05 audit.
	h.applyReleaseStatusUpdate(r.Context(), taskID, &report)

	utils.JSONResponse(w, map[string]string{"status": "ok"}, http.StatusOK)
}

// applyReleaseStatusUpdate advances Release.Status when a submit-phase
// terminal event arrives. Build-phase events have no Release row to
// advance — they're handled solely by applyTaskStatusUpdate.
//
// Best-effort: errors are logged and swallowed so a transient lookup
// failure doesn't make the worker retry the whole webhook (the build
// task row was already updated; we don't want to re-process that path).
func (h *ExpoReportsHandler) applyReleaseStatusUpdate(
	ctx context.Context,
	taskID uuid.UUID,
	report *expoBuildReport,
) {
	if h.ReleaseStore == nil {
		return // tests / pre-wiring path
	}
	if !strings.EqualFold(report.Phase, "submit") {
		return // build-phase events have no associated release row
	}
	statusText, finished, isTerminal := classifyExpoStatus(report.Status)
	if !finished || !isTerminal {
		return
	}
	rel, err := h.ReleaseStore.GetReleaseByCodemagicTaskID(ctx, taskID)
	if err != nil || rel == nil {
		// Most common cause: a submit fired before recordReleaseFromSubmit
		// could insert the row, or insert failed and was logged at warn
		// level. We don't want to re-fail the worker for this.
		if err != nil {
			slog.Warn("expo submit webhook: release lookup failed — skipping status writeback",
				"task_id", taskID.String(), "phase", report.Phase, "status", report.Status, "err", err)
		}
		return
	}
	// Idempotent — terminal Release statuses block further transitions in
	// transitionRelease, so a duplicate webhook is a no-op.
	if models.IsTerminalReleaseStatus(rel.Status) {
		return
	}
	occurred := time.Unix(report.Timestamp, 0).UTC()
	if occurred.IsZero() {
		occurred = time.Now().UTC()
	}
	if statusText == "Failed" {
		msg := report.Message
		if msg == "" {
			msg = "submit failed (codemagic deploy reported error)"
		}
		if _, err := h.ReleaseStore.MarkReleaseRejected(ctx, rel.ID, msg, occurred); err != nil {
			slog.Warn("expo submit webhook: MarkReleaseRejected failed",
				"task_id", taskID.String(), "release_id", rel.ID, "err", err)
		}
		return
	}
	// Terminal success — Codemagic has handed the binary off to the store.
	// Apple/Play take it from here; the release_status_poll_cron will
	// continue advancing the row through approved/live as the store
	// progresses.
	msg := report.Message
	if msg == "" {
		msg = "submitted to store via codemagic"
	}
	if _, err := h.ReleaseStore.MarkReleaseInReview(ctx, rel.ID, msg, occurred); err != nil {
		slog.Warn("expo submit webhook: MarkReleaseInReview failed",
			"task_id", taskID.String(), "release_id", rel.ID, "err", err)
	}
}

// applyTaskStatusUpdate maps a Codemagic status event onto the
// app_build_task row. Returns nil for unknown statuses (we still want the
// event recorded — just don't touch the projection).
func (h *ExpoReportsHandler) applyTaskStatusUpdate(
	ctx context.Context,
	taskID uuid.UUID,
	report *expoBuildReport,
) error {
	statusText, finished, isTerminal := classifyExpoStatus(report.Status)
	if !isTerminal && statusText == "" {
		// Unknown / unmapped status — no projection.
		return nil
	}

	var annotationID *uuid.UUID
	errorMessage := ""
	if finished && isTerminalFailure(report.Status) {
		errorMessage = report.Message
		if h.AnnotationDB != nil && errorMessage != "" {
			id, annotErr := database.MatchBuildAnnotation(ctx, h.AnnotationDB, errorMessage)
			if annotErr != nil {
				slog.Warn("expo annotation match failed",
					"task_id", taskID.String(), "err", annotErr)
			} else if id != uuid.Nil {
				annotationID = &id
			}
		}
	}

	_, err := h.Store.ApplyExpoBuildStatusUpdate(
		ctx, taskID, statusText, finished, errorMessage, annotationID,
	)
	return err
}

// classifyExpoStatus maps a webhook status string to the canonical
// app_build_task.status_text and whether the build is in a terminal
// (Finished=true) state.
//
// Returns (statusText, finished, isTerminal). isTerminal=false with
// statusText="" means the status is unrecognized and the caller should skip
// the projection update.
func classifyExpoStatus(raw string) (statusText string, finished bool, isTerminal bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	// Terminal success vocabulary. "submitted"/"published" come from the
	// submit-phase workflows — they share the same task row, so a
	// successful submit transitions the row to its final state too.
	case "built", "success", "completed", "done", "submitted", "published":
		return "Built", true, true
	case "error", "failed", "cancelled", "canceled":
		return "Failed", true, true
	// Non-terminal progress events.
	case "preparing":
		return "Preparing", false, false
	case "building":
		return "Building", false, false
	case "uploading":
		return "Uploading", false, false
	case "queued":
		return "Queued", false, false
	}
	return "", false, false
}

// isTerminalFailure reports whether the status indicates a failed terminal
// state (so the handler should populate ErrorMessage / annotation lookup).
func isTerminalFailure(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "error", "failed", "cancelled", "canceled":
		return true
	}
	return false
}
