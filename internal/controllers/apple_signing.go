package controllers

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/storeauth"
)

// CredStore "kind" tags for apple-signing assets — distinct from
// apple-p8 / android-keystore so blob lookups stay scoped per-feature.
const (
	appleDistCertKind = "apple-dist-cert"
	appleProfileKind  = "apple-profile"
)

// appleCertKeyBits is the RSA modulus size for the distribution cert
// keypair. 2048 is the floor Apple accepts; 3072 / 4096 add latency without
// security gain for code-signing certs that the App Store re-signs anyway.
const appleCertKeyBits = 2048

// appleRenewalThreshold is how close to expiry a cert / profile must be
// before EnsureAppleSigningAssets re-mints it. The credential health cron
// uses the same threshold so renewals happen ahead of build-time pressure.
const appleRenewalThreshold = 30 * 24 * time.Hour

// ErrAppleSigningNoConnection is returned when EnsureAppleSigningAssets is
// called without an Apple connection — auto-signing requires the user to
// link an ASC API key first.
var ErrAppleSigningNoConnection = errors.New("apple signing: no apple connection provided")

// EnsureAppleSigningAssets returns a valid AppleSigningAssets row for project,
// generating fresh distribution cert + provisioning profile via the App
// Store Connect API on first call (or when the existing assets are within
// `appleRenewalThreshold` of expiry).
//
// Threading: two concurrent first-generates for the same project can both
// hit the cert / profile creation calls (we accept the ASC quota cost — it
// only happens on a true cold start), but the unique constraint on
// app_project_id ensures only one row persists. Race losers re-read the
// winning row and return that.
//
// Caller (HTTP layer) is responsible for confirming `appleConnection`
// belongs to the same org as `project`.
func (c *Controller) EnsureAppleSigningAssets(ctx context.Context, project *models.AppProject, appleConnection *models.AppleConnection) (*models.AppleSigningAssets, error) {
	if project == nil {
		return nil, errors.New("apple signing: nil project")
	}
	if appleConnection == nil {
		return nil, ErrAppleSigningNoConnection
	}
	if project.OrganizationID != appleConnection.OrganizationID {
		return nil, errors.New("apple signing: project / connection org mismatch")
	}

	// Reuse path: existing row that's not expiring soon.
	existing, err := c.DB.GetAppleSigningAssets(ctx, project.ID)
	if err == nil {
		nearestExpiry := existing.CertificateExpiresAt
		if !existing.ProfileExpiresAt.IsZero() && existing.ProfileExpiresAt.Before(nearestExpiry) {
			nearestExpiry = existing.ProfileExpiresAt
		}
		if !nearestExpiry.IsZero() && time.Until(nearestExpiry) > appleRenewalThreshold {
			return existing, nil
		}
		// Fall through to re-mint.
	} else if !database.IsErrNoAppleSigningAssets(err) {
		return nil, fmt.Errorf("apple signing: load existing: %w", err)
	}

	// Need a fresh set. Mint cert + bundle + profile via ASC.
	row, err := c.generateAppleSigningAssets(ctx, project, appleConnection)
	if err != nil {
		return nil, err
	}

	// Persist (upsert handles both first-create and renewal).
	if _, err := c.DB.UpsertAppleSigningAssets(ctx, row); err != nil {
		// Race: another goroutine inserted first. Re-read and return.
		if existing, lookupErr := c.DB.GetAppleSigningAssets(ctx, project.ID); lookupErr == nil {
			return existing, nil
		}
		return nil, fmt.Errorf("apple signing: persist row: %w", err)
	}
	return row, nil
}

