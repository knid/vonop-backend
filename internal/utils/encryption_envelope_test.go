package utils

import (
	"bytes"
	"testing"
)

func TestEnvelopeEncryptDecrypt_Roundtrip(t *testing.T) {
	plaintext := []byte("super secret p8 file content -----BEGIN PRIVATE KEY-----\nfoo\n-----END PRIVATE KEY-----\n")

	dek := make([]byte, 32)
	for i := range dek {
		dek[i] = byte(i)
	}

	ciphertext, err := EncryptBytesEnvelope(plaintext, dek)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if bytes.Equal(ciphertext, plaintext) {
		t.Error("ciphertext equals plaintext (no encryption happened)")
	}
	if len(ciphertext) <= 12+16 {
		t.Errorf("ciphertext suspiciously short: %d bytes (expected nonce+ct+tag)", len(ciphertext))
	}

	decrypted, err := DecryptBytesEnvelope(ciphertext, dek)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if !bytes.Equal(decrypted, plaintext) {
		t.Errorf("roundtrip mismatch")
	}
}

func TestEnvelopeDecrypt_TamperedCiphertext(t *testing.T) {
	dek := make([]byte, 32)
	plaintext := []byte("hello")
	ct, _ := EncryptBytesEnvelope(plaintext, dek)
	ct[len(ct)-1] ^= 0xff // flip last byte (tag)
	if _, err := DecryptBytesEnvelope(ct, dek); err == nil {
		t.Error("expected tamper-detection error")
	}
}

func TestEnvelopeDecrypt_WrongDEK(t *testing.T) {
	dek1 := make([]byte, 32)
	dek2 := make([]byte, 32)
	dek2[0] = 1
	plaintext := []byte("hello")
	ct, _ := EncryptBytesEnvelope(plaintext, dek1)
	if _, err := DecryptBytesEnvelope(ct, dek2); err == nil {
		t.Error("expected decryption to fail with wrong DEK")
	}
}

func TestEnvelopeEncrypt_RejectsWrongDEKSize(t *testing.T) {
	if _, err := EncryptBytesEnvelope([]byte("hi"), make([]byte, 16)); err == nil {
		t.Error("expected error for 16-byte DEK (must be 32)")
	}
	if _, err := EncryptBytesEnvelope([]byte("hi"), nil); err == nil {
		t.Error("expected error for nil DEK")
	}
}

func TestEnvelopeDecrypt_RejectsShortCiphertext(t *testing.T) {
	dek := make([]byte, 32)
	if _, err := DecryptBytesEnvelope([]byte("short"), dek); err == nil {
		t.Error("expected error for too-short ciphertext")
	}
}
