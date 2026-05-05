//go:build integration

// Integration tests for the P0 #1 fix (mobile audit, 2026-05-04):
// UpdateExpoBuildStatus must project terminal webhook events onto the
// app_build_task row, not just append to build_events. Without these tests
// the bug recurs silently — every Expo build sits at "Queued" forever and
// the orphaned-task cron kills successful builds 35 minutes after they
// finished.
package views

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/utils"
)

// signRequest mirrors signTestRequest from the unit test file (kept private
// so this file is self-contained when run with -tags=integration alongside
// the standard test build).
func signRequest(t *testing.T, taskID, ts, body string) string {
	t.Helper()
	derived := utils.DeriveHMACKey([]byte(testHMACMaster), "build-webhook", taskID)
	buildSecret := hex.EncodeToString(derived)
	mac := hmac.New(sha256.New, []byte(buildSecret))
	mac.Write([]byte(ts + "." + body))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// seedExpoBuildFixtures inserts the minimum graph for the webhook to update
// a real app_build_task row: a User, Project, Build, and an unfinished
// AppBuildTask with status_text="Queued" (matching prod creation defaults).
func seedExpoBuildFixtures(t *testing.T, db *database.Database) (project *models.Project, build *models.Build, task *models.AppBuildTask) {
	t.Helper()
	ctx := context.Background()

	user := &models.User{
		ID: uuid.New(), Email: "owner-" + uuid.NewString()[:8] + "@e.com",
		Name: "Owner", Password: "x",
	}
	if _, err := db.DB.NewInsert().Model(user).Exec(ctx); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	project = &models.Project{
		ID: uuid.New(), UserID: user.ID,
		URL: "expo://acme-" + uuid.NewString()[:8],
		PackageName: "io.acme.test." + uuid.NewString()[:8],
		IconPath: "x", OneSignalAppID: uuid.NewString(), OneSignalAppToken: uuid.NewString(),
	}
	if _, err := db.DB.NewInsert().Model(project).Exec(ctx); err != nil {
		t.Fatalf("insert project: %v", err)
	}

	build = &models.Build{
		ID: uuid.New(), ProjectID: project.ID,
		Title: "T", Description: "D", Icon: "i", SplashScreenColor: "#fff",
		NavBarStyle: "default", BuildNumber: 1, VersionString: "1.0.0",
	}
	if _, err := db.DB.NewInsert().Model(build).Exec(ctx); err != nil {
		t.Fatalf("insert build: %v", err)
	}

	task = &models.AppBuildTask{
		ID: uuid.New(), ProjectID: project.ID, BuildID: build.ID,
		Platform: "Android", StatusText: "Queued",
	}
	if _, err := db.DB.NewInsert().Model(task).Exec(ctx); err != nil {
		t.Fatalf("insert task: %v", err)
	}

	return project, build, task
}

// postWebhook fires a single signed webhook at the handler. Returns the
// recorder so callers can inspect status/body.
func postWebhook(t *testing.T, h *ExpoReportsHandler, taskID, status, message string) *httptest.ResponseRecorder {
	t.Helper()
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	body := fmt.Sprintf(`{"task_id":%q,"status":%q,"message":%q,"timestamp":%s}`,
		taskID, status, message, ts)
	sig := signRequest(t, taskID, ts, body)

	req := httptest.NewRequest(http.MethodPost, "/v2/reports/expo-build", bytes.NewBufferString(body))
	req.Header.Set("X-Wemob-Signature", sig)
	req.Header.Set("X-Wemob-Timestamp", ts)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	h.UpdateExpoBuildStatus(rr, req)
	return rr
}

// reloadTask fetches the current row state by id (intentionally NOT through
// the soft-delete-respecting helper — we want to see the row regardless).
func reloadTask(t *testing.T, db *database.Database, id uuid.UUID) *models.AppBuildTask {
	t.Helper()
	var out models.AppBuildTask
	if err := db.DB.NewSelect().Model(&out).Where("id = ?", id).Scan(context.Background()); err != nil {
		t.Fatalf("reload task: %v", err)
	}
	return &out
}

// TestUpdateExpoBuildStatus_BuiltEventUpdatesTaskRow is the core P0 #1
// regression test: a "built" webhook must transition the task row to
// status_text="Built", finished=true. Without the fix, the row stays at
// "Queued" forever (and MarkOrphanedBuildTasksFailed kills it later).
func TestUpdateExpoBuildStatus_BuiltEventUpdatesTaskRow(t *testing.T) {
	db := database.NewTestDB(t,
		(*models.User)(nil),
		(*models.Project)(nil),
		(*models.Build)(nil),
		(*models.AppBuildTask)(nil),
		(*models.BuildEvent)(nil),
	)
	if _, err := db.DB.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS build_events_task_msg_uniq ON build_events (task_id, message_hash)`); err != nil {
		t.Fatalf("create unique index: %v", err)
	}

	_, _, task := seedExpoBuildFixtures(t, db)

	// Sanity: row starts unfinished.
	pre := reloadTask(t, db, task.ID)
	if pre.Finished {
		t.Fatalf("seed bug: task already finished")
	}
	if pre.StatusText != "Queued" {
		t.Fatalf("seed bug: status_text=%q want Queued", pre.StatusText)
	}

	h := NewExpoReportsHandler(db, testHMACMaster)
	rr := postWebhook(t, h, task.ID.String(), "built", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("webhook: got %d body=%s", rr.Code, rr.Body.String())
	}

	got := reloadTask(t, db, task.ID)
	if got.StatusText != "Built" {
		t.Errorf("status_text: got %q want Built", got.StatusText)
	}
	if !got.Finished {
		t.Errorf("finished: got false want true")
	}
	if got.ErrorMessage != "" {
		t.Errorf("error_message: got %q want empty", got.ErrorMessage)
	}

	// Cross-cutting safety check: the OTHER columns must not have been
	// nulled out by Bun's whole-row-update footgun (see memory
	// `feedback_bun_whole_row_update.md`). If we accidentally regress to
	// `db.NewUpdate().Model(task).Where(...)` with a stripped-down struct,
	// project_id / build_id / platform would silently null. This catches
	// that regression at the row level.
	if got.ProjectID == uuid.Nil {
		t.Errorf("project_id was nulled (bun whole-row footgun?)")
	}
	if got.BuildID == uuid.Nil {
		t.Errorf("build_id was nulled (bun whole-row footgun?)")
	}
	if got.Platform != "Android" {
		t.Errorf("platform: got %q want Android (bun whole-row footgun?)", got.Platform)
	}
}

// TestUpdateExpoBuildStatus_FailedEventPopulatesError verifies the
// error-path mirror of the success case.
func TestUpdateExpoBuildStatus_FailedEventPopulatesError(t *testing.T) {
	db := database.NewTestDB(t,
		(*models.User)(nil),
		(*models.Project)(nil),
		(*models.Build)(nil),
		(*models.AppBuildTask)(nil),
		(*models.BuildEvent)(nil),
	)
	if _, err := db.DB.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS build_events_task_msg_uniq ON build_events (task_id, message_hash)`); err != nil {
		t.Fatalf("create unique index: %v", err)
	}

	_, _, task := seedExpoBuildFixtures(t, db)

	h := NewExpoReportsHandler(db, testHMACMaster)
	rr := postWebhook(t, h, task.ID.String(), "error", "gradle build failed: out of memory")
	if rr.Code != http.StatusOK {
		t.Fatalf("webhook: got %d body=%s", rr.Code, rr.Body.String())
	}

	got := reloadTask(t, db, task.ID)
	if got.StatusText != "Failed" {
		t.Errorf("status_text: got %q want Failed", got.StatusText)
	}
	if !got.Finished {
		t.Errorf("finished: got false want true")
	}
	if got.ErrorMessage != "gradle build failed: out of memory" {
		t.Errorf("error_message: got %q want the gradle message", got.ErrorMessage)
	}
}

