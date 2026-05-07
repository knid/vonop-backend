package views

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/utils"
)

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
	body, projectID, ok := readSignedBodyByProjectID(w, r, h.HMACMaster, "keystore-register", keystoreMaxBody)
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
	body, projectID, ok := readSignedBodyByProjectID(w, r, h.HMACMaster, "keystore-fetch", keystoreMaxBody)
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
