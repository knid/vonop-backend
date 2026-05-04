package views

import (
	"bytes"
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

	"github.com/knid/wemob/internal/utils"
)

const testKeystoreHMACMaster = "test-keystore-master-key-do-not-use-in-prod"

// signKeystoreTestRequest mirrors the Codemagic-side signing logic
// post-P0 #8b:
//
//	per-call secret = hex(HKDF(master, salt, info="<purpose>:<project_id>"))
//	signature       = "sha256=" + HMAC(per-call, "<ts>.<body>")
//
// purpose is "keystore-register" for /v2/codemagic/keystore and
// "keystore-fetch" for /v2/codemagic/keystore/fetch — different purposes
// so a leak of one doesn't compromise the other.
func signKeystoreTestRequest(t *testing.T, purpose, projectID, tsHeader, body string) string {
	t.Helper()
	derived := utils.DeriveHMACKey([]byte(testKeystoreHMACMaster), purpose, projectID)
	perCall := hex.EncodeToString(derived)
	h2 := hmac.New(sha256.New, []byte(perCall))
	h2.Write([]byte(tsHeader + "." + body))
	return "sha256=" + hex.EncodeToString(h2.Sum(nil))
}

// newTestKeystoreHandler returns a handler with a nil Controller. All tests
// here exercise only the readSignedBody path; happy-path tests would reach the
// controller call and panic on the nil pointer, so we restrict to bad-input
// cases that return before invoking the controller.
func newTestKeystoreHandler() *CodemagicKeystoreHandler {
	return NewCodemagicKeystoreHandler(nil, testKeystoreHMACMaster)
}

