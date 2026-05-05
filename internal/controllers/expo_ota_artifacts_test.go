package controllers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteOTACertFile_CreatesCertsDirAndFile(t *testing.T) {
	dir := t.TempDir()
	pem := "-----BEGIN CERTIFICATE-----\nFOOBAR\n-----END CERTIFICATE-----\n"
	if err := writeOTACertFile(dir, pem); err != nil {
		t.Fatalf("writeOTACertFile: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "certs", "ota-cert.pem"))
	if err != nil {
		t.Fatalf("read written cert: %v", err)
	}
	if string(got) != pem {
		t.Errorf("cert content mismatch; want %q, got %q", pem, got)
	}
}

func TestWriteOTACertFile_RejectsEmpty(t *testing.T) {
	if err := writeOTACertFile(t.TempDir(), ""); err == nil {
		t.Fatal("expected error on empty cert; got nil")
	}
}

func TestUpdateExpoUpdatesKeyID_RewritesEmptyKeyid(t *testing.T) {
	dir := t.TempDir()
	src := `{
  "expo": {
    "name": "Smoke",
    "updates": {
      "url": "https://example/ota",
      "codeSigningCertificate": "./certs/ota-cert.pem",
      "codeSigningMetadata": {
        "keyid": "",
        "alg": "rsa-v1_5-sha256"
      }
    }
  }
}
`
	if err := os.WriteFile(filepath.Join(dir, "app.json"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := updateExpoUpdatesKeyID(dir, "abc123def456"); err != nil {
		t.Fatalf("updateExpoUpdatesKeyID: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "app.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := `"keyid": "abc123def456"`
	if !strings.Contains(string(got), want) {
		t.Errorf("output missing %q; got: %s", want, got)
	}
	// Surrounding structure must be untouched (key order, indentation).
	for _, marker := range []string{
		`"name": "Smoke"`,
		`"alg": "rsa-v1_5-sha256"`,
		`"url": "https://example/ota"`,
		`"codeSigningCertificate": "./certs/ota-cert.pem"`,
	} {
		if !strings.Contains(string(got), marker) {
			t.Errorf("output dropped surrounding marker %q; got: %s", marker, got)
		}
	}
}

func TestUpdateExpoUpdatesKeyID_IsIdempotent(t *testing.T) {
	dir := t.TempDir()
	src := `{
  "expo": {
    "updates": {
      "codeSigningMetadata": { "keyid": "abc123def456", "alg": "rsa-v1_5-sha256" }
    }
  }
}
`
	path := filepath.Join(dir, "app.json")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	statBefore, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := updateExpoUpdatesKeyID(dir, "abc123def456"); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	statAfter, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// File should not have been rewritten when value already matched —
	// mtime preserved is the sharpest signal.
	if !statBefore.ModTime().Equal(statAfter.ModTime()) {
		t.Errorf("expected no-op write; mtime changed from %v to %v",
			statBefore.ModTime(), statAfter.ModTime())
	}
}

func TestUpdateExpoUpdatesKeyID_NoMetadataIsNoOp(t *testing.T) {
	dir := t.TempDir()
	src := `{ "expo": { "name": "NoSigningMetadata" } }`
	path := filepath.Join(dir, "app.json")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := updateExpoUpdatesKeyID(dir, "abc123def456"); err != nil {
		t.Fatalf("updateExpoUpdatesKeyID: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != src {
		t.Errorf("file modified despite no codeSigningMetadata block; got: %s", got)
	}
}

func TestUpdateExpoUpdatesKeyID_DoesNotTouchOtherKeyids(t *testing.T) {
	dir := t.TempDir()
	// Defensive: any unrelated "keyid" field elsewhere in app.json must
	// stay untouched. The regex is scoped to within codeSigningMetadata.
	src := `{
  "expo": {
    "extra": { "keyid": "DO_NOT_TOUCH" },
    "updates": {
      "codeSigningMetadata": { "keyid": "", "alg": "rsa-v1_5-sha256" }
    }
  }
}
`
	path := filepath.Join(dir, "app.json")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := updateExpoUpdatesKeyID(dir, "newkeyid"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"keyid": "DO_NOT_TOUCH"`) {
		t.Errorf("unrelated keyid was rewritten; got: %s", got)
	}
	if !strings.Contains(string(got), `"keyid": "newkeyid"`) {
		t.Errorf("metadata keyid not rewritten; got: %s", got)
	}
}
