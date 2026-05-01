package utils

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
)

// EncryptedValuePrefix marks a string as already AES-GCM encrypted in
// AppIntegration jsonb columns. Format-discovery prefix lets read paths
// tell encrypted from legacy plaintext without a schema migration on the
// JSON column itself.
const EncryptedValuePrefix = "enc:v1:"

// EncryptToken encrypts plaintext using AES-256-GCM.
// key must be a 64-char hex string (32 bytes decoded).
func EncryptToken(plaintext, hexKey string) (string, error) {
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return "", fmt.Errorf("encryption: invalid hex key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("encryption: %w", err)
	}
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("encryption: %w", err)
	}
	nonce := make([]byte, aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("encryption: %w", err)
	}
	ciphertext := aesGCM.Seal(nonce, nonce, []byte(plaintext), nil)
	return hex.EncodeToString(ciphertext), nil
}

// DecryptToken decrypts a hex-encoded AES-256-GCM ciphertext.
func DecryptToken(hexCiphertext, hexKey string) (string, error) {
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return "", fmt.Errorf("decryption: invalid hex key: %w", err)
	}
	ciphertext, err := hex.DecodeString(hexCiphertext)
	if err != nil {
		return "", fmt.Errorf("decryption: invalid hex ciphertext: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("decryption: %w", err)
	}
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("decryption: %w", err)
	}
	nonceSize := aesGCM.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", fmt.Errorf("decryption: ciphertext too short")
	}
	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := aesGCM.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decryption: %w", err)
	}
	return string(plaintext), nil
}

// EncryptIntegrationMapAny encrypts every string value in an integration
// credentials map, leaving keys (and non-string values) untouched. Already-
// encrypted values (carrying EncryptedValuePrefix) are skipped so backfills
// and double-encrypts are safe. On encryption failure the original value is
// preserved so a single bad key doesn't lose the whole map.
func EncryptIntegrationMapAny(m map[string]any, hexKey string) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		s, ok := v.(string)
		if !ok || s == "" || strings.HasPrefix(s, EncryptedValuePrefix) {
			out[k] = v
			continue
		}
		enc, err := EncryptToken(s, hexKey)
		if err != nil {
			out[k] = v
			continue
		}
		out[k] = EncryptedValuePrefix + enc
	}
	return out
}

// DecryptIntegrationMapAny is the read-side inverse: values prefixed with
// EncryptedValuePrefix are decrypted, anything else is returned as-is so
// legacy plaintext rows still work during the rollout window.
func DecryptIntegrationMapAny(m map[string]any, hexKey string) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		s, ok := v.(string)
		if !ok || !strings.HasPrefix(s, EncryptedValuePrefix) {
			out[k] = v
			continue
		}
		dec, err := DecryptToken(strings.TrimPrefix(s, EncryptedValuePrefix), hexKey)
		if err != nil {
			out[k] = v
			continue
		}
		out[k] = dec
	}
	return out
}

// EncryptIntegrationMapStr / DecryptIntegrationMapStr are the
// map[string]string variants used for AppIntegration.EnvVars.
func EncryptIntegrationMapStr(m map[string]string, hexKey string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		if v == "" || strings.HasPrefix(v, EncryptedValuePrefix) {
			out[k] = v
			continue
		}
		enc, err := EncryptToken(v, hexKey)
		if err != nil {
			out[k] = v
			continue
		}
		out[k] = EncryptedValuePrefix + enc
	}
	return out
}

func DecryptIntegrationMapStr(m map[string]string, hexKey string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		if !strings.HasPrefix(v, EncryptedValuePrefix) {
			out[k] = v
			continue
		}
		dec, err := DecryptToken(strings.TrimPrefix(v, EncryptedValuePrefix), hexKey)
		if err != nil {
			out[k] = v
			continue
		}
		out[k] = dec
	}
	return out
}