// TestUpdateExpoBuildStatus_ProgressEventDoesNotFinish covers the
// non-terminal path: "building" must update status_text but leave finished
// alone so subsequent terminal events can still close the row.
func TestUpdateExpoBuildStatus_ProgressEventDoesNotFinish(t *testing.T) {
	db := database.NewTestDB(t,
		(*models.User)(nil),
		(*models.Project)(nil),
		(*models.Build)(nil),
		(*models.AppBuildTask)(nil),
		(*models.BuildEvent)(nil),
	)
	if _, err := db.DB.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS build_events_task_msg_uniq ON build_events (task_id, message_hash)`); err != nil {
		t.Fatalf("create unique index: %v", err)
	}

	_, _, task := seedExpoBuildFixtures(t, db)

	h := NewExpoReportsHandler(db, testHMACMaster)

	// Send the realistic Codemagic progress sequence.
	for _, status := range []string{"preparing", "building", "uploading"} {
		rr := postWebhook(t, h, task.ID.String(), status, "")
		if rr.Code != http.StatusOK {
			t.Fatalf("webhook %s: got %d body=%s", status, rr.Code, rr.Body.String())
		}
	}

	got := reloadTask(t, db, task.ID)
	if got.StatusText != "Uploading" {
		t.Errorf("status_text after uploading: got %q want Uploading", got.StatusText)
	}
	if got.Finished {
		t.Errorf("finished: got true on a non-terminal sequence")
	}

	// Now send terminal "built" — should flip finished and override status.
	rr := postWebhook(t, h, task.ID.String(), "built", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("built webhook: got %d body=%s", rr.Code, rr.Body.String())
	}

	got = reloadTask(t, db, task.ID)
	if got.StatusText != "Built" || !got.Finished {
		t.Errorf("after built: status=%q finished=%v want Built true", got.StatusText, got.Finished)
	}
}

// TestUpdateExpoBuildStatus_IdempotencyDoesNotRegressTerminal is the
// idempotency test: once a task is finished=true with a successful
// "Built" status, a late-arriving "error" webhook (replay, retry, race)
// must NOT regress it to Failed. This protects against the real-world
// scenario where Codemagic's retry loop double-fires terminal events.
func TestUpdateExpoBuildStatus_IdempotencyDoesNotRegressTerminal(t *testing.T) {
	db := database.NewTestDB(t,
		(*models.User)(nil),
		(*models.Project)(nil),
		(*models.Build)(nil),
		(*models.AppBuildTask)(nil),
		(*models.BuildEvent)(nil),
	)
	if _, err := db.DB.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS build_events_task_msg_uniq ON build_events (task_id, message_hash)`); err != nil {
		t.Fatalf("create unique index: %v", err)
	}

	_, _, task := seedExpoBuildFixtures(t, db)

	h := NewExpoReportsHandler(db, testHMACMaster)

	// First terminal event flips the row to Built/finished.
	rr1 := postWebhook(t, h, task.ID.String(), "built", "")
	if rr1.Code != http.StatusOK {
		t.Fatalf("first webhook: got %d", rr1.Code)
	}
	first := reloadTask(t, db, task.ID)
	if !first.Finished || first.StatusText != "Built" {
		t.Fatalf("first webhook didn't flip row: status=%q finished=%v",
			first.StatusText, first.Finished)
	}
	firstUpdatedAt := first.UpdatedAt

	// Wait so the no-op vs touch is detectable via updated_at.
	time.Sleep(50 * time.Millisecond)

	// Second terminal event — this time "error" with a different message.
	// The handler still 200s (event is appended; idempotency guards the
	// projection). The row must NOT regress.
	rr2 := postWebhook(t, h, task.ID.String(), "error", "racy late failure")
	if rr2.Code != http.StatusOK {
		t.Fatalf("second webhook: got %d", rr2.Code)
	}

	second := reloadTask(t, db, task.ID)
	if second.StatusText != "Built" {
		t.Errorf("status regressed: got %q want Built (idempotency broken)", second.StatusText)
	}
	if !second.Finished {
		t.Errorf("finished regressed to false")
	}
	if second.ErrorMessage != "" {
		t.Errorf("error_message overwritten on already-finished row: got %q", second.ErrorMessage)
	}
	if !second.UpdatedAt.Equal(firstUpdatedAt) {
		t.Errorf("updated_at touched on already-finished row (got %v want %v)",
			second.UpdatedAt, firstUpdatedAt)
	}

	// Bonus: a duplicate "built" should also be a no-op (same MessageHash
	// → AppendBuildEvent dedup, same finished guard → projection skip).
	rr3 := postWebhook(t, h, task.ID.String(), "built", "")
	if rr3.Code != http.StatusOK {
		t.Fatalf("third webhook: got %d", rr3.Code)
	}
	third := reloadTask(t, db, task.ID)
	if third.StatusText != "Built" || !third.Finished {
		t.Errorf("duplicate built broke row: status=%q finished=%v",
			third.StatusText, third.Finished)
	}
}

