package controllers

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/models"
)

// RegisterCodemagicKeystore stores a freshly-generated keystore + key.properties
// from a Codemagic worker. Encrypts both blobs via CredStore and upserts an
// AndroidKeystore row keyed on project_id. Caller (HTTP view) is responsible
// for HMAC validation.
func (c *Controller) RegisterCodemagicKeystore(ctx context.Context, projectIDStr, keystoreB64, propertiesB64 string) error {
	projectID, err := uuid.Parse(projectIDStr)
	if err != nil {
		return fmt.Errorf("invalid project id: %w", err)
	}
	project, err := c.DB.GetAppProjectByID(projectID.String())
	if err != nil {
		return fmt.Errorf("project lookup: %w", err)
	}
	if project == nil || project.OrganizationID == uuid.Nil {
		return errors.New("project not found / no org")
	}
	keystore, err := base64.StdEncoding.DecodeString(keystoreB64)
	if err != nil {
		return fmt.Errorf("decode keystore: %w", err)
	}
	props, err := base64.StdEncoding.DecodeString(propertiesB64)
	if err != nil {
		return fmt.Errorf("decode properties: %w", err)
	}
	if len(keystore) == 0 || len(props) == 0 {
		return errors.New("keystore and properties must both be non-empty")
	}

	keystoreRef, err := c.CredStore.Put(ctx, project.OrganizationID, "android-keystore", keystore)
	if err != nil {
		return fmt.Errorf("credstore keystore put: %w", err)
	}
	propsRef, err := c.CredStore.Put(ctx, project.OrganizationID, "android-keystore-properties", props)
	if err != nil {
		return fmt.Errorf("credstore properties put: %w", err)
	}

	row := &models.AndroidKeystore{
		ProjectID:         projectID,
		OrganizationID:    project.OrganizationID,
		KeystoreCredRef:   credRefFromStorage(keystoreRef),
		PropertiesCredRef: credRefFromStorage(propsRef),
	}
	_, err = c.DB.UpsertAndroidKeystore(ctx, row)
	return err
}

// FetchCodemagicKeystore returns the decrypted keystore + properties as
// base64 for a Codemagic worker. Caller (HTTP view) is responsible for HMAC
// validation.
func (c *Controller) FetchCodemagicKeystore(ctx context.Context, projectIDStr string) (keystoreB64, propsB64 string, err error) {
	projectID, err := uuid.Parse(projectIDStr)
	if err != nil {
		return "", "", fmt.Errorf("invalid project id: %w", err)
	}
	row, err := c.DB.GetAndroidKeystoreByProject(ctx, projectID)
	if err != nil {
		return "", "", fmt.Errorf("keystore lookup: %w", err)
	}
	if row == nil || row.KeystoreCredRef == nil || row.PropertiesCredRef == nil {
		return "", "", errors.New("keystore not registered")
	}
	keystore, err := c.CredStore.Get(ctx, row.OrganizationID, credRefToStorage(row.KeystoreCredRef))
	if err != nil {
		return "", "", fmt.Errorf("credstore keystore get: %w", err)
	}
	props, err := c.CredStore.Get(ctx, row.OrganizationID, credRefToStorage(row.PropertiesCredRef))
	if err != nil {
		return "", "", fmt.Errorf("credstore properties get: %w", err)
	}
	return base64.StdEncoding.EncodeToString(keystore), base64.StdEncoding.EncodeToString(props), nil
}
