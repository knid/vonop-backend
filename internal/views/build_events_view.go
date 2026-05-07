package views

// <P1#13> build events list + SSE — exposes the granular Codemagic event
// stream that the BuildEvent webhook handler captures (queued, preparing,
// building, uploading, built, error, etc.) to the frontend so the publish UI
// can drive its build-stage ladder off the actual stream instead of
// string-matching on a coarse `app_build_task.status` field.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/utils"
)

// terminalEventTypes are event_type values that indicate a build has reached
// a final state. Once we've seen one of these AND the no-new-events grace
// window has elapsed, the SSE stream closes.
//
// Mirrors the worker-side vocabulary in codemagic.yaml (build-webhook posts).
// "submitted" deliberately omitted — the submit phase comes after build and
// uses its own task; including it here would close native build streams
// prematurely on apps that auto-submit.
var terminalEventTypes = map[string]bool{
	"built":     true,
	"error":     true,
	"failed":    true,
	"cancelled": true,
}

// authorizeTaskAccess fetches the task by id, ensures it belongs to an
// AppProject owned by the calling user, and returns the parsed UUID.
//
// Mirrors the ownership guard in CancelBuildTask (controllers/tasks.go) so
// we don't leak event streams across users. The `appProject.UserID == user.ID`
// invariant is the single source of truth for project-scoped read access;
// org-member sharing isn't wired in yet, so we keep this strict until the
// org access layer lands.
func (v *HttpViews) authorizeTaskAccess(r *http.Request) (taskID uuid.UUID, ok bool, errCode string, status int) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		return uuid.Nil, false, constants.ErrAuthInvalidToken, http.StatusUnauthorized
	}

	taskIDStr := chi.URLParam(r, "taskId")
	parsed, err := uuid.Parse(taskIDStr)
	if err != nil {
		return uuid.Nil, false, constants.ErrValidationInvalidFormat, http.StatusBadRequest
	}

	task, err := v.Controller.DB.GetAppBuildTaskById(parsed.String())
	if err != nil {
		return uuid.Nil, false, constants.ErrNotFound, http.StatusNotFound
	}

	if task.AppProjectID == uuid.Nil {
		return uuid.Nil, false, constants.ErrNotFound, http.StatusNotFound
	}
	appProject, err := v.Controller.DB.GetAppProjectByID(task.AppProjectID.String())
	if err != nil || appProject == nil {
		return uuid.Nil, false, constants.ErrNotFound, http.StatusNotFound
	}

	if appProject.UserID.String() != user.ID.String() {
		return uuid.Nil, false, constants.ErrForbidden, http.StatusForbidden
	}

	return parsed, true, "", 0
}

// GetBuildEvents returns every BuildEvent for a task in occurred_at ASC order.
// Lightweight JSON list — used both for SSE-fallback polling and for any
// caller that wants a one-shot snapshot.
//
// Route: GET /v2/tasks/{taskId}/events
func (v *HttpViews) GetBuildEvents(w http.ResponseWriter, r *http.Request) {
	taskID, ok, code, status := v.authorizeTaskAccess(r)
	if !ok {
		utils.JSONErrorCode(w, code, status, nil)
		return
	}

	events, err := v.Controller.DB.ListBuildEventsByTask(r.Context(), taskID)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, events, http.StatusOK)
}

