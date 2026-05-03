//go:build integration

package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/utils/external"
)

// fakeOSCounters tracks calls so we can assert idempotency.
type fakeOSCounters struct {
	createApp atomic.Int32
	createTok atomic.Int32
}

// newFakeOSServer stands in for the OneSignal master API. Handles the two
// endpoints provisionOneSignalApp invokes: POST /apps and POST /apps/.../auth/tokens.
func newFakeOSServer(t *testing.T) (*httptest.Server, *fakeOSCounters) {
	t.Helper()
	counters := &fakeOSCounters{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/apps" && r.Method == "POST":
			counters.createApp.Add(1)
			w.WriteHeader(200)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "os-app-real-1", "name": "x"})
		case strings.HasSuffix(r.URL.Path, "/auth/tokens") && r.Method == "POST":
			counters.createTok.Add(1)
			w.WriteHeader(200)
			_ = json.NewEncoder(w).Encode(map[string]string{"formatted_token": "tok-real-1"})
		default:
			t.Logf("unexpected OS call: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	return server, counters
}

// setupOneSignalProvisionTest creates a fresh DB + controller + AppProject
// suitable for OneSignal provisioning integration testing. Mirrors the
// setupProvisionTest helper in revenuecat_provision_integration_test.go.
func setupOneSignalProvisionTest(t *testing.T, secrets map[string]string) (*Controller, *database.Database, *models.AppProject, *models.User) {
	t.Helper()
	db := database.NewTestDB(t,
		(*models.User)(nil),
		(*models.Organization)(nil),
		(*models.AppProject)(nil),
		(*models.Project)(nil),
	)
	c, _ := NewTestController(t, db, TestControllerOptions{Secrets: secrets})

	ctx := context.Background()
	user := &models.User{ID: uuid.New(), Name: "Owner", Email: "owner-" + uuid.NewString()[:8] + "@example.com", Password: "x"}
	if _, err := db.DB.NewInsert().Model(user).Exec(ctx); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	org := &models.Organization{ID: uuid.New(), Name: "Acme", Slug: "acme-os-" + uuid.NewString()[:8], OwnerID: user.ID, IsPersonal: true}
	if _, err := db.DB.NewInsert().Model(org).Exec(ctx); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	project := &models.AppProject{
		ID: uuid.New(), UserID: user.ID, OrganizationID: org.ID,
		Name: "Push Test", Slug: "push-test-" + uuid.NewString()[:8], Prompt: "x",
		ProjectType: models.ProjectTypeMobileAppNative,
	}
	if _, err := db.DB.NewInsert().Model(project).Exec(ctx); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	return c, db, project, user
}

// TestProvisionOneSignalApp_HappyPath_PersistsIDs exercises the full provisioning
// flow against a fake OneSignal server, asserts that the IDs land on the v1
// shadow row, then calls provisionOneSignalApp AGAIN to verify idempotency:
// the second call must NOT hit the OS API.
func TestProvisionOneSignalApp_HappyPath_PersistsIDs(t *testing.T) {
	c, _, project, user := setupOneSignalProvisionTest(t, map[string]string{
		"ONESIGNAL_ORG_ID":               "org-id-fake",
		"ONESIGNAL_ORG_API_KEY":          "org-key-fake",
		"FCM_V1_SERVICE_ACCOUNT_JSON_B64": "",
	})

	server, counters := newFakeOSServer(t)
	defer server.Close()
	restore := external.SetOneSignalTestEndpoint(server.URL)
	defer restore()

	if err := c.provisionOneSignalApp(context.Background(), project); err != nil {
		t.Fatalf("first provision: %v", err)
	}

	v1, err := c.DB.GetProjectByUserAndURL(user.ID.String(), "expo://"+project.Slug)
	if err != nil || v1 == nil {
		t.Fatalf("v1 shadow not found: %v", err)
	}
	if v1.OneSignalAppID != "os-app-real-1" {
		t.Errorf("os app id: got %q, want os-app-real-1", v1.OneSignalAppID)
	}
	if v1.OneSignalAppToken != "tok-real-1" {
		t.Errorf("os token: got %q, want tok-real-1", v1.OneSignalAppToken)
	}

	if counters.createApp.Load() != 1 {
		t.Errorf("createApp calls: got %d, want 1", counters.createApp.Load())
	}
	if counters.createTok.Load() != 1 {
		t.Errorf("createTok calls: got %d, want 1", counters.createTok.Load())
	}

	// Second call — must be idempotent. Reset counters first.
	counters.createApp.Store(0)
	counters.createTok.Store(0)
	if err := c.provisionOneSignalApp(context.Background(), project); err != nil {
		t.Fatalf("second provision: %v", err)
	}
	if counters.createApp.Load() != 0 {
		t.Errorf("idempotency: got %d createApp calls on second invocation, want 0", counters.createApp.Load())
	}
}

// TestProvisionOneSignalApp_GracefulDegradeWhenKeysUnset verifies that missing
// ONESIGNAL_ORG_ID / ONESIGNAL_ORG_API_KEY secrets result in a no-op rather
// than an error. This is the path dev / staging boxes without OS credentials take.
func TestProvisionOneSignalApp_GracefulDegradeWhenKeysUnset(t *testing.T) {
	c, _, project, user := setupOneSignalProvisionTest(t, nil)

	if err := c.provisionOneSignalApp(context.Background(), project); err != nil {
		t.Fatalf("expected nil error when keys unset, got %v", err)
	}
	// No v1 shadow should have been created.
	v1, _ := c.DB.GetProjectByUserAndURL(user.ID.String(), "expo://"+project.Slug)
	if v1 != nil && v1.ID != uuid.Nil {
		t.Errorf("expected no v1 shadow row when keys unset, got %v", v1)
	}
}
