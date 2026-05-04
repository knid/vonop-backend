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

	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/utils"
)

const keystoreTimestampSkew = 5 * time.Minute
const keystoreMaxBody = 16 << 20 // 16 MiB

// CodemagicKeystoreHandler holds the small dependency set for the two
// HMAC-signed keystore endpoints.
type CodemagicKeystoreHandler struct {
	Controller *controllers.Controller
	HMACMaster string
}

func NewCodemagicKeystoreHandler(c *controllers.Controller, hmacMaster string) *CodemagicKeystoreHandler {
	return &CodemagicKeystoreHandler{Controller: c, HMACMaster: hmacMaster}
}

type registerKeystoreRequest struct {
	ProjectID     string `json:"project_id"`
	KeystoreB64   string `json:"keystore_b64"`
	PropertiesB64 string `json:"properties_b64"`
}

type fetchKeystoreRequest struct {
	ProjectID string `json:"project_id"`
}

type fetchKeystoreResponse struct {
	KeystoreB64   string `json:"keystore_b64"`
	PropertiesB64 string `json:"properties_b64"`
}

// RegisterKeystore — POST /v2/codemagic/keystore.
//
// Per-call secret = HKDF-SHA256(HMACMaster,
//
//	salt="wemob-worker-hmac-v1", info="keystore-register:<project_id>")
//
// hex-encoded.
// Signature = "sha256=" + hex(HMAC-SHA256(per-call secret, "<ts>.<body>")).
//
// P0 #8b: separate purpose from FetchKeystore so a leaked register key
// can't be used to fetch (or vice versa).
func (h *CodemagicKeystoreHandler) RegisterKeystore(w http.ResponseWriter, r *http.Request) {
	body, projectID, ok := h.readSignedBody(w, r, "keystore-register")
	if !ok {
		return
	}
	var req registerKeystoreRequest
	if err := json.Unmarshal(body, &req); err != nil {
		utils.JSONError(w, "bad_request", "invalid json", http.StatusBadRequest, nil)
		return
	}
	if req.ProjectID != projectID {
		utils.JSONError(w, "bad_request", "project_id mismatch", http.StatusBadRequest, nil)
		return
	}
	if err := h.Controller.RegisterCodemagicKeystore(r.Context(), req.ProjectID, req.KeystoreB64, req.PropertiesB64); err != nil {
		// Caller errors (bad UUID, decode fail, empty blob) → 400 with the
		// specific message. Internal failures (KMS / S3 / DB) → 500 with a
		// generic message; the detail goes to logs only so we don't leak
		// infrastructure error strings to the HMAC client.
		if errors.Is(err, controllers.ErrKeystoreInvalidInput) {
			utils.JSONError(w, "bad_request", err.Error(), http.StatusBadRequest, nil)
			return
		}
		slog.Error("codemagic keystore register failed", "project_id", req.ProjectID, "err", err)
		utils.JSONError(w, "internal", "register keystore failed", http.StatusInternalServerError, nil)
		return
	}
	utils.JSONResponse(w, map[string]string{"status": "ok"}, http.StatusOK)
}

// FetchKeystore — POST /v2/codemagic/keystore/fetch.
//
// Per-call secret = HKDF-SHA256(HMACMaster,
//
//	salt="wemob-worker-hmac-v1", info="keystore-fetch:<project_id>")
//
// hex-encoded.
func (h *CodemagicKeystoreHandler) FetchKeystore(w http.ResponseWriter, r *http.Request) {
	body, projectID, ok := h.readSignedBody(w, r, "keystore-fetch")
	if !ok {
		return
	}
	var req fetchKeystoreRequest
	if err := json.Unmarshal(body, &req); err != nil {
		utils.JSONError(w, "bad_request", "invalid json", http.StatusBadRequest, nil)
		return
	}
	if req.ProjectID != projectID {
		utils.JSONError(w, "bad_request", "project_id mismatch", http.StatusBadRequest, nil)
		return
	}
	keystoreB64, propsB64, err := h.Controller.FetchCodemagicKeystore(r.Context(), req.ProjectID)
	if err != nil {
		switch {
		case errors.Is(err, controllers.ErrKeystoreNotRegistered):
			utils.JSONError(w, "not_found", "keystore not registered", http.StatusNotFound, nil)
		case errors.Is(err, controllers.ErrKeystoreInvalidInput):
			utils.JSONError(w, "bad_request", err.Error(), http.StatusBadRequest, nil)
		default:
			slog.Error("codemagic keystore fetch failed", "project_id", req.ProjectID, "err", err)
			utils.JSONError(w, "internal", "fetch keystore failed", http.StatusInternalServerError, nil)
		}
		return
	}
	utils.JSONResponse(w, fetchKeystoreResponse{KeystoreB64: keystoreB64, PropertiesB64: propsB64}, http.StatusOK)
}

// readSignedBody validates the HMAC signature and returns (body, projectID, ok).
//
// purpose is the HKDF info-string label for this call site:
//   - "keystore-register" for POST /v2/codemagic/keystore
//   - "keystore-fetch"    for POST /v2/codemagic/keystore/fetch
//
// Caller must compare projectID to the parsed body's project_id field to
// ensure the signed identity matches the requested resource (defends
// against a body that has a different project_id than the one in the
// header used to derive the per-call key).
func (h *CodemagicKeystoreHandler) readSignedBody(w http.ResponseWriter, r *http.Request, purpose string) ([]byte, string, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, keystoreMaxBody)
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
	if err != nil || time.Since(tsTime) > keystoreTimestampSkew || time.Until(tsTime) > keystoreTimestampSkew {
		utils.JSONError(w, "unauthorized", "stale timestamp", http.StatusUnauthorized, nil)
		return nil, "", false
	}

	// Per-call key = hex(HKDF(master, salt, info="<purpose>:<project_id>"))
	derived := utils.DeriveHMACKey([]byte(h.HMACMaster), purpose, pidHeader)
	perCallSecret := hex.EncodeToString(derived)

	// Expected sig = "sha256=" + hex(HMAC-SHA256(perCallSecret, "<ts>.<body>"))
	h2 := hmac.New(sha256.New, []byte(perCallSecret))
	h2.Write([]byte(tsHeader + "." + string(body)))
	expected := "sha256=" + hex.EncodeToString(h2.Sum(nil))

	if !hmac.Equal([]byte(sigHeader), []byte(expected)) {
		utils.JSONError(w, "unauthorized", "bad signature", http.StatusUnauthorized, nil)
		return nil, "", false
	}
	return body, pidHeader, true
}
