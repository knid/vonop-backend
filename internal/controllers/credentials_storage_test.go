package controllers

import (
	"testing"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
)

func TestCredRefConversion_Roundtrip(t *testing.T) {
	orig := &models.CredRefJSON{
		OrgID:        uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		Kind:         "apple-p8",
		BlobS3Key:    "orgs/x/credentials/apple-p8/abc.ct",
		DEKBlobS3Key: "orgs/x/credentials/apple-p8/abc.dek",
	}
	storageRef := credRefToStorage(orig)
	back := credRefFromStorage(storageRef)
	if *back != *orig {
		t.Errorf("roundtrip mismatch: got %+v want %+v", back, orig)
	}
	// Also test nil-safe behavior.
	if credRefFromStorage(nil) != nil {
		t.Error("expected nil from credRefFromStorage(nil)")
	}
	// And the storage CredRef has the right fields.
	if storageRef.Kind != "apple-p8" || storageRef.BlobS3Key != orig.BlobS3Key {
		t.Errorf("storage ref fields incorrect: %+v", storageRef)
	}
}
