// Package utils — HMAC key derivation helper.
//
// P0 #8b (mobile audit, 2026-05-04): the worker HMAC scheme used to sign
// Codemagic-originated webhooks (build status, keystore fetch/register,
// OTA cert fetch, OTA publish) used to derive a per-project secret as
//
//	HMAC-SHA256(master, project_id || task_id)
//
// That bound the secret to a single scope value but reused the same
// derived key across:
//
//   - different webhook purposes (a key derived for the build webhook was
//     identical to a key derived for keystore fetch, since both used the
//     same scope);
//   - different builds for the same project (when scope was project_id);
//   - the lifetime of the master (no rotation, no time-binding).
//
// The fix migrates every call site to HKDF-SHA256 with a per-purpose info
// string, so a leak of a derived key for "build-webhook" scope=task_id
// can't be reused to sign keystore fetches or OTA publishes.
//
// Salt is fixed at "wemob-worker-hmac-v1" — bumping the salt suffix is
// how we'd rotate the master if it ever leaked.
package utils

import (
	"crypto/sha256"
	"io"

	"golang.org/x/crypto/hkdf"
)

// hkdfSalt is intentionally constant. Versioning the salt (e.g. "...-v2")
// is how we'd rotate the master without breaking in-flight keys for one
// purpose. Code today doesn't need that knob; keep it simple until it does.
const hkdfSalt = "wemob-worker-hmac-v1"

// DeriveHMACKey derives a 32-byte purpose-bound HMAC key from a master.
//
//   - purpose: a stable label like "build-webhook", "keystore-fetch",
//     "keystore-register", "ota-cert-fetch", "ota-publish".
//   - scope: a stable scope like the project_id or task_id (whichever is
//     appropriate for that webhook).
//
// The returned slice is exactly 32 bytes. The caller hex-encodes it before
// using as the key bytes for the next-stage HMAC over <ts>.<body> so it
// matches the shell-side derivation in codemagic.yaml.
//
// Panics if hkdf.Read ever returns a partial read — which it cannot for
// 32 bytes of output from SHA-256 (one block). Defensive panic, not an
// expected runtime path.
func DeriveHMACKey(master []byte, purpose string, scope string) []byte {
	info := []byte(purpose + ":" + scope)
	reader := hkdf.New(sha256.New, master, []byte(hkdfSalt), info)
	out := make([]byte, 32)
	if _, err := io.ReadFull(reader, out); err != nil {
		panic("hkdf failed: " + err.Error())
	}
	return out
}
