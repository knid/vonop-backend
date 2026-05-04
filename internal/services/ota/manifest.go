// Package ota holds pure-function helpers for building expo-updates
// manifest responses. The HTTP handlers live in internal/views; this
// package is unit-testable and has no I/O dependencies.
package ota

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/textproto"
	"strings"
	"time"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/utils"
)

// Asset is one entry in the manifest's `assets` array (or the launchAsset).
// Field order intentionally matches the expo-updates protocol JSON shape.
type Asset struct {
	Hash          string `json:"hash"`          // base64url SHA-256, no padding
	Key           string `json:"key"`           // typically MD5 of asset path; we use the asset filename without ext
	FileExtension string `json:"fileExtension"` // includes leading "."
	ContentType   string `json:"contentType"`
	URL           string `json:"url"`
}

// Manifest is the JSON payload of the manifest part in the multipart/mixed
// expo-updates response.
type Manifest struct {
	ID             string          `json:"id"`
	CreatedAt      string          `json:"createdAt"`
	RuntimeVersion string          `json:"runtimeVersion"`
	Assets         []Asset         `json:"assets"`
	LaunchAsset    Asset           `json:"launchAsset"`
	Metadata       map[string]any  `json:"metadata"`
	Extra          map[string]any  `json:"extra"`
}

// MetadataFile mirrors the shape of `dist/metadata.json` produced by
// `npx expo export`. Only the parts we consume are typed; everything else
// is ignored.
type MetadataFile struct {
	Version      int           `json:"version"`
	Bundler      string        `json:"bundler"`
	FileMetadata FileMetadata  `json:"fileMetadata"`
}

type FileMetadata struct {
	IOS     PlatformMetadata `json:"ios"`
	Android PlatformMetadata `json:"android"`
}

type PlatformMetadata struct {
	Bundle string         `json:"bundle"`
	Assets []AssetMetadata `json:"assets"`
}

type AssetMetadata struct {
	Path string `json:"path"`
	Ext  string `json:"ext"`
}

// ParseMetadataJSON unmarshals dist/metadata.json bytes.
func ParseMetadataJSON(b []byte) (*MetadataFile, error) {
	var m MetadataFile
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("ota: parse metadata.json: %w", err)
	}
	return &m, nil
}

// PlatformBranch returns the appropriate FileMetadata branch for the
// requested platform string ("ios" | "android"). Returns an error for any
// other value.
func (m *MetadataFile) PlatformBranch(platform string) (PlatformMetadata, error) {
	switch strings.ToLower(platform) {
	case "ios":
		return m.FileMetadata.IOS, nil
	case "android":
		return m.FileMetadata.Android, nil
	default:
		return PlatformMetadata{}, fmt.Errorf("ota: unsupported platform %q", platform)
	}
}

// BuildManifest assembles the manifest JSON for a stored OTAUpdate.
//
// assetBytes maps each in-tarball asset path (e.g.
// "bundles/android-abc.js" or "assets/abc") to the raw bytes — needed to
// compute the per-asset hash that the expo-updates client verifies.
//
// baseURL is the absolute URL prefix the mobile client will use to fetch
// assets; typically "{c.BackendDeployURL(p)}/v2/apps/{appId}/ota". The
// manifest URLs are baseURL + "/assets?asset=<key>&runtimeVersion=<rv>&platform=<p>".
func BuildManifest(update *models.OTAUpdate, metadata *MetadataFile, assetBytes map[string][]byte, baseURL string) (Manifest, error) {
	branch, err := metadata.PlatformBranch(update.Platform)
	if err != nil {
		return Manifest{}, err
	}

	// Launch asset: the JS bundle. Path inside the tarball is relative to
	// dist/, but our S3 keys + assetBytes map use the same in-tarball path.
	bundleBytes, ok := assetBytes[branch.Bundle]
	if !ok {
		return Manifest{}, fmt.Errorf("ota: bundle bytes missing for %q", branch.Bundle)
	}
	launch := Asset{
		Hash:          utils.AssetHash(bundleBytes),
		Key:           assetKeyFromPath(branch.Bundle),
		FileExtension: ".bundle",
		ContentType:   "application/javascript",
		URL:           assetURL(baseURL, branch.Bundle, update.RuntimeVersion, update.Platform),
	}

	assets := make([]Asset, 0, len(branch.Assets))
	for _, a := range branch.Assets {
		body, ok := assetBytes[a.Path]
		if !ok {
			return Manifest{}, fmt.Errorf("ota: asset bytes missing for %q", a.Path)
		}
		ext := a.Ext
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		assets = append(assets, Asset{
			Hash:          utils.AssetHash(body),
			Key:           assetKeyFromPath(a.Path),
			FileExtension: ext,
			ContentType:   contentTypeForExt(ext),
			URL:           assetURL(baseURL, a.Path, update.RuntimeVersion, update.Platform),
		})
	}

	// extra.expoClient — the parsed app.json's `expo` block.
	var expoClient map[string]any
	if len(update.ExpoConfigJSON) > 0 {
		// Best-effort: if the column is malformed JSON we ship an empty
		// extra rather than failing the whole manifest. The client tolerates
		// missing fields here.
		_ = json.Unmarshal(update.ExpoConfigJSON, &expoClient)
	}
	if expoClient == nil {
		expoClient = map[string]any{}
	}

	return Manifest{
		ID:             update.UpdateID.String(),
		CreatedAt:      update.CreatedAt.UTC().Format(time.RFC3339Nano),
		RuntimeVersion: update.RuntimeVersion,
		Assets:         assets,
		LaunchAsset:    launch,
		Metadata:       map[string]any{},
		Extra:          map[string]any{"expoClient": expoClient},
	}, nil
}

