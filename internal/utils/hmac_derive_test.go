package utils

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"testing"
)

// TestDeriveHMACKey_DifferentPurposesProduceDifferentKeys verifies that the
// purpose-binding works: a derived key for "build-webhook" with scope "X"
// must not equal one for "keystore-fetch" with the same scope. This is the
// whole point of the migration — a leak of one purpose's key must not
// affect any other purpose.
func TestDeriveHMACKey_DifferentPurposesProduceDifferentKeys(t *testing.T) {
	master := []byte("test-master")
	scope := "00000000-0000-0000-0000-000000000001"
	purposes := []string{
		"build-webhook",
		"keystore-fetch",
		"keystore-register",
		"ota-cert-fetch",
		"ota-publish",
	}
	seen := make(map[string][]byte, len(purposes))
	for _, p := range purposes {
		k := DeriveHMACKey(master, p, scope)
		for prev, prevK := range seen {
			if bytes.Equal(prevK, k) {
				t.Errorf("purpose %q produced same key as %q for scope %q", p, prev, scope)
			}
		}
		seen[p] = k
	}
}

func TestDeriveHMACKey_DifferentScopesProduceDifferentKeys(t *testing.T) {
	master := []byte("test-master")
	purpose := "build-webhook"
	scopes := []string{
		"task-1",
		"task-2",
		"00000000-0000-0000-0000-000000000001",
		"00000000-0000-0000-0000-000000000002",
	}
	seen := make(map[string][]byte, len(scopes))
	for _, s := range scopes {
		k := DeriveHMACKey(master, purpose, s)
		for prev, prevK := range seen {
			if bytes.Equal(prevK, k) {
				t.Errorf("scope %q produced same key as %q for purpose %q", s, prev, purpose)
			}
		}
		seen[s] = k
	}
}

// TestDeriveHMACKey_Deterministic verifies same inputs always produce the
// same output — required because both the backend (Go) and Codemagic
// (shell) compute the derivation independently and must match byte-for-byte.
func TestDeriveHMACKey_Deterministic(t *testing.T) {
	master := []byte("test-master")
	purpose := "build-webhook"
	scope := "00000000-0000-0000-0000-000000000001"
	k1 := DeriveHMACKey(master, purpose, scope)
	k2 := DeriveHMACKey(master, purpose, scope)
	if !bytes.Equal(k1, k2) {
		t.Errorf("expected deterministic output, got %x then %x", k1, k2)
	}
}

func TestDeriveHMACKey_Length32(t *testing.T) {
	master := []byte("test-master")
	k := DeriveHMACKey(master, "build-webhook", "scope")
	if len(k) != 32 {
		t.Errorf("expected 32-byte output, got %d bytes", len(k))
	}
}

// TestDeriveHMACKey_DifferentMastersProduceDifferentKeys verifies the
// derivation actually depends on the master (sanity check — wouldn't be
// HKDF if it didn't).
func TestDeriveHMACKey_DifferentMastersProduceDifferentKeys(t *testing.T) {
	purpose := "build-webhook"
	scope := "scope-1"
	k1 := DeriveHMACKey([]byte("master-1"), purpose, scope)
	k2 := DeriveHMACKey([]byte("master-2"), purpose, scope)
	if bytes.Equal(k1, k2) {
		t.Errorf("different masters produced identical key")
	}
}

// TestDeriveHMACKey_SignaturesUsableForVerification end-to-ends the
// "compute a key on each side, sign on one, verify on the other" loop —
// matching the production flow where codemagic.yaml shells produce the
// derived key and the Go backend recomputes it independently.
func TestDeriveHMACKey_SignaturesUsableForVerification(t *testing.T) {
	master := []byte("test-master")
	purpose := "build-webhook"
	scope := "task-id-123"
	signerKey := DeriveHMACKey(master, purpose, scope)
	verifierKey := DeriveHMACKey(master, purpose, scope)

	body := []byte("the message to sign")
	h := hmac.New(sha256.New, signerKey)
	h.Write(body)
	mac := h.Sum(nil)

	hv := hmac.New(sha256.New, verifierKey)
	hv.Write(body)
	if !hmac.Equal(mac, hv.Sum(nil)) {
		t.Errorf("verifier could not validate signer's MAC — derivation mismatch")
	}
}

// TestDeriveHMACKey_WrongPurposeFailsVerify is the security property test:
// a key derived for "build-webhook" scope=X must NOT verify a MAC that was
// generated with the key for "keystore-fetch" scope=X. If this test fails,
// the migration didn't fix the bug it was meant to fix.
func TestDeriveHMACKey_WrongPurposeFailsVerify(t *testing.T) {
	master := []byte("test-master")
	scope := "00000000-0000-0000-0000-000000000001"
	body := []byte("body")

	signerKey := DeriveHMACKey(master, "build-webhook", scope)
	wrongPurposeVerifierKey := DeriveHMACKey(master, "keystore-fetch", scope)

	h := hmac.New(sha256.New, signerKey)
	h.Write(body)
	mac := h.Sum(nil)

	hv := hmac.New(sha256.New, wrongPurposeVerifierKey)
	hv.Write(body)
	if hmac.Equal(mac, hv.Sum(nil)) {
		t.Errorf("wrong-purpose verifier accepted the MAC — purposes are not separated")
	}
}
