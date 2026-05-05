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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/utils"
)

const testHMACMaster = "test-master-key-do-not-use-in-prod"

// signTestRequest mirrors the Codemagic signing logic post-P0 #8b:
// per-build secret = hex(HKDF(master, salt, info="build-webhook:<task_id>"));
// signature = "sha256=" + HMAC(per-build, "<ts>.<body>").
func signTestRequest(t *testing.T, taskID, tsHeader, body string) (sig string) {
	t.Helper()
	derived := utils.DeriveHMACKey([]byte(testHMACMaster), "build-webhook", taskID)
	buildSecret := hex.EncodeToString(derived)
	h2 := hmac.New(sha256.New, []byte(buildSecret))
	h2.Write([]byte(tsHeader + "." + body))
	return "sha256=" + hex.EncodeToString(h2.Sum(nil))
}


// fakeBuildEventStoreForViews is local to views tests. It also satisfies
// expoReportsTaskStore so the handler can call ApplyExpoBuildStatusUpdate
// without a real Postgres in unit tests.
type fakeBuildEventStoreForViews struct {
	mu     sync.Mutex
	events []*models.BuildEvent
	// statusUpdates captures the (status_text, finished, error_message,
	// annotation_id) tuple for each ApplyExpoBuildStatusUpdate call. Tests
	// assert on this directly. Idempotency is simulated: once a task is
	// finished, subsequent updates are no-ops (mirrors prod SQL guard).
	statusUpdates    []capturedStatusUpdate
	finishedTaskIDs  map[uuid.UUID]bool
}

type capturedStatusUpdate struct {
	TaskID       uuid.UUID
	StatusText   string
	Finished     bool
	ErrorMessage string
	AnnotationID *uuid.UUID
}

func (s *fakeBuildEventStoreForViews) AppendBuildEvent(_ context.Context, e *models.BuildEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.events {
		if existing.TaskID == e.TaskID && existing.MessageHash == e.MessageHash {
			return nil
		}
	}
	s.events = append(s.events, e)
	return nil
}

func (s *fakeBuildEventStoreForViews) ListBuildEventsByTask(_ context.Context, taskID uuid.UUID) ([]*models.BuildEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*models.BuildEvent
	for _, e := range s.events {
		if e.TaskID == taskID {
			out = append(out, e)
		}
	}
	return out, nil
}

func (s *fakeBuildEventStoreForViews) ApplyExpoBuildStatusUpdate(
	_ context.Context,
	taskID uuid.UUID,
	statusText string,
	finished bool,
	errorMessage string,
	annotationID *uuid.UUID,
) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finishedTaskIDs == nil {
		s.finishedTaskIDs = map[uuid.UUID]bool{}
	}
	if s.finishedTaskIDs[taskID] {
		// Mirror the prod SQL: WHERE finished = false skips the row.
		return 0, nil
	}
	s.statusUpdates = append(s.statusUpdates, capturedStatusUpdate{
		TaskID: taskID, StatusText: statusText, Finished: finished,
		ErrorMessage: errorMessage, AnnotationID: annotationID,
	})
	if finished {
		s.finishedTaskIDs[taskID] = true
	}
	return 1, nil
}

// compile-time check that fakeBuildEventStoreForViews satisfies the interfaces
var _ database.BuildEventStore = (*fakeBuildEventStoreForViews)(nil)
var _ expoReportsTaskStore = (*fakeBuildEventStoreForViews)(nil)

func newTestExpoReportsHandler(t *testing.T, store expoReportsTaskStore, hmacKey string) *ExpoReportsHandler {
	t.Helper()
	return NewExpoReportsHandler(store, hmacKey)
}

func TestUpdateExpoBuildStatus_ValidSignature(t *testing.T) {
	store := &fakeBuildEventStoreForViews{}
	handler := newTestExpoReportsHandler(t, store, testHMACMaster)

	taskID := uuid.New().String()
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	body := fmt.Sprintf(`{"task_id":%q,"status":"building","message":"go","timestamp":%s}`, taskID, ts)
	sig := signTestRequest(t, taskID, ts, body)

	req := httptest.NewRequest(http.MethodPost, "/v2/reports/expo-build", bytes.NewBufferString(body))
	req.Header.Set("X-Wemob-Signature", sig)
	req.Header.Set("X-Wemob-Timestamp", ts)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.UpdateExpoBuildStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	store.mu.Lock()
	evCount := len(store.events)
	store.mu.Unlock()
	if evCount != 1 {
		t.Errorf("expected 1 event in store, got %d", evCount)
	}
}

