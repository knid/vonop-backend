package utils

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestAssetHash_Deterministic(t *testing.T) {
	b := []byte("the quick brown fox jumps over the lazy dog")
	got1 := AssetHash(b)
	got2 := AssetHash(b)
	if got1 != got2 {
		t.Errorf("AssetHash not deterministic: %q vs %q", got1, got2)
	}
}

func TestAssetHash_URLSafeNoPadding(t *testing.T) {
	// Pick a payload that produces +/= in standard base64 to exercise the
	// URL-safe encoding.
	b := make([]byte, 64)
	for i := range b {
		b[i] = byte(i)
	}
	got := AssetHash(b)
	if strings.ContainsAny(got, "+/=") {
		t.Errorf("AssetHash %q contains non-URL-safe chars", got)
	}
	// Round-trip: must decode cleanly via RawURLEncoding.
	if _, err := base64.RawURLEncoding.DecodeString(got); err != nil {
		t.Errorf("AssetHash %q does not round-trip: %v", got, err)
	}
}

func TestManifestID_UUIDv4Bits(t *testing.T) {
	b := []byte(`{"foo":"bar"}`)
	u := ManifestID(b)
	// Version nibble in byte 6 must be 4.
	if v := (u[6] >> 4) & 0xf; v != 4 {
		t.Errorf("ManifestID byte 6 high nibble = %x, want 4", v)
	}
	// Variant: top two bits of byte 8 must be 0b10.
	if (u[8] >> 6) != 0b10 {
		t.Errorf("ManifestID byte 8 top two bits = %b, want 10", (u[8] >> 6))
	}
}

func TestManifestID_Deterministic(t *testing.T) {
	b := []byte(`{"id":1}`)
	u1 := ManifestID(b)
	u2 := ManifestID(b)
	if u1 != u2 {
		t.Errorf("ManifestID not deterministic: %s vs %s", u1, u2)
	}
}
