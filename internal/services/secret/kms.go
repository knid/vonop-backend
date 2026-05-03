package secret

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/kms"
	kmsTypes "github.com/aws/aws-sdk-go-v2/service/kms/types"
)

// kmsAPI is the minimal subset of the AWS KMS client our KMSManager needs.
// Defined as an interface for testability — production wires this to *kms.Client.
type kmsAPI interface {
	GenerateDataKey(ctx context.Context, in *kms.GenerateDataKeyInput, opts ...func(*kms.Options)) (*kms.GenerateDataKeyOutput, error)
	Decrypt(ctx context.Context, in *kms.DecryptInput, opts ...func(*kms.Options)) (*kms.DecryptOutput, error)
}

// KMSManager wraps AWS KMS for envelope-encryption workflows: NewDataKey
// returns a fresh (plaintext, encrypted_blob) pair; the caller encrypts
// payload with plaintext, persists the encrypted_blob alongside, and
// discards the plaintext. UnwrapDataKey reverses it via KMS.Decrypt.
type KMSManager struct {
	client kmsAPI
	keyID  string // alias/wemob-credentials, full ARN, etc.
}

// NewKMSManager constructs a KMSManager. Pass a real *kms.Client (e.g.
// kms.NewFromConfig(awsCfg)) and a keyID like "alias/wemob-credentials".
func NewKMSManager(client *kms.Client, keyID string) *KMSManager {
	return &KMSManager{client: client, keyID: keyID}
}

// NewDataKey returns (plaintext_dek, encrypted_dek_blob).
// The caller uses plaintext_dek IMMEDIATELY to encrypt payload bytes, then
// discards it. Only the encrypted_dek_blob is persisted alongside the
// ciphertext. To later decrypt, call UnwrapDataKey(encrypted_dek_blob) to
// retrieve the plaintext DEK from KMS.
func (m *KMSManager) NewDataKey(ctx context.Context) ([]byte, []byte, error) {
	out, err := m.client.GenerateDataKey(ctx, &kms.GenerateDataKeyInput{
		KeyId:   &m.keyID,
		KeySpec: kmsTypes.DataKeySpecAes256,
	})
	if err != nil {
		return nil, nil, err
	}
	return out.Plaintext, out.CiphertextBlob, nil
}

// UnwrapDataKey takes the persisted encrypted_dek_blob and asks KMS to
// return the plaintext DEK. IAM-controlled, audited via CloudTrail.
func (m *KMSManager) UnwrapDataKey(ctx context.Context, encryptedDek []byte) ([]byte, error) {
	out, err := m.client.Decrypt(ctx, &kms.DecryptInput{
		CiphertextBlob: encryptedDek,
	})
	if err != nil {
		return nil, err
	}
	return out.Plaintext, nil
}