func TestUpdateExpoBuildStatus_InvalidSignature(t *testing.T) {
	store := &fakeBuildEventStoreForViews{}
	handler := newTestExpoReportsHandler(t, store, testHMACMaster)

	ts := strconv.FormatInt(time.Now().Unix(), 10)
	body := `{"task_id":"task-123","status":"building","timestamp":` + ts + `}`
	req := httptest.NewRequest(http.MethodPost, "/v2/reports/expo-build", bytes.NewBufferString(body))
	req.Header.Set("X-Wemob-Signature", "sha256=deadbeef")
	req.Header.Set("X-Wemob-Timestamp", ts)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.UpdateExpoBuildStatus(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
	store.mu.Lock()
	evCount := len(store.events)
	store.mu.Unlock()
	if evCount != 0 {
		t.Errorf("expected 0 events stored on invalid sig, got %d", evCount)
	}
}

func TestUpdateExpoBuildStatus_StaleTimestamp(t *testing.T) {
	store := &fakeBuildEventStoreForViews{}
	handler := newTestExpoReportsHandler(t, store, testHMACMaster)

	taskID := uuid.New().String()
	staleTS := "1000" // way in the past
	body := fmt.Sprintf(`{"task_id":%q,"status":"built","timestamp":1000}`, taskID)
	sig := signTestRequest(t, taskID, staleTS, body)

	req := httptest.NewRequest(http.MethodPost, "/v2/reports/expo-build", bytes.NewBufferString(body))
	req.Header.Set("X-Wemob-Signature", sig)
	req.Header.Set("X-Wemob-Timestamp", staleTS)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.UpdateExpoBuildStatus(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for stale timestamp, got %d", rr.Code)
	}
}

func TestUpdateExpoBuildStatus_IdempotentRedelivery(t *testing.T) {
	store := &fakeBuildEventStoreForViews{}
	handler := newTestExpoReportsHandler(t, store, testHMACMaster)

	taskID := uuid.New().String()
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	body := fmt.Sprintf(`{"task_id":%q,"status":"built","message":"done","timestamp":%s}`, taskID, ts)
	sig := signTestRequest(t, taskID, ts, body)

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodPost, "/v2/reports/expo-build", bytes.NewBufferString(body))
		req.Header.Set("X-Wemob-Signature", sig)
		req.Header.Set("X-Wemob-Timestamp", ts)
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		handler.UpdateExpoBuildStatus(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("iter %d: got %d: %s", i, rr.Code, rr.Body.String())
		}
	}
	store.mu.Lock()
	evCount := len(store.events)
	store.mu.Unlock()
	if evCount != 1 {
		t.Errorf("expected 1 event after triple-delivery (deduped), got %d", evCount)
	}
}

// TestUpdateExpoBuildStatus_FutureTimestamp verifies that a request with a
// timestamp 30 minutes in the future is rejected even when the signature is
// otherwise valid (forward-timestamp attack).
func TestUpdateExpoBuildStatus_FutureTimestamp(t *testing.T) {
	store := &fakeBuildEventStoreForViews{}
	handler := newTestExpoReportsHandler(t, store, testHMACMaster)

	taskID := uuid.New().String()
	futureTS := strconv.FormatInt(time.Now().Add(30*time.Minute).Unix(), 10)
	body := fmt.Sprintf(`{"task_id":%q,"status":"built","timestamp":%s}`, taskID, futureTS)
	// Sign with the correct format (ts.body) so the only reason for rejection is the timestamp.
	sig := signTestRequest(t, taskID, futureTS, body)

	req := httptest.NewRequest(http.MethodPost, "/v2/reports/expo-build", bytes.NewBufferString(body))
	req.Header.Set("X-Wemob-Signature", sig)
	req.Header.Set("X-Wemob-Timestamp", futureTS)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.UpdateExpoBuildStatus(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for future timestamp (+30min), got %d: %s", rr.Code, rr.Body.String())
	}
	store.mu.Lock()
	evCount := len(store.events)
	store.mu.Unlock()
	if evCount != 0 {
		t.Errorf("expected 0 events stored on future-timestamp request, got %d", evCount)
	}
}

// TestUpdateExpoBuildStatus_SubmitPhase verifies the P1.2 phase=submit
// extension: payloads with phase="submit" are accepted and stored verbatim
// (the field lands in BuildEvent.Payload jsonb so downstream queries can
// filter via @> operator).
func TestUpdateExpoBuildStatus_SubmitPhase(t *testing.T) {
	store := &fakeBuildEventStoreForViews{}
	handler := newTestExpoReportsHandler(t, store, testHMACMaster)

	taskID := uuid.New().String()
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	body := fmt.Sprintf(`{"task_id":%q,"status":"submitted","phase":"submit","timestamp":%s}`, taskID, ts)
	sig := signTestRequest(t, taskID, ts, body)

	req := httptest.NewRequest(http.MethodPost, "/v2/reports/expo-build", bytes.NewBufferString(body))
	req.Header.Set("X-Wemob-Signature", sig)
	req.Header.Set("X-Wemob-Timestamp", ts)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.UpdateExpoBuildStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("got %d body=%s", rr.Code, rr.Body.String())
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.events) != 1 {
		t.Fatalf("expected 1 stored event, got %d", len(store.events))
	}
	if !strings.Contains(string(store.events[0].Payload), `"phase":"submit"`) {
		t.Errorf("payload missing phase=submit: %s", store.events[0].Payload)
	}
}

