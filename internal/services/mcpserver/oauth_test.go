package mcpserver

import (
	"crypto/sha256"
	"encoding/base64"
	"testing"
	"time"
)

func TestVerifyS256(t *testing.T) {
	verifier := "test-verifier"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	if !verifyS256(verifier, challenge) {
		t.Fatal("expected verifier to match challenge")
	}
	if verifyS256(verifier, "wrong") {
		t.Fatal("expected verifier to reject wrong challenge")
	}
}

func TestCodeStoreTakeConsumesCode(t *testing.T) {
	store := &codeStore{codes: map[string]authCode{}}
	store.put(authCode{Code: "code-1", ExpiresAt: time.Now().Add(time.Minute)})

	if got := store.take("code-1"); got == nil {
		t.Fatal("expected first take to return code")
	}
	if got := store.take("code-1"); got != nil {
		t.Fatal("expected second take to consume code")
	}
}

func TestCodeStoreTakeRejectsExpiredCode(t *testing.T) {
	store := &codeStore{codes: map[string]authCode{}}
	store.put(authCode{Code: "code-1", ExpiresAt: time.Now().Add(-time.Minute)})

	if got := store.take("code-1"); got != nil {
		t.Fatal("expected expired code to be rejected")
	}
}
