package secret

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestNewLocalDEKManager_RejectsEmpty(t *testing.T) {
	if _, err := NewLocalDEKManager(""); err == nil {
		t.Fatal("expected error for empty key")
	}
}

func TestNewLocalDEKManager_RejectsBadHex(t *testing.T) {
	if _, err := NewLocalDEKManager("not-hex-zz"); err == nil {
		t.Fatal("expected error for non-hex string")
	}
}

func TestNewLocalDEKManager_RejectsWrongLength(t *testing.T) {
	// 16 bytes (32 hex chars) instead of 32 bytes (64 hex chars).
	short := strings.Repeat("ab", 16)
	if _, err := NewLocalDEKManager(short); err == nil {
		t.Fatal("expected error for 16-byte key")
	}
}

func TestLocalDEKManager_RoundTrip(t *testing.T) {
	// 32 bytes hex = 64 chars
	hexKey := strings.Repeat("ab", 32)
	m, err := NewLocalDEKManager(hexKey)
	if err != nil {
		t.Fatalf("construct: %v", err)
	}

	plainDEK1, blob1, err := m.NewDataKey(context.Background())
	if err != nil {
		t.Fatalf("NewDataKey: %v", err)
	}
	if len(plainDEK1) != 32 {
		t.Errorf("DEK length: got %d want 32", len(plainDEK1))
	}
	if len(blob1) == 0 {
		t.Error("blob must not be empty")
	}

	// UnwrapDataKey should return the same DEK regardless of blob contents.
	plainDEK2, err := m.UnwrapDataKey(context.Background(), blob1)
	if err != nil {
		t.Fatalf("UnwrapDataKey: %v", err)
	}
	if !bytes.Equal(plainDEK1, plainDEK2) {
		t.Error("DEK from NewDataKey and UnwrapDataKey must match")
	}

	// Even garbage blobs return the same DEK in local mode.
	plainDEK3, err := m.UnwrapDataKey(context.Background(), []byte("garbage"))
	if err != nil {
		t.Fatalf("UnwrapDataKey with garbage blob: %v", err)
	}
	if !bytes.Equal(plainDEK1, plainDEK3) {
		t.Error("DEK should be the same for any blob in local mode")
	}
}

func TestLocalDEKManager_NewDataKey_ReturnsCopies(t *testing.T) {
	hexKey := strings.Repeat("cd", 32)
	m, _ := NewLocalDEKManager(hexKey)

	dek1, _, _ := m.NewDataKey(context.Background())
	dek2, _, _ := m.NewDataKey(context.Background())

	// Mutating the returned slice must not affect the next call's return.
	dek1[0] = 0xFF
	if dek2[0] == 0xFF {
		t.Error("NewDataKey must return defensive copies, not aliases")
	}
}