// fakeReleaseStore satisfies expoReportsReleaseStore for handler tests. The
// release field, when set, must have CodemagicTaskID populated so the lookup
// match mirrors prod's WHERE codemagic_task_id = ? semantics.
type fakeReleaseStore struct {
	mu            sync.Mutex
	release       *models.Release
	lookupErr     error
	inReviewCalls []releaseTransitionCall
	rejectedCalls []releaseTransitionCall
}

type releaseTransitionCall struct {
	ID         uuid.UUID
	Message    string
	OccurredAt time.Time
}

func (s *fakeReleaseStore) GetReleaseByCodemagicTaskID(_ context.Context, taskID uuid.UUID) (*models.Release, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lookupErr != nil {
		return nil, s.lookupErr
	}
	if s.release == nil || s.release.CodemagicTaskID == nil {
		return nil, nil
	}
	if *s.release.CodemagicTaskID != taskID {
		return nil, nil
	}
	return s.release, nil
}

func (s *fakeReleaseStore) MarkReleaseInReview(_ context.Context, id uuid.UUID, msg string, t time.Time) (*models.Release, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inReviewCalls = append(s.inReviewCalls, releaseTransitionCall{ID: id, Message: msg, OccurredAt: t})
	if s.release != nil {
		s.release.Status = models.ReleaseStatusInReview
	}
	return s.release, nil
}

func (s *fakeReleaseStore) MarkReleaseRejected(_ context.Context, id uuid.UUID, msg string, t time.Time) (*models.Release, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rejectedCalls = append(s.rejectedCalls, releaseTransitionCall{ID: id, Message: msg, OccurredAt: t})
	if s.release != nil {
		s.release.Status = models.ReleaseStatusRejected
	}
	return s.release, nil
}

var _ expoReportsReleaseStore = (*fakeReleaseStore)(nil)

// postSubmitWebhook is a small helper that signs and posts a submit-phase
// webhook with the given status/message and returns the recorder so the test
// can assert on response code.
func postSubmitWebhook(t *testing.T, h *ExpoReportsHandler, taskID, status, message string) *httptest.ResponseRecorder {
	t.Helper()
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	body := fmt.Sprintf(`{"task_id":%q,"status":%q,"message":%q,"phase":"submit","timestamp":%s}`,
		taskID, status, message, ts)
	sig := signTestRequest(t, taskID, ts, body)
	req := httptest.NewRequest(http.MethodPost, "/v2/reports/expo-build", bytes.NewBufferString(body))
	req.Header.Set("X-Wemob-Signature", sig)
	req.Header.Set("X-Wemob-Timestamp", ts)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.UpdateExpoBuildStatus(rr, req)
	return rr
}

func TestUpdateExpoBuildStatus_SubmitPhase_AdvancesReleaseToInReview(t *testing.T) {
	store := &fakeBuildEventStoreForViews{}
	taskID := uuid.New()
	relID := uuid.New()
	rs := &fakeReleaseStore{release: &models.Release{
		ID:              relID,
		Status:          models.ReleaseStatusUploaded,
		CodemagicTaskID: &taskID,
	}}
	handler := newTestExpoReportsHandler(t, store, testHMACMaster).WithReleaseStore(rs)

	rr := postSubmitWebhook(t, handler, taskID.String(), "submitted", "uploaded to TestFlight")
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d body=%s", rr.Code, rr.Body.String())
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if len(rs.inReviewCalls) != 1 {
		t.Fatalf("expected 1 MarkReleaseInReview call, got %d", len(rs.inReviewCalls))
	}
	if rs.inReviewCalls[0].ID != relID {
		t.Errorf("transitioned wrong release: got %s want %s", rs.inReviewCalls[0].ID, relID)
	}
	if !strings.Contains(rs.inReviewCalls[0].Message, "TestFlight") {
		t.Errorf("expected message to carry the webhook message, got %q", rs.inReviewCalls[0].Message)
	}
	if len(rs.rejectedCalls) != 0 {
		t.Errorf("unexpected MarkReleaseRejected calls: %d", len(rs.rejectedCalls))
	}
}