// generateAppleSigningAssets does the actual ASC orchestration: CSR ->
// /v1/certificates -> /v1/bundleIds (lookup or create) -> /v1/profiles ->
// CredStore put. Returns the unsaved row; caller persists.
func (c *Controller) generateAppleSigningAssets(ctx context.Context, project *models.AppProject, conn *models.AppleConnection) (*models.AppleSigningAssets, error) {
	bundleID := bundleIDForProject(project, "")

	// 1) Generate keypair + CSR.
	priv, err := rsa.GenerateKey(rand.Reader, appleCertKeyBits)
	if err != nil {
		return nil, fmt.Errorf("apple signing: rsa gen: %w", err)
	}
	csrPEM, err := buildAppleCSR(priv, project.ID.String())
	if err != nil {
		return nil, fmt.Errorf("apple signing: csr build: %w", err)
	}

	// 2) Load p8, mint JWT input.
	p8Bytes, err := c.ReadApplePrimaryP8(ctx, conn)
	if err != nil {
		return nil, fmt.Errorf("apple signing: read p8: %w", err)
	}
	in := storeauth.AppleConnectionInput{
		KeyID:    conn.KeyID,
		IssuerID: conn.IssuerID,
		P8PEM:    p8Bytes,
	}

	// 3) Create distribution certificate.
	cert, err := storeauth.CreateCertificate(ctx, in, csrPEM, "IOS_DISTRIBUTION")
	if err != nil {
		return nil, fmt.Errorf("apple signing: ASC create certificate: %w", err)
	}

	// 4) Look up or create the bundle id.
	existingBundle, err := storeauth.FetchBundleIDByIdentifier(ctx, in, bundleID)
	if err != nil {
		return nil, fmt.Errorf("apple signing: lookup bundle id: %w", err)
	}
	var bundleRowID string
	if existingBundle != nil {
		bundleRowID = existingBundle.ID
	} else {
		created, err := storeauth.CreateBundleID(ctx, in, bundleID, project.Name)
		if err != nil {
			return nil, fmt.Errorf("apple signing: create bundle id: %w", err)
		}
		bundleRowID = created.ID
	}

	// 5) Create the App Store distribution provisioning profile.
	profileName := fmt.Sprintf("Wemob %s App Store", project.Slug)
	profile, err := storeauth.CreateProfile(ctx, in, profileName, "IOS_APP_STORE", bundleRowID, cert.ID, nil)
	if err != nil {
		return nil, fmt.Errorf("apple signing: create profile: %w", err)
	}

	// 6) Encode private key + cert PEM together for CredStore. We stash the
	//    PKCS#8 private key + cert DER (as a CERTIFICATE PEM block) in a
	//    single blob — the worker reconstructs the .p12 from these.
	certDER, err := base64.StdEncoding.DecodeString(cert.CertificateContent)
	if err != nil {
		return nil, fmt.Errorf("apple signing: decode certificateContent: %w", err)
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("apple signing: pkcs8 marshal: %w", err)
	}
	certBundle := append(
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8}),
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})...,
	)

	certRef, err := c.CredStore.Put(ctx, project.OrganizationID, appleDistCertKind, certBundle)
	if err != nil {
		return nil, fmt.Errorf("apple signing: credstore put cert: %w", err)
	}

	// 7) Profile bytes — ProfileContent is base64 of the .mobileprovision blob.
	profileDER, err := base64.StdEncoding.DecodeString(profile.ProfileContent)
	if err != nil {
		return nil, fmt.Errorf("apple signing: decode profileContent: %w", err)
	}
	profileRef, err := c.CredStore.Put(ctx, project.OrganizationID, appleProfileKind, profileDER)
	if err != nil {
		return nil, fmt.Errorf("apple signing: credstore put profile: %w", err)
	}

	row := &models.AppleSigningAssets{
		AppProjectID:         project.ID,
		OrganizationID:       project.OrganizationID,
		AppleConnectionID:    conn.ID,
		CertificateID:        cert.ID,
		CertificateCredRef:   credRefFromStorage(certRef),
		CertificateExpiresAt: cert.ExpiresAt,
		ProfileID:            profile.ID,
		ProfileCredRef:       credRefFromStorage(profileRef),
		ProfileExpiresAt:     profile.ExpiresAt,
		BundleID:             bundleID,
		CreatedAt:            time.Now().UTC(),
		UpdatedAt:            time.Now().UTC(),
	}
	return row, nil
}

// LoadAppleSigningAssets decrypts both CredRefs for projectID and returns
// the cert PEM + private key PEM (as a single combined blob — CERTIFICATE
// + PRIVATE KEY block) and the .mobileprovision bytes.
//
// Returns (certPEM, profileBytes, privKeyPEM, error). The cert + private
// key live in a single CredStore blob so we can't return them separately
// without re-parsing — splitting them here keeps callers (HMAC handler,
// renewal job) simple.
func (c *Controller) LoadAppleSigningAssets(ctx context.Context, projectID uuid.UUID) (certPEM []byte, profileBytes []byte, privKeyPEM []byte, err error) {
	row, err := c.DB.GetAppleSigningAssets(ctx, projectID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("apple signing: load row: %w", err)
	}
	if row == nil || row.CertificateCredRef == nil || row.ProfileCredRef == nil {
		return nil, nil, nil, errors.New("apple signing: row missing CredRefs")
	}
	certBundle, err := c.CredStore.Get(ctx, row.OrganizationID, credRefToStorage(row.CertificateCredRef))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("apple signing: credstore get cert: %w", err)
	}
	profileBytes, err = c.CredStore.Get(ctx, row.OrganizationID, credRefToStorage(row.ProfileCredRef))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("apple signing: credstore get profile: %w", err)
	}

	// Split the combined blob into private key + certificate PEM.
	priv, certPEMOut := splitAppleCertBundle(certBundle)
	if len(priv) == 0 || len(certPEMOut) == 0 {
		return nil, nil, nil, errors.New("apple signing: cert bundle missing PEM block")
	}
	return certPEMOut, profileBytes, priv, nil
}

// buildAppleCSR generates a PEM-encoded PKCS#10 CertificateRequest using
// CN=`wemob:<projectID>`, O="Wemob". Apple's ASC API doesn't read the CN
// (it overwrites with the team's display name) but a stable CN helps when
// debugging the cert chain after signing.
func buildAppleCSR(priv *rsa.PrivateKey, projectID string) ([]byte, error) {
	subj := pkix.Name{
		CommonName:   "wemob:" + projectID,
		Organization: []string{"Wemob"},
	}
	tmpl := &x509.CertificateRequest{
		Subject:            subj,
		SignatureAlgorithm: x509.SHA256WithRSA,
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, tmpl, priv)
	if err != nil {
		return nil, fmt.Errorf("create csr: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}), nil
}

// splitAppleCertBundle decodes a PEM bundle (PRIVATE KEY + CERTIFICATE) and
// returns each as separate PEM-encoded byte slices. Returns empty slices
// when a block type is missing.
func splitAppleCertBundle(bundle []byte) (privPEM []byte, certPEM []byte) {
	rest := bundle
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			return privPEM, certPEM
		}
		switch block.Type {
		case "PRIVATE KEY", "RSA PRIVATE KEY":
			privPEM = pem.EncodeToMemory(block)
		case "CERTIFICATE":
			certPEM = pem.EncodeToMemory(block)
		}
	}
}
