package ota

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/models"
)

func sampleMetadata(t *testing.T) *MetadataFile {
	t.Helper()
	raw := `{
		"version": 0,
		"bundler": "metro",
		"fileMetadata": {
			"android": {
				"bundle": "bundles/android-deadbeef.js",
				"assets": [
					{ "path": "assets/icon-md5", "ext": "png" }
				]
			},
			"ios": {
				"bundle": "bundles/ios-cafebabe.js",
				"assets": []
			}
		}
	}`
	m, err := ParseMetadataJSON([]byte(raw))
	if err != nil {
		t.Fatalf("ParseMetadataJSON: %v", err)
	}
	return m
}

func TestBuildManifest_AndroidShape(t *testing.T) {
	meta := sampleMetadata(t)
	bundleBytes := []byte("var __BUNDLE__ = 1;")
	iconBytes := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a} // PNG magic
	assetBytes := map[string][]byte{
		"bundles/android-deadbeef.js": bundleBytes,
		"assets/icon-md5":             iconBytes,
	}

	update := &models.OTAUpdate{
		ID:             uuid.New(),
		AppProjectID:   uuid.New(),
		RuntimeVersion: "fingerprint:abc123",
		Platform:       "android",
		UpdateID:       uuid.New(),
		S3Prefix:       "ota/x/y/123",
		CreatedAt:      time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC),
		ExpoConfigJSON: []byte(`{"name":"WemobApp","slug":"wemob"}`),
	}

	m, err := BuildManifest(update, meta, assetBytes, "https://api.example.com/v2/apps/aaa/ota")
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}

	if m.ID != update.UpdateID.String() {
		t.Errorf("Manifest.ID = %s, want %s", m.ID, update.UpdateID)
	}
	if m.RuntimeVersion != "fingerprint:abc123" {
		t.Errorf("Manifest.RuntimeVersion = %q", m.RuntimeVersion)
	}
	if m.LaunchAsset.ContentType != "application/javascript" {
		t.Errorf("LaunchAsset.ContentType = %q", m.LaunchAsset.ContentType)
	}
	if m.LaunchAsset.FileExtension != ".bundle" {
		t.Errorf("LaunchAsset.FileExtension = %q", m.LaunchAsset.FileExtension)
	}
	if !strings.Contains(m.LaunchAsset.URL, "asset=bundles/android-deadbeef.js") {
		t.Errorf("LaunchAsset.URL = %q", m.LaunchAsset.URL)
	}
	if !strings.Contains(m.LaunchAsset.URL, "platform=android") {
		t.Errorf("LaunchAsset.URL platform = %q", m.LaunchAsset.URL)
	}
	if !strings.Contains(m.LaunchAsset.URL, "runtimeVersion=fingerprint:abc123") {
		t.Errorf("LaunchAsset.URL rv = %q", m.LaunchAsset.URL)
	}
	if len(m.Assets) != 1 {
		t.Fatalf("Manifest.Assets len = %d, want 1", len(m.Assets))
	}
	if m.Assets[0].FileExtension != ".png" || m.Assets[0].ContentType != "image/png" {
		t.Errorf("png asset wrong: %+v", m.Assets[0])
	}
	// extra.expoClient round-trip
	expoClient, ok := m.Extra["expoClient"].(map[string]any)
	if !ok {
		t.Fatalf("Extra.expoClient not a map: %T", m.Extra["expoClient"])
	}
	if expoClient["name"] != "WemobApp" {
		t.Errorf("Extra.expoClient.name = %v", expoClient["name"])
	}
}

func TestBuildManifest_MissingBundleBytes(t *testing.T) {
	meta := sampleMetadata(t)
	update := &models.OTAUpdate{
		Platform:       "android",
		RuntimeVersion: "fingerprint:abc",
		UpdateID:       uuid.New(),
		CreatedAt:      time.Now(),
	}
	_, err := BuildManifest(update, meta, map[string][]byte{}, "https://x.example/ota")
	if err == nil || !strings.Contains(err.Error(), "bundle bytes missing") {
		t.Errorf("expected bundle-missing error, got %v", err)
	}
}

func TestParseMetadataJSON_BadShape(t *testing.T) {
	_, err := ParseMetadataJSON([]byte(`not json`))
	if err == nil {
		t.Error("expected error for bad json")
	}
}

func TestPlatformBranch_UnknownPlatform(t *testing.T) {
	meta := sampleMetadata(t)
	_, err := meta.PlatformBranch("web")
	if err == nil {
		t.Error("expected error for platform=web")
	}
}