// TestUpdateExpoBuildStatus_FailedAnnotationLookup verifies that a
// matching BuildAnnotation row gets attached when the error_message
// matches a registered LogRegex.
func TestUpdateExpoBuildStatus_FailedAnnotationLookup(t *testing.T) {
	db := database.NewTestDB(t,
		(*models.User)(nil),
		(*models.Project)(nil),
		(*models.Build)(nil),
		(*models.AppBuildTask)(nil),
		(*models.BuildEvent)(nil),
		(*models.BuildAnnotation)(nil),
	)
	if _, err := db.DB.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS build_events_task_msg_uniq ON build_events (task_id, message_hash)`); err != nil {
		t.Fatalf("create unique index: %v", err)
	}

	annot := &models.BuildAnnotation{
		ID: uuid.New(), Name: "gradle_oom",
		LogRegex:     `out of memory`,
		HumanMessage: "Gradle ran out of memory.",
		SuggestedFix: "Increase the heap size.",
		Enabled:      true,
	}
	if _, err := db.DB.NewInsert().Model(annot).Exec(context.Background()); err != nil {
		t.Fatalf("insert annotation: %v", err)
	}

	_, _, task := seedExpoBuildFixtures(t, db)

	h := NewExpoReportsHandler(db, testHMACMaster)
	rr := postWebhook(t, h, task.ID.String(), "error", "gradle: out of memory at line 42")
	if rr.Code != http.StatusOK {
		t.Fatalf("webhook: got %d body=%s", rr.Code, rr.Body.String())
	}

	got := reloadTask(t, db, task.ID)
	if got.AnnotationID == nil {
		t.Fatalf("annotation_id was not populated")
	}
	if *got.AnnotationID != annot.ID {
		t.Errorf("annotation_id: got %s want %s", *got.AnnotationID, annot.ID)
	}
}
