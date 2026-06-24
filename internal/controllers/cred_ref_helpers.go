package controllers

import (
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/storage"
)

// credRefToStorage converts the model-side CredRefJSON into the storage
// package shape expected by CredStore.
func credRefToStorage(m *models.CredRefJSON) *storage.CredRef {
	if m == nil {
		return nil
	}
	return &storage.CredRef{
		OrgID:        m.OrgID,
		Kind:         m.Kind,
		BlobS3Key:    m.BlobS3Key,
		DEKBlobS3Key: m.DEKBlobS3Key,
	}
}

// credRefFromStorage is the inverse used after storing a new secret.
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
