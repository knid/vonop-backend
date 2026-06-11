package views

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/knid/wemob/internal/models"
)

const defaultPreviewGatewayDomain = "preview.wemob.app"

func (v *HttpViews) PreviewGatewayMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slug, ok := previewSlugFromHost(r.Host, previewGatewayDomain())
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		v.ProxyProjectPreview(w, r, slug)
	})
}

func (v *HttpViews) ProxyProjectPreview(w http.ResponseWriter, r *http.Request, previewSlug string) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	session, err := v.Controller.DB.GetProjectPreviewSessionBySlug(ctx, previewSlug)
	if err != nil {
		slog.Warn("preview gateway: session not found", "preview_slug", previewSlug, "error", err)
		http.Error(w, "preview not found", http.StatusNotFound)
		return
	}
	if session.Status != models.PreviewSessionStatusReady || session.DaytonaPreviewURL == "" {
		http.Error(w, "preview is not ready", http.StatusServiceUnavailable)
		return
	}
	target, err := url.Parse(session.DaytonaPreviewURL)
	if err != nil || target.Scheme == "" || target.Host == "" {
		slog.Error("preview gateway: invalid provider URL", "preview_slug", previewSlug, "url", session.DaytonaPreviewURL, "error", err)
		http.Error(w, "preview is misconfigured", http.StatusBadGateway)
		return
	}

	_ = v.Controller.DB.TouchProjectPreviewSessionAccess(context.Background(), previewSlug)

	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		req.URL.Path = singleJoiningSlash(target.Path, r.URL.Path)
		req.URL.RawQuery = r.URL.RawQuery
		req.Host = target.Host
		req.Header.Set("X-Daytona-Skip-Preview-Warning", "true")
		if session.DaytonaPreviewToken != "" {
			req.Header.Set("x-daytona-preview-token", session.DaytonaPreviewToken)
		}
	}
	proxy.ErrorHandler = func(rw http.ResponseWriter, req *http.Request, proxyErr error) {
		slog.Warn("preview gateway: proxy error", "preview_slug", previewSlug, "error", proxyErr)
		http.Error(rw, "preview gateway error", http.StatusBadGateway)
	}
	proxy.ServeHTTP(w, r)
}

func previewGatewayDomain() string {
	if domain := strings.TrimSpace(os.Getenv("WEMOB_PREVIEW_DOMAIN")); domain != "" {
		return strings.TrimPrefix(domain, ".")
	}
	return defaultPreviewGatewayDomain
}

func previewSlugFromHost(host string, domain string) (string, bool) {
	host = strings.ToLower(strings.TrimSpace(host))
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	domain = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(domain), "."))
	suffix := "." + domain
	if !strings.HasSuffix(host, suffix) {
		return "", false
	}
	slug := strings.TrimSuffix(host, suffix)
	if slug == "" || strings.Contains(slug, ".") {
		return "", false
	}
	return slug, true
}

func singleJoiningSlash(a string, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	default:
		return a + b
	}
}
