package storeauth

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// AppleConnectionInput is the minimum set of fields needed to mint an
// App Store Connect JWT. Kept decoupled from the DB model so callers
// (validation, polling) can both use it without circular imports.
type AppleConnectionInput struct {
	KeyID    string
	IssuerID string
	// P8PEM is the raw bytes of the .p8 file as downloaded from App
	// Store Connect.
	P8PEM []byte
}

// AppleToken signs a 10-minute App Store Connect JWT.
func AppleToken(in AppleConnectionInput) (string, error) {
	privateKey, err := parseECPrivateKeyFromP8(in.P8PEM)
	if err != nil {
		return "", err
	}
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"iss": in.IssuerID,
		"iat": now.Unix(),
		"exp": now.Add(10 * time.Minute).Unix(),
		"aud": "appstoreconnect-v1",
	})
	token.Header["kid"] = in.KeyID
	token.Header["typ"] = "JWT"
	return token.SignedString(privateKey)
}

func parseECPrivateKeyFromP8(pemBytes []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found in .p8 file")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse PKCS8 private key: %w", err)
	}
	ecKey, ok := key.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("key is not an EC private key (got %T)", key)
	}
	return ecKey, nil
}
