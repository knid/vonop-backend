package models

import (
	"encoding/json"

	"github.com/google/uuid"
)

// CredRefJSON is the model-package mirror of storage.CredRef. Defined here
// to avoid a models -> services/storage -> utils -> models cycle. Convert
// via helpers in controllers at the boundary.
type CredRefJSON struct {
	OrgID        uuid.UUID `json:"org_id"`
	Kind         string    `json:"kind"`
	BlobS3Key    string    `json:"blob_s3_key"`
	DEKBlobS3Key string    `json:"dek_blob_s3_key"`
}

func (r CredRefJSON) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		OrgID        string `json:"org_id"`
		Kind         string `json:"kind"`
		BlobS3Key    string `json:"blob_s3_key"`
		DEKBlobS3Key string `json:"dek_blob_s3_key"`
	}{r.OrgID.String(), r.Kind, r.BlobS3Key, r.DEKBlobS3Key})
}

func (r *CredRefJSON) UnmarshalJSON(data []byte) error {
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
		return err
	}
	r.OrgID = id
	r.Kind = raw.Kind
	r.BlobS3Key = raw.BlobS3Key
	r.DEKBlobS3Key = raw.DEKBlobS3Key
	return nil
}
