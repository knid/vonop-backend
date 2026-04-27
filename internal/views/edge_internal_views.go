package views

import (
	"net/http"
	"strings"

	"github.com/knid/wemob/internal/utils"
)

// HandleEdgeCertAllowed gates Caddy's on-demand TLS. Returns 200 if the host
// is registered + verified, 404 otherwise. Caddy refuses to issue a Let's
// Encrypt cert for any 404 response, protecting against random hostnames
// pointed at our edge.
//
// Path: GET /internal/edge/cert-allowed?domain=shop.example.com
func (v *HttpViews) HandleEdgeCertAllowed(w http.ResponseWriter, r *http.Request) {
	host := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("domain")))
	if host == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// wemob.app subdomains are always allowed (covered by wildcard cert anyway,
	// but Caddy may still query — be permissive).
	if strings.HasSuffix(host, ".wemob.app") {
		w.WriteHeader(http.StatusOK)
		return
	}

	domain, err := v.Controller.DB.GetAppDomainByName(host)
	if err != nil || domain == nil || domain.DNSCheckStatus != "verified" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// HandleEdgeUpstream resolves a hostname back to the Fly app it should proxy to.
// Caddy queries this on every cache miss (cache_duration 5m).
//
// Path: GET /internal/edge/upstream?host=shop.example.com
func (v *HttpViews) HandleEdgeUpstream(w http.ResponseWriter, r *http.Request) {
	host := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("host")))
	if host == "" {
		utils.JSONErrorCode(w, "bad_request", http.StatusBadRequest, nil)
		return
	}

	// wemob.app subdomains: derive Fly app name directly from slug, no DB hit.
	if strings.HasSuffix(host, ".wemob.app") {
		slug := strings.TrimSuffix(host, ".wemob.app")
		if slug == "" || strings.ContainsAny(slug, "./") {
			utils.JSONErrorCode(w, "invalid_host", http.StatusBadRequest, nil)
			return
		}
		utils.JSONResponse(w, map[string]string{
			"upstream": "wemob-" + slug + ".fly.dev",
		}, http.StatusOK)
		return
	}

	// Custom domains: DB lookup.
	domain, err := v.Controller.DB.GetAppDomainByName(host)
	if err != nil || domain == nil {
		utils.JSONErrorCode(w, "not_found", http.StatusNotFound, nil)
		return
	}
	if domain.DNSCheckStatus != "verified" {
		utils.JSONErrorCode(w, "not_verified", http.StatusServiceUnavailable, nil)
		return
	}
	runtimeApp, err := v.Controller.DB.GetRuntimeAppByAppProjectID(domain.AppProjectID.String())
	if err != nil || runtimeApp.FlyAppName == "" {
		utils.JSONErrorCode(w, "no_runtime", http.StatusNotFound, nil)
		return
	}
	utils.JSONResponse(w, map[string]string{
		"upstream": runtimeApp.FlyAppName + ".fly.dev",
	}, http.StatusOK)
}
