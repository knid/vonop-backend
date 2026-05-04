//go:build integration

package views

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
)

// seedAuthFixtures creates the minimum graph required by the build-events
// handlers: a user, a personal org, a v1 Project, an AppBuildTask scoped to
// that project, plus an auth Token. Returns the token string and the task
// id so tests can assemble httptest requests.
//
// Returns a cleanup function via t.Cleanup — testdb container itself is
// already terminated by NewTestDB's cleanup hook.
func seedAuthFixtures(t *testing.T, db *database.Database) (token string, taskID uuid.UUID, projectID uuid.UUID, otherUserToken string) {
	t.Helper()
	ctx := context.Background()

	user := &models.User{ID: uuid.New(), Email: "owner-" + uuid.NewString()[:8] + "@e.com", Name: "Owner", Password: "x"}
	if _, err := db.DB.NewInsert().Model(user).Exec(ctx); err != nil {
		t.Fatalf("insert owner: %v", err)
	}
	other := &models.User{ID: uuid.New(), Email: "other-" + uuid.NewString()[:8] + "@e.com", Name: "Other", Password: "x"}
	if _, err := db.DB.NewInsert().Model(other).Exec(ctx); err != nil {
		t.Fatalf("insert other: %v", err)
	}

	tok := &models.Token{ID: uuid.New(), UserID: user.ID, String: "tok-" + uuid.NewString(), ExpiredAt: time.Now().Add(24 * time.Hour)}
	if _, err := db.DB.NewInsert().Model(tok).Exec(ctx); err != nil {
		t.Fatalf("insert owner token: %v", err)
	}
	otherTok := &models.Token{ID: uuid.New(), UserID: other.ID, String: "tok-" + uuid.NewString(), ExpiredAt: time.Now().Add(24 * time.Hour)}
	if _, err := db.DB.NewInsert().Model(otherTok).Exec(ctx); err != nil {
		t.Fatalf("insert other token: %v", err)
	}

	// v1 Project owned by `user`. Auth gate compares Project.UserID to
	// the caller's id, so this is the row that gates access.
	project := &models.Project{
		ID: uuid.New(), UserID: user.ID,
		URL: "expo://acme-" + uuid.NewString()[:8], PackageName: "io.acme.test." + uuid.NewString()[:8],
		IconPath: "x", OneSignalAppID: uuid.NewString(), OneSignalAppToken: uuid.NewString(),
	}
	if _, err := db.DB.NewInsert().Model(project).Exec(ctx); err != nil {
		t.Fatalf("insert project: %v", err)
	}

	build := &models.Build{
		ID: uuid.New(), ProjectID: project.ID,
		Title: "T", Description: "D", Icon: "i", SplashScreenColor: "#fff",
		NavBarStyle: "default", BuildNumber: 1, VersionString: "1.0.0",
	}
	if _, err := db.DB.NewInsert().Model(build).Exec(ctx); err != nil {
		t.Fatalf("insert build: %v", err)
	}

	task := &models.AppBuildTask{
		ID: uuid.New(), ProjectID: project.ID, BuildID: build.ID,
		Platform: "Android", StatusText: "Queued",
	}
	if _, err := db.DB.NewInsert().Model(task).Exec(ctx); err != nil {
		t.Fatalf("insert task: %v", err)
	}

	return tok.String, task.ID, project.ID, otherTok.String
}

// seedBuildEvents inserts `n` BuildEvents with sequential occurred_at so
// list / stream ordering can be asserted.
func seedBuildEvents(t *testing.T, db *database.Database, taskID uuid.UUID, types ...string) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	for i, ty := range types {
		ev := &models.BuildEvent{
			TaskID:      taskID,
			EventType:   ty,
			Payload:     []byte(`{"message":"` + ty + `"}`),
			MessageHash: "h-" + ty + "-" + uuid.NewString()[:6],
			OccurredAt:  now.Add(time.Duration(i) * time.Second),
		}
		if err := db.AppendBuildEvent(ctx, ev); err != nil {
			t.Fatalf("append event %s: %v", ty, err)
		}
	}
}

// makeRouter wires the handler into a chi router so URLParam("taskId")
// resolves correctly inside the handler. Mirrors the prod registration
// in cmd/main.go.
func makeRouter(v *HttpViews) *chi.Mux {
	r := chi.NewRouter()
	r.Route("/v2/tasks/{taskId}/events", func(r chi.Router) {
		r.Get("/", v.GetBuildEvents)
		r.Get("/stream", v.StreamBuildEvents)
	})
	return r
}