// MarshalMultipart produces the multipart/mixed body the expo-updates
// client expects: a single part with name="manifest", containing the
// manifest JSON. Returns (body, boundary, err) — caller writes
// "Content-Type: multipart/mixed; boundary=<boundary>" + body to the
// response.
//
// Use MarshalMultipartWithBody when you also need the unwrapped manifest
// JSON bytes (for code-signing purposes).
func MarshalMultipart(manifest Manifest) ([]byte, string, error) {
	_, body, boundary, err := MarshalMultipartWithBody(manifest)
	return body, boundary, err
}

// MarshalMultipartWithBody is MarshalMultipart but also returns the raw
// manifest JSON bytes (the body of the "manifest" multipart part). The
// expo-updates code-signing scheme (P0 #8a) signs over these bytes, so
// we surface them rather than re-marshalling and risking a byte-level
// drift between sign-time and serve-time encoding.
//
// Returns (manifestBody, multipartBody, boundary, err).
func MarshalMultipartWithBody(manifest Manifest) ([]byte, []byte, string, error) {
	var manifestBuf bytes.Buffer
	if err := json.NewEncoder(&manifestBuf).Encode(manifest); err != nil {
		return nil, nil, "", fmt.Errorf("ota: encode manifest: %w", err)
	}
	manifestBody := manifestBuf.Bytes()

	var multiBuf bytes.Buffer
	mw := multipart.NewWriter(&multiBuf)
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", `form-data; name="manifest"`)
	header.Set("Content-Type", "application/json; charset=utf-8")
	part, err := mw.CreatePart(header)
	if err != nil {
		return nil, nil, "", fmt.Errorf("ota: create manifest part: %w", err)
	}
	if _, err := part.Write(manifestBody); err != nil {
		return nil, nil, "", fmt.Errorf("ota: write manifest part: %w", err)
	}
	if err := mw.Close(); err != nil {
		return nil, nil, "", fmt.Errorf("ota: close multipart: %w", err)
	}
	return manifestBody, multiBuf.Bytes(), mw.Boundary(), nil
}

// assetURL builds the absolute URL the mobile client fetches each asset
// from. baseURL is "{deployURL}/v2/apps/{appId}/ota" (no trailing slash).
func assetURL(baseURL, path, runtimeVersion, platform string) string {
	// The asset path MUST round-trip through an unambiguous query param.
	// We don't URL-encode here because all paths are ASCII (bundles/...,
	// assets/<md5>) and the expo client expects them literal. If we ever
	// support non-ASCII paths, switch to url.QueryEscape.
	return fmt.Sprintf(
		"%s/assets?asset=%s&runtimeVersion=%s&platform=%s",
		baseURL, path, runtimeVersion, platform,
	)
}

// assetKeyFromPath returns the basename minus extension as the asset key.
// The reference impl uses MD5 of the path; we use the basename which is
// stable, unique within an OTAUpdate, and easier to reason about.
func assetKeyFromPath(path string) string {
	idx := strings.LastIndex(path, "/")
	if idx < 0 {
		return stripExt(path)
	}
	return stripExt(path[idx+1:])
}

func stripExt(name string) string {
	idx := strings.LastIndex(name, ".")
	if idx < 0 {
		return name
	}
	return name[:idx]
}

// contentTypeForExt maps known asset file extensions to MIME types.
// Everything we don't know about gets octet-stream — the expo client
// reads bytes and doesn't actually use the content-type for anything
// beyond debug logging, so this is best-effort.
func contentTypeForExt(ext string) string {
	switch strings.ToLower(strings.TrimPrefix(ext, ".")) {
	case "png":
		return "image/png"
	case "jpg", "jpeg":
		return "image/jpeg"
	case "gif":
		return "image/gif"
	case "svg":
		return "image/svg+xml"
	case "webp":
		return "image/webp"
	case "ttf":
		return "font/ttf"
	case "otf":
		return "font/otf"
	case "woff":
		return "font/woff"
	case "woff2":
		return "font/woff2"
	case "json":
		return "application/json"
	case "js", "jsbundle", "bundle":
		return "application/javascript"
	default:
		return "application/octet-stream"
	}
}
