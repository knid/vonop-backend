// Package caddy provides a thin client for managing Caddy server config at
// runtime. Used to add/remove per-custom-domain routes when domain DNS
// verification flips state.
package caddy

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Admin writes per-domain Caddyfile snippets to a shared volume that Caddy
// imports, then triggers Caddy to reload its config via the admin API.
type Admin struct {
	AdminURL      string // e.g. http://caddy:2019
	DomainsDir    string // e.g. /data/custom-domains (where api writes)
	CaddyfilePath string // e.g. /etc/caddy/Caddyfile (mounted read-only in api)
	httpClient    *http.Client
}

func NewAdmin(adminURL, domainsDir, caddyfilePath string) *Admin {
	return &Admin{
		AdminURL:      strings.TrimRight(adminURL, "/"),
		DomainsDir:    domainsDir,
		CaddyfilePath: caddyfilePath,
		httpClient:    &http.Client{Timeout: 15 * time.Second},
	}
}

// AddCustomDomainRoute writes a Caddyfile snippet for the given hostname that
// reverse-proxies to the named Fly app, then reloads Caddy. Idempotent —
// overwrites any existing snippet for the same hostname. No-op if the Admin
// is not configured.
func (a *Admin) AddCustomDomainRoute(hostname, flyAppName string) error {
	if a == nil || a.DomainsDir == "" {
		return nil
	}
	fname, err := snippetPath(a.DomainsDir, hostname)
	if err != nil {
		return err
	}

	snippet := fmt.Sprintf(`%s {
    tls {
        on_demand
    }
    reverse_proxy https://%[2]s.fly.dev {
        header_up Host %[2]s.fly.dev
        transport http {
            tls
            tls_server_name %[2]s.fly.dev
        }
    }
}
`, hostname, flyAppName)

	if err := os.WriteFile(fname, []byte(snippet), 0o644); err != nil {
		return fmt.Errorf("write snippet for %s: %w", hostname, err)
	}
	if err := a.reload(); err != nil {
		// Roll back the file so we don't leave a half-applied state behind.
		_ = os.Remove(fname)
		return fmt.Errorf("reload after add %s: %w", hostname, err)
	}
	slog.Info("caddy_route_added", "hostname", hostname, "fly_app", flyAppName)
	return nil
}

// RemoveCustomDomainRoute deletes the per-domain snippet and reloads.
// Idempotent — ENOENT is ignored.
func (a *Admin) RemoveCustomDomainRoute(hostname string) error {
	if a == nil || a.DomainsDir == "" {
		return nil
	}
	fname, err := snippetPath(a.DomainsDir, hostname)
	if err != nil {
		return err
	}
	if err := os.Remove(fname); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove snippet for %s: %w", hostname, err)
	}
	if err := a.reload(); err != nil {
		return fmt.Errorf("reload after remove %s: %w", hostname, err)
	}
	slog.Info("caddy_route_removed", "hostname", hostname)
	return nil
}

// reload posts the Caddyfile to Caddy's /load endpoint, causing Caddy to
// re-read all imports. Caddy resolves imports against its own filesystem
// view, which has the snippet directory mounted.
func (a *Admin) reload() error {
	if a.AdminURL == "" || a.CaddyfilePath == "" {
		return nil
	}
	content, err := os.ReadFile(a.CaddyfilePath)
	if err != nil {
		return fmt.Errorf("read caddyfile: %w", err)
	}
	req, err := http.NewRequest("POST", a.AdminURL+"/load", bytes.NewReader(content))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/caddyfile")
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("post /load: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		buf := make([]byte, 1024)
		n, _ := resp.Body.Read(buf)
		return fmt.Errorf("caddy /load status %d: %s", resp.StatusCode, string(buf[:n]))
	}
	return nil
}

// snippetPath builds the on-disk path for a hostname's snippet, validating
// that the hostname only contains characters safe for filenames.
func snippetPath(dir, hostname string) (string, error) {
	hostname = strings.ToLower(strings.TrimSpace(hostname))
	if hostname == "" {
		return "", fmt.Errorf("empty hostname")
	}
	for _, ch := range hostname {
		ok := (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') ||
			ch == '-' || ch == '.'
		if !ok {
			return "", fmt.Errorf("invalid character in hostname: %q", ch)
		}
	}
	return filepath.Join(dir, hostname+".caddy"), nil
}
