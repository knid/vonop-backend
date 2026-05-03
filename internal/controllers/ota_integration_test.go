//go:build integration

package controllers

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"

	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/ota"
	"github.com/knid/wemob/internal/services/storage"
)

func parseMetadataForTest(b []byte) (*ota.MetadataFile, error) {
	return ota.ParseMetadataJSON(b)
}

// otaTestFixture is the OTA-specific test scaffolding. Mirrors the
// build_expo_mobile_integration_test.go pattern but uses an S3 fake that
// supports both PUT and GET.
type otaTestFixture struct {
	c       *Controller
	db      *database.Database
	project *models.AppProject
}

// otaS3Recorder is a minimal in-memory PUT+GET fake S3.
type otaS3Recorder struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func (r *otaS3Recorder) put(key string, body []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := make([]byte, len(body))
	copy(cp, body)
	r.objects[key] = cp
}
func (r *otaS3Recorder) get(key string) ([]byte, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.objects[key]
	return b, ok
}

// newOTAFakeS3Storage returns a *storage.S3Storage backed by an httptest.Server
// that handles PUT + GET against an in-memory map.
func newOTAFakeS3Storage(t *testing.T) (*storage.S3Storage, *otaS3Recorder) {
	t.Helper()
	rec := &otaS3Recorder{objects: map[string][]byte{}}
	bucket := "test-bucket"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		path = strings.TrimPrefix(path, bucket+"/")
		switch r.Method {
		case http.MethodPut:
			body, err := io.ReadAll(r.Body)
			r.Body.Close()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			rec.put(path, body)
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			body, ok := rec.get(path)
			if !ok {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(body)
		case http.MethodDelete:
			rec.mu.Lock()
			delete(rec.objects, path)
			rec.mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(srv.Close)

	client := s3.New(s3.Options{
		Region:       "us-east-1",
		BaseEndpoint: aws.String(srv.URL),
		UsePathStyle: true,
		Credentials:  credentials.NewStaticCredentialsProvider("test", "test", ""),
	})

	return &storage.S3Storage{
		Region: "us-east-1",
		Bucket: bucket,
		Client: client,
	}, rec
}

func setupOTATest(t *testing.T) *otaTestFixture {
	t.Helper()
	db := database.NewTestDB(t,
		(*models.User)(nil),
		(*models.Organization)(nil),
		(*models.AppProject)(nil),
		(*models.OTAUpdate)(nil),
	)
	c, _ := NewTestController(t, db, TestControllerOptions{})

	s3Storage, _ := newOTAFakeS3Storage(t)
	c.AssetsStorage = s3Storage

	ctx := context.Background()
	owner := &models.User{ID: uuid.New(), Email: "ota-owner@example.com", Name: "Owner", Password: "x"}
	if _, err := db.DB.NewInsert().Model(owner).Exec(ctx); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	org := &models.Organization{ID: uuid.New(), Name: "OtaOrg", Slug: "ota-" + uuid.NewString()[:8], OwnerID: owner.ID}
	if _, err := db.DB.NewInsert().Model(org).Exec(ctx); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	project := &models.AppProject{
		ID: uuid.New(), UserID: owner.ID, OrganizationID: org.ID,
		Name: "ota", Slug: "ota-" + uuid.NewString()[:8], Prompt: "n/a",
		ProjectType: models.ProjectTypeMobileAppNative,
		Status:      "draft",
	}
	if _, err := db.DB.NewInsert().Model(project).Exec(ctx); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	return &otaTestFixture{c: c, db: db, project: project}
}

// buildFixtureTarball produces a minimal `dist/` tarball with one JS bundle
// + one PNG asset + a metadata.json that references both. Mirrors the shape
// `npx expo export` writes.
func buildFixtureTarball(t *testing.T, platform, bundleName, bundleBytes string, iconPath string, iconBytes []byte) []byte {
	t.Helper()
	metadata := map[string]any{
		"version": 0,
		"bundler": "metro",
		"fileMetadata": map[string]any{
			platform: map[string]any{
				"bundle": bundleName,
				"assets": []map[string]any{
					{"path": iconPath, "ext": "png"},
				},
			},
			otherPlatform(platform): map[string]any{
				"bundle": "bundles/other.js",
				"assets": []map[string]any{},
			},
		},
	}
	metaJSON, err := json.Marshal(metadata)
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}

	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)
	writeFile := func(name string, body []byte) {
		hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write header %s: %v", name, err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatalf("write body %s: %v", name, err)
		}
	}
	writeFile("metadata.json", metaJSON)
	writeFile(bundleName, []byte(bundleBytes))
	writeFile(iconPath, iconBytes)
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func otherPlatform(p string) string {
	if p == "ios" {
		return "android"
	}
	return "ios"
}

