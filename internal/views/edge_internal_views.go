package views

import (
	"crypto/tls"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	pathpkg "path"
	"strings"

	"github.com/knid/wemob/internal/models"
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

// HandleEdgeProxy is the edge entrypoint used by Caddy for generated app
// domains. It preserves legacy Fly apps while letting split-web projects serve
// static frontend assets from S3 and route /api/* to the optional backend Fly
// app.
//
// Caddy rewrites every edge request to:
//
//	/internal/edge/proxy?host=<original-host>
//
// and forwards the original path/query in X-Original-URI.
func (v *HttpViews) HandleEdgeProxy(w http.ResponseWriter, r *http.Request) {
	host := normalizeEdgeHost(r.URL.Query().Get("host"))
	if host == "" {
		utils.JSONErrorCode(w, "bad_request", http.StatusBadRequest, nil)
		return
	}
	originalURI := r.Header.Get("X-Original-URI")
	if originalURI == "" {
		originalURI = "/"
	}
	parsedURI, err := url.ParseRequestURI(originalURI)
	if err != nil {
		utils.JSONErrorCode(w, "bad_uri", http.StatusBadRequest, nil)
		return
	}

	project, runtimeApp, err := v.edgeProjectForHost(host)
	if err != nil || project == nil {
		utils.JSONErrorCode(w, "not_found", http.StatusNotFound, nil)
		return
	}

	deployment, _ := v.Controller.DB.GetLatestAppDeployment(project.ID.String(), "web")
	isStatic := deployment != nil &&
		deployment.Status == "deployed" &&
		deployment.Provider == models.FrontendHostingS3CDN

	if isStatic {
		if parsedURI.Path == "/api" || strings.HasPrefix(parsedURI.Path, "/api/") {
			if runtimeApp == nil || runtimeApp.FlyAppName == "" || !project.BackendEnabled {
				utils.JSONErrorCode(w, "backend_not_deployed", http.StatusNotFound, nil)
				return
			}
			proxyToFly(w, r, runtimeApp.FlyAppName, parsedURI, host)
			return
		}
		v.serveStaticDeployment(w, r, deployment, parsedURI.Path)
		return
	}

	flyAppName := ""
	if runtimeApp != nil {
		flyAppName = runtimeApp.FlyAppName
	}
	if flyAppName == "" {
		flyAppName = "wemob-" + project.Slug
	}
	proxyToFly(w, r, flyAppName, parsedURI, host)
}

func normalizeEdgeHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if i := strings.Index(host, ":"); i >= 0 {
		host = host[:i]
	}
	return host
}

func (v *HttpViews) edgeProjectForHost(host string) (*models.AppProject, *models.RuntimeApp, error) {
	var project *models.AppProject
	if strings.HasSuffix(host, ".wemob.app") {
		slug := strings.TrimSuffix(host, ".wemob.app")
		if slug == "" || strings.ContainsAny(slug, "./") {
			return nil, nil, fmt.Errorf("invalid slug host")
		}
		p, err := v.Controller.DB.GetAppProjectBySlug(slug)
		if err != nil {
			return nil, nil, err
		}
		project = p
	} else {
		domain, err := v.Controller.DB.GetAppDomainByName(host)
		if err != nil || domain == nil || domain.DNSCheckStatus != "verified" {
			return nil, nil, fmt.Errorf("domain not found or not verified")
		}
		p, err := v.Controller.DB.GetAppProjectByID(domain.AppProjectID.String())
		if err != nil {
			return nil, nil, err
		}
		project = p
	}
	runtimeApp, _ := v.Controller.DB.GetRuntimeAppByAppProjectID(project.ID.String())
	return project, runtimeApp, nil
}

func (v *HttpViews) serveStaticDeployment(w http.ResponseWriter, r *http.Request, deployment *models.AppDeployment, requestPath string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	prefix, _ := deployment.ProviderData["static_s3_prefix"].(string)
	if prefix == "" || v.Controller.AssetsStorage == nil {
		utils.JSONErrorCode(w, "static_missing", http.StatusNotFound, nil)
		return
	}
	key := staticObjectKey(prefix, requestPath)
	if cdnBaseURL := staticCDNBaseURL(deployment); cdnBaseURL != "" && shouldRedirectStaticAsset(requestPath) {
		if assetURL, joinErr := url.JoinPath(cdnBaseURL, key); joinErr == nil {
			w.Header().Set("Cache-Control", "public, max-age=300")
			http.Redirect(w, r, assetURL, http.StatusTemporaryRedirect)
			return
		} else {
			slog.Warn("edge static cdn url failed", "deployment_id", deployment.ID, "key", key, "err", joinErr)
		}
	}
	body, err := v.Controller.AssetsStorage.ReadObjectBytes(r.Context(), key)
	if err != nil && key != prefix+"/index.html" && pathpkg.Ext(requestPath) == "" {
		key = prefix + "/index.html"
		body, err = v.Controller.AssetsStorage.ReadObjectBytes(r.Context(), key)
	}
	if err != nil {
		slog.Warn("edge static read failed", "deployment_id", deployment.ID, "key", key, "err", err)
		utils.JSONErrorCode(w, "static_not_found", http.StatusNotFound, nil)
		return
	}
	contentType := mime.TypeByExtension(pathpkg.Ext(key))
	if strings.HasSuffix(key, ".html") {
		contentType = "text/html; charset=utf-8"
		w.Header().Set("Cache-Control", "no-cache")
	} else if contentType == "" {
		contentType = http.DetectContentType(body)
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

func staticObjectKey(prefix, requestPath string) string {
	clean := pathpkg.Clean("/" + strings.TrimSpace(requestPath))
	clean = strings.TrimPrefix(clean, "/")
	if clean == "" || clean == "." {
		clean = "index.html"
	}
	return prefix + "/" + clean
}

func shouldRedirectStaticAsset(requestPath string) bool {
	ext := strings.ToLower(pathpkg.Ext(requestPath))
	return ext != "" && ext != ".html" && ext != ".htm"
}

func staticCDNBaseURL(deployment *models.AppDeployment) string {
	if deployment != nil {
		if baseURL, _ := deployment.ProviderData["static_cdn_base_url"].(string); strings.TrimSpace(baseURL) != "" {
			return strings.TrimRight(strings.TrimSpace(baseURL), "/")
		}
	}
	return strings.TrimRight(strings.TrimSpace(os.Getenv("WEMOB_STATIC_CDN_BASE_URL")), "/")
}

func proxyToFly(w http.ResponseWriter, r *http.Request, flyAppName string, originalURI *url.URL, originalHost string) {
	target := &url.URL{Scheme: "https", Host: flyAppName + ".fly.dev"}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = &http.Transport{
		TLSClientConfig: &tls.Config{ServerName: target.Host},
	}
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.URL.Path = originalURI.Path
		req.URL.RawQuery = originalURI.RawQuery
		req.Host = target.Host
		req.Header.Set("Host", target.Host)
		req.Header.Set("X-Forwarded-Host", originalHost)
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, req *http.Request, err error) {
		slog.Warn("edge fly proxy failed", "fly_app", flyAppName, "err", err)
		utils.JSONErrorCode(w, "upstream_unavailable", http.StatusBadGateway, nil)
	}
	proxy.ServeHTTP(w, r)
}
