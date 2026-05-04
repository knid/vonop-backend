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

	"github.com/knid/wemob/internal/utils"
)

const testOTAHMACMaster = "test-ota-master-key-do-not-use-in-prod"

// signOTAPublishTestRequest mirrors the worker-side signing logic post-P0
// #8b: per-call key = hex(HKDF(master, salt, info="ota-publish:<app_id>"));
// signature = HMAC(per-call, "<ts>.<sha256(dist)>"). Returns the (sig,
// body-hash-hex) pair — both must go on the request.
func signOTAPublishTestRequest(t *testing.T, appID, ts string, distBytes []byte) (string, string) {
	t.Helper()
	bh := sha256.Sum256(distBytes)
	bodyHashHex := hex.EncodeToString(bh[:])
	derived := utils.DeriveHMACKey([]byte(testOTAHMACMaster), "ota-publish", appID)
	perCall := hex.EncodeToString(derived)
	h2 := hmac.New(sha256.New, []byte(perCall))
	h2.Write([]byte(ts + "." + bodyHashHex))
	return "sha256=" + hex.EncodeToString(h2.Sum(nil)), bodyHashHex
}

func newOTAPublishTestServer(t *testing.T) (*chi.Mux, *OTAPublishHandler) {
	t.Helper()
	h := NewOTAPublishHandler(nil, testOTAHMACMaster) // nil controller — auth-failure tests don't reach it
	r := chi.NewRouter()
	r.Post("/v2/apps/{appId}/ota/publish", h.PublishOTA)
	return r, h
}

// makeMinimalMultipartBody returns (body, contentType, distBytes). The
// test uses distBytes as input to the signing helper; body is what the
// HTTP request carries.
func makeMinimalMultipartBody(t *testing.T) ([]byte, string, []byte) {
	t.Helper()
	distBytes := []byte("not a real tarball")
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("runtime_version", "fingerprint:abc")
	_ = mw.WriteField("platform", "android")
	_ = mw.WriteField("timestamp", "1700000000")
	dw, err := mw.CreateFormFile("dist", "dist.tar.gz")
	if err != nil {
		t.Fatalf("create dist file: %v", err)
	}
	_, _ = dw.Write(distBytes)
	_ = mw.Close()
	return buf.Bytes(), mw.FormDataContentType(), distBytes
}

func TestPublishOTA_BadSignature(t *testing.T) {
	mux, _ := newOTAPublishTestServer(t)
	appID := uuid.New().String()
	body, ct, distBytes := makeMinimalMultipartBody(t)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	_, bodyHash := signOTAPublishTestRequest(t, appID, ts, distBytes)

	req := httptest.NewRequest(http.MethodPost, "/v2/apps/"+appID+"/ota/publish", bytes.NewReader(body))
	req.Header.Set("Content-Type", ct)
	req.Header.Set("X-Wemob-Signature", "sha256=deadbeef")
	req.Header.Set("X-Wemob-Timestamp", ts)
	req.Header.Set("X-Wemob-Body-Hash", bodyHash)
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
	body, ct, distBytes := makeMinimalMultipartBody(t)
	staleTS := "1000"
	sig, bodyHash := signOTAPublishTestRequest(t, appID, staleTS, distBytes)

	req := httptest.NewRequest(http.MethodPost, "/v2/apps/"+appID+"/ota/publish", bytes.NewReader(body))
	req.Header.Set("Content-Type", ct)
	req.Header.Set("X-Wemob-Signature", sig)
	req.Header.Set("X-Wemob-Timestamp", staleTS)
	req.Header.Set("X-Wemob-Body-Hash", bodyHash)
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
	body, ct, distBytes := makeMinimalMultipartBody(t)
	futureTS := strconv.FormatInt(time.Now().Add(30*time.Minute).Unix(), 10)
	sig, bodyHash := signOTAPublishTestRequest(t, appID, futureTS, distBytes)

	req := httptest.NewRequest(http.MethodPost, "/v2/apps/"+appID+"/ota/publish", bytes.NewReader(body))
	req.Header.Set("Content-Type", ct)
	req.Header.Set("X-Wemob-Signature", sig)
	req.Header.Set("X-Wemob-Timestamp", futureTS)
	req.Header.Set("X-Wemob-Body-Hash", bodyHash)
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
	body, ct, distBytes := makeMinimalMultipartBody(t)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	// Sign for hdrAppID — shouldn't matter, the bad-request check fires first.
	sig, bodyHash := signOTAPublishTestRequest(t, hdrAppID, ts, distBytes)

	req := httptest.NewRequest(http.MethodPost, "/v2/apps/"+urlAppID+"/ota/publish", bytes.NewReader(body))
	req.Header.Set("Content-Type", ct)
	req.Header.Set("X-Wemob-Signature", sig)
	req.Header.Set("X-Wemob-Timestamp", ts)
	req.Header.Set("X-Wemob-Body-Hash", bodyHash)
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
	body, ct, _ := makeMinimalMultipartBody(t)

	req := httptest.NewRequest(http.MethodPost, "/v2/apps/"+appID+"/ota/publish", bytes.NewReader(body))
	req.Header.Set("Content-Type", ct)
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
	body, ct, distBytes := makeMinimalMultipartBody(t)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig, bodyHash := signOTAPublishTestRequest(t, notAUUID, ts, distBytes)

	req := httptest.NewRequest(http.MethodPost, "/v2/apps/"+notAUUID+"/ota/publish", bytes.NewReader(body))
	req.Header.Set("Content-Type", ct)
	req.Header.Set("X-Wemob-Signature", sig)
	req.Header.Set("X-Wemob-Timestamp", ts)
	req.Header.Set("X-Wemob-Body-Hash", bodyHash)
	req.Header.Set(otaPublishHeaderApp, notAUUID)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid app id, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestPublishOTA_DistHashMismatch — even with a valid HMAC over the
// declared body hash, if the dist file in the multipart doesn't match
// that hash, reject. Defends against an attacker swapping the dist file
// after the signature was minted.
func TestPublishOTA_DistHashMismatch(t *testing.T) {
	mux, _ := newOTAPublishTestServer(t)
	appID := uuid.New().String()
	body, ct, _ := makeMinimalMultipartBody(t)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	// Sign over the hash of DIFFERENT bytes.
	sig, bodyHash := signOTAPublishTestRequest(t, appID, ts, []byte("different bytes"))

	req := httptest.NewRequest(http.MethodPost, "/v2/apps/"+appID+"/ota/publish", bytes.NewReader(body))
	req.Header.Set("Content-Type", ct)
	req.Header.Set("X-Wemob-Signature", sig)
	req.Header.Set("X-Wemob-Timestamp", ts)
	req.Header.Set("X-Wemob-Body-Hash", bodyHash) // valid for "different bytes"
	req.Header.Set(otaPublishHeaderApp, appID)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for dist hash mismatch, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "dist body hash mismatch") {
		t.Errorf("expected dist-hash-mismatch error, got %s", rr.Body.String())
	}
}
