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
// HMAC scheme (mirrors codemagic_keystore_view.go):
//
//	per-call secret = HMAC-SHA256(HMACMaster, app_id) hex
//	signature       = "sha256=" + hex(HMAC-SHA256(per-call secret, "<ts>.<sha256(body)>"))
//
// We sign over sha256(body) rather than the raw body because the body is
// a multipart/form-data upload — signing the raw bytes would force the
// caller to canonicalise multipart boundaries deterministically. Hashing
// the body first sidesteps that and is just as secure (collision
// resistance of SHA-256).
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

	// Read full body up front so we can sign over its hash. 100 MiB
	// in-memory buffer is acceptable for the same reason expo-source
	// tarballs are: build-time only, single-tenant request.
	body, err := io.ReadAll(r.Body)
	if err != nil {
		utils.JSONError(w, "bad_request", "read body", http.StatusBadRequest, nil)
		return
	}

	// Validate URL appId vs header appId vs HMAC-derived appId.
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

	// Verify signature.
	sigHeader := strings.TrimSpace(r.Header.Get("X-Wemob-Signature"))
	tsHeader := strings.TrimSpace(r.Header.Get("X-Wemob-Timestamp"))
	if sigHeader == "" || tsHeader == "" {
		utils.JSONError(w, "unauthorized", "missing signature or timestamp", http.StatusUnauthorized, nil)
		return
	}
	ts, err := strconv.ParseInt(tsHeader, 10, 64)
	tsTime := time.Unix(ts, 0)
	if err != nil || time.Since(tsTime) > otaPublishTimestampSkew || time.Until(tsTime) > otaPublishTimestampSkew {
		utils.JSONError(w, "unauthorized", "stale timestamp", http.StatusUnauthorized, nil)
		return
	}
	if !verifyOTAPublishSignature(h.HMACMaster, hdrAppID, tsHeader, body, sigHeader) {
		utils.JSONError(w, "unauthorized", "bad signature", http.StatusUnauthorized, nil)
		return
	}

	// Parse multipart form. We pass the body buffer back through
	// http.NewRequest-flavoured ParseMultipartForm semantics manually
	// because we already consumed r.Body for hashing.
	r.Body = io.NopCloser(strings.NewReader(string(body)))
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
// expected per-call HMAC. Constant-time compare.
func verifyOTAPublishSignature(master, appID, ts string, body []byte, sigHeader string) bool {
	bodyHash := sha256.Sum256(body)
	bodyHashHex := hex.EncodeToString(bodyHash[:])

	h1 := hmac.New(sha256.New, []byte(master))
	h1.Write([]byte(appID))
	perCall := hex.EncodeToString(h1.Sum(nil))

	h2 := hmac.New(sha256.New, []byte(perCall))
	h2.Write([]byte(ts + "." + bodyHashHex))
	expected := "sha256=" + hex.EncodeToString(h2.Sum(nil))
	return hmac.Equal([]byte(sigHeader), []byte(expected))
}
