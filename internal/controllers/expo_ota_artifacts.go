package controllers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/knid/wemob/internal/models"
)

// prepareOTAArtifactsInRepo writes the per-project signing cert into the
// user's mobile/ tree so `expo prebuild` finds the file at the path
// declared by app.json's expo.updates.codeSigningCertificate, and rewrites
// app.json's codeSigningMetadata.keyid to match the signing key the
// manifest endpoint will use.
//
// Idempotent on every axis: EnsureOTAUpdateSigningKey returns the same
// row on subsequent calls (one-time RSA-2048 generation + 10-year
// self-signed cert), the cert file write is a fixed-content overwrite,
// and updateExpoUpdatesKeyID short-circuits when the value already matches.
//
// Disk-only — no git commit. /data/repos/<id>/ persists between worker
// runs so the file usually survives across publishes; re-writing every
// time is cheap and keeps the cert + keyid in sync if either drifts (e.g.
// a manual `git checkout -- mobile/app.json` on the worker host).
func (c *Controller) prepareOTAArtifactsInRepo(ctx context.Context, project *models.AppProject, mobileDir string) (*models.OTAUpdateSigningKey, error) {
	if project == nil {
		return nil, fmt.Errorf("project is required")
	}
	keyRow, err := c.EnsureOTAUpdateSigningKey(ctx, project.ID)
	if err != nil {
		return nil, fmt.Errorf("ensure signing key: %w", err)
	}
	if err := writeOTACertFile(mobileDir, keyRow.CertificatePEM); err != nil {
		return nil, fmt.Errorf("write cert file: %w", err)
	}
	if err := updateExpoUpdatesKeyID(mobileDir, keyRow.KeyID); err != nil {
		return nil, fmt.Errorf("update app.json keyid: %w", err)
	}
	return keyRow, nil
}

// writeOTACertFile writes certPEM to <mobileDir>/certs/ota-cert.pem,
// creating the certs/ directory if needed.
func writeOTACertFile(mobileDir, certPEM string) error {
	if certPEM == "" {
		return fmt.Errorf("empty certPEM")
	}
	certsDir := filepath.Join(mobileDir, "certs")
	if err := os.MkdirAll(certsDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", certsDir, err)
	}
	return os.WriteFile(filepath.Join(certsDir, "ota-cert.pem"), []byte(certPEM), 0o644)
}

// otaKeyIDInMetadataRegex matches a "keyid": "..." field nested inside the
// "codeSigningMetadata" object. The negated-class [^}] keeps the match
// scoped to that object literal — it can't accidentally rewrite an
// unrelated keyid field elsewhere in app.json.
//
// Capture groups:
//
//	$1 = "codeSigningMetadata": {  ...  "keyid": "
//	$2 = the closing quote
var otaKeyIDInMetadataRegex = regexp.MustCompile(
	`("codeSigningMetadata"\s*:\s*\{[^}]*?"keyid"\s*:\s*")[^"]*(")`,
)

// updateExpoUpdatesKeyID rewrites <mobileDir>/app.json so the keyid nested
// under expo.updates.codeSigningMetadata equals keyID. Preserves all other
// content + key order verbatim (regex-based — a JSON parse + re-marshal
// would alphabetize the keys via Go's map iteration).
//
// No-ops when the field is already correct, OR when codeSigningMetadata
// is absent (legacy projects scaffolded before the field was added — they
// don't ship signed updates and don't need the keyid set).
func updateExpoUpdatesKeyID(mobileDir, keyID string) error {
	appJSONPath := filepath.Join(mobileDir, "app.json")
	raw, err := os.ReadFile(appJSONPath)
	if err != nil {
		return fmt.Errorf("read app.json: %w", err)
	}
	if !otaKeyIDInMetadataRegex.Match(raw) {
		return nil
	}
	out := otaKeyIDInMetadataRegex.ReplaceAllString(string(raw), "${1}"+keyID+"${2}")
	if out == string(raw) {
		return nil
	}
	return os.WriteFile(appJSONPath, []byte(out), 0o644)
}
