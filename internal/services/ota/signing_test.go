package ota

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"
)

// TestSignManifest_RoundTripWithSelfSignedCert generates a fresh keypair,
// signs a manifest body, and verifies via the corresponding self-signed
// X.509 cert. Mirrors the runtime path closely enough that any breakage
// in the parse/sign/verify pipeline is caught here without DB.
func TestSignManifest_RoundTripWithSelfSignedCert(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa gen: %v", err)
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal pkcs8: %v", err)
	}
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8})

	now := time.Now().UTC()
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test.wemob-ota"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	manifestBody := []byte(`{"id":"abc","runtimeVersion":"fingerprint:test"}`)
	header, err := SignManifest(manifestBody, privPEM, "deadbeef00000000")
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	for _, want := range []string{`sig="`, `keyid="deadbeef00000000"`, `alg="rsa-v1_5-sha256"`} {
		if !strings.Contains(header, want) {
			t.Errorf("header missing %q: %s", want, header)
		}
	}

	// Pull sig out of the header and verify against the cert.
	sig := extractAttr(header, "sig")
	if err := VerifyManifestSignature(manifestBody, certPEM, sig); err != nil {
		t.Errorf("verify failed: %v", err)
	}

	// Tampered body should fail.
	tamper := append([]byte{}, manifestBody...)
	tamper[0] ^= 0x01
	if err := VerifyManifestSignature(tamper, certPEM, sig); err == nil {
		t.Error("tampered body verified — signature is not actually checked")
	}
}

// TestSignManifest_RejectsBadInputs surfaces obvious caller errors.
func TestSignManifest_RejectsBadInputs(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	pkcs8, _ := x509.MarshalPKCS8PrivateKey(priv)
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8})

	if _, err := SignManifest([]byte("body"), privPEM, ""); err == nil {
		t.Error("empty keyID was accepted")
	}
	if _, err := SignManifest([]byte("body"), []byte("not a pem"), "k"); err == nil {
		t.Error("garbage PEM was accepted")
	}
}

func extractAttr(header, name string) string {
	for _, kv := range strings.Split(header, ",") {
		kv = strings.TrimSpace(kv)
		if !strings.HasPrefix(kv, name+"=") {
			continue
		}
		return strings.Trim(kv[len(name)+1:], `"`)
	}
	return ""
}
