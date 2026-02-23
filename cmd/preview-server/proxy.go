package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// newReverseProxy returns an http.Handler that routes incoming requests based
// on the subdomain of the Host header. A request to
// `{projectID}.preview.wemob.io` is proxied to the corresponding container's
// localhost port. Each proxied request also updates the container's last access
// time to prevent idle cleanup.
func newReverseProxy(cm *containerManager) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host

		// Strip port if present (e.g., "abc123.preview.wemob.io:80").
		if idx := strings.LastIndex(host, ":"); idx != -1 {
			// Only strip if the part after the colon looks like a port (all digits).
			potentialPort := host[idx+1:]
			isPort := true
			for _, c := range potentialPort {
				if c < '0' || c > '9' {
					isPort = false
					break
				}
			}
			if isPort {
				host = host[:idx]
			}
		}

		// Extract project ID from subdomain.
		// Expected format: {projectID}.preview.wemob.io
		projectID := extractProjectID(host, cm.previewDomain)
		if projectID == "" {
			http.Error(w, `{"error":"invalid host"}`, http.StatusBadGateway)
			return
		}

		info := cm.getContainer(projectID)
		if info == nil {
			slog.Warn("proxy: container not found", "project_id", projectID, "host", r.Host)
			http.Error(w, `{"error":"container not found"}`, http.StatusBadGateway)
			return
		}

		if info.Status != "ready" {
			slog.Warn("proxy: container not ready", "project_id", projectID, "status", info.Status)
			http.Error(w, fmt.Sprintf(`{"error":"container not ready","status":"%s"}`, info.Status), http.StatusBadGateway)
			return
		}

		// Update last access time.
		cm.touchContainer(projectID)

		// Proxy to the container's localhost port.
		target, err := url.Parse(fmt.Sprintf("http://localhost:%d", info.Port))
		if err != nil {
			slog.Error("proxy: failed to parse target URL", "project_id", projectID, "error", err)
			http.Error(w, `{"error":"internal proxy error"}`, http.StatusBadGateway)
			return
		}

		proxy := httputil.NewSingleHostReverseProxy(target)

		// Custom error handler to return 502 on backend failures.
		proxy.ErrorHandler = func(rw http.ResponseWriter, req *http.Request, err error) {
			slog.Error("proxy: backend error", "project_id", projectID, "error", err)
			rw.WriteHeader(http.StatusBadGateway)
			fmt.Fprintf(rw, `{"error":"backend unavailable"}`)
		}

		proxy.ServeHTTP(w, r)
	})
}

// extractProjectID parses the project ID from a subdomain. Given a host like
// "abc123.preview.wemob.io" and a domain "preview.wemob.io", it returns
// "abc123". Returns an empty string if the host does not match the expected
// pattern.
func extractProjectID(host, domain string) string {
	// The host should end with ".{domain}".
	suffix := "." + domain
	if !strings.HasSuffix(host, suffix) {
		return ""
	}

	// Everything before the suffix is the project ID.
	projectID := strings.TrimSuffix(host, suffix)

	// Validate: project ID should not be empty and should not contain dots
	// (which would indicate additional subdomain levels).
	if projectID == "" || strings.Contains(projectID, ".") {
		return ""
	}

	return projectID
}
