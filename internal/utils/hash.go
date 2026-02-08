package utils

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// GetSHA256Hash is kept for backwards compatibility during migration.
func GetSHA256Hash(str string) string {
	h := sha256.New()
	h.Write([]byte(str))

	return hex.EncodeToString(h.Sum(nil))
}

// HashPassword creates a bcrypt hash of the password.
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// CheckPassword compares a password against a hash. Supports both bcrypt
// and legacy SHA-256 hashes (for migration).
func CheckPassword(password, hash string) bool {
	// bcrypt hashes start with "$2a$" or "$2b$"
	if strings.HasPrefix(hash, "$2") {
		err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
		return err == nil
	}
	// Legacy SHA-256 comparison
	return hash == GetSHA256Hash(password)
}

// IsBcryptHash returns true if the hash is already bcrypt.
func IsBcryptHash(hash string) bool {
	return strings.HasPrefix(hash, "$2")
}
