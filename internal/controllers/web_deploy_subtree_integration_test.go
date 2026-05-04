//go:build integration

package controllers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEnsureNextjsDockerfile_WritesWhenAbsent verifies that the helper writes
// the default standalone-output Next.js Dockerfile into a directory that has
// no Dockerfile, and that the written content matches the canonical template.
func TestEnsureNextjsDockerfile_WritesWhenAbsent(t *testing.T) {
	dir := t.TempDir()

	if err := ensureNextjsDockerfile(dir); err != nil {
		t.Fatalf("ensureNextjsDockerfile returned error: %v", err)
	}

	dockerPath := filepath.Join(dir, "Dockerfile")
	got, err := os.ReadFile(dockerPath)
	if err != nil {
		t.Fatalf("expected Dockerfile to exist at %s: %v", dockerPath, err)
	}

	contents := string(got)
	for _, want := range []string{
		"FROM node:22-alpine AS deps",
		"mkdir -p ./public",
		"next/standalone",
	} {
		if !strings.Contains(contents, want) {
			t.Errorf("default Dockerfile missing expected snippet %q\nfull contents:\n%s", want, contents)
		}
	}
}

// TestEnsureNextjsDockerfile_NoOverwriteWhenPresent verifies that a
// pre-existing Dockerfile (e.g. one the agent generated, or a hand-written
// override) is left untouched. This is the contract that lets users override
// the default by writing their own backend/Dockerfile.
func TestEnsureNextjsDockerfile_NoOverwriteWhenPresent(t *testing.T) {
	dir := t.TempDir()
	dockerPath := filepath.Join(dir, "Dockerfile")
	custom := []byte("# custom user Dockerfile\nFROM python:3.12-slim\nCMD [\"python\", \"app.py\"]\n")
	if err := os.WriteFile(dockerPath, custom, 0o644); err != nil {
		t.Fatalf("seed custom Dockerfile: %v", err)
	}

	if err := ensureNextjsDockerfile(dir); err != nil {
		t.Fatalf("ensureNextjsDockerfile returned error: %v", err)
	}

	got, err := os.ReadFile(dockerPath)
	if err != nil {
		t.Fatalf("read Dockerfile after helper: %v", err)
	}
	if string(got) != string(custom) {
		t.Errorf("ensureNextjsDockerfile overwrote a pre-existing Dockerfile\nwant: %q\ngot:  %q", custom, got)
	}
}
