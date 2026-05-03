//go:build integration

package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/revenuecat"
)

// fakeRCCounters tracks calls to the fake RC API so tests can assert on
// idempotency (a second provision call must NOT hit createProject again).
type fakeRCCounters struct {
	createProject atomic.Int32
	createApp     atomic.Int32
	listKeys      atomic.Int32
}

// newFakeRCServer returns an httptest.Server that mimics the RevenueCat
// REST v2 API for the three endpoints provisionRevenueCat uses.
//
// Hardcoded IDs:
//
//	project: "proj_fake_1"
//	first CreateApp call → app_ios (type=app_store)
//	second CreateApp call → app_and (type=play_store)
//
// Public keys: appl_PROD for iOS, goog_PROD for Android, both tagged
// "production".
func newFakeRCServer(t *testing.T) (*httptest.Server, *fakeRCCounters) {
	t.Helper()
	counters := &fakeRCCounters{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v2/projects" && r.Method == "POST":
			counters.createProject.Add(1)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "proj_fake_1", "name": "x"})
		case r.URL.Path == "/v2/projects/proj_fake_1/apps" && r.Method == "POST":
			n := counters.createApp.Add(1)
			id := "app_ios"
			typ := "app_store"
			if n == 2 {
				id = "app_and"
				typ = "play_store"
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": id, "type": typ, "name": "x"})
		case r.URL.Path == "/v2/projects/proj_fake_1/apps/app_ios/public_api_keys":
			counters.listKeys.Add(1)
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]string{
				{"key": "appl_PROD", "environment": "production"},
			}})
		case r.URL.Path == "/v2/projects/proj_fake_1/apps/app_and/public_api_keys":
			counters.listKeys.Add(1)
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]string{
				{"key": "goog_PROD", "environment": "production"},
			}})
		default:
			t.Logf("unexpected RC call: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	return server, counters
}

// setupProvisionTest creates a fresh DB + controller + minimal AppProject
// and MobileAppConfig wired for IAP provisioning. Caller plugs in the RC
// service (or leaves it nil for the graceful-degrade test).
func setupProvisionTest(t *testing.T) (*Controller, *database.Database, *models.AppProject) {
	t.Helper()
	db := database.NewTestDB(t,
		(*models.User)(nil),
		(*models.Organization)(nil),
		(*models.AppProject)(nil),
		(*models.MobileAppConfig)(nil),
	)
	c, _ := NewTestController(t, db, TestControllerOptions{})

	ctx := context.Background()
	user := &models.User{ID: uuid.New(), Name: "Owner", Email: "owner-" + uuid.NewString()[:8] + "@example.com", Password: "x"}
	if _, err := db.DB.NewInsert().Model(user).Exec(ctx); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	org := &models.Organization{ID: uuid.New(), Name: "Acme", Slug: "acme-iap-" + uuid.NewString()[:8], OwnerID: user.ID, IsPersonal: true}
	if _, err := db.DB.NewInsert().Model(org).Exec(ctx); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	project := &models.AppProject{
		ID: uuid.New(), UserID: user.ID, OrganizationID: org.ID,
		Name: "IAP Test", Slug: "iap-test-" + uuid.NewString()[:8], Prompt: "x",
		ProjectType: models.ProjectTypeMobileAppNative,
	}
	if _, err := db.DB.NewInsert().Model(project).Exec(ctx); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	return c, db, project
}