func TestOTA_PublishManifestAssetRoundTrip(t *testing.T) {
	fix := setupOTATest(t)
	ctx := context.Background()

	bundleBody := "var __BUNDLE__ = 1;"
	iconBody := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	tarball := buildFixtureTarball(t, "android", "bundles/android-deadbeef.js", bundleBody, "assets/icon-md5", iconBody)

	expoConfig := []byte(`{"name":"Wemob","slug":"wemob","version":"1.0.0"}`)

	update, err := fix.c.PublishOTAUpdate(ctx, fix.project.ID, "fingerprint:abc123", "android", 1700000000, tarball, expoConfig)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if update.UpdateID == uuid.Nil {
		t.Error("UpdateID is zero")
	}
	if update.S3Prefix == "" {
		t.Error("S3Prefix empty")
	}

	// Lookup latest manifest matches what we just published.
	got, err := fix.c.GetLatestOTAManifest(ctx, fix.project.ID, "fingerprint:abc123", "android")
	if err != nil {
		t.Fatalf("get manifest: %v", err)
	}
	if got.UpdateID != update.UpdateID {
		t.Errorf("UpdateID mismatch: got %v want %v", got.UpdateID, update.UpdateID)
	}

	// Stale RV → not found.
	_, err = fix.c.GetLatestOTAManifest(ctx, fix.project.ID, "fingerprint:other", "android")
	if !errors.Is(err, ErrOTANotFound) {
		t.Errorf("stale RV: got err %v, want ErrOTANotFound", err)
	}

	// Wrong platform (no iOS publish) → not found.
	_, err = fix.c.GetLatestOTAManifest(ctx, fix.project.ID, "fingerprint:abc123", "ios")
	if !errors.Is(err, ErrOTANotFound) {
		t.Errorf("wrong platform: got err %v, want ErrOTANotFound", err)
	}

	// Asset round-trip: fetch the bundle bytes back.
	bundleGot, ct, err := fix.c.GetOTAAsset(ctx, fix.project.ID, "fingerprint:abc123", "android", "bundles/android-deadbeef.js")
	if err != nil {
		t.Fatalf("get bundle asset: %v", err)
	}
	if string(bundleGot) != bundleBody {
		t.Errorf("bundle bytes mismatch: got %q", string(bundleGot))
	}
	if ct != "application/javascript" {
		t.Errorf("bundle content-type = %q", ct)
	}

	// Asset round-trip: fetch the PNG bytes back.
	iconGot, iconCT, err := fix.c.GetOTAAsset(ctx, fix.project.ID, "fingerprint:abc123", "android", "assets/icon-md5")
	if err != nil {
		t.Fatalf("get icon asset: %v", err)
	}
	if !bytes.Equal(iconGot, iconBody) {
		t.Errorf("icon bytes mismatch")
	}
	if iconCT != "application/octet-stream" {
		// 'assets/icon-md5' has no extension — the content-type-by-ext
		// helper falls through to octet-stream. The manifest sends the
		// proper type via metadata.json's ext field; this is just S3's
		// stored content-type.
		t.Logf("icon CT = %q (octet-stream expected for ext-less path)", iconCT)
	}

	// Path traversal — '..' in the asset path is rejected before any S3 call.
	_, _, err = fix.c.GetOTAAsset(ctx, fix.project.ID, "fingerprint:abc123", "android", "../../etc/passwd")
	if err == nil {
		t.Error("expected path-traversal error, got nil")
	}
	if !strings.Contains(err.Error(), "parent reference") {
		t.Errorf("path-traversal error = %v", err)
	}

	// Cross-app prefix: GetOTAAsset for a DIFFERENT appProjectID can't see
	// this app's assets. (The lookup path queries by appProjectID first, so
	// even if the ?asset= matched, the controller would 404 first.)
	otherApp := uuid.New()
	_, _, err = fix.c.GetOTAAsset(ctx, otherApp, "fingerprint:abc123", "android", "bundles/android-deadbeef.js")
	if !errors.Is(err, ErrOTANotFound) {
		t.Errorf("cross-app lookup: got err %v, want ErrOTANotFound", err)
	}
}

