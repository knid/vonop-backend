package views

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const testOTAHMACMaster = "test-ota-master-key-do-not-use-in-prod"

// signOTAPublishTestRequest mirrors the worker-side signing logic.
func signOTAPublishTestRequest(t *testing.T, appID, ts string, body []byte) string {
	t.Helper()
	bh := sha256.Sum256(body)
	h1 := hmac.New(sha256.New, []byte(testOTAHMACMaster))
	h1.Write([]byte(appID))
	perCall := hex.EncodeToString(h1.Sum(nil))
	h2 := hmac.New(sha256.New, []byte(perCall))
	h2.Write([]byte(ts + "." + hex.EncodeToString(bh[:])))
	return "sha256=" + hex.EncodeToString(h2.Sum(nil))
}

func newOTAPublishTestServer(t *testing.T) (*chi.Mux, *OTAPublishHandler) {
	t.Helper()
	h := NewOTAPublishHandler(nil, testOTAHMACMaster) // nil controller — auth-failure tests don't reach it
	r := chi.NewRouter()
	r.Post("/v2/apps/{appId}/ota/publish", h.PublishOTA)
	return r, h
}

func makeMinimalMultipartBody(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("runtime_version", "fingerprint:abc")
	_ = mw.WriteField("platform", "android")
	_ = mw.WriteField("timestamp", "1700000000")
	dw, err := mw.CreateFormFile("dist", "dist.tar.gz")
	if err != nil {
		t.Fatalf("create dist file: %v", err)
	}
	_, _ = dw.Write([]byte("not a real tarball"))
	_ = mw.Close()
	return buf.Bytes()
}

func TestPublishOTA_BadSignature(t *testing.T) {
	mux, _ := newOTAPublishTestServer(t)
	appID := uuid.New().String()
	body := makeMinimalMultipartBody(t)
	ts := strconv.FormatInt(time.Now().Unix(), 10)

	req := httptest.NewRequest(http.MethodPost, "/v2/apps/"+appID+"/ota/publish", bytes.NewReader(body))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=----not-real")
	req.Header.Set("X-Wemob-Signature", "sha256=deadbeef")
	req.Header.Set("X-Wemob-Timestamp", ts)
	req.Header.Set(otaPublishHeaderApp, appID)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestPublishOTA_StaleTimestamp(t *testing.T) {
	mux, _ := newOTAPublishTestServer(t)
	appID := uuid.New().String()
	body := makeMinimalMultipartBody(t)
	staleTS := "1000"
	sig := signOTAPublishTestRequest(t, appID, staleTS, body)

	req := httptest.NewRequest(http.MethodPost, "/v2/apps/"+appID+"/ota/publish", bytes.NewReader(body))
	req.Header.Set("X-Wemob-Signature", sig)
	req.Header.Set("X-Wemob-Timestamp", staleTS)
	req.Header.Set(otaPublishHeaderApp, appID)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for stale timestamp, got %d", rr.Code)
	}
}

func TestPublishOTA_FutureTimestamp(t *testing.T) {
	mux, _ := newOTAPublishTestServer(t)
	appID := uuid.New().String()
	body := makeMinimalMultipartBody(t)
	futureTS := strconv.FormatInt(time.Now().Add(30*time.Minute).Unix(), 10)
	sig := signOTAPublishTestRequest(t, appID, futureTS, body)

	req := httptest.NewRequest(http.MethodPost, "/v2/apps/"+appID+"/ota/publish", bytes.NewReader(body))
	req.Header.Set("X-Wemob-Signature", sig)
	req.Header.Set("X-Wemob-Timestamp", futureTS)
	req.Header.Set(otaPublishHeaderApp, appID)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for future timestamp, got %d", rr.Code)
	}
}

func TestPublishOTA_AppIDMismatch(t *testing.T) {
	mux, _ := newOTAPublishTestServer(t)
	urlAppID := uuid.New().String()
	hdrAppID := uuid.New().String() // different
	body := makeMinimalMultipartBody(t)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	// Sign for hdrAppID — shouldn't matter, the bad-request check fires first.
	sig := signOTAPublishTestRequest(t, hdrAppID, ts, body)

	req := httptest.NewRequest(http.MethodPost, "/v2/apps/"+urlAppID+"/ota/publish", bytes.NewReader(body))
	req.Header.Set("X-Wemob-Signature", sig)
	req.Header.Set("X-Wemob-Timestamp", ts)
	req.Header.Set(otaPublishHeaderApp, hdrAppID)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for url/header app id mismatch, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "mismatch") {
		t.Errorf("expected mismatch error in body, got %s", rr.Body.String())
	}
}

func TestPublishOTA_MissingHeaders(t *testing.T) {
	mux, _ := newOTAPublishTestServer(t)
	appID := uuid.New().String()
	body := makeMinimalMultipartBody(t)

	req := httptest.NewRequest(http.MethodPost, "/v2/apps/"+appID+"/ota/publish", bytes.NewReader(body))
	// No X-Wemob-* headers + no X-Wemob-AppProject-Id.
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	// Missing header app id is the FIRST check, so we get 400 before HMAC.
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing header, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestPublishOTA_InvalidAppIDFormat(t *testing.T) {
	mux, _ := newOTAPublishTestServer(t)
	notAUUID := "not-a-uuid"
	body := makeMinimalMultipartBody(t)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := signOTAPublishTestRequest(t, notAUUID, ts, body)

	req := httptest.NewRequest(http.MethodPost, "/v2/apps/"+notAUUID+"/ota/publish", bytes.NewReader(body))
	req.Header.Set("X-Wemob-Signature", sig)
	req.Header.Set("X-Wemob-Timestamp", ts)
	req.Header.Set(otaPublishHeaderApp, notAUUID)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid app id, got %d: %s", rr.Code, rr.Body.String())
	}
}
