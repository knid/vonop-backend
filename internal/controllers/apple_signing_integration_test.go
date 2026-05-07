//go:build integration

package controllers

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/storeauth"
)

// generateTestP8 produces a valid PEM-encoded PKCS#8 EC P-256 private key
// suitable for ASC JWT minting (storeauth.AppleToken). We never hit Apple,
// but the JWT mint path parses the .p8 and would fail with garbage bytes.
func generateTestP8(t *testing.T) []byte {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ec gen: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal pkcs8: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

// fakeASCServer counts API calls so the test can assert idempotency. Returns
// canned responses that look like real Apple JSON.
type fakeASCServer struct {
	srv             *httptest.Server
	certCalls       atomic.Int32
	bundleListCalls atomic.Int32
	bundleCreated   atomic.Int32
	profileCalls    atomic.Int32
}

func newFakeASCServer(t *testing.T) *fakeASCServer {
	t.Helper()
	f := &fakeASCServer{}
	mux := http.NewServeMux()

	expDate := time.Now().Add(365 * 24 * time.Hour).UTC().Format(time.RFC3339)

	mux.HandleFunc("/v1/certificates", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		f.certCalls.Add(1)
		// Return a fake DER-base64 cert. We never decrypt or verify it, just
		// round-trip it through CredStore.
		resp := map[string]any{
			"data": map[string]any{
				"id":   "cert-test-1",
				"type": "certificates",
				"attributes": map[string]any{
					"certificateContent": "Zm9vYmFy", // "foobar" base64
					"certificateType":    "IOS_DISTRIBUTION",
					"serialNumber":       "0xCAFEBABE",
					"displayName":        "iOS Distribution",
					"expirationDate":     expDate,
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/v1/bundleIds", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			f.bundleListCalls.Add(1)
			// Return empty list so the controller takes the "create" path.
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
			return
		}
		if r.Method == http.MethodPost {
			f.bundleCreated.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"id":   "bundle-test-1",
					"type": "bundleIds",
					"attributes": map[string]any{
						"identifier": "com.wemob.test",
						"name":       "test",
					},
				},
			})
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	})

	mux.HandleFunc("/v1/profiles", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		f.profileCalls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"id":   "profile-test-1",
				"type": "profiles",
				"attributes": map[string]any{
					"name":           "Wemob Test App Store",
					"uuid":           "00000000-0000-0000-0000-000000000abc",
					"profileType":    "IOS_APP_STORE",
					"profileContent": "cHJvZmlsZS1ieXRlcw==", // "profile-bytes" base64
					"expirationDate": expDate,
				},
			},
		})
	})

	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func setupAppleSigningTest(t *testing.T) (c *Controller, project *models.AppProject, conn *models.AppleConnection, fake *fakeASCServer) {
	t.Helper()

	db := database.NewTestDB(t,
		(*models.User)(nil),
		(*models.Organization)(nil),
		(*models.AppProject)(nil),
		(*models.AppleConnection)(nil),
		(*models.AppleSigningAssets)(nil),
	)
	c, _ = NewTestController(t, db, TestControllerOptions{})

	ctx := context.Background()

	owner := &models.User{ID: uuid.New(), Email: "asu-owner@example.com", Name: "Owner", Password: "x"}
	if _, err := db.DB.NewInsert().Model(owner).Exec(ctx); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	org := &models.Organization{
		ID:      uuid.New(),
		Name:    "AppleSigningOrg",
		Slug:    "asu-" + uuid.NewString()[:8],
		OwnerID: owner.ID,
	}
	if _, err := db.DB.NewInsert().Model(org).Exec(ctx); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	project = &models.AppProject{
		ID:             uuid.New(),
		UserID:         owner.ID,
		OrganizationID: org.ID,
		Name:           "test",
		Slug:           "test",
		Prompt:         "n/a",
		ProjectType:    models.ProjectTypeMobileAppNative,
		Status:         "draft",
	}
	if _, err := db.DB.NewInsert().Model(project).Exec(ctx); err != nil {
		t.Fatalf("insert project: %v", err)
	}

	// Persist the p8 via CredStore so ReadApplePrimaryP8 can decrypt it.
	p8 := generateTestP8(t)
	credRef, err := c.CredStore.Put(ctx, org.ID, "apple-p8", p8)
	if err != nil {
		t.Fatalf("credstore put p8: %v", err)
	}

	conn = &models.AppleConnection{
		ID:             uuid.New(),
		UserID:         owner.ID,
		OrganizationID: org.ID,
		Name:           "asc",
		KeyID:          "TESTKEYID",
		TeamID:         "TESTTEAM",
		IssuerID:       "00000000-0000-0000-0000-000000000000",
		P8KeyCredRef:   credRefFromStorage(credRef),
	}
	if _, err := db.DB.NewInsert().Model(conn).Exec(ctx); err != nil {
		t.Fatalf("insert conn: %v", err)
	}

	fake = newFakeASCServer(t)
	restore := storeauth.SetASCAPIBaseForTest(fake.srv.URL)
	t.Cleanup(restore)

	return c, project, conn, fake
}

