package views

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/knid/wemob/internal/utils"
)

// hmacTimestampSkew is the ±window allowed between request timestamp and
// server clock. Set tight (5 min) since legitimate Codemagic-side timestamps
// are seconds-old; only NTP drift on either side eats into the allowance.
const hmacTimestampSkew = 5 * time.Minute

// readBodyWithLimit caps the request body and reads it fully. On failure
// writes a 400 and returns ok=false. Pass maxBody=0 to skip the limit.
func readBodyWithLimit(w http.ResponseWriter, r *http.Request, maxBody int64) ([]byte, bool) {
	if maxBody > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		utils.JSONError(w, "bad_request", "read body", http.StatusBadRequest, nil)
		return nil, false
	}
	return body, true
}

// verifyHMACSignature checks the request's HMAC signature against the given
// body and per-call scope. Per-call secret =
// hex(HKDF(master, salt="wemob-worker-hmac-v1", info="<purpose>:<scope>"));
// expected sig = "sha256=" + hex(HMAC-SHA256(per-call, "<ts>.<body>")).
//
// Two scope sources exist in the codebase: project-id callers read
// X-Wemob-Project-Id (use readSignedBodyByProjectID); the build webhook
// parses task_id out of the body first, then calls this directly.
//
// On failure writes the appropriate 401 and returns false.
func verifyHMACSignature(w http.ResponseWriter, r *http.Request, body []byte, master, purpose, scope string) bool {
	sigHeader := strings.TrimSpace(r.Header.Get("X-Wemob-Signature"))
	tsHeader := strings.TrimSpace(r.Header.Get("X-Wemob-Timestamp"))
	if sigHeader == "" || tsHeader == "" {
		utils.JSONError(w, "unauthorized", "missing signature or timestamp", http.StatusUnauthorized, nil)
		return false
	}

	ts, err := strconv.ParseInt(tsHeader, 10, 64)
	tsTime := time.Unix(ts, 0)
	// Reject stale and future-dated timestamps to defeat replay and
	// forward-timestamp attacks.
	if err != nil || time.Since(tsTime) > hmacTimestampSkew || time.Until(tsTime) > hmacTimestampSkew {
		utils.JSONError(w, "unauthorized", "stale timestamp", http.StatusUnauthorized, nil)
		return false
	}

	derived := utils.DeriveHMACKey([]byte(master), purpose, scope)
	perCallSecret := hex.EncodeToString(derived)

	mac := hmac.New(sha256.New, []byte(perCallSecret))
	mac.Write([]byte(tsHeader + "." + string(body)))
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(sigHeader), []byte(expected)) {
		utils.JSONError(w, "unauthorized", "bad signature", http.StatusUnauthorized, nil)
		return false
	}
	return true
}

// readSignedBodyByProjectID is the project-scoped wire format used by the
// codemagic-worker callbacks (keystore register/fetch, OTA cert fetch,
// apple-signing fetch). Reads X-Wemob-Project-Id, then reads + verifies
// the body. Callers must still compare the returned projectID against any
// project_id field embedded in the body to prevent header/body substitution.
func readSignedBodyByProjectID(w http.ResponseWriter, r *http.Request, master, purpose string, maxBody int64) (body []byte, projectID string, ok bool) {
	pid := strings.TrimSpace(r.Header.Get("X-Wemob-Project-Id"))
	if pid == "" {
		utils.JSONError(w, "unauthorized", "missing project id header", http.StatusUnauthorized, nil)
		return nil, "", false
	}
	body, ok = readBodyWithLimit(w, r, maxBody)
	if !ok {
		return nil, "", false
	}
	if !verifyHMACSignature(w, r, body, master, purpose, pid) {
		return nil, "", false
	}
	return body, pid, true
}
