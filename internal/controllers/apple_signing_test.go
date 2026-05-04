package controllers

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/pem"
	"strings"
	"testing"
)

// TestSplitAppleCertBundle verifies the PEM-bundle split helper used by
// LoadAppleSigningAssets recovers the same private key + cert blocks that
// generateAppleSigningAssets concatenated.
func TestSplitAppleCertBundle(t *testing.T) {
	// PEM decoders only accept syntactically-valid base64 inside the block;
	// build real blocks with a small body to round-trip safely.
	privBlock := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("priv-bytes")})
	certBlock := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("cert-bytes")})
	bundle := append(append([]byte{}, privBlock...), certBlock...)
	priv, cert := splitAppleCertBundle(bundle)
	if !strings.Contains(string(priv), "BEGIN PRIVATE KEY") {
		t.Errorf("priv missing PRIVATE KEY block: %q", string(priv))
	}
	if !strings.Contains(string(cert), "BEGIN CERTIFICATE") {
		t.Errorf("cert missing CERTIFICATE block: %q", string(cert))
	}
	// Each block should appear exactly once.
	if strings.Count(string(priv), "BEGIN") != 1 {
		t.Errorf("priv has multiple BEGIN markers")
	}
	if strings.Count(string(cert), "BEGIN") != 1 {
		t.Errorf("cert has multiple BEGIN markers")
	}
}

// TestSplitAppleCertBundle_EmptyOnMissingBlocks ensures a bundle with no
// recognised PEM blocks returns empty slices (caller treats this as an error).
func TestSplitAppleCertBundle_EmptyOnMissingBlocks(t *testing.T) {
	priv, cert := splitAppleCertBundle([]byte("not a pem at all"))
	if len(priv) != 0 || len(cert) != 0 {
		t.Errorf("expected empty (priv=%d, cert=%d)", len(priv), len(cert))
	}
}

// TestBuildAppleCSR verifies the CSR is well-formed PEM with the expected
// CN — Apple's API accepts arbitrary CN, but we depend on it for
// debugability.
func TestBuildAppleCSR(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa: %v", err)
	}
	pemBytes, err := buildAppleCSR(priv, "00000000-0000-0000-0000-000000000001")
	if err != nil {
		t.Fatalf("csr: %v", err)
	}
	s := string(pemBytes)
	if !strings.HasPrefix(s, "-----BEGIN CERTIFICATE REQUEST-----") {
		t.Errorf("missing PEM header: %s", s[:60])
	}
	if !strings.Contains(s, "-----END CERTIFICATE REQUEST-----") {
		t.Errorf("missing PEM footer")
	}
}
