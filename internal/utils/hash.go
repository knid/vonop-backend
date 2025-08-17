package utils

import "crypto/sha256"

func GetSHA256Hash(str string) string {
	h := sha256.New()
	h.Write([]byte(str))

	return string(h.Sum(nil))
}
