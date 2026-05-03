package controllers

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/ota"
	"github.com/knid/wemob/internal/services/storage"
	"github.com/knid/wemob/internal/utils"
)

// Errors returned by the OTA controller — translated to HTTP status codes
// at the view layer.
var (
	ErrOTABadTarball     = errors.New("ota: bad tarball")
	ErrOTAMetadataShape  = errors.New("ota: metadata.json missing required fields")
	ErrOTANotFound       = errors.New("ota: no update for runtime version")
	ErrOTAInvalidPlatform = errors.New("ota: invalid platform")
)

// OTA tarball safety limits. Defense against zip-bomb / unbounded extraction.
const (
	otaMaxFiles            = 1000
	otaMaxFileBytes        = 50 * 1024 * 1024  // 50 MiB per file
	otaMaxUncompressedSize = 100 * 1024 * 1024 // 100 MiB total
	otaKeepLastN           = 10                // size-cap pruning per (RV, platform)
)

// PublishOTAUpdate is the worker-facing entry point for the
// POST /v2/apps/{appId}/ota/publish endpoint. It takes the dist.tar.gz
// produced by `npx expo export`, validates it, uploads each file to S3,
// and persists an OTAUpdate row.
//
// HMAC verification is the view's job — by the time we get here the
// caller is trusted to act on behalf of appProjectID.
//
// Returns the persisted update on success.
func (c *Controller) PublishOTAUpdate(
	ctx context.Context,
	appProjectID uuid.UUID,
	runtimeVersion, platform string,
	timestamp int64,
	distTarGz []byte,
	expoConfigJSON []byte,
) (*models.OTAUpdate, error) {
	if platform != "android" && platform != "ios" {
		return nil, fmt.Errorf("%w: %q", ErrOTAInvalidPlatform, platform)
	}
	if runtimeVersion == "" {
		return nil, fmt.Errorf("%w: empty runtime_version", ErrOTABadTarball)
	}

	files, err := extractTarball(distTarGz)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOTABadTarball, err)
	}

	// metadata.json is mandatory — it tells us which file is the bundle vs
	// which are assets.
	metaBytes, ok := files["metadata.json"]
	if !ok {
		// expo export may produce dist/metadata.json with a leading dist/
		// component when the tarball was built from a parent dir. Tolerate.
		metaBytes, ok = files["dist/metadata.json"]
		if !ok {
			return nil, fmt.Errorf("%w: no metadata.json", ErrOTAMetadataShape)
		}
		// Strip the leading "dist/" component from every key so the
		// in-tarball paths align with metadata.json's relative paths.
		stripped := make(map[string][]byte, len(files))
		for k, v := range files {
			stripped[strings.TrimPrefix(k, "dist/")] = v
		}
		files = stripped
	}

	metadata, err := ota.ParseMetadataJSON(metaBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOTAMetadataShape, err)
	}

	branch, err := metadata.PlatformBranch(platform)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOTAMetadataShape, err)
	}
	if branch.Bundle == "" {
		return nil, fmt.Errorf("%w: missing fileMetadata.%s.bundle", ErrOTAMetadataShape, platform)
	}
	if _, ok := files[branch.Bundle]; !ok {
		return nil, fmt.Errorf("%w: bundle file %q not in tarball", ErrOTAMetadataShape, branch.Bundle)
	}
	for _, a := range branch.Assets {
		if _, ok := files[a.Path]; !ok {
			return nil, fmt.Errorf("%w: asset file %q not in tarball", ErrOTAMetadataShape, a.Path)
		}
	}

	// Stash files in S3 under ota/<appProjectID>/<rv>/<ts>/<path>.
	otaStore := storage.NewOTAStorage(c.AssetsStorage)
	prefix := otaStore.Prefix(appProjectID, runtimeVersion, timestamp)

	// Upload the bundle + assets only — we don't need metadata.json on S3
	// (we keep it on the OTAUpdate row instead).
	uploads := make([]string, 0, 1+len(branch.Assets))
	uploads = append(uploads, branch.Bundle)
	for _, a := range branch.Assets {
		uploads = append(uploads, a.Path)
	}
	for _, p := range uploads {
		ct := contentTypeForUpload(p)
		if err := otaStore.Put(ctx, prefix, p, files[p], ct); err != nil {
			return nil, fmt.Errorf("ota: upload %q: %w", p, err)
		}
	}

	// Compute the manifest ID deterministically from a stable subset of the
	// upload — the bundle hash + asset hashes — so the same publish always
	// gets the same ID. (Means a worker retry that re-uploads the same
	// bytes deduplicates on the client side.)
	idSeed := bytes.Buffer{}
	idSeed.WriteString(runtimeVersion)
	idSeed.WriteString("|")
	idSeed.WriteString(platform)
	idSeed.WriteString("|")
	idSeed.WriteString(utils.AssetHash(files[branch.Bundle]))
	for _, a := range branch.Assets {
		idSeed.WriteString("|")
		idSeed.WriteString(utils.AssetHash(files[a.Path]))
	}
	updateID := utils.ManifestID(idSeed.Bytes())

	row := &models.OTAUpdate{
		AppProjectID:   appProjectID,
		RuntimeVersion: runtimeVersion,
		Platform:       platform,
		UpdateID:       updateID,
		S3Prefix:       prefix,
		MetadataJSON:   metaBytes,
		ExpoConfigJSON: expoConfigJSON,
	}
	if err := c.DB.InsertOTAUpdate(ctx, row); err != nil {
		return nil, fmt.Errorf("ota: persist update: %w", err)
	}

	// Size-cap pruning: keep the otaKeepLastN most recent updates per
	// (project, RV, platform); delete the rest. Best-effort — log on
	// failure but don't fail the publish.
	go c.pruneOldOTAUpdates(context.Background(), appProjectID, runtimeVersion, platform)

	return row, nil
}

