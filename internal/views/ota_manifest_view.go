package views

import (
	"crypto/sha256"
	"encoding/binary"
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

// otaDefaultChannel is the channel returned when the device sends no
// expo-channel-name header. Picked to keep existing single-channel
// deployments behaving exactly as before P2#22.
const otaDefaultChannel = "production"

// rolloutBucket returns a deterministic 0-99 bucket for (clientID, updateID).
// Same input ⇒ same bucket ⇒ same gate decision on every poll, so a device
// that just missed the rollout doesn't randomly catch it on a retry. clientID
// is whatever the caller passes — we treat it as opaque bytes; if the device
// sends expo-current-update-id we use that, otherwise we fall back to the
// candidate update's UpdateID so the bucket is at least stable across retries
// of the same update (every device gets the same bucket, but it's stable).
func rolloutBucket(clientID, updateID string) uint32 {
	sum := sha256.Sum256([]byte(clientID + ":" + updateID))
	return binary.BigEndian.Uint32(sum[:4]) % 100
}

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

	// expo-expect-signature is informational — when present we also validate
	// the algorithm is one we support (defensive; old binaries that don't
	// send the header still get a signed manifest).
	if got := r.Header.Get("expo-expect-signature"); got != "" {
		if !strings.Contains(got, ota.SigningAlgorithmRSAv15SHA256) {
			slog.Warn("ota: client expects unsupported signature algorithm",
				"app_project_id", appProjectID, "expect", got)
		}
	}

	// P2#22 — channel + rollout filtering.
	//
	// expo-channel-name: the channel the binary was built against (set in
	// the EXPO_PUBLIC_OTA_URL → manifest fetch; defaults to "production"
	// when absent so single-channel deployments keep working unchanged).
	//
	// expo-current-update-id: the update the device is currently running.
	// Used as the deterministic key for the rollout-percentage gate so a
	// device that doesn't qualify on one poll keeps not qualifying until
	// the gate widens — no flapping across retries.
	channel := strings.TrimSpace(r.Header.Get("expo-channel-name"))
	if channel == "" {
		channel = otaDefaultChannel
	}
	currentUpdateID := strings.TrimSpace(r.Header.Get("expo-current-update-id"))

	update, err := h.Controller.GetLatestOTAManifestForChannel(r.Context(), appProjectID, runtimeVersion, platform, channel)
	if err != nil {
		switch {
		case errors.Is(err, controllers.ErrOTANotFound):
			// No active update for this (channel, RV, platform). Return 204
			// so the expo-updates client treats it as "stay on the embedded
			// bundle" rather than logging a hard error like a 404 would.
			w.Header().Set("expo-protocol-version", "0")
			w.Header().Set("expo-sfv-version", "0")
			w.WriteHeader(http.StatusNoContent)
		case errors.Is(err, controllers.ErrOTAInvalidPlatform):
			utils.JSONError(w, "bad_request", "invalid platform", http.StatusBadRequest, nil)
		default:
			slog.Error("ota manifest lookup failed", "app_project_id", appProjectID, "err", err)
			utils.JSONError(w, "internal", "manifest lookup failed", http.StatusInternalServerError, nil)
		}
		return
	}

	// Rollout gate: clamp out-of-range values defensively (DB has notnull
	// + default 100; this is belt-and-braces against a bad insert). 0 ⇒
	// nobody, 100 ⇒ everyone, 1-99 ⇒ deterministic-hash bucketed.
	pct := int(update.RolloutPercentage)
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	if pct < 100 {
		clientID := currentUpdateID
		if clientID == "" {
			// No prior update id (fresh install). Fall back to the
			// candidate's UpdateID so the bucket is at least deterministic
			// across retries of THIS update — same device hits the same
			// bucket on every poll until something else changes.
			clientID = update.UpdateID.String()
		}
		bucket := rolloutBucket(clientID, update.UpdateID.String())
		if int(bucket) >= pct {
			// Outside the rollout window for this device. 204 ⇒ stay on
			// whatever you've got. Don't 404 — the update DOES exist for
			// some devices, just not this one.
			w.Header().Set("expo-protocol-version", "0")
			w.Header().Set("expo-sfv-version", "0")
			w.WriteHeader(http.StatusNoContent)
			return
		}
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

	manifestBody, body, boundary, err := ota.MarshalMultipartWithBody(manifest)
	if err != nil {
		slog.Error("ota: marshal multipart", "update_id", update.ID, "err", err)
		utils.JSONError(w, "internal", "manifest construction failed", http.StatusInternalServerError, nil)
		return
	}

	// Sign the manifest body bytes. P0 #8a — without signing, expo-updates
	// clients built with a codeSigningCertificate would reject the response,
	// AND clients built without one accept any unsigned manifest as a
	// silent RCE channel. Lazily generate the per-project keypair on first
	// publish.
	signKey, err := h.Controller.EnsureOTAUpdateSigningKey(r.Context(), appProjectID)
	if err != nil {
		slog.Error("ota: ensure signing key", "app_project_id", appProjectID, "err", err)
		utils.JSONError(w, "internal", "manifest signing failed", http.StatusInternalServerError, nil)
		return
	}
	privPEM, err := h.Controller.LoadOTAUpdateSigningPrivateKeyPEM(r.Context(), signKey)
	if err != nil {
		slog.Error("ota: load signing key", "app_project_id", appProjectID, "err", err)
		utils.JSONError(w, "internal", "manifest signing failed", http.StatusInternalServerError, nil)
		return
	}
	sigHeader, err := ota.SignManifest(manifestBody, privPEM, signKey.KeyID)
	if err != nil {
		slog.Error("ota: sign manifest", "app_project_id", appProjectID, "err", err)
		utils.JSONError(w, "internal", "manifest signing failed", http.StatusInternalServerError, nil)
		return
	}

	w.Header().Set("Content-Type", "multipart/mixed; boundary="+boundary)
	w.Header().Set("expo-protocol-version", "0")
	w.Header().Set("expo-sfv-version", "0")
	w.Header().Set("expo-signature", sigHeader)
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
