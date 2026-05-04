package views

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
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

const appleSigningTimestampSkew = 5 * time.Minute
const appleSigningMaxBody = 4 << 10 // 4 KiB; the body is just {project_id, timestamp}

// CodemagicAppleSigningHandler holds dependencies for POST
// /v2/codemagic/apple-signing — the HMAC-protected endpoint Codemagic
// workers hit at iOS build time to fetch cert + profile + private key
// (P1 #14, mobile pipeline audit). Same wire scheme as the OTA cert
// endpoint, just a different HKDF purpose.
type CodemagicAppleSigningHandler struct {
	Controller *controllers.Controller
	HMACMaster string
}

func NewCodemagicAppleSigningHandler(c *controllers.Controller, hmacMaster string) *CodemagicAppleSigningHandler {
	return &CodemagicAppleSigningHandler{Controller: c, HMACMaster: hmacMaster}
}

type fetchAppleSigningRequest struct {
	ProjectID string `json:"project_id"`
	Timestamp int64  `json:"timestamp"`
}

// fetchAppleSigningResponse returns the cert + profile + private key as
// base64. The worker assembles the .p12 locally with `openssl pkcs12
// -export -in cert.pem -inkey key.pem -out cert.p12 -password
// pass:$P12_PASSWORD`. We return a fresh per-call P12Password — bound to
// the HMAC-signed body so an intercepted password is useless without a
// fresh signed request.
type fetchAppleSigningResponse struct {
	CertPEMB64    string `json:"cert_pem_b64"`
	PrivKeyPEMB64 string `json:"priv_key_pem_b64"`
	ProfileB64    string `json:"profile_b64"`
	BundleID      string `json:"bundle_id"`
	P12Password   string `json:"p12_password"`
}

// FetchAppleSigning — POST /v2/codemagic/apple-signing.
//
// The handler:
//  1. Validates HMAC (purpose="apple-signing-fetch", scope=project_id).
//  2. Loads / lazily mints the per-project AppleSigningAssets row by
//     calling EnsureAppleSigningAssets with the project's default
//     AppleConnection.
//  3. Returns cert + key + profile + a fresh random P12 password.
func (h *CodemagicAppleSigningHandler) FetchAppleSigning(w http.ResponseWriter, r *http.Request) {
	body, projectID, ok := h.readSignedBody(w, r)
	if !ok {
		return
	}
	var req fetchAppleSigningRequest
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

	// Lazy ensure: load project + apple connection, then EnsureAppleSigningAssets.
	project, err := h.Controller.DB.GetAppProjectByID(appProjectID.String())
	if err != nil || project == nil {
		slog.Error("codemagic apple-signing: project lookup", "project_id", req.ProjectID, "err", err)
		utils.JSONError(w, "not_found", "project not found", http.StatusNotFound, nil)
		return
	}
	if project.DefaultAppleConnectionID == nil {
		utils.JSONError(w, "not_configured", "no default apple connection on project", http.StatusBadRequest, nil)
		return
	}
	conn, err := h.Controller.DB.GetAppleConnectionById(project.DefaultAppleConnectionID.String())
	if err != nil || conn == nil {
		slog.Error("codemagic apple-signing: connection lookup", "id", project.DefaultAppleConnectionID, "err", err)
		utils.JSONError(w, "not_configured", "apple connection not found", http.StatusBadRequest, nil)
		return
	}

	if _, err := h.Controller.EnsureAppleSigningAssets(r.Context(), project, conn); err != nil {
		if errors.Is(err, controllers.ErrAppleSigningNoConnection) {
			utils.JSONError(w, "not_configured", err.Error(), http.StatusBadRequest, nil)
			return
		}
		slog.Error("codemagic apple-signing: ensure failed", "project_id", req.ProjectID, "err", err)
		utils.JSONError(w, "internal", "apple signing ensure failed", http.StatusInternalServerError, nil)
		return
	}
	certPEM, profileBytes, privKeyPEM, err := h.Controller.LoadAppleSigningAssets(r.Context(), appProjectID)
	if err != nil {
		slog.Error("codemagic apple-signing: load failed", "project_id", req.ProjectID, "err", err)
		utils.JSONError(w, "internal", "apple signing load failed", http.StatusInternalServerError, nil)
		return
	}

	// Generate a fresh random P12 password — the worker uses it locally as
	// the export password for `openssl pkcs12 -export`. The password is
	// HMAC-bound (it sits in the response body, which is the same body the
	// worker just signed via HMAC), so an intercepted password without a
	// matching signed request is useless.
	pwBytes := make([]byte, 16)
	if _, err := rand.Read(pwBytes); err != nil {
		slog.Error("codemagic apple-signing: random pw", "err", err)
		utils.JSONError(w, "internal", "rand", http.StatusInternalServerError, nil)
		return
	}

	// Pull the bundle id from the row we just ensured.
	row, err := h.Controller.DB.GetAppleSigningAssets(r.Context(), appProjectID)
	if err != nil || row == nil {
		slog.Error("codemagic apple-signing: post-ensure load row", "project_id", req.ProjectID, "err", err)
		utils.JSONError(w, "internal", "post-ensure load failed", http.StatusInternalServerError, nil)
		return
	}

	utils.JSONResponse(w, fetchAppleSigningResponse{
		CertPEMB64:    base64.StdEncoding.EncodeToString(certPEM),
		PrivKeyPEMB64: base64.StdEncoding.EncodeToString(privKeyPEM),
		ProfileB64:    base64.StdEncoding.EncodeToString(profileBytes),
		BundleID:      row.BundleID,
		P12Password:   hex.EncodeToString(pwBytes),
	}, http.StatusOK)
}

// readSignedBody is identical to the OTA-cert handler's variant, just with
// "apple-signing-fetch" as the HKDF purpose.
func (h *CodemagicAppleSigningHandler) readSignedBody(w http.ResponseWriter, r *http.Request) ([]byte, string, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, appleSigningMaxBody)
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
	if err != nil || time.Since(tsTime) > appleSigningTimestampSkew || time.Until(tsTime) > appleSigningTimestampSkew {
		utils.JSONError(w, "unauthorized", "stale timestamp", http.StatusUnauthorized, nil)
		return nil, "", false
	}

	derived := utils.DeriveHMACKey([]byte(h.HMACMaster), "apple-signing-fetch", pidHeader)
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
