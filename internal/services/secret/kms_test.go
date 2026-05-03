package secret

import (
	"bytes"
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/kms"
)

type fakeKMSAPI struct {
	plaintextDEK    []byte
	encryptedBlob   []byte
	decryptResult   []byte
	decryptCallBlob []byte
	decryptError    error
	decryptCalled   int
	generateCalled  int
}

func (f *fakeKMSAPI) GenerateDataKey(_ context.Context, _ *kms.GenerateDataKeyInput, _ ...func(*kms.Options)) (*kms.GenerateDataKeyOutput, error) {
	f.generateCalled++
	return &kms.GenerateDataKeyOutput{
		Plaintext:      f.plaintextDEK,
		CiphertextBlob: f.encryptedBlob,
	}, nil
}

func (f *fakeKMSAPI) Decrypt(_ context.Context, in *kms.DecryptInput, _ ...func(*kms.Options)) (*kms.DecryptOutput, error) {
	f.decryptCalled++
	f.decryptCallBlob = in.CiphertextBlob
	if f.decryptError != nil {
		return nil, f.decryptError
	}
	return &kms.DecryptOutput{Plaintext: f.decryptResult}, nil
}

func TestKMSManager_NewDataKey(t *testing.T) {
	dek := bytes.Repeat([]byte{42}, 32)
	blob := []byte("ciphertext-blob")
	fk := &fakeKMSAPI{plaintextDEK: dek, encryptedBlob: blob}
	mgr := &KMSManager{client: fk, keyID: "alias/wemob"}

	plaintext, encryptedDek, err := mgr.NewDataKey(context.Background())
	if err != nil {
		t.Fatalf("NewDataKey: %v", err)
	}
	if !bytes.Equal(plaintext, dek) {
		t.Error("plaintext DEK mismatch")
	}
	if !bytes.Equal(encryptedDek, blob) {
		t.Error("encrypted DEK blob mismatch")
	}
	if fk.generateCalled != 1 {
		t.Errorf("expected 1 GenerateDataKey call, got %d", fk.generateCalled)
	}
}

func TestKMSManager_UnwrapDataKey(t *testing.T) {
	dek := bytes.Repeat([]byte{42}, 32)
	blob := []byte("encrypted-blob")
	fk := &fakeKMSAPI{decryptResult: dek}
	mgr := &KMSManager{client: fk, keyID: "alias/wemob"}

	out, err := mgr.UnwrapDataKey(context.Background(), blob)
	if err != nil {
		t.Fatalf("UnwrapDataKey: %v", err)
	}
	if !bytes.Equal(out, dek) {
		t.Error("unwrapped DEK mismatch")
	}
	if fk.decryptCalled != 1 {
		t.Errorf("expected 1 Decrypt call, got %d", fk.decryptCalled)
	}
	if !bytes.Equal(fk.decryptCallBlob, blob) {
		t.Error("Decrypt called with wrong blob")
	}
}