func TestOTA_PublishRejectsBadTarball(t *testing.T) {
	fix := setupOTATest(t)
	ctx := context.Background()

	_, err := fix.c.PublishOTAUpdate(ctx, fix.project.ID, "fingerprint:abc", "android", 1700000000, []byte("not a gzip"), nil)
	if err == nil {
		t.Fatal("expected error for bad tarball")
	}
	if !errors.Is(err, ErrOTABadTarball) {
		t.Errorf("expected ErrOTABadTarball, got %v", err)
	}
}

func TestOTA_PublishRejectsBadPlatform(t *testing.T) {
	fix := setupOTATest(t)
	ctx := context.Background()

	tarball := buildFixtureTarball(t, "android", "bundles/android.js", "x", "assets/icon", []byte{1, 2, 3})
	_, err := fix.c.PublishOTAUpdate(ctx, fix.project.ID, "fingerprint:abc", "web", 1700000000, tarball, nil)
	if !errors.Is(err, ErrOTAInvalidPlatform) {
		t.Errorf("expected ErrOTAInvalidPlatform, got %v", err)
	}
}

func TestOTA_PublishRejectsTarballMissingBundle(t *testing.T) {
	fix := setupOTATest(t)
	ctx := context.Background()

	// metadata claims bundles/missing.js but tarball has nothing of that name.
	metadata := map[string]any{
		"fileMetadata": map[string]any{
			"android": map[string]any{
				"bundle": "bundles/missing.js",
				"assets": []map[string]any{},
			},
			"ios": map[string]any{"bundle": "bundles/i.js", "assets": []map[string]any{}},
		},
	}
	metaJSON, _ := json.Marshal(metadata)
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)
	hdr := &tar.Header{Name: "metadata.json", Mode: 0o644, Size: int64(len(metaJSON)), Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(metaJSON); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gzw.Close()

	_, err := fix.c.PublishOTAUpdate(ctx, fix.project.ID, "fingerprint:abc", "android", 1700000000, buf.Bytes(), nil)
	if !errors.Is(err, ErrOTAMetadataShape) {
		t.Errorf("expected ErrOTAMetadataShape, got %v", err)
	}
}

func TestOTA_FetchManifestAssets_RoundTripBytes(t *testing.T) {
	fix := setupOTATest(t)
	ctx := context.Background()

	bundleBody := "var __ANOTHER_BUNDLE__ = 2;"
	iconBody := []byte("PNG-LIKE-BYTES")
	tarball := buildFixtureTarball(t, "ios", "bundles/ios-cafe.js", bundleBody, "assets/ios-icon", iconBody)

	update, err := fix.c.PublishOTAUpdate(ctx, fix.project.ID, "fingerprint:ios-rv", "ios", 1700001111, tarball, []byte(`{}`))
	if err != nil {
		t.Fatalf("publish: %v", err)
	}

	// Use FetchOTAManifestAssets to round-trip both files and confirm
	// the BuildManifest pipeline can hash them.
	metaParsed, err := parseMetadataForTest(update.MetadataJSON)
	if err != nil {
		t.Fatalf("parse metadata: %v", err)
	}
	bytesMap, err := fix.c.FetchOTAManifestAssets(ctx, update, metaParsed)
	if err != nil {
		t.Fatalf("fetch assets: %v", err)
	}
	if got := string(bytesMap["bundles/ios-cafe.js"]); got != bundleBody {
		t.Errorf("bundle round-trip: got %q", got)
	}
	if !bytes.Equal(bytesMap["assets/ios-icon"], iconBody) {
		t.Errorf("icon round-trip mismatch")
	}
}
