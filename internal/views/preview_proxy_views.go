package views

import (
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/knid/wemob/internal/services/sandbox"
	"github.com/knid/wemob/internal/utils"
)

const previewDomainSuffix = ".preview.wemob.app"

func parsePreviewHost(host string) (slug string, kind sandbox.PreviewKind) {
	host = strings.ToLower(strings.TrimSpace(strings.Split(host, ":")[0]))
	if host == "" {
		return "", ""
	}
	if host == "preview.wemob.app" {
		return "", ""
	}
	if !strings.HasSuffix(host, previewDomainSuffix) {
		return "", ""
	}
	label := strings.TrimSuffix(host, previewDomainSuffix)
	if label == "" || strings.Contains(label, ".") {
		return "", ""
	}
	switch {
	case strings.HasSuffix(label, "-backend"):
		return nonEmptySlug(strings.TrimSuffix(label, "-backend")), sandbox.PreviewBackend
	case strings.HasSuffix(label, "-expo"):
		return nonEmptySlug(strings.TrimSuffix(label, "-expo")), sandbox.PreviewExpo
	case strings.HasSuffix(label, "-artifacts"):
		return nonEmptySlug(strings.TrimSuffix(label, "-artifacts")), sandbox.PreviewArtifacts
	default:
		return label, sandbox.PreviewApp
	}
}

func nonEmptySlug(slug string) string {
	if slug == "" {
		return ""
	}
	return slug
}

func (v *HttpViews) ResolvePreviewHost(w http.ResponseWriter, r *http.Request) {
	slug, kind := parsePreviewHost(r.URL.Query().Get("host"))
	if slug == "" || kind == "" {
		utils.JSONError(w, "INVALID_PREVIEW_HOST", "invalid preview host", http.StatusBadRequest, nil)
		return
	}
	utils.JSONResponse(w, map[string]any{"slug": slug, "kind": kind}, http.StatusOK)
}

func (v *HttpViews) HandlePreviewProxy(w http.ResponseWriter, r *http.Request) {
	host := r.URL.Query().Get("host")
	if host == "" {
		host = r.Header.Get("X-Wemob-Preview-Host")
	}
	if host == "" {
		host = r.Host
	}
	slug, kind := parsePreviewHost(host)
	if slug == "" || kind == "" {
		utils.JSONError(w, "INVALID_PREVIEW_HOST", "invalid preview host", http.StatusBadRequest, nil)
		return
	}
	if v == nil || v.Controller == nil || v.Controller.DB == nil || v.Controller.SandboxManager == nil {
		utils.JSONError(w, "PREVIEW_UNAVAILABLE", "preview is unavailable", http.StatusServiceUnavailable, nil)
		return
	}
	project, err := v.Controller.DB.GetAppProjectBySlug(slug)
	if err != nil || project == nil {
		utils.JSONError(w, "PREVIEW_NOT_FOUND", "preview not found", http.StatusNotFound, nil)
		return
	}
	status, err := v.Controller.SandboxStatus(r.Context(), project)
	if err != nil {
		slog.Warn("preview proxy status failed", "error", err, "slug", slug, "kind", kind)
		utils.JSONError(w, "PREVIEW_UNAVAILABLE", "preview is unavailable", http.StatusServiceUnavailable, nil)
		return
	}
	targetURL := previewURLForKind(status, kind)
	if targetURL == "" {
		utils.JSONError(w, "PREVIEW_NOT_READY", "preview is not ready", http.StatusServiceUnavailable, nil)
		return
	}
	target, err := url.Parse(targetURL)
	if err != nil || target.Scheme == "" || target.Host == "" {
		utils.JSONError(w, "PREVIEW_UNAVAILABLE", "preview is unavailable", http.StatusServiceUnavailable, nil)
		return
	}
	originalURI := r.Header.Get("X-Original-URI")
	proxyReq := r
	if originalURI != "" {
		if originalURL, err := url.ParseRequestURI(originalURI); err == nil {
			proxyReq = r.Clone(r.Context())
			proxyReq.URL.Path = originalURL.Path
			proxyReq.URL.RawPath = originalURL.RawPath
			proxyReq.URL.RawQuery = originalURL.RawQuery
		}
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	baseDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		baseDirector(req)
		req.Host = target.Host
		req.Header.Set("X-Forwarded-Host", host)
	}
	proxy.ErrorHandler = func(rw http.ResponseWriter, req *http.Request, proxyErr error) {
		slog.Warn("preview proxy upstream failed", "error", proxyErr, "slug", slug, "kind", kind)
		utils.JSONError(rw, "PREVIEW_UPSTREAM_FAILED", "preview upstream failed", http.StatusBadGateway, nil)
	}
	proxy.ServeHTTP(w, proxyReq)
}

func previewURLForKind(status *sandbox.Status, kind sandbox.PreviewKind) string {
	if status == nil || status.PreviewURLs == nil {
		return ""
	}
	if u := status.PreviewURLs[kind]; u != "" {
		return u
	}
	if kind == sandbox.PreviewApp {
		return status.PreviewURLs[statusDefaultPreviewKind(status)]
	}
	return ""
}

func statusDefaultPreviewKind(status *sandbox.Status) sandbox.PreviewKind {
	if status == nil {
		return ""
	}
	if status.PreviewURLs[sandbox.PreviewApp] != "" {
		return sandbox.PreviewApp
	}
	if status.PreviewURLs[sandbox.PreviewBackend] != "" {
		return sandbox.PreviewBackend
	}
	if status.PreviewURLs[sandbox.PreviewExpo] != "" {
		return sandbox.PreviewExpo
	}
	return ""
}
