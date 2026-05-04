//go:build integration

package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/services/fly"
	"github.com/knid/wemob/internal/utils"
)

// fakeFlyInspector implements FlyInspector for tests.
type fakeFlyInspector struct {
	machine    fly.MachineStatus
	machineErr error
	logs       []fly.LogLine
	logsErr    error

	machineCalls int
	logCalls     int
}

func (f *fakeFlyInspector) GetMachineStatus(_ string) (fly.MachineStatus, error) {
	f.machineCalls++
	return f.machine, f.machineErr
}

func (f *fakeFlyInspector) GetLogs(_ string, _ time.Time, _ string) ([]fly.LogLine, error) {
	f.logCalls++
	return f.logs, f.logsErr
}

// inspectorTestEnv is the shared setup for the backend-inspector integration tests.
type inspectorTestEnv struct {
	c        *Controller
	db       *database.Database
	user     *models.User
	project  *models.AppProject
	tokenStr string
}

func newInspectorEnv(t *testing.T, withRuntimeApp bool) *inspectorTestEnv {
	t.Helper()
	db := database.NewTestDB(t,
		(*models.User)(nil),
		(*models.Organization)(nil),
		(*models.AppProject)(nil),
		(*models.RuntimeApp)(nil),
		(*models.AppDeployment)(nil),
		(*models.Token)(nil),
	)
	c, _ := NewTestController(t, db, TestControllerOptions{})
	ctx := context.Background()

	user := &models.User{ID: uuid.New(), Email: "u@example.com", Name: "U", Password: "x"}
	if _, err := db.DB.NewInsert().Model(user).Exec(ctx); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	org := &models.Organization{ID: uuid.New(), Name: "A", Slug: "ins-" + uuid.NewString()[:8], OwnerID: user.ID}
	if _, err := db.DB.NewInsert().Model(org).Exec(ctx); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	project := &models.AppProject{
		ID: uuid.New(), UserID: user.ID, OrganizationID: org.ID,
		Name: "X", Slug: "ins-" + uuid.NewString()[:8], Prompt: "n/a",
		ProjectType: models.ProjectTypeMobileAppNative, Status: "draft",
	}
	if _, err := db.DB.NewInsert().Model(project).Exec(ctx); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if withRuntimeApp {
		if _, err := db.DB.NewInsert().Model(&models.RuntimeApp{
			AppProjectID:  project.ID,
			GiteaRepoName: project.Slug,
			FlyAppName:    "wemob-" + project.Slug,
			Framework:     "nextjs",
		}).Exec(ctx); err != nil {
			t.Fatalf("insert runtime app: %v", err)
		}
	}

	tokenStr := "tok-" + uuid.NewString()
	if _, err := db.DB.NewInsert().Model(&models.Token{
		UserID:    user.ID,
		String:    tokenStr,
		ExpiredAt: time.Now().Add(24 * time.Hour),
	}).Exec(ctx); err != nil {
		t.Fatalf("insert token: %v", err)
	}

	return &inspectorTestEnv{c: c, db: db, user: user, project: project, tokenStr: tokenStr}
}

// --- Controller-level tests -------------------------------------------------

func TestGetBackendHealth_GreenWhenStartedAndDeploySucceeded(t *testing.T) {
	env := newInspectorEnv(t, true)
	env.c.FlyInspector = &fakeFlyInspector{
		machine: fly.MachineStatus{State: "started", Region: "ams"},
	}
	if _, err := env.db.DB.NewInsert().Model(&models.AppDeployment{
		AppProjectID: env.project.ID,
		UserID:       env.user.ID,
		Platform:     "web",
		Provider:     "fly",
		Status:       "deployed",
		DeployedURL:  "https://x.wemob.app",
	}).Exec(context.Background()); err != nil {
		t.Fatalf("insert deployment: %v", err)
	}

	got, err := env.c.GetBackendHealth(context.Background(), env.project)
	if err != nil {
		t.Fatalf("GetBackendHealth: %v", err)
	}
	if got.DerivedColor != "green" {
		t.Errorf("color = %q, want green; full: %+v", got.DerivedColor, got)
	}
	if !got.LastDeployOK {
		t.Errorf("last_deploy_ok = false; want true")
	}
}

func TestGetBackendHealth_GrayWhenNoRuntimeApp(t *testing.T) {
	env := newInspectorEnv(t, false)

	got, err := env.c.GetBackendHealth(context.Background(), env.project)
	if err != nil {
		t.Fatalf("GetBackendHealth: %v", err)
	}
	if got.HasBackend || got.DerivedColor != "gray" {
		t.Errorf("expected gray + has_backend=false, got %+v", got)
	}
}

func TestGetBackendHealth_YellowWhenNeverDeployed(t *testing.T) {
	env := newInspectorEnv(t, true)
	env.c.FlyInspector = &fakeFlyInspector{
		machine: fly.MachineStatus{State: "started"},
	}

	got, err := env.c.GetBackendHealth(context.Background(), env.project)
	if err != nil {
		t.Fatalf("GetBackendHealth: %v", err)
	}
	if got.DerivedColor != "yellow" {
		t.Errorf("color = %q, want yellow (never deployed); full: %+v", got.DerivedColor, got)
	}
}

func TestGetBackendHealth_RedWhenLastDeployFailed(t *testing.T) {
	env := newInspectorEnv(t, true)
	env.c.FlyInspector = &fakeFlyInspector{
		machine: fly.MachineStatus{State: "started"},
	}
	if _, err := env.db.DB.NewInsert().Model(&models.AppDeployment{
		AppProjectID: env.project.ID,
		UserID:       env.user.ID,
		Platform:     "web",
		Provider:     "fly",
		Status:       "failed",
		DeployedURL:  "",
	}).Exec(context.Background()); err != nil {
		t.Fatalf("insert deployment: %v", err)
	}

	got, err := env.c.GetBackendHealth(context.Background(), env.project)
	if err != nil {
		t.Fatalf("GetBackendHealth: %v", err)
	}
	if got.DerivedColor != "red" {
		t.Errorf("color = %q, want red (deploy failed); full: %+v", got.DerivedColor, got)
	}
}

