//go:build integration

package controllers

import (
	"context"
	"errors"
	"sync"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/events"
	"github.com/knid/wemob/internal/services/secret"
)

// fakeSecretManager is an in-memory secret.SecretManager. Returns "" for
// any unset key (matching production behavior of an Infisical miss).
type fakeSecretManager struct {
	env     string
	secrets map[string]string
}

func newFakeSecretManager(env string, secrets map[string]string) *fakeSecretManager {
	if secrets == nil {
		secrets = map[string]string{}
	}
	if env == "" {
		env = "test"
	}
	return &fakeSecretManager{env: env, secrets: secrets}
}

func (f *fakeSecretManager) GetSecret(key string) string { return f.secrets[key] }
func (f *fakeSecretManager) GetEnv() string              { return f.env }

// Compile-time check.
var _ secret.SecretManager = (*fakeSecretManager)(nil)

// fakeEventCall records a single SendEvent invocation.
type fakeEventCall struct {
	User       *models.User
	Event      constants.EventName
	Attributes map[string]any
}

// fakeEventService captures SendEvent calls into an in-memory slice. Safe
// for concurrent fan-out from multiple goroutines (production callers wrap
// SendEvent in `go ...`).
type fakeEventService struct {
	mu    sync.Mutex
	Calls []fakeEventCall
}

func newFakeEventService() *fakeEventService {
	return &fakeEventService{}
}

func (f *fakeEventService) SendEvent(user *models.User, event constants.EventName, attributes map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, fakeEventCall{User: user, Event: event, Attributes: attributes})
	return nil
}

// Compile-time check.
var _ events.EventService = (*fakeEventService)(nil)

// fakeCredStoreS3 implements the credStoreS3 interface from
// internal/services/storage/credstore.go using an in-memory map.
type fakeCredStoreS3 struct {
	mu      sync.Mutex
	Objects map[string][]byte
}

func newFakeCredStoreS3() *fakeCredStoreS3 {
	return &fakeCredStoreS3{Objects: map[string][]byte{}}
}

func (f *fakeCredStoreS3) PutBytes(_ context.Context, key string, body []byte, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]byte, len(body))
	copy(cp, body)
	f.Objects[key] = cp
	return nil
}

func (f *fakeCredStoreS3) GetBytes(_ context.Context, key string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.Objects[key]
	if !ok {
		return nil, errors.New("fakeCredStoreS3: key not found: " + key)
	}
	cp := make([]byte, len(v))
	copy(cp, v)
	return cp, nil
}

func (f *fakeCredStoreS3) DeleteBytes(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.Objects, key)
	return nil
}

// fakeKMS implements the credStoreKMS interface. Returns a deterministic
// 32-byte DEK so every Put + Get pair round-trips. The "encrypted blob"
// is just the plaintext DEK in this fake — there is no real wrapping.
type fakeKMS struct{}

// fakeDEK is the deterministic 32-byte data-encryption key returned by
// every NewDataKey call. Tests must not rely on confidentiality.
var fakeDEK = func() []byte {
	k := make([]byte, 32)
	for i := range k {
		k[i] = byte(i + 1)
	}
	return k
}()

func (fakeKMS) NewDataKey(_ context.Context) (plaintext, encryptedBlob []byte, err error) {
	plain := make([]byte, len(fakeDEK))
	copy(plain, fakeDEK)
	enc := make([]byte, len(fakeDEK))
	copy(enc, fakeDEK)
	return plain, enc, nil
}

func (fakeKMS) UnwrapDataKey(_ context.Context, encryptedBlob []byte) ([]byte, error) {
	out := make([]byte, len(encryptedBlob))
	copy(out, encryptedBlob)
	return out, nil
}
