package caddy

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newFakeAdmin returns an httptest server that mimics Caddy's admin API with
// `enforce_origin` enabled: requests without an Origin header whose host
// matches the server URL are rejected with 403, exactly like prod since
// Caddyfile commit 2a58475 (2026-05-01).
func newFakeAdmin(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/load" || r.Method != http.MethodPost {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		origin := r.Header.Get("Origin")
		if origin == "" {
			http.Error(w, `{"error":"client is not allowed to access from origin ''"}`, http.StatusForbidden)
			return
		}
		u, err := url.Parse(origin)
		if err != nil || u.Host == "" {
			http.Error(w, `{"error":"bad origin"}`, http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func setupAdmin(t *testing.T, adminURL string) (*Admin, string) {
	t.Helper()
	dir := t.TempDir()
	caddyfile := filepath.Join(dir, "Caddyfile")
	if err := os.WriteFile(caddyfile, []byte("# minimal caddyfile\n"), 0o644); err != nil {
		t.Fatalf("write caddyfile: %v", err)
	}
	domainsDir := filepath.Join(dir, "custom-domains")
	if err := os.MkdirAll(domainsDir, 0o755); err != nil {
		t.Fatalf("mkdir domainsDir: %v", err)
	}
	return NewAdmin(adminURL, domainsDir, caddyfile), domainsDir
}

// TestAddCustomDomainRoute_SetsOriginHeader is the regression test for the
// caddy_route_add_failed bug: with enforce_origin on the Caddy admin, the
// reload POST must include an Origin header derived from the admin URL or
// Caddy returns 403 and the snippet write is rolled back. Pre-fix this test
// fails with: `reload after add ... caddy /load status 403`.
func TestAddCustomDomainRoute_SetsOriginHeader(t *testing.T) {
	srv := newFakeAdmin(t)
	admin, domainsDir := setupAdmin(t, srv.URL)

	if err := admin.AddCustomDomainRoute("example.com", "wemob-example"); err != nil {
		t.Fatalf("AddCustomDomainRoute: %v", err)
	}

	snippet := filepath.Join(domainsDir, "example.com.caddy")
	body, err := os.ReadFile(snippet)
	if err != nil {
		t.Fatalf("snippet missing: %v", err)
	}
	if !strings.Contains(string(body), "/internal/edge/proxy?host={host}") ||
		!strings.Contains(string(body), "header_up X-Internal-Token {env.EDGE_INTERNAL_TOKEN}") {
		t.Fatalf("snippet content unexpected: %s", body)
	}
}

// TestAddCustomDomainRoute_RollsBackOnReloadFailure verifies the existing
// rollback behavior so the fix doesn't regress it: any non-2xx from /load
// must remove the snippet and surface an error.
func TestAddCustomDomainRoute_RollsBackOnReloadFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	admin, domainsDir := setupAdmin(t, srv.URL)

	err := admin.AddCustomDomainRoute("rollback.example.com", "wemob-rollback")
	if err == nil {
		t.Fatal("expected reload error, got nil")
	}
	if _, statErr := os.Stat(filepath.Join(domainsDir, "rollback.example.com.caddy")); !os.IsNotExist(statErr) {
		t.Fatalf("expected snippet to be rolled back, stat err: %v", statErr)
	}
}

// TestRemoveCustomDomainRoute_SetsOriginHeader covers the symmetric remove
// path: it also calls reload() so it has the same enforce_origin failure mode.
func TestRemoveCustomDomainRoute_SetsOriginHeader(t *testing.T) {
	srv := newFakeAdmin(t)
	admin, domainsDir := setupAdmin(t, srv.URL)

	snippet := filepath.Join(domainsDir, "remove-me.example.com.caddy")
	if err := os.WriteFile(snippet, []byte("placeholder"), 0o644); err != nil {
		t.Fatalf("seed snippet: %v", err)
	}

	if err := admin.RemoveCustomDomainRoute("remove-me.example.com"); err != nil {
		t.Fatalf("RemoveCustomDomainRoute: %v", err)
	}
	if _, statErr := os.Stat(snippet); !os.IsNotExist(statErr) {
		t.Fatalf("expected snippet removed, stat err: %v", statErr)
	}
}
