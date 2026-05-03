package revenuecat

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// helper to spin up an httptest server with a single handler.
func newTestServer(handler http.HandlerFunc) *httptest.Server {
	return httptest.NewServer(handler)
}

func TestCreateProject_HappyPath(t *testing.T) {
	var gotAuth, gotMethod, gotPath, gotBody string
	var gotContentType string
	server := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotContentType = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "proj_abc", "name": "myapp"})
	})
	defer server.Close()

	s := NewService("sk_test_xyz")
	s.SetTestEndpoint(server.URL)

	p, err := s.CreateProject(context.Background(), "myapp")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if p.ID != "proj_abc" {
		t.Errorf("ID: got %q, want proj_abc", p.ID)
	}
	if p.Name != "myapp" {
		t.Errorf("Name: got %q, want myapp", p.Name)
	}
	if gotAuth != "Bearer sk_test_xyz" {
		t.Errorf("Authorization header: got %q, want Bearer sk_test_xyz", gotAuth)
	}
	if gotMethod != "POST" || gotPath != "/v2/projects" {
		t.Errorf("path: got %s %s, want POST /v2/projects", gotMethod, gotPath)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type: got %q", gotContentType)
	}
	if !strings.Contains(gotBody, `"name":"myapp"`) {
		t.Errorf("body: got %q, want it to contain \"name\":\"myapp\"", gotBody)
	}
}

func TestCreateApp_iOSBody(t *testing.T) {
	var gotBody string
	server := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		if r.URL.Path != "/v2/projects/proj_xyz/apps" {
			t.Errorf("path: got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "app_ios_1", "type": "app_store", "name": "x iOS"})
	})
	defer server.Close()

	s := NewService("k")
	s.SetTestEndpoint(server.URL)

	app, err := s.CreateApp(context.Background(), "proj_xyz", CreateAppParams{
		Name:     "x iOS",
		Type:     AppTypeAppStore,
		AppStore: &AppStoreConfig{BundleID: "com.wemob.test"},
	})
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if app.ID != "app_ios_1" || app.Type != AppTypeAppStore {
		t.Errorf("app: got %+v", app)
	}
	if !strings.Contains(gotBody, `"type":"app_store"`) {
		t.Errorf("body: missing type field, got %q", gotBody)
	}
	if !strings.Contains(gotBody, `"bundle_id":"com.wemob.test"`) {
		t.Errorf("body: missing bundle_id, got %q", gotBody)
	}
	// Negative — Android sub-payload must be omitted entirely (omitempty).
	if strings.Contains(gotBody, "play_store") {
		t.Errorf("body: Android payload should be omitted on iOS app, got %q", gotBody)
	}
}

func TestCreateApp_AndroidBody(t *testing.T) {
	var gotBody string
	server := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "app_and_1", "type": "play_store", "name": "x Android"})
	})
	defer server.Close()

	s := NewService("k")
	s.SetTestEndpoint(server.URL)

	app, err := s.CreateApp(context.Background(), "proj_xyz", CreateAppParams{
		Name:      "x Android",
		Type:      AppTypePlayStore,
		PlayStore: &PlayStoreConfig{PackageName: "com.wemob.test"},
	})
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if app.Type != AppTypePlayStore {
		t.Errorf("type: got %q", app.Type)
	}
	if !strings.Contains(gotBody, `"type":"play_store"`) {
		t.Errorf("body: missing type, got %q", gotBody)
	}
	if !strings.Contains(gotBody, `"package_name":"com.wemob.test"`) {
		t.Errorf("body: missing package_name, got %q", gotBody)
	}
	if strings.Contains(gotBody, "app_store") {
		t.Errorf("body: iOS payload should be omitted on Android app, got %q", gotBody)
	}
}

