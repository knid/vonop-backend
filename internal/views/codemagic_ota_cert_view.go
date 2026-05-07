package views

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/utils"
)

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
	body, projectID, ok := readSignedBodyByProjectID(w, r, h.HMACMaster, "ota-cert-fetch", otaCertMaxBody)
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
