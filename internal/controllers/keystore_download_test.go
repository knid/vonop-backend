package controllers

import (
	"strconv"
	"testing"
)

func TestGenerateNumericOTP_HasRightLength(t *testing.T) {
	for length := 4; length <= 8; length++ {
		otp, err := generateNumericOTP(length)
		if err != nil {
			t.Fatalf("len=%d: %v", length, err)
		}
		if len(otp) != length {
			t.Errorf("len=%d: got otp %q (len %d)", length, otp, len(otp))
		}
		if _, err := strconv.Atoi(otp); err != nil {
			t.Errorf("len=%d: not numeric: %q", length, otp)
		}
	}
}

func TestGenerateNumericOTP_RejectsBadLength(t *testing.T) {
	if _, err := generateNumericOTP(0); err == nil {
		t.Error("expected error for length=0")
	}
	if _, err := generateNumericOTP(13); err == nil {
		t.Error("expected error for length=13")
	}
}

func TestGenerateNumericOTP_RandomEnough(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		otp, _ := generateNumericOTP(6)
		seen[otp] = true
	}
	// 100 samples from 1M space — collisions extremely unlikely.
	if len(seen) < 95 {
		t.Errorf("OTPs not random enough: %d unique out of 100", len(seen))
	}
}

// Behavioral coverage for the Request flow lives in
// keystore_download_integration_test.go — it needs a real Postgres container
// via testcontainers-go and is tagged //go:build integration.
