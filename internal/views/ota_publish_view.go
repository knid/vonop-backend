package views

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/utils"
)

const (
	otaPublishTimestampSkew = 5 * time.Minute
	otaPublishMaxBody       = 100 << 20 // 100 MiB
	otaPublishHeaderApp     = "X-Wemob-AppProject-Id"
)

// OTAPublishHandler holds dependencies for POST /v2/apps/{appId}/ota/publish.
//
// HMAC scheme:
//
//	per-call secret = HMAC-SHA256(HMACMaster, app_id) hex
//	signature       = "sha256=" + hex(HMAC-SHA256(per-call secret, "<ts>.<sha256(dist.tar.gz bytes)>"))
//
// We sign over sha256(dist.tar.gz) rather than the raw multipart body
// because curl chooses the multipart boundary, which the client can't
// pre-compute. The body hash header is verified separately:
//
//   - the X-Wemob-Body-Hash header carries hex(sha256(dist.tar.gz)) — what
//     the client signed.
//   - after parsing the multipart, the server re-hashes the dist file's
//     bytes and checks they match X-Wemob-Body-Hash. If they don't, the
//     attacker swapped the file under a valid signature for a different
//     file → reject.
type OTAPublishHandler struct {
	Controller *controllers.Controller
	HMACMaster string
}

func NewOTAPublishHandler(c *controllers.Controller, hmacMaster string) *OTAPublishHandler {
	return &OTAPublishHandler{Controller: c, HMACMaster: hmacMaster}
}

