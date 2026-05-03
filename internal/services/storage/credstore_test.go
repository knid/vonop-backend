package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// fakeS3 implements credStoreS3 for testing without a real S3.
type fakeS3 struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func newFakeS3() *fakeS3 { return &fakeS3{objects: map[string][]byte{}} }

func (f *fakeS3) PutBytes(_ context.Context, key string, body []byte, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects[key] = append([]byte(nil), body...)
	return nil
}
func (f *fakeS3) GetBytes(_ context.Context, key string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.objects[key]
	if !ok {
		return nil, fmt.Errorf("not found: %s", key)
	}
	return append([]byte(nil), b...), nil
}
func (f *fakeS3) DeleteBytes(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.objects, key)
	return nil
}

// fakeKMS implements credStoreKMS for testing: returns a deterministic DEK so
// roundtrip is verifiable without real KMS calls.
type fakeKMS struct {
	dek []byte
}

func newFakeKMS() *fakeKMS { return &fakeKMS{dek: bytes.Repeat([]byte{7}, 32)} }

func (f *fakeKMS) NewDataKey(_ context.Context) ([]byte, []byte, error) {
	return f.dek, []byte("fake-encrypted-blob"), nil
}
func (f *fakeKMS) UnwrapDataKey(_ context.Context, _ []byte) ([]byte, error) {
	return f.dek, nil
}

func TestCredStore_PutGetRoundtrip(t *testing.T) {
	cs := NewCredStore(newFakeS3(), newFakeKMS())
	orgID := uuid.New()
	plaintext := []byte("very secret p8 contents")

	ref, err := cs.Put(context.Background(), orgID, "apple-p8", plaintext)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if ref.OrgID != orgID {
		t.Errorf("ref orgID: got %v want %v", ref.OrgID, orgID)
	}
	if ref.Kind != "apple-p8" {
		t.Errorf("ref kind: got %q want apple-p8", ref.Kind)
	}
	if ref.BlobS3Key == "" || ref.DEKBlobS3Key == "" {
		t.Error("ref S3 keys must not be empty")
	}

	got, err := cs.Get(context.Background(), orgID, ref)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Errorf("roundtrip mismatch")
	}
}

func TestCredStore_RejectsCrossOrgRead(t *testing.T) {
	cs := NewCredStore(newFakeS3(), newFakeKMS())
	ownerOrg := uuid.New()
	otherOrg := uuid.New()

	ref, _ := cs.Put(context.Background(), ownerOrg, "apple-p8", []byte("secret"))
	_, err := cs.Get(context.Background(), otherOrg, ref)
	if err == nil {
		t.Error("expected cross-org read to fail")
	}
	if !errors.Is(err, ErrCrossOrgAccess) {
		t.Errorf("expected ErrCrossOrgAccess, got %v", err)
	}
}

func TestCredStore_DeleteAlsoEnforcesOrg(t *testing.T) {
	s3 := newFakeS3()
	cs := NewCredStore(s3, newFakeKMS())
	ownerOrg := uuid.New()
	otherOrg := uuid.New()
	ref, _ := cs.Put(context.Background(), ownerOrg, "google-sa", []byte("svcacct"))

	// Cross-org delete rejected
	if err := cs.Delete(context.Background(), otherOrg, ref); err == nil {
		t.Error("expected cross-org delete to fail")
	}
	if _, err := s3.GetBytes(context.Background(), ref.BlobS3Key); err != nil {
		t.Error("blob should still exist after rejected delete")
	}

	// Owner delete succeeds
	if err := cs.Delete(context.Background(), ownerOrg, ref); err != nil {
		t.Errorf("owner delete: %v", err)
	}
	if _, err := s3.GetBytes(context.Background(), ref.BlobS3Key); err == nil {
		t.Error("blob should be gone after delete")
	}
}

func TestCredStore_ScopesS3KeysUnderOrg(t *testing.T) {
	cs := NewCredStore(newFakeS3(), newFakeKMS())
	orgID := uuid.New()
	ref, _ := cs.Put(context.Background(), orgID, "apple-p8", []byte("x"))

	expectedPrefix := fmt.Sprintf("orgs/%s/credentials/apple-p8/", orgID.String())
	if !strings.HasPrefix(ref.BlobS3Key, expectedPrefix) {
		t.Errorf("BlobS3Key = %q, expected prefix %q", ref.BlobS3Key, expectedPrefix)
	}
	if !strings.HasPrefix(ref.DEKBlobS3Key, expectedPrefix) {
		t.Errorf("DEKBlobS3Key = %q, expected prefix %q", ref.DEKBlobS3Key, expectedPrefix)
	}
}
