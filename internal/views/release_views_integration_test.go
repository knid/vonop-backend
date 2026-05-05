//go:build integration

// Integration tests for the P1 #6 fix (mobile audit, 2026-05-04):
// release-management HTTP routes were never registered. The controller
// methods existed but no view + chi mount, so the dashboard had no way to
// list releases, halt phased rollouts, or trigger rollbacks. These tests
// drive the new release_views.go through a real chi router so the wiring
// is exercised end-to-end.
package views

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
)

// seedReleaseFixtures creates a User+Org+OrganizationMember+AppProject and
// the (token, otherUserToken) pair that drives the auth gate. Returns ids
// the tests use to mount routes + assert on.
type releaseFixture struct {
	user    *models.User
	other   *models.User
	org     *models.Organization
	project *models.AppProject
	token   string
	otherTk string
}

func seedReleaseFixtures(t *testing.T, db *database.Database) *releaseFixture {
	t.Helper()
	ctx := context.Background()

	user := &models.User{ID: uuid.New(), Email: "owner-" + uuid.NewString()[:8] + "@e.com", Name: "Owner", Password: "x"}
	if _, err := db.DB.NewInsert().Model(user).Exec(ctx); err != nil {
		t.Fatalf("insert owner: %v", err)
	}
	other := &models.User{ID: uuid.New(), Email: "other-" + uuid.NewString()[:8] + "@e.com", Name: "Other", Password: "x"}
	if _, err := db.DB.NewInsert().Model(other).Exec(ctx); err != nil {
		t.Fatalf("insert other: %v", err)
	}

	tok := &models.Token{ID: uuid.New(), UserID: user.ID, String: "tok-" + uuid.NewString(), ExpiredAt: time.Now().Add(24 * time.Hour)}
	if _, err := db.DB.NewInsert().Model(tok).Exec(ctx); err != nil {
		t.Fatalf("insert owner token: %v", err)
	}
	otherTok := &models.Token{ID: uuid.New(), UserID: other.ID, String: "tok-" + uuid.NewString(), ExpiredAt: time.Now().Add(24 * time.Hour)}
	if _, err := db.DB.NewInsert().Model(otherTok).Exec(ctx); err != nil {
		t.Fatalf("insert other token: %v", err)
	}

	org := &models.Organization{ID: uuid.New(), Name: "Acme", Slug: "acme-" + uuid.NewString()[:8], OwnerID: user.ID}
	if _, err := db.DB.NewInsert().Model(org).Exec(ctx); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	if _, err := db.DB.NewInsert().Model(&models.OrganizationMember{
		OrganizationID: org.ID, UserID: user.ID, Role: "owner",
	}).Exec(ctx); err != nil {
		t.Fatalf("insert member: %v", err)
	}

	project := &models.AppProject{
		ID: uuid.New(), UserID: user.ID, OrganizationID: org.ID,
		Name: "X", Slug: "x-" + uuid.NewString()[:8], Prompt: "n",
		ProjectType: models.ProjectTypeMobileAppNative, Status: "draft",
	}
	if _, err := db.DB.NewInsert().Model(project).Exec(ctx); err != nil {
		t.Fatalf("insert project: %v", err)
	}

	return &releaseFixture{
		user: user, other: other, org: org, project: project,
		token: tok.String, otherTk: otherTok.String,
	}
}

// makeReleaseRouter mounts the release routes the same way cmd/main.go does.
func makeReleaseRouter(v *HttpViews) *chi.Mux {
	r := chi.NewRouter()
	r.Route("/v2/apps/{appId}/releases", func(r chi.Router) {
		r.Get("/", v.ListReleases)
		r.Get("/current", v.GetCurrentLiveRelease)
		r.Post("/rollback", v.RollbackRelease)
		r.Post("/{releaseID}/halt", v.HaltPhasedRollout)
	})
	return r
}