// PublishOTA — POST /v2/apps/{appId}/ota/publish.
func (h *OTAPublishHandler) PublishOTA(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, otaPublishMaxBody)

	// Validate URL appId vs header appId.
	urlAppID := chi.URLParam(r, "appId")
	hdrAppID := strings.TrimSpace(r.Header.Get(otaPublishHeaderApp))
	if urlAppID == "" || hdrAppID == "" || urlAppID != hdrAppID {
		utils.JSONError(w, "bad_request", "url/header app id mismatch", http.StatusBadRequest, nil)
		return
	}
	appProjectID, err := uuid.Parse(urlAppID)
	if err != nil {
		utils.JSONError(w, "bad_request", "invalid app id", http.StatusBadRequest, nil)
		return
	}

	// Header signature inputs.
	sigHeader := strings.TrimSpace(r.Header.Get("X-Wemob-Signature"))
	tsHeader := strings.TrimSpace(r.Header.Get("X-Wemob-Timestamp"))
	bodyHashHeader := strings.TrimSpace(r.Header.Get("X-Wemob-Body-Hash"))
	if sigHeader == "" || tsHeader == "" || bodyHashHeader == "" {
		utils.JSONError(w, "unauthorized", "missing signature, timestamp, or body hash header", http.StatusUnauthorized, nil)
		return
	}
	ts, err := strconv.ParseInt(tsHeader, 10, 64)
	tsTime := time.Unix(ts, 0)
	if err != nil || time.Since(tsTime) > otaPublishTimestampSkew || time.Until(tsTime) > otaPublishTimestampSkew {
		utils.JSONError(w, "unauthorized", "stale timestamp", http.StatusUnauthorized, nil)
		return
	}
	// Verify HMAC over <ts>.<bodyHashHeader>. The body hash here is the
	// hash of the dist.tar.gz the client signed; we'll re-verify it
	// matches the dist file the multipart form actually contains
	// AFTER parsing.
	if !verifyOTAPublishSignature(h.HMACMaster, hdrAppID, tsHeader, bodyHashHeader, sigHeader) {
		utils.JSONError(w, "unauthorized", "bad signature", http.StatusUnauthorized, nil)
		return
	}

	if err := r.ParseMultipartForm(otaPublishMaxBody); err != nil {
		utils.JSONError(w, "bad_request", "parse multipart: "+err.Error(), http.StatusBadRequest, nil)
		return
	}

	runtimeVersion := r.FormValue("runtime_version")
	platform := r.FormValue("platform")
	timestampStr := r.FormValue("timestamp")
	if runtimeVersion == "" || platform == "" || timestampStr == "" {
		utils.JSONError(w, "bad_request", "missing form fields", http.StatusBadRequest, nil)
		return
	}
	timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil {
		utils.JSONError(w, "bad_request", "invalid timestamp form value", http.StatusBadRequest, nil)
		return
	}

	distFile, _, err := r.FormFile("dist")
	if err != nil {
		utils.JSONError(w, "bad_request", "missing dist file: "+err.Error(), http.StatusBadRequest, nil)
		return
	}
	defer distFile.Close()
	distBytes, err := io.ReadAll(distFile)
	if err != nil {
		utils.JSONError(w, "bad_request", "read dist", http.StatusBadRequest, nil)
		return
	}

	// Verify the dist file's hash matches what the client signed. This
	// closes the gap where the HMAC signature only covers the body-hash
	// header — without this check, an attacker who could inject a
	// different multipart payload (impossible over TLS but defense in
	// depth) could swap the dist file without the signature catching it.
	gotHash := sha256.Sum256(distBytes)
	gotHashHex := hex.EncodeToString(gotHash[:])
	if !hmac.Equal([]byte(gotHashHex), []byte(strings.ToLower(bodyHashHeader))) {
		utils.JSONError(w, "unauthorized", "dist body hash mismatch", http.StatusUnauthorized, nil)
		return
	}

	// expo_config is optional but recommended — empty bytes are fine, the
	// manifest builder treats it as no extra.expoClient.
	var expoConfigBytes []byte
	if expoConfigFile, _, ferr := r.FormFile("expo_config"); ferr == nil {
		defer expoConfigFile.Close()
		expoConfigBytes, _ = io.ReadAll(expoConfigFile)
	}

	update, err := h.Controller.PublishOTAUpdate(r.Context(), appProjectID, runtimeVersion, platform, timestamp, distBytes, expoConfigBytes)
	if err != nil {
		switch {
		case errors.Is(err, controllers.ErrOTABadTarball),
			errors.Is(err, controllers.ErrOTAMetadataShape),
			errors.Is(err, controllers.ErrOTAInvalidPlatform):
			utils.JSONError(w, "bad_request", err.Error(), http.StatusBadRequest, nil)
		default:
			slog.Error("ota publish failed", "app_project_id", appProjectID, "err", err)
			utils.JSONError(w, "internal", "publish failed", http.StatusInternalServerError, nil)
		}
		return
	}

	utils.JSONResponse(w, map[string]any{
		"update_id":    update.UpdateID.String(),
		"manifest_url": fmt.Sprintf("/v2/apps/%s/ota/manifest", appProjectID),
		"runtime_version": update.RuntimeVersion,
		"platform":     update.Platform,
	}, http.StatusOK)
}

// verifyOTAPublishSignature returns true when sigHeader matches the
// expected per-call HMAC over "<ts>.<bodyHashHex>". Constant-time compare.
//
// bodyHashHex is the hex-encoded SHA-256 of the dist.tar.gz file the
// client uploaded — it's a header value (X-Wemob-Body-Hash) the client
// sends and the server cross-checks against the actually-received dist
// file post-multipart-parsing.
func verifyOTAPublishSignature(master, appID, ts, bodyHashHex, sigHeader string) bool {
	h1 := hmac.New(sha256.New, []byte(master))
	h1.Write([]byte(appID))
	perCall := hex.EncodeToString(h1.Sum(nil))

	h2 := hmac.New(sha256.New, []byte(perCall))
	h2.Write([]byte(ts + "." + strings.ToLower(bodyHashHex)))
	expected := "sha256=" + hex.EncodeToString(h2.Sum(nil))
	return hmac.Equal([]byte(sigHeader), []byte(expected))
}
