// signing.go — pure-function helpers for expo-updates manifest signing
// (P0 #8a). The HTTP handler in internal/views/ota_manifest_view.go is the
// only caller; this file is unit-testable and has no I/O dependencies.
//
// Algorithm: rsa-v1_5-sha256 (the only mode `expo-updates` supports). The
// client (expo-updates SDK) verifies the manifest part bytes using the
// X.509 certificate bundled in the binary at build time. See
// https://docs.expo.dev/eas-update/code-signing/.
//
// The signed payload is the manifest JSON BODY (same bytes the client
// sees when reading the "manifest" multipart part) — NOT the multipart
// wrapper. This matches expo-updates' verification path.
package ota

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
)

// SigningAlgorithmRSAv15SHA256 is the only algorithm expo-updates supports
// for manifest code-signing. We refuse to deviate.
const SigningAlgorithmRSAv15SHA256 = "rsa-v1_5-sha256"

// SignManifest produces the value of the `expo-signature` response header
// for a given manifest body. Header format (Structured Field Values):
//
//	sig="<base64 signature>", keyid="<key_id>", alg="rsa-v1_5-sha256"
//
// Inputs:
//   - manifestBody: the bytes the client reads as the "manifest" multipart
//     part body (the raw JSON before multipart wrapping).
//   - privateKeyPEM: PKCS#8 PEM-encoded RSA private key (decrypted by the
//     caller from CredStore).
//   - keyID: short stable identifier matching the keyid the binary's
//     `codeSigningMetadata` is built with.
func SignManifest(manifestBody, privateKeyPEM []byte, keyID string) (string, error) {
	if keyID == "" {
		return "", errors.New("ota: empty keyID")
	}
	priv, err := parseRSAPrivateKey(privateKeyPEM)
	if err != nil {
		return "", fmt.Errorf("ota: parse private key: %w", err)
	}
	digest := sha256.Sum256(manifestBody)
	sig, err := rsa.SignPKCS1v15(nil, priv, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("ota: sign: %w", err)
	}
	return formatExpoSignatureHeader(sig, keyID, SigningAlgorithmRSAv15SHA256), nil
}

// VerifyManifestSignature is the test-only inverse of SignManifest: parse
// the cert PEM, decode the base64 signature, compute SHA-256 of the
// manifest body, and verify with the certificate's public key.
//
// Returns nil on a valid signature.
func VerifyManifestSignature(manifestBody, certPEM []byte, signatureBase64 string) error {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return errors.New("ota: cert pem decode: empty block")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("ota: cert parse: %w", err)
	}
	pub, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok {
		return errors.New("ota: cert public key is not RSA")
	}
	sigBytes, err := base64.StdEncoding.DecodeString(signatureBase64)
	if err != nil {
		return fmt.Errorf("ota: signature base64: %w", err)
	}
	digest := sha256.Sum256(manifestBody)
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, digest[:], sigBytes); err != nil {
		return fmt.Errorf("ota: signature verify: %w", err)
	}
	return nil
}

// formatExpoSignatureHeader builds the Structured Field Values dict-style
// header value expo-updates parses. Field order is sig, keyid, alg —
// matches the reference impl in expo's docs and tests.
func formatExpoSignatureHeader(sigBytes []byte, keyID, alg string) string {
	return fmt.Sprintf(`sig="%s", keyid="%s", alg="%s"`,
		base64.StdEncoding.EncodeToString(sigBytes), keyID, alg)
}

// parseRSAPrivateKey decodes PEM and unmarshals PKCS#8 OR PKCS#1, returning
// an *rsa.PrivateKey. We default to writing PKCS#8 in
// EnsureOTAUpdateSigningKey but accept PKCS#1 too in case an admin replaces
// the blob in CredStore manually.
func parseRSAPrivateKey(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("pem decode: empty block")
	}
	// PKCS#8 first — that's what we write.
	if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		rsaKey, ok := k.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("pkcs8 key is not RSA")
		}
		return rsaKey, nil
	}
	// PKCS#1 fallback.
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	return nil, errors.New("unrecognized RSA private key encoding (tried PKCS#8 and PKCS#1)")
}