// GetLatestOTAManifest returns the most-recent OTAUpdate for
// (appProjectID, runtimeVersion, platform). Translates "no rows" to
// ErrOTANotFound for the view.
func (c *Controller) GetLatestOTAManifest(ctx context.Context, appProjectID uuid.UUID, runtimeVersion, platform string) (*models.OTAUpdate, error) {
	if platform != "android" && platform != "ios" {
		return nil, fmt.Errorf("%w: %q", ErrOTAInvalidPlatform, platform)
	}
	row, err := c.DB.GetLatestOTAUpdate(ctx, appProjectID, runtimeVersion, platform)
	if err != nil {
		if database.IsErrNoOTAUpdate(err) {
			return nil, ErrOTANotFound
		}
		return nil, err
	}
	return row, nil
}

// FetchOTAManifestAssets reads bundle + asset bytes from S3 for an
// OTAUpdate so the view can hand them to BuildManifest. We pull all
// referenced bytes up-front because BuildManifest needs each one's hash.
func (c *Controller) FetchOTAManifestAssets(ctx context.Context, update *models.OTAUpdate, metadata *ota.MetadataFile) (map[string][]byte, error) {
	branch, err := metadata.PlatformBranch(update.Platform)
	if err != nil {
		return nil, err
	}
	otaStore := storage.NewOTAStorage(c.AssetsStorage)
	out := map[string][]byte{}

	bundleBytes, err := otaStore.Get(ctx, update.S3Prefix, branch.Bundle)
	if err != nil {
		return nil, fmt.Errorf("ota: fetch bundle: %w", err)
	}
	out[branch.Bundle] = bundleBytes

	for _, a := range branch.Assets {
		body, err := otaStore.Get(ctx, update.S3Prefix, a.Path)
		if err != nil {
			return nil, fmt.Errorf("ota: fetch asset %q: %w", a.Path, err)
		}
		out[a.Path] = body
	}
	return out, nil
}

// GetOTAAsset streams a single asset (the value of the ?asset= query
// param). Validates the asset path and that the prefix belongs to
// appProjectID. Returns (bytes, contentType, error).
func (c *Controller) GetOTAAsset(ctx context.Context, appProjectID uuid.UUID, runtimeVersion, platform, assetKey string) ([]byte, string, error) {
	if err := storage.ValidateAssetPath(assetKey); err != nil {
		return nil, "", err
	}
	update, err := c.GetLatestOTAManifest(ctx, appProjectID, runtimeVersion, platform)
	if err != nil {
		return nil, "", err
	}
	if err := storage.ValidatePrefixOwnership(update.S3Prefix, appProjectID); err != nil {
		// Should be impossible — Prefix() always uses appProjectID — but
		// belt-and-braces.
		return nil, "", err
	}
	otaStore := storage.NewOTAStorage(c.AssetsStorage)
	body, err := otaStore.Get(ctx, update.S3Prefix, assetKey)
	if err != nil {
		return nil, "", fmt.Errorf("ota: fetch asset %q: %w", assetKey, err)
	}
	return body, contentTypeForUpload(assetKey), nil
}

