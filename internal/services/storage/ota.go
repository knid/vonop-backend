package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// OTAStorage is a thin wrapper over S3Storage scoped to the
// `ota/<appProjectID>/...` prefix. Owned by the assets bucket — same place
// as expo-source/{...}.tar.gz tarballs and icons/{...}.png. We don't reuse
// CredStore here because OTA assets are public (the manifest's asset URLs
// are unauthenticated, since the mobile client has no backend session).
//
// Path traversal is prevented by ValidatePrefixOwnership before any S3
// operation that's parametrised by user input (the ?asset= query param).
type OTAStorage struct {
	S3 *S3Storage
}

// NewOTAStorage returns a new OTAStorage backed by the given assets bucket
// S3 client.
func NewOTAStorage(s3 *S3Storage) *OTAStorage {
	return &OTAStorage{S3: s3}
}

// Prefix returns the canonical S3 key prefix for an OTA update:
//
//	ota/<appProjectID>/<runtimeVersion>/<timestamp>
//
// (no trailing slash).
func (o *OTAStorage) Prefix(appProjectID uuid.UUID, runtimeVersion string, timestamp int64) string {
	// runtimeVersion can contain ':' (e.g. fingerprint:abc...). S3 keys can
	// contain ':' literally — checked against the AWS S3 spec — but to be
	// safe and to make the key easier to ls-ish we replace it with '_'.
	rv := strings.ReplaceAll(runtimeVersion, ":", "_")
	return fmt.Sprintf("ota/%s/%s/%d", appProjectID, rv, timestamp)
}

// Put stores a single asset (bundle, png, font, etc.) under the OTA prefix.
// `key` is the in-tarball path (e.g. "bundles/android-abc.js").
func (o *OTAStorage) Put(ctx context.Context, prefix, key string, body []byte, contentType string) error {
	full := prefix + "/" + key
	return o.S3.PutBytes(ctx, full, body, contentType)
}

// Get returns the bytes at <prefix>/<key>. Caller must validate prefix
// ownership via ValidatePrefixOwnership before calling.
func (o *OTAStorage) Get(ctx context.Context, prefix, key string) ([]byte, error) {
	full := prefix + "/" + key
	return o.S3.GetBytes(ctx, full)
}

// ErrCrossAppOTAAccess is returned when ValidatePrefixOwnership is given a
// prefix that doesn't belong to the requesting appProjectID. Defends
// against a mobile client of app A asking for an asset under app B's
// prefix via a crafted ?asset= URL.
var ErrCrossAppOTAAccess = errors.New("ota: cross-app prefix access denied")

// ValidatePrefixOwnership ensures `prefix` is shaped `ota/<appProjectID>/...`
// for the requesting app. Returns ErrCrossAppOTAAccess on mismatch.
func ValidatePrefixOwnership(prefix string, appProjectID uuid.UUID) error {
	expected := fmt.Sprintf("ota/%s/", appProjectID)
	if !strings.HasPrefix(prefix+"/", expected) {
		return ErrCrossAppOTAAccess
	}
	return nil
}

// ValidateAssetPath ensures `assetKey` (the value of the ?asset= query
// param) doesn't contain '..' or absolute-path components. Required because
// we concat it directly into the S3 key. Returns an error string suitable
// for a 400 response.
func ValidateAssetPath(assetKey string) error {
	if assetKey == "" {
		return errors.New("ota: empty asset path")
	}
	if strings.Contains(assetKey, "..") {
		return errors.New("ota: asset path contains parent reference")
	}
	if strings.HasPrefix(assetKey, "/") {
		return errors.New("ota: asset path is absolute")
	}
	// Disallow CR/LF and NULL — defense against header injection if the
	// path ever flows back into a header.
	if strings.ContainsAny(assetKey, "\x00\r\n") {
		return errors.New("ota: asset path contains control characters")
	}
	return nil
}