func TestGetBuildEvents_ReturnsEventsInOrder(t *testing.T) {
	db := database.NewTestDB(t,
		(*models.User)(nil),
		(*models.Token)(nil),
		(*models.Project)(nil),
		(*models.Build)(nil),
		(*models.AppBuildTask)(nil),
		(*models.BuildEvent)(nil),
	)
	// Index that production migrations install — replicate so On Conflict works.
	if _, err := db.DB.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS build_events_task_msg_uniq ON build_events (task_id, message_hash)`); err != nil {
		t.Fatalf("create unique index: %v", err)
	}

	c, _ := controllers.NewTestController(t, db, controllers.TestControllerOptions{})
	v := &HttpViews{Controller: c}
	router := makeRouter(v)

	token, taskID, _, otherToken := seedAuthFixtures(t, db)
	seedBuildEvents(t, db, taskID, "queued", "preparing", "building", "uploading", "built")

	t.Run("happy path returns events in order", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v2/tasks/"+taskID.String()+"/events", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var got []models.BuildEvent
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("unmarshal: %v body=%s", err, w.Body.String())
		}
		if len(got) != 5 {
			t.Fatalf("expected 5 events, got %d", len(got))
		}
		want := []string{"queued", "preparing", "building", "uploading", "built"}
		for i, e := range got {
			if e.EventType != want[i] {
				t.Errorf("idx %d: got %q want %q", i, e.EventType, want[i])
			}
		}
	})

	t.Run("403 when caller doesn't own the project", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v2/tasks/"+taskID.String()+"/events", nil)
		req.Header.Set("Authorization", "Bearer "+otherToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Errorf("expected 403, got %d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("400 on malformed task id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v2/tasks/not-a-uuid/events", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d body=%s", w.Code, w.Body.String())
		}
	})
}

// TestStreamBuildEvents_SnapshotAndLiveEvents drives the SSE handler under
// two phases: first verifies the initial snapshot frame contains all
// pre-seeded events; then injects a new event mid-stream and confirms the
// stream emits an `event` frame for it. Finally asserts the stream closes
// after a terminal event + grace window.
func TestStreamBuildEvents_SnapshotAndLiveEvents(t *testing.T) {
	db := database.NewTestDB(t,
		(*models.User)(nil),
		(*models.Token)(nil),
		(*models.Project)(nil),
		(*models.Build)(nil),
		(*models.AppBuildTask)(nil),
		(*models.BuildEvent)(nil),
	)
	if _, err := db.DB.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS build_events_task_msg_uniq ON build_events (task_id, message_hash)`); err != nil {
		t.Fatalf("create unique index: %v", err)
	}

	c, _ := controllers.NewTestController(t, db, controllers.TestControllerOptions{})
	v := &HttpViews{Controller: c}
	router := makeRouter(v)

	token, taskID, _, _ := seedAuthFixtures(t, db)
	seedBuildEvents(t, db, taskID, "queued", "preparing")

	// Spin the SSE endpoint via httptest.NewServer so we can do a real HTTP
	// read with chunked transfer rather than fighting httptest.ResponseRecorder
	// (which buffers everything until the handler returns).
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/v2/tasks/"+taskID.String()+"/events/stream", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("expected SSE content-type, got %q", ct)
	}

	reader := bufio.NewReader(resp.Body)

	// First frame should be `event: snapshot` carrying both seeded events.
	snapshot := readSSEFrame(t, reader, 3*time.Second)
	if !strings.Contains(snapshot, "event: snapshot") {
		t.Fatalf("expected snapshot frame, got %q", snapshot)
	}
	if !strings.Contains(snapshot, `"queued"`) || !strings.Contains(snapshot, `"preparing"`) {
		t.Errorf("snapshot missing seeded events: %q", snapshot)
	}

	// Inject a fresh event and read the next non-heartbeat frame. Poll
	// cadence is 1s, so allow up to 3s of slack.
	time.Sleep(100 * time.Millisecond) // stagger occurred_at past snapshot's max
	seedBuildEvents(t, db, taskID, "building")

	live := readSSEFrameMatching(t, reader, 4*time.Second, "event: event")
	if !strings.Contains(live, `"building"`) {
		t.Errorf("expected live frame for `building`, got %q", live)
	}

	// Inject the terminal event. Stream should send `end` after the 5s
	// grace window (we wait up to 8s before failing).
	time.Sleep(100 * time.Millisecond)
	seedBuildEvents(t, db, taskID, "built")

	// First receive the `built` event frame.
	builtFrame := readSSEFrameMatching(t, reader, 4*time.Second, `"built"`)
	if !strings.Contains(builtFrame, "event: event") {
		t.Errorf("expected `event` frame for terminal, got %q", builtFrame)
	}

	endFrame := readSSEFrameMatching(t, reader, 8*time.Second, "event: end")
	if !strings.Contains(endFrame, "event: end") {
		t.Fatalf("expected `end` frame after terminal grace, got %q", endFrame)
	}
}

// readSSEFrame reads bytes off the SSE stream until it sees a blank-line
// frame separator or the timeout elapses. Returns the assembled frame
// (including the final blank line).
func readSSEFrame(t *testing.T, r *bufio.Reader, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var b strings.Builder
	for time.Now().Before(deadline) {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("read sse: %v (so far: %q)", err, b.String())
		}
		b.WriteString(line)
		// SSE frame separator is a blank line ("\n" after the last "data:" line).
		if line == "\n" || line == "\r\n" {
			return b.String()
		}
	}
	t.Fatalf("readSSEFrame: timed out after %s, partial: %q", timeout, b.String())
	return ""
}

// readSSEFrameMatching keeps reading frames (skipping heartbeats and any
// frame that doesn't contain `needle`) until it finds one that does, or
// the timeout elapses.
func readSSEFrameMatching(t *testing.T, r *bufio.Reader, timeout time.Duration, needle string) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		frame := readSSEFrame(t, r, remaining)
		if strings.Contains(frame, needle) {
			return frame
		}
		// otherwise loop — heartbeats and unrelated events are dropped
	}
	t.Fatalf("readSSEFrameMatching: never saw %q within %s", needle, timeout)
	return ""
}
