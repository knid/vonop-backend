//go:build integration

package controllers

import (
	"context"
	"encoding/base64"
	"regexp"
	"strings"
	"testing"

	"github.com/knid/wemob/internal/services/ota"
)

// TestEnsureOTAUpdateSigningKey_GeneratesAndPersists verifies the lazy
// generation path: first call mints a keypair + cert, persists, returns
// it. Second call reads the same row back.
func TestEnsureOTAUpdateSigningKey_GeneratesAndPersists(t *testing.T) {
	fix := setupOTATest(t)
	ctx := context.Background()

	row1, err := fix.c.EnsureOTAUpdateSigningKey(ctx, fix.project.ID)
	if err != nil {
		t.Fatalf("ensure (first): %v", err)
	}
	if row1.AppProjectID != fix.project.ID {
		t.Errorf("AppProjectID mismatch")
	}
	if row1.OrganizationID != fix.project.OrganizationID {
		t.Errorf("OrganizationID mismatch")
	}
	if row1.CredRefPrivate == nil {
		t.Fatal("CredRefPrivate is nil — private key not stored in CredStore")
	}
	if row1.Algorithm != "rsa-v1_5-sha256" {
		t.Errorf("Algorithm = %q; want rsa-v1_5-sha256", row1.Algorithm)
	}
	if !strings.Contains(row1.CertificatePEM, "-----BEGIN CERTIFICATE-----") {
		t.Errorf("CertificatePEM missing PEM header: %q", row1.CertificatePEM[:64])
	}
	matched, _ := regexp.MatchString(`^[a-f0-9]{16}$`, row1.KeyID)
	if !matched {
		t.Errorf("KeyID %q doesn't look like a 16-char hex fingerprint", row1.KeyID)
	}

	// Idempotent: a second call returns the same row.
	row2, err := fix.c.EnsureOTAUpdateSigningKey(ctx, fix.project.ID)
	if err != nil {
		t.Fatalf("ensure (second): %v", err)
	}
	if row2.KeyID != row1.KeyID {
		t.Errorf("KeyID drifted: first=%s second=%s", row1.KeyID, row2.KeyID)
	}
	if row2.CertificatePEM != row1.CertificatePEM {
		t.Error("CertificatePEM drifted between calls")
	}
}

// TestOTAManifestSigning_RoundTrip verifies the signing pipeline end-to-end
// at the controller layer (the view is a thin shell over EnsureSigningKey
// + LoadPrivateKeyPEM + ota.SignManifest, and adding a view-level test
// would create an import cycle).
//
// Round-trip:
//  1. Publish an OTA update.
//  2. Build the manifest body the view would emit.
//  3. Generate the key + sign the body.
//  4. Verify the signature against the stored cert.
func TestOTAManifestSigning_RoundTrip(t *testing.T) {
	fix := setupOTATest(t)
	ctx := context.Background()

	bundleBody := "var __SIGNED__ = true;"
	iconBody := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	tarball := buildFixtureTarball(t, "android", "bundles/android-deadbeef.js", bundleBody, "assets/icon-md5", iconBody)
	expoConfig := []byte(`{"name":"Wemob","slug":"wemob","version":"1.0.0"}`)
	update, err := fix.c.PublishOTAUpdate(ctx, fix.project.ID, "fingerprint:abc123", "android", 1700000000, tarball, expoConfig)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}

	metaParsed, err := parseMetadataForTest(update.MetadataJSON)
	if err != nil {
		t.Fatalf("parse metadata: %v", err)
	}
	bytesMap, err := fix.c.FetchOTAManifestAssets(ctx, update, metaParsed)
	if err != nil {
		t.Fatalf("fetch assets: %v", err)
	}
	manifest, err := ota.BuildManifest(update, metaParsed, bytesMap, "https://example.com/v2/apps/"+fix.project.ID.String()+"/ota")
	if err != nil {
		t.Fatalf("build manifest: %v", err)
	}
	manifestBody, _, _, err := ota.MarshalMultipartWithBody(manifest)
	if err != nil {
		t.Fatalf("marshal multipart: %v", err)
	}

	signKey, err := fix.c.EnsureOTAUpdateSigningKey(ctx, fix.project.ID)
	if err != nil {
		t.Fatalf("ensure key: %v", err)
	}
	privPEM, err := fix.c.LoadOTAUpdateSigningPrivateKeyPEM(ctx, signKey)
	if err != nil {
		t.Fatalf("load priv: %v", err)
	}
	headerVal, err := ota.SignManifest(manifestBody, privPEM, signKey.KeyID)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	for _, want := range []string{`sig="`, `keyid="`, `alg="rsa-v1_5-sha256"`} {
		if !strings.Contains(headerVal, want) {
			t.Errorf("expo-signature missing %q substring: %s", want, headerVal)
		}
	}
	parsed := parseExpoSignatureHeader(headerVal)
	if parsed["keyid"] != signKey.KeyID {
		t.Errorf("header keyid=%q; want %q", parsed["keyid"], signKey.KeyID)
	}
	if parsed["alg"] != "rsa-v1_5-sha256" {
		t.Errorf("header alg=%q; want rsa-v1_5-sha256", parsed["alg"])
	}
	// Round-trip: verify the signature against the cert.
	if err := ota.VerifyManifestSignature(manifestBody, []byte(signKey.CertificatePEM), parsed["sig"]); err != nil {
		t.Errorf("signature verify failed: %v", err)
	}
	// Sanity: tampering with the body breaks the signature.
	tampered := append([]byte{}, manifestBody...)
	tampered[0] ^= 0xFF
	if err := ota.VerifyManifestSignature(tampered, []byte(signKey.CertificatePEM), parsed["sig"]); err == nil {
		t.Error("tampered body verified successfully — signature scheme is broken")
	}
	// Sanity: signature parses as valid base64.
	if _, err := base64.StdEncoding.DecodeString(parsed["sig"]); err != nil {
		t.Errorf("sig isn't valid base64: %v", err)
	}
}

// parseExpoSignatureHeader parses `sig="...", keyid="...", alg="..."`.
// Tolerant of whitespace.
func parseExpoSignatureHeader(h string) map[string]string {
	out := map[string]string{}
	for _, kv := range strings.Split(h, ",") {
		kv = strings.TrimSpace(kv)
		eq := strings.Index(kv, "=")
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(kv[:eq])
		val := strings.TrimSpace(kv[eq+1:])
		val = strings.Trim(val, `"`)
		out[key] = val
	}
	return out
}
