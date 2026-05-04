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


// fakeBuildEventStoreForViews is local to views tests.
type fakeBuildEventStoreForViews struct {
	mu     sync.Mutex
	events []*models.BuildEvent
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

// compile-time check that fakeBuildEventStoreForViews satisfies the interface
var _ database.BuildEventStore = (*fakeBuildEventStoreForViews)(nil)

func newTestExpoReportsHandler(t *testing.T, store database.BuildEventStore, hmacKey string) *ExpoReportsHandler {
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
