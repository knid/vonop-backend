package controllers

import (
	"context"
	"fmt"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/storage"
)

// ReadApplePrimaryP8 returns the plaintext .p8 contents for an Apple
// connection. Prefers the encrypted CredRef when present; falls back to
// the legacy S3 path during the dual-read transition window.
func (c *Controller) ReadApplePrimaryP8(ctx context.Context, conn *models.AppleConnection) ([]byte, error) {
	if conn == nil {
		return nil, fmt.Errorf("ReadApplePrimaryP8: nil conn")
	}
	if conn.P8KeyCredRef != nil {
		ref := credRefToStorage(conn.P8KeyCredRef)
		return c.CredStore.Get(ctx, conn.OrganizationID, ref)
	}
	return c.UserApiKeysStorage.ReadObjectBytes(ctx, conn.P8KeyPath)
}

// ReadAppleAPNSP8 mirrors ReadApplePrimaryP8 for the APNs key.
func (c *Controller) ReadAppleAPNSP8(ctx context.Context, conn *models.AppleConnection) ([]byte, error) {
	if conn == nil {
		return nil, fmt.Errorf("ReadAppleAPNSP8: nil conn")
	}
	if conn.APNSP8KeyCredRef != nil {
		ref := credRefToStorage(conn.APNSP8KeyCredRef)
		return c.CredStore.Get(ctx, conn.OrganizationID, ref)
	}
	return c.UserApiKeysStorage.ReadObjectBytes(ctx, conn.APNSP8KeyPath)
}

// ReadGoogleSA returns plaintext SA JSON for a Google Play connection.
func (c *Controller) ReadGoogleSA(ctx context.Context, conn *models.GooglePlayConnection) ([]byte, error) {
	if conn == nil {
		return nil, fmt.Errorf("ReadGoogleSA: nil conn")
	}
	if conn.ServiceAccountCredRef != nil {
		ref := credRefToStorage(conn.ServiceAccountCredRef)
		return c.CredStore.Get(ctx, conn.OrganizationID, ref)
	}
	return c.UserApiKeysStorage.ReadObjectBytes(ctx, conn.ServiceAccountPath)
}

// credRefToStorage converts the model-side CredRefJSON (kept in models to
// avoid an import cycle) into the storage-package CredRef the CredStore expects.
func credRefToStorage(m *models.CredRefJSON) *storage.CredRef {
	return &storage.CredRef{
		OrgID:        m.OrgID,
		Kind:         m.Kind,
		BlobS3Key:    m.BlobS3Key,
		DEKBlobS3Key: m.DEKBlobS3Key,
	}
}

// credRefFromStorage is the inverse — used after a Put when persisting the
// returned ref back into a model row.
func credRefFromStorage(s *storage.CredRef) *models.CredRefJSON {
	if s == nil {
		return nil
	}
	return &models.CredRefJSON{
		OrgID:        s.OrgID,
		Kind:         s.Kind,
		BlobS3Key:    s.BlobS3Key,
		DEKBlobS3Key: s.DEKBlobS3Key,
	}
}
