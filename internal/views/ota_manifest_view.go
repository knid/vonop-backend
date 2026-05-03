package views

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/services/ota"
	"github.com/knid/wemob/internal/utils"
)

// OTAManifestHandler holds dependencies for the un-authenticated mobile-
// client-facing endpoints:
//
//	GET /v2/apps/{appId}/ota/manifest
//	GET /v2/apps/{appId}/ota/assets
//
// The mobile client has no backend session — these are public reads. The
// manifest + asset bytes are not secrets (they're served from S3 anyway).
type OTAManifestHandler struct {
	Controller *controllers.Controller
}

func NewOTAManifestHandler(c *controllers.Controller) *OTAManifestHandler {
	return &OTAManifestHandler{Controller: c}
}

// GetManifest — GET /v2/apps/{appId}/ota/manifest.
//
// Required headers from the expo-updates client (see
// docs.expo.dev/technical-specs/expo-updates-1):
//
//	expo-platform           ios | android
//	expo-runtime-version    e.g. fingerprint:abc123...
//
// Optional:
//
//	expo-protocol-version    0 | 1 (defaults 0)
//	expo-current-update-id   client compares to manifest.id; we always
//	                         return latest, client no-ops on equality
//	expo-expect-signature    we don't yet sign — log a warn + return
//	                         unsigned manifest (client likely rejects but
//	                         doesn't crash)
func (h *OTAManifestHandler) GetManifest(w http.ResponseWriter, r *http.Request) {
	appIDStr := chi.URLParam(r, "appId")
	appProjectID, err := uuid.Parse(appIDStr)
	if err != nil {
		utils.JSONError(w, "bad_request", "invalid app id", http.StatusBadRequest, nil)
		return
	}

	platform := strings.ToLower(strings.TrimSpace(r.Header.Get("expo-platform")))
	runtimeVersion := strings.TrimSpace(r.Header.Get("expo-runtime-version"))
	if platform == "" || runtimeVersion == "" {
		utils.JSONError(w, "bad_request", "missing expo-platform or expo-runtime-version", http.StatusBadRequest, nil)
		return
	}
	if platform != "ios" && platform != "android" {
		utils.JSONError(w, "bad_request", "unsupported platform", http.StatusBadRequest, nil)
		return
	}

	if r.Header.Get("expo-expect-signature") != "" {
		// V1 ships without manifest signing. Client may reject the
		// unsigned response. Log so we have a metric we can graph later.
		slog.Warn("ota: client requested signed manifest but signing is unimplemented (P2.1.B)",
			"app_project_id", appProjectID, "platform", platform, "runtime_version", runtimeVersion)
	}

	update, err := h.Controller.GetLatestOTAManifest(r.Context(), appProjectID, runtimeVersion, platform)
	if err != nil {
		switch {
		case errors.Is(err, controllers.ErrOTANotFound):
			utils.JSONError(w, "not_found", "no update for runtime version", http.StatusNotFound, nil)
		case errors.Is(err, controllers.ErrOTAInvalidPlatform):
			utils.JSONError(w, "bad_request", "invalid platform", http.StatusBadRequest, nil)
		default:
			slog.Error("ota manifest lookup failed", "app_project_id", appProjectID, "err", err)
			utils.JSONError(w, "internal", "manifest lookup failed", http.StatusInternalServerError, nil)
		}
		return
	}

	metadata, err := ota.ParseMetadataJSON(update.MetadataJSON)
	if err != nil {
		slog.Error("ota: stored metadata.json invalid", "update_id", update.ID, "err", err)
		utils.JSONError(w, "internal", "manifest construction failed", http.StatusInternalServerError, nil)
		return
	}

	assetBytes, err := h.Controller.FetchOTAManifestAssets(r.Context(), update, metadata)
	if err != nil {
		slog.Error("ota: fetch manifest assets", "update_id", update.ID, "err", err)
		utils.JSONError(w, "internal", "manifest construction failed", http.StatusInternalServerError, nil)
		return
	}

	baseURL := h.manifestBaseURL(r, appProjectID)
	manifest, err := ota.BuildManifest(update, metadata, assetBytes, baseURL)
	if err != nil {
		slog.Error("ota: build manifest", "update_id", update.ID, "err", err)
		utils.JSONError(w, "internal", "manifest construction failed", http.StatusInternalServerError, nil)
		return
	}

	body, boundary, err := ota.MarshalMultipart(manifest)
	if err != nil {
		slog.Error("ota: marshal multipart", "update_id", update.ID, "err", err)
		utils.JSONError(w, "internal", "manifest construction failed", http.StatusInternalServerError, nil)
		return
	}

	w.Header().Set("Content-Type", "multipart/mixed; boundary="+boundary)
	w.Header().Set("expo-protocol-version", "0")
	w.Header().Set("expo-sfv-version", "0")
	w.Header().Set("cache-control", "private, max-age=0")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// GetAsset — GET /v2/apps/{appId}/ota/assets?asset=<key>&runtimeVersion=<rv>&platform=<p>.
//
// Streams a single asset from S3 with the appropriate Content-Type.
// Validates the asset path (no '..', no leading '/') AND that the OTA
// update for (appProjectID, rv, platform) actually owns that path.
func (h *OTAManifestHandler) GetAsset(w http.ResponseWriter, r *http.Request) {
	appIDStr := chi.URLParam(r, "appId")
	appProjectID, err := uuid.Parse(appIDStr)
	if err != nil {
		utils.JSONError(w, "bad_request", "invalid app id", http.StatusBadRequest, nil)
		return
	}
	q := r.URL.Query()
	assetKey := q.Get("asset")
	runtimeVersion := q.Get("runtimeVersion")
	platform := strings.ToLower(q.Get("platform"))
	if assetKey == "" || runtimeVersion == "" || platform == "" {
		utils.JSONError(w, "bad_request", "missing query params (asset, runtimeVersion, platform)", http.StatusBadRequest, nil)
		return
	}

	body, contentType, err := h.Controller.GetOTAAsset(r.Context(), appProjectID, runtimeVersion, platform, assetKey)
	if err != nil {
		switch {
		case errors.Is(err, controllers.ErrOTANotFound):
			utils.JSONError(w, "not_found", "no update for runtime version", http.StatusNotFound, nil)
		case errors.Is(err, controllers.ErrOTAInvalidPlatform):
			utils.JSONError(w, "bad_request", "invalid platform", http.StatusBadRequest, nil)
		default:
			// Path validation and S3 not-found errors share this branch.
			// 400 covers both — the client sent us something bad.
			utils.JSONError(w, "bad_request", err.Error(), http.StatusBadRequest, nil)
		}
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, strings.NewReader(string(body)))
}

// manifestBaseURL returns the absolute URL prefix used in the manifest's
// asset URLs. We use the Host header from the incoming request so the
// manifest URL matches whatever the mobile client used to reach us
// (works through proxies, custom domains, etc.).
func (h *OTAManifestHandler) manifestBaseURL(r *http.Request, appProjectID uuid.UUID) string {
	scheme := "https"
	if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") == "" {
		scheme = "http"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}
	host := r.Host
	if h2 := r.Header.Get("X-Forwarded-Host"); h2 != "" {
		host = h2
	}
	return scheme + "://" + host + "/v2/apps/" + appProjectID.String() + "/ota"
}