func TestGetBackendRoutes_NoRepoReturnsEmpty(t *testing.T) {
	env := newInspectorEnv(t, true)
	// GitRepoService is nil — controller must short-circuit to empty without erroring.
	got, err := env.c.GetBackendRoutes(context.Background(), env.project)
	if err != nil {
		t.Fatalf("GetBackendRoutes: %v", err)
	}
	if got == nil {
		t.Errorf("got nil slice, want empty (non-nil) slice")
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0; %+v", len(got), got)
	}
}

func TestGetBackendLogs_NoRuntimeAppReturnsEmpty(t *testing.T) {
	env := newInspectorEnv(t, false)
	env.c.FlyInspector = &fakeFlyInspector{
		logs: []fly.LogLine{{Timestamp: "2026-05-04T12:00:00Z", Level: "info", Message: "should not appear"}},
	}

	got, err := env.c.GetBackendLogs(context.Background(), env.project, time.Time{}, "")
	if err != nil {
		t.Fatalf("GetBackendLogs: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty without runtime app, got %+v", got)
	}
}

func TestGetBackendLogs_DelegatesToFlyInspector(t *testing.T) {
	env := newInspectorEnv(t, true)
	insp := &fakeFlyInspector{
		logs: []fly.LogLine{
			{Timestamp: "2026-05-04T12:00:00Z", Level: "info", Message: "hello"},
			{Timestamp: "2026-05-04T12:00:01Z", Level: "error", Message: "boom"},
		},
	}
	env.c.FlyInspector = insp

	got, err := env.c.GetBackendLogs(context.Background(), env.project, time.Time{}, "")
	if err != nil {
		t.Fatalf("GetBackendLogs: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
	if insp.logCalls != 1 {
		t.Errorf("expected 1 inspector call, got %d", insp.logCalls)
	}
	if got[1].Message != "boom" {
		t.Errorf("got[1].Message = %q, want boom", got[1].Message)
	}
}

// --- HTTP-level happy-path test --------------------------------------------
//
// We wire minimal inline handlers here to avoid an import cycle:
// the controllers package cannot import views (which imports controllers).
// The handlers mirror the views exactly — auth via GetUserFromReq, project
// lookup via GetAppProject, then the controller method.

// inlineGetBackendHealth is a slimmed-down copy of views.HttpViews.GetBackendHealth,
// used only in tests to avoid the controllers → views import cycle.
func inlineGetBackendHealth(c *Controller) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := c.GetUserFromReq(r)
		if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		appID := chi.URLParam(r, "appId")
		project, err := c.GetAppProject(appID, user)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		out, err := c.GetBackendHealth(r.Context(), project)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		utils.JSONResponse(w, out, http.StatusOK)
	}
}

// inlineGetBackendLogs mirrors views.HttpViews.GetBackendLogs for the auth-rejection test.
func inlineGetBackendLogs(c *Controller) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := c.GetUserFromReq(r)
		if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		appID := chi.URLParam(r, "appId")
		project, err := c.GetAppProject(appID, user)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		sinceStr := strings.TrimSpace(r.URL.Query().Get("since"))
		var since time.Time
		if sinceStr != "" {
			if d, err := time.ParseDuration(sinceStr); err == nil && d > 0 {
				since = time.Now().Add(-d)
			}
		}
		level := strings.TrimSpace(r.URL.Query().Get("level"))
		lines, err := c.GetBackendLogs(r.Context(), project, since, level)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		utils.JSONResponse(w, responses.BackendLogsResponse{Lines: lines}, http.StatusOK)
	}
}

func TestGetBackendHealthEndpoint_HTTPHappyPath(t *testing.T) {
	env := newInspectorEnv(t, true)
	env.c.FlyInspector = &fakeFlyInspector{
		machine: fly.MachineStatus{State: "started", Region: "ams"},
	}
	if _, err := env.db.DB.NewInsert().Model(&models.AppDeployment{
		AppProjectID: env.project.ID,
		UserID:       env.user.ID,
		Platform:     "web",
		Provider:     "fly",
		Status:       "deployed",
		DeployedURL:  "https://x.wemob.app",
	}).Exec(context.Background()); err != nil {
		t.Fatalf("insert deployment: %v", err)
	}

	r := chi.NewRouter()
	r.Get("/v2/apps/{appId}/backend/health", inlineGetBackendHealth(env.c))

	req := httptest.NewRequest("GET", "/v2/apps/"+env.project.ID.String()+"/backend/health", nil)
	req.Header.Set("Authorization", "Bearer "+env.tokenStr)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp responses.BackendHealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.HasBackend {
		t.Errorf("has_backend = false; want true")
	}
	if resp.DerivedColor != "green" {
		t.Errorf("derived_color = %q, want green", resp.DerivedColor)
	}
}

func TestGetBackendLogsEndpoint_HTTP_RejectsBadAuth(t *testing.T) {
	env := newInspectorEnv(t, true)

	r := chi.NewRouter()
	r.Get("/v2/apps/{appId}/backend/logs", inlineGetBackendLogs(env.c))

	// No Authorization header — expect 401.
	req := httptest.NewRequest("GET", "/v2/apps/"+env.project.ID.String()+"/backend/logs", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body = %s", rec.Code, rec.Body.String())
	}
}