func TestRegisterKeystore_BadSignature(t *testing.T) {
	handler := newTestKeystoreHandler()

	projectID := uuid.New().String()
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	body := fmt.Sprintf(`{"project_id":%q,"keystore_b64":"YQ==","properties_b64":"Yg=="}`, projectID)

	req := httptest.NewRequest(http.MethodPost, "/v2/codemagic/keystore", bytes.NewBufferString(body))
	req.Header.Set("X-Wemob-Signature", "sha256=deadbeef")
	req.Header.Set("X-Wemob-Timestamp", ts)
	req.Header.Set("X-Wemob-Project-Id", projectID)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.RegisterKeystore(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestRegisterKeystore_StaleTimestamp(t *testing.T) {
	handler := newTestKeystoreHandler()

	projectID := uuid.New().String()
	staleTS := "1000" // way in the past
	body := fmt.Sprintf(`{"project_id":%q,"keystore_b64":"YQ==","properties_b64":"Yg=="}`, projectID)
	sig := signKeystoreTestRequest(t, "keystore-register", projectID, staleTS, body)

	req := httptest.NewRequest(http.MethodPost, "/v2/codemagic/keystore", bytes.NewBufferString(body))
	req.Header.Set("X-Wemob-Signature", sig)
	req.Header.Set("X-Wemob-Timestamp", staleTS)
	req.Header.Set("X-Wemob-Project-Id", projectID)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.RegisterKeystore(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for stale timestamp, got %d", rr.Code)
	}
}

func TestRegisterKeystore_FutureTimestamp(t *testing.T) {
	handler := newTestKeystoreHandler()

	projectID := uuid.New().String()
	futureTS := strconv.FormatInt(time.Now().Add(30*time.Minute).Unix(), 10)
	body := fmt.Sprintf(`{"project_id":%q,"keystore_b64":"YQ==","properties_b64":"Yg=="}`, projectID)
	sig := signKeystoreTestRequest(t, "keystore-register", projectID, futureTS, body)

	req := httptest.NewRequest(http.MethodPost, "/v2/codemagic/keystore", bytes.NewBufferString(body))
	req.Header.Set("X-Wemob-Signature", sig)
	req.Header.Set("X-Wemob-Timestamp", futureTS)
	req.Header.Set("X-Wemob-Project-Id", projectID)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.RegisterKeystore(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for future timestamp (+30min), got %d", rr.Code)
	}
}

func TestRegisterKeystore_MissingHeaders(t *testing.T) {
	handler := newTestKeystoreHandler()

	body := `{"project_id":"x","keystore_b64":"YQ==","properties_b64":"Yg=="}`
	req := httptest.NewRequest(http.MethodPost, "/v2/codemagic/keystore", bytes.NewBufferString(body))
	// Intentionally do not set X-Wemob-Signature / Timestamp / Project-Id.
	rr := httptest.NewRecorder()

	handler.RegisterKeystore(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for missing headers, got %d", rr.Code)
	}
}

// TestFetchKeystore_BadSignature verifies the FetchKeystore endpoint also
// enforces signature checking (smoke test — same readSignedBody path).
func TestFetchKeystore_BadSignature(t *testing.T) {
	handler := newTestKeystoreHandler()

	projectID := uuid.New().String()
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	body := fmt.Sprintf(`{"project_id":%q}`, projectID)

	req := httptest.NewRequest(http.MethodPost, "/v2/codemagic/keystore/fetch", bytes.NewBufferString(body))
	req.Header.Set("X-Wemob-Signature", "sha256=deadbeef")
	req.Header.Set("X-Wemob-Timestamp", ts)
	req.Header.Set("X-Wemob-Project-Id", projectID)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.FetchKeystore(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestRegisterKeystore_ProjectIDMismatch verifies that even with a valid
// signature, a body whose project_id does not match the X-Wemob-Project-Id
// header is rejected (defense against attacker swapping body fields under a
// signature minted for a different project_id).
func TestRegisterKeystore_ProjectIDMismatch(t *testing.T) {
	handler := newTestKeystoreHandler()

	signedProjectID := uuid.New().String()
	bodyProjectID := uuid.New().String() // different
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	body := fmt.Sprintf(`{"project_id":%q,"keystore_b64":"YQ==","properties_b64":"Yg=="}`, bodyProjectID)
	// Signature is valid for signedProjectID over this exact body.
	sig := signKeystoreTestRequest(t, "keystore-register", signedProjectID, ts, body)

	req := httptest.NewRequest(http.MethodPost, "/v2/codemagic/keystore", bytes.NewBufferString(body))
	req.Header.Set("X-Wemob-Signature", sig)
	req.Header.Set("X-Wemob-Timestamp", ts)
	req.Header.Set("X-Wemob-Project-Id", signedProjectID)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.RegisterKeystore(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for project_id mismatch, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestFetchKeystore_RejectsRegisterPurposeKey is the P0 #8b security
// property test: a signature minted with the "keystore-register" derived
// key MUST NOT verify on the FetchKeystore endpoint, which derives its
// per-call key with purpose="keystore-fetch". Without HKDF purpose
// separation, a leaked register key could fetch any project's keystore.
func TestFetchKeystore_RejectsRegisterPurposeKey(t *testing.T) {
	handler := newTestKeystoreHandler()

	projectID := uuid.New().String()
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	body := fmt.Sprintf(`{"project_id":%q}`, projectID)
	// Sign with purpose=keystore-register but POST to fetch endpoint.
	sig := signKeystoreTestRequest(t, "keystore-register", projectID, ts, body)

	req := httptest.NewRequest(http.MethodPost, "/v2/codemagic/keystore/fetch", bytes.NewBufferString(body))
	req.Header.Set("X-Wemob-Signature", sig)
	req.Header.Set("X-Wemob-Timestamp", ts)
	req.Header.Set("X-Wemob-Project-Id", projectID)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.FetchKeystore(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 (wrong-purpose key), got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestRegisterKeystore_RejectsFetchPurposeKey is the converse: a key for
// "keystore-fetch" must not pass the register endpoint's verifier.
func TestRegisterKeystore_RejectsFetchPurposeKey(t *testing.T) {
	handler := newTestKeystoreHandler()

	projectID := uuid.New().String()
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	body := fmt.Sprintf(`{"project_id":%q,"keystore_b64":"YQ==","properties_b64":"Yg=="}`, projectID)
	sig := signKeystoreTestRequest(t, "keystore-fetch", projectID, ts, body)

	req := httptest.NewRequest(http.MethodPost, "/v2/codemagic/keystore", bytes.NewBufferString(body))
	req.Header.Set("X-Wemob-Signature", sig)
	req.Header.Set("X-Wemob-Timestamp", ts)
	req.Header.Set("X-Wemob-Project-Id", projectID)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.RegisterKeystore(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 (wrong-purpose key), got %d: %s", rr.Code, rr.Body.String())
	}
}
