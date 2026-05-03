package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/utils"
)

// ErrCrossOrgAccess is returned when a CredStore Get/Delete is called
// with a requesting orgID that doesn't match the CredRef's owner.
var ErrCrossOrgAccess = errors.New("credstore: cross-org access denied")

// credStoreS3 is the minimal S3 surface CredStore needs. Defined here as
// an interface so tests can use a fake without instantiating S3Storage.
// *S3Storage satisfies this interface via PutBytes/GetBytes/DeleteBytes
// wrapper methods added below.
type credStoreS3 interface {
	PutBytes(ctx context.Context, key string, body []byte, contentType string) error
	GetBytes(ctx context.Context, key string) ([]byte, error)
	DeleteBytes(ctx context.Context, key string) error
}

// credStoreKMS is the KMS surface CredStore needs.
// *secret.KMSManager satisfies this interface.
type credStoreKMS interface {
	NewDataKey(ctx context.Context) (plaintext, encryptedBlob []byte, err error)
	UnwrapDataKey(ctx context.Context, encryptedBlob []byte) ([]byte, error)
}

// CredStore is the high-level secure-credential storage layer. Encrypts on
// Put with a fresh KMS-issued DEK; ciphertext + encrypted DEK go to S3 under
// org-scoped prefixes. Get rejects cross-org reads and unwraps the DEK to
// decrypt.
type CredStore struct {
	s3  credStoreS3
	kms credStoreKMS
}

// NewCredStore constructs a CredStore.
func NewCredStore(s3 credStoreS3, kms credStoreKMS) *CredStore {
	return &CredStore{s3: s3, kms: kms}
}

// CredRef is the persistent handle to a stored credential. Persist this in
// the AppleConnection / GooglePlayConnection / etc. row; pass it back to
// Get when reading. Holds no plaintext.
type CredRef struct {
	OrgID        uuid.UUID
	Kind         string // "apple-p8" | "apple-apns" | "google-sa" | "android-keystore" | etc.
	BlobS3Key    string
	DEKBlobS3Key string
}

// MarshalJSON implements json.Marshaler so CredRef can be persisted as a
// jsonb column via Bun. UUID is marshalled as a string.
func (r CredRef) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		OrgID        string `json:"org_id"`
		Kind         string `json:"kind"`
		BlobS3Key    string `json:"blob_s3_key"`
		DEKBlobS3Key string `json:"dek_blob_s3_key"`
	}{
		OrgID:        r.OrgID.String(),
		Kind:         r.Kind,
		BlobS3Key:    r.BlobS3Key,
		DEKBlobS3Key: r.DEKBlobS3Key,
	})
}

// UnmarshalJSON parses a jsonb-stored CredRef.
func (r *CredRef) UnmarshalJSON(data []byte) error {
	var raw struct {
		OrgID        string `json:"org_id"`
		Kind         string `json:"kind"`
		BlobS3Key    string `json:"blob_s3_key"`
		DEKBlobS3Key string `json:"dek_blob_s3_key"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	id, err := uuid.Parse(raw.OrgID)
	if err != nil {
		return fmt.Errorf("credref: org_id parse: %w", err)
	}
	r.OrgID = id
	r.Kind = raw.Kind
	r.BlobS3Key = raw.BlobS3Key
	r.DEKBlobS3Key = raw.DEKBlobS3Key
	return nil
}

// Put encrypts plaintext, stores ciphertext + encrypted DEK in S3 under
// the org's prefix, returns a CredRef.
func (s *CredStore) Put(ctx context.Context, orgID uuid.UUID, kind string, plaintext []byte) (*CredRef, error) {
	plainDEK, encryptedDEK, err := s.kms.NewDataKey(ctx)
	if err != nil {
		return nil, fmt.Errorf("credstore: kms data key: %w", err)
	}
	ciphertext, err := utils.EncryptBytesEnvelope(plaintext, plainDEK)
	if err != nil {
		return nil, fmt.Errorf("credstore: encrypt: %w", err)
	}

	objectID := uuid.New().String()
	blobKey := fmt.Sprintf("orgs/%s/credentials/%s/%s.ct", orgID, kind, objectID)
	dekKey := fmt.Sprintf("orgs/%s/credentials/%s/%s.dek", orgID, kind, objectID)

	if err := s.s3.PutBytes(ctx, blobKey, ciphertext, "application/octet-stream"); err != nil {
		return nil, fmt.Errorf("credstore: put ciphertext: %w", err)
	}
	if err := s.s3.PutBytes(ctx, dekKey, encryptedDEK, "application/octet-stream"); err != nil {
		return nil, fmt.Errorf("credstore: put dek: %w", err)
	}

	return &CredRef{
		OrgID:        orgID,
		Kind:         kind,
		BlobS3Key:    blobKey,
		DEKBlobS3Key: dekKey,
	}, nil
}

// Get fetches and decrypts. Rejects with ErrCrossOrgAccess if the requesting
// orgID does not match the ref's owner.
func (s *CredStore) Get(ctx context.Context, requestingOrgID uuid.UUID, ref *CredRef) ([]byte, error) {
	if ref == nil {
		return nil, errors.New("credstore: nil ref")
	}
	if ref.OrgID != requestingOrgID {
		return nil, fmt.Errorf("%w: ref owner %s, requested by %s", ErrCrossOrgAccess, ref.OrgID, requestingOrgID)
	}
	encDEK, err := s.s3.GetBytes(ctx, ref.DEKBlobS3Key)
	if err != nil {
		return nil, fmt.Errorf("credstore: get dek: %w", err)
	}
	plainDEK, err := s.kms.UnwrapDataKey(ctx, encDEK)
	if err != nil {
		return nil, fmt.Errorf("credstore: unwrap dek: %w", err)
	}
	ct, err := s.s3.GetBytes(ctx, ref.BlobS3Key)
	if err != nil {
		return nil, fmt.Errorf("credstore: get ciphertext: %w", err)
	}
	return utils.DecryptBytesEnvelope(ct, plainDEK)
}

// Delete removes both blob and DEK objects from S3. Rejects cross-org
// deletes with ErrCrossOrgAccess.
func (s *CredStore) Delete(ctx context.Context, requestingOrgID uuid.UUID, ref *CredRef) error {
	if ref == nil {
		return errors.New("credstore: nil ref")
	}
	if ref.OrgID != requestingOrgID {
		return fmt.Errorf("%w: ref owner %s, requested by %s", ErrCrossOrgAccess, ref.OrgID, requestingOrgID)
	}
	if err := s.s3.DeleteBytes(ctx, ref.BlobS3Key); err != nil {
		return fmt.Errorf("credstore: delete ciphertext: %w", err)
	}
	if err := s.s3.DeleteBytes(ctx, ref.DEKBlobS3Key); err != nil {
		return fmt.Errorf("credstore: delete dek: %w", err)
	}
	return nil
}

// Compile-time interface satisfaction check.
var _ credStoreS3 = (*S3Storage)(nil)