// StreamBuildEvents pushes BuildEvents to the client as Server-Sent Events.
//
// Route: GET /v2/tasks/{taskId}/events/stream
//
// Protocol:
//
//	event: snapshot   — initial frame, data is `[]BuildEvent` (every existing event)
//	event: event      — one new event, data is a single `BuildEvent`
//	: heartbeat       — comment frame every 30s to keep the connection alive
//	event: end        — terminal frame; client should close the EventSource
//
// Lifecycle:
//  1. Send snapshot.
//  2. Poll the DB every 1s for new events (rows with occurred_at > lastSeen).
//  3. If a terminal event has arrived AND no new events for 5s, send `end`
//     and return.
//  4. If the client disconnects (ctx.Done), return immediately.
//
// We intentionally use polling rather than Postgres LISTEN/NOTIFY — there's
// no listener wired into the database layer today, and a 1s poll cadence
// against an indexed (task_id, occurred_at) lookup is cheap. If event volume
// per stream grows, switch to NOTIFY without changing the wire protocol.
func (v *HttpViews) StreamBuildEvents(w http.ResponseWriter, r *http.Request) {
	taskID, ok, code, status := v.authorizeTaskAccess(r)
	if !ok {
		utils.JSONErrorCode(w, code, status, nil)
		return
	}

	flusher, fok := w.(http.Flusher)
	if !fok {
		utils.JSONErrorCode(w, constants.ErrStreamingUnsupported, http.StatusInternalServerError, nil)
		return
	}

	utils.SetSSEHeaders(w)

	ctx := r.Context()

	// Snapshot every existing event so the client doesn't have to call the
	// list endpoint first. Empty array is valid (newly-created task with no
	// webhooks yet) — we still want the stream to stay open and surface events
	// as they arrive.
	initial, err := v.Controller.DB.ListBuildEventsByTask(ctx, taskID)
	if err != nil {
		// At this point we've already written SSE headers, so we can't fall
		// back to a JSON error response. Send an `error` event with a generic
		// payload and bail.
		_ = sseWrite(w, flusher, "error", map[string]string{"message": "snapshot_failed"})
		return
	}
	if err := sseWrite(w, flusher, "snapshot", initial); err != nil {
		return // client gone
	}

	var lastSeen time.Time
	terminalSeen := false
	for _, e := range initial {
		if e.OccurredAt.After(lastSeen) {
			lastSeen = e.OccurredAt
		}
		if terminalEventTypes[strings.ToLower(e.EventType)] {
			terminalSeen = true
		}
	}

	pollTicker := time.NewTicker(1 * time.Second)
	defer pollTicker.Stop()
	heartbeatTicker := time.NewTicker(30 * time.Second)
	defer heartbeatTicker.Stop()

	// Once a terminal event lands we wait this long for any trailing events
	// (e.g. a final "log_uploaded" report posted right after "built") before
	// closing the stream. Five seconds is enough to cover ordinary webhook
	// retry windows.
	const terminalGrace = 5 * time.Second
	var terminalAt time.Time

	for {
		select {
		case <-ctx.Done():
			return

		case <-heartbeatTicker.C:
			// SSE comment frame — keeps proxies (Caddy, browsers) from idling
			// the connection out. Doesn't unblock the EventSource onmessage.
			if _, werr := fmt.Fprintf(w, ": heartbeat\n\n"); werr != nil {
				return
			}
			flusher.Flush()

		case now := <-pollTicker.C:
			fresh, err := pollFreshEvents(ctx, v, taskID, lastSeen)
			if err != nil {
				slog.Debug("StreamBuildEvents: poll failed", "task_id", taskID, "error", err)
				continue
			}
			for _, e := range fresh {
				if werr := sseWrite(w, flusher, "event", e); werr != nil {
					return // client gone
				}
				if e.OccurredAt.After(lastSeen) {
					lastSeen = e.OccurredAt
				}
				if terminalEventTypes[strings.ToLower(e.EventType)] {
					terminalSeen = true
					terminalAt = now
				}
			}

			if terminalSeen {
				if terminalAt.IsZero() {
					// Terminal event already in the snapshot — start the
					// grace window from the first poll cycle that observes it.
					terminalAt = now
				}
				if len(fresh) == 0 && now.Sub(terminalAt) >= terminalGrace {
					_ = sseWrite(w, flusher, "end", map[string]any{"task_id": taskID})
					return
				}
				if len(fresh) > 0 {
					// New events arrived during the grace window — reset the
					// timer so we don't truncate trailing logs.
					terminalAt = now
				}
			}
		}
	}
}

// pollFreshEvents returns events with occurred_at > since, ordered ASC.
// Filters in-process because BuildEventStore.ListBuildEventsByTask doesn't
// take a time predicate; the row count per task stays small (Codemagic
// posts ~5–10 events per build), so the cost is negligible compared to
// adding a new DB method.
func pollFreshEvents(ctx context.Context, v *HttpViews, taskID uuid.UUID, since time.Time) ([]*models.BuildEvent, error) {
	all, err := v.Controller.DB.ListBuildEventsByTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if since.IsZero() {
		return all, nil
	}
	out := make([]*models.BuildEvent, 0, len(all))
	for _, e := range all {
		if e.OccurredAt.After(since) {
			out = append(out, e)
		}
	}
	return out, nil
}

// sseWrite emits one named SSE frame with a JSON payload. Returns the
// underlying io.Writer error (typically a broken pipe when the client
// disconnects) so the caller can stop the stream.
func sseWrite(w http.ResponseWriter, flusher http.Flusher, event string, data any) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, payload); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