// pruneOldOTAUpdates deletes OTAUpdate rows + their S3 objects beyond the
// otaKeepLastN most recent for a (project, RV, platform). Best-effort —
// runs in a goroutine after a successful publish.
func (c *Controller) pruneOldOTAUpdates(ctx context.Context, appProjectID uuid.UUID, runtimeVersion, platform string) {
	rows, err := c.DB.ListOldOTAUpdatesForRV(ctx, appProjectID, runtimeVersion, platform, otaKeepLastN)
	if err != nil {
		slog.Warn("ota prune: list failed", "app_project_id", appProjectID, "err", err)
		return
	}
	for _, r := range rows {
		// Delete the S3 prefix's contents. We don't list+delete via S3 API
		// here — we just delete the bundle + assets we know about from
		// metadata.json. Anything else (orphaned uploads) is unreachable
		// once the row goes away.
		meta, err := ota.ParseMetadataJSON(r.MetadataJSON)
		if err == nil {
			if branch, perr := meta.PlatformBranch(r.Platform); perr == nil {
				_ = c.AssetsStorage.DeleteObjectDirect(ctx, r.S3Prefix+"/"+branch.Bundle)
				for _, a := range branch.Assets {
					_ = c.AssetsStorage.DeleteObjectDirect(ctx, r.S3Prefix+"/"+a.Path)
				}
			}
		}
		if err := c.DB.DeleteOTAUpdate(ctx, r.ID); err != nil {
			slog.Warn("ota prune: delete row", "id", r.ID, "err", err)
		}
	}
}

// extractTarball decompresses a gzipped tar archive into a map of
// {path → bytes}. Enforces otaMaxFiles, otaMaxFileBytes,
// otaMaxUncompressedSize. Rejects entries with `..` in their path
// (zip-slip defense).
func extractTarball(b []byte) (map[string][]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("gunzip: %w", err)
	}
	defer gz.Close()

	files := make(map[string][]byte)
	totalBytes := 0
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar next: %w", err)
		}
		if len(files) >= otaMaxFiles {
			return nil, fmt.Errorf("too many files (>%d)", otaMaxFiles)
		}
		// Reject path traversal: filepath.Clean + '..' rejection. The tar
		// header has forward slashes per spec but tolerate either.
		clean := filepath.ToSlash(filepath.Clean(hdr.Name))
		if strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") || clean == ".." {
			return nil, fmt.Errorf("zip-slip path %q", hdr.Name)
		}
		if strings.HasPrefix(clean, "/") {
			return nil, fmt.Errorf("absolute path %q", hdr.Name)
		}
		// Skip directories + non-regular files.
		switch hdr.Typeflag {
		case tar.TypeReg, tar.TypeRegA:
		default:
			continue
		}
		if hdr.Size > otaMaxFileBytes {
			return nil, fmt.Errorf("file %q size %d exceeds limit", hdr.Name, hdr.Size)
		}
		buf := bytes.NewBuffer(make([]byte, 0, hdr.Size))
		// Cap the per-file read defensively: the tar header could lie
		// about size and a malicious archive could send unlimited bytes.
		_, err = io.CopyN(buf, tr, hdr.Size)
		if err != nil {
			return nil, fmt.Errorf("read %q: %w", hdr.Name, err)
		}
		totalBytes += buf.Len()
		if totalBytes > otaMaxUncompressedSize {
			return nil, fmt.Errorf("total uncompressed size exceeds %d bytes", otaMaxUncompressedSize)
		}
		files[clean] = buf.Bytes()
	}
	return files, nil
}

// contentTypeForUpload picks a Content-Type for an in-tarball file based
// on its extension. The S3 object's stored content-type matters because
// we serve it as the asset Content-Type from /v2/apps/{id}/ota/assets.
func contentTypeForUpload(path string) string {
	idx := strings.LastIndex(path, ".")
	if idx < 0 {
		// Expo asset paths like "assets/abc" have no extension — the ext
		// lives in metadata.json. Default to octet-stream; the manifest
		// builder uses metadata.json's ext for the manifest's
		// fileExtension/contentType fields, so this only affects the
		// browser-style Content-Type the asset endpoint serves.
		return "application/octet-stream"
	}
	switch strings.ToLower(path[idx+1:]) {
	case "js":
		return "application/javascript"
	case "json":
		return "application/json"
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
	default:
		return "application/octet-stream"
	}
}
