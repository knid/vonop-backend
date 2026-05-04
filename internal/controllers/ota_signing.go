package controllers

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
)

// otaSigningKeyKind is the CredStore "kind" tag for OTA-update RSA private
// keys. Distinct from the apple/google/keystore kinds so blob lookups stay
// scoped to this feature.
const otaSigningKeyKind = "ota-signing-rsa"

// otaSigningKeyValidity is the certificate self-sign lifetime. 10y matches
// the "long enough that we never have to think about it" property — the
// cert is bundled in the installed binary, so rotating means shipping a
// new binary anyway.
const otaSigningKeyValidity = 10 * 365 * 24 * time.Hour

// otaSigningKeyBits is the RSA modulus size. 2048 is the floor expo-updates
// is documented against; bigger costs us nothing at sign time and the cert
// goes into the binary so we don't pay it on every manifest fetch.
const otaSigningKeyBits = 2048

// EnsureOTAUpdateSigningKey returns the per-project signing-key row,
// generating a fresh RSA-2048 keypair + self-signed X.509 cert lazily on
// first call. Idempotent: subsequent calls reuse the stored row.
//
// Threading: two concurrent first-publishes for the same project will both
// generate keys; one INSERT wins, the other hits the PK conflict on
// app_project_id and we re-read the winning row. Both callers end up using
// the same persisted keypair.
//
// The returned row has CredRefPrivate populated; callers wanting to sign
// must Decrypt(CredRefPrivate) via CredStore.
func (c *Controller) EnsureOTAUpdateSigningKey(ctx context.Context, appProjectID uuid.UUID) (*models.OTAUpdateSigningKey, error) {
	row, err := c.DB.GetOTAUpdateSigningKey(ctx, appProjectID)
	if err == nil {
		return row, nil
	}
	if !database.IsErrNoOTASigningKey(err) {
		return nil, fmt.Errorf("ota signing key load: %w", err)
	}
	// Look up the project to (a) get OrganizationID for CredStore put + (b)
	// produce a stable CN for the self-signed cert ("<slug>.wemob-ota").
	project, err := c.DB.GetAppProjectByID(appProjectID.String())
	if err != nil {
		return nil, fmt.Errorf("ota signing key: load project: %w", err)
	}
	if project == nil || project.OrganizationID == uuid.Nil {
		return nil, errors.New("ota signing key: project missing org")
	}

	priv, err := rsa.GenerateKey(rand.Reader, otaSigningKeyBits)
	if err != nil {
		return nil, fmt.Errorf("ota signing key: rsa gen: %w", err)
	}
	certPEM, err := selfSignOTACert(priv, project.Slug)
	if err != nil {
		return nil, fmt.Errorf("ota signing key: cert sign: %w", err)
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("ota signing key: pkcs8 marshal: %w", err)
	}
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8})

	credRef, err := c.CredStore.Put(ctx, project.OrganizationID, otaSigningKeyKind, privPEM)
	if err != nil {
		return nil, fmt.Errorf("ota signing key: credstore put: %w", err)
	}

	keyID, err := computeKeyID(certPEM)
	if err != nil {
		return nil, fmt.Errorf("ota signing key: keyid compute: %w", err)
	}

	newRow := &models.OTAUpdateSigningKey{
		AppProjectID:   appProjectID,
		OrganizationID: project.OrganizationID,
		CredRefPrivate: credRefFromStorage(credRef),
		CertificatePEM: string(certPEM),
		Algorithm:      "rsa-v1_5-sha256",
		KeyID:          keyID,
		CreatedAt:      time.Now().UTC(),
	}
	if err := c.DB.InsertOTAUpdateSigningKey(ctx, newRow); err != nil {
		// Race: another goroutine inserted first. Re-read the persisted row
		// and use that one — both rows would have valid keys, but only one
		// is durable, and that's the row the binary's cert will match.
		if existing, lookupErr := c.DB.GetOTAUpdateSigningKey(ctx, appProjectID); lookupErr == nil {
			return existing, nil
		}
		return nil, fmt.Errorf("ota signing key: insert: %w", err)
	}
	return newRow, nil
}

// LoadOTAUpdateSigningPrivateKeyPEM decrypts and returns the PKCS#8 PEM
// private key bytes for a project's signing key. Caller is responsible for
// scrubbing the bytes from memory if the threat model requires it (we
// don't, since we hold them only during a single sign call).
func (c *Controller) LoadOTAUpdateSigningPrivateKeyPEM(ctx context.Context, row *models.OTAUpdateSigningKey) ([]byte, error) {
	if row == nil || row.CredRefPrivate == nil {
		return nil, errors.New("ota signing key: row missing CredRefPrivate")
	}
	pemBytes, err := c.CredStore.Get(ctx, row.OrganizationID, credRefToStorage(row.CredRefPrivate))
	if err != nil {
		return nil, fmt.Errorf("ota signing key: credstore get: %w", err)
	}
	return pemBytes, nil
}

// selfSignOTACert builds and PEM-encodes a self-signed X.509 certificate
// over priv's public key. CN = "<slug>.wemob-ota" (slug is unique per
// AppProject, so the CN distinguishes one project's cert from another's
// in any external chain inspection).
//
// SerialNumber is a fresh 128-bit random value (collision-resistant per
// project, doesn't need to be globally unique since the cert is self-signed).
func selfSignOTACert(priv *rsa.PrivateKey, slug string) ([]byte, error) {
	if slug == "" {
		slug = "wemob-app"
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("serial random: %w", err)
	}
	now := time.Now().UTC()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   slug + ".wemob-ota",
			Organization: []string{"Wemob"},
		},
		NotBefore: now.Add(-1 * time.Hour),
		NotAfter:  now.Add(otaSigningKeyValidity),
		KeyUsage:  x509.KeyUsageDigitalSignature,
		// expo-updates only checks digital-signature usage; we don't add
		// ExtKeyUsage so the cert isn't confusable with a TLS server cert.
		BasicConstraintsValid: true,
		IsCA:                  false,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		return nil, fmt.Errorf("create cert: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), nil
}

// computeKeyID derives the stable keyid sent in the `expo-signature`
// header from the cert's PEM bytes (SHA-256 → first 16 hex chars). The
// binary's `codeSigningMetadata.keyid` MUST match this same value, so the
// derivation rule lives in this single place; the codemagic step reads
// the keyid from the backend rather than re-deriving it.
func computeKeyID(certPEM []byte) (string, error) {
	if len(certPEM) == 0 {
		return "", errors.New("empty certPEM")
	}
	sum := sha256.Sum256(certPEM)
	return hex.EncodeToString(sum[:])[:16], nil
}