func TestUpdateExpoBuildStatus_SubmitPhase_AdvancesReleaseToRejected(t *testing.T) {
	store := &fakeBuildEventStoreForViews{}
	taskID := uuid.New()
	relID := uuid.New()
	rs := &fakeReleaseStore{release: &models.Release{
		ID:              relID,
		Status:          models.ReleaseStatusUploaded,
		CodemagicTaskID: &taskID,
	}}
	handler := newTestExpoReportsHandler(t, store, testHMACMaster).WithReleaseStore(rs)

	rr := postSubmitWebhook(t, handler, taskID.String(), "error", "fastlane: invalid binary")
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d body=%s", rr.Code, rr.Body.String())
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if len(rs.rejectedCalls) != 1 {
		t.Fatalf("expected 1 MarkReleaseRejected call, got %d", len(rs.rejectedCalls))
	}
	if rs.rejectedCalls[0].ID != relID {
		t.Errorf("transitioned wrong release: got %s want %s", rs.rejectedCalls[0].ID, relID)
	}
	if !strings.Contains(rs.rejectedCalls[0].Message, "invalid binary") {
		t.Errorf("expected message to surface the failure reason, got %q", rs.rejectedCalls[0].Message)
	}
	if len(rs.inReviewCalls) != 0 {
		t.Errorf("unexpected MarkReleaseInReview calls: %d", len(rs.inReviewCalls))
	}
}

func TestUpdateExpoBuildStatus_BuildPhase_LeavesReleaseAlone(t *testing.T) {
	// Build-phase events (phase absent or "build") must NOT touch the release row.
	store := &fakeBuildEventStoreForViews{}
	taskID := uuid.New()
	rs := &fakeReleaseStore{release: &models.Release{
		ID:              uuid.New(),
		Status:          models.ReleaseStatusUploaded,
		CodemagicTaskID: &taskID,
	}}
	handler := newTestExpoReportsHandler(t, store, testHMACMaster).WithReleaseStore(rs)

	ts := strconv.FormatInt(time.Now().Unix(), 10)
	// Note: no "phase" field — defaults to build.
	body := fmt.Sprintf(`{"task_id":%q,"status":"built","timestamp":%s}`, taskID.String(), ts)
	sig := signTestRequest(t, taskID.String(), ts, body)
	req := httptest.NewRequest(http.MethodPost, "/v2/reports/expo-build", bytes.NewBufferString(body))
	req.Header.Set("X-Wemob-Signature", sig)
	req.Header.Set("X-Wemob-Timestamp", ts)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.UpdateExpoBuildStatus(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d body=%s", rr.Code, rr.Body.String())
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if len(rs.inReviewCalls)+len(rs.rejectedCalls) != 0 {
		t.Errorf("build-phase webhook must not transition a release: in_review=%d rejected=%d",
			len(rs.inReviewCalls), len(rs.rejectedCalls))
	}
}

func TestUpdateExpoBuildStatus_SubmitPhase_NoReleaseRow(t *testing.T) {
	// recordReleaseFromSubmit is best-effort; if the release row was never
	// inserted, the webhook must not retry-spam — return 200 and skip.
	store := &fakeBuildEventStoreForViews{}
	taskID := uuid.New()
	rs := &fakeReleaseStore{release: nil} // no release rows exist
	handler := newTestExpoReportsHandler(t, store, testHMACMaster).WithReleaseStore(rs)

	rr := postSubmitWebhook(t, handler, taskID.String(), "submitted", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d body=%s", rr.Code, rr.Body.String())
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if len(rs.inReviewCalls)+len(rs.rejectedCalls) != 0 {
		t.Errorf("must not transition when no release row found")
	}
}

func TestUpdateExpoBuildStatus_SubmitPhase_TerminalReleaseStaysPut(t *testing.T) {
	// A duplicate webhook arriving after the release went live (e.g. via the
	// release_status_poll_cron) must not regress the row back to in_review.
	store := &fakeBuildEventStoreForViews{}
	taskID := uuid.New()
	rs := &fakeReleaseStore{release: &models.Release{
		ID:              uuid.New(),
		Status:          models.ReleaseStatusLive,
		CodemagicTaskID: &taskID,
	}}
	handler := newTestExpoReportsHandler(t, store, testHMACMaster).WithReleaseStore(rs)

	rr := postSubmitWebhook(t, handler, taskID.String(), "submitted", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d body=%s", rr.Code, rr.Body.String())
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if len(rs.inReviewCalls)+len(rs.rejectedCalls) != 0 {
		t.Errorf("terminal release must not be re-transitioned")
	}
}

func TestUpdateExpoBuildStatus_SubmitPhase_NilStoreSkipped(t *testing.T) {
	// Default handler (no WithReleaseStore) must not crash on submit-phase
	// events. The task projection still runs.
	store := &fakeBuildEventStoreForViews{}
	handler := newTestExpoReportsHandler(t, store, testHMACMaster) // no release store

	rr := postSubmitWebhook(t, handler, uuid.New().String(), "submitted", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d body=%s", rr.Code, rr.Body.String())
	}
}
