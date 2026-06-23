//go:build integration

package views

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/storage"
)

func seedBuildDownloadAuthFixtures(t *testing.T, db *database.Database) (ownerToken string, otherToken string, buildID uuid.UUID) {
	t.Helper()
	ctx := context.Background()

	owner := &models.User{ID: uuid.New(), Email: "owner-" + uuid.NewString()[:8] + "@e.com", Name: "Owner", Password: "x"}
	if _, err := db.DB.NewInsert().Model(owner).Exec(ctx); err != nil {
		t.Fatalf("insert owner: %v", err)
	}
	other := &models.User{ID: uuid.New(), Email: "other-" + uuid.NewString()[:8] + "@e.com", Name: "Other", Password: "x"}
	if _, err := db.DB.NewInsert().Model(other).Exec(ctx); err != nil {
		t.Fatalf("insert other: %v", err)
	}

	ownerTok := &models.Token{ID: uuid.New(), UserID: owner.ID, String: "tok-" + uuid.NewString(), ExpiredAt: time.Now().Add(24 * time.Hour)}
	if _, err := db.DB.NewInsert().Model(ownerTok).Exec(ctx); err != nil {
		t.Fatalf("insert owner token: %v", err)
	}
	otherTok := &models.Token{ID: uuid.New(), UserID: other.ID, String: "tok-" + uuid.NewString(), ExpiredAt: time.Now().Add(24 * time.Hour)}
	if _, err := db.DB.NewInsert().Model(otherTok).Exec(ctx); err != nil {
		t.Fatalf("insert other token: %v", err)
	}

	org := &models.Organization{
		ID: uuid.New(), Name: "Personal " + uuid.NewString()[:6],
		Slug: "org-" + uuid.NewString()[:8], OwnerID: owner.ID, IsPersonal: true,
	}
	if _, err := db.DB.NewInsert().Model(org).Exec(ctx); err != nil {
		t.Fatalf("insert org: %v", err)
	}

	appProject := &models.AppProject{
		ID: uuid.New(), UserID: owner.ID, OrganizationID: org.ID,
		Name:   "App " + uuid.NewString()[:6],
		Slug:   "app-" + uuid.NewString()[:8],
		Prompt: "test", ProjectType: models.ProjectTypeMobileAppNative,
	}
	if _, err := db.DB.NewInsert().Model(appProject).Exec(ctx); err != nil {
		t.Fatalf("insert app project: %v", err)
	}

	build := &models.Build{
		ID: uuid.New(), AppProjectID: appProject.ID,
		Title: "T", Description: "D", Icon: "i", SplashScreenColor: "#fff",
		NavBarStyle: "default", BuildNumber: 1, VersionString: "1.0.0",
	}
	if _, err := db.DB.NewInsert().Model(build).Exec(ctx); err != nil {
		t.Fatalf("insert build: %v", err)
	}

	return ownerTok.String, otherTok.String, build.ID
}

func makeStoragePresignRouter(v *HttpViews) *chi.Mux {
	r := chi.NewRouter()
	r.Get("/v1/storage/builds/aab/{buildId}", v.DownloadAABFromS3Presign)
	r.Get("/v1/storage/builds/ipa/{buildId}", v.DownloadIPAFromS3Presign)
	return r
}

func TestDownloadBuildArtifactPresign_RejectsForeignUser(t *testing.T) {
	db := database.NewTestDB(t,
		(*models.User)(nil),
		(*models.Token)(nil),
		(*models.Organization)(nil),
		(*models.AppProject)(nil),
		(*models.Build)(nil),
	)

	c, _ := controllers.NewTestController(t, db, controllers.TestControllerOptions{})
	s3Storage, err := storage.NewS3Storage("us-east-1", "test-bucket", "access", "secret")
	if err != nil {
		t.Fatalf("new s3 storage: %v", err)
	}
	c.AssetsStorage = s3Storage
	v := &HttpViews{Controller: c}
	router := makeStoragePresignRouter(v)

	_, otherToken, buildID := seedBuildDownloadAuthFixtures(t, db)

	req := httptest.NewRequest(http.MethodGet, "/v1/storage/builds/aab/"+buildID.String(), nil)
	req.Header.Set("Authorization", "Bearer "+otherToken)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for foreign build download, got %d body=%s", w.Code, w.Body.String())
	}
}