// newReleaseTestDB spins a Postgres + creates the unique index that prod
// migrations install, so UpsertRelease works.
func newReleaseTestDB(t *testing.T) *database.Database {
	t.Helper()
	db := database.NewTestDB(t,
		(*models.User)(nil),
		(*models.Token)(nil),
		(*models.Organization)(nil),
		(*models.OrganizationMember)(nil),
		(*models.AppProject)(nil),
		(*models.Release)(nil),
	)
	if _, err := db.DB.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uniq_releases_project_platform_track_version
		ON releases (app_project_id, platform, track, version_code) WHERE deleted_at IS NULL`); err != nil {
		t.Fatalf("create unique index: %v", err)
	}
	return db
}

// TestListReleases_ReturnsRowsForProject covers the happy path: the GET
// endpoint surfaces the rows the polling cron / submit pipeline writes, in
// newest-first order, scoped to the project in the URL.
func TestListReleases_ReturnsRowsForProject(t *testing.T) {
	db := newReleaseTestDB(t)
	c, _ := controllers.NewTestController(t, db, controllers.TestControllerOptions{})
	v := &HttpViews{Controller: c}
	router := makeReleaseRouter(v)
	fx := seedReleaseFixtures(t, db)
	ctx := context.Background()

	// Two iOS releases + one Android.
	for i, vc := range []int{1, 2} {
		_, err := c.CreateReleaseFromSubmit(ctx, controllers.CreateReleaseFromSubmitInput{
			AppProjectID:   fx.project.ID,
			OrganizationID: fx.org.ID,
			Platform:       models.ReleasePlatformIOS,
			Track:          models.ReleaseTrackTestFlight,
			VersionCode:    vc,
		})
		if err != nil {
			t.Fatalf("seed ios release %d: %v", i, err)
		}
	}
	if _, err := c.CreateReleaseFromSubmit(ctx, controllers.CreateReleaseFromSubmitInput{
		AppProjectID:   fx.project.ID,
		OrganizationID: fx.org.ID,
		Platform:       models.ReleasePlatformAndroid,
		Track:          models.ReleaseTrackInternal,
		VersionCode:    1,
	}); err != nil {
		t.Fatalf("seed android release: %v", err)
	}

	t.Run("returns all releases for project", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v2/apps/"+fx.project.ID.String()+"/releases", nil)
		req.Header.Set("Authorization", "Bearer "+fx.token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
		}
		var got []models.Release
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("unmarshal: %v body=%s", err, w.Body.String())
		}
		if len(got) != 3 {
			t.Errorf("expected 3 releases, got %d", len(got))
		}
	})

	t.Run("filters by platform=ios", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v2/apps/"+fx.project.ID.String()+"/releases?platform=ios", nil)
		req.Header.Set("Authorization", "Bearer "+fx.token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		var got []models.Release
		_ = json.Unmarshal(w.Body.Bytes(), &got)
		if len(got) != 2 {
			t.Errorf("expected 2 ios releases, got %d", len(got))
		}
		for _, rel := range got {
			if rel.Platform != "ios" {
				t.Errorf("non-ios release leaked through filter: %+v", rel)
			}
		}
	})

	t.Run("400 on bad platform filter", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v2/apps/"+fx.project.ID.String()+"/releases?platform=windows", nil)
		req.Header.Set("Authorization", "Bearer "+fx.token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	t.Run("404 when caller doesn't have access", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v2/apps/"+fx.project.ID.String()+"/releases", nil)
		req.Header.Set("Authorization", "Bearer "+fx.otherTk)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404 (other user), got %d", w.Code)
		}
	})

	t.Run("401 without token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v2/apps/"+fx.project.ID.String()+"/releases", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", w.Code)
		}
	})
}

// TestHaltPhasedRollout_IOSAndroidBranching: iOS halt updates the local row
// to status=paused; Android returns 422 with ErrReleaseNotImplemented.
//
// Note: the iOS path requires no upstream call here because StoreReleaseID
// is empty — haltApplePhasedRollout returns early without touching ASC, but
// the local SetReleasePhasedState side-effect still runs first. We assert on
// the local persisted state, not the upstream response.
func TestHaltPhasedRollout_IOSAndroidBranching(t *testing.T) {
	db := newReleaseTestDB(t)
	c, _ := controllers.NewTestController(t, db, controllers.TestControllerOptions{})
	v := &HttpViews{Controller: c}
	router := makeReleaseRouter(v)
	fx := seedReleaseFixtures(t, db)
	ctx := context.Background()

	iosRel, err := c.CreateReleaseFromSubmit(ctx, controllers.CreateReleaseFromSubmitInput{
		AppProjectID:   fx.project.ID,
		OrganizationID: fx.org.ID,
		Platform:       models.ReleasePlatformIOS,
		Track:          models.ReleaseTrackProduction,
		VersionCode:    7,
	})
	if err != nil {
		t.Fatalf("seed ios release: %v", err)
	}
	andRel, err := c.CreateReleaseFromSubmit(ctx, controllers.CreateReleaseFromSubmitInput{
		AppProjectID:   fx.project.ID,
		OrganizationID: fx.org.ID,
		Platform:       models.ReleasePlatformAndroid,
		Track:          models.ReleaseTrackProduction,
		VersionCode:    7,
	})
	if err != nil {
		t.Fatalf("seed android release: %v", err)
	}

	t.Run("ios halt persists paused state locally (5xx allowed when ASC unreachable)", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v2/apps/"+fx.project.ID.String()+"/releases/"+iosRel.ID.String()+"/halt", nil)
		req.Header.Set("Authorization", "Bearer "+fx.token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		// HaltPhasedRollout always sets local state to paused before the
		// upstream call. With StoreReleaseID empty the upstream call returns
		// an error, which the view maps to 500. Either way the row must show
		// status=paused after the call.
		if w.Code != http.StatusOK && w.Code != http.StatusInternalServerError {
			t.Errorf("expected 200 or 500, got %d body=%s", w.Code, w.Body.String())
		}
		got, err := c.DB.GetRelease(ctx, iosRel.ID)
		if err != nil {
			t.Fatalf("reload ios release: %v", err)
		}
		if got.Status != models.ReleaseStatusPaused {
			t.Errorf("local status: got %q want paused", got.Status)
		}
	})

	t.Run("android halt returns 422 not_implemented", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v2/apps/"+fx.project.ID.String()+"/releases/"+andRel.ID.String()+"/halt", nil)
		req.Header.Set("Authorization", "Bearer "+fx.token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusUnprocessableEntity {
			t.Errorf("expected 422 for android halt, got %d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("404 on unknown release id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v2/apps/"+fx.project.ID.String()+"/releases/"+uuid.NewString()+"/halt", nil)
		req.Header.Set("Authorization", "Bearer "+fx.token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", w.Code)
		}
	})

	t.Run("400 on malformed release id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v2/apps/"+fx.project.ID.String()+"/releases/not-a-uuid/halt", nil)
		req.Header.Set("Authorization", "Bearer "+fx.token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	t.Run("404 when releases belongs to another project (cross-project guard)", func(t *testing.T) {
		// Create a second project + a release on it. Hitting the first
		// project's endpoint with the second project's release id must 404.
		other := &models.AppProject{
			ID: uuid.New(), UserID: fx.user.ID, OrganizationID: fx.org.ID,
			Name: "Y", Slug: "y-" + uuid.NewString()[:8], Prompt: "n",
			ProjectType: models.ProjectTypeMobileAppNative, Status: "draft",
		}
		if _, err := db.DB.NewInsert().Model(other).Exec(ctx); err != nil {
			t.Fatalf("seed other project: %v", err)
		}
		otherRel, err := c.CreateReleaseFromSubmit(ctx, controllers.CreateReleaseFromSubmitInput{
			AppProjectID:   other.ID,
			OrganizationID: fx.org.ID,
			Platform:       models.ReleasePlatformIOS,
			Track:          models.ReleaseTrackProduction,
			VersionCode:    99,
		})
		if err != nil {
			t.Fatalf("seed other release: %v", err)
		}

		req := httptest.NewRequest(http.MethodPost, "/v2/apps/"+fx.project.ID.String()+"/releases/"+otherRel.ID.String()+"/halt", nil)
		req.Header.Set("Authorization", "Bearer "+fx.token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404 cross-project, got %d body=%s", w.Code, w.Body.String())
		}
	})
}

// TestRollbackRelease_HappyAndErrorPaths drives the rollback view.
func TestRollbackRelease_HappyAndErrorPaths(t *testing.T) {
	db := newReleaseTestDB(t)
	c, _ := controllers.NewTestController(t, db, controllers.TestControllerOptions{})
	v := &HttpViews{Controller: c}
	router := makeReleaseRouter(v)
	fx := seedReleaseFixtures(t, db)
	ctx := context.Background()

	// Two production iOS releases on the same project — older "target",
	// newer "live".
	target, err := c.CreateReleaseFromSubmit(ctx, controllers.CreateReleaseFromSubmitInput{
		AppProjectID:   fx.project.ID,
		OrganizationID: fx.org.ID,
		Platform:       models.ReleasePlatformIOS,
		Track:          models.ReleaseTrackProduction,
		VersionCode:    1,
	})
	if err != nil {
		t.Fatalf("seed target: %v", err)
	}
	live, err := c.CreateReleaseFromSubmit(ctx, controllers.CreateReleaseFromSubmitInput{
		AppProjectID:   fx.project.ID,
		OrganizationID: fx.org.ID,
		Platform:       models.ReleasePlatformIOS,
		Track:          models.ReleaseTrackProduction,
		VersionCode:    2,
	})
	if err != nil {
		t.Fatalf("seed live: %v", err)
	}

	t.Run("happy path", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{
			"from_release_id":   live.ID.String(),
			"target_release_id": target.ID.String(),
		})
		req := httptest.NewRequest(http.MethodPost, "/v2/apps/"+fx.project.ID.String()+"/releases/rollback",
			bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+fx.token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
		}
		// Verify both rows updated correctly.
		gotLive, _ := c.DB.GetRelease(ctx, live.ID)
		if gotLive.Status != models.ReleaseStatusRolledBack {
			t.Errorf("live.Status: got %q want rolled_back", gotLive.Status)
		}
		gotTarget, _ := c.DB.GetRelease(ctx, target.ID)
		if gotTarget.RolledBackFromReleaseID == nil || *gotTarget.RolledBackFromReleaseID != live.ID {
			t.Errorf("target.RolledBackFromReleaseID: got %v want %v", gotTarget.RolledBackFromReleaseID, live.ID)
		}
	})

	t.Run("400 on missing body fields", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v2/apps/"+fx.project.ID.String()+"/releases/rollback",
			bytes.NewReader([]byte(`{"from_release_id":""}`)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+fx.token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("404 on unknown release ids", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{
			"from_release_id":   uuid.NewString(),
			"target_release_id": uuid.NewString(),
		})
		req := httptest.NewRequest(http.MethodPost, "/v2/apps/"+fx.project.ID.String()+"/releases/rollback",
			bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+fx.token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", w.Code)
		}
	})

	t.Run("404 cross-project (release belongs to a different app)", func(t *testing.T) {
		other := &models.AppProject{
			ID: uuid.New(), UserID: fx.user.ID, OrganizationID: fx.org.ID,
			Name: "Z", Slug: "z-" + uuid.NewString()[:8], Prompt: "n",
			ProjectType: models.ProjectTypeMobileAppNative, Status: "draft",
		}
		if _, err := db.DB.NewInsert().Model(other).Exec(ctx); err != nil {
			t.Fatalf("seed other project: %v", err)
		}
		otherRel, err := c.CreateReleaseFromSubmit(ctx, controllers.CreateReleaseFromSubmitInput{
			AppProjectID:   other.ID,
			OrganizationID: fx.org.ID,
			Platform:       models.ReleasePlatformIOS,
			Track:          models.ReleaseTrackProduction,
			VersionCode:    1,
		})
		if err != nil {
			t.Fatalf("seed other release: %v", err)
		}
		body, _ := json.Marshal(map[string]string{
			"from_release_id":   otherRel.ID.String(), // belongs to `other`, not fx.project
			"target_release_id": target.ID.String(),
		})
		req := httptest.NewRequest(http.MethodPost, "/v2/apps/"+fx.project.ID.String()+"/releases/rollback",
			bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+fx.token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404 cross-project, got %d body=%s", w.Code, w.Body.String())
		}
	})
}

// TestGetCurrentLiveRelease covers the platform-required + present/absent
// branches.
func TestGetCurrentLiveRelease(t *testing.T) {
	db := newReleaseTestDB(t)
	c, _ := controllers.NewTestController(t, db, controllers.TestControllerOptions{})
	v := &HttpViews{Controller: c}
	router := makeReleaseRouter(v)
	fx := seedReleaseFixtures(t, db)
	ctx := context.Background()

	t.Run("400 when platform query missing", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v2/apps/"+fx.project.ID.String()+"/releases/current", nil)
		req.Header.Set("Authorization", "Bearer "+fx.token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	t.Run("404 when nothing live yet", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v2/apps/"+fx.project.ID.String()+"/releases/current?platform=ios", nil)
		req.Header.Set("Authorization", "Bearer "+fx.token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("returns the live release", func(t *testing.T) {
		rel, err := c.CreateReleaseFromSubmit(ctx, controllers.CreateReleaseFromSubmitInput{
			AppProjectID:   fx.project.ID,
			OrganizationID: fx.org.ID,
			Platform:       models.ReleasePlatformIOS,
			Track:          models.ReleaseTrackProduction,
			VersionCode:    1,
		})
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
		if _, err := c.MarkReleaseLive(ctx, rel.ID, "READY_FOR_SALE", time.Now().UTC()); err != nil {
			t.Fatalf("mark live: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/v2/apps/"+fx.project.ID.String()+"/releases/current?platform=ios", nil)
		req.Header.Set("Authorization", "Bearer "+fx.token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
		}
		var got models.Release
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got.ID != rel.ID {
			t.Errorf("got release %s, want %s", got.ID, rel.ID)
		}
	})
}
