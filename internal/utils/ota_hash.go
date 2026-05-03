package utils

import (
	"crypto/sha256"
	"encoding/base64"

	"github.com/google/uuid"
)

// AssetHash returns the URL-safe base64 SHA-256 of b without padding,
// matching the asset hash format expected by the expo-updates client
// (see github.com/expo/custom-expo-updates-server reference impl).
func AssetHash(b []byte) string {
	sum := sha256.Sum256(b)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// ManifestID returns a UUIDv5-style UUID derived from the SHA-256 of the
// manifest bytes. The expo-updates client uses this as the persistent
// identity of an update; equality with the locally-installed update ID
// means "no new update available", so the derivation must be deterministic.
//
// Per RFC 4122 §4.4, we set:
//   - the high nibble of byte 6 to 0x4 (version 4 — random/SHA-derived)
//   - the high two bits of byte 8 to 0b10 (variant RFC 4122)
//
// The reference Next.js server (custom-expo-updates-server) uses a similar
// scheme to derive its update IDs.
func ManifestID(b []byte) uuid.UUID {
	sum := sha256.Sum256(b)
	var u uuid.UUID
	copy(u[:], sum[:16])
	u[6] = (u[6] & 0x0f) | 0x40 // version 4
	u[8] = (u[8] & 0x3f) | 0x80 // variant RFC 4122
	return u
}
