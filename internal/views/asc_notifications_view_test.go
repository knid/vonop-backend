package views

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// TestVerifyHMACSHA256_AcceptsValidSignature locks down the HMAC verification
// against a known fixture. Apple sends signatures as raw hex; some hub-style
// integrations prefix with "sha256=" — both must work.
func TestVerifyHMACSHA256_AcceptsValidSignature(t *testing.T) {
	secret := "test-secret"
	body := []byte(`{"notificationType":"APP_STORE_VERSION_STATE_UPDATED"}`)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))

	if !verifyHMACSHA256(body, secret, want) {
		t.Errorf("expected valid hex signature to pass")
	}
	if !verifyHMACSHA256(body, secret, "sha256="+want) {
		t.Errorf("expected sha256= prefix to pass")
	}
}

// TestVerifyHMACSHA256_RejectsTampered ensures any change to body, secret,
// or signature flips the result to false.
func TestVerifyHMACSHA256_RejectsTampered(t *testing.T) {
	secret := "test-secret"
	body := []byte("hello")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	good := hex.EncodeToString(mac.Sum(nil))

	if verifyHMACSHA256(body, secret, "deadbeef") {
		t.Errorf("bad sig accepted")
	}
	if verifyHMACSHA256([]byte("world"), secret, good) {
		t.Errorf("modified body accepted")
	}
	if verifyHMACSHA256(body, "wrong", good) {
		t.Errorf("wrong secret accepted")
	}
	if verifyHMACSHA256(body, secret, "") {
		t.Errorf("empty signature accepted")
	}
	if verifyHMACSHA256(body, "", good) {
		t.Errorf("empty secret accepted")
	}
}