func TestListPublicAPIKeys_HappyPath(t *testing.T) {
	server := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("method: got %s", r.Method)
		}
		if r.URL.Path != "/v2/projects/p1/apps/a1/public_api_keys" {
			t.Errorf("path: got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]string{
				{"key": "appl_SBX", "environment": "sandbox"},
				{"key": "appl_PROD", "environment": "production"},
			},
		})
	})
	defer server.Close()

	s := NewService("k")
	s.SetTestEndpoint(server.URL)

	keys, err := s.ListPublicAPIKeys(context.Background(), "p1", "a1")
	if err != nil {
		t.Fatalf("ListPublicAPIKeys: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}
	prod := ProductionKeyForApp(keys)
	if prod != "appl_PROD" {
		t.Errorf("ProductionKeyForApp: got %q, want appl_PROD", prod)
	}
}

func TestProductionKeyForApp_FallsBackToFirst(t *testing.T) {
	keys := []PublicAPIKey{{Key: "appl_X", Environment: "default"}}
	if got := ProductionKeyForApp(keys); got != "appl_X" {
		t.Errorf("fallback: got %q, want appl_X", got)
	}
	if got := ProductionKeyForApp(nil); got != "" {
		t.Errorf("empty: got %q, want \"\"", got)
	}
}

func TestDo_Auth401_NoRetry(t *testing.T) {
	var calls atomic.Int32
	server := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	})
	defer server.Close()

	s := NewService("bad")
	s.SetTestEndpoint(server.URL)
	_, err := s.CreateProject(context.Background(), "x")
	if !errors.Is(err, ErrAuth) {
		t.Fatalf("expected ErrAuth, got %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("expected exactly 1 attempt on 401, got %d", calls.Load())
	}
}

func TestDo_429Retries_ThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	server := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "proj_z", "name": "x"})
	})
	defer server.Close()

	s := NewService("k")
	s.SetTestEndpoint(server.URL)
	// Override delays implicitly via a tight context window — but the actual
	// backoff is hardcoded in do(). The test takes ~3s wall time (1+2). We
	// allow up to 10s to be safe across CI variance.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	p, err := s.CreateProject(ctx, "x")
	if err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if p.ID != "proj_z" {
		t.Errorf("ID mismatch: %q", p.ID)
	}
	if calls.Load() != 3 {
		t.Errorf("expected 3 attempts, got %d", calls.Load())
	}
}

func TestDo_429ExhaustsRetryBudget(t *testing.T) {
	var calls atomic.Int32
	server := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	})
	defer server.Close()

	s := NewService("k")
	s.SetTestEndpoint(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := s.CreateProject(ctx, "x")
	if !errors.Is(err, ErrRateLimit) {
		t.Fatalf("expected ErrRateLimit on persistent 429, got %v", err)
	}
	// Budget = initial + 3 retries = 4 attempts.
	if calls.Load() != 4 {
		t.Errorf("expected 4 attempts (initial + 3 retries) on 429, got %d", calls.Load())
	}
}

func TestDo_5xxRetriesOnce(t *testing.T) {
	var calls atomic.Int32
	server := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	})
	defer server.Close()
	s := NewService("k")
	s.SetTestEndpoint(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := s.CreateProject(ctx, "x")
	if err == nil {
		t.Fatalf("expected error on persistent 5xx")
	}
	// 5xx → initial + 1 retry only.
	if calls.Load() != 2 {
		t.Errorf("expected 2 attempts (initial + 1 retry), got %d", calls.Load())
	}
}

func TestDo_4xxFailsFast(t *testing.T) {
	var calls atomic.Int32
	server := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":"bundle_id_taken"}`))
	})
	defer server.Close()
	s := NewService("k")
	s.SetTestEndpoint(server.URL)
	_, err := s.CreateProject(context.Background(), "x")
	if err == nil {
		t.Fatal("expected error on 422")
	}
	if errors.Is(err, ErrAuth) || errors.Is(err, ErrRateLimit) {
		t.Errorf("422 should not be ErrAuth or ErrRateLimit, got %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("expected 1 attempt on 4xx fail-fast, got %d", calls.Load())
	}
}

func TestDo_RespectsCanceledContext(t *testing.T) {
	server := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})
	defer server.Close()
	s := NewService("k")
	s.SetTestEndpoint(server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.CreateProject(ctx, "x")
	if err == nil {
		t.Fatal("expected error on canceled ctx")
	}
}
