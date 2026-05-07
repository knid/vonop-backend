//go:build integration

package controllers

import (
	"testing"

	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/services/storage"
)

// TestControllerOptions configures NewTestController. All fields optional.
type TestControllerOptions struct {
	// Secrets seeds the fakeSecretManager. nil → empty map.
	Secrets map[string]string
	// Env is the value returned by SecretManager.GetEnv(). Defaults to "test".
	Env string
}

// TestFakes bundles the fake services wired into the Controller so tests
// can assert on captured calls (events) or peek at stored blobs (S3).
type TestFakes struct {
	S3      *fakeCredStoreS3
	KMS     fakeKMS
	Secrets *fakeSecretManager
	Events  *fakeEventService
}

// NewTestController returns a *Controller wired with in-memory fakes for
// CredStore (KMS+S3), SecretManager, and EventService, plus the supplied
// real *Database. All other Controller fields are zero-valued — tests that
// need additional dependencies should set them on the returned Controller
// directly.
//
// The Controller is constructed via struct literal (not NewController) so
// tests don't have to provide every positional dependency.
//
// KNOWN nil fields (intentional — set explicitly in tests that need them):
//   - UserApiKeysStorage, AssetsStorage, AndroidKeysStorage (legacy *S3Storage)
//   - SlackService, TaskService, AsynqInspector
//   - FlyService, GiteaService, NeonService, GitRepoService
//   - CodemagicService, RemoteConfig, E2BService, PostHog, CaddyAdmin, EdgeIPs
//
// Tests that touch S3 directly (presigned URL generation, RedeemKeystoreDownload's
// S3 read, the encryptAndStoreCredential staging-bucket read used by Create/Update
// connection flows) will nil-deref on the storage fields. Inject a real or fake
// *storage.S3Storage on the returned Controller in those tests.
func NewTestController(t *testing.T, db *database.Database, opts TestControllerOptions) (*Controller, *TestFakes) {
	t.Helper()

	fakes := &TestFakes{
		S3:      newFakeCredStoreS3(),
		KMS:     fakeKMS{},
		Secrets: newFakeSecretManager(opts.Env, opts.Secrets),
		Events:  newFakeEventService(),
	}

	credStore := storage.NewCredStore(fakes.S3, fakes.KMS)

	c := &Controller{
		DB:            db,
		SecretManager: fakes.Secrets,
		EventService:  fakes.Events,
		CredStore:     credStore,
	}

	return c, fakes
}
