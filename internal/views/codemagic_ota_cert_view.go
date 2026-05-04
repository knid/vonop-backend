package views

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/utils"
)

const otaCertTimestampSkew = 5 * time.Minute
const otaCertMaxBody = 4 << 10 // 4 KiB; this is a tiny GET-with-body

// CodemagicOTACertHandler holds dependencies for POST /v2/codemagic/ota-cert.
//
// HMAC scheme mirrors the keystore endpoints exactly:
//
//	per-call secret = hex(HKDF-SHA256(master, salt="wemob-worker-hmac-v1",
//	                       info="ota-cert-fetch:<project_id>"))
//	sig             = "sha256=" + hex(HMAC(per-call, "<ts>.<body>"))
//
// Body is the JSON request including project_id (which we also require in
// the X-Wemob-Project-Id header for the per-call key derivation). P0 #8b
// (mobile audit, 2026-05-04): "ota-cert-fetch" is a distinct purpose
// from keystore-fetch / keystore-register / build-webhook / ota-publish.
//
// Returns the public X.509 certificate PEM + the keyid the binary's
// codeSigningMetadata needs. Codemagic writes the cert into
// certs/ota-cert.pem and rewrites app.json's expo.updates.codeSigningMetadata
// before `expo prebuild`.
type CodemagicOTACertHandler struct {
	Controller *controllers.Controller
	HMACMaster string
}

func NewCodemagicOTACertHandler(c *controllers.Controller, hmacMaster string) *CodemagicOTACertHandler {
	return &CodemagicOTACertHandler{Controller: c, HMACMaster: hmacMaster}
}

type fetchOTACertRequest struct {
	ProjectID string `json:"project_id"`
}

type fetchOTACertResponse struct {
	CertPEM   string `json:"cert_pem"`
	KeyID     string `json:"key_id"`
	Algorithm string `json:"algorithm"`
}

// FetchOTACert — POST /v2/codemagic/ota-cert.
//
// Lazily generates the per-project signing key (same path as the manifest
// view) so a Codemagic build can run before any OTA publish has happened —
// the cert needs to be present in the binary before any signed manifest
// arrives.
func (h *CodemagicOTACertHandler) FetchOTACert(w http.ResponseWriter, r *http.Request) {
	body, projectID, ok := h.readSignedBody(w, r)
	if !ok {
		return
	}
	var req fetchOTACertRequest
	if err := json.Unmarshal(body, &req); err != nil {
		utils.JSONError(w, "bad_request", "invalid json", http.StatusBadRequest, nil)
		return
	}
	if req.ProjectID != projectID {
		utils.JSONError(w, "bad_request", "project_id mismatch", http.StatusBadRequest, nil)
		return
	}
	appProjectID, err := uuid.Parse(req.ProjectID)
	if err != nil {
		utils.JSONError(w, "bad_request", "invalid project id", http.StatusBadRequest, nil)
		return
	}
	row, err := h.Controller.EnsureOTAUpdateSigningKey(r.Context(), appProjectID)
	if err != nil {
		// Caller errors (project missing org, bad UUID) → 400 with detail;
		// internal failures (CredStore down, DB error) → 500 with generic
		// message + log only.
		if errors.Is(err, controllers.ErrKeystoreInvalidInput) {
			utils.JSONError(w, "bad_request", err.Error(), http.StatusBadRequest, nil)
			return
		}
		slog.Error("codemagic ota-cert fetch failed", "project_id", req.ProjectID, "err", err)
		utils.JSONError(w, "internal", "ota cert fetch failed", http.StatusInternalServerError, nil)
		return
	}
	utils.JSONResponse(w, fetchOTACertResponse{
		CertPEM:   row.CertificatePEM,
		KeyID:     row.KeyID,
		Algorithm: row.Algorithm,
	}, http.StatusOK)
}

// readSignedBody validates the HMAC signature and returns (body, projectID, ok).
// Identical to CodemagicKeystoreHandler.readSignedBody — duplicated rather
// than shared because each handler holds its own HMACMaster wiring.
func (h *CodemagicOTACertHandler) readSignedBody(w http.ResponseWriter, r *http.Request) ([]byte, string, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, otaCertMaxBody)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		utils.JSONError(w, "bad_request", "read body", http.StatusBadRequest, nil)
		return nil, "", false
	}

	sigHeader := strings.TrimSpace(r.Header.Get("X-Wemob-Signature"))
	tsHeader := strings.TrimSpace(r.Header.Get("X-Wemob-Timestamp"))
	pidHeader := strings.TrimSpace(r.Header.Get("X-Wemob-Project-Id"))
	if sigHeader == "" || tsHeader == "" || pidHeader == "" {
		utils.JSONError(w, "unauthorized", "missing signature, timestamp, or project id header", http.StatusUnauthorized, nil)
		return nil, "", false
	}

	ts, err := strconv.ParseInt(tsHeader, 10, 64)
	tsTime := time.Unix(ts, 0)
	if err != nil || time.Since(tsTime) > otaCertTimestampSkew || time.Until(tsTime) > otaCertTimestampSkew {
		utils.JSONError(w, "unauthorized", "stale timestamp", http.StatusUnauthorized, nil)
		return nil, "", false
	}

	// Per-call key = hex(HKDF(master, salt, info="ota-cert-fetch:<project_id>")).
	derived := utils.DeriveHMACKey([]byte(h.HMACMaster), "ota-cert-fetch", pidHeader)
	perCallSecret := hex.EncodeToString(derived)

	h2 := hmac.New(sha256.New, []byte(perCallSecret))
	h2.Write([]byte(tsHeader + "." + string(body)))
	expected := "sha256=" + hex.EncodeToString(h2.Sum(nil))

	if !hmac.Equal([]byte(sigHeader), []byte(expected)) {
		utils.JSONError(w, "unauthorized", "bad signature", http.StatusUnauthorized, nil)
		return nil, "", false
	}
	return body, pidHeader, true
}
