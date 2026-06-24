package secret

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
)

// LocalDEKManager is a no-AWS-KMS replacement that uses a single static DEK
// derived from a hex-encoded secret. Implements the same surface CredStore
// expects (NewDataKey + UnwrapDataKey) so the storage layer can't tell which
// key manager it's wired to.
//
// Trade-offs vs KMSManager:
//   - No key rotation (single DEK across all blobs and all time)
//   - No IAM-gated access (anyone with read access to the secret can decrypt)
//   - No CloudTrail audit log of unwrap calls
//   - Real AES-256-GCM encryption is still in place — only the KMS round-trip
//     is skipped
//
// Use only for dev/staging when you want CredStore to function without an AWS
// KMS dependency. Production should use KMSManager.
//
// **One-way:** rows encrypted under LocalDEKManager are unreadable by
// KMSManager (the persisted "DEK blob" is meaningless to KMS.Decrypt).
// Switching from local mode to real KMS requires re-encrypting all existing
// rows. Document loudly when enabling local mode.
type LocalDEKManager struct {
	dek []byte
}

// localDEKBlob is the sentinel value stored where KMSManager would put an
// encrypted DEK ciphertext blob. The blob has no cryptographic meaning in
// local mode; UnwrapDataKey ignores its contents and returns the static DEK.
// Tagged with "wemob-local-dek-v1:" so an operator inspecting raw S3 objects
// can tell the row was written under local mode.
var localDEKBlob = []byte("wemob-local-dek-v1:placeholder")

// NewLocalDEKManager constructs a LocalDEKManager from a hex-encoded 32-byte key.
// AES-256-GCM requires exactly 32 bytes; anything else returns an error.
//
// Generate with: openssl rand -hex 32
func NewLocalDEKManager(hexKey string) (*LocalDEKManager, error) {
	if hexKey == "" {
		return nil, errors.New("LocalDEKManager: hex key is empty")
	}
	dek, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("LocalDEKManager: hex decode: %w", err)
	}
	if len(dek) != 32 {
		return nil, fmt.Errorf("LocalDEKManager: expected 32-byte key (got %d bytes — generate with: openssl rand -hex 32)", len(dek))
	}
	return &LocalDEKManager{dek: dek}, nil
}

// NewDataKey returns the static DEK + a sentinel blob. The blob is persisted
// alongside ciphertext but UnwrapDataKey ignores its contents.
func (m *LocalDEKManager) NewDataKey(_ context.Context) ([]byte, []byte, error) {
	// Return copies so callers can't mutate the underlying key.
	dekCopy := make([]byte, len(m.dek))
	copy(dekCopy, m.dek)
	blobCopy := make([]byte, len(localDEKBlob))
	copy(blobCopy, localDEKBlob)
	return dekCopy, blobCopy, nil
}

// UnwrapDataKey ignores the encryptedDek argument and returns the static DEK.
// The blob is persisted alongside ciphertext for format consistency with
// KMSManager but has no cryptographic meaning in local mode.
func (m *LocalDEKManager) UnwrapDataKey(_ context.Context, _ []byte) ([]byte, error) {
	dekCopy := make([]byte, len(m.dek))
	copy(dekCopy, m.dek)
	return dekCopy, nil
}
