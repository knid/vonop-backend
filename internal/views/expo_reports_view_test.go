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
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
)

const testHMACMaster = "test-master-key-do-not-use-in-prod"

// signTestBody mirrors the Codemagic signing logic.
func signTestBody(t *testing.T, taskID, body string) (sig string) {
	t.Helper()
	h := hmac.New(sha256.New, []byte(testHMACMaster))
	h.Write([]byte(taskID))
	buildSecret := hex.EncodeToString(h.Sum(nil))
	h2 := hmac.New(sha256.New, []byte(buildSecret))
	h2.Write([]byte(body))
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
	body := fmt.Sprintf(`{"task_id":%q,"status":"building","message":"go","timestamp":%d}`, taskID, time.Now().Unix())
	sig := signTestBody(t, taskID, body)
	ts := strconv.FormatInt(time.Now().Unix(), 10)

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

	body := `{"task_id":"task-123","status":"building","timestamp":` + strconv.FormatInt(time.Now().Unix(), 10) + `}`
	req := httptest.NewRequest(http.MethodPost, "/v2/reports/expo-build", bytes.NewBufferString(body))
	req.Header.Set("X-Wemob-Signature", "sha256=deadbeef")
	req.Header.Set("X-Wemob-Timestamp", strconv.FormatInt(time.Now().Unix(), 10))
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
	body := fmt.Sprintf(`{"task_id":%q,"status":"built","timestamp":1000}`, taskID)
	sig := signTestBody(t, taskID, body)

	req := httptest.NewRequest(http.MethodPost, "/v2/reports/expo-build", bytes.NewBufferString(body))
	req.Header.Set("X-Wemob-Signature", sig)
	req.Header.Set("X-Wemob-Timestamp", "1000") // way in the past
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
	body := fmt.Sprintf(`{"task_id":%q,"status":"built","message":"done","timestamp":%d}`, taskID, time.Now().Unix())
	sig := signTestBody(t, taskID, body)
	ts := strconv.FormatInt(time.Now().Unix(), 10)

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
