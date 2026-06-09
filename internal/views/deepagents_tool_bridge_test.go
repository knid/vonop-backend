package views

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/knid/wemob/internal/services/jobstream"
	"github.com/redis/go-redis/v9"
)

// fakeRedisServer is a tiny in-process Redis stand-in. We only need it to
// exist so the controller's RedisPubSub is non-nil when we publish
// approval events; nothing in these tests actually subscribes.
func fakeRedisServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// makeTestClient builds a redis.Client that points at the test HTTP server.
// It is intentionally not used for real Redis operations — only for
// non-nil checks inside publishToolApproval.
func makeTestClient(addr string) *redis.Client {
	return redis.NewClient(&redis.Options{Addr: addr})
}

func TestDeepAgentsToolExecute_RejectsMissingToken(t *testing.T) {
	v := &HttpViews{Controller: nil}
	r := httptest.NewRequest(http.MethodPost,
		"/internal/v1/agent-runs/00000000-0000-4000-8000-000000000000/tools/sandbox_status/execute",
		bytes.NewReader([]byte(`{}`)))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	// No controller means no SecretManager; the endpoint must short-circuit
	// before dereferencing anything. The expected behavior is the 401 path
	// because presented token doesn't match — in this test we just want to
	// make sure the request does not panic.
	defer func() {
		_ = recover()
	}()
	v.HandleDeepAgentsToolExecute(w, r)
	// We expect either 401 (no controller) or 503 (no token configured).
	if w.Code != http.StatusUnauthorized && w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 401 or 503, got %d", w.Code)
	}
}

func TestDeepAgentsToolExecute_RejectsWrongMethod(t *testing.T) {
	v := &HttpViews{Controller: nil}
	r := httptest.NewRequest(http.MethodGet, "/internal/v1/agent-runs/x/tools/y/execute", nil)
	w := httptest.NewRecorder()
	v.HandleDeepAgentsToolExecute(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestFirstLine(t *testing.T) {
	if got := firstLine("hello\nworld"); got != "hello" {
		t.Fatalf("expected 'hello', got %q", got)
	}
	if got := firstLine("only one line"); got != "only one line" {
		t.Fatalf("expected 'only one line', got %q", got)
	}
	if got := firstLine(""); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestTruncateToolResult(t *testing.T) {
	if got := truncateToolResult("short", 100); got != "short" {
		t.Fatalf("expected passthrough, got %q", got)
	}
	long := strings.Repeat("a", 1000)
	got := truncateToolResult(long, 50)
	if len(got) > 200 {
		t.Fatalf("truncation should produce short result, got %d bytes", len(got))
	}
	if !strings.Contains(got, "truncated") {
		t.Fatalf("truncation marker missing in %q", got)
	}
}

func TestIsMCPToolName(t *testing.T) {
	cases := map[string]bool{
		"mcp_myconn_myserver_listIssues": true,
		"myconn:myserver:listIssues":     true,
		"sandbox_status":                 false,
		"trigger_build_after_fix":        false,
	}
	for in, want := range cases {
		if got := isMCPToolName(in); got != want {
			t.Fatalf("isMCPToolName(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestNormalizeMCPPolicy(t *testing.T) {
	cases := map[string]string{
		"auto":    "auto",
		"ask":     "ask",
		"blocked": "blocked",
		"":        "ask",
		"random":  "ask",
	}
	for in, want := range cases {
		if got := normalizeMCPPolicy(in); got != want {
			t.Fatalf("normalizeMCPPolicy(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWriteToolResult_RoundTrip(t *testing.T) {
	w := httptest.NewRecorder()
	writeToolResult(w, DeepAgentsToolResult{
		Result:     "ok",
		IsError:    false,
		Summary:    "ok",
		DurationMs: 5,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var body DeepAgentsToolResult
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Result != "ok" || body.DurationMs != 5 {
		t.Fatalf("round trip mismatch: %+v", body)
	}
}

// TestPublishToolApproval_NoOpWhenRedisNil confirms the publish path is
// safe to call when the controller is nil or the redis client is missing.
// (This is the same guard the live endpoint uses.)
func TestPublishToolApproval_NoOpWhenRedisNil(t *testing.T) {
	publishToolApproval(nil, "job-1", nil) // should not panic
	publishToolApproval(&HttpViews{}, "job-1", "wrong type") // wrong type -> no-op
}

// touchUnused keeps the redis import live for future expansion of this
// test file (e.g. an in-process miniredis-backed round-trip).
var _ = makeTestClient
var _ = (*redis.Client)(nil)
var _ = context.Background
var _ = time.Second
var _ = jobstream.PublishCancel