// TestProvisionRevenueCat_HappyPath_PersistsIDsAndKeys exercises the full
// provisioning flow against a fake RC server, asserts that the IDs + keys
// land in MobileAppConfig, then calls provisionRevenueCat AGAIN to verify
// idempotency: the second call must NOT hit RC's API.
func TestProvisionRevenueCat_HappyPath_PersistsIDsAndKeys(t *testing.T) {
	c, db, project := setupProvisionTest(t)
	server, counters := newFakeRCServer(t)
	defer server.Close()
	c.RevenueCatService = revenuecat.NewService("fake")
	c.RevenueCatService.SetTestEndpoint(server.URL)

	cfg := &models.MobileAppConfig{ProjectID: project.ID, IAPEnabled: true}
	if _, err := db.UpsertMobileAppConfig(cfg); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	// First call — should hit the API and persist results.
	if err := c.provisionRevenueCat(context.Background(), project); err != nil {
		t.Fatalf("first provision: %v", err)
	}

	got, err := db.GetMobileAppConfig(project.ID.String())
	if err != nil {
		t.Fatalf("get config after provision: %v", err)
	}
	if got.RevenueCatProjectID != "proj_fake_1" {
		t.Errorf("RevenueCatProjectID: got %q, want proj_fake_1", got.RevenueCatProjectID)
	}
	if got.RevenueCatIOSAppID != "app_ios" {
		t.Errorf("RevenueCatIOSAppID: got %q, want app_ios", got.RevenueCatIOSAppID)
	}
	if got.RevenueCatAndroidAppID != "app_and" {
		t.Errorf("RevenueCatAndroidAppID: got %q, want app_and", got.RevenueCatAndroidAppID)
	}
	if got.RevenueCatPublicKeyIOS != "appl_PROD" {
		t.Errorf("RevenueCatPublicKeyIOS: got %q, want appl_PROD", got.RevenueCatPublicKeyIOS)
	}
	if got.RevenueCatPublicKeyAndroid != "goog_PROD" {
		t.Errorf("RevenueCatPublicKeyAndroid: got %q, want goog_PROD", got.RevenueCatPublicKeyAndroid)
	}
	if counters.createProject.Load() != 1 {
		t.Errorf("createProject calls: got %d, want 1", counters.createProject.Load())
	}
	if counters.createApp.Load() != 2 {
		t.Errorf("createApp calls: got %d, want 2", counters.createApp.Load())
	}
	if counters.listKeys.Load() != 2 {
		t.Errorf("listKeys calls: got %d, want 2", counters.listKeys.Load())
	}

	// Second call — must be idempotent. Reset the counters first.
	counters.createProject.Store(0)
	counters.createApp.Store(0)
	counters.listKeys.Store(0)
	if err := c.provisionRevenueCat(context.Background(), project); err != nil {
		t.Fatalf("second provision: %v", err)
	}
	if counters.createProject.Load() != 0 {
		t.Errorf("idempotency: got %d createProject calls on second invocation, want 0", counters.createProject.Load())
	}
	if counters.createApp.Load() != 0 {
		t.Errorf("idempotency: got %d createApp calls on second invocation, want 0", counters.createApp.Load())
	}
}

// TestProvisionRevenueCat_GracefulDegradeWhenServiceNil verifies that a
// missing REVENUECAT_API_KEY (Controller.RevenueCatService == nil) results
// in a no-op rather than a panic or error. This is the path every dev /
// staging box without an RC API key takes.
func TestProvisionRevenueCat_GracefulDegradeWhenServiceNil(t *testing.T) {
	c, db, project := setupProvisionTest(t)
	c.RevenueCatService = nil

	cfg := &models.MobileAppConfig{ProjectID: project.ID, IAPEnabled: true}
	if _, err := db.UpsertMobileAppConfig(cfg); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	if err := c.provisionRevenueCat(context.Background(), project); err != nil {
		t.Fatalf("expected nil error when RC service nil, got %v", err)
	}

	got, _ := db.GetMobileAppConfig(project.ID.String())
	if got != nil && got.RevenueCatProjectID != "" {
		t.Errorf("expected RevenueCatProjectID empty when graceful-degraded, got %q", got.RevenueCatProjectID)
	}
}

// TestProvisionRevenueCat_DisabledNoOp verifies that IAPEnabled=false
// short-circuits BEFORE any RC API call — even if the service is wired.
func TestProvisionRevenueCat_DisabledNoOp(t *testing.T) {
	c, db, project := setupProvisionTest(t)
	server, counters := newFakeRCServer(t)
	defer server.Close()
	c.RevenueCatService = revenuecat.NewService("fake")
	c.RevenueCatService.SetTestEndpoint(server.URL)

	cfg := &models.MobileAppConfig{ProjectID: project.ID, IAPEnabled: false}
	if _, err := db.UpsertMobileAppConfig(cfg); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	if err := c.provisionRevenueCat(context.Background(), project); err != nil {
		t.Fatalf("expected nil err when IAPEnabled=false, got %v", err)
	}
	if counters.createProject.Load() != 0 {
		t.Errorf("expected zero RC API calls when disabled, got %d createProject", counters.createProject.Load())
	}
	got, _ := db.GetMobileAppConfig(project.ID.String())
	if got.RevenueCatProjectID != "" {
		t.Errorf("expected RevenueCatProjectID empty when disabled, got %q", got.RevenueCatProjectID)
	}
}