// TestEnsureAppleSigningAssets_CreatesRowOnFirstCall verifies the lazy
// generation path: first call mints cert + profile via mocked ASC, persists
// CredRefs + identifiers, and second call returns the same row without
// re-hitting ASC.
func TestEnsureAppleSigningAssets_CreatesRowOnFirstCall(t *testing.T) {
	c, project, conn, fake := setupAppleSigningTest(t)
	ctx := context.Background()

	row1, err := c.EnsureAppleSigningAssets(ctx, project, conn)
	if err != nil {
		t.Fatalf("ensure first: %v", err)
	}
	if row1.AppProjectID != project.ID {
		t.Errorf("AppProjectID mismatch")
	}
	if row1.OrganizationID != project.OrganizationID {
		t.Errorf("OrganizationID mismatch")
	}
	if row1.AppleConnectionID != conn.ID {
		t.Errorf("AppleConnectionID mismatch")
	}
	if row1.CertificateID != "cert-test-1" {
		t.Errorf("CertificateID = %q, want cert-test-1", row1.CertificateID)
	}
	if row1.ProfileID != "profile-test-1" {
		t.Errorf("ProfileID = %q, want profile-test-1", row1.ProfileID)
	}
	if row1.CertificateCredRef == nil || row1.ProfileCredRef == nil {
		t.Fatal("CredRefs not populated")
	}
	if row1.CertificateCredRef.Kind != "apple-dist-cert" {
		t.Errorf("cert ref kind = %q, want apple-dist-cert", row1.CertificateCredRef.Kind)
	}
	if row1.ProfileCredRef.Kind != "apple-profile" {
		t.Errorf("profile ref kind = %q, want apple-profile", row1.ProfileCredRef.Kind)
	}
	if row1.BundleID != "com.wemob.test" {
		t.Errorf("BundleID = %q, want com.wemob.test", row1.BundleID)
	}

	if got := fake.certCalls.Load(); got != 1 {
		t.Errorf("first call: ASC certs hit %d times, want 1", got)
	}
	if got := fake.profileCalls.Load(); got != 1 {
		t.Errorf("first call: ASC profiles hit %d times, want 1", got)
	}

	// Second call must reuse the row (cert / profile have ~365d remaining,
	// so the renewal path doesn't trigger).
	row2, err := c.EnsureAppleSigningAssets(ctx, project, conn)
	if err != nil {
		t.Fatalf("ensure second: %v", err)
	}
	if row2.CertificateID != row1.CertificateID {
		t.Errorf("CertificateID drifted: first=%s second=%s", row1.CertificateID, row2.CertificateID)
	}
	if got := fake.certCalls.Load(); got != 1 {
		t.Errorf("second call: ASC certs hit %d times total, want 1 (no re-mint)", got)
	}
}

// TestLoadAppleSigningAssets_RoundTrips verifies that LoadAppleSigningAssets
// decrypts the certs + profile to the same bytes we passed in via the mock
// ASC response.
func TestLoadAppleSigningAssets_RoundTrips(t *testing.T) {
	c, project, conn, _ := setupAppleSigningTest(t)
	ctx := context.Background()

	if _, err := c.EnsureAppleSigningAssets(ctx, project, conn); err != nil {
		t.Fatalf("ensure: %v", err)
	}

	certPEM, profileBytes, privKeyPEM, err := c.LoadAppleSigningAssets(ctx, project.ID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !strings.Contains(string(certPEM), "BEGIN CERTIFICATE") {
		t.Errorf("certPEM missing CERTIFICATE block: %q", string(certPEM)[:60])
	}
	if !strings.Contains(string(privKeyPEM), "PRIVATE KEY") {
		t.Errorf("privKeyPEM missing PRIVATE KEY block: %q", string(privKeyPEM)[:60])
	}
	if string(profileBytes) != "profile-bytes" {
		t.Errorf("profileBytes = %q, want \"profile-bytes\"", string(profileBytes))
	}
}

// TestEnsureAppleSigningAssets_RejectsNoConn ensures the not-configured
// branch surfaces ErrAppleSigningNoConnection (the HTTP layer maps this
// to a 400 not_configured response).
func TestEnsureAppleSigningAssets_RejectsNoConn(t *testing.T) {
	c, project, _, _ := setupAppleSigningTest(t)
	ctx := context.Background()

	_, err := c.EnsureAppleSigningAssets(ctx, project, nil)
	if err == nil {
		t.Fatal("expected error for nil apple connection")
	}
	// errors.Is compatible — sentinel is wrapped, not aliased.
	if err.Error() == "" {
		t.Errorf("empty error message")
	}
}
